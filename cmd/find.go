package cmd

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// findDatsFiles recursively finds all .dats files under root. Hidden
// directories and hidden files (names starting with ".") are skipped, except
// for root itself, so discovery works even when started inside a dotted
// directory. Paths that cannot be walked (e.g. unreadable directories) are
// reported as a warning on warnw and skipped instead of aborting discovery.
func findDatsFiles(root string, warnw io.Writer) ([]string, error) {
	// filepath.WalkDir does not follow a symlink root (it lstats the root,
	// sees a non-directory, and stops), so a symlinked directory arg would
	// silently yield nothing. Resolve the root only; symlinks encountered
	// during the walk are still not followed. On resolution failure keep the
	// original root and let the walk report the problem.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}

	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			fmt.Fprintf(warnw, "warning: skipping %s: %v\n", path, err)
			return nil
		}
		hidden := path != root && strings.HasPrefix(d.Name(), ".")
		if d.IsDir() {
			if hidden {
				return filepath.SkipDir
			}
			return nil
		}
		if !hidden && filepath.Ext(path) == ".dats" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", root, err)
	}

	return files, nil
}

// resolveFiles expands args into the list of .dats files to process. A file
// arg must have the .dats extension; a directory arg is searched recursively
// with the same rules as no-arg discovery and must contain at least one .dats
// file. Without args, files are discovered from the current directory. The
// result is deduplicated by absolute path, preserving first-seen order.
func resolveFiles(args []string) ([]string, error) {
	if len(args) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getting working directory: %w", err)
		}
		files, err := findDatsFiles(cwd, os.Stderr)
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("no .dats files found in current directory tree")
		}
		return files, nil
	}

	var files []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, fmt.Errorf("cannot access %s: %v", arg, err)
		}
		if info.IsDir() {
			found, err := findDatsFiles(arg, os.Stderr)
			if err != nil {
				return nil, err
			}
			if len(found) == 0 {
				return nil, fmt.Errorf("no .dats files found in %s", arg)
			}
			files = append(files, found...)
			continue
		}
		if filepath.Ext(arg) != ".dats" {
			return nil, fmt.Errorf("input file %s must have .dats extension", arg)
		}
		files = append(files, arg)
	}
	return dedupePaths(files), nil
}

// dedupePaths removes duplicate paths, compared by absolute path, keeping the
// first occurrence (and its original spelling) of each.
func dedupePaths(paths []string) []string {
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
