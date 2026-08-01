// Package paths holds path helpers shared by the library and the CLI.
package paths

import "path/filepath"

// Dedupe removes duplicate paths, compared by absolute path, keeping the
// first occurrence (and its original spelling) of each.
func Dedupe(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		key := p
		if abs, err := filepath.Abs(p); err == nil {
			key = abs
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	return out
}
