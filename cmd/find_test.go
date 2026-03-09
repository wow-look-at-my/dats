package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

func TestResolveFilesWithArgs(t *testing.T) {
	tmp := t.TempDir()
	datsFile := filepath.Join(tmp, "test.dats")
	require.Nil(t, os.WriteFile(datsFile, []byte("tests:\n  - cmd: echo hi\n"), 0644))

	files, err := resolveFiles([]string{datsFile})
	require.Nil(t, err)
	assert.Equal(t, []string{datsFile}, files)
}

func TestResolveFilesWrongExtension(t *testing.T) {
	_, err := resolveFiles([]string{"test.yaml"})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), ".dats extension")
}

func TestResolveFilesNonexistent(t *testing.T) {
	_, err := resolveFiles([]string{"/nonexistent/test.dats"})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestResolveFilesDiscovery(t *testing.T) {
	tmp := t.TempDir()
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "a.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "b.dats"), []byte(""), 0644))

	origDir, _ := os.Getwd()
	require.Nil(t, os.Chdir(tmp))
	defer os.Chdir(origDir)

	files, err := resolveFiles(nil)
	require.Nil(t, err)
	assert.Len(t, files, 2)
}

func TestResolveFilesDiscoveryNone(t *testing.T) {
	tmp := t.TempDir()

	origDir, _ := os.Getwd()
	require.Nil(t, os.Chdir(tmp))
	defer os.Chdir(origDir)

	_, err := resolveFiles(nil)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "no .dats files found")
}

func TestFindDatsFiles(t *testing.T) {
	tmp := t.TempDir()
	subDir := filepath.Join(tmp, "sub")
	require.Nil(t, os.MkdirAll(subDir, 0755))
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "root.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(subDir, "nested.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "ignore.yaml"), []byte(""), 0644))

	origDir, _ := os.Getwd()
	require.Nil(t, os.Chdir(tmp))
	defer os.Chdir(origDir)

	files, err := findDatsFiles()
	require.Nil(t, err)
	assert.Len(t, files, 2)
}
