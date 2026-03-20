package config

import (
	"sync"
	"time"
)

// WatchMode determines how file watching behaves for a task.
type WatchMode string

const (
	WatchNone    WatchMode = "none"    // No file watching
	WatchSelf    WatchMode = "self"    // We watch & restart the process
	WatchBuiltin WatchMode = "builtin" // Command has its own watcher (e.g. vite, nodemon)
)

// TaskStatus represents the current state of a running task.
type TaskStatus int

const (
	StatusPending    TaskStatus = iota
	StatusRunning
	StatusStopped
	StatusRestarting
	StatusErrored
)

func (s TaskStatus) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusRunning:
		return "running"
	case StatusStopped:
		return "stopped"
	case StatusRestarting:
		return "restarting"
	case StatusErrored:
		return "errored"
	default:
		return "unknown"
	}
}

// TaskConfig is the resolved configuration for a single runnable task.
type TaskConfig struct {
	Label       string
	Type        string // "shell" or "process"
	Command     string
	Args        []string
	Cwd         string
	Env         map[string]string
	DependsOn   []string
	DependsOrder string // "parallel" or "sequence"
	IsBackground bool
	Group        string

	// Watch settings
	WatchMode       WatchMode
	WatchEnabled    bool     // Runtime toggle (can be flipped in TUI)
	WatchExtensions []string // e.g. [".go", ".html", ".tmpl"]
	WatchPaths      []string // relative paths to watch (default: workspace root)
}

// LogLine is a single log entry emitted by a running task.
type LogLine struct {
	TaskLabel string
	Content   string
	Stream    string // "stdout" or "stderr"
	Timestamp time.Time
}

// TaskState holds the runtime state for a single managed task.
type TaskState struct {
	Config  TaskConfig
	Status  TaskStatus
	mu      sync.Mutex
}

func (ts *TaskState) SetStatus(s TaskStatus) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.Status = s
}

func (ts *TaskState) GetStatus() TaskStatus {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.Status
}
