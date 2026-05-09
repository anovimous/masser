package matcher

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/anovimous/masser/internal/config"
)

// Response is the minimal view of an HTTP response that matchers and filters
// need.
type Response struct {
	Status int
	Body   []byte
}

func (r *Response) Size() int  { return len(r.Body) }
func (r *Response) Lines() int { return countLines(r.Body) }
func (r *Response) Words() int { return countWords(r.Body) }

func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := 1
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}

func countWords(b []byte) int {
	return len(strings.Fields(string(b)))
}

// Engine bundles the parsed matcher and filter rules so they can be applied
// repeatedly without re-parsing.
type Engine struct {
	mc, ml, ms, mw *NumberSet
	mr             *regexp.Regexp
	mcSpecified    bool
	mMode          config.Mode

	fc, fl, fs, fw *NumberSet
	fr             *regexp.Regexp
	fMode          config.Mode
}

func New(cfg *config.Config) (*Engine, error) {
	e := &Engine{mMode: cfg.MMode, fMode: cfg.FMode}

	var err error

	if cfg.MC != "" {
		e.mcSpecified = true
		if e.mc, err = ParseNumberSet(cfg.MC); err != nil {
			return nil, fmt.Errorf("-mc: %w", err)
		}
	}
	if cfg.ML != "" {
		if e.ml, err = ParseNumberSet(cfg.ML); err != nil {
			return nil, fmt.Errorf("-ml: %w", err)
		}
	}
	if cfg.MS != "" {
		if e.ms, err = ParseNumberSet(cfg.MS); err != nil {
			return nil, fmt.Errorf("-ms: %w", err)
		}
	}
	if cfg.MW != "" {
		if e.mw, err = ParseNumberSet(cfg.MW); err != nil {
			return nil, fmt.Errorf("-mw: %w", err)
		}
	}
	if cfg.MR != "" {
		if e.mr, err = regexp.Compile(cfg.MR); err != nil {
			return nil, fmt.Errorf("-mr: %w", err)
		}
	}

	if cfg.FC != "" {
		if e.fc, err = ParseNumberSet(cfg.FC); err != nil {
			return nil, fmt.Errorf("-fc: %w", err)
		}
	}
	if cfg.FL != "" {
		if e.fl, err = ParseNumberSet(cfg.FL); err != nil {
			return nil, fmt.Errorf("-fl: %w", err)
		}
	}
	if cfg.FS != "" {
		if e.fs, err = ParseNumberSet(cfg.FS); err != nil {
			return nil, fmt.Errorf("-fs: %w", err)
		}
	}
	if cfg.FW != "" {
		if e.fw, err = ParseNumberSet(cfg.FW); err != nil {
			return nil, fmt.Errorf("-fw: %w", err)
		}
	}
	if cfg.FR != "" {
		if e.fr, err = regexp.Compile(cfg.FR); err != nil {
			return nil, fmt.Errorf("-fr: %w", err)
		}
	}

	return e, nil
}

// Pass returns true if a response should continue on to hit detection. Filters
// are applied first (drop on match) and then matchers (keep on match).
func (e *Engine) Pass(r *Response) bool {
	if e.dropByFilter(r) {
		return false
	}
	return e.keepByMatcher(r)
}

func (e *Engine) dropByFilter(r *Response) bool {
	checks := []*bool{}

	if e.fc != nil {
		v := e.fc.Contains(r.Status)
		checks = append(checks, &v)
	}
	if e.fl != nil {
		v := e.fl.Contains(r.Lines())
		checks = append(checks, &v)
	}
	if e.fs != nil {
		v := e.fs.Contains(r.Size())
		checks = append(checks, &v)
	}
	if e.fw != nil {
		v := e.fw.Contains(r.Words())
		checks = append(checks, &v)
	}
	if e.fr != nil {
		v := e.fr.Match(r.Body)
		checks = append(checks, &v)
	}

	if len(checks) == 0 {
		return false
	}
	return combine(checks, e.fMode)
}

func (e *Engine) keepByMatcher(r *Response) bool {
	checks := []*bool{}

	if e.mcSpecified && e.mc != nil {
		v := e.mc.Contains(r.Status)
		checks = append(checks, &v)
	}
	if e.ml != nil {
		v := e.ml.Contains(r.Lines())
		checks = append(checks, &v)
	}
	if e.ms != nil {
		v := e.ms.Contains(r.Size())
		checks = append(checks, &v)
	}
	if e.mw != nil {
		v := e.mw.Contains(r.Words())
		checks = append(checks, &v)
	}
	if e.mr != nil {
		v := e.mr.Match(r.Body)
		checks = append(checks, &v)
	}

	// "If no -mc specified then all are matched" -> when no matcher rule is
	// active at all, everything passes.
	if len(checks) == 0 {
		return true
	}
	return combine(checks, e.mMode)
}

func combine(checks []*bool, mode config.Mode) bool {
	switch mode {
	case config.ModeAnd:
		for _, c := range checks {
			if !*c {
				return false
			}
		}
		return true
	default: // OR
		for _, c := range checks {
			if *c {
				return true
			}
		}
		return false
	}
}
