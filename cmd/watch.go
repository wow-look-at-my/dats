package cmd

// The watch command: run the resolved tests once, then keep watching the
// resolved .dats files, their snapshot golden directories, and any directory
// arguments (where new .dats files change the scope) via fsnotify, and
// re-run on every relevant change (debounced). Every re-run executes the
// COMPLETE original argument scope: dats has a hard design ban on test
// filtering/selection, so there is no per-file or narrowed re-run and no
// flag to add one. Ctrl-C/SIGTERM exits 0; a run interrupted mid-flight is
// aborted via the context plumbed through the runner (in-flight process
// groups are killed, teardown still runs) and its outcome is discarded.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/dats/runner"
)

// watchDebounce is how long the watcher waits after the LAST relevant event
// before re-running, so editor save storms coalesce into a single run.
const watchDebounce = 250 * time.Millisecond

var watchCmd = &cobra.Command{
	Use:   "watch [files-or-dirs...]",
	Short: "Run tests, then re-run them whenever their files change",
	Long: `Run tests exactly like "dats test", then keep watching the resolved .dats
files, their snapshot golden directories, and any directory arguments (where
newly created .dats files join the scope) -- and re-run on every relevant
change, debounced. Each re-run executes the COMPLETE original argument
scope: dats has no test selection or filtering by design, so there is no
per-file or otherwise narrowed re-run.

Ctrl-C (or SIGTERM) exits with code 0. When a run is in flight, the
interrupt kills the running commands' whole process groups promptly; the
interrupted file's teardown still runs.`,
	RunE: runWatchCommand,
	Args: cobra.ArbitraryArgs,
}

// runWatchCommand implements dats watch: an initial run, then re-runs driven
// by filesystem events. Only startup argument resolution can fail the
// command (exactly like dats test); once watching, errors are reported and
// the watch continues -- the only exit is an interrupt, with code 0.
func runWatchCommand(cmd *cobra.Command, args []string) error {
	jobs, err := resolveJobs(cmd.Flags())
	if err != nil {
		return err
	}
	// A hard resolution error at startup (nonexistent argument, no .dats
	// files found, wrong extension) exits like dats test would. Every cycle
	// below re-resolves from scratch so scope changes are picked up.
	if _, err := resolveFiles(args); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	w := &watchSession{
		args:   args,
		jobs:   jobs,
		out:    os.Stdout,
		isTTY:  stdoutIsTTY(),
		events: make(chan watchEvent, 64),
	}
	defer w.closeWatcher()

	watchLoop(ctx, w.events, w.cycle, watchDebounce)

	// watchLoop only returns on cancellation: whether idle or mid-run (the
	// aborted run's outcome is discarded), watch says goodbye and exits 0.
	fmt.Fprint(w.out, "\n# watch: interrupted, exiting\n")
	return nil
}

// watchEvent is one relevant filesystem change, as delivered to watchLoop.
type watchEvent struct {
	path string
}

// watchLoop is the watch engine, kept free of fsnotify and I/O so tests can
// drive it with a fake event channel: it runs cycle once immediately (the
// initial run), then once per batch of events -- waiting until debounce has
// elapsed since the LAST event, so bursts coalesce into one re-run carrying
// all their (deduplicated) paths. Events that arrive while a cycle is
// running queue in the channel and coalesce into exactly one follow-up
// cycle. Returns promptly when ctx is canceled (or events is closed).
func watchLoop(ctx context.Context, events <-chan watchEvent, cycle func(ctx context.Context, changed []string), debounce time.Duration) {
	cycle(ctx, nil)
	for {
		var changed []string
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			changed = append(changed, ev.path)
		}
		timer := time.NewTimer(debounce)
	debouncing:
		for {
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case ev, ok := <-events:
				if !ok {
					timer.Stop()
					return
				}
				changed = append(changed, ev.path)
				if !timer.Stop() {
					<-timer.C
				}
				timer.Reset(debounce)
			case <-timer.C:
				break debouncing
			}
		}
		cycle(ctx, dedupePaths(changed))
	}
}

// watchSession owns one dats watch invocation's mutable state: the frozen
// argument scope, the last successfully resolved file list, and the current
// fsnotify watcher feeding the shared event channel.
type watchSession struct {
	args  []string
	jobs  int
	out   io.Writer
	isTTY bool

	run     int      // completed-run counter (1-based in headers)
	files   []string // last successfully resolved file list
	watcher *fsnotify.Watcher
	events  chan watchEvent
}

