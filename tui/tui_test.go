package tui

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/altlimit/taskr/config"
	"github.com/altlimit/taskr/runner"
)

// newTestModel creates a minimal Model for testing log behavior
// without needing a full Bubble Tea runtime.
func newTestModel(labels []string) Model {
	ti := textinput.New()
	taskIndex := make(map[string]int)
	for i, l := range labels {
		taskIndex[l] = i
	}

	r := runner.New(makeConfigs(labels))

	m := Model{
		runner:       r,
		taskLabels:   labels,
		taskIndex:    taskIndex,
		allLogs:      make([]config.LogLine, 0, maxLogLines),
		follow:       true,
		activeTab:    -1,
		searchInput:  ti,
		capturedURLs: make(map[string][]string),
		lastRestart:  make(map[string]time.Time),
		restartMu:    &sync.Mutex{},
		mouseMode:    true,
		width:        120,
		height:       40,
		ready:        true,
		viewport:     viewport.New(120, 30),
	}
	return m
}

func makeConfigs(labels []string) []config.TaskConfig {
	var configs []config.TaskConfig
	for _, l := range labels {
		configs = append(configs, config.TaskConfig{
			Label:   l,
			Command: "echo " + l,
		})
	}
	return configs
}

// --- Empty line filtering tests ---

func TestAppendLog_SkipsEmptyLines(t *testing.T) {
	m := newTestModel([]string{"api"})

	m.appendLog(config.LogLine{TaskLabel: "api", Content: "", Timestamp: time.Now()})
	m.appendLog(config.LogLine{TaskLabel: "api", Content: "   ", Timestamp: time.Now()})
	m.appendLog(config.LogLine{TaskLabel: "api", Content: "\t\n", Timestamp: time.Now()})

	if len(m.allLogs) != 0 {
		t.Errorf("expected 0 logs (all empty), got %d", len(m.allLogs))
	}
}

func TestAppendLog_KeepsNonEmptyLines(t *testing.T) {
	m := newTestModel([]string{"api"})

	m.appendLog(config.LogLine{TaskLabel: "api", Content: "hello world", Timestamp: time.Now()})
	m.appendLog(config.LogLine{TaskLabel: "api", Content: " data ", Timestamp: time.Now()})

	if len(m.allLogs) != 2 {
		t.Errorf("expected 2 logs, got %d", len(m.allLogs))
	}
}

// --- Log deduplication tests ---

func TestAppendLog_DeduplicatesConsecutive(t *testing.T) {
	m := newTestModel([]string{"api"})
	now := time.Now()

	m.appendLog(config.LogLine{TaskLabel: "api", Content: "WARNING: something bad", Timestamp: now})
	m.appendLog(config.LogLine{TaskLabel: "api", Content: "WARNING: something bad", Timestamp: now.Add(1 * time.Second)})
	m.appendLog(config.LogLine{TaskLabel: "api", Content: "WARNING: something bad", Timestamp: now.Add(2 * time.Second)})

	if len(m.allLogs) != 1 {
		t.Fatalf("expected 1 log (deduped), got %d", len(m.allLogs))
	}
	if m.allLogs[0].Content != "(×3) WARNING: something bad" {
		t.Errorf("content = %q, want %q", m.allLogs[0].Content, "(×3) WARNING: something bad")
	}
}

func TestAppendLog_DifferentLinesNotDeduped(t *testing.T) {
	m := newTestModel([]string{"api"})
	now := time.Now()

	m.appendLog(config.LogLine{TaskLabel: "api", Content: "line 1", Timestamp: now})
	m.appendLog(config.LogLine{TaskLabel: "api", Content: "line 2", Timestamp: now.Add(1 * time.Second)})
	m.appendLog(config.LogLine{TaskLabel: "api", Content: "line 3", Timestamp: now.Add(2 * time.Second)})

	if len(m.allLogs) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(m.allLogs))
	}
}

func TestAppendLog_DedupResetsOnDifferentLine(t *testing.T) {
	m := newTestModel([]string{"api"})
	now := time.Now()

	m.appendLog(config.LogLine{TaskLabel: "api", Content: "repeat", Timestamp: now})
	m.appendLog(config.LogLine{TaskLabel: "api", Content: "repeat", Timestamp: now.Add(1 * time.Second)})
	m.appendLog(config.LogLine{TaskLabel: "api", Content: "different", Timestamp: now.Add(2 * time.Second)})
	m.appendLog(config.LogLine{TaskLabel: "api", Content: "repeat", Timestamp: now.Add(3 * time.Second)})

	if len(m.allLogs) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(m.allLogs))
	}
	if m.allLogs[0].Content != "(×2) repeat" {
		t.Errorf("first log = %q, want %q", m.allLogs[0].Content, "(×2) repeat")
	}
	if m.allLogs[1].Content != "different" {
		t.Errorf("second log = %q, want %q", m.allLogs[1].Content, "different")
	}
	if m.allLogs[2].Content != "repeat" {
		t.Errorf("third log = %q, want %q", m.allLogs[2].Content, "repeat")
	}
}

