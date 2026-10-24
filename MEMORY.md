# Projekt-Memory - ssh-easy

## Projektueberblick
- SSH-Verbindungsmanager mit Go TUI (Bubbletea/Lipgloss)
- Local Port Forwarding ueber golang.org/x/crypto/ssh
- Cross-Compilation: Linux/Windows x86/ARM
- Konfiguration: ~/.ssh-easy/connections.json
- Log-Datei: ~/.ssh-easy/ssh-easy.log

## Technologie
- Go 1.27.2 (seit Build 44; go.mod-Minimum 1.26.0)
- Bubbletea v1.3.10, Lipgloss v1.1.0, Bubbles v1.0.0
- golang.org/x/crypto v0.58.0 (Build 44, CVE-Fix)
- OSC 52: src/osc52.go filtert SSH-Ausgabe, schreibt Zwischenablage (conhost kann kein OSC 52)
- Bracketed Paste (Build 47): src/bracketed_paste.go - Tracker hinter dem OSC-52-Filter erkennt ESC[?2004h/l, preparePaste() schliesst Shift+Einfg-Text ein; forwardStdinWithPaste/attachOutputFilters haben dafuer einen pasteMode-Parameter

## Dateistruktur (Build 3)
- src/main.go - Einstiegspunkt (Logger + SSHManager)
- src/types.go - Datenstrukturen (Connection, TunnelConfig, AppConfig)
- src/config.go - JSON laden/speichern (atomar) + ConfigCache
- src/ssh.go - Tunnel-Helfer, Key-Generierung, Disconnect
- src/ssh_manager.go - SSHManager mit Reconnect, Keepalive, Agent
- src/logger.go - Datei-basiertes Logging
- src/tui.go - TUI-Core (Model, Styles, Dispatch)
- src/tui_list.go - Listenansicht
- src/tui_form.go - Formular + Connect
- src/tui_status.go - Statusansicht
- src/tui_keygen.go - Key-Generierung

## Datei-Manager (MC-Modus, ab Build 53, 2026-10-21)
- Letzter Listeneintrag (rowFileManager, Leerzeile davor) -> linke/rechte Verbindung wählen (ab Build 55: Platzhalter "(nicht ausgewählt)" zuerst, Enter darauf wirkungslos = Zwang zur bewussten Wahl; links gewählte rechts ausgeblendet) -> eigene SSH-Verbindungen ohne Tunnel (SSHManager.ConnectStandalone) + SFTP (github.com/pkg/sftp)
- Stand Build 65: F6 Verschieben (runCopy mit move: Quelle erst nach Umbenennen löschen, sameEntry-Prüfdatei gegen "Ziel = Quelle" über zweiten Verbindungseintrag, movesIntoItself), F7 MkDir (safeCopyName), F8 Löschen (runDelete, Bestätigung NUR j/y, kein Enter; Lstat, nie Symlinks folgen). src/ssh-easy aus dem Repo genommen (.gitignore). Build 64: F9 ohne Office-Formate. Build 62: F4 Editor wie mcedit + Hex-Editor (Build 62); F9 mit Erlaubnisliste (Build 61); Betrachter mit mc-F-Tasten + F9 lokal öffnen (Build 60); F3 Ansehen (Build 59), Kopieren gehärtet (Temp-Datei O_EXCL 0600 + posix-rename, Build 58). Build 57: F5 Kopieren (rekursiv, Symlinks als Link, Rechte/Zeit, Rückfrage j/n/a/k/u/g, Fortschritt), Eintrag unter 2 Verbindungen ausgegraut. Davor: Navigation, Tab, F10/q zurück, Markieren (Einfg = Eintrag + weiter, * = alles umkehren wie mc, Dateien UND Verzeichnisse); Menü + F1-F9 nur optisch; 3 Farbmodi vorbereitet (Umschalten später über Optionen)
- Mockups: claude-generated/mc-mockup.html
- Build 66-68: F8 prüft nach ReadDir (Lstat ganze Kette) und vor jedem Remove (Lstat Eltern) IMMER per Lstat, zusätzlich RealPath-Vergleich (remoteFS.RealPath); Fensterverzeichnis selbst darf Symlink sein (ohne RealPath: Vergleich mit Zustand beim Start, Build 69); errDeleteRace. RealPath nie als Ersatz (fail-open, Review Build 67)
- Build 70: "Lokaler PC" (localFS in filemanager_local.go, Pseudo-Verbindung fmLocalConnID, erster Eintrag der Auswahl; Windows: "/" = Laufwerksliste, "/C:/x" = C:\x, ".." über C:\ -> Laufwerke; F5-F8 in der Laufwerksliste gesperrt; validWindowsName + nameValidator; Server->PC markDownloads; Build 71: MOTW vor chmod auf Temp-Datei, Fehler = nicht ablegen; Build 74: Symlinks Server->PC gar nicht mehr anlegen, nur melden). Datei-Manager ab EINER Verbindung (txtFMNeedConnection)
- Build 73: Menüs wie mc (tui_filemanager_menu.go; F9 öffnet Menü des aktiven Fensters; Links/Rechts: Neu einlesen + Strg+R; Datei: F3-F8, *, F10; Optionen: Aussehen + Endungen für F9; "Befehl" entfällt). Einstellungen in AppConfig (file_manager_theme, file_manager_open_extensions). F9-Endungen: keine harte Sperre, Risikohinweis j/y + Rückfrage beim Öffnen. Herunterladen (tar.gz) vom Nutzer GESTRICHEN
- Agents (2026-10-22, überarbeitet): 24 Projekt-Agents in .claude/agents/ – lead-vorarbeiter (opus); specialists tui/filemanager/ssh/terminal/windows/security (opus), config/i18n/release (sonnet); analysts code-review/security-audit (fable), requirements/debug (opus), research (sonnet); worker test-writer/refactorer/docs (sonnet); helper e2e-sshd (sonnet), test-runner/cross-check/screen-preview/git-check/export-reader (haiku). Alle effort high, alle dürfen Subagents starten. Überschneidungsfrei: .claude/agents-shared/ZUSTAENDIGKEITEN.md (Aufgabenarten + Dateitabelle, jede Datei genau ein Eigentümer). Projektwissen aufgeteilt: .claude/CLAUDE.md = Kern (≤ 90 Zeilen), Fachwissen in .claude/rules/*.md (teils mit paths:). Hook .claude/hooks/check-project-structure.py (Stop blockierend + SessionStart) prüft Eigentümer, Längen, paths-Muster, Modell je Ebene, Agent-Werkzeug, Testzahl. Start: claude --agent lead-vorarbeiter. .claude/ ist gitignored (nur lokal).

## Bekannte Probleme
- /tmp auf dem LXC kann volllaufen (Cronjob eingerichtet)
- Locale de_DE.UTF-8 musste manuell generiert werden

## Build
- Build-Nummer in src/build.txt (hochzaehlen bei Aenderung)
- Output: build/ssh-easy (+ Varianten fuer ARM/Windows)

## Build 48 (2026-10-16) - 13 Nutzerwuensche
- Gruppen per Doppelpunkt, scrollbare Liste (15 Zeilen), Auto-Update (u, Standard aus), DNS-Fallback,
  5-s-Timeout + Abbruch bei Netzwerkfehler, Fenstergroesse merken, Tunnel "+N weitere"/rot,
  neue Verbindung ausgewaehlt, Shift+Einfg in Formularen (Windows NUL-Rune), Host+Benutzer bei Passwort
- Bug "erste Taste nach Terminal verschluckt": haengender stdin-Forwarder -> terminal_input*.go
- Alle Texte in 41 Sprachen; tests/i18n_complete_test.go + Stop-Hook .claude/hooks/check-translations.sh
- Offen: Taste g (Keygen) ohne Funktion (BUGS.md)

## Build 51 (2026-10-17) - Signierte Releases + Export
- Scanner 2.0.0: 25 HOCH -> 24 FP (ZWNJ in Kannada/Telugu/Persisch), 1 echt: Auto-Update ohne Signatur
- Fix: src/releasesig (Ed25519, Liste ssh-easy-SHA256SUMS + .sig, Kopfzeile bindet an Release-Tag),
  Signierwerkzeug src/cmd/release-sign, build.sh signiert automatisch
- Privater Schluessel: ~/.config/ssh-easy/release-signing.key (0600, NICHT im Repo, sichern!)
- Oeffentlicher Schluessel eingebettet in src/updater.go (releaseSigningPublicKeyHex)
- Releases haben jetzt 8 Assets (6 Binaries/Setups + SHA256SUMS + .sig) - ohne die beiden lehnt der Updater ab
- Export der Verbindungsliste (Taste E): alle/Auswahl/Gruppe, ohne key_path, AuthType key -> password, 0600, O_EXCL
- tests/source_hygiene_test.go verbietet Bidi/ZWSP/BOM im Quellcode

## Build 52 (2026-10-17) - Import
- Import (Taste I, auch bei leerer Liste): ViewImportFile (Dateiname, Vorschlag neueste Exportdatei) -> ViewImportSelect
- Gemeinsame Auswahlliste src/checklist.go (connChecklist) fuer Export und Import
- Vorhandene Namen: markiert, anfangs abgewaehlt; gewaehlt = ersetzen (lokale ID/CreatedAt/Key bleiben)
- Datei untrusted: 1 MiB, Validate + validGroupName, keine Steuer-/Cf-Zeichen (ZWNJ/ZWJ erlaubt), key_path verworfen, neue IDs

## Build 81 (2026-10-24) - Upload übers Internet + Hänger
- Nutzerbericht: 4-5 statt 40 Mbit beim Kopieren von ~/x (11.869 Dateien, 3.763 Verzeichnisse, 23 GB; 92 % der Bytes in 13 Dateien > 100 MB)
- Messverfahren: Wegwerf-sshd 127.0.0.1:2222 + Go-Proxy mit Latenz und 40-Mbit-Begrenzung (Test im Scratchpad, nicht im Repo); "schwarzes Loch" für tote Leitung
- pipeCopyAt (4 Blöcke per WriteAt gleichzeitig), copyParallel 32, setAttributes, dirJob (Verzeichnisse parallel, Einträge warten auf 0700-Elternverzeichnis), finishDirs ebenenweise parallel
- watchConnection/newSFTPFS: Keepalive 15 s, Timeout 45 s, dann client.Close; errConnectionLost + txtFMErrConnectionLost
- Fable-Audit: MITTEL behoben vor Release (gescheitertes Mkdir + Lstat durch untergeschobenen Symlink -> fremder Ordner); Review: Keepalive wertet jetzt auch eingehende Daten (activityReader), Esc nach Ablaufende = abgebrochen, dirPending-Grenze
- Offen: Temp-Datei nach Abriss bleibt liegen (BUGS.md), Download-Pausen pro Block (OPTIMIZE.md)

## Letzte Aenderung
- 2026-03-07: Build 3 - SSHManager, Reconnect, Agent, Keepalive, Logging, TUI-Split
