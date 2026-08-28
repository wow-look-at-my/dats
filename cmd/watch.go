package cmd

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
	"github.com/wow-look-at-my/go-containers/set"

	dats "github.com/wow-look-at-my/dats"
	"github.com/wow-look-at-my/dats/internal/paths"
)

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

// runWatchCommand implements dats watch: an initial run, then re-runs driven by filesystem events.
func runWatchCommand(cmd *cobra.Command, args []string) error {
	jobs, err := resolveJobs(cmd.Flags())
	if err != nil {
		return err
	}
	sandbox, err := resolveSandbox(cmd.Flags())
	if err != nil {
		return err
	}
	sshTarget, err := resolveSSH(cmd.Flags())
	if err != nil {
		return err
	}
	if _, err := dats.FindFiles(args); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	w := &watchSession{
		args:      args,
		jobs:      jobs,
		sandbox:   sandbox,
		sshTarget: sshTarget,
		out:       os.Stdout,
		isTTY:     stdoutIsTTY(),
		events:    make(chan watchEvent, 64),
	}
	defer w.closeWatcher()

	watchLoop(ctx, w.events, w.cycle, watchDebounce)

	fmt.Fprint(w.out, "\n# watch: interrupted, exiting\n")
	return nil
}

// watchEvent is one relevant filesystem change, as delivered to watchLoop.
type watchEvent struct {
	path string
}

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
		cycle(ctx, paths.Dedupe(changed))
	}
}

type watchSession struct {
	args      []string
	jobs      int
	sandbox   *runner.SandboxConfig
	sshTarget string
	out       io.Writer
	isTTY     bool

	run     int      // completed-run counter (1-based in headers)
	files   []string // last successfully resolved file list
	watcher *fsnotify.Watcher
	events  chan watchEvent
}

func (w *watchSession) cycle(ctx context.Context, changed []string) {
	// Re-resolve so new, renamed, or removed .dats files under directory arguments join or leave the scope.
	files, err := dats.FindFiles(w.args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		w.printFooter()
		return
	}
	w.files = files

	if err := w.rebuildWatches(); err != nil {
		// Keep the previous watcher; watching degrades, the run still counts.
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}

	w.run++
	w.printHeader(changed)

	runErr := runTests(ctx, w.args, w.out, w.jobs, w.sandbox, w.sshTarget)
	if ctx.Err() != nil {
		return
	}
	if runErr != nil && !errors.Is(runErr, errTestsFailed) {
		fmt.Fprintf(os.Stderr, "Error: %v\n", runErr)
	}
	w.printFooter()
}

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

// watchScope is one cycle's relevance configuration, all paths absolute.
type watchScope struct {
	files    set.Set[string] // resolved .dats files
	snapDirs []string        // the resolved files' snapshot golden directories
	dirTrees []string        // directory arguments (or the cwd in no-arg mode)
	reports  set.Set[string] // --report-junit/--report-json output paths
	update   bool            // --update: the run itself rewrites goldens
}

func buildWatchScope(files, args []string) *watchScope {
	scope := &watchScope{
		files:   set.New[string](len(files)),
		reports: set.New[string](2),
		update:  updateGoldens,
	}
	for _, file := range files {
		abs := watchAbs(file)
		scope.files.Add(abs)
		scope.snapDirs = append(scope.snapDirs, runner.SnapshotDir(abs))
	}
	scope.dirTrees = watchDirTrees(args)
	for _, path := range []string{reportJUnit, reportJSON} {
		if path != "" {
			scope.reports.Add(watchAbs(path))
		}
	}
	return scope
}

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

func (s *watchScope) relevantChange(op fsnotify.Op, path string, isDir bool) bool {
	if !op.Has(fsnotify.Write) && !op.Has(fsnotify.Create) && !op.Has(fsnotify.Remove) && !op.Has(fsnotify.Rename) {
		return false
	}
	abs := watchAbs(path)
	if s.reports.Contains(abs) {
		return false
	}
	if isDir {
		return op.Has(fsnotify.Create) && !strings.HasPrefix(filepath.Base(abs), ".") && underAny(abs, s.dirTrees)
	}
	switch filepath.Ext(abs) {
	case ".dats":
		if s.files.Contains(abs) {
			return true
		}
		return !strings.HasPrefix(filepath.Base(abs), ".") && underAny(abs, s.dirTrees)
	case ".golden":
		return !s.update && underAny(abs, s.snapDirs)
	}
	return false
}

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
		// Like findDatsFiles, resolve a symlinked root so the walk descends into it.
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
	return paths.Dedupe(dirs)
}

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

func stdoutIsTTY() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func init() {
	rootCmd.AddCommand(watchCmd)
}
