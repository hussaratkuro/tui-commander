# tui-commander

`tui-commander` is a keyboard-first, two-pane file manager for local files,
FTP, FTPES/FTPS, SFTP, and SMB. It uses Double Commander-style file-operation
keys and follows the active HyDE/Wallbash terminal palette, with Catppuccin
Mocha as a fallback.

## Features

- Local/local, local/remote, and remote/remote panes.
- FTP, explicit FTPES/FTPS, implicit FTPS, host-key-verified SFTP, and SMB 2/3.
- Recursive streaming copy and move without loading complete files into memory.
- Multiple selection, rename, mkdir, FreeDesktop recycle-bin restore/permanent
  delete, and cancellable jobs.
- Mouse selection, right-click multi-selection, double-click open, and wheel
  navigation.
- Independent tabs in both panes, preserving each tab's location, selection,
  cursor, and scroll position.
- Create and extract 7z, ZIP, RAR, GZ, TAR, and TAR.GZ archives.
- MIME-aware application chooser with open-once and set-default actions.
- Toggleable integrated terminal panel that starts in the active local directory.
- Instant type-to-filter, `fzf` integration, hidden-file toggling, and Git status
  summaries in local pane headers.
- Saved connection bookmarks with optional password persistence or encrypted
  credential references resolved through `gopass`.
- Remote files open through a managed local copy; changed copies can be uploaded
  with `Alt+U`.
- Launch `merger` for the current local entry in both panes.
- Dry-run directory sync center with one-way/two-way plans, optional SHA-256
  comparison, resumable queues, and failed-item retry.

## Build and install

```bash
go test ./...
./install-local.sh
```

Or build without installing:

```bash
go build .
./tui-commander
```

The active palette is read from `~/.cache/hyde/wallbash/shell-colors` and is
refreshed every two seconds while the app is open. Set
`TUI_THEME=catppuccin` to force the built-in fallback palette.

## Starting locations

With no arguments both panes start in the current directory:

```bash
tui-commander
```

Local locations and remote URLs can be mixed:

```bash
tui-commander ~/Downloads sftp://alice@example.com/home/alice
tui-commander --left=/srv/files --right=ftps://alice@example.com/incoming
```

Supported URLs:

```text
ftp://user@host/path
ftps://user@host/path
ftpes://user@host/path
ftpes://user@host:port/path?tls-server-name=certificate-host
ftpes://user@host:port/path?tls-legacy-common-name=true
ftpes://user@host:port/path?tls-cert-sha256=leaf-certificate-fingerprint
ftps+implicit://user@host/path
sftp://user@host/path
smb://user@host/share/path
smb://DOMAIN;user@host/share/path
```

`ftpes://` is an alias for explicit-TLS `ftps://`. The connection field also
accepts the graphical-file-manager notation `FTPES, user@host:port/path`.
If an alias host serves a certificate issued for another known DNS name, the
`tls-server-name` query selects that certificate identity without disabling
certificate-chain or hostname verification.
For an old FTPS certificate that has no Subject Alternative Name extension,
the remote-directory prompt offers **Allow legacy Common Name certificate**.
This opt-in setting is limited to that connection and stored in its URL as
`tls-legacy-common-name=true`. It normally verifies the system-trusted CA
chain, validity period, server-auth usage, and the Common Name hostname; it
never lets a Common Name override a certificate that contains a SAN extension.
If the CA is unknown, tui-commander shows the leaf certificate's SHA-256
fingerprint and asks before pinning that exact certificate. A pinned connection
stores `tls-cert-sha256` in its bookmark and still checks the hostname, validity
period, and server-auth usage. A changed certificate is rejected until the user
explicitly trusts its new fingerprint.
The connection prompt asks for a password separately. A password placed in a
URL is extracted and removed from the saved URL. It is persisted in a separate
field only when **Save password in config** is checked while saving a bookmark.
SFTP checks `~/.ssh/known_hosts` and tries the SSH agent, standard private-key
files, and the supplied password. SMB requires a share name as the first path
component, uses NTLMv2 with required message signing, and defaults to the
`guest` account when the URL contains no username. A `?domain=DOMAIN` query is
also accepted instead of the `DOMAIN;user` form.

## Keys

