package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"tui-commander/internal/ui"
)

func main() {
	options, err := ui.ParseArgs(os.Args[1:])
	if err != nil {
		if ui.IsHelp(err) {
			fmt.Print(ui.Usage)
			return
		}
		fmt.Fprintln(os.Stderr, "tui-commander:", err)
		fmt.Fprint(os.Stderr, ui.Usage)
		os.Exit(2)
	}

	model, err := ui.New(options)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tui-commander:", err)
		os.Exit(1)
	}
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui-commander:", err)
		os.Exit(1)
	}
}
