package ui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"tui-commander/internal/config"
	"tui-commander/internal/openwith"
	"tui-commander/internal/theme"
	"tui-commander/internal/trash"
	"tui-commander/internal/vfs"
)

type pane struct {
	location      vfs.Location
	entries       []vfs.Entry
	cursor        int
	offset        int
	selected      map[string]bool
	filter        string
	filterInput   bool
	search        string
	showHidden    bool
	revealPath    string
	git           gitSummary
	password      string
	passwordSaved bool
	localReturn   string
	loading       bool
	err           error
}

type modalKind uint8

const (
	modalNone modalKind = iota
	modalHelp
	modalPrompt
	modalConfirm
	modalApplications
	modalBookmarks
	modalCommands
	modalSync
)

type promptAction uint8

const (
	promptNone promptAction = iota
	promptMkdir
	promptRename
	promptCopyAs
	promptMoveAs
	promptLocation
	promptPassword
	promptPack
	promptBookmarkName
	promptRemoteDirectory
	promptBookmarkDisplayName
	promptBookmarkGroup
	promptBookmarkCredential
	promptCredentialPassword
)

type promptState struct {
	title           string
	value           []rune
	cursor          int
	masked          bool
	action          promptAction
	pendingRaw      string
	checkboxVisible bool
	checkboxChecked bool
	checkboxFocus   bool
	checkboxLabel   string
	checkboxHint    string
}

type confirmAction uint8

const (
	confirmNone confirmAction = iota
	confirmCopy
	confirmMove
	confirmTrash
	confirmRestore
	confirmDelete
	confirmExtract
	confirmTrustCertificate
)

type confirmState struct {
	title           string
	action          confirmAction
	destinationName string
}

type applicationState struct {
	items      []openwith.Application
	cursor     int
	path       string
	mimeType   string
	remoteFile *remoteFile
}

type bookmarkState struct{ cursor int }

type certificateTrustState struct {
	index         int
	raw           string
	password      string
	passwordSaved bool
	fingerprint   string
}

type credentialRequest struct {
	index        int
	bookmarkName string
	location     string
	ref          string
	master       string
}

type remoteFile struct {
	localPath  string
	backend    vfs.Backend
	remotePath string
	mode       os.FileMode
	stamp      time.Time
	size       int64
	dirty      bool
	readOnly   bool
}

type Model struct {
	width, height     int
	panes             [2]*pane
	tabs              [2][]*pane
	activeTab         [2]int
	backends          []vfs.Backend
	focus             int
	modal             modalKind
	prompt            promptState
	confirm           confirmState
	applications      applicationState
	bookmarks         bookmarkState
	pendingTrust      certificateTrustState
	pendingCredential credentialRequest
	credentialMaster  string
	credentialUntil   time.Time
	helpOffset        int
	config            config.Config
	status            string
	statusError       bool
	busy              bool
	busyLabel         string
	busyFrame         int
	cancel            context.CancelFunc
	progress          vfs.TransferProgress
	progressCh        chan vfs.TransferProgress
	tempRoot          string
	remoteFiles       []*remoteFile
	terminal          *terminalSession
	terminalVisible   bool
	lastClickPane     int
	lastClickRow      int
	lastClickAt       time.Time
	commands          commandPaletteState
	sync              syncCenterState
	persistSession    bool
	closed            bool
	closeErr          error
}

func New(options Options) (*Model, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load configuration: %w", err)
	}
	model := &Model{config: cfg, status: "Ready"}
	restored := false
	if options.restoreSession && cfg.Session != nil {
		restored = model.restoreSession(*cfg.Session, [2]string{options.Left, options.Right})
	}
	if !restored {
		model.status = "Ready"
		if err := model.openInitialPanes(options.Left, options.Right); err != nil {
			model.closeBackends()
			return nil, err
		}
	}
	tempRoot, err := os.MkdirTemp("", "tui-commander-")
	if err != nil {
		model.closeBackends()
		return nil, err
	}
	model.tempRoot = tempRoot
	model.persistSession = options.saveSession
	return model, nil
}

