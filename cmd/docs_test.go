package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dats "github.com/wow-look-at-my/dats"
)

func TestDocsWithoutArgsListsEveryTopic(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, runDocs(&buf, nil))

	out := buf.String()
	for _, page := range dats.Docs() {
		assert.Contains(t, out, page.Name)
		assert.Contains(t, out, page.Summary)
	}
	assert.Contains(t, out, "dats docs all")
}

func TestDocsPrintsThePageBare(t *testing.T) {
	page, ok := dats.LookupDoc("format")
	require.True(t, ok)
	want, err := page.Text()
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, runDocs(&buf, []string{"file-format.md"}))

	// A single topic prints the page and nothing else, so `dats docs format`
	// can be redirected straight into a file.
	assert.Equal(t, want, buf.String())
}

func TestDocsPrintsSeveralPagesWithBanners(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, runDocs(&buf, []string{"cli", "reports"}))

	out := buf.String()
	assert.Contains(t, out, "========== docs/cli.md ==========")
	assert.Contains(t, out, "========== docs/reports.md ==========")
	assert.Less(t, strings.Index(out, "docs/cli.md"), strings.Index(out, "docs/reports.md"))
}

func TestDocsAllPrintsEveryPage(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, runDocs(&buf, []string{"all"}))

	out := buf.String()
	for _, page := range dats.Docs() {
		assert.Contains(t, out, "========== "+page.File+" ==========")

		text, err := page.Text()
		require.NoError(t, err)
		assert.Contains(t, out, strings.TrimSpace(text))
	}
}

func TestDocsRejectsUnknownTopic(t *testing.T) {
	var buf bytes.Buffer
	err := runDocs(&buf, []string{"format", "nonsense"})

	// The whole call fails: printing the topics that did resolve and silently
	// dropping the rest would answer a question nobody asked.
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown docs topic "nonsense"`)
	assert.Contains(t, err.Error(), "format")
	assert.Empty(t, buf.String())
}

func TestDocsCommandIsWiredUp(t *testing.T) {
	out, err := executeRoot(t, "docs", "overview")
	require.NoError(t, err)
	assert.Contains(t, out, "# DATS Documentation")
}
