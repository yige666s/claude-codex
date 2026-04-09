package tui

import "github.com/charmbracelet/lipgloss"

type themeStyles struct {
	app            lipgloss.Style
	header         lipgloss.Style
	subtle         lipgloss.Style
	user           lipgloss.Style
	assistant      lipgloss.Style
	tool           lipgloss.Style
	errorText      lipgloss.Style
	input          lipgloss.Style
	status         lipgloss.Style
	modal          lipgloss.Style
	diff           lipgloss.Style
	userBlock      lipgloss.Style
	assistantBlock lipgloss.Style
	border         lipgloss.Color
	background     lipgloss.Color
}

func NormalizeTheme(value string) string {
	switch value {
	case "light", "dark":
		return value
	default:
		return "dark"
	}
}

func stylesForTheme(theme string) themeStyles {
	theme = NormalizeTheme(theme)
	if theme == "light" {
		border := lipgloss.Color("#5F5F87")
		return themeStyles{
			app:            lipgloss.NewStyle().Padding(1, 2).Foreground(lipgloss.Color("#1F1F1F")).Background(lipgloss.Color("#F7F2E8")),
			header:         lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#2A4B7C")),
			subtle:         lipgloss.NewStyle().Foreground(lipgloss.Color("#6C6C6C")),
			user:           lipgloss.NewStyle().Foreground(lipgloss.Color("#7A2E2E")).Bold(true),
			assistant:      lipgloss.NewStyle().Foreground(lipgloss.Color("#244A2C")),
			tool:           lipgloss.NewStyle().Foreground(lipgloss.Color("#4A3267")),
			errorText:      lipgloss.NewStyle().Foreground(lipgloss.Color("#A12727")).Bold(true),
			input:          lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border),
			status:         lipgloss.NewStyle().Padding(0, 1).Background(lipgloss.Color("#E4D8C4")).Foreground(lipgloss.Color("#2B2B2B")),
			modal:          lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(border).Padding(1, 2).Background(lipgloss.Color("#FFF8EE")),
			diff:           lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1),
			userBlock:      lipgloss.NewStyle().Background(lipgloss.Color("#EDE4D2")).Foreground(lipgloss.Color("#4A2020")).Padding(0, 1).MarginBottom(1),
			assistantBlock: lipgloss.NewStyle().Background(lipgloss.Color("#D8EDE0")).Foreground(lipgloss.Color("#1A3A24")).Padding(0, 1).MarginBottom(1),
			border:         border,
			background:     lipgloss.Color("#F7F2E8"),
		}
	}

	border := lipgloss.Color("#8C7A58")
	return themeStyles{
		app:            lipgloss.NewStyle().Padding(1, 2).Foreground(lipgloss.Color("#F5E9D8")).Background(lipgloss.Color("#161413")),
		header:         lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#D5B06B")),
		subtle:         lipgloss.NewStyle().Foreground(lipgloss.Color("#B29F88")),
		user:           lipgloss.NewStyle().Foreground(lipgloss.Color("#F28C6F")).Bold(true),
		assistant:      lipgloss.NewStyle().Foreground(lipgloss.Color("#CFE4A7")),
		tool:           lipgloss.NewStyle().Foreground(lipgloss.Color("#92C8D7")),
		errorText:      lipgloss.NewStyle().Foreground(lipgloss.Color("#FF7A7A")).Bold(true),
		input:          lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border),
		status:         lipgloss.NewStyle().Padding(0, 1).Background(lipgloss.Color("#2B2420")).Foreground(lipgloss.Color("#F0E3D0")),
		modal:          lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(border).Padding(1, 2).Background(lipgloss.Color("#231D1A")),
		diff:           lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1),
		// user messages: warm dark amber background；assistant: cool dark teal background
		userBlock:      lipgloss.NewStyle().Background(lipgloss.Color("#2A1F0E")).Foreground(lipgloss.Color("#F5D9B0")).Padding(0, 1).MarginBottom(1),
		assistantBlock: lipgloss.NewStyle().Background(lipgloss.Color("#0E1F1A")).Foreground(lipgloss.Color("#B8E8C8")).Padding(0, 1).MarginBottom(1),
		border:         border,
		background:     lipgloss.Color("#161413"),
	}
}