| Key | Action |
|---|---|
| `Tab` | Switch pane |
| `Ctrl+T` | Create a tab in the active pane |
| `Ctrl+W` | Close the active tab |
| `F11` / `F12` | Previous / next tab |
| `Ctrl+Tab` / `Ctrl+Shift+Tab` | Next / previous tab |
| `Alt+1` … `Alt+9` / `Alt+0` | Activate a numbered tab / last tab |
| `Enter` | Enter a directory or open a file with its default app |
| `F3` / `Ctrl+F` | Fuzzy-find files and directories with `fzf` |
| `F4` / `Ctrl+O` | Choose an application |
| `Space` | Toggle selection |
| `Ctrl+A` | Select every visible entry |
| `*` | Invert the selection of all visible entries |
| `Backspace` | Edit the quick filter, or open the parent when no filter is active |
| `F2` | Rename |
| `F5` | Copy to the other pane; restore when the Recycle Bin is active |
| `F6` | Move to the other pane |
| `F7` | Create directory |
| `F8` | Trash local items; permanently delete trashed or remote items |
| `F9` | Show or hide the integrated terminal |
| `F10` / `Ctrl+C` | Quit |
| `Alt+F5` | Pack selection into an archive |
| `Alt+F6` | Extract archive into the other pane |
| `Ctrl+Alt+C` | Copy the highlighted entry's full path |
| `Ctrl+N` / `Alt+C` | Connection bookmarks |
| `Ctrl+Shift+N` | Disconnect the active network connection |
| `Ctrl+L` | Enter a path or connection URL |
| `Ctrl+B` | Bookmark the active location |
| `Ctrl+H` | Show or hide dotfiles |
| `Alt+M` | Open current local entries in `merger` |
| `Alt+R` | Open or close the Recycle Bin in the active pane |
| `Alt+U` | Upload a changed remote file opened locally |
| `Ctrl+R` | Refresh active pane |
| `Ctrl+S` | Open the directory sync and transfer center |
| `Ctrl+Shift+P` / `Ctrl+P` | Open the fuzzy command palette |
| `F1` | Help |

Inside the bookmark selector, press `g` to assign the highlighted bookmark to
a named group; submit an empty name to move it back to **Ungrouped**. Groups
are shown as separate sections. `Alt+Up` and `Alt+Down` move the highlighted
bookmark inside its group and save the new order. Press `s` to sort all
bookmarks by group, protocol, and display name.

Press `v` on a bookmark to link a gopass entry by its permanent ID or unique
title. Opening it asks for the gopass vault password, resolves the credential
through `gopass credential`, and caches only the vault unlock secret in memory
for 15 minutes. Linking a credential removes any plaintext saved password;
submitting an empty reference unlinks it.

Typing printable characters while a file pane is focused immediately filters
that pane to names containing the typed text, case-insensitively. The active
query and match count appear in the bottom status row. `Backspace` edits the
query and `Esc` clears it. Since ordinary letters now belong to quick filtering,
connection, location, merger, upload, and quit actions use the explicit
shortcuts shown above.

The fuzzy finder searches recursively from a local pane and searches the
current listing on remote panes. It respects the `Ctrl+H` hidden-file setting.
Choosing a nested local result moves the pane to its parent and highlights it.
The single top location bar follows the active pane and shows its Git branch
plus staged, modified, untracked, ahead, and behind counts when the directory
is inside a Git worktree; use
`Ctrl+R` to refresh it after external changes.

In the application chooser, `Enter` opens once and `d` sets that application as
the default for the file's MIME type before opening it. The default is written
through `xdg-mime`, so desktop applications and other file managers see it too.

When the integrated terminal is visible it owns the keyboard, including
`Ctrl+C`, function keys, and pasted text. Press `F9` to return keyboard focus to
the file panes without ending the shell session. A newly created terminal starts
in the active local pane; if both panes are remote, it starts in tui-commander's
working directory.

## Synchronizing directories

Open the two roots in the left and right panes, then press `Ctrl+S`. The first
screen is always a dry-run plan; it does not copy anything and never deletes
destination-only entries. Use `Left`/`Right` (or `d`) to choose left-to-right,
right-to-left, or two-way sync. Press `c` to replace the quick size/timestamp
comparison with SHA-256 content checks, then `Enter` to run the displayed
queue.

Completed queue items stay marked, so `Enter` resumes pending work after a
cancellation. Press `r` to retry only failed items and `s` to rescan both
trees. Two-way sync copies the newer version; equal-timestamp differences and
file/directory type mismatches are shown as conflicts and deliberately skipped.

