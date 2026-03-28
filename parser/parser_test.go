package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/altlimit/taskr/config"
)

// --- stripComments tests ---

func TestStripComments_LineComments(t *testing.T) {
	input := []byte(`{
  // This is a comment
  "key": "value" // trailing comment
}`)
	got := string(stripComments(input))
	if contains(got, "// This is a comment") {
		t.Errorf("line comment not stripped: %s", got)
	}
	if contains(got, "// trailing") {
		t.Errorf("trailing comment not stripped: %s", got)
	}
	if !contains(got, `"key": "value"`) {
		t.Errorf("value was incorrectly stripped: %s", got)
	}
}

func TestStripComments_BlockComments(t *testing.T) {
	input := []byte(`{
  /* block comment */
  "key": "value"
}`)
	got := string(stripComments(input))
	if contains(got, "block comment") {
		t.Errorf("block comment not stripped: %s", got)
	}
	if !contains(got, `"key": "value"`) {
		t.Errorf("value was incorrectly stripped: %s", got)
	}
}

func TestStripComments_MultiLineBlockComment(t *testing.T) {
	input := []byte(`{
  /*
   * multi-line
   * block comment
   */
  "key": "value"
}`)
	got := string(stripComments(input))
	if contains(got, "multi-line") || contains(got, "block comment") {
		t.Errorf("multi-line block comment not stripped: %s", got)
	}
	if !contains(got, `"key": "value"`) {
		t.Errorf("value was incorrectly stripped: %s", got)
	}
}

func TestStripComments_TrailingCommas(t *testing.T) {
	input := []byte(`{
  "tasks": [
    {"label": "a"},
    {"label": "b"},
  ]
}`)
	got := string(stripComments(input))
	// Should be valid JSON after stripping
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Errorf("trailing comma not removed, invalid JSON: %v\nGot: %s", err, got)
	}
}

func TestStripComments_CommentInsideString(t *testing.T) {
	input := []byte(`{
  "url": "http://example.com//path",
  "note": "this has // in it"
}`)
	got := string(stripComments(input))
	if !contains(got, `"http://example.com//path"`) {
		t.Errorf("// inside string was incorrectly stripped: %s", got)
	}
	if !contains(got, `"this has // in it"`) {
		t.Errorf("// inside string was incorrectly stripped: %s", got)
	}
}

// --- lineCommentIndex tests ---

func TestLineCommentIndex(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		want  int
	}{
		{"no comment", `"key": "value"`, -1},
		{"line comment", `"key": "value" // comment`, 15},
		{"comment inside string", `"url": "http://example.com"`, -1},
		{"escaped quote then comment", `"say \"hello\"" // comment`, 16},
		{"only comment", `// full line comment`, 0},
		{"empty", ``, -1},
		{"double slash in string then real comment", `"a//b" // real`, 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lineCommentIndex(tt.line)
			if got != tt.want {
				t.Errorf("lineCommentIndex(%q) = %d, want %d", tt.line, got, tt.want)
			}
		})
	}
}

// --- resolveVariables tests ---

func TestResolveVariables_WorkspaceFolder(t *testing.T) {
	got := resolveVariables("${workspaceFolder}/src", "/home/user/project")
	if got != "/home/user/project/src" {
		t.Errorf("got %q, want %q", got, "/home/user/project/src")
	}
}

func TestResolveVariables_WorkspaceRoot(t *testing.T) {
	got := resolveVariables("${workspaceRoot}/src", "/home/user/project")
	if got != "/home/user/project/src" {
		t.Errorf("got %q, want %q", got, "/home/user/project/src")
	}
}

func TestResolveVariables_EnvVar(t *testing.T) {
	os.Setenv("TASKR_TEST_VAR", "hello")
	defer os.Unsetenv("TASKR_TEST_VAR")

	got := resolveVariables("${env:TASKR_TEST_VAR}_world", "/root")
	if got != "hello_world" {
		t.Errorf("got %q, want %q", got, "hello_world")
	}
}

