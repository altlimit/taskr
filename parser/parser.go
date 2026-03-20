package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/altlimit/taskr/config"
)

// --- Raw JSON schema types matching VSCode tasks.json ---

type rawTasksFile struct {
	Version string        `json:"version"`
	Tasks   []rawTask     `json:"tasks"`
	Options *rawOptions   `json:"options,omitempty"`
}

type rawTask struct {
	Label        string            `json:"label"`
	Type         string            `json:"type"`
	Command      string            `json:"command"`
	Args         []string          `json:"args,omitempty"`
	DependsOn    jsonStringOrArray `json:"dependsOn,omitempty"`
	DependsOrder string            `json:"dependsOrder,omitempty"`
	IsBackground bool              `json:"isBackground,omitempty"`
	Group        json.RawMessage   `json:"group,omitempty"`
	Options      *rawOptions       `json:"options,omitempty"`
	// Windows/linux/osx overrides (future)
}

type rawOptions struct {
	Cwd string            `json:"cwd,omitempty"`
	Env map[string]string `json:"env,omitempty"`
}

// jsonStringOrArray handles dependsOn being either a string or []string.
type jsonStringOrArray []string

func (j *jsonStringOrArray) UnmarshalJSON(data []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*j = []string{s}
		return nil
	}
	// Try array
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	*j = arr
	return nil
}

// FindTasksJSON walks up from startDir looking for .vscode/tasks.json.
// Returns the path and the workspace root directory.
func FindTasksJSON(startDir string) (tasksPath string, workspaceRoot string, err error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", "", err
	}

	for {
		candidate := filepath.Join(dir, ".vscode", "tasks.json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", "", fmt.Errorf("could not find .vscode/tasks.json walking up from %s", startDir)
}

// stripComments removes single-line // comments and trailing commas from
// JSON-with-comments (JSONC) content used by VSCode.
func stripComments(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	var result []string
	inBlockComment := false
	for _, line := range lines {
		if inBlockComment {
			if idx := strings.Index(line, "*/"); idx >= 0 {
				line = line[idx+2:]
				inBlockComment = false
			} else {
				continue
			}
		}
		// Remove block comment starts
		if idx := strings.Index(line, "/*"); idx >= 0 {
			endIdx := strings.Index(line[idx+2:], "*/")
			if endIdx >= 0 {
				line = line[:idx] + line[idx+2+endIdx+2:]
			} else {
				line = line[:idx]
				inBlockComment = true
			}
		}
		// Remove line comments (// but not inside strings - simple heuristic)
		if idx := lineCommentIndex(line); idx >= 0 {
			line = line[:idx]
		}
		result = append(result, line)
	}
	joined := strings.Join(result, "\n")
	// Remove trailing commas before } or ]
	re := regexp.MustCompile(`,\s*([}\]])`)
	joined = re.ReplaceAllString(joined, "$1")
	return []byte(joined)
}

// lineCommentIndex finds the index of a // line comment that is NOT inside a quoted string.
func lineCommentIndex(line string) int {
	inString := false
	escaped := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if !inString && i+1 < len(line) && ch == '/' && line[i+1] == '/' {
			return i
		}
	}
	return -1
}

var varRe = regexp.MustCompile(`\$\{([^}]+)\}`)

// resolveVariables replaces VSCode-style variables in a string.
func resolveVariables(s string, workspaceRoot string) string {
	return varRe.ReplaceAllStringFunc(s, func(match string) string {
		inner := match[2 : len(match)-1] // strip ${ and }
		switch {
		case inner == "workspaceFolder":
			return workspaceRoot
		case inner == "workspaceRoot":
			return workspaceRoot
		case inner == "cwd":
			cwd, _ := os.Getwd()
			return cwd
		case strings.HasPrefix(inner, "env:"):
			envVar := inner[4:]
			return os.Getenv(envVar)
		case inner == "pathSeparator":
			return string(os.PathSeparator)
		default:
			return match // leave unresolved
		}
	})
}

