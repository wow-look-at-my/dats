package runtime

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed files/*
var Files embed.FS

// ExtractTo extracts all embedded runtime files to the specified directory.
// Returns the path to test_helper.bash within the extracted directory.
func ExtractTo(destDir string) (string, error) {
	err := fs.WalkDir(Files, "files", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the root "files" directory itself
		if path == "files" {
			return nil
		}

		// Calculate destination path (strip "files/" prefix)
		relPath, err := filepath.Rel("files", path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(destDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		// Read and write file
		data, err := Files.ReadFile(path)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		return os.WriteFile(destPath, data, 0644)
	})

	if err != nil {
		return "", err
	}

	return filepath.Join(destDir, "test_helper.bash"), nil
}