func (m *Model) openInitialPanes(leftRaw, rightRaw string) error {
	left, err := connectInitialLocation(leftRaw, "")
	if err != nil {
		return fmt.Errorf("open left location: %w", err)
	}
	right, err := connectInitialLocation(rightRaw, "")
	if err != nil {
		_ = left.Backend.Close()
		return fmt.Errorf("open right location: %w", err)
	}
	m.backends = append(m.backends, left.Backend, right.Backend)
	fallback, _ := os.Getwd()
	leftReturn, rightReturn := fallback, fallback
	if left.Backend.ID() == "local" {
		leftReturn = left.Path
	}
	if right.Backend.ID() == "local" {
		rightReturn = right.Path
	}
	m.panes[0] = &pane{location: left, selected: make(map[string]bool), localReturn: leftReturn, loading: true}
	m.panes[1] = &pane{location: right, selected: make(map[string]bool), localReturn: rightReturn, loading: true}
	m.tabs[0] = []*pane{m.panes[0]}
	m.tabs[1] = []*pane{m.panes[1]}
	return nil
}

func connectInitialLocation(raw, password string) (vfs.Location, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return vfs.Connect(ctx, raw, password)
}

func (m *Model) restoreSession(session config.Session, fallbackLocations [2]string) bool {
	fallback, _ := os.Getwd()
	skipped := 0
	for paneIndex, savedPane := range session.Panes {
		restoredActive := 0
		for savedIndex, savedTab := range savedPane.Tabs {
			password := sessionPassword(m.config, savedTab.Location)
			location, err := connectInitialLocation(savedTab.Location, password)
			if err != nil {
				skipped++
				continue
			}
			localReturn := savedTab.LocalReturn
			if localReturn == "" {
				localReturn = fallback
			}
			if location.Backend.ID() == "local" {
				localReturn = location.Path
			}
			restored := &pane{
				location: location, selected: make(map[string]bool), showHidden: savedTab.ShowHidden,
				password: password, passwordSaved: password != "", localReturn: localReturn, loading: true,
			}
			m.tabs[paneIndex] = append(m.tabs[paneIndex], restored)
			m.backends = append(m.backends, location.Backend)
			if savedIndex <= savedPane.ActiveTab {
				restoredActive = len(m.tabs[paneIndex]) - 1
			}
		}
		if len(m.tabs[paneIndex]) == 0 {
			location, err := connectInitialLocation(fallbackLocations[paneIndex], "")
			if err != nil {
				m.closeBackends()
				m.tabs, m.panes, m.activeTab = [2][]*pane{}, [2]*pane{}, [2]int{}
				return false
			}
			localReturn := fallback
			if location.Backend.ID() == "local" {
				localReturn = location.Path
			}
			m.tabs[paneIndex] = []*pane{{
				location: location, selected: make(map[string]bool), localReturn: localReturn, loading: true,
			}}
			m.backends = append(m.backends, location.Backend)
			restoredActive = 0
		}
		m.activeTab[paneIndex] = restoredActive
		m.panes[paneIndex] = m.tabs[paneIndex][m.activeTab[paneIndex]]
	}
	m.focus = session.Focus
	if skipped == 1 {
		m.setStatus("Restored session; one unavailable tab was skipped", true)
	} else if skipped > 1 {
		m.setStatus(fmt.Sprintf("Restored session; %d unavailable tabs were skipped", skipped), true)
	} else {
		m.setStatus("Previous session restored", false)
	}
	return true
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadPaneCmd(0), m.loadPaneCmd(1), watchTickCmd(), theme.Watch())
}

func (m *Model) Close() error { return m.shutdown() }

func (m *Model) close() { _ = m.shutdown() }

func (m *Model) shutdown() error {
	if m.closed {
		return m.closeErr
	}
	m.closed = true
	if m.cancel != nil {
		m.cancel()
	}
	var sessionErr error
	if m.persistSession {
		sessionErr = m.saveSession()
	}
	if m.terminal != nil {
		m.terminal.close()
	}
	backendErr := m.closeBackends()
	tempErr := os.RemoveAll(m.tempRoot)
	m.closeErr = errors.Join(sessionErr, backendErr, tempErr)
	return m.closeErr
}

func (m *Model) closeBackends() error {
	var result error
	for _, backend := range m.backends {
		result = errors.Join(result, backend.Close())
	}
	m.backends = nil
	return result
}