// cycle is one watch iteration: re-resolve the argument scope, rebuild the
// watch set, print the header, run the COMPLETE original argument scope, and
// print the waiting footer. changed is nil on the initial run.
func (w *watchSession) cycle(ctx context.Context, changed []string) {
	// Re-resolve so new, renamed, or removed .dats files under directory
	// arguments join or leave the scope. On error (e.g. the only resolved
	// file was deleted), report once, keep the previous resolved list and
	// watch set, and skip the run -- runTests would only repeat the same
	// resolution error, and the fix will trigger the next cycle.
	files, err := resolveFiles(w.args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		w.printFooter()
		return
	}
	w.files = files

	// Rebuild the watch set BEFORE running: changes made while the run is in
	// flight are then caught and coalesce into one pending re-run.
	if err := w.rebuildWatches(); err != nil {
		// Keep the previous watcher; watching degrades, the run still counts.
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}

	w.run++
	w.printHeader(changed)

	runErr := runTests(ctx, w.args, w.out, w.jobs)
	if ctx.Err() != nil {
		// Interrupted mid-run: the outcome is discarded and watch is about
		// to exit -- no footer, no error reporting for the aborted run.
		return
	}
	if runErr != nil && !errors.Is(runErr, errTestsFailed) {
		// A parse or infrastructure error must not kill the watch: report it
		// and keep waiting -- the user fixes the file and it re-runs.
		// (errTestsFailed needs nothing extra: the runner already reported.)
		fmt.Fprintf(os.Stderr, "Error: %v\n", runErr)
	}
	w.printFooter()
}

// printHeader clears the screen (TTY) or prints a separator line (non-TTY,
// except before the very first run), then the run header line.
func (w *watchSession) printHeader(changed []string) {
	if w.isTTY {
		fmt.Fprint(w.out, "\x1b[2J\x1b[H")
	} else if w.run > 1 {
		fmt.Fprintln(w.out, strings.Repeat("-", 40))
	}
	fmt.Fprintln(w.out, watchHeader(w.run, time.Now(), changed))
}

func (w *watchSession) printFooter() {
	fmt.Fprintln(w.out, "# watch: waiting for changes (Ctrl-C to exit)")
}

// watchHeader renders one run's header: "# watch: run N at HH:MM:SS" plus
// " (initial)" on the first run, or the deduplicated changed event paths
// (cleaned; up to 3, then ", +K more") on re-runs.
func watchHeader(run int, now time.Time, changed []string) string {
	header := fmt.Sprintf("# watch: run %d at %s", run, now.Format("15:04:05"))
	if run == 1 {
		return header + " (initial)"
	}
	if len(changed) == 0 {
		return header
	}
	shown := changed
	if len(shown) > 3 {
		shown = shown[:3]
	}
	cleaned := make([]string, len(shown))
	for i, path := range shown {
		cleaned[i] = filepath.Clean(path)
	}
	header += " (changed: " + strings.Join(cleaned, ", ")
	if extra := len(changed) - len(shown); extra > 0 {
		header += fmt.Sprintf(", +%d more", extra)
	}
	return header + ")"
}

// rebuildWatches replaces the fsnotify watcher with a fresh one covering the
// current scope -- recreating the watcher each cycle is the simplest way to
// track a changing directory set. The new watcher is registered before the
// old one closes, so the only unwatched moments fall inside the rebuild
// itself, immediately before a run that re-reads everything anyway.
// Directory registrations are best-effort: a directory vanishing mid-rebuild
// is skipped.
func (w *watchSession) rebuildWatches() error {
	scope := buildWatchScope(w.files, w.args)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	for _, dir := range computeWatchDirs(w.files, scope.dirTrees) {
		_ = watcher.Add(dir)
	}
	w.closeWatcher() // ends the previous pump goroutine
	w.watcher = watcher
	go pumpEvents(watcher, scope, w.events)
	return nil
}

func (w *watchSession) closeWatcher() {
	if w.watcher != nil {
		_ = w.watcher.Close()
	}
}