// Parse reads a tasks.json file and returns resolved TaskConfigs.
func Parse(tasksPath string, workspaceRoot string) ([]config.TaskConfig, error) {
	data, err := os.ReadFile(tasksPath)
	if err != nil {
		return nil, fmt.Errorf("reading tasks.json: %w", err)
	}

	data = stripComments(data)

	var raw rawTasksFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing tasks.json: %w", err)
	}

	// Global env/cwd from top-level options
	globalEnv := map[string]string{}
	globalCwd := ""
	if raw.Options != nil {
		for k, v := range raw.Options.Env {
			globalEnv[k] = resolveVariables(v, workspaceRoot)
		}
		if raw.Options.Cwd != "" {
			globalCwd = resolveVariables(raw.Options.Cwd, workspaceRoot)
		}
	}

	var configs []config.TaskConfig

	for _, rt := range raw.Tasks {
		tc := config.TaskConfig{
			Label:        rt.Label,
			Type:         rt.Type,
			Command:      resolveVariables(rt.Command, workspaceRoot),
			Args:         make([]string, len(rt.Args)),
			DependsOn:    []string(rt.DependsOn),
			DependsOrder: rt.DependsOrder,
			IsBackground: rt.IsBackground,
		}

		if tc.DependsOrder == "" {
			tc.DependsOrder = "parallel"
		}
		if tc.Type == "" {
			tc.Type = "shell"
		}

		// Resolve args
		for i, a := range rt.Args {
			tc.Args[i] = resolveVariables(a, workspaceRoot)
		}

		// Resolve group
		if rt.Group != nil {
			var groupStr string
			if err := json.Unmarshal(rt.Group, &groupStr); err == nil {
				tc.Group = groupStr
			} else {
				var groupObj struct {
					Kind string `json:"kind"`
				}
				if err := json.Unmarshal(rt.Group, &groupObj); err == nil {
					tc.Group = groupObj.Kind
				}
			}
		}

		// Merge env: OS env is handled at exec time, here we layer global + task
		mergedEnv := map[string]string{}
		for k, v := range globalEnv {
			mergedEnv[k] = v
		}
		if rt.Options != nil {
			for k, v := range rt.Options.Env {
				mergedEnv[k] = resolveVariables(v, workspaceRoot)
			}
		}
		tc.Env = mergedEnv

		// Resolve cwd
		cwd := globalCwd
		if rt.Options != nil && rt.Options.Cwd != "" {
			cwd = resolveVariables(rt.Options.Cwd, workspaceRoot)
		}
		if cwd == "" {
			cwd = workspaceRoot
		}
		if !filepath.IsAbs(cwd) {
			cwd = filepath.Join(workspaceRoot, cwd)
		}
		tc.Cwd = cwd

		// Extract TASKR_* overrides from env
		parseTaskrEnvOverrides(&tc)

		configs = append(configs, tc)
	}

	return configs, nil
}

// parseTaskrEnvOverrides reads TASKR_* env vars from the task's merged env
// and applies them as watch config. These don't cause VSCode lint errors
// because env vars in tasks.json are freeform.
func parseTaskrEnvOverrides(tc *config.TaskConfig) {
	if v, ok := tc.Env["TASKR_WATCH"]; ok {
		switch strings.ToLower(v) {
		case "true", "1", "yes":
			tc.WatchMode = config.WatchSelf
			tc.WatchEnabled = true
		case "false", "0", "no":
			tc.WatchMode = config.WatchNone
			tc.WatchEnabled = false
		}
		delete(tc.Env, "TASKR_WATCH") // don't pass to child process
	}

	if v, ok := tc.Env["TASKR_WATCH_EXTENSIONS"]; ok {
		parts := strings.Split(v, ",")
		for _, ext := range parts {
			ext = strings.TrimSpace(ext)
			if ext != "" {
				if !strings.HasPrefix(ext, ".") {
					ext = "." + ext
				}
				tc.WatchExtensions = append(tc.WatchExtensions, ext)
			}
		}
		delete(tc.Env, "TASKR_WATCH_EXTENSIONS")
	}

	if v, ok := tc.Env["TASKR_WATCH_PATHS"]; ok {
		parts := strings.Split(v, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				tc.WatchPaths = append(tc.WatchPaths, p)
			}
		}
		delete(tc.Env, "TASKR_WATCH_PATHS")
	}
}

// ResolveDependencies takes a selected task label, the full list of configs,
// and returns the flat list of tasks to run (resolving dependsOn recursively).
// Returns tasks in dependency order.
func ResolveDependencies(label string, allTasks []config.TaskConfig) ([]config.TaskConfig, error) {
	return ResolveMultipleDependencies([]string{label}, allTasks)
}

// ResolveMultipleDependencies takes multiple selected task labels, resolves all
// their dependsOn chains, and returns a merged, deduplicated list in dependency order.
func ResolveMultipleDependencies(labels []string, allTasks []config.TaskConfig) ([]config.TaskConfig, error) {
	taskMap := map[string]config.TaskConfig{}
	for _, t := range allTasks {
		taskMap[t.Label] = t
	}

	visited := map[string]bool{}
	var result []config.TaskConfig

	var resolve func(l string) error
	resolve = func(l string) error {
		if visited[l] {
			return nil
		}
		visited[l] = true

		t, ok := taskMap[l]
		if !ok {
			return fmt.Errorf("task %q not found", l)
		}

		// Resolve dependencies first
		for _, dep := range t.DependsOn {
			if err := resolve(dep); err != nil {
				return err
			}
		}

		// Skip compound tasks with empty commands (they're just grouping)
		if t.Command != "" || len(t.DependsOn) == 0 {
			result = append(result, t)
		}

		return nil
	}

	for _, label := range labels {
		if err := resolve(label); err != nil {
			return nil, err
		}
	}

	return result, nil
}

