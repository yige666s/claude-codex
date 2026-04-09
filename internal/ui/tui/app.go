package tui

import tea "github.com/charmbracelet/bubbletea"

func Run(options Options) error {
	model := newModel(options)
	program := tea.NewProgram(
		model,
		tea.WithInput(options.Input),
		tea.WithOutput(options.Output),
		tea.WithAltScreen(), // use alternate screen buffer so terminal history stays clean
	)
	_, err := program.Run()
	return err
}
