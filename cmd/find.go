package cmd

import (
	"fmt"
	"os"
	"path/filepath"
)

// findDatsFiles recursively finds all .dats files starting from the current directory.
func findDatsFiles() ([]string, error) {
	var files []string
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting working directory: %w", err)
	}

	err = filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) == ".dats" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	return files, nil
}

// resolveFiles returns the provided files, or discovers them recursively if none are given.
func resolveFiles(args []string) ([]string, error) {
	if len(args) > 0 {
		// Validate provided files
		for _, path := range args {
			if filepath.Ext(path) != ".dats" {
				return nil, fmt.Errorf("input file %s must have .dats extension", path)
			}
			if _, err := os.Stat(path); os.IsNotExist(err) {
				return nil, fmt.Errorf("input file %s does not exist", path)
			}
		}
		return args, nil
	}

	files, err := findDatsFiles()
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .dats files found in current directory tree")
	}
	return files, nil
}
