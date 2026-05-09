package matcher

import (
	"fmt"
	"strconv"
	"strings"
)

// NumberSet represents a comma-separated list of integers and inclusive ranges.
//
// Examples: "200", "200-299", "200-299,301,302".
type NumberSet struct {
	ranges []intRange
}

type intRange struct {
	lo, hi int
}

// ParseNumberSet parses strings like "200-299,301,302".
func ParseNumberSet(s string) (*NumberSet, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return &NumberSet{}, nil
	}
	out := &NumberSet{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			seg := strings.SplitN(part, "-", 2)
			lo, err := strconv.Atoi(strings.TrimSpace(seg[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid range %q: %w", part, err)
			}
			hi, err := strconv.Atoi(strings.TrimSpace(seg[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid range %q: %w", part, err)
			}
			if lo > hi {
				lo, hi = hi, lo
			}
			out.ranges = append(out.ranges, intRange{lo, hi})
		} else {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid number %q: %w", part, err)
			}
			out.ranges = append(out.ranges, intRange{n, n})
		}
	}
	return out, nil
}

func (s *NumberSet) Empty() bool { return s == nil || len(s.ranges) == 0 }

func (s *NumberSet) Contains(n int) bool {
	if s == nil {
		return false
	}
	for _, r := range s.ranges {
		if n >= r.lo && n <= r.hi {
			return true
		}
	}
	return false
}
