package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	yamlFile := filepath.Join(t.TempDir(), "test.yaml")
	require.Nil(t, os.WriteFile(yamlFile, []byte(""), 0644))

	_, err := resolveFiles([]string{yamlFile})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), ".dats extension")
}

func TestResolveFilesNonexistent(t *testing.T) {
	_, err := resolveFiles([]string{"/nonexistent/test.dats"})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "cannot access /nonexistent/test.dats")
}

func TestResolveFilesStatError(t *testing.T) {
	// A path with a regular file as an intermediate component fails Stat with
	// ENOTDIR rather than ENOENT; it must be reported, not silently accepted.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	require.Nil(t, os.WriteFile(blocker, []byte(""), 0644))

	_, err := resolveFiles([]string{filepath.Join(blocker, "test.dats")})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "cannot access")
}

func TestResolveFilesExplicitHiddenFile(t *testing.T) {
	// Explicitly named files are exempt from the hidden-file discovery rule.
	tmp := t.TempDir()
	hidden := filepath.Join(tmp, ".hidden.dats")
	require.Nil(t, os.WriteFile(hidden, []byte(""), 0644))

	files, err := resolveFiles([]string{hidden})
	require.Nil(t, err)
	assert.Equal(t, []string{hidden}, files)
}

func TestResolveFilesDiscovery(t *testing.T) {
	tmp := t.TempDir()
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "a.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "b.dats"), []byte(""), 0644))

	t.Chdir(tmp)

	files, err := resolveFiles(nil)
	require.Nil(t, err)
	assert.Len(t, files, 2)
}

func TestResolveFilesDiscoveryNone(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := resolveFiles(nil)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "no .dats files found")
}

func TestResolveFilesDirectoryArg(t *testing.T) {
	tmp := t.TempDir()
	subDir := filepath.Join(tmp, "sub")
	hiddenDir := filepath.Join(tmp, ".hidden")
	require.Nil(t, os.MkdirAll(subDir, 0755))
	require.Nil(t, os.MkdirAll(hiddenDir, 0755))
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "root.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(subDir, "nested.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(hiddenDir, "skipped.dats"), []byte(""), 0644))

	files, err := resolveFiles([]string{tmp})
	require.Nil(t, err)
	assert.ElementsMatch(t, []string{
		filepath.Join(tmp, "root.dats"),
		filepath.Join(subDir, "nested.dats"),
	}, files)
}

func TestResolveFilesDirectoryArgEmpty(t *testing.T) {
	tmp := t.TempDir()

	_, err := resolveFiles([]string{tmp})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "no .dats files found in "+tmp)
}

func TestResolveFilesDedupe(t *testing.T) {
	tmp := t.TempDir()
	datsFile := filepath.Join(tmp, "test.dats")
	require.Nil(t, os.WriteFile(datsFile, []byte(""), 0644))

	// The same file named twice explicitly, plus covered by a directory arg,
	// must run exactly once. First-seen order (and spelling) is preserved.
	files, err := resolveFiles([]string{datsFile, tmp, datsFile})
	require.Nil(t, err)
	assert.Equal(t, []string{datsFile}, files)
}

func TestResolveFilesDedupeRelativeAndAbsolute(t *testing.T) {
	tmp := t.TempDir()
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "test.dats"), []byte(""), 0644))

	t.Chdir(tmp)

	files, err := resolveFiles([]string{"test.dats", filepath.Join(tmp, "test.dats")})
	require.Nil(t, err)
	assert.Equal(t, []string{"test.dats"}, files)
}

func TestFindDatsFiles(t *testing.T) {
	tmp := t.TempDir()
	subDir := filepath.Join(tmp, "sub")
	require.Nil(t, os.MkdirAll(subDir, 0755))
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "root.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(subDir, "nested.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "ignore.yaml"), []byte(""), 0644))

	files, err := findDatsFiles(tmp, io.Discard)
	require.Nil(t, err)
	assert.ElementsMatch(t, []string{
		filepath.Join(tmp, "root.dats"),
		filepath.Join(subDir, "nested.dats"),
	}, files)
}

func TestFindDatsFilesSkipsHiddenDir(t *testing.T) {
	tmp := t.TempDir()
	hiddenDir := filepath.Join(tmp, ".git")
	require.Nil(t, os.MkdirAll(hiddenDir, 0755))
	require.Nil(t, os.WriteFile(filepath.Join(hiddenDir, "inside.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "visible.dats"), []byte(""), 0644))

	files, err := findDatsFiles(tmp, io.Discard)
	require.Nil(t, err)
	assert.Equal(t, []string{filepath.Join(tmp, "visible.dats")}, files)
}

func TestFindDatsFilesSkipsHiddenFile(t *testing.T) {
	tmp := t.TempDir()
	require.Nil(t, os.WriteFile(filepath.Join(tmp, ".hidden.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "visible.dats"), []byte(""), 0644))

	files, err := findDatsFiles(tmp, io.Discard)
	require.Nil(t, err)
	assert.Equal(t, []string{filepath.Join(tmp, "visible.dats")}, files)
}

func TestFindDatsFilesHiddenRoot(t *testing.T) {
	// The walk root itself is exempt from the hidden-name rule: running inside
	// (or on) a dotted directory must still discover its contents.
	tmp := t.TempDir()
	dottedRoot := filepath.Join(tmp, ".dotted")
	require.Nil(t, os.MkdirAll(dottedRoot, 0755))
	require.Nil(t, os.WriteFile(filepath.Join(dottedRoot, "found.dats"), []byte(""), 0644))

	files, err := findDatsFiles(dottedRoot, io.Discard)
	require.Nil(t, err)
	assert.Equal(t, []string{filepath.Join(dottedRoot, "found.dats")}, files)
}

func TestFindDatsFilesUnreadableDirWarns(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not restrict root")
	}

	tmp := t.TempDir()
	locked := filepath.Join(tmp, "locked")
	require.Nil(t, os.MkdirAll(locked, 0755))
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "visible.dats"), []byte(""), 0644))
	require.Nil(t, os.Chmod(locked, 0000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0755) })

	var warnings bytes.Buffer
	files, err := findDatsFiles(tmp, &warnings)
	require.Nil(t, err)
	assert.Equal(t, []string{filepath.Join(tmp, "visible.dats")}, files)
	assert.Contains(t, warnings.String(), "warning: skipping "+locked)
}
