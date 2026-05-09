package tester

import (
	"context"
	"fmt"

	"github.com/anovimous/masser/internal/config"
	"github.com/anovimous/masser/internal/hit"
	"github.com/anovimous/masser/internal/httpx"
	"github.com/anovimous/masser/internal/injector"
	"github.com/anovimous/masser/internal/logger"
	"github.com/anovimous/masser/internal/matcher"
	"github.com/anovimous/masser/internal/request"
)

// Hit is the in-memory record of a single confirmed hit.
type Hit struct {
	Fields []string
	Status int
	Body   []byte
}

// Result is the aggregate outcome of testing a single source request.
type Result struct {
	RequestID    int64
	BaselineCode int
	BaselineSize int
	Hits         []Hit
	Errors       []string
}

// Tester drives the divide-and-conquer test for a single ParsedRequest.
type Tester struct {
	cfg          *config.Config
	client       *httpx.Client
	replayClient *httpx.Client
	mEngine      *matcher.Engine
	hEngine      *hit.Engine
	log          *logger.Logger
}

func New(cfg *config.Config, client, replayClient *httpx.Client, m *matcher.Engine, h *hit.Engine, log *logger.Logger) *Tester {
	return &Tester{
		cfg:          cfg,
		client:       client,
		replayClient: replayClient,
		mEngine:      m,
		hEngine:      h,
		log:          log,
	}
}

// Run executes the full test plan for one request: baseline, then iterate the
// wordlist in batches of N, narrowing each hit batch via binary search.
func (t *Tester) Run(ctx context.Context, base *request.ParsedRequest, wordlist []string) Result {
	res := Result{RequestID: base.ID}

	// 1. Baseline.
	bStatus, bBody, err := t.client.Do(ctx, base)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("baseline: %v", err))
		t.log.Error("baseline request id=%d failed: %v", base.ID, err)
		return res
	}
	baseline := &matcher.Response{Status: bStatus, Body: bBody}
	res.BaselineCode = bStatus
	res.BaselineSize = len(bBody)
	t.log.Info("baseline id=%d status=%d size=%d", base.ID, bStatus, len(bBody))

	// 2. Walk the wordlist in batches of N.
	n := t.cfg.N
	for i := 0; i < len(wordlist); i += n {
		j := i + n
		if j > len(wordlist) {
			j = len(wordlist)
		}
		batch := wordlist[i:j]
		t.testBatch(ctx, base, baseline, batch, &res)
	}

	return res
}

// testBatch sends a request with all `fields` injected; if it's a hit, it
// recursively splits the batch in halves to isolate the responsible field(s).
func (t *Tester) testBatch(ctx context.Context, base *request.ParsedRequest, baseline *matcher.Response, fields []string, res *Result) {
	if len(fields) == 0 {
		return
	}

	resp, err := t.send(ctx, base, fields)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("batch %v: %v", fields, err))
		t.log.Error("id=%d batch=%v error=%v", base.ID, fields, err)
		return
	}
	// Nothing was actually injected — skip this batch entirely.
	if resp == nil {
		t.log.Debug("id=%d batch=%v skipped (no fields injected)", base.ID, fields)
		return
	}

	// Filter+matcher gate.
	if !t.mEngine.Pass(resp) {
		return
	}

	if !t.hEngine.IsHit(baseline, resp, fields) {
		return
	}

	t.log.Info("id=%d batch=%v hit status=%d", base.ID, fields, resp.Status)

	// If this is a single field, record it. Otherwise, split sequentially.
	if len(fields) == 1 {
		t.recordHit(ctx, base, fields, resp, res)
		return
	}

	mid := len(fields) / 2
	left, right := fields[:mid], fields[mid:]

	// Sequential: fully resolve left, then right.
	t.testBatch(ctx, base, baseline, left, res)
	t.testBatch(ctx, base, baseline, right, res)
}

func (t *Tester) recordHit(ctx context.Context, base *request.ParsedRequest, fields []string, resp *matcher.Response, res *Result) {
	hcopy := Hit{
		Fields: append([]string(nil), fields...),
		Status: resp.Status,
		Body:   append([]byte(nil), resp.Body...),
	}
	res.Hits = append(res.Hits, hcopy)

	if t.replayClient != nil {
		go func() {
			req, _, err := t.buildInjected(base, fields)
			if err != nil {
				t.log.Error("replay build id=%d: %v", base.ID, err)
				return
			}
			if _, _, err := t.replayClient.Do(ctx, req); err != nil {
				t.log.Error("replay send id=%d: %v", base.ID, err)
			}
		}()
	}
}

// send builds an injected request, fires it, and returns a *matcher.Response.
// If the injector reports that no field was actually inserted (because every
// field already existed at every level it would have been injected into),
// send returns (nil, nil) and the caller should treat the batch as a no-op.
func (t *Tester) send(ctx context.Context, base *request.ParsedRequest, fields []string) (*matcher.Response, error) {
	req, didInject, err := t.buildInjected(base, fields)
	if err != nil {
		return nil, err
	}
	if !didInject {
		return nil, nil
	}
	status, body, err := t.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	return &matcher.Response{Status: status, Body: body}, nil
}

// buildInjected clones the base request and injects the given fields into the
// body using the kind detected from the Content-Type. It returns the cloned
// request, a flag reporting whether any field was actually inserted, and any
// error encountered.
func (t *Tester) buildInjected(base *request.ParsedRequest, fields []string) (*request.ParsedRequest, bool, error) {
	clone := base.Clone()
	kind := injector.DetectKind(clone.ContentType())

	var (
		newBody  []byte
		injected bool
		err      error
	)
	switch kind {
	case injector.KindJSON:
		newBody, injected, err = injector.InjectJSON(clone.Body, fields, t.cfg.ValueJSON)
	case injector.KindForm:
		newBody, injected, err = injector.InjectForm(clone.Body, fields, t.cfg.ValueJSON)
	default:
		// Content-Type missing or unknown: try JSON first, fall back to form.
		newBody, injected, err = injector.InjectJSON(clone.Body, fields, t.cfg.ValueJSON)
		if err != nil {
			newBody, injected, err = injector.InjectForm(clone.Body, fields, t.cfg.ValueJSON)
		}
	}
	if err != nil {
		return nil, false, err
	}
	clone.Body = newBody
	clone.Headers.Set("Content-Length", fmt.Sprintf("%d", len(newBody)))
	return clone, injected, nil
}
