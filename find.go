package dats

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/wow-look-at-my/dats/internal/paths"
)

// findDatsFiles recursively finds all .dats files under root.
func findDatsFiles(root string, warnw io.Writer) ([]string, error) {
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

// FindFiles expands paths into the list of .dats files to run.
func FindFiles(args []string) ([]string, error) {
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
	return paths.Dedupe(files), nil
}
