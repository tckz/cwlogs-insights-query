package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	"github.com/fatih/color"
)

// colorMode controls whether diff output is colorized: auto, always or never.
type colorMode string

const (
	colorAuto   colorMode = "auto"
	colorAlways colorMode = "always"
	colorNever  colorMode = "never"
)

func (m *colorMode) String() string { return string(*m) }

func (m *colorMode) Set(v string) error {
	switch colorMode(v) {
	case colorAuto, colorAlways, colorNever:
		*m = colorMode(v)
		return nil
	}
	return fmt.Errorf("must be one of auto, always, never")
}

// apply configures github.com/fatih/color globally.
// In auto mode, the library's own detection is kept: color is disabled when stdout is not a terminal or NO_COLOR is set.
func (m colorMode) apply() {
	switch m {
	case colorAlways:
		color.NoColor = false
	case colorNever:
		color.NoColor = true
	}
}

var (
	diffHeaderColor  = color.New(color.Bold)
	diffHunkColor    = color.New(color.FgCyan)
	diffDeleteColor  = color.New(color.FgRed)
	diffInsertColor  = color.New(color.FgGreen)
	diffNoNewlineMsg = color.New(color.FgYellow)
)

// writeUnified writes u in the standard unified diff format, the same as u.String(),
// colorized with github.com/fatih/color unless color.NoColor is set.
func writeUnified(w io.Writer, u udiff.UnifiedDiff) error {
	if len(u.Hunks) == 0 {
		return nil
	}

	// paintLine colorizes a line while keeping the trailing newline outside the escape sequence.
	paintLine := func(c *color.Color, prefix, content string) string {
		body := strings.TrimSuffix(content, "\n")
		s := prefix + body
		if c != nil {
			s = c.Sprint(s)
		}
		if strings.HasSuffix(content, "\n") {
			s += "\n"
		}
		return s
	}

	b := new(strings.Builder)
	fmt.Fprintf(b, "%s\n", diffHeaderColor.Sprint("--- "+u.From))
	fmt.Fprintf(b, "%s\n", diffHeaderColor.Sprint("+++ "+u.To))
	for _, hunk := range u.Hunks {
		fromCount, toCount := 0, 0
		for _, l := range hunk.Lines {
			switch l.Kind {
			case udiff.Delete:
				fromCount++
			case udiff.Insert:
				toCount++
			default:
				fromCount++
				toCount++
			}
		}
		hb := new(strings.Builder)
		fmt.Fprint(hb, "@@")
		if fromCount > 1 {
			fmt.Fprintf(hb, " -%d,%d", hunk.FromLine, fromCount)
		} else if hunk.FromLine == 1 && fromCount == 0 {
			// Match odd GNU diff -u behavior adding to empty file.
			fmt.Fprintf(hb, " -0,0")
		} else {
			fmt.Fprintf(hb, " -%d", hunk.FromLine)
		}
		if toCount > 1 {
			fmt.Fprintf(hb, " +%d,%d", hunk.ToLine, toCount)
		} else if hunk.ToLine == 1 && toCount == 0 {
			// Match odd GNU diff -u behavior adding to empty file.
			fmt.Fprintf(hb, " +0,0")
		} else {
			fmt.Fprintf(hb, " +%d", hunk.ToLine)
		}
		fmt.Fprint(hb, " @@")
		fmt.Fprintf(b, "%s\n", diffHunkColor.Sprint(hb.String()))

		for _, l := range hunk.Lines {
			switch l.Kind {
			case udiff.Delete:
				fmt.Fprint(b, paintLine(diffDeleteColor, "-", l.Content))
			case udiff.Insert:
				fmt.Fprint(b, paintLine(diffInsertColor, "+", l.Content))
			default:
				fmt.Fprint(b, paintLine(nil, " ", l.Content))
			}
			if !strings.HasSuffix(l.Content, "\n") {
				fmt.Fprintf(b, "\n%s\n", diffNoNewlineMsg.Sprint(`\ No newline at end of file`))
			}
		}
	}

	_, err := io.WriteString(w, b.String())
	return err
}
