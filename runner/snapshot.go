package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wow-look-at-my/dats/schema"
	"github.com/wow-look-at-my/go-containers/set"
)

func SnapshotDir(datsPath string) string {
	return strings.TrimSuffix(datsPath, filepath.Ext(datsPath)) + ".snapshots"
}

func GoldenFileName(index int, instanceName, stream string) string {
	return fmt.Sprintf("%03d-%s.%s.golden", index+1, slugifySnapshotName(instanceName), stream)
}

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

func NormalizeSnapshotText(s string, ctx *TestContext) string {
	// A remote command prints REMOTE paths, so those roots normalize to the same tokens.
	if ctx.RemoteBase != "" {
		s = strings.ReplaceAll(s, ctx.commandPath(testDirPath(ctx.BaseDir, ctx.TestIndex)), "{testdir}")
		s = strings.ReplaceAll(s, ctx.commandPath(ctx.SharedDir), "{shareddir}")
		s = strings.ReplaceAll(s, ctx.RemoteBase, "{tmproot}")
	}
	s = strings.ReplaceAll(s, testDirPath(ctx.BaseDir, ctx.TestIndex), "{testdir}")
	s = strings.ReplaceAll(s, ctx.SharedDir, "{shareddir}")
	return strings.ReplaceAll(s, ctx.BaseDir, "{tmproot}")
}

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

func (r *Runner) applySnapshot(result *TestResult, inst *schema.TestInstance, datsPath, baseDir string, index int) {
	check := inst.Test.Outputs.Snapshot
	if !check.Enabled || (!check.Stdout && !check.Stderr) {
		return
	}
	if !result.ranToCompletion {
		return
	}
	if r.Update && len(result.Failures) > 0 {
		return
	}

	ctx := &TestContext{
		BaseDir:    baseDir,
		TestIndex:  index,
		SharedDir:  filepath.Join(baseDir, sharedDirName),
		RemoteBase: r.remoteBase,
	}

	for _, stream := range snapshotStreams(check, result) {
		name, content := stream[0], stream[1]
		fileName := GoldenFileName(index, result.Name, name)
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
	// Unreachable for differing inputs: equal line counts with equal lines means equal strings.
	return "outputs differ"
}

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
		// Ignore errors: a concurrent write or an unremovable directory just leaves the empty directory behind.
		_ = os.Remove(dir)
	}
}
