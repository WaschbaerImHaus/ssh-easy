# Changelog – ssh-easy

All notable changes to this project are documented here.
Format loosely follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

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
