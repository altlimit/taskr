package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/altlimit/taskr/config"
	"github.com/altlimit/taskr/parser"
	"github.com/altlimit/taskr/runner"
	"github.com/altlimit/taskr/tui"
	"github.com/altlimit/taskr/watcher"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("v", false, "Show version")
	configPath := flag.String("config", "", "Path to tasks.json (default: auto-detect .vscode/tasks.json)")
	noWatch := flag.Bool("no-watch", false, "Disable file watching for all tasks")
	debounce := flag.Duration("watch-debounce", 300*time.Millisecond, "File watcher debounce duration")
	flag.Parse()

	if *showVersion {
		fmt.Println("taskr version", version)
		os.Exit(0)
	}

	taskLabels := flag.Args() // Support multiple labels: taskr api web worker

	// 1. Find and parse tasks.json
	var tasksPath, workspaceRoot string
	var err error

	if *configPath != "" {
		tasksPath = *configPath
		workspaceRoot, _ = os.Getwd()
	} else {
		cwd, _ := os.Getwd()
		tasksPath, workspaceRoot, err = parser.FindTasksJSON(cwd)
		if err != nil {
			log.Fatalf("Error: %v\nRun this from a directory with .vscode/tasks.json, or pass --config", err)
		}
	}

	allTasks, err := parser.Parse(tasksPath, workspaceRoot)
	if err != nil {
		log.Fatalf("Error parsing tasks.json: %v", err)
	}

	if len(allTasks) == 0 {
		log.Fatal("No tasks found in tasks.json")
	}

	// 2. If no task labels given, show interactive picker (multi-select)
	if len(taskLabels) == 0 {
		taskLabels, err = pickTasks(allTasks)
		if err != nil {
			log.Fatalf("Error: %v", err)
		}
		if len(taskLabels) == 0 {
			log.Fatal("No tasks selected")
		}
	}

	// 3. Resolve dependencies for all selected labels (merged, deduped)
	tasksToRun, err := parser.ResolveMultipleDependencies(taskLabels, allTasks)
	if err != nil {
		log.Fatalf("Error resolving tasks: %v", err)
	}

	// 4. Detect watch modes
	for i := range tasksToRun {
		if *noWatch {
			tasksToRun[i].WatchMode = config.WatchNone
			tasksToRun[i].WatchEnabled = false
		} else {
			watcher.DetectWatchMode(&tasksToRun[i])
		}
	}

	// 5. Start the runner
	r := runner.New(tasksToRun)
	r.StartAll()

	// 6. Start file watchers (in background so TUI starts immediately)
	w := watcher.New(*debounce)
	go func() {
		for _, tc := range tasksToRun {
			if err := w.Watch(tc, workspaceRoot, func(label string) {
				r.RestartTask(label)
			}); err != nil {
				fmt.Fprintf(os.Stderr, "[taskr] warning: could not watch files for %s: %v\n", tc.Label, err)
			}
		}
	}()

	// 7. Launch TUI
	model := tui.NewModel(r,
		func(label string) bool { return w.Toggle(label) },
		func(label string) bool { return w.IsEnabled(label) },
	)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
	}

	// 8. Cleanup
	w.Shutdown()
	r.Shutdown()
}

// pickTasks shows an interactive task picker.
// Space toggles tasks, Enter confirms. If nothing is toggled, runs the highlighted task.
func pickTasks(tasks []config.TaskConfig) ([]string, error) {
	m := pickerModel{tasks: tasks, toggled: make(map[int]bool)}
	p := tea.NewProgram(m)
	result, err := p.Run()
	if err != nil {
		return nil, err
	}
	pm := result.(pickerModel)
	if pm.cancelled {
		return nil, fmt.Errorf("cancelled")
	}
	return pm.selected(), nil
}

type pickerModel struct {
	tasks     []config.TaskConfig
	cursor    int
	toggled   map[int]bool
	cancelled bool
	done      bool
}

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.tasks)-1 {
				m.cursor++
			}
		case " ":
			m.toggled[m.cursor] = !m.toggled[m.cursor]
		case "enter":
			m.done = true
			return m, tea.Quit
		case "q", "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m pickerModel) View() string {
	s := "\n  \033[1;35m❯ TaskR\033[0m  \033[2mSelect tasks (Space toggle, Enter confirm)\033[0m\n\n"
	for i, t := range m.tasks {
		cursor := "  "
		if m.cursor == i {
			cursor = "\033[35m❯ \033[0m"
		}
		check := "\033[2m○\033[0m"
		if m.toggled[i] {
			check = "\033[32m✓\033[0m"
		}
		desc := t.Command
		if len(t.DependsOn) > 0 {
			desc = fmt.Sprintf("(%d tasks) %s", len(t.DependsOn), desc)
		}
		label := t.Label
		if m.cursor == i {
			label = "\033[1m" + label + "\033[0m"
		}
		s += fmt.Sprintf("%s%s %s  \033[2m%s\033[0m\n", cursor, check, label, desc)
	}
	s += "\n"
	return s
}

func (m pickerModel) selected() []string {
	var labels []string
	for i, t := range m.tasks {
		if m.toggled[i] {
			labels = append(labels, t.Label)
		}
	}
	// If nothing toggled, use the highlighted task
	if len(labels) == 0 && len(m.tasks) > 0 {
		labels = []string{m.tasks[m.cursor].Label}
	}
	return labels
}
