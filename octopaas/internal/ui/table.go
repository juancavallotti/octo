package ui

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// Table writes rows to stdout under headers: a rounded, coloured table for
// somebody looking, aligned columns for everything else, so `awk` still works.
func (u *UI) Table(headers []string, rows [][]string) {

	if len(rows) == 0 {
		u.Say("%s", u.err.NewStyle().Foreground(mist).Italic(true).Render("  (nothing here yet)"))
		return
	}
	if !isTerminal(u.Out) || !u.Fancy {
		w := tabwriter.NewWriter(u.Out, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, strings.Join(headers, "\t"))
		for _, row := range rows {
			plain := make([]string, len(row))
			for i, cell := range row {
				plain[i] = stripStyles(cell)
			}
			_, _ = fmt.Fprintln(w, strings.Join(plain, "\t"))
		}
		_ = w.Flush()
		return
	}

	head := u.out.NewStyle().Foreground(coral).Bold(true).Padding(0, 1)
	cell := u.out.NewStyle().Padding(0, 1)
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(u.out.NewStyle().Foreground(ink)).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return head
			}
			return cell
		})
	_, _ = fmt.Fprintln(u.Out, t.Render())
}

// Fields writes label/value pairs to stdout, one per line, labels aligned.
func (u *UI) Fields(pairs [][2]string) {

	width := 0
	for _, p := range pairs {
		width = max(width, len(p[0]))
	}
	label := u.out.NewStyle().Foreground(ocean).Bold(true)
	for _, p := range pairs {
		_, _ = fmt.Fprintf(u.Out, "%s  %s\n", label.Render(fmt.Sprintf("%-*s", width, p[0])), p[1])
	}
}

// Status renders a deployment status as a pill, coloured by how it is going.
func (u *UI) Status(status string) string {

	if !isTerminal(u.Out) || !u.Fancy {
		return status
	}
	bg := mist
	switch strings.ToLower(status) {
	case "running", "ready", "healthy":
		bg = kelp
	case "pending", "deploying", "rolling", "scaling", "starting", "progressing":
		bg = sand
	case "failed", "error", "crashloopbackoff", "degraded":
		bg = reef
	case "stopped", "deleted":
		bg = ink
	}
	return u.out.NewStyle().Background(bg).Foreground(lipgloss.Color("#1A1B26")).Bold(true).Padding(0, 1).Render(status)
}

// Box frames a summary on stderr: what just happened, at a glance.
func (u *UI) Box(title string, lines ...string) {

	if !u.Fancy {
		u.Say("%s", title)
		for _, l := range lines {
			u.Say("  %s", stripStyles(l))
		}
		return
	}
	body := u.err.NewStyle().Foreground(coral).Bold(true).Render(title)
	if len(lines) > 0 {
		body += "\n\n" + strings.Join(lines, "\n")
	}
	u.Say("%s", u.err.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ink).Padding(0, 2).Render(body))
}
