package runner

import (
	"testing"

	"github.com/altlimit/taskr/config"
)

// --- URL extraction tests ---

func TestURLPattern_BasicHTTP(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			"simple localhost",
			"Server listening on http://localhost:8080",
			[]string{"http://localhost:8080"},
		},
		{
			"localhost with path",
			"API ready at http://localhost:3000/api/v1",
			[]string{"http://localhost:3000/api/v1"},
		},
		{
			"https",
			"Visit https://example.com/dashboard",
			[]string{"https://example.com/dashboard"},
		},
		{
			"multiple URLs",
			"Local: http://localhost:5173 Network: http://192.168.1.100:5173",
			[]string{"http://localhost:5173", "http://192.168.1.100:5173"},
		},
		{
			"vite output",
			"  ➜  Local:   http://localhost:5173/",
			[]string{"http://localhost:5173/"},
		},
		{
			"no URL",
			"Starting build process...",
			nil,
		},
		{
			"URL with trailing period",
			"Visit http://localhost:8080.",
			[]string{"http://localhost:8080"},
		},
		{
			"URL with query params",
			"Open http://localhost:3000?foo=bar&baz=1",
			[]string{"http://localhost:3000?foo=bar&baz=1"},
		},
		{
			"IP address URL",
			"http://0.0.0.0:4000",
			[]string{"http://0.0.0.0:4000"},
		},
		{
			"URL in quotes",
			`Server at "http://localhost:9090"`,
			[]string{"http://localhost:9090"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := urlPattern.FindAllString(tt.input, -1)
			// Trim trailing punctuation same as extractURLs does
			var cleaned []string
			for _, m := range matches {
				m = trimURLPunctuation(m)
				cleaned = append(cleaned, m)
			}

			if tt.want == nil {
				if len(cleaned) != 0 {
					t.Errorf("expected no matches, got %v", cleaned)
				}
				return
			}

			if len(cleaned) != len(tt.want) {
				t.Fatalf("got %d matches %v, want %d %v", len(cleaned), cleaned, len(tt.want), tt.want)
			}
			for i, got := range cleaned {
				if got != tt.want[i] {
					t.Errorf("match[%d] = %q, want %q", i, got, tt.want[i])
				}
			}
		})
	}
}

func TestURLPattern_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		hasMatch bool
	}{
		{"ftp not matched", "ftp://files.example.com", false},
		{"just protocol", "http://", false}, // bare protocol correctly doesn't match
		{"embedded in ANSI", "\033[32mhttp://localhost:3000\033[0m", true},
		{"extremely long path", "http://localhost:8080/" + repeat("a", 200), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := urlPattern.FindAllString(tt.input, -1)
			hasMatch := len(matches) > 0
			if hasMatch != tt.hasMatch {
				t.Errorf("hasMatch = %v, want %v (matches: %v)", hasMatch, tt.hasMatch, matches)
			}
		})
	}
}

// -- helpers ---

func trimURLPunctuation(s string) string {
	for len(s) > 0 {
		last := s[len(s)-1]
		if last == '.' || last == ',' || last == ';' || last == ':' ||
			last == '!' || last == '?' || last == ')' {
			s = s[:len(s)-1]
		} else {
			break
		}
	}
	return s
}

func repeat(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}

// --- Hidden tasks tests ---

func TestIsHidden_DefaultFalse(t *testing.T) {
	r := New([]config.TaskConfig{
		{Label: "api", Command: "go run ."},
	})
	if r.IsHidden("api") {
		t.Error("task should not be hidden by default")
	}
}

func TestIsHidden_NonexistentTask(t *testing.T) {
	r := New([]config.TaskConfig{
		{Label: "api", Command: "go run ."},
	})
	if r.IsHidden("nonexistent") {
		t.Error("nonexistent task should not be hidden")
	}
}

func TestToggleHidden(t *testing.T) {
	r := New([]config.TaskConfig{
		{Label: "api", Command: "go run ."},
	})

	// Toggle on
	result := r.ToggleHidden("api")
	if !result {
		t.Error("first toggle should return true (hidden)")
	}
	if !r.IsHidden("api") {
		t.Error("task should be hidden after toggle")
	}

	// Toggle off
	result = r.ToggleHidden("api")
	if result {
		t.Error("second toggle should return false (visible)")
	}
	if r.IsHidden("api") {
		t.Error("task should be visible after second toggle")
	}
}

func TestHideLogs_ConfigSeeding(t *testing.T) {
	r := New([]config.TaskConfig{
		{Label: "noisy", Command: "echo spam", HideLogs: true},
		{Label: "quiet", Command: "echo hello", HideLogs: false},
	})

	if !r.IsHidden("noisy") {
		t.Error("task with HideLogs=true should start hidden")
	}
	if r.IsHidden("quiet") {
		t.Error("task with HideLogs=false should start visible")
	}
}

func TestToggleHidden_IndependentTasks(t *testing.T) {
	r := New([]config.TaskConfig{
		{Label: "a", Command: "echo a"},
		{Label: "b", Command: "echo b"},
	})

	r.ToggleHidden("a")
	if !r.IsHidden("a") {
		t.Error("a should be hidden")
	}
	if r.IsHidden("b") {
		t.Error("b should not be affected by toggling a")
	}
}
