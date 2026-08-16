package cmd

// Tests for the watch command's engine: the pure event-relevance filter, the
// watch-directory computation, the debouncing loop core (driven by a fake
// event channel), the header rendering, and one real-fsnotify integration
// probe. The full re-run behavior against real test files is exercised at
// the binary level; these tests pin the parts unit tests can reach.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/go-containers/set"
)

func TestRelevantChange(t *testing.T) {
	tmp := t.TempDir()
	// demo.dats stands in for a FILE argument: its parent dir is watched but
	// no tree scope covers it. tree/ is a DIRECTORY argument.
	resolved := filepath.Join(tmp, "suite", "demo.dats")
	snapDir := filepath.Join(tmp, "suite", "demo.snapshots")
	dirArg := filepath.Join(tmp, "tree")
	report := filepath.Join(tmp, "tree", "report.json")

	scope := &watchScope{
		files:    set.Of(resolved),
		snapDirs: []string{snapDir},
		dirTrees: []string{dirArg},
		reports:  set.Of(report),
	}
	scopeUpdate := &watchScope{
		files:    scope.files,
		snapDirs: scope.snapDirs,
		dirTrees: scope.dirTrees,
		reports:  scope.reports,
		update:   true,
	}

	cases := []struct {
		name  string
		scope *watchScope
		op    fsnotify.Op
		path  string
		isDir bool
		want  bool
	}{
		{"write to a resolved .dats file", scope, fsnotify.Write, resolved, false, true},
		{"out-of-scope .dats in a merely-parent-watched dir", scope, fsnotify.Write, filepath.Join(tmp, "suite", "other.dats"), false, false},
		{"new .dats created under a dir arg", scope, fsnotify.Create, filepath.Join(dirArg, "sub", "new.dats"), false, true},
		{"removed .dats under a dir arg", scope, fsnotify.Remove, filepath.Join(dirArg, "gone.dats"), false, true},
		{"golden under a resolved snapshot dir", scope, fsnotify.Write, filepath.Join(snapDir, "001-x.stdout.golden"), false, true},
		{"golden under a resolved snapshot dir with --update", scopeUpdate, fsnotify.Write, filepath.Join(snapDir, "001-x.stdout.golden"), false, false},
		{"golden outside any snapshot dir", scope, fsnotify.Write, filepath.Join(dirArg, "stray.golden"), false, false},
		{"report file write", scope, fsnotify.Write, report, false, false},
		{"chmod only", scope, fsnotify.Chmod, resolved, false, false},
		{"directory created under a watched tree", scope, fsnotify.Create, filepath.Join(dirArg, "newdir"), true, true},
		{"hidden directory created under a watched tree", scope, fsnotify.Create, filepath.Join(dirArg, ".hidden"), true, false},
		{"directory created outside the trees", scope, fsnotify.Create, filepath.Join(tmp, "suite", "newdir"), true, false},
		{"directory removed under a watched tree", scope, fsnotify.Remove, filepath.Join(dirArg, "olddir"), true, false},
		{"hidden .dats under a dir arg", scope, fsnotify.Create, filepath.Join(dirArg, ".hidden.dats"), false, false},
		{"unrelated file under a dir arg", scope, fsnotify.Write, filepath.Join(dirArg, "notes.txt"), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.scope.relevantChange(tc.op, tc.path, tc.isDir))
		})
	}
}

func TestComputeWatchDirs(t *testing.T) {
	tmp := t.TempDir()
	// A resolved file with an existing snapshot dir, a second resolved file
	// without one, and a directory-argument tree with nested and hidden
	// subdirectories.
	fileDir := filepath.Join(tmp, "suite")
	resolved := filepath.Join(fileDir, "demo.dats")
	snapDir := filepath.Join(fileDir, "demo.snapshots")
	otherDir := filepath.Join(tmp, "other")
	second := filepath.Join(otherDir, "more.dats")
	tree := filepath.Join(tmp, "tree")
	sub := filepath.Join(tree, "sub")
	nested := filepath.Join(sub, "deeper")
	hidden := filepath.Join(tree, ".hidden")
	for _, dir := range []string{fileDir, snapDir, otherDir, sub, nested, hidden} {
		require.Nil(t, os.MkdirAll(dir, 0o755))
	}
	require.Nil(t, os.WriteFile(resolved, []byte("tests:\n\t- cmd: true\n"), 0o644))
	require.Nil(t, os.WriteFile(second, []byte("tests:\n\t- cmd: true\n"), 0o644))

	// The duplicate resolved entry must collapse; the missing more.snapshots
	// dir must not be registered; the hidden subdir must be skipped.
	dirs := computeWatchDirs([]string{resolved, second, resolved}, []string{tree})
	assert.ElementsMatch(t, []string{fileDir, snapDir, otherDir, tree, sub, nested}, dirs)
}