func (m *Model) saveSession() error {
	session := &config.Session{Focus: m.focus}
	for paneIndex := range m.tabs {
		session.Panes[paneIndex].ActiveTab = m.activeTab[paneIndex]
		for _, tab := range m.tabs[paneIndex] {
			location := currentLocationString(*tab)
			if tab.location.Backend.ID() == "trash" {
				location = tab.localReturn
			}
			session.Panes[paneIndex].Tabs = append(session.Panes[paneIndex].Tabs, config.SessionTab{
				Location: config.SanitizeLocation(location), LocalReturn: tab.localReturn, ShowHidden: tab.showHidden,
			})
		}
	}
	m.config.Session = session
	return m.config.Save()
}

func (m *Model) persistSessionState() {
	if !m.persistSession || m.closed {
		return
	}
	if err := m.saveSession(); err != nil {
		m.setStatus("Save session: "+err.Error(), true)
	}
}

func sessionPassword(cfg config.Config, location string) string {
	identity := sessionConnectionIdentity(location)
	if identity == "" {
		return ""
	}
	for _, bookmark := range cfg.Bookmarks {
		if bookmark.Password != "" && sessionConnectionIdentity(bookmark.Location) == identity {
			return bookmark.Password
		}
	}
	return ""
}

func sessionConnectionIdentity(location string) string {
	parsed, err := url.Parse(vfs.NormalizeLocation(config.SanitizeLocation(location)))
	if err != nil || parsed.Scheme == "" || parsed.Scheme == "file" {
		return ""
	}
	username := ""
	if parsed.User != nil {
		username = parsed.User.Username()
	}
	identity := strings.ToLower(parsed.Scheme) + "://" + username + "@" + strings.ToLower(parsed.Host)
	if strings.EqualFold(parsed.Scheme, "smb") {
		share := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")[0]
		identity += "/" + share + "?domain=" + strings.ToLower(parsed.Query().Get("domain"))
	}
	return identity
}

type paneLoadedMsg struct {
	index   int
	pane    *pane
	path    string
	entries []vfs.Entry
	git     gitSummary
	err     error
}

type operationDoneMsg struct {
	description string
	err         error
}

type connectedMsg struct {
	index         int
	location      vfs.Location
	raw           string
	password      string
	passwordSaved bool
	err           error
}

type applicationsLoadedMsg struct {
	items      []openwith.Application
	mimeType   string
	path       string
	remoteFile *remoteFile
	err        error
}

type appClosedMsg struct{ err error }

type remoteDownloadedMsg struct {
	file    *remoteFile
	chooser bool
	err     error
}

type remoteUploadedMsg struct {
	file *remoteFile
	err  error
}

type watchTickMsg struct{}
type busyTickMsg struct{}
type progressMsg struct{ progress vfs.TransferProgress }

type credentialResolvedMsg struct {
	request  credentialRequest
	username string
	password string
	err      error
}

func (m *Model) loadPaneCmd(index int) tea.Cmd {
	// Persist before starting the asynchronous load. This also survives the
	// terminal window being closed before Bubble Tea can shut down cleanly.
	m.persistSessionState()
	target := m.panes[index]
	location := target.location
	return func() tea.Msg {
		entries, err := location.Backend.List(context.Background(), location.Path)
		var git gitSummary
		if err == nil && location.Backend.ID() == "local" {
			git = readGitSummary(location.Path)
		}
		return paneLoadedMsg{index: index, pane: target, path: location.Path, entries: entries, git: git, err: err}
	}
}

func connectCmd(index int, raw, password string, passwordSaved bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		location, err := vfs.Connect(ctx, raw, password)
		return connectedMsg{index: index, location: location, raw: raw, password: password, passwordSaved: passwordSaved, err: err}
	}
}

func applicationsCmd(path string, remote *remoteFile) tea.Cmd {
	return func() tea.Msg {
		items, mimeType, err := openwith.Applications(path)
		return applicationsLoadedMsg{items: items, mimeType: mimeType, path: path, remoteFile: remote, err: err}
	}
}

func (m *Model) currentPane() *pane { return m.panes[m.focus] }
func (m *Model) otherPane() *pane   { return m.panes[1-m.focus] }

