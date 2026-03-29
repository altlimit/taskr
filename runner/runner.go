package runner

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/altlimit/taskr/config"
)

// urlPattern matches common URL patterns in log output.
var urlPattern = regexp.MustCompile(`https?://[^\s"'\x60\x29\x5d>]+`)

// CapturedURL holds a URL found in task output.
type CapturedURL struct {
	TaskLabel string
	URL       string
}

// Runner manages the lifecycle of all tasks.
type Runner struct {
	tasks       map[string]*managedTask
	taskOrder   []string
	hiddenTasks map[string]bool // per-task log visibility toggle
	logCh       chan config.LogLine
	urlCh       chan CapturedURL
	urlReady    chan string // signals task label when first URL is captured
	mu          sync.Mutex
	ctx         context.Context
	cancelAll   context.CancelFunc
}

type managedTask struct {
	config config.TaskConfig
	state  *config.TaskState
	cmd    *exec.Cmd
	cancel context.CancelFunc
	mu     sync.Mutex
}

// New creates a new Runner with the given task configs.
func New(configs []config.TaskConfig) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{
		tasks:       make(map[string]*managedTask),
		hiddenTasks: make(map[string]bool),
		logCh:       make(chan config.LogLine, 1000),
		urlCh:       make(chan CapturedURL, 100),
		urlReady:    make(chan string, 10),
		ctx:         ctx,
		cancelAll:   cancel,
	}
	for _, c := range configs {
		mt := &managedTask{
			config: c,
			state: &config.TaskState{
				Config: c,
				Status: config.StatusPending,
			},
		}
		r.tasks[c.Label] = mt
		r.taskOrder = append(r.taskOrder, c.Label)
		if c.HideLogs {
			r.hiddenTasks[c.Label] = true
		}
	}
	return r
}

// LogCh returns the channel the TUI reads log lines from.
func (r *Runner) LogCh() <-chan config.LogLine {
	return r.logCh
}

// URLCh returns the channel for auto-captured URLs.
func (r *Runner) URLCh() <-chan CapturedURL {
	return r.urlCh
}

// TaskOrder returns labels in their original order.
func (r *Runner) TaskOrder() []string {
	return r.taskOrder
}

// GetStatus returns the status of a specific task.
func (r *Runner) GetStatus(label string) config.TaskStatus {
	if mt, ok := r.tasks[label]; ok {
		return mt.state.GetStatus()
	}
	return config.StatusPending
}

// IsHidden returns whether log output is hidden for the given task.
func (r *Runner) IsHidden(label string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hiddenTasks[label]
}

// ToggleHidden flips the hidden state for a task and returns the new state.
func (r *Runner) ToggleHidden(label string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hiddenTasks[label] = !r.hiddenTasks[label]
	return r.hiddenTasks[label]
}

// StartAll launches all tasks.
// Dev server tasks (builtin watch mode) are staggered — each waits for a URL
// capture or a timeout before the next starts, preventing port conflicts.
func (r *Runner) StartAll() {
	var devServers []string
	var others []string

	for _, label := range r.taskOrder {
		mt := r.tasks[label]
		if mt.config.WatchMode == config.WatchBuiltin {
			devServers = append(devServers, label)
		} else {
			others = append(others, label)
		}
	}

	// Start non-dev-server tasks immediately in parallel
	for _, label := range others {
		mt := r.tasks[label]
		go r.startTask(mt)
	}

	// Stagger dev server tasks — wait for URL or timeout between each
	if len(devServers) > 1 {
		go func() {
			for i, label := range devServers {
				mt := r.tasks[label]
				go r.startTask(mt)

				// Don't wait after the last one
				if i < len(devServers)-1 {
					r.emitLog(label, "stdout", "[taskr] waiting for server ready before starting next...")
					r.waitForURL(label, 15*time.Second)
				}
			}
		}()
	} else {
		// 0 or 1 dev server, just start normally
		for _, label := range devServers {
			mt := r.tasks[label]
			go r.startTask(mt)
		}
	}
}

// waitForURL blocks until a URL is captured for the given task label, or timeout.
func (r *Runner) waitForURL(label string, timeout time.Duration) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			r.emitLog(label, "stdout", "[taskr] timeout waiting for server, starting next task...")
			return
		case <-ticker.C:
			// Check if a URL has been captured for this task
			r.mu.Lock()
			mt := r.tasks[label]
			status := mt.state.GetStatus()
			r.mu.Unlock()

			// If the task errored or stopped, don't wait
			if status == config.StatusErrored || status == config.StatusStopped {
				return
			}
		case url := <-r.urlReady:
			if url == label {
				r.emitLog(label, "stdout", "[taskr] server ready, starting next task...")
				return
			}
			// URL for a different task, keep waiting
		case <-r.ctx.Done():
			return
		}
	}
}

