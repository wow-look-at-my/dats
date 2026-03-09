package runner

import (
	"testing"

	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

func TestExecute(t *testing.T) {
	result, err := Execute("echo hello", "", nil)
	require.Nil(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "hello\n", result.Stdout)
	assert.Equal(t, "", result.Stderr)
	assert.Equal(t, []string{"hello"}, result.StdoutLines)
}

func TestExecuteWithStdin(t *testing.T) {
	result, err := Execute("cat", "input text", nil)
	require.Nil(t, err)
	assert.Equal(t, "input text", result.Stdout)
}

func TestExecuteNonZeroExit(t *testing.T) {
	result, err := Execute("exit 42", "", nil)
	require.Nil(t, err)
	assert.Equal(t, 42, result.ExitCode)
}

func TestExecuteStderr(t *testing.T) {
	result, err := Execute("echo err >&2", "", nil)
	require.Nil(t, err)
	assert.Equal(t, "err\n", result.Stderr)
	assert.Equal(t, []string{"err"}, result.StderrLines)
}

func TestExecuteWithEnv(t *testing.T) {
	result, err := Execute("echo $TEST_VAR", "", []string{"TEST_VAR=hello123"})
	require.Nil(t, err)
	assert.Equal(t, "hello123\n", result.Stdout)
}

func TestSplitLines(t *testing.T) {
	assert.Equal(t, []string{}, splitLines(""))
	assert.Equal(t, []string{"a"}, splitLines("a\n"))
	assert.Equal(t, []string{"a", "b"}, splitLines("a\nb\n"))
	assert.Equal(t, []string{"a", "b"}, splitLines("a\nb"))
}
