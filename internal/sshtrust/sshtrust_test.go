package sshtrust

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolate points the store at a throwaway config directory, so a test never
// reads or writes the developer's real approvals.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func TestMissingStoreIsEmptyNotAnError(t *testing.T) {
	isolate(t)
	store, err := Load()
	require.NoError(t, err)
	assert.Empty(t, store.List())
}

func TestApproveThenApprovedSurvivesAReload(t *testing.T) {
	dir := isolate(t)
	suite := filepath.Join(t.TempDir(), "suite.dats")
	require.NoError(t, os.WriteFile(suite, []byte("tests:\n"), 0o644))

	store, err := Load()
	require.NoError(t, err)
	require.NoError(t, store.Approve(suite, "build@box"))

	reloaded, err := Load()
	require.NoError(t, err)

	ok, err := reloaded.Approved(suite, "build@box")
	require.NoError(t, err)
	assert.True(t, ok)

	// An approval is per pair: another host on the same file is not covered.
	ok, err = reloaded.Approved(suite, "other@box")
	require.NoError(t, err)
	assert.False(t, ok, "approving one host must not approve every host")

	_, err = os.Stat(filepath.Join(dir, "dats", "ssh-trust.json"))
	assert.NoError(t, err)
}

func TestApproveIsIdempotent(t *testing.T) {
	isolate(t)
	store, err := Load()
	require.NoError(t, err)
	require.NoError(t, store.Approve("a.dats", "box"))
	require.NoError(t, store.Approve("a.dats", "box"))
	assert.Len(t, store.List(), 1)
}

func TestRevoke(t *testing.T) {
	isolate(t)
	store, err := Load()
	require.NoError(t, err)
	require.NoError(t, store.Approve("a.dats", "box"))

	removed, err := store.Revoke("a.dats", "box")
	require.NoError(t, err)
	assert.True(t, removed)

	ok, err := store.Approved("a.dats", "box")
	require.NoError(t, err)
	assert.False(t, ok)

	removed, err = store.Revoke("a.dats", "box")
	require.NoError(t, err)
	assert.False(t, removed, "revoking twice reports that there was nothing to remove")
}

// TestApprovalKeyIsTheResolvedPath pins that two spellings of one file share
// an approval: otherwise the same suite would be asked about again from a
// different working directory.
func TestApprovalKeyIsTheResolvedPath(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	suite := filepath.Join(dir, "suite.dats")
	require.NoError(t, os.WriteFile(suite, []byte("tests:\n"), 0o644))

	store, err := Load()
	require.NoError(t, err)
	require.NoError(t, store.Approve(suite, "box"))

	ok, err := store.Approved(filepath.Join(dir, ".", "suite.dats"), "box")
	require.NoError(t, err)
	assert.True(t, ok)
}

// TestCorruptStoreIsAnErrorNotAnEmptyList pins that a damaged file never
// reads as "nothing was approved": that would silently drop the operator's
// own decisions.
func TestCorruptStoreIsAnErrorNotAnEmptyList(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "dats", "ssh-trust.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not valid JSON")
}

func TestListIsSorted(t *testing.T) {
	isolate(t)
	store, err := Load()
	require.NoError(t, err)
	require.NoError(t, store.Approve("b.dats", "box"))
	require.NoError(t, store.Approve("a.dats", "zeta"))
	require.NoError(t, store.Approve("a.dats", "alpha"))

	entries := store.List()
	require.Len(t, entries, 3)
	assert.Equal(t, "alpha", entries[0].Target)
	assert.Equal(t, "zeta", entries[1].Target)
}
