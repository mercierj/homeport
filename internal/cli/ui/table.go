package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Table styles
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	cellStyle = lipgloss.NewStyle().
			Padding(0, 1)

	evenRowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA"))

	oddRowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E0E0E0"))
)

// Table represents a table with headers and rows
type Table struct {
	Headers []string
	Rows    [][]string
}

// NewTable creates a new table
func NewTable(headers []string) *Table {
	return &Table{
		Headers: headers,
		Rows:    make([][]string, 0),
	}
}

// AddRow adds a row to the table
func (t *Table) AddRow(row []string) {
	t.Rows = append(t.Rows, row)
}

// Render renders the table as a formatted string
func (t *Table) Render() string {
	if len(t.Headers) == 0 {
		return ""
	}

	// Calculate column widths
	colWidths := make([]int, len(t.Headers))
	for i, header := range t.Headers {
		colWidths[i] = len(header)
	}

	for _, row := range t.Rows {
		for i, cell := range row {
			if i < len(colWidths) && len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}

	var sb strings.Builder

	// Render headers
	for i, header := range t.Headers {
		paddedHeader := padRight(header, colWidths[i])
		sb.WriteString(headerStyle.Render(paddedHeader))
		if i < len(t.Headers)-1 {
			sb.WriteString(" ")
		}
	}
	sb.WriteString("\n")

	// Render rows
	for rowIdx, row := range t.Rows {
		var rowStyle lipgloss.Style
		if rowIdx%2 == 0 {
			rowStyle = evenRowStyle
		} else {
			rowStyle = oddRowStyle
		}

		for i, cell := range row {
			if i >= len(colWidths) {
				break
			}
			paddedCell := padRight(cell, colWidths[i])
			sb.WriteString(rowStyle.Render(cellStyle.Render(paddedCell)))
			if i < len(row)-1 {
				sb.WriteString(" ")
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// RenderSimple renders a simple ASCII table without styling
func (t *Table) RenderSimple() string {
	if len(t.Headers) == 0 {
		return ""
	}

	// Calculate column widths
	colWidths := make([]int, len(t.Headers))
	for i, header := range t.Headers {
		colWidths[i] = len(header)
	}

	for _, row := range t.Rows {
		for i, cell := range row {
			if i < len(colWidths) && len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}

	var sb strings.Builder

	// Render headers
	sb.WriteString("| ")
	for i, header := range t.Headers {
		sb.WriteString(padRight(header, colWidths[i]))
		sb.WriteString(" | ")
	}
	sb.WriteString("\n")

	// Render separator
	sb.WriteString("|")
	for _, width := range colWidths {
		sb.WriteString("-")
		sb.WriteString(strings.Repeat("-", width))
		sb.WriteString("-|")
	}
	sb.WriteString("\n")

	// Render rows
	for _, row := range t.Rows {
		sb.WriteString("| ")
		for i, cell := range row {
			if i >= len(colWidths) {
				break
			}
			sb.WriteString(padRight(cell, colWidths[i]))
			sb.WriteString(" | ")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// padRight pads a string to the right with spaces
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// PrintTable is a helper function to quickly print a table
func PrintTable(headers []string, rows [][]string) {
	table := NewTable(headers)
	for _, row := range rows {
		table.AddRow(row)
	}
	fmt.Println(table.Render())
}
