package cmd

import (
	"bytes"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// executeRoot runs the real command tree with the given argv and returns
// everything it wrote. It holds the command tree and restores its writers and
// args, so tests that share it stay independent.
func executeRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	holdRootCmd(t)

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	err := rootCmd.Execute()
	return buf.String(), err
}

func TestHelpWithoutArgsPrintsRootHelp(t *testing.T) {
	out, err := executeRoot(t, "help")
	require.NoError(t, err)

	assert.Contains(t, out, "DATS runs tests defined in declarative YAML files")
	// The root help is where a reader finds out the documentation is in here.
	assert.Contains(t, out, "dats docs")
}

func TestHelpResolvesCommandsBeforeTopics(t *testing.T) {
	out, err := executeRoot(t, "help", "syntax")
	require.NoError(t, err)

	assert.Contains(t, out, "Parse and validate .dats files")
	assert.Contains(t, out, "Usage:")
}

func TestHelpFallsBackToDocTopics(t *testing.T) {
	out, err := executeRoot(t, "help", "format")
	require.NoError(t, err)

	assert.Contains(t, out, "# DATS File Format Reference")
	assert.Contains(t, out, "## Matrix (Parameterized) Tests")
}

func TestHelpRejectsUnknownTopic(t *testing.T) {
	_, err := executeRoot(t, "help", "nonsense")

	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown help topic "nonsense"`)
	assert.Contains(t, err.Error(), "dats docs")
}

func TestFlagUsageStringsHaveNoBackticks(t *testing.T) {
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		assert.NotContains(t, f.Usage, "`",
			"--%s: pflag reads a backticked word as the flag's value placeholder", f.Name)
	})
}

func TestRootHelpFlagMentionsTheDocs(t *testing.T) {
	out, err := executeRoot(t, "--help")
	require.NoError(t, err)

	assert.Contains(t, out, "dats docs format")
	assert.Contains(t, out, "docs")
}