// StartSequential runs tasks in order, waiting for each to finish.
// Used when a compound task has dependsOrder: "sequence".
func (r *Runner) StartSequential(labels []string) {
	for _, label := range labels {
		mt := r.tasks[label]
		if mt == nil {
			continue
		}
		r.startTask(mt)
		// Wait for it to exit before starting next
		mt.mu.Lock()
		cmd := mt.cmd
		mt.mu.Unlock()
		if cmd != nil && cmd.Process != nil {
			cmd.Wait()
		}
	}
}

func (r *Runner) startTask(mt *managedTask) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	if r.ctx.Err() != nil {
		return
	}

	ctx, cancel := context.WithCancel(r.ctx)
	mt.cancel = cancel

	var cmd *exec.Cmd

	if mt.config.Type == "shell" {
		// Shell task: run through the system shell
		fullCmd := mt.config.Command
		if len(mt.config.Args) > 0 {
			fullCmd += " " + strings.Join(mt.config.Args, " ")
		}
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", fullCmd)
		} else {
			cmd = exec.Command("sh", "-c", fullCmd)
		}
	} else {
		// Process task: run command directly
		cmd = exec.Command(mt.config.Command, mt.config.Args...)
	}

	// Put process in its own group so we can kill the entire tree
	setProcGroup(cmd)

	cmd.Dir = mt.config.Cwd

	// Build environment
	env := os.Environ()
	for k, v := range mt.config.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	// Pipe stdout
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.emitLog(mt.config.Label, "stderr", fmt.Sprintf("[taskr] failed to pipe stdout: %v", err))
		mt.state.SetStatus(config.StatusErrored)
		return
	}
	// Pipe stderr
	stderr, err := cmd.StderrPipe()
	if err != nil {
		r.emitLog(mt.config.Label, "stderr", fmt.Sprintf("[taskr] failed to pipe stderr: %v", err))
		mt.state.SetStatus(config.StatusErrored)
		return
	}

	// Validate working directory exists before starting — the OS gives a
	// misleading "no such file or directory" for the shell binary when the
	// real problem is a missing cwd.
	if cmd.Dir != "" {
		if _, statErr := os.Stat(cmd.Dir); statErr != nil {
			r.emitLog(mt.config.Label, "stderr", fmt.Sprintf("[taskr] working directory does not exist: %s", cmd.Dir))
			mt.state.SetStatus(config.StatusErrored)
			return
		}
	}

	if err := cmd.Start(); err != nil {
		r.emitLog(mt.config.Label, "stderr", fmt.Sprintf("[taskr] failed to start: %v", err))
		mt.state.SetStatus(config.StatusErrored)
		return
	}
	mt.cmd = cmd
	mt.state.SetStatus(config.StatusRunning)
	r.emitLog(mt.config.Label, "stdout", fmt.Sprintf("[taskr] started (pid %d)", cmd.Process.Pid))

	// Scan stdout and stderr in goroutines
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line
		for scanner.Scan() {
			line := scanner.Text()
			r.emitLog(mt.config.Label, "stdout", line)
			r.extractURLs(mt.config.Label, line)
		}
	}()
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			r.emitLog(mt.config.Label, "stderr", line)
			r.extractURLs(mt.config.Label, line)
		}
	}()

	// Wait for pipes to be drained, then wait for process exit
	go func() {
		wg.Wait()
		err := cmd.Wait()
		if err != nil && ctx.Err() == nil {
			r.emitLog(mt.config.Label, "stderr", fmt.Sprintf("[taskr] exited with error: %v", err))
			mt.state.SetStatus(config.StatusErrored)
			r.emitLog(mt.config.Label, "stdout", "[taskr] waiting for file change or manual restart (r)...")
		} else if ctx.Err() != nil {
			// Cancelled/killed by us
			mt.state.SetStatus(config.StatusStopped)
		} else {
			r.emitLog(mt.config.Label, "stdout", "[taskr] exited successfully")
			mt.state.SetStatus(config.StatusStopped)
		}
	}()
}

// extractURLs scans a log line for URLs and sends them to the URL channel.
func (r *Runner) extractURLs(label string, line string) {
	matches := urlPattern.FindAllString(line, -1)
	for _, u := range matches {
		// Clean trailing punctuation
		u = strings.TrimRight(u, ".,;:!?)")
		select {
		case r.urlCh <- CapturedURL{TaskLabel: label, URL: u}:
		default:
			// Channel full, drop
		}
		// Signal staggered startup that this task has a URL
		select {
		case r.urlReady <- label:
		default:
		}
	}
}

func (r *Runner) emitLog(label, stream, content string) {
	select {
	case r.logCh <- config.LogLine{
		TaskLabel: label,
		Content:   content,
		Stream:    stream,
		Timestamp: time.Now(),
	}:
	default:
		// Channel full, drop oldest would require ring buffer — for now just drop
	}
}