func (m *Model) newTab(index int) tea.Cmd {
	current := m.panes[index]
	created := &pane{
		location:      current.location,
		selected:      make(map[string]bool),
		showHidden:    current.showHidden,
		password:      current.password,
		passwordSaved: current.passwordSaved,
		localReturn:   current.localReturn,
		loading:       true,
	}
	m.tabs[index] = append(m.tabs[index], created)
	m.activeTab[index] = len(m.tabs[index]) - 1
	m.panes[index] = created
	m.setStatus("Opened a new tab", false)
	return m.loadPaneCmd(index)
}

func (m *Model) closeTab(index int) tea.Cmd {
	if len(m.tabs[index]) <= 1 {
		m.setStatus("Each pane keeps at least one tab", true)
		return nil
	}
	active := m.activeTab[index]
	m.tabs[index] = append(m.tabs[index][:active], m.tabs[index][active+1:]...)
	m.activeTab[index] = min(active, len(m.tabs[index])-1)
	m.panes[index] = m.tabs[index][m.activeTab[index]]
	m.setStatus("Closed tab", false)
	return m.loadPaneCmd(index)
}

func (m *Model) changeTab(index, delta int) tea.Cmd {
	count := len(m.tabs[index])
	if count <= 1 {
		m.setStatus("This pane has only one tab", false)
		return nil
	}
	m.activeTab[index] = (m.activeTab[index] + delta + count) % count
	m.panes[index] = m.tabs[index][m.activeTab[index]]
	m.panes[index].loading = true
	m.setStatus(fmt.Sprintf("Activated tab %d of %d", m.activeTab[index]+1, count), false)
	return m.loadPaneCmd(index)
}

func (m *Model) activateTab(index, tabIndex int) tea.Cmd {
	if tabIndex < 0 {
		tabIndex = len(m.tabs[index]) - 1
	}
	if tabIndex < 0 || tabIndex >= len(m.tabs[index]) || tabIndex == m.activeTab[index] {
		return nil
	}
	m.activeTab[index] = tabIndex
	m.panes[index] = m.tabs[index][tabIndex]
	m.panes[index].loading = true
	m.setStatus(fmt.Sprintf("Activated tab %d of %d", tabIndex+1, len(m.tabs[index])), false)
	return m.loadPaneCmd(index)
}

func (p *pane) current() (vfs.Entry, bool) {
	entries := p.visibleEntries()
	if p.cursor < 0 || p.cursor >= len(entries) {
		return vfs.Entry{}, false
	}
	return entries[p.cursor], true
}

