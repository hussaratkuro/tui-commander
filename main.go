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
	_, runErr := program.Run()
	closeErr := model.Close()
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "tui-commander:", runErr)
		os.Exit(1)
	}
	if closeErr != nil {
		fmt.Fprintln(os.Stderr, "tui-commander: save session or close resources:", closeErr)
		os.Exit(1)
	}
}
