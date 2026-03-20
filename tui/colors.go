package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Palette of visually distinct colors for task labels.
var palette = []lipgloss.Color{
	lipgloss.Color("#00BFFF"), // cyan
	lipgloss.Color("#FF69B4"), // hot pink
	lipgloss.Color("#FFD700"), // gold
	lipgloss.Color("#00FF7F"), // spring green
	lipgloss.Color("#9370DB"), // medium purple
	lipgloss.Color("#FF6347"), // tomato
	lipgloss.Color("#20B2AA"), // light sea green
	lipgloss.Color("#FF8C00"), // dark orange
	lipgloss.Color("#7B68EE"), // medium slate blue
	lipgloss.Color("#3CB371"), // medium sea green
}

// ColorForIndex returns the Lip Gloss color for a task index (cycles).
func ColorForIndex(i int) lipgloss.Color {
	return palette[i%len(palette)]
}

// LabelStyle returns a styled rendering function for a task's label.
func LabelStyle(i int) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(ColorForIndex(i)).
		Bold(true)
}

// StatusDot returns a colored dot for the task status.
func StatusDot(i int, running bool) string {
	color := ColorForIndex(i)
	dot := "○"
	if running {
		dot = "●"
	}
	return lipgloss.NewStyle().Foreground(color).Render(dot)
}
