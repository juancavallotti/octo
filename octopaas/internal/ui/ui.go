// Package ui is how octopaas looks: the octopus, the spinners, the tables and
// the colours, and the discipline that keeps all of it out of the way.
//
// Two rules hold everything up. Data goes to stdout and nothing else does, so
// `octopaas ... -o json | jq` always works. And every effect switches itself off
// when nobody is watching: no terminal on stderr, CI set, NO_COLOR set, --plain,
// or machine output asked for. Plain mode is not a degraded mode; it is the one
// scripts and logs get, and it says the same things in fewer colours.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// UI is a command's two outputs and how dressed up they may be.
type UI struct {
	Out io.Writer
	Err io.Writer

	// Fancy is whether animations and art go to Err.
	Fancy bool

	out *lipgloss.Renderer
	err *lipgloss.Renderer
}

// Options say what the command line and the environment asked for.
type Options struct {
	Plain   bool
	Machine bool
	Getenv  func(string) string
}

// New dresses out and err according to opts and to what they are connected to.
func New(out, err io.Writer, opts Options) *UI {

	quiet := opts.Plain || opts.Machine || opts.Getenv("CI") != "" || opts.Getenv("NO_COLOR") != "" || opts.Getenv("TERM") == "dumb"
	ret := &UI{
		Out:   out,
		Err:   err,
		Fancy: !quiet && isTerminal(err),
		out:   lipgloss.NewRenderer(out),
		err:   lipgloss.NewRenderer(err),
	}
	//colour follows the same switch as animation, per stream: a table piped into
	//a file is plain even when you are watching stderr.
	if quiet || !isTerminal(out) {
		ret.out.SetColorProfile(termenv.Ascii)
	}
	if !ret.Fancy {
		ret.err.SetColorProfile(termenv.Ascii)
	}

	//adaptive colours would ask the terminal for its background and wait for
	//an answer some terminals never send. We decide instead: dark, unless
	//COLORFGBG says the background is light.
	dark := darkBackground(opts.Getenv("COLORFGBG"))
	ret.out.SetHasDarkBackground(dark)
	ret.err.SetHasDarkBackground(dark)
	lipgloss.SetHasDarkBackground(dark)
	return ret
}

// darkBackground reads COLORFGBG ("fg;bg", where a bg of 7 or 15 is light), and
// assumes dark when it says nothing.
func darkBackground(colorfgbg string) bool {

	i := strings.LastIndexByte(colorfgbg, ';')
	if i < 0 {
		return true
	}
	bg := colorfgbg[i+1:]
	return bg != "7" && bg != "15"
}

func isTerminal(w io.Writer) bool {

	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// Width is the terminal's width on stderr, or 80 when it does not say.
func (u *UI) Width() int {

	if f, ok := u.Err.(*os.File); ok {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
			return w
		}
	}
	return 80
}

// Say writes a line of commentary to stderr. Commentary is never data.
func (u *UI) Say(format string, args ...any) {
	_, _ = fmt.Fprintf(u.Err, format+"\n", args...)
}

// Print writes data to stdout.
func (u *UI) Print(format string, args ...any) {
	_, _ = fmt.Fprintf(u.Out, format+"\n", args...)
}

// Success is a one-line win, with a tick.
func (u *UI) Success(format string, args ...any) {
	u.Say("%s %s", u.err.NewStyle().Foreground(kelp).Bold(true).Render("✓"), fmt.Sprintf(format, args...))
}

// Warn is a one-line heads-up.
func (u *UI) Warn(format string, args ...any) {
	u.Say("%s %s", u.err.NewStyle().Foreground(sand).Bold(true).Render("!"), fmt.Sprintf(format, args...))
}

// Hint is a quieter line, for what to try next.
func (u *UI) Hint(format string, args ...any) {
	u.Say("%s", u.err.NewStyle().Foreground(mist).Italic(true).Render("  "+fmt.Sprintf(format, args...)))
}

// Bold renders s emphasised for stderr.
func (u *UI) Bold(s string) string {
	return u.err.NewStyle().Bold(true).Render(s)
}

// Accent renders s in the octopus's colour for stderr.
func (u *UI) Accent(s string) string {
	return u.err.NewStyle().Foreground(coral).Bold(true).Render(s)
}

// Dim renders s quietly for stdout, where most of it ends up.
func (u *UI) Dim(s string) string {
	return u.out.NewStyle().Foreground(mist).Render(s)
}
