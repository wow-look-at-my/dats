package dats

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tempDir is t.TempDir() with symlinks resolved.
//
// Discovery resolves a directory root through filepath.EvalSymlinks (see
// findDatsFiles -- WalkDir will not follow a symlinked root, so without it a
// symlinked directory arg yields nothing). On macOS the temp root reached
// via /tmp is itself a symlink to /private/tmp, so discovery legitimately
// returns /private/tmp/... while an unresolved t.TempDir() string still says
// /tmp/... -- the assertion fails on a path difference the product is right
// about. Resolving here makes the expectation match what discovery returns,
// on every platform (a no-op where the temp dir is already a real path).
func tempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return dir
	}
	return resolved
}

func TestFindFilesWithArgs(t *testing.T) {
	tmp := tempDir(t)
	datsFile := filepath.Join(tmp, "test.dats")
	require.Nil(t, os.WriteFile(datsFile, []byte("tests:\n\t- cmd: echo hi\n"), 0644))

	files, err := FindFiles([]string{datsFile})
	require.Nil(t, err)
	assert.Equal(t, []string{datsFile}, files)
}

func TestFindFilesWrongExtension(t *testing.T) {
	yamlFile := filepath.Join(t.TempDir(), "test.yaml")
	require.Nil(t, os.WriteFile(yamlFile, []byte(""), 0644))

	_, err := FindFiles([]string{yamlFile})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), ".dats extension")
}

func TestFindFilesNonexistent(t *testing.T) {
	_, err := FindFiles([]string{"/nonexistent/test.dats"})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "cannot access /nonexistent/test.dats")
}

func TestFindFilesStatError(t *testing.T) {
	// A path with a regular file as an intermediate component fails Stat with
	// ENOTDIR rather than ENOENT; it must be reported, not silently accepted.
	tmp := tempDir(t)
	blocker := filepath.Join(tmp, "blocker")
	require.Nil(t, os.WriteFile(blocker, []byte(""), 0644))

	_, err := FindFiles([]string{filepath.Join(blocker, "test.dats")})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "cannot access")
}

func TestFindFilesExplicitHiddenFile(t *testing.T) {
	// Explicitly named files are exempt from the hidden-file discovery rule.
	tmp := tempDir(t)
	hidden := filepath.Join(tmp, ".hidden.dats")
	require.Nil(t, os.WriteFile(hidden, []byte(""), 0644))

	files, err := FindFiles([]string{hidden})
	require.Nil(t, err)
	assert.Equal(t, []string{hidden}, files)
}

func TestFindFilesDiscovery(t *testing.T) {
	tmp := tempDir(t)
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "a.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "b.dats"), []byte(""), 0644))

	t.Chdir(tmp)

	files, err := FindFiles(nil)
	require.Nil(t, err)
	assert.Len(t, files, 2)
}

func TestFindFilesDiscoveryRecursesIntoSubdirectories(t *testing.T) {
	tmp := tempDir(t)
	nested := filepath.Join(tmp, "a", "b", "c")
	hiddenDir := filepath.Join(tmp, ".hidden")
	require.Nil(t, os.MkdirAll(nested, 0755))
	require.Nil(t, os.MkdirAll(hiddenDir, 0755))
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "root.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "a", "shallow.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(nested, "deep.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(hiddenDir, "skipped.dats"), []byte(""), 0644))

	t.Chdir(tmp)

	files, err := FindFiles(nil)
	require.Nil(t, err)
	assert.ElementsMatch(t, []string{
		filepath.Join(tmp, "root.dats"),
		filepath.Join(tmp, "a", "shallow.dats"),
		filepath.Join(nested, "deep.dats"),
	}, files)
}

func TestFindFilesDiscoveryNone(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := FindFiles(nil)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "no .dats files found")
}

func TestFindFilesDirectoryArg(t *testing.T) {
	tmp := tempDir(t)
	subDir := filepath.Join(tmp, "sub")
	hiddenDir := filepath.Join(tmp, ".hidden")
	require.Nil(t, os.MkdirAll(subDir, 0755))
	require.Nil(t, os.MkdirAll(hiddenDir, 0755))
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "root.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(subDir, "nested.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(hiddenDir, "skipped.dats"), []byte(""), 0644))

	files, err := FindFiles([]string{tmp})
	require.Nil(t, err)
	assert.ElementsMatch(t, []string{
		filepath.Join(tmp, "root.dats"),
		filepath.Join(subDir, "nested.dats"),
	}, files)
}

func TestFindFilesDirectoryArgEmpty(t *testing.T) {
	tmp := tempDir(t)

	_, err := FindFiles([]string{tmp})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "no .dats files found in "+tmp)
}

func TestFindFilesSymlinkedDirArg(t *testing.T) {
	// A symlink to a directory stats as a directory, but filepath.WalkDir does
	// not follow a symlink root; without resolving the root first the walk
	// yields nothing and the arg errors with "no .dats files found".
	tmp := tempDir(t)
	realDir := filepath.Join(tmp, "real")
	require.Nil(t, os.MkdirAll(realDir, 0755))
	require.Nil(t, os.WriteFile(filepath.Join(realDir, "linked.dats"), []byte(""), 0644))

	link := filepath.Join(tmp, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	files, err := FindFiles([]string{link})
	require.Nil(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "linked.dats", filepath.Base(files[0]))
}

func TestFindFilesDedupe(t *testing.T) {
	tmp := tempDir(t)
	datsFile := filepath.Join(tmp, "test.dats")
	require.Nil(t, os.WriteFile(datsFile, []byte(""), 0644))

	// The same file named twice explicitly, plus covered by a directory arg,
	// must run exactly once. First-seen order (and spelling) is preserved.
	files, err := FindFiles([]string{datsFile, tmp, datsFile})
	require.Nil(t, err)
	assert.Equal(t, []string{datsFile}, files)
}

func TestFindFilesDedupeRelativeAndAbsolute(t *testing.T) {
	tmp := tempDir(t)
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "test.dats"), []byte(""), 0644))

	t.Chdir(tmp)

	files, err := FindFiles([]string{"test.dats", filepath.Join(tmp, "test.dats")})
	require.Nil(t, err)
	assert.Equal(t, []string{"test.dats"}, files)
}

func TestFindDatsFiles(t *testing.T) {
	tmp := tempDir(t)
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
	tmp := tempDir(t)
	hiddenDir := filepath.Join(tmp, ".git")
	require.Nil(t, os.MkdirAll(hiddenDir, 0755))
	require.Nil(t, os.WriteFile(filepath.Join(hiddenDir, "inside.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "visible.dats"), []byte(""), 0644))

	files, err := findDatsFiles(tmp, io.Discard)
	require.Nil(t, err)
	assert.Equal(t, []string{filepath.Join(tmp, "visible.dats")}, files)
}

func TestFindDatsFilesSkipsHiddenFile(t *testing.T) {
	tmp := tempDir(t)
	require.Nil(t, os.WriteFile(filepath.Join(tmp, ".hidden.dats"), []byte(""), 0644))
	require.Nil(t, os.WriteFile(filepath.Join(tmp, "visible.dats"), []byte(""), 0644))

	files, err := findDatsFiles(tmp, io.Discard)
	require.Nil(t, err)
	assert.Equal(t, []string{filepath.Join(tmp, "visible.dats")}, files)
}

func TestFindDatsFilesHiddenRoot(t *testing.T) {
	// The walk root itself is exempt from the hidden-name rule: running inside
	// (or on) a dotted directory must still discover its contents.
	tmp := tempDir(t)
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

	tmp := tempDir(t)
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
