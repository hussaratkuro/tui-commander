package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type Options struct {
	Left  string
	Right string
}

const Usage = `tui-commander - two-pane local and remote file manager

Usage:
  tui-commander [LEFT [RIGHT]]
  tui-commander --left LOCATION --right LOCATION

Locations:
  /local/path
  ftp://user@host/path
  ftps://user@host/path              explicit TLS
  ftpes://user@host/path             explicit TLS alias
  ftps+implicit://user@host/path     implicit TLS
  sftp://user@host/path
  smb://[DOMAIN;]user@host/share/path

Passwords may be entered in the connection prompt and are saved only when
explicitly enabled while creating a bookmark.
`

var errHelp = errors.New("help requested")

func IsHelp(err error) bool { return errors.Is(err, errHelp) }

func ParseArgs(args []string) (Options, error) {
	var options Options
	var positional []string
	for index := 0; index < len(args); index++ {
		argument := args[index]
		value := func() (string, error) {
			if index+1 >= len(args) {
				return "", fmt.Errorf("%s requires a location", argument)
			}
			index++
			return args[index], nil
		}
		switch {
		case argument == "-h" || argument == "--help" || argument == "help":
			return Options{}, errHelp
		case argument == "--left":
			result, err := value()
			if err != nil {
				return Options{}, err
			}
			options.Left = result
		case strings.HasPrefix(argument, "--left="):
			options.Left = strings.TrimPrefix(argument, "--left=")
		case argument == "--right":
			result, err := value()
			if err != nil {
				return Options{}, err
			}
			options.Right = result
		case strings.HasPrefix(argument, "--right="):
			options.Right = strings.TrimPrefix(argument, "--right=")
		case strings.HasPrefix(argument, "-"):
			return Options{}, fmt.Errorf("unknown option: %s", argument)
		default:
			positional = append(positional, argument)
		}
	}
	if len(positional) > 2 {
		return Options{}, fmt.Errorf("expected at most two locations")
	}
	if options.Left != "" && len(positional) > 0 || options.Right != "" && len(positional) > 1 {
		return Options{}, fmt.Errorf("do not mix positional locations with the corresponding named option")
	}
	if options.Left == "" && len(positional) > 0 {
		options.Left = positional[0]
	}
	if options.Right == "" && len(positional) > 1 {
		options.Right = positional[1]
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return Options{}, err
	}
	if options.Left == "" {
		options.Left = workingDirectory
	}
	if options.Right == "" {
		options.Right = options.Left
	}
	return options, nil
}