func TestResolveVariables_PathSeparator(t *testing.T) {
	got := resolveVariables("a${pathSeparator}b", "/root")
	want := "a" + string(os.PathSeparator) + "b"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveVariables_UnknownVar(t *testing.T) {
	got := resolveVariables("${unknownVariable}", "/root")
	if got != "${unknownVariable}" {
		t.Errorf("unknown var should be left as-is, got %q", got)
	}
}

func TestResolveVariables_Multiple(t *testing.T) {
	got := resolveVariables("${workspaceFolder}/bin/${workspaceFolder}/lib", "/ws")
	if got != "/ws/bin//ws/lib" {
		t.Errorf("got %q, want %q", got, "/ws/bin//ws/lib")
	}
}

func TestResolveVariables_NoVars(t *testing.T) {
	got := resolveVariables("plain string", "/root")
	if got != "plain string" {
		t.Errorf("got %q, want %q", got, "plain string")
	}
}

// --- jsonStringOrArray tests ---

func TestJsonStringOrArray_String(t *testing.T) {
	var j jsonStringOrArray
	err := json.Unmarshal([]byte(`"single-task"`), &j)
	if err != nil {
		t.Fatal(err)
	}
	if len(j) != 1 || j[0] != "single-task" {
		t.Errorf("got %v, want [single-task]", j)
	}
}

func TestJsonStringOrArray_Array(t *testing.T) {
	var j jsonStringOrArray
	err := json.Unmarshal([]byte(`["task-a", "task-b"]`), &j)
	if err != nil {
		t.Fatal(err)
	}
	if len(j) != 2 || j[0] != "task-a" || j[1] != "task-b" {
		t.Errorf("got %v, want [task-a, task-b]", j)
	}
}

func TestJsonStringOrArray_InvalidJSON(t *testing.T) {
	var j jsonStringOrArray
	err := json.Unmarshal([]byte(`123`), &j)
	if err == nil {
		t.Error("expected error for numeric input")
	}
}

// --- Parse integration tests ---

func writeTempTasksJSON(t *testing.T, content string) (tasksPath string, workspaceRoot string) {
	t.Helper()
	dir := t.TempDir()
	vscodeDir := filepath.Join(dir, ".vscode")
	os.MkdirAll(vscodeDir, 0755)
	path := filepath.Join(vscodeDir, "tasks.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path, dir
}

func TestParse_BasicTask(t *testing.T) {
	tasksPath, wsRoot := writeTempTasksJSON(t, `{
		"version": "2.0.0",
		"tasks": [
			{
				"label": "build",
				"type": "shell",
				"command": "go build ./..."
			}
		]
	}`)

	configs, err := Parse(tasksPath, wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 task, got %d", len(configs))
	}
	if configs[0].Label != "build" {
		t.Errorf("label = %q, want %q", configs[0].Label, "build")
	}
	if configs[0].Command != "go build ./..." {
		t.Errorf("command = %q, want %q", configs[0].Command, "go build ./...")
	}
	if configs[0].Type != "shell" {
		t.Errorf("type = %q, want %q", configs[0].Type, "shell")
	}
}

func TestParse_DefaultType(t *testing.T) {
	tasksPath, wsRoot := writeTempTasksJSON(t, `{
		"version": "2.0.0",
		"tasks": [{"label": "test", "command": "echo hi"}]
	}`)

	configs, err := Parse(tasksPath, wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if configs[0].Type != "shell" {
		t.Errorf("default type should be 'shell', got %q", configs[0].Type)
	}
}

func TestParse_DefaultDependsOrder(t *testing.T) {
	tasksPath, wsRoot := writeTempTasksJSON(t, `{
		"version": "2.0.0",
		"tasks": [{"label": "test", "command": "echo"}]
	}`)

	configs, err := Parse(tasksPath, wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if configs[0].DependsOrder != "parallel" {
		t.Errorf("default dependsOrder should be 'parallel', got %q", configs[0].DependsOrder)
	}
}

func TestParse_VariableResolution(t *testing.T) {
	tasksPath, wsRoot := writeTempTasksJSON(t, `{
		"version": "2.0.0",
		"tasks": [
			{
				"label": "run",
				"command": "${workspaceFolder}/bin/app",
				"args": ["--config", "${workspaceFolder}/config.json"]
			}
		]
	}`)

	configs, err := Parse(tasksPath, wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	wantCmd := filepath.Join(wsRoot, "bin", "app")
	// Normalize path separators for comparison
	gotCmd := filepath.FromSlash(configs[0].Command)
	wantCmd = filepath.FromSlash(wantCmd)
	if gotCmd != wantCmd {
		t.Errorf("command = %q, want %q", gotCmd, wantCmd)
	}
	wantArg := filepath.Join(wsRoot, "config.json")
	gotArg := filepath.FromSlash(configs[0].Args[1])
	wantArg = filepath.FromSlash(wantArg)
	if gotArg != wantArg {
		t.Errorf("arg[1] = %q, want %q", gotArg, wantArg)
	}
}

func TestParse_EnvMerging(t *testing.T) {
	tasksPath, wsRoot := writeTempTasksJSON(t, `{
		"version": "2.0.0",
		"options": {
			"env": {
				"GLOBAL_VAR": "global_value",
				"OVERRIDE": "global"
			}
		},
		"tasks": [
			{
				"label": "test",
				"command": "echo",
				"options": {
					"env": {
						"TASK_VAR": "task_value",
						"OVERRIDE": "task"
					}
				}
			}
		]
	}`)

	configs, err := Parse(tasksPath, wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	env := configs[0].Env

	if env["GLOBAL_VAR"] != "global_value" {
		t.Errorf("GLOBAL_VAR = %q, want %q", env["GLOBAL_VAR"], "global_value")
	}
	if env["TASK_VAR"] != "task_value" {
		t.Errorf("TASK_VAR = %q, want %q", env["TASK_VAR"], "task_value")
	}
	// Task-level should override global
	if env["OVERRIDE"] != "task" {
		t.Errorf("OVERRIDE = %q, want %q (task should override global)", env["OVERRIDE"], "task")
	}
}

func TestParse_CwdResolution(t *testing.T) {
	tasksPath, wsRoot := writeTempTasksJSON(t, `{
		"version": "2.0.0",
		"tasks": [
			{
				"label": "no-cwd",
				"command": "echo"
			},
			{
				"label": "relative-cwd",
				"command": "echo",
				"options": {"cwd": "subdir"}
			},
			{
				"label": "variable-cwd",
				"command": "echo",
				"options": {"cwd": "${workspaceFolder}/frontend"}
			}
		]
	}`)

	configs, err := Parse(tasksPath, wsRoot)
	if err != nil {
		t.Fatal(err)
	}

	// No cwd → defaults to workspace root
	if configs[0].Cwd != wsRoot {
		t.Errorf("no-cwd: got %q, want %q", configs[0].Cwd, wsRoot)
	}

	// Relative cwd → resolved against workspace root
	wantRelative := filepath.Join(wsRoot, "subdir")
	if configs[1].Cwd != wantRelative {
		t.Errorf("relative-cwd: got %q, want %q", configs[1].Cwd, wantRelative)
	}

	// Variable cwd → resolved
	wantVariable := wsRoot + string(os.PathSeparator) + "frontend"
	// Handle the fact that resolveVariables uses / which filepath.Join may normalize
	if filepath.Clean(configs[2].Cwd) != filepath.Clean(wantVariable) {
		t.Errorf("variable-cwd: got %q, want %q", configs[2].Cwd, wantVariable)
	}
}

func TestParse_GlobalCwd(t *testing.T) {
	tasksPath, wsRoot := writeTempTasksJSON(t, `{
		"version": "2.0.0",
		"options": {
			"cwd": "${workspaceFolder}/global-dir"
		},
		"tasks": [
			{
				"label": "uses-global",
				"command": "echo"
			},
			{
				"label": "overrides",
				"command": "echo",
				"options": {"cwd": "${workspaceFolder}/task-dir"}
			}
		]
	}`)

	configs, err := Parse(tasksPath, wsRoot)
	if err != nil {
		t.Fatal(err)
	}

	// Task without its own cwd should use global cwd
	wantGlobal := filepath.Clean(wsRoot + "/global-dir")
	if filepath.Clean(configs[0].Cwd) != wantGlobal {
		t.Errorf("uses-global: got %q, want %q", configs[0].Cwd, wantGlobal)
	}

	// Task with its own cwd should override global
	wantOverride := filepath.Clean(wsRoot + "/task-dir")
	if filepath.Clean(configs[1].Cwd) != wantOverride {
		t.Errorf("overrides: got %q, want %q", configs[1].Cwd, wantOverride)
	}
}

func TestParse_GroupString(t *testing.T) {
	tasksPath, wsRoot := writeTempTasksJSON(t, `{
		"version": "2.0.0",
		"tasks": [{"label": "test", "command": "echo", "group": "build"}]
	}`)

	configs, err := Parse(tasksPath, wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if configs[0].Group != "build" {
		t.Errorf("group = %q, want %q", configs[0].Group, "build")
	}
}

func TestParse_GroupObject(t *testing.T) {
	tasksPath, wsRoot := writeTempTasksJSON(t, `{
		"version": "2.0.0",
		"tasks": [{
			"label": "test",
			"command": "echo",
			"group": {"kind": "test", "isDefault": true}
		}]
	}`)

	configs, err := Parse(tasksPath, wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if configs[0].Group != "test" {
		t.Errorf("group = %q, want %q", configs[0].Group, "test")
	}
}

func TestParse_DependsOnString(t *testing.T) {
	tasksPath, wsRoot := writeTempTasksJSON(t, `{
		"version": "2.0.0",
		"tasks": [
			{"label": "a", "command": "echo a"},
			{"label": "b", "command": "echo b", "dependsOn": "a"}
		]
	}`)

	configs, err := Parse(tasksPath, wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(configs[1].DependsOn) != 1 || configs[1].DependsOn[0] != "a" {
		t.Errorf("dependsOn = %v, want [a]", configs[1].DependsOn)
	}
}

func TestParse_DependsOnArray(t *testing.T) {
	tasksPath, wsRoot := writeTempTasksJSON(t, `{
		"version": "2.0.0",
		"tasks": [
			{"label": "a", "command": "echo a"},
			{"label": "b", "command": "echo b"},
			{"label": "all", "command": "", "dependsOn": ["a", "b"]}
		]
	}`)

	configs, err := Parse(tasksPath, wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	deps := configs[2].DependsOn
	if len(deps) != 2 || deps[0] != "a" || deps[1] != "b" {
		t.Errorf("dependsOn = %v, want [a, b]", deps)
	}
}

func TestParse_JSONC_FullFile(t *testing.T) {
	tasksPath, wsRoot := writeTempTasksJSON(t, `{
		// This file has comments everywhere
		"version": "2.0.0",
		"tasks": [
			{
				"label": "build", // inline comment
				"type": "shell",
				"command": "go build ./...",
			}, // trailing comma
		],
	}`)

	configs, err := Parse(tasksPath, wsRoot)
	if err != nil {
		t.Fatalf("failed to parse JSONC: %v", err)
	}
	if len(configs) != 1 || configs[0].Label != "build" {
		t.Errorf("unexpected parse result: %+v", configs)
	}
}

// --- TASKR_* env overrides tests ---

func TestParseTaskrEnvOverrides_WatchTrue(t *testing.T) {
	tc := config.TaskConfig{
		Env: map[string]string{
			"TASKR_WATCH": "true",
			"OTHER":       "value",
		},
	}
	parseTaskrEnvOverrides(&tc)

	if tc.WatchMode != config.WatchSelf {
		t.Errorf("WatchMode = %q, want %q", tc.WatchMode, config.WatchSelf)
	}
	if !tc.WatchEnabled {
		t.Error("WatchEnabled should be true")
	}
	if _, ok := tc.Env["TASKR_WATCH"]; ok {
		t.Error("TASKR_WATCH should be removed from env")
	}
	if tc.Env["OTHER"] != "value" {
		t.Error("non-TASKR env should be preserved")
	}
}

func TestParseTaskrEnvOverrides_WatchFalse(t *testing.T) {
	tc := config.TaskConfig{
		Env: map[string]string{"TASKR_WATCH": "false"},
	}
	parseTaskrEnvOverrides(&tc)

	if tc.WatchMode != config.WatchNone {
		t.Errorf("WatchMode = %q, want %q", tc.WatchMode, config.WatchNone)
	}
	if tc.WatchEnabled {
		t.Error("WatchEnabled should be false")
	}
}

func TestParseTaskrEnvOverrides_Extensions(t *testing.T) {
	tc := config.TaskConfig{
		Env: map[string]string{"TASKR_WATCH_EXTENSIONS": ".cs, .razor, json"},
	}
	parseTaskrEnvOverrides(&tc)

	want := []string{".cs", ".razor", ".json"}
	if len(tc.WatchExtensions) != len(want) {
		t.Fatalf("WatchExtensions = %v, want %v", tc.WatchExtensions, want)
	}
	for i, ext := range tc.WatchExtensions {
		if ext != want[i] {
			t.Errorf("WatchExtensions[%d] = %q, want %q", i, ext, want[i])
		}
	}
	if _, ok := tc.Env["TASKR_WATCH_EXTENSIONS"]; ok {
		t.Error("TASKR_WATCH_EXTENSIONS should be removed from env")
	}
}

func TestParseTaskrEnvOverrides_Paths(t *testing.T) {
	tc := config.TaskConfig{
		Env: map[string]string{"TASKR_WATCH_PATHS": "src/, config/, scripts"},
	}
	parseTaskrEnvOverrides(&tc)

	want := []string{"src/", "config/", "scripts"}
	if len(tc.WatchPaths) != len(want) {
		t.Fatalf("WatchPaths = %v, want %v", tc.WatchPaths, want)
	}
	for i, p := range tc.WatchPaths {
		if p != want[i] {
			t.Errorf("WatchPaths[%d] = %q, want %q", i, p, want[i])
		}
	}
}

// --- ResolveDependencies tests ---

func TestResolveDependencies_SingleTask(t *testing.T) {
	tasks := []config.TaskConfig{
		{Label: "build", Command: "go build"},
	}
	result, err := ResolveDependencies("build", tasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Label != "build" {
		t.Errorf("got %v, want [build]", labelList(result))
	}
}

func TestResolveDependencies_ChainedDeps(t *testing.T) {
	tasks := []config.TaskConfig{
		{Label: "compile", Command: "go build"},
		{Label: "lint", Command: "golint"},
		{Label: "all", DependsOn: []string{"compile", "lint"}},
	}
	result, err := ResolveDependencies("all", tasks)
	if err != nil {
		t.Fatal(err)
	}
	// Empty-command compound task "all" is skipped, only concrete deps returned
	labels := labelList(result)
	if len(labels) != 2 {
		t.Fatalf("expected 2 tasks (compound skipped), got %d: %v", len(labels), labels)
	}
	if indexOf(labels, "compile") == -1 || indexOf(labels, "lint") == -1 {
		t.Errorf("expected [compile, lint], got %v", labels)
	}
}

func TestResolveDependencies_DeepChain(t *testing.T) {
	tasks := []config.TaskConfig{
		{Label: "a", Command: "echo a"},
		{Label: "b", Command: "echo b", DependsOn: []string{"a"}},
		{Label: "c", Command: "echo c", DependsOn: []string{"b"}},
	}
	result, err := ResolveDependencies("c", tasks)
	if err != nil {
		t.Fatal(err)
	}
	labels := labelList(result)
	if len(labels) != 3 {
		t.Fatalf("expected 3 tasks, got %v", labels)
	}
	// Must be a→b→c order
	if labels[0] != "a" || labels[1] != "b" || labels[2] != "c" {
		t.Errorf("expected [a, b, c], got %v", labels)
	}
}

func TestResolveDependencies_TaskNotFound(t *testing.T) {
	tasks := []config.TaskConfig{
		{Label: "a", Command: "echo"},
	}
	_, err := ResolveDependencies("nonexistent", tasks)
	if err == nil {
		t.Error("expected error for missing task")
	}
}

func TestResolveDependencies_MissingDep(t *testing.T) {
	tasks := []config.TaskConfig{
		{Label: "a", DependsOn: []string{"missing"}},
	}
	_, err := ResolveDependencies("a", tasks)
	if err == nil {
		t.Error("expected error for missing dependency")
	}
}

func TestResolveDependencies_NoDuplicates(t *testing.T) {
	// Diamond dependency: c depends on a and b, both depend on base
	tasks := []config.TaskConfig{
		{Label: "base", Command: "echo base"},
		{Label: "a", Command: "echo a", DependsOn: []string{"base"}},
		{Label: "b", Command: "echo b", DependsOn: []string{"base"}},
		{Label: "c", Command: "echo c", DependsOn: []string{"a", "b"}},
	}
	result, err := ResolveDependencies("c", tasks)
	if err != nil {
		t.Fatal(err)
	}
	labels := labelList(result)
	// "base" should only appear once
	count := 0
	for _, l := range labels {
		if l == "base" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("base appeared %d times (should be 1): %v", count, labels)
	}
}

// --- FindTasksJSON tests ---

func TestFindTasksJSON_Found(t *testing.T) {
	dir := t.TempDir()
	vscodeDir := filepath.Join(dir, ".vscode")
	os.MkdirAll(vscodeDir, 0755)
	os.WriteFile(filepath.Join(vscodeDir, "tasks.json"), []byte(`{}`), 0644)

	subDir := filepath.Join(dir, "src", "deep")
	os.MkdirAll(subDir, 0755)

	path, wsRoot, err := FindTasksJSON(subDir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(wsRoot) != filepath.Clean(dir) {
		t.Errorf("wsRoot = %q, want %q", wsRoot, dir)
	}
	if filepath.Base(filepath.Dir(path)) != ".vscode" {
		t.Errorf("path should be in .vscode dir, got %q", path)
	}
}

func TestFindTasksJSON_NotFound(t *testing.T) {
	dir := t.TempDir() // No .vscode/tasks.json
	_, _, err := FindTasksJSON(dir)
	if err == nil {
		t.Error("expected error when tasks.json not found")
	}
}

// --- Parse integration with JSONC, env, overrides ---

func TestParse_FullIntegration(t *testing.T) {
	tasksPath, wsRoot := writeTempTasksJSON(t, `{
		// Full integration test
		"version": "2.0.0",
		"options": {
			"env": {"APP_ENV": "dev"},
			"cwd": "${workspaceFolder}"
		},
		"tasks": [
			{
				"label": "api",
				"type": "shell",
				"command": "go run ./cmd/api",
				"args": ["--port", "8080"],
				"isBackground": true,
				"group": {"kind": "build", "isDefault": true},
				"options": {
					"cwd": "${workspaceFolder}/backend",
					"env": {
						"PORT": "8080",
						"TASKR_WATCH": "true",
						"TASKR_WATCH_EXTENSIONS": ".go,.html,.tmpl",
					}
				}
			},
			{
				"label": "web",
				"command": "npm run dev",
				"options": {
					"cwd": "${workspaceFolder}/frontend",
				}
			},
			{
				"label": "dev",
				"command": "",
				"dependsOn": ["api", "web"],
				"dependsOrder": "parallel",
			},
		]
	}`)

	configs, err := Parse(tasksPath, wsRoot)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(configs) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(configs))
	}

	api := configs[0]

	// Label and type
	if api.Label != "api" || api.Type != "shell" {
		t.Errorf("api: label=%q type=%q", api.Label, api.Type)
	}

	// Command with variable resolution
	if api.Command != "go run ./cmd/api" {
		t.Errorf("api.Command = %q", api.Command)
	}

	// Args
	if len(api.Args) != 2 || api.Args[0] != "--port" || api.Args[1] != "8080" {
		t.Errorf("api.Args = %v", api.Args)
	}

	// IsBackground
	if !api.IsBackground {
		t.Error("api should be background")
	}

	// Group from object
	if api.Group != "build" {
		t.Errorf("api.Group = %q, want build", api.Group)
	}

	// Cwd resolved
	wantCwd := filepath.Clean(wsRoot + "/backend")
	if filepath.Clean(api.Cwd) != wantCwd {
		t.Errorf("api.Cwd = %q, want %q", api.Cwd, wantCwd)
	}

	// Env: global APP_ENV should be present, task PORT should be present
	if api.Env["APP_ENV"] != "dev" {
		t.Errorf("api.Env[APP_ENV] = %q, want dev", api.Env["APP_ENV"])
	}
	if api.Env["PORT"] != "8080" {
		t.Errorf("api.Env[PORT] = %q, want 8080", api.Env["PORT"])
	}

	// TASKR_WATCH should be consumed (removed from env)
	if _, ok := api.Env["TASKR_WATCH"]; ok {
		t.Error("TASKR_WATCH should be consumed from env")
	}
	if api.WatchMode != config.WatchSelf {
		t.Errorf("api.WatchMode = %q, want self", api.WatchMode)
	}
	if !api.WatchEnabled {
		t.Error("api.WatchEnabled should be true")
	}

	// TASKR_WATCH_EXTENSIONS should be consumed
	if _, ok := api.Env["TASKR_WATCH_EXTENSIONS"]; ok {
		t.Error("TASKR_WATCH_EXTENSIONS should be consumed from env")
	}
	wantExts := []string{".go", ".html", ".tmpl"}
	if len(api.WatchExtensions) != len(wantExts) {
		t.Fatalf("api.WatchExtensions = %v, want %v", api.WatchExtensions, wantExts)
	}
	for i, e := range api.WatchExtensions {
		if e != wantExts[i] {
			t.Errorf("WatchExtensions[%d] = %q, want %q", i, e, wantExts[i])
		}
	}

	// web task — should default type/dependsOrder
	web := configs[1]
	if web.Type != "shell" {
		t.Errorf("web.Type = %q, want shell", web.Type)
	}

	// dev task — compound with dependsOn
	dev := configs[2]
	if len(dev.DependsOn) != 2 {
		t.Errorf("dev.DependsOn = %v, want [api, web]", dev.DependsOn)
	}
	if dev.DependsOrder != "parallel" {
		t.Errorf("dev.DependsOrder = %q, want parallel", dev.DependsOrder)
	}

	// Test ResolveDependencies on the compound task ("dev" has empty command, gets skipped)
	resolved, err := ResolveDependencies("dev", configs)
	if err != nil {
		t.Fatal(err)
	}
	resolvedLabels := labelList(resolved)
	if len(resolvedLabels) != 2 {
		t.Errorf("resolved %v, expected 2 tasks (compound 'dev' skipped)", resolvedLabels)
	}
	// Should contain api and web
	if indexOf(resolvedLabels, "api") == -1 || indexOf(resolvedLabels, "web") == -1 {
		t.Errorf("expected [api, web], got %v", resolvedLabels)
	}
}

func TestParse_EmptyTasksFile(t *testing.T) {
	path, ws := writeTempTasksJSON(t, `{"version": "2.0.0", "tasks": []}`)
	configs, err := Parse(path, ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(configs))
	}
}

func TestParse_InvalidJSON(t *testing.T) {
	path, ws := writeTempTasksJSON(t, `{not valid json at all}`)
	_, err := Parse(path, ws)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParse_WindowsPathSeparator(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}
	tasksPath, wsRoot := writeTempTasksJSON(t, `{
		"version": "2.0.0",
		"tasks": [{
			"label": "test",
			"command": "echo",
			"options": {"cwd": "${workspaceFolder}\\subdir"}
		}]
	}`)
	configs, err := Parse(tasksPath, wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	// Should resolve to a valid Windows path
	if !filepath.IsAbs(configs[0].Cwd) {
		t.Errorf("cwd should be absolute, got %q", configs[0].Cwd)
	}
}

// --- TASKR_HIDE tests ---

func TestParseTaskrEnvOverrides_HideTrue(t *testing.T) {
	tc := config.TaskConfig{
		Env: map[string]string{
			"TASKR_HIDE": "true",
			"OTHER":      "value",
		},
	}
	parseTaskrEnvOverrides(&tc)

	if !tc.HideLogs {
		t.Error("HideLogs should be true")
	}
	if _, ok := tc.Env["TASKR_HIDE"]; ok {
		t.Error("TASKR_HIDE should be removed from env")
	}
	if tc.Env["OTHER"] != "value" {
		t.Error("non-TASKR env should be preserved")
	}
}

func TestParseTaskrEnvOverrides_HideFalse(t *testing.T) {
	tc := config.TaskConfig{
		HideLogs: true, // start as true
		Env:      map[string]string{"TASKR_HIDE": "false"},
	}
	parseTaskrEnvOverrides(&tc)

	if tc.HideLogs {
		t.Error("HideLogs should be false")
	}
	if _, ok := tc.Env["TASKR_HIDE"]; ok {
		t.Error("TASKR_HIDE should be removed from env")
	}
}

func TestParseTaskrEnvOverrides_HideYes(t *testing.T) {
	tc := config.TaskConfig{
		Env: map[string]string{"TASKR_HIDE": "yes"},
	}
	parseTaskrEnvOverrides(&tc)

	if !tc.HideLogs {
		t.Error("HideLogs should be true for 'yes'")
	}
}

func TestParse_TaskrHideIntegration(t *testing.T) {
	tasksPath, wsRoot := writeTempTasksJSON(t, `{
		"version": "2.0.0",
		"tasks": [
			{
				"label": "noisy-task",
				"command": "echo spam",
				"options": {
					"env": { "TASKR_HIDE": "true" }
				}
			},
			{
				"label": "visible-task",
				"command": "echo hello"
			}
		]
	}`)

	configs, err := Parse(tasksPath, wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(configs))
	}
	if !configs[0].HideLogs {
		t.Error("noisy-task should have HideLogs=true")
	}
	if configs[1].HideLogs {
		t.Error("visible-task should have HideLogs=false")
	}
	// TASKR_HIDE should be consumed from env
	if _, ok := configs[0].Env["TASKR_HIDE"]; ok {
		t.Error("TASKR_HIDE should be consumed from env")
	}
}

// --- PrefixTasks tests ---

func TestPrefixTasks_Empty(t *testing.T) {
	tasks := []config.TaskConfig{
		{Label: "build", Command: "go build"},
	}
	PrefixTasks(tasks, "")
	if tasks[0].Label != "build" {
		t.Errorf("empty prefix should not change label, got %q", tasks[0].Label)
	}
}

func TestPrefixTasks_WithPrefix(t *testing.T) {
	tasks := []config.TaskConfig{
		{Label: "build", Command: "go build", DependsOn: []string{"lint"}},
		{Label: "lint", Command: "golint"},
	}
	PrefixTasks(tasks, "frontend")

	if tasks[0].Label != "frontend/build" {
		t.Errorf("label = %q, want %q", tasks[0].Label, "frontend/build")
	}
	if tasks[1].Label != "frontend/lint" {
		t.Errorf("label = %q, want %q", tasks[1].Label, "frontend/lint")
	}
	if tasks[0].DependsOn[0] != "frontend/lint" {
		t.Errorf("dependsOn = %q, want %q", tasks[0].DependsOn[0], "frontend/lint")
	}
}

func TestPrefixTasks_NestedPrefix(t *testing.T) {
	tasks := []config.TaskConfig{
		{Label: "serve", Command: "npm start"},
	}
	PrefixTasks(tasks, "apps/web")
	if tasks[0].Label != "apps/web/serve" {
		t.Errorf("label = %q, want %q", tasks[0].Label, "apps/web/serve")
	}
}

// --- FindAllTasksJSON tests ---

func TestFindAllTasksJSON_RootOnly(t *testing.T) {
	dir := t.TempDir()
	vscodeDir := filepath.Join(dir, ".vscode")
	os.MkdirAll(vscodeDir, 0755)
	os.WriteFile(filepath.Join(vscodeDir, "tasks.json"), []byte(`{}`), 0644)

	results := FindAllTasksJSON(dir)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].RelPrefix != "" {
		t.Errorf("root should have empty prefix, got %q", results[0].RelPrefix)
	}
}

func TestFindAllTasksJSON_Nested(t *testing.T) {
	dir := t.TempDir()

	// Root tasks.json
	os.MkdirAll(filepath.Join(dir, ".vscode"), 0755)
	os.WriteFile(filepath.Join(dir, ".vscode", "tasks.json"), []byte(`{}`), 0644)

	// Nested: frontend/.vscode/tasks.json
	os.MkdirAll(filepath.Join(dir, "frontend", ".vscode"), 0755)
	os.WriteFile(filepath.Join(dir, "frontend", ".vscode", "tasks.json"), []byte(`{}`), 0644)

	// Nested: backend/api/.vscode/tasks.json
	os.MkdirAll(filepath.Join(dir, "backend", "api", ".vscode"), 0755)
	os.WriteFile(filepath.Join(dir, "backend", "api", ".vscode", "tasks.json"), []byte(`{}`), 0644)

	results := FindAllTasksJSON(dir)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d: %+v", len(results), results)
	}

	// Check prefixes
	prefixes := map[string]bool{}
	for _, r := range results {
		prefixes[r.RelPrefix] = true
	}
	if !prefixes[""] {
		t.Error("missing root prefix (empty)")
	}
	if !prefixes["frontend"] {
		t.Error("missing 'frontend' prefix")
	}
	if !prefixes["backend/api"] {
		t.Error("missing 'backend/api' prefix")
	}
}

func TestFindAllTasksJSON_SkipsIgnoredDirs(t *testing.T) {
	dir := t.TempDir()

	// Root tasks.json
	os.MkdirAll(filepath.Join(dir, ".vscode"), 0755)
	os.WriteFile(filepath.Join(dir, ".vscode", "tasks.json"), []byte(`{}`), 0644)

	// Should be skipped: node_modules/.vscode/tasks.json
	os.MkdirAll(filepath.Join(dir, "node_modules", "pkg", ".vscode"), 0755)
	os.WriteFile(filepath.Join(dir, "node_modules", "pkg", ".vscode", "tasks.json"), []byte(`{}`), 0644)

	// Should be skipped: .git-something/.vscode/tasks.json (hidden dir)
	os.MkdirAll(filepath.Join(dir, ".hidden", ".vscode"), 0755)
	os.WriteFile(filepath.Join(dir, ".hidden", ".vscode", "tasks.json"), []byte(`{}`), 0644)

	results := FindAllTasksJSON(dir)
	if len(results) != 1 {
		t.Fatalf("expected 1 result (only root), got %d: %+v", len(results), results)
	}
}

func TestFindAllTasksJSON_NoRoot(t *testing.T) {
	dir := t.TempDir()
	// No .vscode/tasks.json at all

	results := FindAllTasksJSON(dir)
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestFindAllTasksJSON_OnlyNested(t *testing.T) {
	dir := t.TempDir()
	// No root tasks.json, only nested
	os.MkdirAll(filepath.Join(dir, "sub", ".vscode"), 0755)
	os.WriteFile(filepath.Join(dir, "sub", ".vscode", "tasks.json"), []byte(`{}`), 0644)

	results := FindAllTasksJSON(dir)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].RelPrefix != "sub" {
		t.Errorf("prefix = %q, want %q", results[0].RelPrefix, "sub")
	}
}

// --- ResolveMultipleDependencies with prefixed tasks ---

func TestResolveMultipleDependencies_CrossFile(t *testing.T) {
	// Simulate tasks from two different tasks.json files merged together
	rootTasks := []config.TaskConfig{
		{Label: "api", Command: "go run ./api"},
	}
	nestedTasks := []config.TaskConfig{
		{Label: "lint", Command: "eslint ."},
		{Label: "dev", Command: "npm run dev", DependsOn: []string{"lint"}},
	}
	PrefixTasks(nestedTasks, "frontend")

	allTasks := append(rootTasks, nestedTasks...)

	// Resolve a nested task with dependencies
	result, err := ResolveMultipleDependencies([]string{"api", "frontend/dev"}, allTasks)
	if err != nil {
		t.Fatal(err)
	}
	labels := labelList(result)
	if indexOf(labels, "api") == -1 {
		t.Error("missing 'api' in resolved tasks")
	}
	if indexOf(labels, "frontend/lint") == -1 {
		t.Error("missing 'frontend/lint' in resolved tasks")
	}
	if indexOf(labels, "frontend/dev") == -1 {
		t.Error("missing 'frontend/dev' in resolved tasks")
	}
}

// --- helpers ---

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func labelList(tasks []config.TaskConfig) []string {
	var labels []string
	for _, t := range tasks {
		labels = append(labels, t.Label)
	}
	return labels
}

func indexOf(s []string, val string) int {
	for i, v := range s {
		if v == val {
			return i
		}
	}
	return -1
}