## Creating and extracting archives

To create an archive:

1. Use `Space` to select entries in the active local pane. With no explicit
   selection, the entry under the cursor is used.
2. Open the destination directory in the other pane.
3. Press `Alt+F5`.
4. Enter an archive filename ending in `.7z`, `.zip`, `.rar`, `.gz`, `.tar`,
   `.tar.gz`, or `.tgz`, then press `Enter`.

To extract an archive, place the cursor on it, open the destination directory in
the other pane, press `Alt+F6`, and confirm with `Enter` or `y`.

## Connecting to FTP, FTPES/FTPS, SFTP, or SMB

1. Focus the pane that should contain the connection and press `Ctrl+N` or
   `Alt+C`.
2. Press `a`, then enter a URL such as `ftp://alice@example.com`,
   `ftps://alice@example.com/incoming`,
   `ftpes://alice@nas.ehazhub.hu:5021/?tls-server-name=ehaziroda.myqnapcloud.com`,
   `sftp://alice@example.com`, or `smb://WORKGROUP;alice@fileserver`.
3. Enter the initial remote directory in the next field, such as `/incoming`,
   `/home/alice`, or `/Shared/folder` for SMB. A path already present in the
   URL is prefilled and can be edited. For the specific FTPS error
   `certificate relies on legacy Common Name field`, press `Tab` and enable
   **Allow legacy Common Name certificate** with `Space` before continuing.
4. Type the password in the masked prompt and press `Enter`. Leave it blank for
   anonymous FTP, SSH agent/key authentication, or a passwordless SMB account.
   If the FTPS certificate uses an unknown CA, compare the displayed SHA-256
   fingerprint with one obtained from the server administrator, then confirm
   only if they match. The app reconnects using that exact certificate pin.
5. Once connected, press `Ctrl+B` and enter a bookmark name. To remember the
   password, press `Tab`, check **Save password in config** with `Space`, then
   press `Enter`.

Open saved connections later with `Ctrl+N` or `Alt+C`, then press `Enter`.
A bookmark with a saved password connects immediately; otherwise the masked
password prompt opens. Press `d` in the connection list to delete the
highlighted bookmark, or `e` to edit its display name. The list shows only the
protocol and that display name; connection details remain stored but hidden
from the selector. Bookmarks always save the protocol, host, username,
port, and path; saving the password is opt-in. Press `v` to replace that saved
password with an encrypted gopass credential reference. Press `g` to change the
highlighted bookmark's group. Use `Alt+Up` / `Alt+Down` for a custom persistent
order within that group, or `s` to sort by group, protocol, and name.

Press `Ctrl+Shift+N` to close the active network session and return every tab
using that same session to its last local directory.

## Recycle Bin

In a local pane, `F8` moves selected items to the FreeDesktop Recycle Bin after
confirmation. Press `Alt+R` to open or close it in the active pane. There,
`F5` restores selected items to their recorded original locations, while `F8`
permanently deletes them after an explicit confirmation. Restore never
overwrites an existing path. Remote servers have no portable recycle-bin API,
so `F8` remains permanent on remote panes and its confirmation says so.

## Archive support

| Format | Create | Extract | Backend |
|---|---:|---:|---|
| 7z | yes | yes | `7z` |
| ZIP | yes | yes | `7z` |
| RAR | with `rar` installed | yes | `rar` / `7z` |
| GZ | yes, one file | yes | `gzip` |
| TAR | yes | yes | `tar` |
| TAR.GZ / TGZ | yes | yes | `tar` |

Packing and extraction currently operate between local panes. Files can be
copied from a remote pane locally, archived, then copied back remotely.

## Security notes

- FTP is unencrypted; prefer FTPS, SFTP, or a trusted/VPN-protected SMB network.
- FTPS verifies the server certificate and SFTP verifies `known_hosts`.
- Legacy Common Name-only FTPS certificates require an explicit per-connection
  opt-in. Unknown-CA certificates require a second explicit SHA-256 pin;
  pinned connections still verify the exact hostname, validity period, and
  server-auth usage.
- SMB uses NTLMv2 and requires message signing, but `smb://` does not imply
  transport encryption.
- Bookmark configuration is mode `0600`. Opted-in passwords are stored as
  plain text in that user-readable-only file; leave the checkbox clear if this
  is not appropriate for the machine.
- Application launches and archive commands pass argument arrays directly and
  do not evaluate filenames through a shell.
