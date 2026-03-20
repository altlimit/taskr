import * as vscode from 'vscode';
import * as path from 'path';
import * as fs from 'fs';
import * as os from 'os';
import * as https from 'https';
import { execSync, exec } from 'child_process';

const GITHUB_REPO = 'altlimit/taskr';
const BINARY_NAME = process.platform === 'win32' ? 'taskr.exe' : 'taskr';

interface TaskEntry {
    label: string;
    command?: string;
    dependsOn?: string | string[];
}

interface TasksFile {
    version: string;
    tasks: TaskEntry[];
}

export function activate(context: vscode.ExtensionContext) {
    const command = vscode.commands.registerCommand('taskr.run', async () => {
        const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
        if (!workspaceFolder) {
            vscode.window.showErrorMessage('TaskR: No workspace folder open');
            return;
        }

        // 1. Read tasks.json to get labels for the picker
        const tasksPath = path.join(workspaceFolder.uri.fsPath, '.vscode', 'tasks.json');
        if (!fs.existsSync(tasksPath)) {
            vscode.window.showErrorMessage('TaskR: No .vscode/tasks.json found in workspace');
            return;
        }

        const tasks = readTasksJson(tasksPath);
        if (!tasks || tasks.length === 0) {
            vscode.window.showErrorMessage('TaskR: No tasks found in tasks.json');
            return;
        }

        // 2. Show multi-select QuickPick
        const items: vscode.QuickPickItem[] = tasks.map(t => ({
            label: t.label,
            description: t.dependsOn
                ? `(${Array.isArray(t.dependsOn) ? t.dependsOn.length : 1} tasks)`
                : t.command || '',
            picked: false,
        }));

        let selectedLabels: string[];

        const selected = await vscode.window.showQuickPick(items, {
            canPickMany: true,
            placeHolder: 'Space to check tasks, Enter to confirm (or Enter with none checked to run highlighted)',
            title: 'TaskR: Run Tasks',
        });

        if (!selected) {
            return;
        }

        if (selected.length === 0) {
            // Nothing was checked — fall back to single-select
            const single = await vscode.window.showQuickPick(items, {
                canPickMany: false,
                placeHolder: 'Select a task to run',
                title: 'TaskR: Run Task',
            });
            if (!single) {
                return;
            }
            selectedLabels = [single.label];
        } else {
            selectedLabels = selected.map(s => s.label);
        }

        // 3. Find or install the taskr binary
        const binaryPath = await resolveBinary(context);
        if (!binaryPath) {
            return; // Error already shown
        }

        const cwd = workspaceFolder.uri.fsPath;

        // 4. Launch — integrated or external terminal based on setting
        const terminalPref = vscode.workspace.getConfiguration('taskr').get<string>('terminal', 'integrated');
        if (terminalPref === 'external') {
            launchExternal(binaryPath, selectedLabels, cwd);
        } else {
            // Integrated terminal: use single-quoted args so PowerShell handles
            // labels with spaces, parentheses, colons etc. without parse errors.
            const psArgs = selectedLabels.map(l => `'${l.replace(/'/g, "''")}'`).join(' ');
            const terminal = vscode.window.createTerminal({
                name: `TaskR: ${selectedLabels.join(', ')}`,
                cwd,
            });
            terminal.show();
            terminal.sendText(`& '${binaryPath.replace(/'/g, "''")}' ${psArgs}`);
        }
    });

    context.subscriptions.push(command);
}

/**
 * Open an external terminal window and run the given command string in it.
 * Tries the most capable terminal available on each platform.
 */
