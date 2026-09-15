package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// zoomKey reports +1 for Ctrl+Numpad+ and -1 for Ctrl+Numpad-. Bubble Tea v1
// exposes these modified keys only as unrecognised CSI sequences, so the Kitty
// keypad codes, the plain CSI u form, and xterm's modifyOtherKeys encoding are
// all decoded here.
func zoomKey(message tea.Msg) int {
	switch unknownCSISequence(message) {
	case "57413;5u", // Kitty KP_ADD
		"43;5u",    // CSI u '+'
		"27;5;43~": // modifyOtherKeys '+'
		return 1
	case "57412;5u", // Kitty KP_SUBTRACT
		"45;5u",    // CSI u '-'
		"27;5;45~": // modifyOtherKeys '-'
		return -1
	default:
		return 0
	}
}

// unknownCSISequence turns Bubble Tea's "?CSI[51 59 53 126]?" debug string back
// into the parameter text that followed ESC [, e.g. "3;5~".
func unknownCSISequence(message tea.Msg) string {
	stringer, ok := message.(fmt.Stringer)
	if !ok {
		return ""
	}
	text := stringer.String()
	if !strings.HasPrefix(text, "?CSI[") || !strings.HasSuffix(text, "]?") {
		return ""
	}
	var sequence strings.Builder
	for _, field := range strings.Fields(text[len("?CSI[") : len(text)-len("]?")]) {
		value, err := strconv.Atoi(field)
		if err != nil {
			return ""
		}
		sequence.WriteByte(byte(value))
	}
	return sequence.String()
}

// zoomFont changes the terminal font size. The font belongs to the terminal
// emulator, so the request is sent through kitty's remote control protocol on
// the controlling terminal; kitty only honours it when allow_remote_control is
// enabled in kitty.conf.
func (m *Model) zoomFont(direction int) tea.Cmd {
	if os.Getenv("KITTY_WINDOW_ID") == "" && !strings.Contains(os.Getenv("TERM"), "kitty") {
		m.setStatus("Zoom is only available inside the kitty terminal", true)
		return nil
	}
	operation, label := "+", "Zoom in"
	if direction < 0 {
		operation, label = "-", "Zoom out"
	}
	if err := sendKittyCommand("set-font-size", map[string]any{"size": 1.0, "all": false, "increment_op": operation}); err != nil {
		m.setStatus(label+": "+err.Error(), true)
		return nil
	}
	m.setStatus(label+" · requires allow_remote_control yes in kitty.conf", false)
	return nil
}

func sendKittyCommand(name string, payload map[string]any) error {
	body, err := json.Marshal(map[string]any{
		"cmd": name, "version": []int{0, 14, 2}, "no_response": true, "payload": payload,
	})
	if err != nil {
		return err
	}
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer tty.Close()
	// One write keeps the DCS sequence contiguous next to Bubble Tea's own output.
	_, err = tty.Write([]byte("\x1bP@kitty-cmd" + string(body) + "\x1b\\"))
	return err
}
