package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/altlimit/taskr/config"
	"github.com/altlimit/taskr/runner"
)

const maxLogLines = 10000

// --- Bubble Tea messages ---

type logLineMsg config.LogLine
type capturedURLMsg runner.CapturedURL
type tickMsg time.Time

// ReloadMsg is sent to the TUI when tasks.json is reloaded so the tab bar,
// URL bar and task index are updated to reflect the new set of running tasks.
type ReloadMsg struct{ Labels []string }

// ClearURLsMsg clears pinned URLs for a specific task label.
// If Label is empty, all URLs are cleared.
type ClearURLsMsg struct{ Label string }

// --- Styles ---

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	tabStyle = lipgloss.NewStyle().
			Padding(0, 1)

	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A0A0A0")).
			Background(lipgloss.Color("#1A1A2E")).
			Padding(0, 1)

	urlBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00BFFF")).
			Background(lipgloss.Color("#1A1A2E")).
			Padding(0, 1)

	searchBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFD700")).
			Padding(0, 1)

	stderrStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6347"))

	timestampStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555"))

	watchIconStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF7F"))

	hiddenIconStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF4500"))

	noResultsStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Italic(true).
			Padding(1, 2)
)

// Model is the Bubble Tea model for the taskr TUI.
type Model struct {
	runner     *runner.Runner
	taskLabels []string
	taskIndex  map[string]int // label → index

	// Log storage
	allLogs    []config.LogLine
	viewport   viewport.Model
	follow     bool

	// Filtering
	activeTab  int // -1 = ALL, 0..n = specific task
	searchMode bool
	searchInput textinput.Model
	searchQuery string

	// URLs
	capturedURLs map[string][]string // label → urls (deduped)
	urlOrder     []string            // insertion order for display

	// Watcher state callback
	watchToggle  func(label string) bool
	watchEnabled func(label string) bool

	// Restart cooldown (prevents accidental double-restarts)
	lastRestart  map[string]time.Time
	restartMu    *sync.Mutex

	// Mouse mode (true = mouse captured for scroll, false = copy mode)
	mouseMode bool

	// Label display (false = compact bullet, true = full names)
	showLabels bool

	// Deduplication tracking
	lastLogKey     string    // "label\x00content" of last appended log
	lastLogContent string    // original content (without count prefix)
	lastLogCount   int       // how many consecutive duplicates
	lastLogTime    time.Time // timestamp of  first line in current dedup run

	// Batched rendering
	pendingLogs int // number of logs received since last tick

	// Terminal dimensions
	width  int
	height int
	ready  bool
}

// NewModel creates the TUI model.
func NewModel(r *runner.Runner, watchToggle func(string) bool, watchEnabled func(string) bool) Model {
	ti := textinput.New()
	ti.Placeholder = "search..."
	ti.CharLimit = 100

	labels := r.TaskOrder()
	taskIndex := make(map[string]int)
	for i, l := range labels {
		taskIndex[l] = i
	}

	return Model{
		runner:       r,
		taskLabels:   labels,
		taskIndex:    taskIndex,
		allLogs:      make([]config.LogLine, 0, maxLogLines),
		follow:       true,
		activeTab:    -1, // ALL
		searchInput:  ti,
		capturedURLs: make(map[string][]string),
		watchToggle:  watchToggle,
		watchEnabled: watchEnabled,
		lastRestart:  make(map[string]time.Time),
		restartMu:    &sync.Mutex{},
		mouseMode:    true,
	}
}

// waitForLog returns a tea.Cmd that waits for the next log line.
func waitForLog(ch <-chan config.LogLine) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil
		}
		return logLineMsg(line)
	}
}