func TestWatchHeader(t *testing.T) {
	at := time.Date(2026, 7, 19, 9, 5, 7, 0, time.UTC)
	assert.Equal(t, "# watch: run 1 at 09:05:07 (initial)", watchHeader(1, at, nil))
	assert.Equal(t, "# watch: run 2 at 09:05:07 (changed: /a/x.dats)",
		watchHeader(2, at, []string{"/a/x.dats"}))
	assert.Equal(t, "# watch: run 3 at 09:05:07 (changed: /a.dats, /b.golden, /c.dats, +2 more)",
		watchHeader(3, at, []string{"/a.dats", "/b.golden", "/c.dats", "/d.dats", "/e.dats"}))
}

// startWatchLoop runs watchLoop in a goroutine with a recording cycle
// function, returning the channel of recorded batches and a done channel
// closed when the loop returns.
func startWatchLoop(ctx context.Context, events chan watchEvent, debounce time.Duration, onCycle func(changed []string)) (<-chan []string, <-chan struct{}) {
	cycles := make(chan []string, 16)
	done := make(chan struct{})
	go func() {
		watchLoop(ctx, events, func(_ context.Context, changed []string) {
			if onCycle != nil {
				onCycle(changed)
			}
			cycles <- changed
		}, debounce)
		close(done)
	}()
	return cycles, done
}

func waitCycle(t *testing.T, cycles <-chan []string, what string) []string {
	t.Helper()
	select {
	case changed := <-cycles:
		return changed
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not fire", what)
		return nil
	}
}

func TestWatchLoopInitialCycleAndCancel(t *testing.T) {
	events := make(chan watchEvent)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cycles, done := startWatchLoop(ctx, events, 5*time.Millisecond, nil)

	assert.Empty(t, waitCycle(t, cycles, "initial cycle"), "the initial cycle carries no changed paths")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchLoop did not return promptly after cancel")
	}
}

func TestWatchLoopBatchesBurstIntoOneCycle(t *testing.T) {
	events := make(chan watchEvent, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cycles, _ := startWatchLoop(ctx, events, 20*time.Millisecond, nil)
	waitCycle(t, cycles, "initial cycle")

	// A burst of events, duplicate included: exactly one re-cycle, carrying
	// the deduplicated batch.
	events <- watchEvent{path: "/w/a.dats"}
	events <- watchEvent{path: "/w/b.dats"}
	events <- watchEvent{path: "/w/a.dats"}

	assert.Equal(t, []string{"/w/a.dats", "/w/b.dats"}, waitCycle(t, cycles, "re-cycle"))

	select {
	case extra := <-cycles:
		t.Fatalf("burst must coalesce into exactly one re-cycle, got another with %v", extra)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestWatchLoopEventsDuringCycleCoalesce(t *testing.T) {
	events := make(chan watchEvent, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Hold the initial cycle open while events arrive: they must queue and
	// then coalesce into exactly one follow-up cycle. onCycle runs on the
	// watchLoop goroutine only, so the plain bool is race-free.
	release := make(chan struct{})
	first := true
	cycles, _ := startWatchLoop(ctx, events, 20*time.Millisecond, func([]string) {
		if first {
			first = false
			<-release
		}
	})

	events <- watchEvent{path: "/w/a.dats"}
	events <- watchEvent{path: "/w/b.dats"}
	close(release)

	waitCycle(t, cycles, "initial cycle")
	assert.Equal(t, []string{"/w/a.dats", "/w/b.dats"}, waitCycle(t, cycles, "follow-up cycle"))

	select {
	case extra := <-cycles:
		t.Fatalf("mid-cycle events must coalesce into exactly one follow-up, got another with %v", extra)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestWatchRealFsnotifyIntegration(t *testing.T) {
	// One probe against the real fsnotify plumbing: a watchSession watching
	// an actual .dats file must re-cycle after the file is written. The
	// write is retried until the cycle fires so a slow watcher registration
	// on the CI filesystem cannot flake the test; the overall deadline is
	// generous for the same reason.
	dir := t.TempDir()
	file := filepath.Join(dir, "probe.dats")
	require.Nil(t, os.WriteFile(file, []byte("tests:\n\t- cmd: true\n"), 0o644))

	w := &watchSession{
		args:   []string{file},
		files:  []string{file},
		events: make(chan watchEvent, 64),
	}
	require.Nil(t, w.rebuildWatches())
	defer w.closeWatcher()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cycles, done := startWatchLoop(ctx, w.events, 20*time.Millisecond, nil)
	waitCycle(t, cycles, "initial cycle")

	deadline := time.Now().Add(15 * time.Second)
	var changed []string
	for changed == nil && time.Now().Before(deadline) {
		require.Nil(t, os.WriteFile(file, []byte("tests:\n\t- cmd: echo changed\n"), 0o644))
		select {
		case changed = <-cycles:
		case <-time.After(500 * time.Millisecond):
		}
	}
	require.NotNil(t, changed, "fsnotify write event never produced a re-cycle")
	require.NotEmpty(t, changed)
	assert.True(t, strings.HasSuffix(changed[0], "probe.dats"), "changed batch should name the written file, got %v", changed)

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watchLoop did not return promptly after cancel")
	}
}
