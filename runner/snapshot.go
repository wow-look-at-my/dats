package runner

// Snapshot (golden-file) assertions: a test's outputs.snapshot key asserts
// that captured stdout and/or stderr byte-match a golden file stored in a
// .snapshots directory next to the .dats file. Golden text is normalized
// (framework temp paths become stable tokens) so goldens are reproducible
// across runs. The --update flag (Runner.Update) rewrites goldens from
// actual output instead of failing, and prunes stale golden files whose
// instance or stream no longer exists.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wow-look-at-my/dats/schema"
	"github.com/wow-look-at-my/go-containers/set"
)

// SnapshotDir returns the directory golden files for datsPath live in: the
// .dats file's path with its extension replaced by ".snapshots", e.g.
// examples/demo.dats -> examples/demo.snapshots.
func SnapshotDir(datsPath string) string {
	return strings.TrimSuffix(datsPath, filepath.Ext(datsPath)) + ".snapshots"
}

// GoldenFileName returns the golden file name for one instance's stream:
// "NNN-<slug>.<stream>.golden", where NNN is the canonical 1-based instance
// number (index is the 0-based expanded-instance index, so the number
// matches the CLI's "ok N -" numbering and the JSON report's index) and
// <slug> derives from the instance's display name, matrix label included.
// stream is "stdout" or "stderr".
func GoldenFileName(index int, instanceName, stream string) string {
	return fmt.Sprintf("%03d-%s.%s.golden", index+1, slugifySnapshotName(instanceName), stream)
}

// slugifySnapshotName maps an instance display name onto the golden-file
// slug: lowercased, with every rune outside [a-z0-9] replaced by "-",
// consecutive dashes collapsed, leading/trailing dashes trimmed, and the
// result truncated to 60 bytes (the charset is ASCII after mapping, so byte
// truncation is rune-safe). An empty result falls back to "test". The slug
// charset makes path traversal impossible by construction.
func slugifySnapshotName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	lastDash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 60 {
		slug = strings.Trim(slug[:60], "-")
	}
	if slug == "" {
		return "test"
	}
	return slug
}

// NormalizeSnapshotText replaces the framework-injected temp paths in s with
// stable tokens so golden files are reproducible across runs: the instance's
// test directory becomes {testdir}, the file's shared directory becomes
// {shareddir}, and the per-run temp root becomes {tmproot}. Replacement is
// longest-path-first so the more specific directories never surface as
// "{tmproot}/test-0". Everything else is left byte-exact; trailing newlines
// are never munged.
func NormalizeSnapshotText(s string, ctx *TestContext) string {
	// A remote command prints REMOTE paths, so those roots normalize to the
	// same tokens. Without this a golden bakes in one run's remote temp dir,
	// and every existing golden breaks the moment its suite runs over ssh.
	if ctx.RemoteBase != "" {
		s = strings.ReplaceAll(s, ctx.commandPath(testDirPath(ctx.BaseDir, ctx.TestIndex)), "{testdir}")
		s = strings.ReplaceAll(s, ctx.commandPath(ctx.SharedDir), "{shareddir}")
		s = strings.ReplaceAll(s, ctx.RemoteBase, "{tmproot}")
	}
	s = strings.ReplaceAll(s, testDirPath(ctx.BaseDir, ctx.TestIndex), "{testdir}")
	s = strings.ReplaceAll(s, ctx.SharedDir, "{shareddir}")
	return strings.ReplaceAll(s, ctx.BaseDir, "{tmproot}")
}

// snapshotStreams returns the enabled (stream name, captured content) pairs
// of result in canonical order: stdout, then stderr.
func snapshotStreams(check schema.SnapshotCheck, result *TestResult) [][2]string {
	var streams [][2]string
	if check.Stdout {
		streams = append(streams, [2]string{"stdout", result.Stdout})
	}
	if check.Stderr {
		streams = append(streams, [2]string{"stderr", result.Stderr})
	}
	return streams
}

// applySnapshot applies the instance's snapshot assertions to result --
// comparing each enabled stream against its golden file, or (with
// Runner.Update) rewriting the golden from actual output -- and recomputes
// result.Passed. Callers must set result.Name to the instance display name
// first: the golden file name derives from it. No-op when the test declares
// no snapshot or when the command did not run to completion (fixture-setup
// failure, spawn failure, or timeout -- consistent with the timeout rule
// that every other assertion is skipped). Called by both RunFile and
// runFileParallel; golden files are per-instance unique, so concurrent calls
// for distinct instances are safe.
func (r *Runner) applySnapshot(result *TestResult, inst *schema.TestInstance, datsPath, baseDir string, index int) {
	check := inst.Test.Outputs.Snapshot
	if !check.Enabled || (!check.Stdout && !check.Stderr) {
		return
	}
	if !result.ranToCompletion {
		return
	}
	// Goldens never update from a failing run: an instance that already has
	// any other failure neither writes nor compares its goldens.
	if r.Update && len(result.Failures) > 0 {
		return
	}

	// The test and shared directories are derived from baseDir+index exactly
	// as SetupFixtures derives them (testDirPath is shared); fixture setup is
	// not re-run.
	ctx := &TestContext{
		BaseDir:   baseDir,
		TestIndex: index,
		SharedDir: filepath.Join(baseDir, sharedDirName),
	}

	for _, stream := range snapshotStreams(check, result) {
		name, content := stream[0], stream[1]
		fileName := GoldenFileName(index, result.Name, name)
		// The slug charset makes traversal impossible, but assert locality
		// anyway -- the same safety net as fixture names -- and never touch
		// the disk on a violation.
		if !filepath.IsLocal(fileName) {
			result.Failures = append(result.Failures, fmt.Sprintf(
				"snapshot: %s: golden file name %q must be a relative path that stays inside the snapshot directory", name, fileName))
			continue
		}
		goldenPath := filepath.Join(SnapshotDir(datsPath), fileName)
		actual := NormalizeSnapshotText(content, ctx)

		if r.Update {
			r.updateGolden(result, name, datsPath, goldenPath, actual)
			continue
		}

		golden, err := os.ReadFile(goldenPath)
		if os.IsNotExist(err) {
			result.Failures = append(result.Failures, fmt.Sprintf(
				"snapshot: %s: golden file %s does not exist (run with --update to create it)", name, goldenPath))
			continue
		}
		if err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf(
				"snapshot: %s: reading golden file %s: %v", name, goldenPath, err))
			continue
		}
		if string(golden) != actual {
			result.Failures = append(result.Failures, fmt.Sprintf(
				"snapshot: %s: output does not match golden file %s (%s)", name, goldenPath, firstDifference(string(golden), actual)))
		}
	}

	result.Passed = len(result.Failures) == 0
}