// waitForURL returns a tea.Cmd that waits for the next captured URL.
func waitForURL(ch <-chan runner.CapturedURL) tea.Cmd {
	return func() tea.Msg {
		url, ok := <-ch
		if !ok {
			return nil
		}
		return capturedURLMsg(url)
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		waitForLog(m.runner.LogCh()),
		waitForURL(m.runner.URLCh()),
		tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) }),
	)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeViewport()
		m.refreshViewport()

	case tea.KeyMsg:
		if m.searchMode {
			switch msg.String() {
			case "esc":
				m.searchMode = false
				m.searchQuery = ""
				m.searchInput.SetValue("")
				m.searchInput.Blur()
				m.refreshViewport()
			case "enter":
				m.searchQuery = m.searchInput.Value()
				m.refreshViewport()
			default:
				var cmd tea.Cmd
				m.searchInput, cmd = m.searchInput.Update(msg)
				// Live search as you type
				m.searchQuery = m.searchInput.Value()
				m.refreshViewport()
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "left":
			if m.activeTab > -1 {
				m.activeTab--
			}
			m.refreshViewport()

		case "right":
			if m.activeTab < len(m.taskLabels)-1 {
				m.activeTab++
			}
			m.refreshViewport()

		case "tab":
			m.activeTab++
			if m.activeTab >= len(m.taskLabels) {
				m.activeTab = -1
			}
			m.refreshViewport()

		case "a":
			m.activeTab = -1
			m.refreshViewport()

		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			idx := int(msg.String()[0]-'0') - 1
			if idx < len(m.taskLabels) {
				m.activeTab = idx
				m.refreshViewport()
			}

		case "r":
			if m.activeTab >= 0 {
				label := m.taskLabels[m.activeTab]
				if m.canRestart(label) {
					m.markRestart(label)
					m.clearURLs(label)
					m.runner.RestartTask(label)
				}
			} else {
				if m.canRestart("__all__") {
					m.markRestart("__all__")
					m.clearURLs("")
					m.runner.RestartAll()
				}
			}

		case "R":
			if m.canRestart("__all__") {
				m.markRestart("__all__")
				m.clearURLs("")
				m.runner.RestartAll()
			}

		case "s":
			if m.activeTab >= 0 {
				m.runner.StopTask(m.taskLabels[m.activeTab])
			}

		case "S":
			m.runner.StopAll()

		case " ":
			if m.activeTab >= 0 {
				label := m.taskLabels[m.activeTab]
				if m.watchToggle != nil {
					m.watchToggle(label)
				}
			}

		case "h":
			if m.activeTab >= 0 {
				label := m.taskLabels[m.activeTab]
				m.runner.ToggleHidden(label)
				m.refreshViewport()
			}

		case "f":
			m.follow = !m.follow
			if m.follow {
				m.viewport.GotoBottom()
			}

		case "m":
			m.mouseMode = !m.mouseMode
			m.refreshViewport()
			if m.mouseMode {
				return m, func() tea.Msg { return tea.EnableMouseCellMotion() }
			}
			return m, func() tea.Msg { return tea.DisableMouse() }

		case "l":
			m.showLabels = !m.showLabels
			m.refreshViewport()

		case "c":
			m.allLogs = m.allLogs[:0]
			m.refreshViewport()

		case "/":
			m.searchMode = true
			m.searchInput.Focus()
			return m, m.searchInput.Cursor.BlinkCmd()

		case "up", "down", "pgup", "pgdown":
			m.follow = false
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
		}

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.follow = false
				m.viewport.LineUp(3)
			case tea.MouseButtonWheelDown:
				m.viewport.LineDown(3)
			}
		}

	case logLineMsg:
		line := config.LogLine(msg)
		m.appendLog(line)
		m.pendingLogs++
		cmds = append(cmds, waitForLog(m.runner.LogCh()))

	case tickMsg:
		if m.pendingLogs > 0 {
			m.refreshViewport()
			if m.follow {
				m.viewport.GotoBottom()
			}
			m.pendingLogs = 0
		}
		cmds = append(cmds, tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) }))

	case ClearURLsMsg:
		m.clearURLs(msg.Label)
		m.resizeViewport()

	case capturedURLMsg:
		url := runner.CapturedURL(msg)
		m.addURL(url.TaskLabel, url.URL)
		m.resizeViewport() // URL bar changed height
		cmds = append(cmds, waitForURL(m.runner.URLCh()))

	case ReloadMsg:
		// Rebuild task list and index from the new label set.
		m.taskLabels = msg.Labels
		newIndex := make(map[string]int, len(msg.Labels))
		for i, l := range msg.Labels {
			newIndex[l] = i
		}
		m.taskIndex = newIndex

		// Clear URLs for tasks that were restarted or removed.
		var newURLOrder []string
		for _, l := range m.urlOrder {
			if _, ok := newIndex[l]; ok {
				newURLOrder = append(newURLOrder, l)
			} else {
				delete(m.capturedURLs, l)
			}
		}
		m.urlOrder = newURLOrder

		// Clamp activeTab so it stays valid.
		if m.activeTab >= len(m.taskLabels) {
			m.activeTab = len(m.taskLabels) - 1
		}

		m.resizeViewport()
		m.refreshViewport()
	}

	return m, tea.Batch(cmds...)
}