func (p *pane) visibleEntries() []vfs.Entry {
	if p.showHidden && p.filter == "" {
		return p.entries
	}
	query := strings.ToLower(p.filter)
	entries := make([]vfs.Entry, 0, len(p.entries))
	for _, entry := range p.entries {
		if !p.showHidden && strings.HasPrefix(entry.Name, ".") {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(entry.Name), query) {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func (p *pane) chosen() []vfs.Entry {
	if len(p.selected) > 0 {
		return p.selectedEntries()
	} else if entry, ok := p.current(); ok {
		return []vfs.Entry{entry}
	}
	return nil
}

func (p *pane) selectedEntries() []vfs.Entry {
	result := make([]vfs.Entry, 0, len(p.selected))
	for _, entry := range p.entries {
		if p.selected[entry.Path] {
			result = append(result, entry)
		}
	}
	return result
}

// clearInput drops the quick filter, its input mode, and the type-ahead search.
func (p *pane) clearInput() {
	p.filter, p.filterInput, p.search = "", false, ""
}

// enterDirectory changes the listing and resets every per-listing input state.
func (p *pane) enterDirectory(path string) {
	p.location.Path, p.cursor, p.offset, p.loading = path, 0, 0, true
	p.clearInput()
}

// jumpToSearch moves the highlight to the first visible entry matching the
// type-ahead search, preferring a name prefix over a substring match.
func (p *pane) jumpToSearch(visible int) bool {
	query := strings.ToLower(p.search)
	if query == "" {
		return false
	}
	match := -1
	for index, entry := range p.visibleEntries() {
		name := strings.ToLower(entry.Name)
		if strings.HasPrefix(name, query) {
			match = index
			break
		}
		if match < 0 && strings.Contains(name, query) {
			match = index
		}
	}
	if match < 0 {
		return false
	}
	p.cursor = match
	p.clamp(visible)
	return true
}

func (p *pane) clamp(visible int) {
	entries := p.visibleEntries()
	if len(entries) == 0 {
		p.cursor, p.offset = 0, 0
		return
	}
	p.cursor = max(0, min(p.cursor, len(entries)-1))
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+visible {
		p.offset = p.cursor - visible + 1
	}
	p.offset = max(0, min(p.offset, max(0, len(entries)-visible)))
}

func (m *Model) visibleRows() int {
	return max(1, m.height-6-len(shortcutRows(max(1, m.width-2)))-m.terminalPanelHeight())
}

func (m *Model) setStatus(message string, isError bool) {
	m.status, m.statusError = message, isError
}

func (m *Model) startPrompt(title, value string, action promptAction, masked bool) {
	m.modal = modalPrompt
	m.prompt = promptState{title: title, value: []rune(value), cursor: len([]rune(value)), action: action, masked: masked}
}

func (m *Model) startTransferNamePrompt(move bool, entry vfs.Entry) {
	action, verb := promptCopyAs, "Copy"
	if move {
		action, verb = promptMoveAs, "Move"
	}
	m.startPrompt(verb+" to the other pane as", entry.Name, action, false)
}

func (m *Model) startPasswordPrompt(raw string) {
	raw = vfs.NormalizeLocation(raw)
	password := ""
	if parsed, err := url.Parse(raw); err == nil && parsed.User != nil {
		password, _ = parsed.User.Password()
	}
	m.startPrompt("Password (blank for SSH key/agent, anonymous FTP, or passwordless SMB)", password, promptPassword, true)
	m.prompt.pendingRaw = config.SanitizeLocation(raw)
}

func (m *Model) startRemoteDirectoryPrompt(raw string) {
	raw = vfs.NormalizeLocation(raw)
	directory := "/"
	if parsed, err := url.Parse(raw); err == nil {
		if parsed.Path != "" {
			directory = parsed.Path
		}
		switch strings.ToLower(parsed.Scheme) {
		case "ftps", "ftpes", "ftps+implicit":
			m.startPrompt("Remote directory (SMB: /share/path)", directory, promptRemoteDirectory, false)
			m.prompt.pendingRaw = raw
			m.prompt.checkboxVisible = true
			m.prompt.checkboxLabel = "Allow legacy Common Name certificate"
			m.prompt.checkboxHint = "Enter continues"
			m.prompt.checkboxChecked = queryEnabled(parsed.Query().Get("tls-legacy-common-name"))
			return
		}
	}
	m.startPrompt("Remote directory (SMB: /share/path)", directory, promptRemoteDirectory, false)
	m.prompt.pendingRaw = raw
}

func remoteLocationWithDirectory(raw, directory string, allowLegacyCommonName bool) (string, error) {
	parsed, err := url.Parse(vfs.NormalizeLocation(raw))
	if err != nil {
		return "", err
	}
	directory = strings.TrimSpace(directory)
	if directory == "" {
		directory = "/"
	}
	if !strings.HasPrefix(directory, "/") {
		directory = "/" + directory
	}
	parsed.Path, parsed.RawPath = directory, ""
	query := parsed.Query()
	query.Del("tls-legacy-common-name")
	if allowLegacyCommonName {
		query.Set("tls-legacy-common-name", "true")
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func queryEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func locationWithCertificatePin(raw, fingerprint string) (string, error) {
	parsed, err := url.Parse(vfs.NormalizeLocation(raw))
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("tls-cert-sha256", fingerprint)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func displayCertificateFingerprint(fingerprint string) string {
	if len(fingerprint) != 64 {
		return fingerprint
	}
	groups := make([]string, 0, 32)
	for index := 0; index < len(fingerprint); index += 2 {
		groups = append(groups, fingerprint[index:index+2])
	}
	return strings.Join(groups[:16], ":") + "\n" + strings.Join(groups[16:], ":")
}

func (m *Model) startBusy(label string, cancel context.CancelFunc) tea.Cmd {
	m.busy, m.busyLabel, m.busyFrame, m.cancel = true, label, 0, cancel
	m.progress = vfs.TransferProgress{}
	m.progressCh = make(chan vfs.TransferProgress, 32)
	return busyTickCmd()
}

func busyTickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return busyTickMsg{} })
}

func watchTickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return watchTickMsg{} })
}

