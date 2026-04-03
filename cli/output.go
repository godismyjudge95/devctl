package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Styles
var (
	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	styleWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	styleErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red
	styleDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // gray
	styleBold   = lipgloss.NewStyle().Bold(true)
	styleLabel  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")) // cyan
)

// StatusStyle returns a colored status string.
func StatusStyle(status string) string {
	switch status {
	case "running":
		return styleOK.Render("● running")
	case "stopped":
		return styleErr.Render("○ stopped")
	case "pending":
		return styleWarn.Render("◌ pending")
	case "warning":
		return styleWarn.Render("⚠ warning")
	default:
		return styleDim.Render("? " + status)
	}
}

// PrintJSON marshals v to indented JSON and prints it.
func PrintJSON(v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		PrintErr(fmt.Errorf("json: %w", err))
		return
	}
	fmt.Println(string(b))
}

// PrintErr prints an error to stderr in red.
func PrintErr(err error) {
	fmt.Fprintln(os.Stderr, styleErr.Render("error: "+err.Error()))
}

// PrintOK prints a success message in green.
func PrintOK(msg string) {
	fmt.Println(styleOK.Render("✓ " + msg))
}

// Header prints a bold section header.
func Header(s string) {
	fmt.Println(styleHeader.Render(s))
}

// KV prints a key-value pair aligned in two columns.
func KV(key, value string) {
	fmt.Printf("  %-26s %s\n", styleDim.Render(key), value)
}

// Table prints a simple aligned table.
// headers is a slice of column names; rows is a slice of row slices.
// widths optionally sets minimum column widths.
func Table(headers []string, rows [][]string) {
	// Compute column widths
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(stripANSI(cell)) > widths[i] {
				widths[i] = len(stripANSI(cell))
			}
		}
	}

	// Header row
	var sb strings.Builder
	for i, h := range headers {
		sb.WriteString(styleHeader.Render(padRight(h, widths[i])))
		if i < len(headers)-1 {
			sb.WriteString("  ")
		}
	}
	fmt.Println(sb.String())

	// Divider
	divParts := make([]string, len(headers))
	for i, w := range widths {
		divParts[i] = strings.Repeat("─", w)
	}
	fmt.Println(styleDim.Render(strings.Join(divParts, "  ")))

	// Data rows
	for _, row := range rows {
		var rb strings.Builder
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			// Pad based on visible length
			visible := len(stripANSI(cell))
			padding := widths[i] - visible
			if padding < 0 {
				padding = 0
			}
			rb.WriteString(cell)
			if i < len(headers)-1 {
				rb.WriteString(strings.Repeat(" ", padding+2))
			}
		}
		fmt.Println(rb.String())
	}
}

func padRight(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

// stripANSI removes ANSI escape codes for width measurement.
func stripANSI(s string) string {
	var out strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