// resizeViewport recalculates viewport height based on current header/footer.
func (m *Model) resizeViewport() {
	urlBarLines := 0
	if urlBar := m.buildURLBar(); urlBar != "" {
		urlBarLines = strings.Count(urlBar, "\n") + 1
	}
	headerHeight := 2 + urlBarLines
	footerHeight := 2
	if m.searchMode {
		footerHeight = 3
	}
	viewportHeight := m.height - headerHeight - footerHeight
	if viewportHeight < 1 {
		viewportHeight = 1
	}
	if !m.ready {
		m.viewport = viewport.New(m.width, viewportHeight)
		m.viewport.SetContent("")
		m.ready = true
	} else {
		m.viewport.Width = m.width
		m.viewport.Height = viewportHeight
	}
}

func (m *Model) appendLog(line config.LogLine) {
	// Skip empty/whitespace-only lines
	if strings.TrimSpace(line.Content) == "" {
		return
	}

	// Deduplication: collapse consecutive identical lines from the same task
	// Only collapse within a 1-minute window
	key := line.TaskLabel + "\x00" + line.Content
	if key == m.lastLogKey && len(m.allLogs) > 0 && line.Timestamp.Sub(m.lastLogTime) <= time.Minute {
		m.lastLogCount++
		// Update the last log entry's content with the count
		last := &m.allLogs[len(m.allLogs)-1]
		last.Content = fmt.Sprintf("(×%d) %s", m.lastLogCount, m.lastLogContent)
		last.Timestamp = line.Timestamp
		return
	}

	// New unique line or dedup window expired
	m.lastLogKey = key
	m.lastLogContent = line.Content
	m.lastLogCount = 1
	m.lastLogTime = line.Timestamp

	m.allLogs = append(m.allLogs, line)
	// Ring buffer: if we exceed max, trim the front
	if len(m.allLogs) > maxLogLines {
		copy(m.allLogs, m.allLogs[len(m.allLogs)-maxLogLines:])
		m.allLogs = m.allLogs[:maxLogLines]
	}
}

// clearURLs removes captured URLs for a task so the URL bar is fresh after a
// restart. Pass an empty label to clear all tasks' URLs.
func (m *Model) clearURLs(label string) {
	if label == "" {
		m.capturedURLs = make(map[string][]string)
		m.urlOrder = nil
	} else {
		delete(m.capturedURLs, label)
		for i, l := range m.urlOrder {
			if l == label {
				m.urlOrder = append(m.urlOrder[:i], m.urlOrder[i+1:]...)
				break
			}
		}
	}
	m.resizeViewport()
}

func (m *Model) addURL(label, url string) {
	// Deduplicate
	existing := m.capturedURLs[label]
	for _, u := range existing {
		if u == url {
			return
		}
	}
	m.capturedURLs[label] = append(existing, url)
	// Track insertion order for the label
	found := false
	for _, l := range m.urlOrder {
		if l == label {
			found = true
			break
		}
	}
	if !found {
		m.urlOrder = append(m.urlOrder, label)
	}
}

func (m *Model) refreshViewport() {
	if !m.ready {
		return
	}
	content := m.buildLogContent()
	m.viewport.SetContent(content)
}

