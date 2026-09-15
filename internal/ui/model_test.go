package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tui-commander/internal/config"
	"tui-commander/internal/vfs"
)

type promptCSIMessage string

func (message promptCSIMessage) String() string { return string(message) }

func TestModelRendersTwoLocalPanes(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	for index := range model.panes {
		message := model.loadPaneCmd(index)()
		model.Update(message)
	}
	view := model.View()
	if strings.Count(view, "· Local  ") != 1 || !strings.Contains(view, "F4 OpenWith") {
		t.Fatalf("unexpected view:\n%s", view)
	}
	if strings.Contains(view, "tui-commander  ·") {
		t.Fatalf("redundant application header is still visible:\n%s", view)
	}
	if !strings.Contains(view, "┌") || !strings.Contains(view, "┘") {
		t.Fatalf("outer frame missing:\n%s", view)
	}
	if got := lipgloss.Width(view); got != 120 {
		t.Fatalf("view width = %d, want 120", got)
	}
	if got := lipgloss.Height(view); got != 32 {
		t.Fatalf("view height = %d, want 32", got)
	}
}

func TestSingleLocationHeaderTracksFocusedPane(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	model, err := New(Options{Left: left, Right: right})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})

	view := model.View()
	if !strings.Contains(view, "Left · Local") || !strings.Contains(view, left) || strings.Contains(view, right) {
		t.Fatalf("left location header is incorrect:\n%s", view)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyTab})
	view = model.View()
	if !strings.Contains(view, "Right · Local") || !strings.Contains(view, right) || strings.Contains(view, left) {
		t.Fatalf("right location header is incorrect:\n%s", view)
	}
}

func TestShortcutFooterWrapsWithoutDroppingBindings(t *testing.T) {
	rows := shortcutRows(80)
	joined := strings.Join(rows, "\n")
	for _, binding := range []string{"F1", "F12", "Ctrl+N", "Ctrl+L", "Ctrl+B", "Ctrl+R", "Alt+F5", "Alt+F6", "Alt+R"} {
		if !strings.Contains(joined, binding) {
			t.Fatalf("shortcut footer is missing %s:\n%s", binding, joined)
		}
	}
	for _, excluded := range []string{"Ctrl+G", "Ctrl+H", "Ctrl+T", "Ctrl+Alt+C", "Alt+M", "Alt+U"} {
		if strings.Contains(joined, excluded) {
			t.Fatalf("shortcut footer unexpectedly contains %s:\n%s", excluded, joined)
		}
	}
	for _, row := range rows {
		if width := lipgloss.Width(row); width > 78 {
			t.Fatalf("shortcut row width = %d, want <= 78: %q", width, row)
		}
	}
}

func TestHelpListsBindingsOneActionPerRowAndScrolls(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model.Update(tea.KeyMsg{Type: tea.KeyF1})

	view := model.View()
	if model.modal != modalHelp || !strings.Contains(view, "F3 / Alt+F") || !strings.Contains(view, "one action per row") {
		t.Fatalf("initial Help view is incomplete:\n%s", view)
	}
	if got := lipgloss.Height(view); got != 24 {
		t.Fatalf("Help view height = %d, want 24", got)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyEnd})
	view = model.View()
	if !strings.Contains(view, "End (Help)") || !strings.Contains(view, "Jump to the end of Help") {
		t.Fatalf("Help did not scroll to its final binding:\n%s", view)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyF1})
	if model.modal != modalNone {
		t.Fatal("F1 did not close Help")
	}
}

func TestHelpActionsFitOneNarrowRow(t *testing.T) {
	const actionWidth = 30 // renderHelp's action column at an 80-column window.
	for _, binding := range helpBindings {
		if width := lipgloss.Width(binding.action); width > actionWidth {
			t.Errorf("Help action %q is %d cells, want <= %d", binding.action, width, actionWidth)
		}
	}
}

func TestSelectAllAndInvertVisibleSelection(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	pane := model.panes[0]
	pane.loading = false
	pane.entries = []vfs.Entry{
		{Name: "alpha.txt", Path: filepath.Join(directory, "alpha.txt")},
		{Name: "beta.txt", Path: filepath.Join(directory, "beta.txt")},
		{Name: "gamma.txt", Path: filepath.Join(directory, "gamma.txt")},
	}
	pane.filter = "alpha"

	model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if !pane.selected[pane.entries[0].Path] || len(pane.selected) != 1 {
		t.Fatalf("Ctrl+A selection = %#v", pane.selected)
	}
	pane.filter = ""
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'*'}})
	if pane.selected[pane.entries[0].Path] || !pane.selected[pane.entries[1].Path] || !pane.selected[pane.entries[2].Path] {
		t.Fatalf("inverted selection = %#v", pane.selected)
	}
}

