package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunSyntax_Valid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ok.dats")
	require.Nil(t, os.WriteFile(path, []byte("tests:\n  - cmd: echo hi\n"), 0644))

	var out, errw bytes.Buffer
	ok := runSyntax([]string{path}, &out, &errw)
	assert.True(t, ok)
	assert.Contains(t, out.String(), "ok   ")
	assert.Equal(t, "", errw.String())
}

func TestRunSyntax_Invalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.dats")
	// Missing the required 'cmd' field.
	require.Nil(t, os.WriteFile(path, []byte("tests:\n  - desc: no command\n"), 0644))

	var out, errw bytes.Buffer
	ok := runSyntax([]string{path}, &out, &errw)
	assert.False(t, ok)
	assert.Contains(t, errw.String(), "FAIL")
}

func TestSyntaxCmd_FailureReturnsSentinel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.dats")
	require.Nil(t, os.WriteFile(path, []byte("tests:\n  - desc: no command\n"), 0644))

	err := syntaxCmd.RunE(syntaxCmd, []string{path})
	assert.ErrorIs(t, err, errSyntaxFailed)
}

func TestSyntaxCmd_ValidReturnsNil(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ok.dats")
	require.Nil(t, os.WriteFile(path, []byte("tests:\n  - cmd: echo hi\n"), 0644))

	err := syntaxCmd.RunE(syntaxCmd, []string{path})
	assert.Nil(t, err)
}