function launchExternal(binaryPath: string, labels: string[], cwd: string): void {
    // Build a PowerShell-safe command using single quotes (handles spaces,
    // parentheses, colons in label names without parse errors).
    const psBin  = binaryPath.replace(/'/g, "''");
    const psArgs = labels.map(l => `'${l.replace(/'/g, "''")}'`).join(' ');
    const psCmd  = `& '${psBin}' ${psArgs}`;

    if (process.platform === 'win32') {
        const wtPath = findOnPath('wt');
        let spawnCmd: string;
        if (wtPath) {
            // Windows Terminal: pass the PS command as a quoted -Command argument
            spawnCmd = `wt -d "${cwd}" powershell -NoExit -Command "${psCmd.replace(/"/g, '\\"')}"`;
        } else {
            // Fallback: open a new PowerShell window
            spawnCmd = `start powershell -NoExit -Command "${psCmd.replace(/"/g, '\\"')}"`;
        }
        exec(spawnCmd, { cwd });
    } else if (process.platform === 'darwin') {
        const shCmd = `cd '${cwd}' && '${psBin}' ${labels.map(l => `'${l.replace(/'/g, "'\\''")}'`).join(' ')}`;
        const script = `tell application "Terminal" to do script "${shCmd}"`;
        exec(`osascript -e '${script}'`);
    } else {
        // Linux: try common terminal emulators in order of preference
        const shCmd = `cd '${cwd}' && '${psBin}' ${labels.map(l => `'${l.replace(/'/g, "'\\''")}'`).join(' ')}; exec bash`;
        const terminals = [
            { bin: 'gnome-terminal', args: ['--', 'bash', '-c', shCmd] },
            { bin: 'konsole',        args: ['-e', 'bash', '-c', shCmd] },
            { bin: 'xfce4-terminal', args: ['-e', `bash -c '${shCmd}'`] },
            { bin: 'xterm',          args: ['-e', 'bash', '-c', shCmd] },
        ];
        const found = terminals.find(t => findOnPath(t.bin));
        if (found) {
            const { spawn } = require('child_process');
            spawn(found.bin, found.args, { cwd, detached: true, stdio: 'ignore' }).unref();
        } else {
            vscode.window.showErrorMessage('TaskR: Could not find a terminal emulator (tried gnome-terminal, konsole, xfce4-terminal, xterm).');
        }
    }
}

/**
 * Read tasks.json, strip JSONC comments, and parse task labels.
 */
function readTasksJson(filePath: string): TaskEntry[] | null {
    try {
        const raw = fs.readFileSync(filePath, 'utf-8');
        const content = stripJsoncComments(raw);

        const parsed: TasksFile = JSON.parse(content);
        return parsed.tasks || [];
    } catch (err) {
        vscode.window.showErrorMessage(`TaskR: Failed to parse tasks.json: ${err}`);
        return null;
    }
}

/**
 * Strip JSONC comments (line and block) without touching // inside strings.
 */
function stripJsoncComments(text: string): string {
    let result = '';
    let i = 0;
    let inString = false;
    let escaped = false;

    while (i < text.length) {
        const ch = text[i];

        if (escaped) {
            result += ch;
            escaped = false;
            i++;
            continue;
        }

        if (inString) {
            if (ch === '\\') {
                escaped = true;
                result += ch;
            } else if (ch === '"') {
                inString = false;
                result += ch;
            } else {
                result += ch;
            }
            i++;
            continue;
        }

        // Not in a string
        if (ch === '"') {
            inString = true;
            result += ch;
            i++;
        } else if (ch === '/' && i + 1 < text.length && text[i + 1] === '/') {
            // Line comment — skip to end of line
            while (i < text.length && text[i] !== '\n') { i++; }
        } else if (ch === '/' && i + 1 < text.length && text[i + 1] === '*') {
            // Block comment — skip to */
            i += 2;
            while (i + 1 < text.length && !(text[i] === '*' && text[i + 1] === '/')) { i++; }
            i += 2; // skip */
        } else {
            result += ch;
            i++;
        }
    }

    // Strip trailing commas before } or ]
    result = result.replace(/,\s*([}\]])/g, '$1');
    return result;
}

/**
 * Resolve the taskr binary path using this priority:
 * 1. Check if `taskr` is already on PATH
 * 2. Check if Go is installed → `go install`
 * 3. Download pre-built binary from GitHub releases
 */
async function resolveBinary(context: vscode.ExtensionContext): Promise<string | null> {
    // 1. Check PATH
    const pathBinary = findOnPath(BINARY_NAME);
    if (pathBinary) {
        return pathBinary;
    }

    // 2. Check extension's local storage for a previously downloaded binary
    const storagePath = context.globalStorageUri.fsPath;
    const localBinary = path.join(storagePath, BINARY_NAME);
    if (fs.existsSync(localBinary)) {
        return localBinary;
    }

    // 3. Try `go install` if Go is available
    const goPath = findOnPath('go');
    if (goPath) {
        const choice = await vscode.window.showInformationMessage(
            'TaskR binary not found. Install via `go install`?',
            'Install with Go',
            'Download Binary',
            'Cancel'
        );

        if (choice === 'Install with Go') {
            return await goInstall();
        } else if (choice === 'Download Binary') {
            return await downloadBinary(storagePath);
        }
        return null;
    }

    // 4. No Go available — offer download
    const choice = await vscode.window.showInformationMessage(
        'TaskR binary not found and Go is not installed. Download pre-built binary?',
        'Download',
        'Cancel'
    );

    if (choice === 'Download') {
        return await downloadBinary(storagePath);
    }
    return null;
}

