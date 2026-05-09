package output

import (
	"encoding/json"
	"io"
	"os"

	"github.com/anovimous/masser/internal/config"
	"github.com/anovimous/masser/internal/tester"
)

type fileResult struct {
	RequestID    int64       `json:"requestId"`
	BaselineCode int         `json:"baselineStatus,omitempty"`
	BaselineSize int         `json:"baselineSize,omitempty"`
	Hits         []hitRecord `json:"hits"`
	Errors       []string    `json:"errors,omitempty"`
}

type hitRecord struct {
	Fields []string `json:"fields"`
	Status int      `json:"status,omitempty"`
	Body   string   `json:"body,omitempty"`
}

// Write serializes all results to the configured destination, honouring the
// requested verbosity level.
func Write(cfg *config.Config, results []tester.Result) error {
	out := make([]fileResult, 0, len(results))
	for _, r := range results {
		fr := fileResult{
			RequestID:    r.RequestID,
			BaselineCode: r.BaselineCode,
			BaselineSize: r.BaselineSize,
			Errors:       r.Errors,
		}
		for _, h := range r.Hits {
			rec := hitRecord{Fields: h.Fields}
			switch cfg.Verbosity {
			case config.VerbositySummarized:
				// fields only
			case config.VerbosityNormal:
				rec.Status = h.Status
			case config.VerbosityVerbose:
				rec.Status = h.Status
				rec.Body = string(h.Body)
			}
			fr.Hits = append(fr.Hits, rec)
		}
		out = append(out, fr)
	}

	var w io.Writer = os.Stdout
	if cfg.OutputPath != "" {
		f, err := os.Create(cfg.OutputPath)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
