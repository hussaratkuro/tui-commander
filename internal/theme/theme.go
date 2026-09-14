// Package theme maps HyDE Wallbash colors to a shared TUI palette.
package theme

import (
	"bufio"
	"encoding/hex"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const refreshInterval = 2 * time.Second

type Palette struct{ Base, Mantle, Crust, Surface0, Surface1, Surface2, Overlay0, Overlay1, Overlay2, Subtext0, Subtext1, Text, Lavender, Blue, Sapphire, Sky, Teal, Green, Yellow, Peach, Red, Mauve, Pink, Rosewater, OnAccent lipgloss.Color }
type ChangedMsg struct{ Palette Palette }

func Watch() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return ChangedMsg{Palette: Current()} })
}
func Current() Palette {
	f := CatppuccinMocha()
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TUI_THEME")), "catppuccin") {
		return f
	}
	v, ok := readColors(themePath())
	if !ok || v["wallbash_pry1"] == "" || v["wallbash_txt1"] == "" {
		return f
	}
	p := Palette{Base: color(v, "wallbash_pry1", f.Base), Mantle: color(v, "wallbash_pry2", f.Mantle), Crust: color(v, "wallbash_1xa1", f.Crust), Surface0: color(v, "wallbash_1xa2", f.Surface0), Surface1: color(v, "wallbash_1xa3", f.Surface1), Surface2: color(v, "wallbash_1xa4", f.Surface2), Overlay0: color(v, "wallbash_1xa5", f.Overlay0), Overlay1: color(v, "wallbash_1xa6", f.Overlay1), Overlay2: color(v, "wallbash_1xa7", f.Overlay2), Subtext0: color(v, "wallbash_1xa8", f.Subtext0), Subtext1: color(v, "wallbash_1xa9", f.Subtext1), Text: color(v, "wallbash_txt1", f.Text), Lavender: color(v, "wallbash_3xa8", f.Lavender), Blue: color(v, "wallbash_3xa7", f.Blue), Sapphire: color(v, "wallbash_3xa8", f.Sapphire), Sky: color(v, "wallbash_2xa8", f.Sky), Teal: color(v, "wallbash_2xa7", f.Teal), Green: color(v, "wallbash_2xa9", f.Green), Yellow: color(v, "wallbash_1xa8", f.Yellow), Peach: color(v, "wallbash_1xa9", f.Peach), Red: color(v, "wallbash_4xa8", f.Red), Mauve: color(v, "wallbash_3xa8", f.Mauve), Pink: color(v, "wallbash_2xa8", f.Pink), Rosewater: color(v, "wallbash_4xa9", f.Rosewater)}
	p.OnAccent = contrasting(string(p.Mauve))
	return p
}
func CatppuccinMocha() Palette {
	return Palette{Base: "#1e1e2e", Mantle: "#181825", Crust: "#11111b", Surface0: "#313244", Surface1: "#45475a", Surface2: "#585b70", Overlay0: "#6c7086", Overlay1: "#7f849c", Overlay2: "#9399b2", Subtext0: "#a6adc8", Subtext1: "#bac2de", Text: "#cdd6f4", Lavender: "#b4befe", Blue: "#89b4fa", Sapphire: "#74c7ec", Sky: "#89dceb", Teal: "#94e2d5", Green: "#a6e3a1", Yellow: "#f9e2af", Peach: "#fab387", Red: "#f38ba8", Mauve: "#cba6f7", Pink: "#f5c2e7", Rosewater: "#f5e0dc", OnAccent: "#1e1e2e"}
}
func themePath() string {
	if p := strings.TrimSpace(os.Getenv("TUI_THEME_FILE")); p != "" {
		return p
	}
	c := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME"))
	if c == "" {
		if h, e := os.UserHomeDir(); e == nil {
			c = filepath.Join(h, ".cache")
		}
	}
	return filepath.Join(c, "hyde", "wallbash", "shell-colors")
}
func readColors(path string) (map[string]string, bool) {
	f, e := os.Open(path)
	if e != nil {
		return nil, false
	}
	defer f.Close()
	v := make(map[string]string)
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, r, ok := strings.Cut(line, "=")
		if !ok || strings.HasSuffix(k, "_rgba") {
			continue
		}
		fields := strings.Fields(r)
		if len(fields) == 0 {
			continue
		}
		r = strings.Trim(fields[0], "'\"")
		if n, ok := normalizeHex(r); ok {
			v[strings.TrimSpace(k)] = n
		}
	}
	return v, s.Err() == nil
}
func normalizeHex(v string) (string, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "#")
	if len(v) != 6 {
		return "", false
	}
	if _, e := hex.DecodeString(v); e != nil {
		return "", false
	}
	return "#" + strings.ToUpper(v), true
}
func color(v map[string]string, k string, f lipgloss.Color) lipgloss.Color {
	if x := v[k]; x != "" {
		return lipgloss.Color(x)
	}
	return f
}
func contrasting(v string) lipgloss.Color {
	b, e := hex.DecodeString(strings.TrimPrefix(v, "#"))
	if e != nil || len(b) != 3 {
		return "#111111"
	}
	if (299*int(b[0])+587*int(b[1])+114*int(b[2]))/1000 >= 150 {
		return "#111111"
	}
	return "#ffffff"
}
