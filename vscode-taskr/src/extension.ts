import * as vscode from 'vscode';
import * as path from 'path';
import * as fs from 'fs';
import * as os from 'os';
import * as https from 'https';
import { execSync, exec } from 'child_process';

const GITHUB_REPO = 'altlimit/taskr';
const BINARY_NAME = process.platform === 'win32' ? 'taskr.exe' : 'taskr';
const UPDATE_CHECK_INTERVAL_MS = 24 * 60 * 60 * 1000; // 24 hours
const LAST_UPDATE_CHECK_KEY = 'taskr.lastUpdateCheck';

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
    // Fire-and-forget update check on activation
    checkForUpdates(context);

    const command = vscode.commands.registerCommand('taskr.run', async () => {
        const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
        if (!workspaceFolder) {
            vscode.window.showErrorMessage('TaskR: No workspace folder open');
            return;
        }

        // 1. Discover all tasks.json files (root + nested)
        const wsRoot = workspaceFolder.uri.fsPath;
        const allFound = findAllTasksJson(wsRoot);

        if (allFound.length === 0) {
            vscode.window.showErrorMessage('TaskR: No .vscode/tasks.json found in workspace');
            return;
        }

        // Parse and merge tasks from all files
        const tasks: TaskEntry[] = [];
        for (const found of allFound) {
            const fileTasks = readTasksJson(found.tasksPath);
            if (!fileTasks) { continue; }
            for (const t of fileTasks) {
                if (found.relPrefix) {
                    t.label = `${found.relPrefix}/${t.label}`;
                    if (t.dependsOn) {
                        const deps = Array.isArray(t.dependsOn) ? t.dependsOn : [t.dependsOn];
                        t.dependsOn = deps.map(d => `${found.relPrefix}/${d}`);
                    }
                }
                tasks.push(t);
            }
        }

        if (tasks.length === 0) {
            vscode.window.showErrorMessage('TaskR: No tasks found in any tasks.json');
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
            const terminal = vscode.window.createTerminal({
                name: `TaskR: ${selectedLabels.join(', ')}`,
                cwd,
            });
            terminal.show();
            if (process.platform === 'win32') {
                // PowerShell: use & '...' call operator with ''-escaped single quotes
                const psArgs = selectedLabels.map(l => `'${l.replace(/'/g, "''")}'`).join(' ');
                terminal.sendText(`& '${binaryPath.replace(/'/g, "''")}' ${psArgs}`);
            } else {
                // bash/zsh (Linux, macOS, WSL Remote): call directly with POSIX quoting
                const shArgs = selectedLabels.map(l => `'${l.replace(/'/g, "'\\''")}'`).join(' ');
                terminal.sendText(`'${binaryPath.replace(/'/g, "'\\''")}' ${shArgs}`);
            }
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
        // Linux / WSL: if we're inside WSL or can't find a GUI terminal, fall
        // back to the VSCode integrated terminal (always available).
        const isWsl = !!process.env.WSL_DISTRO_NAME || !!process.env.WSLENV ||
            (() => {
                try { return fs.readFileSync('/proc/version', 'utf-8').toLowerCase().includes('microsoft'); }
                catch { return false; }
            })();

        const shCmd = `cd '${cwd}' && '${psBin}' ${labels.map(l => `'${l.replace(/'/g, "'\\''")}'`).join(' ')}; exec bash`;

        if (isWsl) {
            // From WSL we can invoke Windows executables (wt.exe, wsl.exe) directly
            // via the WSL interop layer. Two important constraints:
            //   1. Windows Terminal uses ';' as its own tab-separator, so we must
            //      NOT put any ';' in the wt.exe argument list — use a temp script.
            //   2. The WSL cwd (e.g. /home/user/...) becomes a UNC path when seen
            //      by Windows processes; never pass it as cwd — let the script cd.
            const { spawn } = require('child_process');

            // Write a temp script so there are no quoting or ';' issues in args.
            const tmpScript = `/tmp/taskr_run_${Date.now()}.sh`;
            const scriptLines = [
                // No shebang — bash is invoked explicitly with -l below.
                // Source ~/.bashrc explicitly: Debian login shells source ~/.profile
                // but typically NOT ~/.bashrc, so GOPATH/bin etc. would be missing.
                '[ -f "$HOME/.bashrc" ] && . "$HOME/.bashrc"',
                `cd '${cwd.replace(/'/g, "'\\''")}'`,
                `'${psBin.replace(/'/g, "'\\''")}' ${labels.map(l => `'${l.replace(/'/g, "'\\''")}'`).join(' ')}`,
                'exec bash',
            ].join('\n');
            fs.writeFileSync(tmpScript, scriptLines + '\n', { mode: 0o755 });

            // Use the current distro name so the terminal always opens in the
            // same distro the extension host is running in, not whatever is
            // currently set as the Windows default.
            const distroArgs = process.env.WSL_DISTRO_NAME
                ? ['-d', process.env.WSL_DISTRO_NAME]
                : [];

            const fallbackToIntegrated = () => {
                // Last resort: run in the VSCode integrated terminal
                const shArgs = labels.map(l => `'${l.replace(/'/g, "'\\''")}'`).join(' ');
                const terminal = vscode.window.createTerminal({ name: `TaskR: ${labels.join(', ')}`, cwd });
                terminal.show();
                terminal.sendText(`'${psBin.replace(/'/g, "'\\''")}' ${shArgs}`);
            };

            const child = spawn('wt.exe', ['wsl.exe', ...distroArgs, 'bash', '-li', tmpScript],
                { detached: true, stdio: 'ignore' });
            child.on('error', () => {
                // wt.exe not available — try a bare WSL console window
                const child2 = spawn('cmd.exe', ['/c', 'start', 'wsl.exe', ...distroArgs, 'bash', '-li', tmpScript],
                    { detached: true, stdio: 'ignore' });
                child2.on('error', fallbackToIntegrated);
                child2.unref();
            });
            child.unref();
        } else {
            const terminals = [
                { bin: 'gnome-terminal',      args: ['--', 'bash', '-c', shCmd] },
                { bin: 'konsole',             args: ['-e', 'bash', '-c', shCmd] },
                { bin: 'xfce4-terminal',      args: ['-e', `bash -c '${shCmd}'`] },
                { bin: 'xterm',               args: ['-e', 'bash', '-c', shCmd] },
                { bin: 'x-terminal-emulator', args: ['-e', `bash -c '${shCmd}'`] },
            ];
            const found = terminals.find(t => findOnPath(t.bin));
            if (found) {
                const { spawn } = require('child_process');
                spawn(found.bin, found.args, { cwd, detached: true, stdio: 'ignore' }).unref();
            } else {
                vscode.window.showErrorMessage(
                    'TaskR: Could not find a terminal emulator (tried gnome-terminal, konsole, xfce4-terminal, xterm, x-terminal-emulator).'
                );
            }
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
        vscode.window.showErrorMessage(`TaskR: Failed to parse ${filePath}: ${err}`);
        return null;
    }
}