func (m *Model) selectedSummary() string {
	chosen := m.currentPane().chosen()
	if len(chosen) == 1 {
		return chosen[0].Name
	}
	return fmt.Sprintf("%d entries", len(chosen))
}

func (m *Model) overwriteWarning() string {
	chosen := m.currentPane().chosen()
	names := make([]string, len(chosen))
	for index, entry := range chosen {
		names[index] = entry.Name
	}
	return m.overwriteWarningFor(names)
}

func (m *Model) overwriteWarningFor(names []string) string {
	destinationNames := make(map[string]bool, len(m.otherPane().entries))
	for _, entry := range m.otherPane().entries {
		destinationNames[entry.Name] = true
	}
	count := 0
	for _, name := range names {
		if destinationNames[name] {
			count++
		}
	}
	if count == 0 {
		return ""
	}
	if count == 1 {
		return " One destination entry already exists and will be replaced or merged."
	}
	return fmt.Sprintf(" %d destination entries already exist and will be replaced or merged.", count)
}

func (m *Model) localPaths(entries []vfs.Entry) ([]string, bool) {
	if m.currentPane().location.Backend.ID() != "local" {
		return nil, false
	}
	paths := make([]string, len(entries))
	for i, entry := range entries {
		paths[i] = entry.Path
	}
	return paths, true
}

func currentLocationString(p pane) string {
	return locationStringForPath(p, p.location.Path)
}

func locationStringForPath(p pane, locationPath string) string {
	if p.location.Backend.ID() == "local" {
		return locationPath
	}
	if p.location.Backend.ID() == "trash" {
		return "trash:///"
	}
	raw := config.SanitizeLocation(p.location.Raw)
	if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" {
		parsed.Path = locationPath
		parsed.RawPath = ""
		return parsed.String()
	}
	if separator := strings.Index(raw, "://"); separator >= 0 {
		remainder := raw[separator+3:]
		if slash := strings.Index(remainder, "/"); slash >= 0 {
			return raw[:separator+3] + remainder[:slash] + locationPath
		}
		return raw + locationPath
	}
	return raw
}

func (m *Model) openTrash() tea.Cmd {
	current := m.currentPane()
	if current.location.Backend.ID() == "trash" {
		return m.returnTrashToLocal()
	}
	backend, err := trash.New()
	if err != nil {
		m.setStatus("Open Recycle Bin: "+err.Error(), true)
		return nil
	}
	returnPath := current.localReturn
	if current.location.Backend.ID() == "local" {
		returnPath = current.location.Path
	}
	replacement := &pane{
		location: vfs.Location{Backend: backend, Path: trash.Root, Raw: "trash:///"},
		selected: make(map[string]bool), showHidden: true, localReturn: returnPath, loading: true,
	}
	m.panes[m.focus] = replacement
	m.tabs[m.focus][m.activeTab[m.focus]] = replacement
	m.backends = append(m.backends, backend)
	m.setStatus("Recycle Bin · F5 restores · F8 permanently deletes", false)
	return m.loadPaneCmd(m.focus)
}

func (m *Model) returnTrashToLocal() tea.Cmd {
	current := m.currentPane()
	returnPath := usableLocalDirectory(current.localReturn)
	backend := vfs.NewLocal()
	replacement := &pane{
		location: vfs.Location{Backend: backend, Path: returnPath, Raw: returnPath},
		selected: make(map[string]bool), showHidden: current.showHidden,
		localReturn: returnPath, loading: true,
	}
	m.panes[m.focus] = replacement
	m.tabs[m.focus][m.activeTab[m.focus]] = replacement
	m.backends = append(m.backends, backend)
	m.setStatus("Closed Recycle Bin", false)
	return m.loadPaneCmd(m.focus)
}

func sortRemoteFiles(files []*remoteFile) {
	sort.SliceStable(files, func(i, j int) bool { return files[i].localPath < files[j].localPath })
}

func (m *Model) tempPath(entry vfs.Entry) string {
	name := filepath.Base(entry.Name)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "remote-file"
	}
	return filepath.Join(m.tempRoot, fmt.Sprintf("%d-%s", time.Now().UnixNano(), name))
}
