package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/dats/schema"
)

func TestAssertContains(t *testing.T) {
	assert.Nil(t, AssertContains("hello world", "hello"))
	assert.Nil(t, AssertContains("hello world", "world"))
	assert.NotNil(t, AssertContains("hello world", "missing"))
}

func TestRefuteContains(t *testing.T) {
	assert.Nil(t, RefuteContains("hello world", "missing"))
	assert.NotNil(t, RefuteContains("hello world", "hello"))
}

func TestAssertLineRegex(t *testing.T) {
	lines := []string{"first line", "second line", "third line"}

	assert.Nil(t, AssertLineRegex(lines, 0, "^first"))
	assert.Nil(t, AssertLineRegex(lines, 1, "second"))
	assert.NotNil(t, AssertLineRegex(lines, 0, "^second"))
	assert.NotNil(t, AssertLineRegex(lines, 5, "anything"))
	assert.NotNil(t, AssertLineRegex(lines, -1, "anything"))
	assert.NotNil(t, AssertLineRegex(lines, 0, "[invalid"))
}

func TestRefuteLineRegex(t *testing.T) {
	lines := []string{"first line", "second line", "third line"}

	// Line exists and regex does not match: pass
	assert.Nil(t, RefuteLineRegex(lines, 0, "^second"))
	// Line exists and regex matches (unanchored search): fail
	assert.NotNil(t, RefuteLineRegex(lines, 0, "^first"))
	assert.NotNil(t, RefuteLineRegex(lines, 1, "second"))
	// Nonexistent line: pass (nothing there to match)
	assert.Nil(t, RefuteLineRegex(lines, 5, "anything"))
	assert.Nil(t, RefuteLineRegex(lines, -1, "anything"))
	// Invalid regex always fails, even on a nonexistent line
	assert.NotNil(t, RefuteLineRegex(lines, 0, "[invalid"))
	assert.NotNil(t, RefuteLineRegex(lines, 5, "[invalid"))
}

func TestAssertExitCode(t *testing.T) {
	assert.Nil(t, AssertExitCode(0, schema.ExitCode{Value: 0}))
	assert.NotNil(t, AssertExitCode(1, schema.ExitCode{Value: 0}))
	assert.Nil(t, AssertExitCode(0, schema.ExitCode{Variable: "EXIT_SUCCESS"}))
	assert.Nil(t, AssertExitCode(1, schema.ExitCode{Variable: "EXIT_FAILURE"}))
	assert.NotNil(t, AssertExitCode(0, schema.ExitCode{Variable: "EXIT_FAILURE"}))
	assert.NotNil(t, AssertExitCode(0, schema.ExitCode{Variable: "UNKNOWN_VAR"}))
}

func TestAssertFileExists(t *testing.T) {
	tmp := t.TempDir()
	existing := filepath.Join(tmp, "exists.txt")
	require.Nil(t, os.WriteFile(existing, []byte("hi"), 0644))

	assert.Nil(t, AssertFileExists(existing))
	assert.NotNil(t, AssertFileExists(filepath.Join(tmp, "nope.txt")))
}

func TestRefuteFileExists(t *testing.T) {
	tmp := t.TempDir()
	existing := filepath.Join(tmp, "exists.txt")
	require.Nil(t, os.WriteFile(existing, []byte("hi"), 0644))

	assert.Nil(t, RefuteFileExists(filepath.Join(tmp, "nope.txt")))
	assert.NotNil(t, RefuteFileExists(existing))
}

// symlinkLoopPath creates a path whose os.Stat fails with ELOOP (an error
// that is NOT ErrNotExist). Skips the test if symlinks cannot be created.
func symlinkLoopPath(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	if err := os.Symlink("loop", filepath.Join(tmp, "loop")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	return filepath.Join(tmp, "loop", "x")
}

func TestAssertFileExistsStatError(t *testing.T) {
	// A stat error other than not-exist must fail, not silently pass.
	badPath := symlinkLoopPath(t)
	err := AssertFileExists(badPath)
	require.NotNil(t, err, "an un-statable path must not pass an existence assertion")
	assert.Contains(t, err.Error(), "could not stat")
}

func TestRefuteFileExistsStatError(t *testing.T) {
	// A stat error other than not-exist means absence cannot be verified;
	// that is a failure, not a pass.
	badPath := symlinkLoopPath(t)
	err := RefuteFileExists(badPath)
	require.NotNil(t, err, "an un-statable path must not pass a non-existence assertion")
	assert.Contains(t, err.Error(), "could not stat")
}

func TestAssertFileContains(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "test.txt")
	require.Nil(t, os.WriteFile(f, []byte("hello world\nfoo bar"), 0644))

	errs := AssertFileContains(f, []string{"hello", "foo"})
	assert.Empty(t, errs)

	errs = AssertFileContains(f, []string{"hello", "missing"})
	assert.Len(t, errs, 1)

	errs = AssertFileContains(filepath.Join(tmp, "nope.txt"), []string{"hello"})
	assert.Len(t, errs, 1)

	errs = AssertFileContains(f, []string{"[invalid"})
	assert.Len(t, errs, 1)
}

func TestRefuteFileContains(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "test.txt")
	require.Nil(t, os.WriteFile(f, []byte("hello world"), 0644))

	errs := RefuteFileContains(f, []string{"missing"})
	assert.Empty(t, errs)

	errs = RefuteFileContains(f, []string{"hello"})
	assert.Len(t, errs, 1)

	// Non-existent file is fine for refute
	errs = RefuteFileContains(filepath.Join(tmp, "nope.txt"), []string{"hello"})
	assert.Empty(t, errs)

	errs = RefuteFileContains(f, []string{"[invalid"})
	assert.Len(t, errs, 1)

	// A path that exists but cannot be read as a file (here: a directory) is
	// a real failure, not a vacuous pass
	errs = RefuteFileContains(tmp, []string{"hello"})
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "could not read file")
}
