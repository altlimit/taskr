package watcher

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/altlimit/taskr/config"
	"github.com/fsnotify/fsnotify"
)

// command patterns for watch mode auto-detection
var builtinWatchPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bvite\b`),
	regexp.MustCompile(`(?i)\bnext\s+dev\b`),
	regexp.MustCompile(`(?i)\bnuxt\s+dev\b`),
	regexp.MustCompile(`(?i)\bnodemon\b`),
	regexp.MustCompile(`(?i)\btsc\s+.*--watch\b`),
	regexp.MustCompile(`(?i)\bcargo\s+watch\b`),
	regexp.MustCompile(`(?i)\bnpm\s+run\s+dev\b`),
	regexp.MustCompile(`(?i)\byarn\s+dev\b`),
	regexp.MustCompile(`(?i)\bpnpm\s+dev\b`),
	regexp.MustCompile(`(?i)\bwebpack\s+serve\b`),
	regexp.MustCompile(`(?i)\bng\s+serve\b`),
}

var selfWatchPatterns = []struct {
	Pattern    *regexp.Regexp
	Extensions []string
}{
	{regexp.MustCompile(`(?i)\bgo\s+(run|build)\b`), []string{".go", ".mod", ".sum"}},
	{regexp.MustCompile(`(?i)\bpython\d?\s+`), []string{".py"}},
	{regexp.MustCompile(`(?i)\bnode\s+[^-]`), []string{".js", ".ts", ".json"}},
	{regexp.MustCompile(`(?i)\bdeno\s+run\b`), []string{".ts", ".js", ".json"}},
	{regexp.MustCompile(`(?i)\bruby\s+`), []string{".rb"}},
	{regexp.MustCompile(`(?i)\bcargo\s+run\b`), []string{".rs", ".toml"}},
	{regexp.MustCompile(`(?i)\bdotnet\s+run\b`), []string{".cs", ".csproj", ".json"}},
}

// Default ignored directories
var defaultIgnoreDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
	".next":        true,
	".nuxt":        true,
	"dist":         true,
	"build":        true,
	"target":       true,
	"bin":          true,
	"obj":          true,
	".vscode":      true,
}

// isIgnoredPath returns true if any path segment matches a defaultIgnoreDirs entry.
func isIgnoredPath(path string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
		if defaultIgnoreDirs[seg] {
			return true
		}
	}
	return false
}

// DetectWatchMode inspects a task command and determines the appropriate watch mode.
// Also sets default watch extensions if not already configured.
func DetectWatchMode(tc *config.TaskConfig) {
	// If TASKR_WATCH already set an explicit mode, respect it
	if tc.WatchMode == config.WatchSelf || tc.WatchMode == config.WatchBuiltin {
		if tc.WatchMode == config.WatchSelf {
			tc.WatchEnabled = true
		}
		return
	}
	// Was explicitly disabled
	if tc.Env != nil {
		if v, ok := tc.Env["TASKR_WATCH"]; ok && strings.ToLower(v) == "false" {
			tc.WatchMode = config.WatchNone
			tc.WatchEnabled = false
			return
		}
	}

	fullCmd := tc.Command
	if len(tc.Args) > 0 {
		fullCmd += " " + strings.Join(tc.Args, " ")
	}

	// Check builtin patterns first (these have their own file watching)
	for _, p := range builtinWatchPatterns {
		if p.MatchString(fullCmd) {
			tc.WatchMode = config.WatchBuiltin
			tc.WatchEnabled = false
			return
		}
	}

	// Check self-watch patterns (we need to watch for them)
	for _, sp := range selfWatchPatterns {
		if sp.Pattern.MatchString(fullCmd) {
			tc.WatchMode = config.WatchSelf
			tc.WatchEnabled = true
			if len(tc.WatchExtensions) == 0 {
				tc.WatchExtensions = sp.Extensions
			}
			return
		}
	}

	tc.WatchMode = config.WatchNone
	tc.WatchEnabled = false
}

// OnChange is a callback type invoked when watched files change.
type OnChange func(label string)

// Watcher manages file watching for multiple tasks.
type Watcher struct {
	watchers   map[string]*taskWatcher
	mu         sync.Mutex
	debounce   time.Duration
}

type taskWatcher struct {
	label      string
	fsWatcher  *fsnotify.Watcher
	extensions []string
	enabled    bool
	done       chan struct{}
	onChange   OnChange
	mu         sync.Mutex
}

// New creates a Watcher with the given debounce duration.
func New(debounce time.Duration) *Watcher {
	if debounce == 0 {
		debounce = 300 * time.Millisecond
	}
	return &Watcher{
		watchers: make(map[string]*taskWatcher),
		debounce: debounce,
	}
}

// Watch starts file watching for a task.
func (w *Watcher) Watch(tc config.TaskConfig, workspaceRoot string, onChange OnChange) error {
	if !tc.WatchEnabled || tc.WatchMode != config.WatchSelf {
		return nil
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	tw := &taskWatcher{
		label:      tc.Label,
		fsWatcher:  fsw,
		extensions: tc.WatchExtensions,
		enabled:    true,
		done:       make(chan struct{}),
		onChange:   onChange,
	}

	// Determine watch roots
	watchPaths := tc.WatchPaths
	if len(watchPaths) == 0 {
		// Default: watch the task's own working directory, not the whole workspace
		if tc.Cwd != "" {
			watchPaths = []string{tc.Cwd}
		} else {
			watchPaths = []string{workspaceRoot}
		}
	}

	// Start event loop immediately (handles events as dirs are added)
	go tw.eventLoop(w.debounce)

	w.mu.Lock()
	w.watchers[tc.Label] = tw
	w.mu.Unlock()

	// Walk and add directories in the background so we don't block startup.
	// We yield every 50 directories so the Go scheduler can run other
	// goroutines (e.g. the TUI log reader) during a large workspace walk.
	go func() {
		const yieldEvery = 50
		var count int
		for _, wp := range watchPaths {
			root := wp
			if !filepath.IsAbs(root) {
				root = filepath.Join(workspaceRoot, root)
			}
			filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if info.IsDir() {
					base := filepath.Base(path)
					if defaultIgnoreDirs[base] {
						return filepath.SkipDir
					}
					fsw.Add(path)
					count++
					if count%yieldEvery == 0 {
						runtime.Gosched()
					}
				}
				return nil
			})
		}
	}()

	return nil
}

func (tw *taskWatcher) eventLoop(debounce time.Duration) {
	var timer *time.Timer
	for {
		select {
		case event, ok := <-tw.fsWatcher.Events:
			if !ok {
				return
			}
			tw.mu.Lock()
			enabled := tw.enabled
			tw.mu.Unlock()
			if !enabled {
				continue
			}

			// Filter out events inside ignored directories (e.g. node_modules, .git)
			if isIgnoredPath(event.Name) {
				continue
			}

			// Filter by extension
			if len(tw.extensions) > 0 {
				ext := strings.ToLower(filepath.Ext(event.Name))
				matched := false
				for _, e := range tw.extensions {
					if ext == e {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}

			// Only trigger on write/create/remove
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove) == 0 {
				continue
			}

			// Debounce
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounce, func() {
				tw.onChange(tw.label)
			})

		case _, ok := <-tw.fsWatcher.Errors:
			if !ok {
				return
			}

		case <-tw.done:
			return
		}
	}
}

// Toggle enables or disables watching for a specific task. Returns new state.
func (w *Watcher) Toggle(label string) bool {
	w.mu.Lock()
	tw, ok := w.watchers[label]
	w.mu.Unlock()
	if !ok {
		return false
	}
	tw.mu.Lock()
	tw.enabled = !tw.enabled
	state := tw.enabled
	tw.mu.Unlock()
	return state
}

// IsEnabled returns whether watching is active for a task.
func (w *Watcher) IsEnabled(label string) bool {
	w.mu.Lock()
	tw, ok := w.watchers[label]
	w.mu.Unlock()
	if !ok {
		return false
	}
	tw.mu.Lock()
	defer tw.mu.Unlock()
	return tw.enabled
}

// Shutdown stops all watchers.
func (w *Watcher) Shutdown() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, tw := range w.watchers {
		close(tw.done)
		tw.fsWatcher.Close()
	}
}