func TestPaneShowsCompleteTimestamp(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	stamp := time.Date(2026, time.September, 14, 12, 34, 0, 0, time.Local)
	model.panes[0].loading = false
	model.panes[0].entries = []vfs.Entry{{Name: "document.txt", Path: filepath.Join(directory, "document.txt"), Size: 42, ModTime: stamp}}

	view := model.renderPane(0, 78, 12)
	if !strings.Contains(view, "2026-09-14 12:34") {
		t.Fatalf("complete timestamp missing from pane:\n%s", view)
	}
	if got := lipgloss.Width(view); got != 78 {
		t.Fatalf("pane width = %d, want 78", got)
	}
}

func TestPromptMasksPassword(t *testing.T) {
	prompt := promptState{value: []rune("secret"), cursor: 6, masked: true}
	view := renderInput(prompt, 20)
	if strings.Contains(view, "secret") {
		t.Fatalf("password leaked in rendered input: %q", view)
	}
}

func TestPromptWordEditingShortcuts(t *testing.T) {
	model := &Model{modal: modalPrompt}
	setPrompt := func(value string, cursor int) {
		model.prompt = promptState{value: []rune(value), cursor: cursor}
	}

	value := "report-final 2026.txt"
	setPrompt(value, len([]rune(value)))
	model.Update(tea.KeyMsg{Type: tea.KeyCtrlLeft})
	if want := len([]rune("report-final 2026.")); model.prompt.cursor != want {
		t.Fatalf("first Ctrl+Left cursor = %d, want %d", model.prompt.cursor, want)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyCtrlLeft})
	if want := len([]rune("report-final ")); model.prompt.cursor != want {
		t.Fatalf("second Ctrl+Left cursor = %d, want %d", model.prompt.cursor, want)
	}
	setPrompt(value, 0)
	model.Update(tea.KeyMsg{Type: tea.KeyCtrlRight})
	if want := len([]rune("report-")); model.prompt.cursor != want {
		t.Fatalf("Ctrl+Right cursor = %d, want %d", model.prompt.cursor, want)
	}

	value = "report-final.txt"
	setPrompt(value, len([]rune(value)))
	model.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	if got := string(model.prompt.value); got != "report-final." {
		t.Fatalf("Ctrl+Backspace value = %q, want %q", got, "report-final.")
	}
	model.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	if got := string(model.prompt.value); got != "report-" {
		t.Fatalf("Ctrl+W fallback value = %q, want %q", got, "report-")
	}

	setPrompt(value, 0)
	model.Update(promptCSIMessage("?CSI[51 59 53 126]?"))
	if got := string(model.prompt.value); got != "-final.txt" {
		t.Fatalf("Ctrl+Delete value = %q, want %q", got, "-final.txt")
	}

	value = "árvíz-tűrő.txt"
	if got, want := nextPromptWord([]rune(value), 0), len([]rune("árvíz-")); got != want {
		t.Fatalf("Unicode next word cursor = %d, want %d", got, want)
	}
	if got, want := previousPromptWord([]rune(value), len([]rune(value))), len([]rune("árvíz-tűrő.")); got != want {
		t.Fatalf("Unicode previous word cursor = %d, want %d", got, want)
	}
}

