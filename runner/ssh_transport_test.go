package runner

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTarRoundTripPreservesTreeAndModes proves the fixture transport keeps
// what dats promises about fixtures: nested names survive, and a copied
// script keeps its executable bit. Needs no ssh -- the archive is the whole
// contract, and the far side only untars it.
func TestTarRoundTripPreservesTreeAndModes(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(src, "inputs", "sub"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(src, outputsDirName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "inputs", "plain.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "inputs", "sub", "nested.txt"), []byte("deep"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "inputs", "run.sh"), []byte("#!/bin/sh\n"), 0o755))

	var buf bytes.Buffer
	require.NoError(t, writeTarDir(src, &buf))

	dest := t.TempDir()
	require.NoError(t, extractTar(&buf, dest))

	content, err := os.ReadFile(filepath.Join(dest, "inputs", "plain.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello", string(content))

	nested, err := os.ReadFile(filepath.Join(dest, "inputs", "sub", "nested.txt"))
	require.NoError(t, err)
	assert.Equal(t, "deep", string(nested))

	info, err := os.Stat(filepath.Join(dest, "inputs", "run.sh"))
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&0o100, "a copied script must keep its executable bit")

	// An empty outputs directory must travel, or {outputs.X} writes nowhere.
	outInfo, err := os.Stat(filepath.Join(dest, outputsDirName))
	require.NoError(t, err)
	assert.True(t, outInfo.IsDir())
}

func TestTarRoundTripOnEmptyDirectory(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeTarDir(t.TempDir(), &buf))
	dest := t.TempDir()
	require.NoError(t, extractTar(&buf, dest))
	entries, err := os.ReadDir(dest)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// TestExtractTarRefusesAnEscapingMember pins that the archive is not trusted
// to stay inside the destination: it arrives from another machine.
func TestExtractTarRefusesAnEscapingMember(t *testing.T) {
	for _, name := range []string{"../ev.txt", "/etc/ev.txt", "a/../../ev.txt"} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)
			body := []byte("pwned")
			require.NoError(t, tw.WriteHeader(&tar.Header{
				Typeflag: tar.TypeReg,
				Name:     name,
				Mode:     0o644,
				Size:     int64(len(body)),
			}))
			_, err := tw.Write(body)
			require.NoError(t, err)
			require.NoError(t, tw.Close())

			err = extractTar(&buf, t.TempDir())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "escapes the destination")
		})
	}
}

func TestSSHConfigCloseIsSafeWithoutConnecting(t *testing.T) {
	assert.NotPanics(t, func() { NewSSHConfig("build@box").Close() })
}

func TestKillRemoteIgnoresAnEmptyIdentity(t *testing.T) {
	c := NewSSHConfig("build@box")
	// Nothing recorded to kill, so this must not spawn ssh at all.
	assert.NotPanics(t, func() { c.KillRemote("", "") })
	assert.NotPanics(t, func() { c.RemoveBase("") })
}

func TestRemoteJoinBuildsPOSIXPaths(t *testing.T) {
	assert.Equal(t, "/tmp/dats-x/shared", remoteJoin("/tmp/dats-x", sharedDirName))
	assert.Equal(t, "/tmp/dats-x/test-2/outputs", remoteJoin("/tmp/dats-x", "test-2", outputsDirName))
}
