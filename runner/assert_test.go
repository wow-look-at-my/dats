package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wow-look-at-my/dats/schema"
	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
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
}
