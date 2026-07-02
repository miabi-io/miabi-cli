// Package ui is the CLI's presentation layer: TTY-aware color, status glyphs,
// aligned tables, relative timestamps, a spinner, and confirmation prompts.
// Everything degrades cleanly when output is not a terminal (pipes, CI) or when
// color is disabled via NO_COLOR / --no-color, so machine consumers get plain
// text and humans get a polished terminal experience.
package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// colorEnabled gates every escape sequence. Resolved once at startup from the
// environment and the stdout TTY, then overridable by the root command.
var colorEnabled = envColor() && IsTerminal()

// envColor reports whether color is allowed by the environment. NO_COLOR (any
// value) and MIABI_NO_COLOR disable it; see https://no-color.org.
func envColor() bool {
	return os.Getenv("NO_COLOR") == "" && os.Getenv("MIABI_NO_COLOR") == ""
}

// IsTerminal reports whether stdout is an interactive terminal (a character
// device), using only the standard library.
func IsTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// SetColor force-enables or disables color, overriding auto-detection. The root
// command calls this for --no-color and structured (--json/-o) output. Color is
// only ever honored on a TTY, so passing true off a pipe stays plain.
func SetColor(on bool) { colorEnabled = on && IsTerminal() }

// ANSI SGR codes.
const (
	cReset   = "\033[0m"
	cBold    = "\033[1m"
	cDim     = "\033[2m"
	cRed     = "\033[31m"
	cGreen   = "\033[32m"
	cYellow  = "\033[33m"
	cBlue    = "\033[34m"
	cMagenta = "\033[35m"
	cCyan    = "\033[36m"
)

func paint(code, s string) string {
	if !colorEnabled {
		return s
	}
	return code + s + cReset
}

// Text-styling helpers — no-ops when color is disabled.
func Bold(s string) string    { return paint(cBold, s) }
func Dim(s string) string     { return paint(cDim, s) }
func Red(s string) string     { return paint(cRed, s) }
func Green(s string) string   { return paint(cGreen, s) }
func Yellow(s string) string  { return paint(cYellow, s) }
func Cyan(s string) string    { return paint(cCyan, s) }
func Magenta(s string) string { return paint(cMagenta, s) }

// --- message helpers -------------------------------------------------------

// Success prints a green check line to stdout.
func Success(format string, a ...any) {
	fmt.Fprintln(os.Stdout, Green("✓")+" "+fmt.Sprintf(format, a...))
}

// Info prints a cyan arrow line to stdout (progress / neutral status).
func Info(format string, a ...any) {
	fmt.Fprintln(os.Stdout, Cyan("→")+" "+fmt.Sprintf(format, a...))
}

// Warn prints a yellow bang line to stderr.
func Warn(format string, a ...any) {
	fmt.Fprintln(os.Stderr, Yellow("!")+" "+fmt.Sprintf(format, a...))
}

// Fail prints a red cross line to stderr.
func Fail(format string, a ...any) {
	fmt.Fprintln(os.Stderr, Red("✗")+" "+fmt.Sprintf(format, a...))
}

// Status returns the status string colored by outcome: green for success,
// red for failure, yellow for anything in-progress/pending.
func Status(s string) string {
	switch strings.ToLower(s) {
	case "succeeded", "running", "active", "healthy":
		return Green(s)
	case "failed", "error", "crashed", "unhealthy":
		return Red(s)
	default:
		return Yellow(s)
	}
}

// --- relative time ---------------------------------------------------------

// Age renders t as a short, human relative age ("just now", "5m", "3h", "2d").
func Age(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < 0:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// --- table -----------------------------------------------------------------

// Table is a minimal column-aligned table with a dimmed header row.
type Table struct {
	headers []string
	rows    [][]string
}

// NewTable starts a table with the given (uppercased) column headers.
func NewTable(headers ...string) *Table { return &Table{headers: headers} }

// Row appends one row; cells are formatted with %v and padded to the header count.
func (t *Table) Row(cells ...any) {
	r := make([]string, len(cells))
	for i, c := range cells {
		r[i] = fmt.Sprintf("%v", c)
	}
	t.rows = append(t.rows, r)
}

// Render writes the aligned table to w.
func (t *Table) Render(w io.Writer) {
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = len(h)
	}
	for _, r := range t.rows {
		for i, c := range r {
			if i < len(widths) && len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}
	// Header (dimmed).
	var b strings.Builder
	for i, h := range t.headers {
		b.WriteString(pad(h, widths[i], i == len(t.headers)-1))
	}
	fmt.Fprintln(w, Dim(strings.TrimRight(b.String(), " ")))
	for _, r := range t.rows {
		b.Reset()
		for i, c := range r {
			last := i == len(t.headers)-1
			b.WriteString(pad(c, widths[i], last))
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
}

// Print renders the table to stdout.
func (t *Table) Print() { t.Render(os.Stdout) }

func pad(s string, w int, last bool) string {
	if last {
		return s
	}
	if len(s) < w {
		s += strings.Repeat(" ", w-len(s))
	}
	return s + "  "
}

// --- confirm ---------------------------------------------------------------

// Confirm asks a yes/no question on stderr and reads the answer from stdin.
// A non-interactive stdin (no TTY) returns false — callers must pass --yes.
func Confirm(prompt string) bool {
	if !IsTerminal() {
		return false
	}
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes"
}