// updateGolden writes actual as the golden file when it is missing or
// differs, recording the write in result.UpdatedGoldens. An up-to-date
// golden is left completely untouched (no write, no churn, not listed). A
// read error other than absence falls through to the write, whose error
// message surfaces the underlying problem (e.g. the golden path is a
// directory).
func (r *Runner) updateGolden(result *TestResult, stream, datsPath, goldenPath, actual string) {
	golden, err := os.ReadFile(goldenPath)
	if err == nil && string(golden) == actual {
		return
	}
	if err := os.MkdirAll(SnapshotDir(datsPath), 0o755); err != nil {
		result.Failures = append(result.Failures, fmt.Sprintf(
			"snapshot: %s: writing golden file %s: %v", stream, goldenPath, err))
		return
	}
	if err := os.WriteFile(goldenPath, []byte(actual), 0o644); err != nil {
		result.Failures = append(result.Failures, fmt.Sprintf(
			"snapshot: %s: writing golden file %s: %v", stream, goldenPath, err))
		return
	}
	result.UpdatedGoldens = append(result.UpdatedGoldens, goldenPath)
}

// firstDifference describes where golden (the expected text) and actual
// first diverge, computed on the raw newline-split lines (no trimming):
// either the first line index where the two differ, or the point where one
// side ran out of lines. The special case of texts differing only by one
// trailing newline is called out directly -- the split-based description of
// that mismatch ("expected end of output") would obscure it. Line numbers
// are 0-indexed, matching the existing line-check convention.
func firstDifference(golden, actual string) string {
	if actual == golden+"\n" || golden == actual+"\n" {
		return "output differs only by a trailing newline"
	}
	goldenLines := strings.Split(golden, "\n")
	actualLines := strings.Split(actual, "\n")
	for i := 0; i < len(goldenLines) || i < len(actualLines); i++ {
		switch {
		case i >= len(goldenLines):
			return fmt.Sprintf("line %d: expected end of output, got %q", i, actualLines[i])
		case i >= len(actualLines):
			return fmt.Sprintf("line %d: expected %q, got end of output", i, goldenLines[i])
		case goldenLines[i] != actualLines[i]:
			return fmt.Sprintf("line %d: expected %q, got %q", i, goldenLines[i], actualLines[i])
		}
	}
	// Unreachable for differing inputs: equal line counts with equal lines
	// means equal strings.
	return "outputs differ"
}

// pruneStaleGoldens removes golden files in datsPath's snapshot directory
// that no expanded instance's enabled streams name (update mode only;
// callers gate on Runner.Update and a clean file setup). The expected name
// set derives from the expansion regardless of pass/fail -- dats has no test
// filtering, so the expanded list is authoritative. Only direct *.golden
// files are considered (no recursion); other files are never touched. A
// removal failure prints a warning and continues -- it is not a test
// failure. The directory itself is removed when pruning leaves it empty, and
// is never created here. Successfully pruned paths land sorted in
// fileResult.PrunedGoldens.
func (r *Runner) pruneStaleGoldens(fileResult *FileResult, instances []schema.TestInstance, datsPath string) {
	expected := set.New[string]()
	for i := range instances {
		check := instances[i].Test.Outputs.Snapshot
		if !check.Enabled {
			continue
		}
		name := instanceName(&instances[i])
		if check.Stdout {
			expected.Add(GoldenFileName(i, name, "stdout"))
		}
		if check.Stderr {
			expected.Add(GoldenFileName(i, name, "stderr"))
		}
	}

	dir := SnapshotDir(datsPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Most commonly the directory does not exist (no goldens yet); any
		// other read error also means there is nothing to prune safely.
		return
	}
	remaining := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".golden") || expected.Contains(entry.Name()) {
			remaining++
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.Remove(path); err != nil {
			fmt.Fprintf(r.Formatter.Writer, "# warning: could not prune golden %s: %v\n", path, err)
			remaining++
			continue
		}
		fileResult.PrunedGoldens = append(fileResult.PrunedGoldens, path)
	}
	sort.Strings(fileResult.PrunedGoldens)
	if remaining == 0 {
		// Ignore errors: a concurrent write or an unremovable directory just
		// leaves the empty directory behind.
		_ = os.Remove(dir)
	}
}
