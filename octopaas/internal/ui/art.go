package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The octopus, drawn after the logo: a round head, slanted eyes, and tentacles
// curling out to circuit nodes. Plain ASCII on purpose: it has to survive every
// terminal font, a Windows console and a CI log viewer.
var octopus = []string{
	`                .-''''''''-.`,
	`             .'              '.`,
	`            /                  \`,
	`           |                    |`,
	`           |     \\      //     |`,
	`            \     \\    //     /`,
	`   o-.       '.              .'       .-o`,
	`      \    .--''-.        .-''--.    /`,
	`  o.   '--'   .-' '-.  .-' '-.   '--'   .o`,
	`   '-._    .-'  .-' |  | '-.  '-.    _.-'`,
	` o-.   '--'   .'   /    \   '.   '--'   .-o`,
	`    '-.____.-'   .'      '.   '-.____.-'`,
	`               o'          'o`,
}

// letters is just enough of figlet's standard font to spell the name. Each
// letter is six rows; word pads them so the columns line up.
var letters = map[rune][]string{
	'o': {`       `, `  ___  `, ` / _ \ `, `| (_) |`, ` \___/ `, `       `},
	'c': {`      `, `  ___ `, ` / __|`, `| (__ `, ` \___|`, `      `},
	't': {` _   `, `| |_ `, `| __|`, `| |_ `, ` \__|`, `     `},
	'p': {`       `, ` _ __  `, `| '_ \ `, `| |_) |`, `| .__/ `, `|_|    `},
	'a': {`       `, `  __ _ `, ` / _' |`, `| (_| |`, ` \__,_|`, `       `},
	's': {`     `, ` ___ `, `/ __|`, `\__ \`, `|___/`, `     `},
}

// word sets s in the banner font. Letters it does not know are skipped.
func word(s string) []string {

	ret := make([]string, 6)
	for _, r := range s {
		glyph, ok := letters[r]
		if !ok {
			continue
		}
		for row := range ret {
			ret[row] += glyph[row]
		}
	}
	return ret
}

// Banner is the octopus over the name, shaded from the surface to the deep,
// with a tagline under it. It is only drawn for somebody watching.
func (u *UI) Banner(tagline string) {

	if !u.Fancy {
		return
	}
	lines := append(append([]string{}, octopus...), word("octopaas")...)
	for i, line := range lines {
		color := gradient[i*len(gradient)/len(lines)]
		u.Say("%s", u.err.NewStyle().Foreground(lipgloss.Color(color)).Bold(true).Render(line))
	}
	if tagline != "" {
		u.Say("%s", u.err.NewStyle().Foreground(mist).Italic(true).Render("   "+tagline))
	}
	u.Say("")
}

// Mood is how the little octopus feels about what just happened.
type Mood int

// The moods there are art for.
const (
	Happy Mood = iota
	Confused
	Sleepy
	Searching
	Sad
	Lost
)

var eyes = map[Mood]string{
	Happy:     "^  ^",
	Confused:  "o  O",
	Sleepy:    "-  -",
	Searching: "°  °",
	Sad:       "T  T",
	Lost:      "@  @",
}

var mouths = map[Mood]string{
	Happy:     ` \__/ `,
	Confused:  `  ~~  `,
	Sleepy:    `  zz  `,
	Searching: `  ()  `,
	Sad:       `  /\  `,
	Lost:      `  ~~  `,
}

// Critter is the small octopus, feeling mood, as lines.
func Critter(mood Mood) []string {
	return []string{
		`    .-''-.`,
		`   ( ` + eyes[mood] + ` )`,
		`o-.)` + mouths[mood] + `(.-o`,
		`  o'/||\'o`,
	}
}

// Moment draws the critter beside a message and its hint: a success worth
// celebrating, or a failure worth explaining. Without a terminal it is just the
// two lines of text.
func (u *UI) Moment(mood Mood, message, hint string) {

	if !u.Fancy {
		u.Say("%s", message)
		if hint != "" {
			u.Say("%s", hint)
		}
		return
	}

	color := coral
	switch mood {
	case Happy:
		color = kelp
	case Sad, Lost:
		color = reef
	case Confused, Searching:
		color = sand
	case Sleepy:
		color = ink
	}
	art := u.err.NewStyle().Foreground(color).Bold(true).Render(strings.Join(Critter(mood), "\n"))
	text := u.err.NewStyle().Bold(true).Render(message)
	if hint != "" {
		text += "\n" + u.err.NewStyle().Foreground(mist).Italic(true).Render(hint)
	}
	text = u.err.NewStyle().PaddingLeft(2).PaddingTop(1).Width(max(u.Width()-16, 20)).Render(text)
	u.Say("%s\n", lipgloss.JoinHorizontal(lipgloss.Top, art, text))
}