func (m *Model) buildLogContent() string {
	var sb strings.Builder
	searchLower := strings.ToLower(m.searchQuery)
	matchCount := 0

	// Label display mode
	prefixLen := 10 // just bullet + timestamp: "● 15:04:05 "
	if m.showLabels {
		prefixLen = 22 // approximate for wrapping
	}
	wrapWidth := m.width
	if wrapWidth < 40 {
		wrapWidth = 40
	}

	for _, line := range m.allLogs {
		// Filter by active tab
		if m.activeTab >= 0 {
			if line.TaskLabel != m.taskLabels[m.activeTab] {
				continue
			}
		}

		// Hide logs for hidden tasks (in ALL view, skip hidden; in tab view the toggle is visible)
		if m.activeTab == -1 && m.runner.IsHidden(line.TaskLabel) {
			continue
		}

		// Format the line
		idx, ok := m.taskIndex[line.TaskLabel]
		if !ok {
			idx = 0
		}

		ts := timestampStyle.Render(line.Timestamp.Format("15:04:05"))
		var label string
		if m.showLabels {
			label = LabelStyle(idx).Render(line.TaskLabel)
		} else {
			label = LabelStyle(idx).Render("●")
		}

		content := line.Content
		if line.Stream == "stderr" {
			content = stderrStyle.Render(content)
		}

		// Copy mode: show raw content only (no labels/timestamps)
		if !m.mouseMode {
			if m.searchQuery != "" {
				if !strings.Contains(strings.ToLower(content), searchLower) &&
					!strings.Contains(strings.ToLower(line.Content), searchLower) {
					continue
				}
			}
			matchCount++
			sb.WriteString(content)
			sb.WriteByte('\n')
			continue
		}

		// Wrap long content at terminal width
		maxContentWidth := wrapWidth - prefixLen
		if maxContentWidth > 0 && len(line.Content) > maxContentWidth {
			// Filter by search before rendering wrapped lines
			if m.searchQuery != "" {
				if !strings.Contains(strings.ToLower(line.Content), searchLower) {
					continue
				}
			}
			matchCount++
			lines := wrapText(content, maxContentWidth)
			padding := strings.Repeat(" ", prefixLen)
			for i, wl := range lines {
				if i == 0 {
					sb.WriteString(fmt.Sprintf("%s %s %s\n", label, ts, wl))
				} else {
					sb.WriteString(fmt.Sprintf("%s%s\n", padding, wl))
				}
			}
		} else {
			formatted := fmt.Sprintf("%s %s %s", label, ts, content)

			// Filter by search
			if m.searchQuery != "" {
				if !strings.Contains(strings.ToLower(formatted), searchLower) &&
					!strings.Contains(strings.ToLower(line.Content), searchLower) {
					continue
				}
			}
			matchCount++

			sb.WriteString(formatted)
			sb.WriteByte('\n')
		}
	}

	// Show "no results" message when search is active but nothing matched
	if m.searchQuery != "" && matchCount == 0 {
		return noResultsStyle.Render(fmt.Sprintf("No results for %q", m.searchQuery))
	}

	return sb.String()
}

// wrapText breaks a string into lines of at most width characters.
func wrapText(s string, width int) []string {
	if width <= 0 || len(s) <= width {
		return []string{s}
	}
	var lines []string
	for len(s) > width {
		// Try to break at a space
		breakAt := width
		for i := width; i > width/2; i-- {
			if s[i] == ' ' {
				breakAt = i
				break
			}
		}
		lines = append(lines, s[:breakAt])
		s = s[breakAt:]
		if len(s) > 0 && s[0] == ' ' {
			s = s[1:] // skip the space at the break
		}
	}
	if len(s) > 0 {
		lines = append(lines, s)
	}
	return lines
}

// canRestart checks if enough time has passed since the last manual restart (2s cooldown).
func (m *Model) canRestart(key string) bool {
	m.restartMu.Lock()
	defer m.restartMu.Unlock()
	last, ok := m.lastRestart[key]
	if !ok {
		return true
	}
	return time.Since(last) > 2*time.Second
}

