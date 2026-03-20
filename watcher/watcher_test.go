package watcher

import (
	"testing"

	"github.com/altlimit/taskr/config"
)

func TestDetectWatchMode_GoRun(t *testing.T) {
	tc := config.TaskConfig{Command: "go run ./cmd/server"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchSelf {
		t.Errorf("go run: WatchMode = %q, want self", tc.WatchMode)
	}
	if !tc.WatchEnabled {
		t.Error("go run: WatchEnabled should be true")
	}
	assertExtensions(t, tc.WatchExtensions, []string{".go", ".mod", ".sum"})
}

func TestDetectWatchMode_GoBuild(t *testing.T) {
	tc := config.TaskConfig{Command: "go build -o app ./..."}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchSelf {
		t.Errorf("go build: WatchMode = %q, want self", tc.WatchMode)
	}
}

func TestDetectWatchMode_GoRunWithArgs(t *testing.T) {
	tc := config.TaskConfig{Command: "go", Args: []string{"run", "./main.go"}}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchSelf {
		t.Errorf("go run (args): WatchMode = %q, want self", tc.WatchMode)
	}
}

func TestDetectWatchMode_Python(t *testing.T) {
	tc := config.TaskConfig{Command: "python app.py"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchSelf {
		t.Errorf("python: WatchMode = %q, want self", tc.WatchMode)
	}
	assertExtensions(t, tc.WatchExtensions, []string{".py"})
}

func TestDetectWatchMode_Python3(t *testing.T) {
	tc := config.TaskConfig{Command: "python3 manage.py runserver"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchSelf {
		t.Errorf("python3: WatchMode = %q, want self", tc.WatchMode)
	}
}

func TestDetectWatchMode_NodeScript(t *testing.T) {
	tc := config.TaskConfig{Command: "node server.js"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchSelf {
		t.Errorf("node: WatchMode = %q, want self", tc.WatchMode)
	}
	assertExtensions(t, tc.WatchExtensions, []string{".js", ".ts", ".json"})
}

func TestDetectWatchMode_DenoRun(t *testing.T) {
	tc := config.TaskConfig{Command: "deno run --allow-net server.ts"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchSelf {
		t.Errorf("deno run: WatchMode = %q, want self", tc.WatchMode)
	}
	assertExtensions(t, tc.WatchExtensions, []string{".ts", ".js", ".json"})
}

func TestDetectWatchMode_Ruby(t *testing.T) {
	tc := config.TaskConfig{Command: "ruby app.rb"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchSelf {
		t.Errorf("ruby: WatchMode = %q, want self", tc.WatchMode)
	}
	assertExtensions(t, tc.WatchExtensions, []string{".rb"})
}

func TestDetectWatchMode_CargoRun(t *testing.T) {
	tc := config.TaskConfig{Command: "cargo run"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchSelf {
		t.Errorf("cargo run: WatchMode = %q, want self", tc.WatchMode)
	}
	assertExtensions(t, tc.WatchExtensions, []string{".rs", ".toml"})
}

func TestDetectWatchMode_DotnetRun(t *testing.T) {
	tc := config.TaskConfig{Command: "dotnet run"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchSelf {
		t.Errorf("dotnet run: WatchMode = %q, want self", tc.WatchMode)
	}
	assertExtensions(t, tc.WatchExtensions, []string{".cs", ".csproj", ".json"})
}

// --- Builtin watcher commands (should NOT trigger self-watch) ---

func TestDetectWatchMode_Vite(t *testing.T) {
	tc := config.TaskConfig{Command: "vite"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchBuiltin {
		t.Errorf("vite: WatchMode = %q, want builtin", tc.WatchMode)
	}
	if tc.WatchEnabled {
		t.Error("vite: WatchEnabled should be false")
	}
}

func TestDetectWatchMode_ViteInPath(t *testing.T) {
	tc := config.TaskConfig{Command: "npx vite --host"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchBuiltin {
		t.Errorf("npx vite: WatchMode = %q, want builtin", tc.WatchMode)
	}
}

func TestDetectWatchMode_NextDev(t *testing.T) {
	tc := config.TaskConfig{Command: "next dev"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchBuiltin {
		t.Errorf("next dev: WatchMode = %q, want builtin", tc.WatchMode)
	}
}

func TestDetectWatchMode_NuxtDev(t *testing.T) {
	tc := config.TaskConfig{Command: "nuxt dev"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchBuiltin {
		t.Errorf("nuxt dev: WatchMode = %q, want builtin", tc.WatchMode)
	}
}

func TestDetectWatchMode_Nodemon(t *testing.T) {
	tc := config.TaskConfig{Command: "nodemon server.js"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchBuiltin {
		t.Errorf("nodemon: WatchMode = %q, want builtin", tc.WatchMode)
	}
}

func TestDetectWatchMode_TscWatch(t *testing.T) {
	tc := config.TaskConfig{Command: "tsc --watch"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchBuiltin {
		t.Errorf("tsc --watch: WatchMode = %q, want builtin", tc.WatchMode)
	}
}

func TestDetectWatchMode_CargoWatch(t *testing.T) {
	tc := config.TaskConfig{Command: "cargo watch -x run"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchBuiltin {
		t.Errorf("cargo watch: WatchMode = %q, want builtin", tc.WatchMode)
	}
}

func TestDetectWatchMode_NpmRunDev(t *testing.T) {
	tc := config.TaskConfig{Command: "npm run dev"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchBuiltin {
		t.Errorf("npm run dev: WatchMode = %q, want builtin", tc.WatchMode)
	}
}

func TestDetectWatchMode_YarnDev(t *testing.T) {
	tc := config.TaskConfig{Command: "yarn dev"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchBuiltin {
		t.Errorf("yarn dev: WatchMode = %q, want builtin", tc.WatchMode)
	}
}

func TestDetectWatchMode_PnpmDev(t *testing.T) {
	tc := config.TaskConfig{Command: "pnpm dev"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchBuiltin {
		t.Errorf("pnpm dev: WatchMode = %q, want builtin", tc.WatchMode)
	}
}

func TestDetectWatchMode_WebpackServe(t *testing.T) {
	tc := config.TaskConfig{Command: "webpack serve --mode development"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchBuiltin {
		t.Errorf("webpack serve: WatchMode = %q, want builtin", tc.WatchMode)
	}
}

func TestDetectWatchMode_NgServe(t *testing.T) {
	tc := config.TaskConfig{Command: "ng serve"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchBuiltin {
		t.Errorf("ng serve: WatchMode = %q, want builtin", tc.WatchMode)
	}
}

// --- No watch ---

func TestDetectWatchMode_UnknownCommand(t *testing.T) {
	tc := config.TaskConfig{Command: "echo hello world"}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchNone {
		t.Errorf("unknown: WatchMode = %q, want none", tc.WatchMode)
	}
	if tc.WatchEnabled {
		t.Error("unknown: WatchEnabled should be false")
	}
}

func TestDetectWatchMode_EmptyCommand(t *testing.T) {
	tc := config.TaskConfig{Command: ""}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchNone {
		t.Errorf("empty: WatchMode = %q, want none", tc.WatchMode)
	}
}

// --- TASKR_WATCH override respecting ---

func TestDetectWatchMode_ExplicitSelfOverride(t *testing.T) {
	tc := config.TaskConfig{
		Command:   "echo hello",
		WatchMode: config.WatchSelf, // Set by TASKR_WATCH=true in parser
	}
	DetectWatchMode(&tc)
	if tc.WatchMode != config.WatchSelf {
		t.Errorf("explicit self should be preserved, got %q", tc.WatchMode)
	}
	if !tc.WatchEnabled {
		t.Error("explicit self should set WatchEnabled=true")
	}
}

func TestDetectWatchMode_PreserveCustomExtensions(t *testing.T) {
	tc := config.TaskConfig{
		Command:         "go run ./main.go",
		WatchExtensions: []string{".go", ".html", ".tmpl"}, // Custom from TASKR_WATCH_EXTENSIONS
	}
	DetectWatchMode(&tc)
	// Should use custom extensions, not override with defaults
	assertExtensions(t, tc.WatchExtensions, []string{".go", ".html", ".tmpl"})
}

// --- Case insensitivity ---

func TestDetectWatchMode_CaseInsensitive(t *testing.T) {
	tests := []struct {
		cmd  string
		want config.WatchMode
	}{
		{"GO RUN ./main.go", config.WatchSelf},
		{"Go Build ./...", config.WatchSelf},
		{"VITE", config.WatchBuiltin},
		{"NPM RUN DEV", config.WatchBuiltin},
		{"Nodemon server.js", config.WatchBuiltin},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			tc := config.TaskConfig{Command: tt.cmd}
			DetectWatchMode(&tc)
			if tc.WatchMode != tt.want {
				t.Errorf("%q: WatchMode = %q, want %q", tt.cmd, tc.WatchMode, tt.want)
			}
		})
	}
}

// --- helpers ---

func assertExtensions(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("extensions = %v, want %v", got, want)
		return
	}
	for i, e := range got {
		if e != want[i] {
			t.Errorf("extensions[%d] = %q, want %q", i, e, want[i])
		}
	}
}