// Log emits a message into the shared log channel under the given task label.
// Use label "taskr" for system-level messages.
func (r *Runner) Log(label, content string) {
	r.emitLog(label, "stdout", content)
}

// RestartTask kills and restarts a single task.
func (r *Runner) RestartTask(label string) {
	mt, ok := r.tasks[label]
	if !ok {
		return
	}

	r.emitLog(label, "stdout", "[taskr] restarting...")
	mt.state.SetStatus(config.StatusRestarting)

	// Kill existing
	r.killTask(mt)

	// Small delay to let the process fully exit
	time.Sleep(100 * time.Millisecond)

	// Restart
	go r.startTask(mt)
}

// Reload applies a new set of task configs without restarting the process.
// Tasks that no longer exist are stopped. New tasks are started. Tasks whose
// configs have changed are restarted. The TUI channels remain open throughout.
func (r *Runner) Reload(newConfigs []config.TaskConfig) {
	// Build a lookup for new configs
	newMap := make(map[string]config.TaskConfig, len(newConfigs))
	for _, c := range newConfigs {
		newMap[c.Label] = c
	}

	// Collect work under the lock — no blocking calls while holding it.
	r.mu.Lock()
	var toStop []struct {
		label string
		mt    *managedTask
	}
	for label, mt := range r.tasks {
		if _, exists := newMap[label]; !exists {
			toStop = append(toStop, struct {
				label string
				mt    *managedTask
			}{label, mt})
		}
	}

	var toRestart []*managedTask
	var toStart []*managedTask
	for _, c := range newConfigs {
		c := c
		if mt, exists := r.tasks[c.Label]; exists {
			if fmt.Sprintf("%+v", mt.config) != fmt.Sprintf("%+v", c) {
				mt.config = c
				mt.state.Config = c
				toRestart = append(toRestart, mt)
			}
		} else {
			mt := &managedTask{
				config: c,
				state: &config.TaskState{
					Config: c,
					Status: config.StatusPending,
				},
			}
			r.tasks[c.Label] = mt
			r.taskOrder = append(r.taskOrder, c.Label)
			toStart = append(toStart, mt)
		}
	}
	r.mu.Unlock()

	// Stop removed tasks and clean them up.
	for _, item := range toStop {
		r.emitLog(item.label, "stdout", "[taskr] task removed by config reload, stopping...")
		r.killTask(item.mt)
		r.mu.Lock()
		delete(r.tasks, item.label)
		for i, l := range r.taskOrder {
			if l == item.label {
				r.taskOrder = append(r.taskOrder[:i], r.taskOrder[i+1:]...)
				break
			}
		}
		r.mu.Unlock()
	}

	for _, mt := range toRestart {
		r.emitLog(mt.config.Label, "stdout", "[taskr] config changed, restarting...")
		r.RestartTask(mt.config.Label)
	}
	for _, mt := range toStart {
		r.emitLog(mt.config.Label, "stdout", "[taskr] new task added by config reload, starting...")
		go r.startTask(mt)
	}
}

// RestartAll restarts all tasks.
func (r *Runner) RestartAll() {
	for _, label := range r.taskOrder {
		r.RestartTask(label)
	}
}

// StopTask stops a single task.
func (r *Runner) StopTask(label string) {
	mt, ok := r.tasks[label]
	if !ok {
		return
	}
	r.emitLog(label, "stdout", "[taskr] stopping...")
	r.killTask(mt)
	mt.state.SetStatus(config.StatusStopped)
}

// StopAll stops all tasks.
func (r *Runner) StopAll() {
	for _, label := range r.taskOrder {
		r.StopTask(label)
	}
}

func (r *Runner) killTask(mt *managedTask) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	if mt.cancel != nil {
		mt.cancel()
	}

	if mt.cmd == nil || mt.cmd.Process == nil {
		return
	}

	// Step 1: Send graceful signal (SIGTERM on Unix, CTRL_BREAK on Windows)
	if err := signalProcessGroup(mt.cmd); err != nil {
		// If signaling fails, go straight to force kill
		killProcessGroup(mt.cmd)
		mt.cmd.Wait()
		return
	}

	// Step 2: Wait for the process to exit gracefully
	done := make(chan struct{})
	go func() {
		mt.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Process exited gracefully
		return
	case <-time.After(gracefulShutdownTimeout):
		// Step 3: Force kill after timeout
		r.emitLog(mt.config.Label, "stderr", "[taskr] process did not exit gracefully, force killing...")
		killProcessGroup(mt.cmd)
		<-done // wait for Wait() to complete after kill
	}
}

// Shutdown gracefully stops everything.
func (r *Runner) Shutdown() {
	r.cancelAll()
	for _, mt := range r.tasks {
		r.killTask(mt)
	}
	close(r.logCh)
	close(r.urlCh)
}