func (m *Model) markRestart(key string) {
	m.restartMu.Lock()
	defer m.restartMu.Unlock()
	m.lastRestart[key] = time.Now()
}

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return "Starting taskr..."
	}

	var sections []string

	// Title bar
	runningCount := 0
	for _, l := range m.taskLabels {
		if m.runner.GetStatus(l) == config.StatusRunning {
			runningCount++
		}
	}
	title := titleStyle.Render(fmt.Sprintf(" taskr ▸ %d/%d tasks running ", runningCount, len(m.taskLabels)))

	// URL bar (persistent display of captured URLs)
	urlBar := m.buildURLBar()

	sections = append(sections, title)

	// Tab bar
	tabs := m.buildTabBar()
	sections = append(sections, tabs)

	// URL bar (only show if we have URLs)
	if urlBar != "" {
		sections = append(sections, urlBar)
	}

	// Viewport (log area)
	sections = append(sections, m.viewport.View())

	// Search bar
	if m.searchMode {
		searchBar := searchBarStyle.Render("🔍 ") + m.searchInput.View()
		sections = append(sections, searchBar)
	}

	// Status/shortcut bar
	followIcon := "○"
	if m.follow {
		followIcon = "●"
	}
	mouseIcon := "scroll"
	if !m.mouseMode {
		mouseIcon = "copy"
	}
	labelIcon := "●"
	if m.showLabels {
		labelIcon = "[ab]"
	}
	shortcuts := statusBarStyle.Render(
		fmt.Sprintf(" ←→ task │ r restart │ s stop │ Space watch │ h hide │ / search │ f follow %s │ l %s │ m %s │ c clear │ q quit ", followIcon, labelIcon, mouseIcon),
	)
	sections = append(sections, shortcuts)

	return strings.Join(sections, "\n")
}

func (m Model) buildTabBar() string {
	var tabs []string

	// ALL tab
	if m.activeTab == -1 {
		tabs = append(tabs, activeTabStyle.Render("ALL"))
	} else {
		tabs = append(tabs, tabStyle.Render("ALL"))
	}

	for i, label := range m.taskLabels {
		status := m.runner.GetStatus(label)
		dot := StatusDot(i, status == config.StatusRunning)

		watchIcon := ""
		if m.watchEnabled != nil && m.watchEnabled(label) {
			watchIcon = watchIconStyle.Render("👁")
		}

		hideIcon := ""
		if m.runner.IsHidden(label) {
			hideIcon = hiddenIconStyle.Render("🔇")
		}

		tabText := fmt.Sprintf("%s %s%s%s", label, dot, watchIcon, hideIcon)

		if m.activeTab == i {
			tabs = append(tabs, activeTabStyle.Render(tabText))
		} else {
			tabs = append(tabs, tabStyle.Render(tabText))
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}

func (m Model) buildURLBar() string {
	if len(m.capturedURLs) == 0 {
		return ""
	}

	var parts []string
	for _, label := range m.urlOrder {
		urls := m.capturedURLs[label]
		if len(urls) == 0 {
			continue
		}
		idx := m.taskIndex[label]
		labelStyled := LabelStyle(idx).Render(label)
		// Show the most recent / primary URL (first one)
		for _, u := range urls {
			parts = append(parts, fmt.Sprintf("%s → %s", labelStyled, u))
		}
	}

	if len(parts) == 0 {
		return ""
	}

	// Show 2 URLs per line, with 🔗 prefix on each line
	var lines []string
	for i := 0; i < len(parts); i += 2 {
		if i+1 < len(parts) {
			lines = append(lines, "🔗 "+parts[i]+" │ "+parts[i+1])
		} else {
			lines = append(lines, "🔗 "+parts[i])
		}
	}
	return urlBarStyle.Render(strings.Join(lines, "\n"))
}

// truncateStr shortens a string to maxLen, adding "…" if truncated.
func truncateStr(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return "…"
	}
	return s[:maxLen-1] + "…"
}
