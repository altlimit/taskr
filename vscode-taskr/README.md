# TaskR for VS Code

Launch your `tasks.json` tasks in [TaskR](https://github.com/altlimit/taskr) — a concurrent task runner with colored logs, file watching, and an interactive TUI.

## Usage

1. **Open a project** with a `.vscode/tasks.json`
2. **Press `Ctrl+Shift+T`** (or run `TaskR: Run Tasks` from the Command Palette)
3. **Pick tasks** from the multi-select picker
4. TaskR launches in a VS Code terminal with all selected tasks running concurrently

> The TaskR CLI is automatically installed on first use via `go install`.

## Features

- **Multi-select**: Toggle multiple tasks with Space, confirm with Enter
- **Concurrent execution**: All selected tasks run in parallel with colored, interleaved logs
- **File watching**: Auto-restarts tasks on file changes (configurable via `TASKR_WATCH`)
- **JSONC support**: Reads `tasks.json` with comments

## Configuration

TaskR is configured through environment variables in your `tasks.json`:

```jsonc
{
  "label": "Dev Server",
  "type": "shell",
  "command": "npm run dev",
  "options": {
    "env": {
      "TASKR_WATCH": "true",              // Enable file watching
      "TASKR_WATCH_EXTENSIONS": ".ts,.js", // File types to watch
      "TASKR_WATCH_PATHS": "./src"         // Directories to watch
    }
  }
}
```

## Keybinding

| Shortcut | Action |
|---|---|
| `Ctrl+Shift+T` | Run TaskR |