interface FoundTasksJson {
    tasksPath: string;
    workspaceRoot: string;
    relPrefix: string; // empty for root, e.g. "frontend" or "backend/api"
}

/** Directories to skip when walking for nested tasks.json files. */
const IGNORED_DIRS = new Set([
    'node_modules', 'vendor', '__pycache__', '.next', '.nuxt',
    'dist', 'build', 'target', 'bin', 'obj',
]);

/**
 * Recursively discover all .vscode/tasks.json files from the workspace root.
 * Returns them with the root-level file first (empty prefix), then nested
 * files with their relative directory as the prefix.
 */
function findAllTasksJson(rootDir: string): FoundTasksJson[] {
    const results: FoundTasksJson[] = [];

    // Check root
    const rootTasksPath = path.join(rootDir, '.vscode', 'tasks.json');
    if (fs.existsSync(rootTasksPath)) {
        results.push({
            tasksPath: rootTasksPath,
            workspaceRoot: rootDir,
            relPrefix: '',
        });
    }

    // Walk subdirectories
    function walk(dir: string): void {
        let entries: fs.Dirent[];
        try {
            entries = fs.readdirSync(dir, { withFileTypes: true });
        } catch {
            return;
        }
        for (const entry of entries) {
            if (!entry.isDirectory()) { continue; }
            const name = entry.name;
            // Skip hidden dirs except .vscode
            if (name !== '.vscode' && name.startsWith('.')) { continue; }
            if (IGNORED_DIRS.has(name)) { continue; }

            const fullPath = path.join(dir, name);

            if (name === '.vscode') {
                const tasksFile = path.join(fullPath, 'tasks.json');
                if (fs.existsSync(tasksFile) && tasksFile !== rootTasksPath) {
                    const wsRoot = path.dirname(fullPath); // parent of .vscode
                    const rel = path.relative(rootDir, wsRoot).replace(/\\/g, '/');
                    results.push({
                        tasksPath: tasksFile,
                        workspaceRoot: wsRoot,
                        relPrefix: rel,
                    });
                }
            } else {
                walk(fullPath);
            }
        }
    }

    walk(rootDir);
    return results;
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

/**
 * Check for updated taskr binary in the background.
 * Throttled to once per UPDATE_CHECK_INTERVAL_MS.
 */
async function checkForUpdates(context: vscode.ExtensionContext): Promise<void> {
    try {
        const lastCheck = context.globalState.get<number>(LAST_UPDATE_CHECK_KEY, 0);
        if (Date.now() - lastCheck < UPDATE_CHECK_INTERVAL_MS) {
            return; // Checked recently, skip
        }

        // Find the currently installed binary to compare against
        const currentBinary = findInstalledBinary(context);
        if (!currentBinary) {
            return; // No binary installed yet — nothing to update
        }

        const localVersion = getLocalVersion(currentBinary);
        if (!localVersion) {
            return; // Can't determine local version
        }

        const latestTag = await fetchLatestReleaseTag();
        if (!latestTag) {
            return; // Network error or no releases
        }

        // Record that we checked, regardless of result
        await context.globalState.update(LAST_UPDATE_CHECK_KEY, Date.now());

        // Compare versions: strip leading 'v' from tag
        const latestVersion = latestTag.replace(/^v/, '');
        if (!isNewerVersion(latestVersion, localVersion)) {
            return; // Already up to date
        }

        // Prompt the user
        const goAvailable = !!findOnPath('go');
        const actions = goAvailable
            ? ['Update with Go', 'Download Binary', 'Dismiss']
            : ['Download', 'Dismiss'];

        const choice = await vscode.window.showInformationMessage(
            `TaskR: A new version is available (${latestVersion}, current: ${localVersion})`,
            ...actions
        );

        if (choice === 'Update with Go') {
            const updated = await goInstall();
            if (updated) {
                vscode.window.showInformationMessage(`TaskR: Updated to v${latestVersion}`);
            }
        } else if (choice === 'Download Binary' || choice === 'Download') {
            const storagePath = context.globalStorageUri.fsPath;
            const updated = await downloadBinary(storagePath);
            if (updated) {
                vscode.window.showInformationMessage(`TaskR: Updated to v${latestVersion}`);
            }
        }
    } catch {
        // Silently ignore — update checks should never block normal usage
    }
}

/**
 * Find the installed binary: PATH first, then extension storage.
 */
function findInstalledBinary(context: vscode.ExtensionContext): string | null {
    const pathBinary = findOnPath(BINARY_NAME);
    if (pathBinary) {
        return pathBinary;
    }
    const storagePath = context.globalStorageUri.fsPath;
    const localBinary = path.join(storagePath, BINARY_NAME);
    if (fs.existsSync(localBinary)) {
        return localBinary;
    }
    return null;
}

/**
 * Get the local version by running `taskr -v`.
 * Expected output: "taskr version 0.1.42"
 */
function getLocalVersion(binaryPath: string): string | null {
    try {
        const output = execSync(`"${binaryPath}" -v`, {
            encoding: 'utf-8',
            timeout: 5000,
        }).trim();
        // Parse "taskr version X.Y.Z"
        const match = output.match(/version\s+(\S+)/);
        return match ? match[1] : null;
    } catch {
        return null;
    }
}

/**
 * Fetch the latest release tag from GitHub.
 */
function fetchLatestReleaseTag(): Promise<string | null> {
    return new Promise((resolve) => {
        const options = {
            hostname: 'api.github.com',
            path: `/repos/${GITHUB_REPO}/releases/latest`,
            headers: { 'User-Agent': 'vscode-taskr' },
        };

        const req = https.get(options, (res) => {
            // Follow redirect
            if ((res.statusCode === 301 || res.statusCode === 302) && res.headers.location) {
                https.get(res.headers.location, { headers: options.headers }, (res2) => {
                    collectBody(res2, resolve);
                }).on('error', () => resolve(null));
                return;
            }
            collectBody(res, resolve);
        });

        req.on('error', () => resolve(null));
        req.setTimeout(10000, () => {
            req.destroy();
            resolve(null);
        });
    });
}

function collectBody(res: import('http').IncomingMessage, resolve: (tag: string | null) => void) {
    if (res.statusCode !== 200) {
        resolve(null);
        return;
    }
    let body = '';
    res.on('data', (chunk: Buffer) => { body += chunk.toString(); });
    res.on('end', () => {
        try {
            const data = JSON.parse(body);
            resolve(data.tag_name || null);
        } catch {
            resolve(null);
        }
    });
    res.on('error', () => resolve(null));
}

/**
 * Compare two semver-like version strings (e.g. "0.1.50" vs "0.1.42").
 * Returns true if `latest` is newer than `current`.
 */
function isNewerVersion(latest: string, current: string): boolean {
    // Handle 'dev' builds — always consider them outdated
    if (current === 'dev') {
        return true;
    }

    const latestParts = latest.split('.').map(Number);
    const currentParts = current.split('.').map(Number);

    for (let i = 0; i < Math.max(latestParts.length, currentParts.length); i++) {
        const l = latestParts[i] || 0;
        const c = currentParts[i] || 0;
        if (l > c) { return true; }
        if (l < c) { return false; }
    }
    return false; // Equal
}

export function deactivate() {}
