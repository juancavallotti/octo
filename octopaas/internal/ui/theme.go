package ui

import "github.com/charmbracelet/lipgloss"

// The palette: an octopus at the bottom of a reef. Adaptive, so it reads on a
// light terminal as well as a dark one.
var (
	coral = lipgloss.AdaptiveColor{Light: "#1C7ED6", Dark: "#4FE0FF"}
	ink   = lipgloss.AdaptiveColor{Light: "#5F3DC4", Dark: "#B197FC"}
	ocean = lipgloss.AdaptiveColor{Light: "#0B7285", Dark: "#3BC9DB"}
	kelp  = lipgloss.AdaptiveColor{Light: "#2B8A3E", Dark: "#69DB7C"}
	sand  = lipgloss.AdaptiveColor{Light: "#E67700", Dark: "#FFD43B"}
	reef  = lipgloss.AdaptiveColor{Light: "#C92A2A", Dark: "#FF6B6B"}
	mist  = lipgloss.AdaptiveColor{Light: "#868E96", Dark: "#868E96"}
)

// gradient is the banner's colours, top to bottom, taken from the logo: a cyan
// head shading through blue into violet tentacles.
var gradient = []string{"#7DF9FF", "#4FE0FF", "#27C4FF", "#1EA7FF", "#2F8BFF", "#3D72FF", "#4C5CF5", "#5B4FEA", "#6A45E0", "#7A3DD6"}
