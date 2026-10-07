package cli

import (
	"io"
	"os"

	"github.com/gremlyn-ai/gremlyn/internal/arena/scoring"
)

type palette struct{ on bool }

func paletteFor(w io.Writer) palette {
	if os.Getenv("NO_COLOR") != "" {
		return palette{}
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return palette{on: true}
	}
	f, ok := w.(*os.File)
	if !ok {
		return palette{}
	}
	fi, err := f.Stat()
	return palette{on: err == nil && fi.Mode()&os.ModeCharDevice != 0}
}

func (p palette) paint(code, s string) string {
	if !p.on {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p palette) pass(s string) string { return p.paint("1;32", s) }
func (p palette) fail(s string) string { return p.paint("1;31", s) }
func (p palette) bad(s string) string  { return p.paint("31", s) }
func (p palette) dim(s string) string  { return p.paint("2", s) }
func (p palette) bold(s string) string { return p.paint("1", s) }

func (p palette) grade(g scoring.Grade, s string) string {
	switch g {
	case scoring.GradeExcellent, scoring.GradeGood:
		return p.paint("32", s)
	case scoring.GradeNeedsWork:
		return p.paint("33", s)
	case scoring.GradeCritical:
		return p.paint("31", s)
	default:
		return p.paint("2", s)
	}
}
