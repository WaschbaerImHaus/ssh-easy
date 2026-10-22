# Changelog – ssh-easy

All notable changes to this project are documented here.
Format loosely follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [v0.63] – Build 63
- **Security** `F9` no longer opens the legacy Office formats (`.doc`, `.xls`, `.ppt`), RTF and OpenDocument files (`.odt`, `.ods`, `.odp`, `.odg`) – they can carry macros or embedded OLE objects. `.docx`, `.xlsx` and `.pptx` are checked after downloading: they must be a real Office ZIP container without macros (`vbaProject.bin`) or ActiveX controls, otherwise they are not opened and the copy is deleted (Office detects files by content, so a renamed `.docm` or `.doc` would otherwise slip through; finding of the automated security review of Build 62)
- **Added** 2 new tests (509 total, all green)

## [v0.62] – Build 62
- **Added** editor (`F4`) in the file manager, working like mcedit with an F-key bar at the bottom: `F1` help, `F2` save, `F3` mark block (start, end; a third press unmarks), `F4` search and replace (asks per match: `j`/`y` replace, `n` skip, `a` all, `Esc` cancel), `F5` copy block, `F6` move block, `F7` search (`F7` + `Enter` again searches on), `F8` delete block, `F10`/`Esc` quit (asks to save if there are changes), `Ctrl+Y` delete line. Windows line endings (CRLF), a missing final line break and lone carriage returns are kept exactly
- **Added** binary files and files that are not valid UTF-8 open in an editable hex view: hex digits overwrite the byte under the cursor (e.g. `AD` → `AF`), `Tab` switches to the character column, changed bytes are highlighted, the file size never changes. `F7` searches bytes (`ad ff`) or text
- **Added** saving writes a temporary file with owner-only permissions in the same directory and renames it in one step (original permissions are kept, a failed save leaves the file untouched); if the file was changed on the server since it was opened (size or time), ssh-easy asks before overwriting. Files opened through a symlink are saved to the target, the link stays
- **Changed** the German F10 label of viewer and editor is now "Ende" like in the panels (fits at 80 columns)
- **Added** 39 new tests (507 total, all green)

## [v0.61] – Build 61
- **Security** `F9` (open with the default program) now uses an allowlist instead of a blocklist: only known document and media types are opened (images, PDF, text, HTML, Office without macro variants, audio/video, archives). Everything else – programs, scripts, shortcuts and unknown types (e.g. `.py`, `.jnlp`, `.mhtml`, `.theme`, `.webloc`) – is refused (finding of the automated security review of Build 60)
- **Security** long file names are cut first and normalized afterwards, so a shortened name can no longer end in a dot or space that Windows would strip (which could have turned it into a different extension)
- **Security** on macOS the local copy gets the quarantine attribute (Gatekeeper)
- **Added** 1 new test, extended checks (468 total, all green)