/**
 * Find an executable on the system PATH.
 */
function findOnPath(name: string): string | null {
    try {
        const cmd = process.platform === 'win32' ? `where ${name}` : `which ${name}`;
        const result = execSync(cmd, { encoding: 'utf-8', timeout: 5000 }).trim();
        // `where` on Windows can return multiple lines
        const firstLine = result.split('\n')[0].trim();
        if (firstLine && fs.existsSync(firstLine)) {
            return firstLine;
        }
    } catch {
        // Not found
    }
    return null;
}

/**
 * Install taskr via `go install`.
 */
async function goInstall(): Promise<string | null> {
    return vscode.window.withProgress(
        { location: vscode.ProgressLocation.Notification, title: 'Installing taskr...' },
        async () => {
            return new Promise((resolve) => {
                exec(
                    `go install github.com/${GITHUB_REPO}@latest`,
                    { timeout: 120000 },
                    (err) => {
                        if (err) {
                            vscode.window.showErrorMessage(`TaskR: go install failed: ${err.message}`);
                            resolve(null);
                            return;
                        }
                        // Find the installed binary in GOPATH/bin or GOBIN
                        const gobin = process.env.GOBIN || path.join(process.env.GOPATH || path.join(os.homedir(), 'go'), 'bin');
                        const installed = path.join(gobin, BINARY_NAME);
                        if (fs.existsSync(installed)) {
                            vscode.window.showInformationMessage('TaskR: Installed successfully!');
                            resolve(installed);
                        } else {
                            vscode.window.showErrorMessage('TaskR: go install completed but binary not found');
                            resolve(null);
                        }
                    }
                );
            });
        }
    );
}

/**
 * Download a pre-built binary from GitHub releases.
 */
async function downloadBinary(storagePath: string): Promise<string | null> {
    const platform = process.platform; // win32, darwin, linux
    const arch = process.arch;         // x64, arm64

    let osName: string;
    let archName: string;
    let ext = '';

    switch (platform) {
        case 'win32': osName = 'windows'; ext = '.exe'; break;
        case 'darwin': osName = 'darwin'; break;
        case 'linux': osName = 'linux'; break;
        default:
            vscode.window.showErrorMessage(`TaskR: Unsupported platform: ${platform}`);
            return null;
    }

    switch (arch) {
        case 'x64': archName = 'amd64'; break;
        case 'arm64': archName = 'arm64'; break;
        default:
            vscode.window.showErrorMessage(`TaskR: Unsupported architecture: ${arch}`);
            return null;
    }

    const assetName = `taskr_${osName}_${archName}${ext}`;
    const downloadUrl = `https://github.com/${GITHUB_REPO}/releases/latest/download/${assetName}`;

    return vscode.window.withProgress(
        { location: vscode.ProgressLocation.Notification, title: 'Downloading taskr...' },
        async () => {
            return new Promise((resolve) => {
                fs.mkdirSync(storagePath, { recursive: true });
                const destPath = path.join(storagePath, BINARY_NAME);

                downloadFile(downloadUrl, destPath, (err) => {
                    if (err) {
                        vscode.window.showErrorMessage(`TaskR: Download failed: ${err.message}`);
                        resolve(null);
                        return;
                    }
                    // Make executable on Unix
                    if (process.platform !== 'win32') {
                        fs.chmodSync(destPath, 0o755);
                    }
                    vscode.window.showInformationMessage('TaskR: Downloaded successfully!');
                    resolve(destPath);
                });
            });
        }
    );
}

/**
 * Download a file via HTTPS, following redirects.
 */
function downloadFile(url: string, dest: string, callback: (err: Error | null) => void) {
    const file = fs.createWriteStream(dest);
    const request = https.get(url, (response) => {
        // Follow redirects (GitHub uses 302)
        if (response.statusCode === 301 || response.statusCode === 302) {
            const redirectUrl = response.headers.location;
            if (redirectUrl) {
                file.close();
                fs.unlinkSync(dest);
                downloadFile(redirectUrl, dest, callback);
                return;
            }
        }

        if (response.statusCode !== 200) {
            file.close();
            fs.unlinkSync(dest);
            callback(new Error(`HTTP ${response.statusCode}`));
            return;
        }

        response.pipe(file);
        file.on('finish', () => {
            file.close();
            callback(null);
        });
    });

    request.on('error', (err) => {
        fs.unlink(dest, () => {});
        callback(err);
    });
}

export function deactivate() {}
