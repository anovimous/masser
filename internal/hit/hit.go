package hit

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/anovimous/masser/internal/config"
	"github.com/anovimous/masser/internal/matcher"
)

// Engine decides whether a response is a hit, given a baseline response.
type Engine struct {
	cfg     *config.Config
	hr      *regexp.Regexp
	anyRule bool
}

func New(cfg *config.Config) (*Engine, error) {
	e := &Engine{cfg: cfg}
	if cfg.HR != "" {
		r, err := regexp.Compile(cfg.HR)
		if err != nil {
			return nil, fmt.Errorf("-hr: %w", err)
		}
		e.hr = r
	}
	e.anyRule = cfg.HC || cfg.HS || cfg.HL || cfg.HRF || e.hr != nil
	return e, nil
}

// IsHit returns whether `resp` is considered a hit relative to `baseline`,
// given the fields that were injected into the request.
func (e *Engine) IsHit(baseline, resp *matcher.Response, injectedFields []string) bool {
	if !e.anyRule {
		// Default: any response that reaches this point (already passed
		// matchers/filters) is a hit.
		return true
	}

	checks := []bool{}

	if e.cfg.HC {
		checks = append(checks, baseline == nil || baseline.Status != resp.Status)
	}
	if e.cfg.HS {
		checks = append(checks, baseline == nil || baseline.Size() != resp.Size())
	}
	if e.cfg.HL {
		checks = append(checks, baseline == nil || baseline.Lines() != resp.Lines())
	}
	if e.hr != nil {
		checks = append(checks, e.hr.Match(resp.Body))
	}
	if e.cfg.HRF {
		reflected := false
		for _, f := range injectedFields {
			if f != "" && bytes.Contains(resp.Body, []byte(f)) {
				reflected = true
				break
			}
		}
		checks = append(checks, reflected)
	}

	switch e.cfg.HMode {
	case config.ModeAnd:
		for _, c := range checks {
			if !c {
				return false
			}
		}
		return true
	default:
		for _, c := range checks {
			if c {
				return true
			}
		}
		return false
	}
}
