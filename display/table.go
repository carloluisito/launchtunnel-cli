package display

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Table formats columnar data for terminal output.
type Table struct {
	headers []string
	rows    [][]string
	widths  []int
}

// NewTable creates a table with the given column headers.
func NewTable(headers ...string) *Table {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	return &Table{
		headers: headers,
		widths:  widths,
	}
}

// AddRow appends a row of values.
func (t *Table) AddRow(cols ...string) {
	for i, c := range cols {
		if i < len(t.widths) && len(c) > t.widths[i] {
			t.widths[i] = len(c)
		}
	}
	t.rows = append(t.rows, cols)
}

// Render writes the formatted table to w.
func (t *Table) Render(w io.Writer) {
	fmts := make([]string, len(t.headers))
	for i, width := range t.widths {
		if i == len(t.headers)-1 {
			fmts[i] = "%s" // last column: no padding
		} else {
			fmts[i] = fmt.Sprintf("%%-%ds", width)
		}
	}

	// Header row.
	parts := make([]string, len(t.headers))
	for i, h := range t.headers {
		parts[i] = fmt.Sprintf(fmts[i], h)
	}
	fmt.Fprintln(w, strings.Join(parts, "  "))

	// Data rows.
	for _, row := range t.rows {
		parts := make([]string, len(t.headers))
		for i := range t.headers {
			val := ""
			if i < len(row) {
				val = row[i]
			}
			parts[i] = fmt.Sprintf(fmts[i], val)
		}
		fmt.Fprintln(w, strings.Join(parts, "  "))
	}
}

// PrintJSON marshals v as indented JSON and writes it to w.
func PrintJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// FormatBytes formats a byte count for human-readable display.
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
