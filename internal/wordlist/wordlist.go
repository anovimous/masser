package wordlist

import (
	"bufio"
	"os"
	"strings"
)

// Load reads each path, merges entries, trims whitespace, drops empties and
// comments (#) and deduplicates while preserving first-seen order.
func Load(paths ...string) ([]string, error) {
	seen := make(map[string]struct{})
	var out []string

	for _, p := range paths {
		if p == "" {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if _, ok := seen[line]; ok {
				continue
			}
			seen[line] = struct{}{}
			out = append(out, line)
		}
		closeErr := f.Close()
		if err := sc.Err(); err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	return out, nil
}
