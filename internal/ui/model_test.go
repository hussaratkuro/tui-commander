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
	for _, binding := range []string{"F1", "F12", "Ctrl+N", "Ctrl+L", "Ctrl+B", "Ctrl+R", "Alt+F5", "Alt+F6", "Alt+U", "Alt+R"} {
		if !strings.Contains(joined, binding) {
			t.Fatalf("shortcut footer is missing %s:\n%s", binding, joined)
		}
	}
	for _, excluded := range []string{"Ctrl+G", "Ctrl+H", "Ctrl+T", "Ctrl+Alt+C", "Alt+M"} {
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
	if model.modal != modalHelp || !strings.Contains(view, "F3 / Ctrl+F") || !strings.Contains(view, "one action per row") {
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