## [v0.60] – Build 60
- **Changed** the file manager viewer now works like the mc viewer with an F-key bar at the bottom: `F1` help, `F2` wrap, `F3`/`F10` quit, `F4` hex/text, `F5` go to line (hex: offset, decimal or `0x…`), `F6` search with a regular expression, `F7` search (case-insensitive, `n` searches on, the field is pre-filled with the last term and the first typed character replaces it), `F9` open. The match line is highlighted; in hex view the raw bytes are searched
- **Added** `F9` in the viewer opens the file locally with the default program of this computer (Windows: by file extension, Linux: `xdg-open`, macOS: `open`) – e.g. a `.jpg` in the image viewer, an `.html` in the browser. The file is downloaded into a private temporary folder (`0700`/`0600`) and deleted when ssh-easy exits (leftovers are removed at the next start)
- **Security** `F9` refuses executable and script types (`.exe`, `.bat`, `.cmd`, `.ps1`, `.vbs`, `.js`, `.hta`, `.msi`, `.lnk`, `.url`, `.reg`, `.jar`, `.iso`, `.desktop`, `.sh`, …) – opening them would run them. The local name is made Windows-safe (no `:`/`\`/`<>|?*`, no reserved device names, no trailing dots/spaces – `evil.exe.` would otherwise become `evil.exe`). On Windows the file gets the Mark of the Web, so Office opens it in Protected View and SmartScreen warns
- **Added** 16 new tests (467 total, all green)

## [v0.59] – Build 59
- **Added** viewer (`F3`) in the file manager: shows the file under the cursor full-screen (loaded via SFTP, at most 16 MiB – larger files are cut with a hint). Content from the server is displayed safely: control and invisible format characters become `?`, tabs become spaces, CRLF works. Long lines are wrapped (`F2` toggles wrapping; without wrapping `←`/`→` scroll sideways), binary files (NUL bytes) open as hex view automatically (`F4` toggles hex/text). `↑`/`↓`, `PgUp`/`PgDn`, `Home`/`End` scroll; `q`, `Esc`, `F3` or `F10` close the viewer. `F3` on a directory enters it like `Enter`, symlinks to files show the target file
- **Added** 16 new tests (451 total, all green)

## [v0.58] – Build 58
- **Security** file manager copy writes every file as an exclusively created temporary file in the target directory, restricted to `0600` right away, and renames it to the target name in one step at the end. Private files are therefore never readable by others during the transfer (if the host refuses to restrict permissions, a private file is not copied), a symlink planted at the target between check and write is replaced instead of written through, an existing target stays untouched until the copy is complete, and copying a file onto itself through two connection entries to the same server (name vs. IP) can no longer empty the source. New directories are `0700` while being filled
- **Security** names received from a server containing a backslash (path separator on Windows SFTP servers) or control characters are rejected when copying; every target path is additionally checked to lie inside the target directory
- **Security** invisible Unicode format characters (e.g. right-to-left override U+202E, zero-width space) in file names are shown as `?`, so a name like `invoice<RLO>fdp.exe` cannot pose as `invoiceexe.pdf`
- **Security** sizes reported by a server are added without overflow and the progress bar is computed safely – a malicious server can no longer crash ssh-easy with absurd file sizes; scanning stops after 1,000,000 entries or 128 directory levels (nothing is copied then)
- **Added** 8 new tests (435 total, all green); verified end-to-end against a real SFTP server

## [v0.57] – Build 57
- **Added** copying with `F5` in the file manager: the marked entries of the active panel (without marks: the entry under the cursor) are copied into the current directory of the other panel. Directories are copied recursively with their content, symlinks as symlinks (their target is not followed), permissions and modification time are kept (silently skipped if the host forbids it). The data flows through this computer, the two servers do not need to reach each other; large buffers are written with concurrent SFTP requests
- **Added** dialogs like in mc: confirmation (`Enter` copy, `Esc`/`q` cancel), progress with current file, file and byte counters and a progress bar (`Esc` cancels, a partially written file is removed), and a question when the target already exists, showing size and date of source and target: `j`/`y` overwrite, `n` skip, `a` all, `k` none, `u` all if newer, `g` all if larger, `Esc` cancel. Existing directories are merged. Errors of single entries do not stop the copy and are reported at the end; afterwards the target panel is reloaded and fully copied entries are unmarked
- **Changed** the file manager entry is always shown; with fewer than two connections it is greyed out with the hint to add two connections or one more connection
- **Security** file names received from a server that contain `/` or are `.`/`..` are rejected (no path traversal out of the target directory on the other server); an existing symlink at the target is replaced instead of written through; copying a file onto itself (same host and path) is refused
- **Added** 31 new tests (427 total, all green)

## [v0.56] – Build 56
- **Added** marking in the file manager: `Insert` marks/unmarks the entry under the cursor and moves down one line (on Windows, where Insert arrives as a NUL key, too); `*` inverts the marking of all entries in the active panel, files and directories alike (like in mc). `..` can never be marked
- **Added** marked entries are highlighted (yellow in the classic and ssh-easy themes, white on dark grey in the monochrome theme); the info line of a panel shows the number of marked entries and the total size of the marked files (e.g. `3 marked, 2058 B`). Marks are kept per panel when switching with `Tab` and are cleared when changing the directory (like in mc)
- **Added** 11 new tests (396 total, all green)

## [v0.55] – Build 55
- **Changed** file manager connection choice: the first entry is now the placeholder "(not selected)" and the cursor starts on it. Enter on the placeholder does nothing, so you cannot end up in the file manager by accidentally pressing Enter twice – both connections have to be chosen deliberately (translated into all 41 languages)
- **Changed** a connection can no longer be paired with itself: the connection chosen for the left panel is removed from the list for the right panel
- **Added** 3 new tests, 1 replaced (385 total, all green)

## [v0.54] – Build 54
- **Security** error messages from connection attempts (file manager and normal connect, key deployment/removal) are shown with control characters replaced: SSH/SFTP error texts can contain text sent by the server (e.g. during the handshake) and could otherwise inject terminal escape sequences (finding of the automated security review of Build 53)
- **Added** 2 new tests (383 total, all green)

## [v0.53] – Build 53
- **Added** file manager (first draft of a reduced Midnight Commander clone for copying between two SSH hosts): new last entry in the connection list, separated by a blank line. Choose the left and the right connection one after the other (`q` goes back at any time); both are opened as separate SSH connections without tunnels, independent of connections already open in the list (agent/keys, password prompt if needed, known_hosts check as usual). Files are listed via SFTP, nothing has to be installed on the servers
- **Added** two-panel view: turquoise menu bar at the top, F-key bar at the bottom, no command line. Each panel shows `user@host:path`, name, size and modification time; directories first, `..` on top, start directory is the user's home. `Enter` changes into a directory or `..` (cursor lands on the directory you came from), `Tab` switches panels, arrows/PgUp/PgDn/Home/End move the cursor, `F10` or `q` closes both connections and returns to the list with the file manager entry still selected
- **Added** a directory without permission keeps the panel in the current directory and shows a message; symlinks to directories are marked with `~` and are not followed (message instead)
- **Added** three colour themes prepared (classic mc, ssh-easy, monochrome); switching via the Options menu follows in a later build. Menu and F1–F9 are visual only in this draft
- **Security** file names from the server are shown with control characters replaced (no terminal escape injection via crafted file names)
- **Fixed** after deleting the last connection the cursor stays on a connection instead of jumping to the file manager entry
- **Added** 43 new tests (381 total, all green)

## [v0.50] – Build 50
- **Changed** the status view no longer shows an empty "Tunnel:" heading for connections without (enabled) tunnels; it shows "No tunnels" instead (translated into all 41 languages)
- **Added** 1 new test (280 total, all green)

## [v0.49] – Build 49
- **Security** DNS fallback hardening (automated security review of Build 48): local-only names (no dot, `.local`, `.lan`, `.internal`, `.home.arpa`, `.fritz.box`, …) are never sent to public DNS servers. If an address came from the public fallback DNS and the host key is still unknown, it is no longer trusted automatically (no trust-on-first-use) — ssh-easy explains that you should connect once via the local network or by IP. Known hosts are unaffected and still protected by known_hosts
- **Security** Windows auto-update via setup: every setup is downloaded into its own freshly created, owner-only subfolder; right before the elevated start ssh-easy verifies that nothing but the setup is in that folder (protection against DLL planting next to an elevated installer)
- **Added** 9 new tests (279 total, all green)

## [v0.48] – Build 48
- **Added** connection groups: a colon in the name is a group separator (`Office:Server1`, `Office:NAS` → expandable group *Office*). Only the first colon splits; names need text before and after the colon. A group shows a green dot when any member is connected
- **Added** scrollable connection list: at most 15 rows are visible (fewer in small windows), `↑ +N` / `↓ +N` show hidden rows; PgUp/PgDn/Home/End page through long lists
- **Added** optional auto-update from GitHub releases (toggle with `u`, last menu line, off by default): download at startup, installation on exit. Linux replaces the binary atomically; Windows renames the running EXE (`.old`, removed on next start) or, when installed in *Program Files*, runs the setup silently with UAC elevation. Mandatory SHA-256 check against the GitHub API digest, size and format check, downloads only from this repository's release URL. Before installing, the checksum is verified again; on Windows the setup is hashed through a handle that blocks writes, deletes and renames until it has been launched (no swap window before the elevated start). The download folder must not be a symlink/reparse point, planted symlinks at the target path are not followed
- **Added** DNS fallback: if the system DNS does not work (timeout, server error), names are resolved via 1.1.1.1, 1.0.0.1, 8.8.8.8 and 8.8.4.4, starting with the server that worked last (rotation). An unknown name (NXDOMAIN) is not sent to public servers. IPv4 addresses are tried before IPv6
- **Added** the window size (columns × rows) is remembered and restored on Windows unless the window was maximized, fullscreen or minimized (checked every 2 s, after resizing and on exit; skipped in Windows Terminal, which remembers its own size)
- **Added** the password prompt shows host and, on the next line, the user name
- **Changed** first connection attempt uses a 5-second connect timeout; a network error (host off, DNS failure, handshake timeout) stops immediately instead of trying every SSH key with its own 10-second timeout. Without any SSH key or agent, the host is probed first, so an unreachable host is reported right away instead of asking for a password
- **Changed** the list shows at most two tunnel ports, further ports as a count (`+3 more`)
- **Changed** tunnels that could not be opened are shown in red (list and status view) without the error text; details remain in the log. If a hidden port (3rd and later) failed, the `+N more` hint is red
- **Changed** a newly created or renamed connection is selected after saving (its group is expanded)
- **Changed** all UI texts are now translated into all 41 languages (previously several messages, e.g. "This name is already in use", were English in every language); a new completeness test fails the build if a text is missing, a placeholder differs or a sentence is an English copy
- **Fixed** the first key press after leaving the SSH terminal was swallowed (e.g. the first `x` to disconnect did nothing): the stdin forwarder stayed blocked in a read after the session ended and consumed the next key. Input is now cancellable (Unix: cancelreader/epoll, Windows: console input peek) and the forwarder is stopped before Bubbletea takes over stdin again
- **Fixed** the selected row of a connected host only highlighted the green dot, the name stayed on a black background (the dot's reset sequence ended the selection style). Rows are now rendered segment by segment with the same base style
- **Fixed** Shift+Insert did nothing in the connection form and password prompt on Windows: Bubbletea v1 does not map VK_INSERT and delivers a NUL rune; ssh-easy now checks that Shift and Insert are really pressed (GetAsyncKeyState) — Shift+Ctrl or Alt+Shift (layout switch) do not paste. Plain Insert no longer inserts a NUL character. `ESC[2;2~` from Unix terminals pastes as well
- **Added** 115 new tests (270 total, all green)

## [v0.47] – Build 47
- **Security** Shift+Insert paste into the SSH session now supports bracketed paste. ssh-easy watches the session output for `ESC[?2004h` / `ESC[?2004l` (also combined, e.g. `ESC[?1049;2004h`, and split across SSH packets). While the mode is active, pasted text is wrapped in `ESC[200~ … ESC[201~` and embedded start/end markers are stripped repeatedly. Previously a multi-line clipboard (e.g. copied from a crafted web page) was executed line by line in the remote shell. Without the mode the old behavior is unchanged
- **Added** 19 new tests in `bracketed_paste_test.go` (155 total, all green)

## [v0.46] – Build 46
- **Fixed** the main menu showed "ssh-easy vdev" in installed versions: the build number was read at runtime from `build.txt` next to the executable, which installers don't ship. `build.txt` is now embedded into the binary via `go:embed`
- **Changed** menu title now shows version and build, matching the release scheme: `ssh-easy v0.46 (Build 46)`
- **Added** 5 new tests in `build_info_test.go` (136 total, all green)

## [v0.45] – Build 45
- **Security** OSC 52 clipboard content from the remote host is sanitized: all control characters except tab, CR and LF are removed (C0, DEL, C1). Prevents paste-jacking, e.g. a malicious server planting `ESC[201~` (end of bracketed paste) so that text pasted later into a local shell would execute immediately. Found by automated security review of Build 44
- **Added** 4 new tests (131 total, all green)

## [v0.44] – Build 44
- **Fixed** copying from programs running on the remote host did nothing (e.g. the login link in Claude Code): such programs copy via the OSC 52 escape sequence, which the Windows console (conhost) silently discards. ssh-easy now filters OSC 52 out of the SSH output stream itself and writes the decoded text to the local clipboard (Windows, Linux with xclip/xsel/wl-clipboard). Supports BEL and ST terminators, sequences split across SSH packets, unpadded base64 and all selection parameters. If the clipboard is unavailable, the original sequence is passed through to the terminal emulator
- **Security** OSC 52 is write-only: clipboard read requests (`?`) are never answered, payloads are limited to 1 MiB
- **Security** `golang.org/x/crypto` 0.48.0 → 0.58.0 — closes the reachable critical CVEs in known_hosts handling and the SSH agent path (CVE-2026-42508, CVE-2026-39830 to -39834, CVE-2026-46595, CVE-2026-46597, CVE-2026-39829 and later advisories up to 0.56.0). Also `x/sys` 0.49.0, `x/term` 0.47.0, `x/text` 0.43.0
- **Security** built with Go 1.27.2 (was 1.26.0) — fixes three reachable standard library vulnerabilities (GO-2026-5972 encoding/asn1, GO-2026-4971 net, GO-2026-4602 os). govulncheck: 0 reachable vulnerabilities
- **Added** 26 new tests in `osc52_test.go`

## [v0.43] – Build 43
- **Fixed** font size keys not working on Windows: Bubbletea v1.x drops the Ctrl modifier for character keys in its Windows coninput path (Ctrl+Plus arrives as rune 0x00 — a "ctrl++" key message can never be produced). Font size is now bound to **Ctrl+Up / Ctrl+Down**, which are explicitly mapped as KeyCtrlUp/KeyCtrlDown in Bubbletea's Windows VK table and arrive reliably. The Ctrl+Plus/Minus aliases remain for terminals that can deliver them
- **Added** font size is persisted on exit: the native conhost Ctrl+MouseWheel zoom bypasses the application, so ssh-easy now reads the actual console font size at shutdown and saves it to `font_size` — your mouse zoom survives a restart

## [v0.42] – Build 42
- **Added** clipboard paste in all TUI form fields via Ctrl+V or Shift+Insert (connection form, password prompt, keygen form) — e.g. to paste a password from a password manager. Only the first line of the clipboard is inserted so stray newlines can't trigger an accidental submit
- **Added** console window receives keyboard focus on startup (Windows): `SetForegroundWindow` + `ShowWindow` after `AllocConsole` — no more clicking into the window before you can type
- **Added** Alt+F4 quits the app everywhere (like PuTTY): as a global key in all menu views, and inside a running SSH session via the stdin forwarder detecting the `CSI 1;3S` VT sequence — the session is closed, all connections disconnected and the program exits cleanly
- **Added** PuTTY-style scrollback snap (Windows): typing while scrolled up in the console buffer jumps the viewport back to the input line (`SetConsoleWindowInfo` on each stdin chunk)
- **Added** console font size adjustable via Ctrl+Plus / Ctrl+Minus (Windows, 8–72px via `SetCurrentConsoleFontEx`); the value is persisted as `font_size` in connections.json and re-applied on startup. On Linux a hint points to the terminal emulator's own settings
- **Added** 18 new tests in `feature_ux_test.go` (total: 74 → 92, all green)

## [v0.41] – Build 41
- **Security** Password in `ManagedConnection` now stored as `[]byte` with active zero-out on disconnect (was `string`, immutable and never cleared). New `clearPassword()` helper overwrites the backing array before nulling the reference, reducing RAM lifetime of credentials (HIGH finding)
- **Security** Keyboard-Interactive callback no longer copies the password into an extra `pwCopy` variable – closure references the caller's string directly (one less copy in memory, HIGH finding)
- **Security** `expandTilde()` blocks path traversal: `~/../../etc/passwd` is normalized by `filepath.Join` and would have escaped the home directory. New guard returns the original path when the expanded result is not a prefix of `$HOME` (MEDIUM finding)
- **Security** SSH key auto-discovery in `~/.ssh/` rejects symlinks via `entry.Type().IsRegular()` – a crafted symlink `id_fake → /etc/shadow` no longer triggers a file read (MEDIUM finding)
- **Security** Auto-discovery uses a PEM content whitelist instead of a filename blacklist: new `looksLikePEMKey()` reads the first 11 bytes and only accepts files starting with `-----BEGIN ` (MEDIUM finding). Replaces incomplete skip-list (`known_hosts`, `config`, `authorized_keys`, `known_hosts.old`, `environment`) that let other non-key files through
- **Security** `HostKeyChangedError` is now a typed error with `Hostname` field – detection uses `errors.As()` instead of fragile string matching on the German-localized error message. Transparent through `fmt.Errorf("%w", ...)` wrapping (MEDIUM finding)
- **Security** Windows DACL hardening via `golang.org/x/sys/windows`: new `restrictFilePermissions()` calls `SetNamedSecurityInfo` with an owner-only DACL + `PROTECTED_DACL_SECURITY_INFORMATION` (blocks inheritance) on log file, config, auth-cache, generated private keys and `known_hosts`. Fixes `os.OpenFile(..., 0600)` being a no-op on Windows (MEDIUM finding). CGO-free – uses pure-Go syscalls
- **Added** 12 new tests in `security_fixes_test.go` covering all six hardenings (total: 62 → 74 tests, all green)

## [v0.40] – Build 40
- **Added** Linux/macOS auto-launch in a terminal when started without TTY (e.g. double-click from Nemo/Nautilus/Dolphin): `ensureTerminal()` detects missing TTY + presence of `$DISPLAY` or `$WAYLAND_DISPLAY`, searches for a terminal emulator (respects `$TERMINAL`; falls back through `x-terminal-emulator`, `gnome-terminal`, `konsole`, `xfce4-terminal`, `mate-terminal`, `lxterminal`, `tilix`, `alacritty`, `kitty`, `urxvt`, `rxvt`, `xterm`) and relaunches itself via `exec.Command(emu, "-e", self)`. `SSH_EASY_RELAUNCHED=1` guards against infinite loops. On headless hosts (no display) a clear stderr message is printed instead of retrying
- **Added** `ssh-easy.desktop` + `ssh-easy.png` shipped in `build/` for Linux desktop integration (Exec=ssh-easy, Terminal=true, category Network/ConsoleOnly); can be copied to `~/.local/share/applications/` or the desktop for double-click launch
- **Fixed** build.sh was missing `-ldflags "-H windowsgui"` for Windows targets; would have produced binaries with the wrong taskbar icon if the script was used instead of manual builds

## [v0.39] – Build 39
- **Added** clipboard paste via Shift+Insert inside SSH sessions on Windows: the app now intercepts the `ESC[2;2~` VT sequence, reads the system clipboard and feeds the content into the SSH stdin pipe. CRLF/LF line endings are normalized to CR so pasted multi-line text behaves as if each line was typed followed by Enter. Own stdin forwarder replaces `session.Stdin = os.Stdin` so the interception layer is available without breaking existing passthrough for normal keys and other VT sequences (arrow keys, function keys etc.)

## [v0.38] – Build 38
- **Fixed** double keypress required after leaving SSH terminal with `exit`: `flushStdinBuffer()` was called before the defers (`term.Restore`, `restoreVT`), which can produce console mode-change events after the flush. Moved flush to first-registered defer so it runs last — after all cleanup

## [v0.37] – Build 37
- **Fixed** TUI colors still missing (gray/white) after Build 36: Lipgloss initializes its global renderer at import time — with GUI subsystem there's no console yet, so it caches "Ascii" color profile. Fixed by calling `lipgloss.SetColorProfile(termenv.TrueColor)` immediately after `AllocConsole()`

## [v0.36] – Build 36
- **Fixed** colors missing in main TUI after GUI subsystem switch: `AllocConsole()` creates a console without `ENABLE_VIRTUAL_TERMINAL_PROCESSING` — now enabled immediately after console creation so Lipgloss/Bubbletea can render ANSI colors correctly

## [v0.35] – Build 35
- **Fixed** app icon still showing cmd icon on Windows 11 for non-admin users: switched to GUI subsystem (`-H windowsgui`) + `AllocConsole()` so ssh-easy owns the console window directly — taskbar and title bar now show the correct icon unconditionally

## [v0.34] – Build 34
- **Fixed** app icon not visible in title bar and taskbar for non-admin users: added `SetConsoleIcon` (undocumented kernel32 API) which sets the icon at OS level for the entire console — title bar, taskbar, Alt+Tab

## [v0.33] – Build 33
- **Fixed** app icon not visible in Windows taskbar for non-admin users: added `SetCurrentProcessExplicitAppUserModelID("KurtIngwer.ssh-easy")` so Windows assigns the taskbar button to the app instead of the console host (conhost.exe / Windows Terminal)

## [v0.29] – Build 29
- **Fixed** connection list alignment: selected and unselected entries now share the same indent, eliminating the 2-character offset
- **Removed** red `-` dash for disconnected entries; only connected entries show the green `●`

## [v0.28] – Build 28
- **Fixed** double-keypress required after closing SSH terminal: the trailing `\r` from `exit↵` was left in the kernel tty buffer and consumed by Bubbletea as the first keypress. Fixed by flushing stdin after `session.Wait()` (Unix: `SetNonblock` + drain; Windows: `FlushConsoleInputBuffer`)
- **Changed** status view: ESC no longer disconnects, only `x` does
- **Removed** manual key-gen (`g`) from main list – SSH key is generated automatically on first password login

## [v0.27] – Build 27
- **Changed** connection list now shows only `● Name  [ports]` – host, user and auth type removed (still visible in status view)
- **Added** duplicate name prevention: saving a connection with an already-used name is rejected (case-insensitive; own name excluded when editing)

## [v0.26] – Build 26
- **Added** green `●` indicator for connected entries in the connection list
- **Changed** after a successful connect the app now jumps directly to the status view instead of the list, so `t` for terminal is immediately available

## [v0.25] – Build 25
- **Fixed** auth cache failure counter: network errors (I/O timeout, connection refused, etc.) no longer increment the counter – only genuine SSH authentication rejections do

## [v0.24] – Build 24
- **Added** persistent auth method cache (`~/.ssh-easy/auth-cache.json`): remembers which SSH key or agent worked per connection and tries it first on the next connect
- **Added** `r` key in status view to manually clear the cached auth key for a connection
- After 2 consecutive auth failures the cached entry is deleted and full rediscovery runs

## [v0.23] – Build 23
- **Changed** connection list is now sorted alphabetically by name (case-insensitive)
- **Changed** ESC in status view now disconnects (same as `x`); `q` returns without disconnecting
- Updated help text in all 41 languages

## [v0.22] – Build 22
- **Fixed** Windows taskbar icon: the `.syso` resource files set the Explorer icon but the console host showed its own icon in the taskbar. Fixed by calling `GetConsoleWindow` + `LoadImage` + `SendMessage(WM_SETICON)` at startup via `init()`

## [v0.21] – Build 21
- **Fixed** `removeKnownHost`: hostname is now normalised (lowercase, explicit port stripped) before comparison with `known_hosts` entries

## [v0.20] – Build 20
- **Fixed** host-key change error message: German umlauts (Ä, ü, ö) were garbled due to encoding issue

## [v0.19] – Build 19
- **Changed** all binaries built with `CGO_ENABLED=0` for fully static linking – fixes startup failure on older Linux distributions (e.g. Linux Mint: `CXXABI_1.3.15 not found`)

## [v0.18] – Build 18
- **Fixed** goroutine leak caused by `time.Sleep` in keepalive/reconnect loops; replaced with select-based cancellable waits
- Security improvements

## [v0.17] – Build 17
- **Fixed** language list layout issues
- **Changed** English is now the default language on first start

## [v0.16] – Build 16
- **Added** scrollable language selection window (8 visible entries with arrow navigation)

## [v0.15] – Build 15
- **Added** i18n expanded to 41 languages; language selection list now two-column

## [v0.14] – Build 14
- **Fixed** missing translations for placeholders and tunnel labels

## [v0.13] – Build 13
- **Added** multilingual UI (i18n) with 14 languages and language selection on first start

## [v0.12] – Build 12
- **Fixed** terminal resize: window enlargement is now forwarded to remote programs (mc, vim, …)
- **Fixed** Windows app icon: ICO now uses BMP format instead of PNG
- **Added** Windows installer automatically uninstalls previous version
- **Fixed** ANSI/VT colour rendering in SSH terminal on Windows

## [v0.11] – Build 11 *(Windows hotfix)*
- **Fixed** ANSI/VT escape sequences shown as raw text in Windows SSH terminal

## [v0.10] – Build 10
- **Added** Windows NSIS installer with Start Menu entry and optional desktop icon
- **Fixed** Windows Alt-Tab causing SSH terminal to lose raw mode; terminal resize now works on Windows
- **Added** app icon embedded in Windows executable via `goversioninfo`/`.syso`

## [v0.9] – Build 9
- **Changed** port field now defaults to `22`
- **Changed** Enter key navigates between form fields (no longer requires Tab)

## [v0.8] – Build 8
- **Added** interactive remote terminal session via `t` key (PTY over existing SSH connection)
- **Improved** `known_hosts` cleanup on host-key mismatch

## [v0.7] – Build 7
- **Removed** auth-type and key-path fields from connection form; authentication is fully automatic

## [v0.6] – Build 6
- **Fixed** German umlauts in all UI texts and code comments

## [v0.5] – Build 5
- **Added** host-key-changed warning dialog: alerts the user when a server's host key has changed (possible MITM), with option to accept or abort

## [v0.4] – Build 4
- **Added** automatic SSH key authentication: tries SSH agent and all unencrypted keys from `~/.ssh/` without asking the user
- **Added** auto key-deploy: on first password login a new SSH key is generated and added to `authorized_keys` on the server

## [v0.3] – Build 3 *(initial public release)*
- SSH connection manager with terminal UI (Bubbletea)
- Save, edit, delete connections (stored in `~/.ssh-easy/connections.json`)
- Local port forwarding tunnels per connection
- Password and SSH-key authentication
- `known_hosts` verification (no `InsecureIgnoreHostKey`)
- Tunnels bound to `127.0.0.1` only
- Cross-platform: Linux x64/ARM64, Windows x64/ARM64
