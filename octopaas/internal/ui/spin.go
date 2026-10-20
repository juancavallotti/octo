package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
)

// Spinner is an animation for something we are waiting on.
type Spinner struct {
	frames []string
	fps    time.Duration
}

// The spinners, each for a kind of wait.
var (
	// Tentacles is the everyday one: the octopus waving while the API thinks.
	Tentacles = Spinner{frames: []string{`~(°▿°)~`, `≈(°▿°)≈`, `∿(°▿°)∿`, `≈(°▿°)≈`}, fps: 160 * time.Millisecond}
	// Bubbles rise while something goes up to the platform.
	Bubbles = Spinner{frames: []string{`  .  `, ` .o  `, ` o°. `, `.°O o`, `°O o.`, `O o°.`, ` o°. `, `  °  `}, fps: 120 * time.Millisecond}
	// Ink spreads while something comes down from it.
	Ink = Spinner{frames: []string{`▁▂▃`, `▂▃▅`, `▃▅▆`, `▅▆▇`, `▆▇▆`, `▇▆▅`, `▆▅▃`, `▅▃▂`, `▃▂▁`}, fps: 90 * time.Millisecond}
	// Rocket flies while a deployment rolls out.
	Rocket = Spinner{frames: rocketFrames(), fps: 80 * time.Millisecond}
)

func rocketFrames() []string {

	const lane = 12
	ret := make([]string, 0, lane)
	for i := range lane {
		trail := strings.Repeat("~", min(i, 3))
		ret = append(ret, fmt.Sprintf("%-*s", lane+4, strings.Repeat(" ", max(i-3, 0))+trail+"=>"))
	}
	return ret
}

// Live is a line that is being waited on. A step can retitle it or report what
// it sees while it waits.
type Live struct {
	u      *UI
	mu     sync.Mutex
	title  string
	detail string
	ratio  float64
	bytes  string
}

// Title changes what the line says we are doing.
func (l *Live) Title(s string) {

	l.mu.Lock()
	defer l.mu.Unlock()
	l.title = s
}

// Detail is what the wait looks like right now, shown after the title. Without a
// terminal, each new detail is written as a line, so a CI log still tells the
// story of a slow deploy.
func (l *Live) Detail(s string) {

	l.mu.Lock()
	changed := s != l.detail
	l.detail = s
	title := l.title
	l.mu.Unlock()

	if changed && !l.u.Fancy && s != "" {
		l.u.Say("%s: %s", title, stripStyles(s))
	}
}

// Transferred reports bytes moved of total (-1 when unknown).
func (l *Live) Transferred(done, total int64) {

	l.mu.Lock()
	defer l.mu.Unlock()
	if total > 0 {
		l.ratio = float64(done) / float64(total)
		l.bytes = Bytes(done) + " / " + Bytes(total)
	} else {
		l.ratio = -1
		l.bytes = Bytes(done)
	}
}

// Spin runs step while the spinner plays on stderr. Without a terminal, step
// just runs.
func (u *UI) Spin(s Spinner, title string, step func(*Live) error) error {

	live := &Live{u: u, title: title, ratio: -2}
	if !u.Fancy {
		return step(live)
	}

	start := time.Now()
	stop := make(chan struct{})
	stopped := make(chan struct{})
	bar := progress.New(progress.WithGradient("#4FE0FF", "#7A3DD6"), progress.WithWidth(24), progress.WithoutPercentage())
	frame := u.err.NewStyle().Foreground(coral).Bold(true)
	quiet := u.err.NewStyle().Foreground(mist)

	go func() {

		defer close(stopped)
		_, _ = fmt.Fprint(u.Err, "\x1b[?25l")
		ticker := time.NewTicker(s.fps)
		defer ticker.Stop()
		for i := 0; ; i++ {
			live.mu.Lock()
			line := frame.Render(s.frames[i%len(s.frames)]) + " " + live.title
			if live.ratio >= 0 {
				line += "  " + bar.ViewAs(live.ratio)
			}
			if live.bytes != "" {
				line += "  " + quiet.Render(live.bytes)
			}
			if live.detail != "" {
				line += "  " + live.detail
			}
			live.mu.Unlock()
			_, _ = fmt.Fprint(u.Err, "\r\x1b[K"+truncate(line, u.Width()-1))

			select {
			case <-stop:
				_, _ = fmt.Fprint(u.Err, "\r\x1b[K\x1b[?25h")
				return
			case <-ticker.C:
			}
		}
	}()

	err := step(live)
	close(stop)
	<-stopped

	//a wait that worked leaves nothing behind: the command says what came of
	//it. One that failed leaves its line, so you can see which step it was.
	if err != nil {
		took := quiet.Render(fmt.Sprintf("(%s)", time.Since(start).Round(100*time.Millisecond)))
		u.Say("%s %s %s", u.err.NewStyle().Foreground(reef).Bold(true).Render("✗"), live.title, took)
	}
	return err
}

// truncate cuts a styled line to width visible cells, so a narrow terminal does
// not wrap the spinner onto a new line every frame.
func truncate(s string, width int) string {

	if lipgloss.Width(s) <= width {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

func stripStyles(s string) string {

	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Bytes is n in the unit a person would use.
func Bytes(n int64) string {

	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