func TestAppendLog_DedupDifferentTasks(t *testing.T) {
	m := newTestModel([]string{"api", "web"})
	now := time.Now()

	m.appendLog(config.LogLine{TaskLabel: "api", Content: "same line", Timestamp: now})
	m.appendLog(config.LogLine{TaskLabel: "web", Content: "same line", Timestamp: now.Add(1 * time.Second)})

	// Different tasks, even with same content, should NOT deduplicate
	if len(m.allLogs) != 2 {
		t.Fatalf("expected 2 logs (different tasks), got %d", len(m.allLogs))
	}
}

func TestAppendLog_DedupTimeBound(t *testing.T) {
	m := newTestModel([]string{"api"})
	now := time.Now()

	m.appendLog(config.LogLine{TaskLabel: "api", Content: "old warning", Timestamp: now})
	// Same content but 2 minutes later — should NOT collapse
	m.appendLog(config.LogLine{TaskLabel: "api", Content: "old warning", Timestamp: now.Add(2 * time.Minute)})

	if len(m.allLogs) != 2 {
		t.Fatalf("expected 2 logs (time window expired), got %d", len(m.allLogs))
	}
}

func TestAppendLog_DedupWithin1Minute(t *testing.T) {
	m := newTestModel([]string{"api"})
	now := time.Now()

	m.appendLog(config.LogLine{TaskLabel: "api", Content: "warning", Timestamp: now})
	// Same content 59 seconds later — should collapse
	m.appendLog(config.LogLine{TaskLabel: "api", Content: "warning", Timestamp: now.Add(59 * time.Second)})

	if len(m.allLogs) != 1 {
		t.Fatalf("expected 1 log (within time window), got %d", len(m.allLogs))
	}
	if m.allLogs[0].Content != "(×2) warning" {
		t.Errorf("content = %q, want %q", m.allLogs[0].Content, "(×2) warning")
	}
}

// --- Search no-results tests ---

func TestBuildLogContent_NoResultsMessage(t *testing.T) {
	m := newTestModel([]string{"api"})
	now := time.Now()

	m.appendLog(config.LogLine{TaskLabel: "api", Content: "server started", Timestamp: now})
	m.appendLog(config.LogLine{TaskLabel: "api", Content: "listening on :8080", Timestamp: now})

	m.searchQuery = "nonexistent_query_xyz"

	content := m.buildLogContent()
	if !strings.Contains(content, "No results") {
		t.Errorf("expected 'No results' message, got: %q", content)
	}
	if !strings.Contains(content, "nonexistent_query_xyz") {
		t.Errorf("expected query in no-results message, got: %q", content)
	}
}

func TestBuildLogContent_SearchFindsResults(t *testing.T) {
	m := newTestModel([]string{"api"})
	now := time.Now()

	m.appendLog(config.LogLine{TaskLabel: "api", Content: "server started", Timestamp: now})
	m.appendLog(config.LogLine{TaskLabel: "api", Content: "listening on :8080", Timestamp: now})

	m.searchQuery = "server"

	content := m.buildLogContent()
	if strings.Contains(content, "No results") {
		t.Error("should not show 'No results' when search matches")
	}
	if !strings.Contains(content, "server started") {
		t.Error("matching line should be in content")
	}
}

func TestBuildLogContent_EmptySearchShowsAll(t *testing.T) {
	m := newTestModel([]string{"api"})
	now := time.Now()

	m.appendLog(config.LogLine{TaskLabel: "api", Content: "line 1", Timestamp: now})
	m.appendLog(config.LogLine{TaskLabel: "api", Content: "line 2", Timestamp: now})

	m.searchQuery = ""

	content := m.buildLogContent()
	if !strings.Contains(content, "line 1") || !strings.Contains(content, "line 2") {
		t.Error("empty search should show all lines")
	}
}

// --- Hidden task filtering in ALL view ---

func TestBuildLogContent_HiddenTaskFiltered(t *testing.T) {
	m := newTestModel([]string{"api", "web"})
	now := time.Now()

	m.appendLog(config.LogLine{TaskLabel: "api", Content: "api log", Timestamp: now})
	m.appendLog(config.LogLine{TaskLabel: "web", Content: "web log", Timestamp: now})

	// Hide "web" task
	m.runner.ToggleHidden("web")
	m.activeTab = -1 // ALL view

	content := m.buildLogContent()
	if !strings.Contains(content, "api log") {
		t.Error("visible task log should appear")
	}
	if strings.Contains(content, "web log") {
		t.Error("hidden task log should not appear in ALL view")
	}
}

func TestBuildLogContent_HiddenTaskShowsInTab(t *testing.T) {
	m := newTestModel([]string{"api", "web"})
	now := time.Now()

	m.appendLog(config.LogLine{TaskLabel: "web", Content: "web log", Timestamp: now})

	// Hide "web" task
	m.runner.ToggleHidden("web")
	m.activeTab = 1 // web's tab

	content := m.buildLogContent()
	// When viewing the specific tab, hidden tasks should still show their logs
	if !strings.Contains(content, "web log") {
		t.Error("hidden task log should appear when viewing its tab directly")
	}
}