func TestBookmarkPromptOffersOptInPasswordCheckbox(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.panes[0].location.Backend = remotePathBackend{Local: vfs.NewLocal()}
	model.panes[0].location.Raw = "ftpes://alice@nas.example:5021/files"
	model.panes[0].password = "secret"

	model.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if !model.prompt.checkboxVisible || model.prompt.checkboxChecked {
		t.Fatalf("initial bookmark checkbox = visible %v, checked %v", model.prompt.checkboxVisible, model.prompt.checkboxChecked)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if !model.prompt.checkboxChecked || !model.prompt.checkboxFocus {
		t.Fatal("Space did not check the focused password-save checkbox")
	}
	view := model.renderModal(100, 20)
	if !strings.Contains(view, "[x] Save password in config") || strings.Contains(view, "secret") {
		t.Fatalf("bookmark prompt is incorrect or leaked the password:\n%s", view)
	}

	model.prompt.value = []rune("NAS")
	model.prompt.cursor = 3
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil {
		t.Fatal("bookmark save unexpectedly returned a command")
	}
	if len(model.config.Bookmarks) != 1 || model.config.Bookmarks[0].Password != "secret" {
		t.Fatalf("saved bookmarks = %#v", model.config.Bookmarks)
	}
	info, err := os.Stat(filepath.Join(configHome, "tui-commander", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %o", info.Mode().Perm())
	}
}

func TestSavedBookmarkConnectsWithoutPasswordPrompt(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.config.Bookmarks = []config.Bookmark{{Name: "NAS", Location: "ftpes://alice@nas.example:5021/", Password: "secret"}}
	model.modal = modalBookmarks

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || model.modal != modalNone || !model.busy {
		t.Fatalf("saved-password bookmark state = command %v, modal %v, busy %v", command != nil, model.modal, model.busy)
	}
	model.stopBusy()
}

func TestLocalBookmarkOpensWithoutAnyPasswordPrompt(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.config.Bookmarks = []config.Bookmark{{
		Name: "Local work", Location: directory, Password: "stale", CredentialRef: "stale-gopass-entry",
	}}
	model.modal = modalBookmarks

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || model.modal != modalNone || !model.busy || model.prompt.action != promptNone || model.pendingCredential.ref != "" {
		t.Fatalf("local bookmark state = command %v, modal %v, busy %v, prompt %v, pending %#v", command != nil, model.modal, model.busy, model.prompt.action, model.pendingCredential)
	}
	model.stopBusy()

	model.modal = modalBookmarks
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if model.modal != modalBookmarks || model.prompt.action != promptNone || !strings.Contains(model.status, "do not need gopass") {
		t.Fatalf("local gopass action = modal %v, prompt %v, status %q", model.modal, model.prompt.action, model.status)
	}
}

func TestFriendlyFTPESLocationOffersRemoteDirectoryThenPassword(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()

	command := model.submitPrompt(promptLocation, "FTPES, alice@nas.ehazhub.hu:5021", "", false)
	if command != nil || model.modal != modalPrompt || model.prompt.action != promptRemoteDirectory {
		t.Fatalf("FTPES prompt state = command %v, modal %v, action %v", command != nil, model.modal, model.prompt.action)
	}
	if model.prompt.pendingRaw != "ftpes://alice@nas.ehazhub.hu:5021" {
		t.Fatalf("normalized FTPES location = %q", model.prompt.pendingRaw)
	}
	if !model.prompt.checkboxVisible || model.prompt.checkboxLabel != "Allow legacy Common Name certificate" {
		t.Fatalf("legacy certificate option = visible %v, label %q", model.prompt.checkboxVisible, model.prompt.checkboxLabel)
	}
	pending := model.prompt.pendingRaw
	command = model.submitPrompt(promptRemoteDirectory, "/incoming", pending, false)
	if command != nil || model.prompt.action != promptPassword {
		t.Fatalf("remote directory prompt result = command %v, action %v", command != nil, model.prompt.action)
	}
	if model.prompt.pendingRaw != "ftpes://alice@nas.ehazhub.hu:5021/incoming" {
		t.Fatalf("FTPES location with remote directory = %q", model.prompt.pendingRaw)
	}
}

func TestRemoteDirectoryPreservesTLSIdentityQuery(t *testing.T) {
	location, err := remoteLocationWithDirectory(
		"ftpes://alice@nas.ehazhub.hu:5021/?tls-server-name=ehaziroda.myqnapcloud.com",
		"incoming/reports",
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "ftpes://alice@nas.ehazhub.hu:5021/incoming/reports?tls-server-name=ehaziroda.myqnapcloud.com"
	if location != want {
		t.Fatalf("location = %q, want %q", location, want)
	}
}

func TestRemoteDirectoryCanEnableLegacyCommonName(t *testing.T) {
	location, err := remoteLocationWithDirectory(
		"ftpes://alice@legacy.example:2121/?tls-server-name=certificate.example",
		"/incoming",
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "ftpes://alice@legacy.example:2121/incoming?tls-legacy-common-name=true&tls-server-name=certificate.example"
	if location != want {
		t.Fatalf("location = %q, want %q", location, want)
	}
	model, err := New(Options{Left: t.TempDir(), Right: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.startRemoteDirectoryPrompt(location)
	if !model.prompt.checkboxChecked {
		t.Fatal("saved legacy Common Name option was not restored in the connection form")
	}
	if view := model.renderModal(100, 20); !strings.Contains(view, "[x] Allow legacy Common Name certificate") {
		t.Fatalf("legacy Common Name checkbox missing:\n%s", view)
	}
}

func TestUnknownCertificateOffersExactPinConfirmation(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	const fingerprint = "1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF"
	model.busy = true
	model.Update(connectedMsg{
		index: 0, raw: "ftpes://alice@legacy.example/?tls-legacy-common-name=true", password: "secret",
		err: &vfs.UntrustedCertificateError{CommonName: "legacy.example", Fingerprint: fingerprint, Err: errors.New("unknown authority")},
	})
	if model.modal != modalConfirm || model.confirm.action != confirmTrustCertificate {
		t.Fatalf("certificate confirmation modal = %v, action = %v", model.modal, model.confirm.action)
	}
	if !strings.Contains(model.confirm.title, displayCertificateFingerprint(fingerprint)) || model.pendingTrust.password != "secret" {
		t.Fatalf("certificate confirmation state = %#v, title %q", model.pendingTrust, model.confirm.title)
	}
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || !model.busy || model.pendingTrust != (certificateTrustState{}) {
		t.Fatalf("trusted reconnect = command %v, busy %v, pending %#v", command != nil, model.busy, model.pendingTrust)
	}
	location, err := locationWithCertificatePin("ftpes://alice@legacy.example/?tls-legacy-common-name=true", fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	want := "ftpes://alice@legacy.example/?tls-cert-sha256=" + fingerprint + "&tls-legacy-common-name=true"
	if location != want {
		t.Fatalf("pinned location = %q, want %q", location, want)
	}
	model.stopBusy()
}

func TestPasswordInURLIsSeparatedBeforeConnection(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()

	model.startPasswordPrompt("ftpes://alice:url-secret@nas.example:5021/files")
	if string(model.prompt.value) != "url-secret" {
		t.Fatalf("extracted password = %q", model.prompt.value)
	}
	if model.prompt.pendingRaw != "ftpes://alice@nas.example:5021/files" {
		t.Fatalf("sanitized pending URL = %q", model.prompt.pendingRaw)
	}
	if view := model.renderModal(100, 20); strings.Contains(view, "url-secret") {
		t.Fatalf("password leaked in prompt:\n%s", view)
	}
}

func TestPaneTabsPreserveIndependentLocations(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	model, err := New(Options{Left: left, Right: right})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	original := model.panes[0]
	if command := model.newTab(0); command == nil {
		t.Fatal("newTab returned no load command")
	}
	if len(model.tabs[0]) != 2 || model.activeTab[0] != 1 || model.panes[0] == original {
		t.Fatalf("new tab state = count %d, active %d", len(model.tabs[0]), model.activeTab[0])
	}
	model.panes[0].location.Path = right
	model.changeTab(0, -1)
	if model.panes[0] != original || model.panes[0].location.Path != left {
		t.Fatalf("original tab was not restored: %#v", model.panes[0].location)
	}
	model.changeTab(0, 1)
	if model.panes[0].location.Path != right {
		t.Fatalf("second tab path = %q", model.panes[0].location.Path)
	}
	model.closeTab(0)
	if len(model.tabs[0]) != 1 || model.activeTab[0] != 0 {
		t.Fatalf("closed tab state = count %d, active %d", len(model.tabs[0]), model.activeTab[0])
	}
}

func TestSessionRestoresTabsActiveIndicesFocusAndSinglePaneDirectory(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	leftFirst, leftSecond, rightOnly := t.TempDir(), t.TempDir(), t.TempDir()
	model, err := New(Options{Left: leftFirst, Right: rightOnly})
	if err != nil {
		t.Fatal(err)
	}
	model.persistSession = true
	model.newTab(0)
	model.panes[0].location.Path = leftSecond
	model.panes[0].location.Raw = leftSecond
	model.panes[0].showHidden = true
	model.focus = 1
	if err := model.Close(); err != nil {
		t.Fatal(err)
	}

	restored, err := New(Options{Left: t.TempDir(), Right: t.TempDir(), restoreSession: true})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	if len(restored.tabs[0]) != 2 || restored.activeTab[0] != 1 || restored.panes[0].location.Path != leftSecond {
		t.Fatalf("restored left tabs = count %d, active %d, path %q", len(restored.tabs[0]), restored.activeTab[0], restored.panes[0].location.Path)
	}
	if restored.tabs[0][0].location.Path != leftFirst || !restored.tabs[0][1].showHidden {
		t.Fatalf("restored left tab details = %#v", restored.tabs[0])
	}
	if len(restored.tabs[1]) != 1 || restored.panes[1].location.Path != rightOnly {
		t.Fatalf("restored single right tab = count %d, path %q", len(restored.tabs[1]), restored.panes[1].location.Path)
	}
	if restored.focus != 1 || restored.status != "Previous session restored" {
		t.Fatalf("restored focus/status = %d, %q", restored.focus, restored.status)
	}
}

func TestSessionIsPersistedBeforeAsynchronousPaneLoad(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	left, right, next := t.TempDir(), t.TempDir(), t.TempDir()
	model, err := New(Options{Left: left, Right: right})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.persistSession = true
	model.panes[0].location.Path = next
	model.panes[0].location.Raw = next

	// Merely scheduling the load must save the new location. The command need
	// not finish, which models a terminal window being closed immediately.
	if command := model.loadPaneCmd(0); command == nil {
		t.Fatal("loadPaneCmd returned no command")
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Session == nil || loaded.Session.Panes[0].Tabs[0].Location != next || loaded.Session.Panes[1].Tabs[0].Location != right {
		t.Fatalf("autosaved session = %#v", loaded.Session)
	}
}

func TestExplicitLocationsOverrideSavedSession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	savedLeft, savedRight := t.TempDir(), t.TempDir()
	cfg := config.Config{Session: &config.Session{Panes: [2]config.SessionPane{
		{Tabs: []config.SessionTab{{Location: savedLeft}}},
		{Tabs: []config.SessionTab{{Location: savedRight}}},
	}}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	explicitLeft, explicitRight := t.TempDir(), t.TempDir()
	model, err := New(Options{Left: explicitLeft, Right: explicitRight})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	if model.panes[0].location.Path != explicitLeft || model.panes[1].location.Path != explicitRight || len(model.tabs[0]) != 1 || len(model.tabs[1]) != 1 {
		t.Fatalf("explicit locations were not honored: left %#v, right %#v", model.tabs[0], model.tabs[1])
	}
}

func TestFunctionKeysMoveBetweenTabs(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.newTab(0)
	if model.activeTab[0] != 1 {
		t.Fatalf("active tab = %d", model.activeTab[0])
	}
	model.Update(tea.KeyMsg{Type: tea.KeyF11})
	if model.activeTab[0] != 0 {
		t.Fatalf("F11 active tab = %d, want 0", model.activeTab[0])
	}
	model.Update(tea.KeyMsg{Type: tea.KeyF12})
	if model.activeTab[0] != 1 {
		t.Fatalf("F12 active tab = %d, want 1", model.activeTab[0])
	}
}

func TestTabsSwitchWithMouseAndReliableKeyboardFallbacks(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.width, model.height = 120, 32
	model.newTab(0)

	// The first tab starts at screen column 2: one frame cell and one tab-row pad.
	model.Update(tea.MouseMsg{X: 3, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if model.activeTab[0] != 0 {
		t.Fatalf("mouse-selected tab = %d, want 0", model.activeTab[0])
	}
	model.Update(tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	if model.activeTab[0] != 1 {
		t.Fatalf("Alt+Right selected tab = %d, want 1", model.activeTab[0])
	}
	model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if model.activeTab[0] != 0 {
		t.Fatalf("Shift+Tab selected tab = %d, want 0", model.activeTab[0])
	}
	model.Update(tea.KeyMsg{Type: tea.KeyCtrlRight})
	if model.activeTab[0] != 1 || model.status != "Activated tab 2 of 2" {
		t.Fatalf("Ctrl+Right tab state = active %d, status %q", model.activeTab[0], model.status)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyCtrlLeft})
	if model.activeTab[0] != 0 || model.status != "Activated tab 1 of 2" {
		t.Fatalf("Ctrl+Left tab state = active %d, status %q", model.activeTab[0], model.status)
	}
}

func TestSingleTransferPromptsForDestinationName(t *testing.T) {
	sourceDir, destinationDir := t.TempDir(), t.TempDir()
	model, err := New(Options{Left: sourceDir, Right: destinationDir})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	entry := vfs.Entry{Name: "report.txt", Path: filepath.Join(sourceDir, "report.txt")}
	model.panes[0].entries = []vfs.Entry{entry}
	model.panes[1].location.Backend = remotePathBackend{Local: vfs.NewLocal()}

	model.Update(tea.KeyMsg{Type: tea.KeyF5})
	if model.modal != modalPrompt || model.prompt.action != promptCopyAs || string(model.prompt.value) != entry.Name {
		t.Fatalf("copy prompt = modal %v, action %v, value %q", model.modal, model.prompt.action, model.prompt.value)
	}
	model.prompt.value, model.prompt.cursor = []rune("renamed.txt"), len([]rune("renamed.txt"))
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.modal != modalConfirm || model.confirm.action != confirmCopy || model.confirm.destinationName != "renamed.txt" {
		t.Fatalf("copy confirmation = modal %v, action %v, destination %q", model.modal, model.confirm.action, model.confirm.destinationName)
	}

	model.modal, model.confirm = modalNone, confirmState{}
	model.Update(tea.KeyMsg{Type: tea.KeyF6})
	if model.modal != modalPrompt || model.prompt.action != promptMoveAs || string(model.prompt.value) != entry.Name {
		t.Fatalf("move prompt = modal %v, action %v, value %q", model.modal, model.prompt.action, model.prompt.value)
	}
}

func TestMergerUsesSingleSelectionsIncludingDirectories(t *testing.T) {
	leftDir, rightDir := t.TempDir(), t.TempDir()
	model, err := New(Options{Left: leftDir, Right: rightDir})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	leftFile := vfs.Entry{Name: "cursor.txt", Path: filepath.Join(leftDir, "cursor.txt")}
	leftSelected := vfs.Entry{Name: "chosen", Path: filepath.Join(leftDir, "chosen"), Dir: true}
	rightFile := vfs.Entry{Name: "cursor.txt", Path: filepath.Join(rightDir, "cursor.txt")}
	rightSelected := vfs.Entry{Name: "chosen", Path: filepath.Join(rightDir, "chosen"), Dir: true}
	model.panes[0].entries = []vfs.Entry{leftFile, leftSelected}
	model.panes[1].entries = []vfs.Entry{rightFile, rightSelected}
	model.panes[0].selected[leftSelected.Path] = true
	model.panes[1].selected[rightSelected.Path] = true

	leftPath, rightPath, ok := model.mergerPaths()
	if !ok || leftPath != leftSelected.Path || rightPath != rightSelected.Path {
		t.Fatalf("merger paths = %q, %q, %v", leftPath, rightPath, ok)
	}
	model.panes[0].selected[leftFile.Path] = true
	leftPath, rightPath, ok = model.mergerPaths()
	if !ok || leftPath != leftFile.Path || rightPath != leftSelected.Path {
		t.Fatalf("same-pane merger paths = %q, %q, %v", leftPath, rightPath, ok)
	}

	// A same-pane comparison does not depend on the other pane being local.
	model.panes[1].location.Backend = remotePathBackend{Local: vfs.NewLocal()}
	leftPath, rightPath, ok = model.mergerPaths()
	if !ok || leftPath != leftFile.Path || rightPath != leftSelected.Path {
		t.Fatalf("same-pane merger paths with remote other pane = %q, %q, %v", leftPath, rightPath, ok)
	}
}

func TestCtrlNOpensConnectionBookmarks(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()

	model.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if model.modal != modalBookmarks {
		t.Fatalf("Ctrl+N modal = %v, want bookmarks", model.modal)
	}
	model.modal = modalNone
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}, Alt: true})
	if model.modal != modalBookmarks {
		t.Fatalf("Alt+C modal = %v, want bookmarks", model.modal)
	}
}

func TestConnectionListShowsProtocolAndEditableDisplayNameOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.config.Bookmarks = []config.Bookmark{{
		Name: "Production logs", Location: "ftpes://alice@secret.example:2121/private/logs", Password: "secret",
	}}
	model.modal = modalBookmarks

	view := model.renderModal(100, 24)
	if !strings.Contains(view, "FTPES") || !strings.Contains(view, "Production logs") {
		t.Fatalf("connection label missing:\n%s", view)
	}
	for _, hidden := range []string{"secret.example", "alice@", "/private/logs", "🔐"} {
		if strings.Contains(view, hidden) {
			t.Fatalf("connection list leaked %q:\n%s", hidden, view)
		}
	}

	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if model.modal != modalPrompt || model.prompt.action != promptBookmarkDisplayName || string(model.prompt.value) != "Production logs" {
		t.Fatalf("edit prompt = modal %v, action %v, value %q", model.modal, model.prompt.action, model.prompt.value)
	}
	model.prompt.value = []rune("Web logs")
	model.prompt.cursor = len(model.prompt.value)
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.modal != modalBookmarks || len(model.config.Bookmarks) != 1 || model.config.Bookmarks[0].Name != "Web logs" {
		t.Fatalf("edited connections = modal %v, bookmarks %#v", model.modal, model.config.Bookmarks)
	}
	if model.config.Bookmarks[0].Location != "ftpes://alice@secret.example:2121/private/logs" || model.config.Bookmarks[0].Password != "secret" {
		t.Fatalf("edit changed hidden connection details: %#v", model.config.Bookmarks[0])
	}
}

func TestBookmarkListCanBeReorderedAndSorted(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.config.Bookmarks = []config.Bookmark{
		{Name: "Alpha", Location: "/srv/alpha"},
		{Name: "Zulu", Location: "/srv/zulu"},
	}
	model.modal = modalBookmarks
	model.bookmarks.cursor = 1

	model.Update(tea.KeyMsg{Type: tea.KeyUp, Alt: true})
	if model.bookmarks.cursor != 0 || model.config.Bookmarks[0].Name != "Zulu" {
		t.Fatalf("manual bookmark order = cursor %d, bookmarks %#v", model.bookmarks.cursor, model.config.Bookmarks)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Bookmarks) != 2 || loaded.Bookmarks[0].Name != "Zulu" {
		t.Fatalf("persisted bookmark order = %#v", loaded.Bookmarks)
	}

	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if model.config.Bookmarks[0].Name != "Alpha" || model.config.Bookmarks[1].Name != "Zulu" || model.bookmarks.cursor != 1 {
		t.Fatalf("sorted bookmark state = cursor %d, bookmarks %#v", model.bookmarks.cursor, model.config.Bookmarks)
	}
}

func TestBookmarkCanBeAssignedToAGroup(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.config.Bookmarks = []config.Bookmark{
		{Name: "Home", Location: directory},
		{Name: "NAS", Group: "Work", Location: "ftpes://nas.example/logs"},
	}
	model.config.SortBookmarks()
	model.modal = modalBookmarks
	model.bookmarks.cursor = 1

	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if model.modal != modalPrompt || model.prompt.action != promptBookmarkGroup || model.prompt.pendingRaw != "Home" {
		t.Fatalf("group prompt = modal %v, action %v, pending %q", model.modal, model.prompt.action, model.prompt.pendingRaw)
	}
	model.prompt.value = []rune("Work")
	model.prompt.cursor = len(model.prompt.value)
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.modal != modalBookmarks || model.config.Bookmarks[1].Name != "Home" || model.config.Bookmarks[1].Group != "Work" {
		t.Fatalf("assigned group state = modal %v, cursor %d, bookmarks %#v", model.modal, model.bookmarks.cursor, model.config.Bookmarks)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Bookmarks) != 2 || loaded.Bookmarks[1].Group != "Work" {
		t.Fatalf("persisted groups = %#v", loaded.Bookmarks)
	}
	view := model.renderModal(100, 24)
	if strings.Count(view, "▾ Work") != 1 || strings.Contains(view, "▾ Ungrouped") {
		t.Fatalf("grouped bookmark list is incorrect:\n%s", view)
	}
}

func TestBookmarkCanLinkGopassCredential(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.config.Bookmarks = []config.Bookmark{{
		Name: "NAS", Location: "ftpes://alice@nas.example", Password: "plaintext",
	}}
	model.modal = modalBookmarks

	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if model.modal != modalPrompt || model.prompt.action != promptBookmarkCredential || model.prompt.pendingRaw != "NAS" {
		t.Fatalf("credential prompt = modal %v, action %v, pending %q", model.modal, model.prompt.action, model.prompt.pendingRaw)
	}
	model.prompt.value = []rune("Office NAS")
	model.prompt.cursor = len(model.prompt.value)
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	bookmark := model.config.Bookmarks[0]
	if model.modal != modalBookmarks || bookmark.CredentialRef != "Office NAS" || bookmark.Password != "" {
		t.Fatalf("linked credential bookmark = modal %v, bookmark %#v", model.modal, bookmark)
	}

	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.modal != modalPrompt || model.prompt.action != promptCredentialPassword || model.pendingCredential.ref != "Office NAS" {
		t.Fatalf("vault password prompt = modal %v, action %v, pending %#v", model.modal, model.prompt.action, model.pendingCredential)
	}
}

func TestLocationUsesCredentialUsernameOnlyWhenMissing(t *testing.T) {
	if got := locationWithCredentialUsername("ftpes://nas.example/logs", "alice"); got != "ftpes://alice@nas.example/logs" {
		t.Fatalf("location with credential username = %q", got)
	}
	if got := locationWithCredentialUsername("sftp://bob@host.example/home", "alice"); got != "sftp://bob@host.example/home" {
		t.Fatalf("existing username was replaced: %q", got)
	}
}

func TestSessionUsesSavedBookmarkPasswordForSameRemoteConnection(t *testing.T) {
	cfg := config.Config{Bookmarks: []config.Bookmark{{
		Name: "NAS", Location: "ftpes://alice@nas.example:5021/home", Password: "saved-secret",
	}}}
	if got := sessionPassword(cfg, "ftpes://alice@nas.example:5021/other/path"); got != "saved-secret" {
		t.Fatalf("session password = %q", got)
	}
	if got := sessionPassword(cfg, "ftpes://bob@nas.example:5021/other/path"); got != "" {
		t.Fatalf("session reused password for another user: %q", got)
	}
	if got := sessionPassword(cfg, "/tmp/local"); got != "" {
		t.Fatalf("local session received password %q", got)
	}
}

type closingRemoteBackend struct {
	*vfs.Local
	closed bool
}

func (b *closingRemoteBackend) ID() string    { return "ftpes://test" }
func (b *closingRemoteBackend) Label() string { return "FTPES test" }
func (b *closingRemoteBackend) Close() error {
	b.closed = true
	return nil
}

func TestCtrlShiftNControlCodeDisconnectsNetwork(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	remote := &closingRemoteBackend{Local: vfs.NewLocal()}
	model.panes[0].location = vfs.Location{Backend: remote, Path: "/", Raw: "ftpes://test/"}
	model.panes[0].localReturn = directory
	model.backends = append(model.backends, remote)

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	if command == nil {
		t.Fatal("disconnect returned no reload command")
	}
	if !remote.closed {
		t.Fatal("network backend was not closed")
	}
	if model.panes[0].location.Backend.ID() != "local" || model.panes[0].location.Path != directory {
		t.Fatalf("disconnected pane = %#v", model.panes[0].location)
	}
}

func TestAltROpensAndClosesRecycleBin(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}, Alt: true}

	_, command := model.Update(key)
	if command == nil || model.panes[0].location.Backend.ID() != "trash" {
		t.Fatalf("Recycle Bin open = command %v, backend %q", command != nil, model.panes[0].location.Backend.ID())
	}
	_, command = model.Update(key)
	if command == nil || model.panes[0].location.Backend.ID() != "local" || model.panes[0].location.Path != directory {
		t.Fatalf("Recycle Bin close = command %v, location %#v", command != nil, model.panes[0].location)
	}
}

func TestDeleteAndRestoreKeysAreContextAware(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	entry := vfs.Entry{Name: "report.txt", Path: filepath.Join(directory, "report.txt")}
	model.panes[0].entries = []vfs.Entry{entry}

	model.Update(tea.KeyMsg{Type: tea.KeyF8})
	if model.modal != modalConfirm || model.confirm.action != confirmTrash {
		t.Fatalf("local F8 modal = %v, action = %v", model.modal, model.confirm.action)
	}
	model.modal, model.confirm = modalNone, confirmState{}
	if command := model.openTrash(); command == nil {
		t.Fatal("openTrash returned no load command")
	}
	model.panes[0].loading = false
	model.panes[0].entries = []vfs.Entry{{Name: "report.txt", Path: "/report.txt", OriginalPath: entry.Path}}

	model.Update(tea.KeyMsg{Type: tea.KeyF5})
	if model.modal != modalConfirm || model.confirm.action != confirmRestore {
		t.Fatalf("Recycle Bin F5 modal = %v, action = %v", model.modal, model.confirm.action)
	}
	model.modal, model.confirm = modalNone, confirmState{}
	model.Update(tea.KeyMsg{Type: tea.KeyF8})
	if model.modal != modalConfirm || model.confirm.action != confirmDelete {
		t.Fatalf("Recycle Bin F8 modal = %v, action = %v", model.modal, model.confirm.action)
	}
}

func TestAltF6OpensExtractConfirmation(t *testing.T) {
	directory := t.TempDir()
	model, err := New(Options{Left: directory, Right: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	model.panes[0].entries = []vfs.Entry{{Name: "bundle.tar.gz", Path: filepath.Join(directory, "bundle.tar.gz")}}
	model.Update(tea.KeyMsg{Type: tea.KeyF6, Alt: true})
	if model.modal != modalConfirm || model.confirm.action != confirmExtract {
		t.Fatalf("Alt+F6 modal = %v, action = %v", model.modal, model.confirm.action)
	}
}