// pumpEvents forwards relevant filesystem events from one fsnotify watcher
// into the session's shared event channel until the watcher is closed. The
// send never blocks: when the channel is full, a re-run is already pending
// -- and every re-run covers the complete scope, so dropping the overflow
// loses nothing. Watcher errors are ignored (watching is best-effort).
func pumpEvents(watcher *fsnotify.Watcher, scope *watchScope, out chan<- watchEvent) {
	for {
		select {
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			isDir := false
			if ev.Op.Has(fsnotify.Create) {
				if info, err := os.Lstat(ev.Name); err == nil {
					isDir = info.IsDir()
				}
			}
			if scope.relevantChange(ev.Op, ev.Name, isDir) {
				select {
				case out <- watchEvent{path: ev.Name}:
				default:
				}
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// watchScope is one cycle's relevance configuration, all paths absolute. It
// is immutable once built and holds no handles, so relevantChange is a pure
// function of its inputs.
type watchScope struct {
	files    map[string]bool // resolved .dats files
	snapDirs []string        // the resolved files' snapshot golden directories
	dirTrees []string        // directory arguments (or the cwd in no-arg mode)
	reports  map[string]bool // --report-junit/--report-json output paths
	update   bool            // --update: the run itself rewrites goldens
}

// buildWatchScope captures the relevance configuration for one cycle from
// the resolved file list, the original arguments, and the report/update
// flags.
func buildWatchScope(files, args []string) *watchScope {
	scope := &watchScope{
		files:   make(map[string]bool, len(files)),
		reports: make(map[string]bool, 2),
		update:  updateGoldens,
	}
	for _, file := range files {
		abs := watchAbs(file)
		scope.files[abs] = true
		// All snapshot dirs count for relevance whether or not they exist
		// yet -- in tree mode a freshly blessed golden arrives via the tree
		// watch. (Existence gates only the watch-dir registration.)
		scope.snapDirs = append(scope.snapDirs, runner.SnapshotDir(abs))
	}
	scope.dirTrees = watchDirTrees(args)
	for _, path := range []string{reportJUnit, reportJSON} {
		if path != "" {
			scope.reports[watchAbs(path)] = true
		}
	}
	return scope
}

// watchDirTrees returns the absolute directory-argument roots -- every
// argument that is a directory, or the current directory when no arguments
// were given: the trees where newly created .dats files change the scope.
func watchDirTrees(args []string) []string {
	if len(args) == 0 {
		if cwd, err := os.Getwd(); err == nil {
			return []string{cwd}
		}
		return nil
	}
	var trees []string
	for _, arg := range args {
		if info, err := os.Stat(arg); err == nil && info.IsDir() {
			trees = append(trees, watchAbs(arg))
		}
	}
	return trees
}

// relevantChange reports whether a filesystem event should trigger a re-run:
//   - Chmod-only events are ignored; Write, Create, Remove, and Rename count.
//   - The report file paths never count -- they are the run's own outputs,
//     and reacting to them would retrigger the watch forever.
//   - A directory created under a directory-argument tree counts (the
//     re-cycle registers it, so its future files are seen); hidden names are
//     skipped, matching discovery.
//   - A .dats path counts when it is one of the resolved files, or when it
//     lies (non-hidden, like discovery) under a directory-argument tree --
//     creation or removal there changes the scope.
//   - A .golden path counts when it lies under a resolved file's snapshot
//     directory AND --update is not set: goldens are inputs, but under
//     --update the run itself rewrites them -- reacting would self-retrigger,
//     and the re-run would be a no-op anyway.
func (s *watchScope) relevantChange(op fsnotify.Op, path string, isDir bool) bool {
	if !op.Has(fsnotify.Write) && !op.Has(fsnotify.Create) && !op.Has(fsnotify.Remove) && !op.Has(fsnotify.Rename) {
		return false
	}
	abs := watchAbs(path)
	if s.reports[abs] {
		return false
	}
	if isDir {
		return op.Has(fsnotify.Create) && !strings.HasPrefix(filepath.Base(abs), ".") && underAny(abs, s.dirTrees)
	}
	switch filepath.Ext(abs) {
	case ".dats":
		if s.files[abs] {
			return true
		}
		return !strings.HasPrefix(filepath.Base(abs), ".") && underAny(abs, s.dirTrees)
	case ".golden":
		return !s.update && underAny(abs, s.snapDirs)
	}
	return false
}

// computeWatchDirs returns the deduplicated list of directories to register
// with fsnotify: the parent directory of every resolved .dats file (watching
// directories rather than files survives editors' rename-replace saves),
// each resolved file's snapshot golden directory when it exists (goldens are
// inputs), and every directory-argument tree -- the root plus all non-hidden
// subdirectories recursively (the same hidden-skip rule as discovery), so
// new .dats files and new subdirectories are noticed.
func computeWatchDirs(files, dirTrees []string) []string {
	var dirs []string
	for _, file := range files {
		abs := watchAbs(file)
		dirs = append(dirs, filepath.Dir(abs))
		if snapDir := runner.SnapshotDir(abs); isExistingDir(snapDir) {
			dirs = append(dirs, snapDir)
		}
	}
	for _, root := range dirTrees {
		// Like findDatsFiles, resolve a symlinked root so the walk descends
		// into it. Unwalkable paths are skipped: watching is best-effort.
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			dirs = append(dirs, path)
			return nil
		})
	}
	return dedupePaths(dirs)
}

// underDir reports whether path lies strictly below dir (both should be
// absolute; the directory itself does not count).
func underDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func underAny(path string, dirs []string) bool {
	for _, dir := range dirs {
		if underDir(path, dir) {
			return true
		}
	}
	return false
}

// watchAbs normalizes a path for scope comparisons, falling back to the
// cleaned input when the working directory is unavailable.
func watchAbs(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

func isExistingDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// stdoutIsTTY reports whether stdout is a character device: real terminals
// get a screen clear before each run, pipes and files get a separator line.
func stdoutIsTTY() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func init() {
	// watch inherits every persistent flag (-v, -j/--jobs, --report-junit/
	// --report-json, --update, --keep-temp, --coverdir) and adds none of its
	// own -- in particular no filtering or narrowing flags: dats always runs
	// everything in scope.
	rootCmd.AddCommand(watchCmd)
}
