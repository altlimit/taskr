# taskr

A terminal-native task runner that reads your existing `.vscode/tasks.json` and gives you concurrent execution, colored labeled logs, an interactive TUI, smart file watching, and more.

## Features

- **Concurrent execution** — each task runs in its own goroutine
- **Colored labeled logs** — unified log stream with auto-assigned colors per task
- **Task switching** — filter to a single task's logs or view all interleaved
- **Search** — live-filter log lines with `/`
- **URL auto-capture** — detects `http://localhost:...` from output and pins them in a persistent bar
- **Smart file watching** — auto-detects `go run` (watches `.go` files), knows `vite`/`nodemon` have their own watcher
- **Watcher toggle** — enable/disable watching per task at runtime
- **Custom watch patterns** — configure via `TASKR_*` env vars in tasks.json (no VSCode lint errors)
- **Multi-select** — run multiple tasks from CLI or interactive picker
- **VSCode extension** — launch tasks from the Command Palette with `Ctrl+Shift+T`

## Install

### Go Install

```bash
go install github.com/altlimit/taskr@latest
```

### Download Binary

Grab the latest binary for your platform from [Releases](https://github.com/altlimit/taskr/releases).

### VSCode Extension

Install the **TaskR** extension from the [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=altlimit.taskr), or download the `.vsix` from Releases.

## Usage

```bash
# Interactive picker (multi-select with Space, confirm with Enter)
taskr

# Run a specific task
taskr api

# Run multiple tasks
taskr api web worker

# Run a compound task (resolves dependsOn)
taskr dev

# Options
taskr --config path/to/tasks.json   # Explicit config path
taskr --no-watch                     # Disable file watching
taskr --watch-debounce 500ms         # Custom debounce
taskr -v                             # Print version
```

## Keyboard Shortcuts

| Key | Action |
|---|---|
| `←` `→` | Navigate between task views |
| `Tab` | Cycle to next task |
| `a` | Show all tasks (unified view) |
| `1-9` | Jump to task by index |
| `r` | Restart focused task (or all on ALL view) |
| `R` | Restart all tasks |
| `s` | Stop focused task |
| `S` | Stop all tasks |
| `Space` | Toggle file watcher on/off for focused task |
| `f` | Toggle auto-follow (re-enable after scrolling) |
| `/` | Search/filter logs |
| `Esc` | Exit search |
| `c` | Clear log viewport |
| `q` / `Ctrl+C` | Quit (kills all tasks) |

## Smart Watch Mode

taskr auto-detects whether a command needs file watching:

| Command | Behavior |
|---|---|
| `go run`, `go build` | Watches `.go`, `.mod`, `.sum` files |
| `python`, `node` (raw) | Watches relevant extensions |
| `cargo run`, `dotnet run` | Watches `.rs`/`.cs` files |
| `vite`, `next dev`, `nodemon` | Skipped (has built-in HMR) |
| `npm run dev`, `yarn dev` | Skipped (assumed own watcher) |
| Everything else | No watching by default |

### Custom Watch Configuration

Override watch behavior using `TASKR_*` env vars in your tasks.json (VSCode won't lint-error on env vars):

```json
{
  "label": "my-server",
  "command": "dotnet run",
  "options": {
    "env": {
      "TASKR_WATCH": "true",
      "TASKR_WATCH_EXTENSIONS": ".cs,.razor,.json",
      "TASKR_WATCH_PATHS": "src/,config/"
    }
  }
}
```

| Variable | Description |
|---|---|
| `TASKR_WATCH` | Force `"true"` or `"false"`, overrides auto-detection |
| `TASKR_WATCH_EXTENSIONS` | Comma-separated file extensions to watch |
| `TASKR_WATCH_PATHS` | Comma-separated relative paths to watch |

## VSCode Extension

The extension adds a **TaskR: Run Tasks** command (`Ctrl+Shift+T`) that opens a multi-select picker for your tasks and launches taskr in the integrated terminal.

**Binary resolution order:**
1. `taskr` already on PATH → use it
2. Previously downloaded → use cached copy
3. Go installed → offers `go install github.com/altlimit/taskr@latest`
4. No Go → auto-downloads the platform binary from GitHub Releases

## Project Structure

```
├── main.go              # CLI entry point, flags, task picker
├── config/config.go     # Shared types
├── parser/parser.go     # tasks.json parser (JSONC, variables, env)
├── runner/runner.go     # Process lifecycle, log multiplexing, URL capture
├── watcher/watcher.go   # fsnotify watcher, auto-detection
├── tui/
│   ├── tui.go           # Bubble Tea TUI
│   └── colors.go        # Color palette
└── vscode-taskr/        # VSCode extension
    ├── package.json
    └── src/extension.ts
```

## Development

```bash
# Build
go build -o taskr.exe .

# Run tests
go test ./...

# Build extension
cd vscode-taskr && npm install && npx tsc -p ./

# Test extension locally (in VS Code)
# Open vscode-taskr/ folder, press F5
```

## License

[MIT](LICENSE)
