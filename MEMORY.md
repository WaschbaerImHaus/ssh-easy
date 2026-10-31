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

## Build 82 (2026-10-26) - Download-Tempo, Temp-Aufräumen, eine Verbindung
- readChunksAt: Quellen mit io.ReaderAt bis zu 4 Blöcke gleichzeitig, langsamer Start (erst nach vollem 1. Block), copyPipeChunks 10; Messung 40 ms: 35,7 -> 38,8 Mbit/s, kleine Dateien unverändert
- Temp-Aufräumen (filemanager_stale_temp.go): vormerken in listTarget, löschen am Laufende nach Lstat-Vergleich; 24 h, Server nur 0600, max. 100/Lauf, Log über copyRequest.logf
- Audit MITTEL behoben: Prüfdatei sameEntry eigener Name (.ssh-easy-probe-), fail-closed (errMoveCheckFailed)
- Nutzerwunsch: "Datei-Manager (eine Verbindung)" (rowFileManagerSingle, fm.single): ein Fenster, F5/F6 entfallen, Menüs Fenster/Datei/Optionen; F7/F8 bleiben (Auslegung, Nutzer informiert)
- Container-Neustart 2026-10-26 05:00 unterbrach die vorige Sitzung (kein Programmfehler); Sanitize-Lauf scheitert seit jeher mit "Not logged in" wegen --bare (sanitize.sh, Nutzer informiert)

## Build 83 (2026-10-27) - Windows-Setup blieb still auf alter Version
- Nutzer: "bleibt auf Build 81 hängen". Release v0.82 war korrekt; Setup: Delete der laufenden/gesperrten ssh-easy.exe scheiterte, File entpackte ssh-easy-windows-amd64.exe daneben, Rename still gescheitert, Exit 0 (typisch beim Auto-Update: Setup startet, während ssh-easy sich beendet)
- Fix: RemoveOldBinary (Delete -> Rename .old/.oldN -> warten 30 s still / 3 s UI -> Rückfrage -> Abort, Exit 2) -> RunOldUninstaller (ExecWait mit _?=, erst nach der Prüfung) -> File /oname=ssh-easy.exe; DisplayVersion aus -DVERSION/-DBUILD (vorher immer "0.15.0 (Build 15)")
- Prüfen: debugging/installer_wine_check.sh (wine + wine32:i386 im LXC installiert, 6 Fälle)
- Nutzer strich die offenen Punkte (3 Features + Bug Taste g); "X während SSH-Sitzung" steht noch unter Offen (nachgefragt)

## Build 84 (2026-10-27) - Hauptmenü m, Kopie c, Reset r, sofort speichern
- m: showMenu (Standard aus, Hinweis "m:Menü", Update-Status bleibt sichtbar), listChromeLines 14
- c: connection_copy.go nextCopyName (" #N", erste freie Zahl ab 2, vorhandenes " #N" ersetzt); keine Schlüssel. Build 85: Host wird mitkopiert (Nutzerwunsch), Fokus auf Name wie bei n
- r: console_state.go, consoleDefaults = Stand beim Start vor applyStartup*; Standardwerte werden als 0 gespeichert
- Sofort speichern: handleWindowPoll + consoleStateSaver im SSH-Terminal (alle 2 s); updateConfig mit configUpdateMu/configFileMu
- Fable-Review: r in der Statusansicht (Schlüssel löschen) jetzt mit j/y-Rückfrage; WT-Schrift = nicht steuerbar
- README: g-Taste entfernt (funktionierte seit langem nicht)

## Build 86 (2026-10-30) - Schlüssel nach jeder Anmeldung, Merken und aussetzen
- Nutzerentscheidung: JEDE erfolgreiche Anmeldung (Passwort, Agent, anderer Schlüssel aus ~/.ssh; Hauptablauf und Datei-Manager) vergibt einer Verbindung ohne eigenen Schlüssel ungefragt einen - bewusst auch nach Agent-Login. Datei ~/.ssh/id_ed25519_<Host>_<Benutzer>[_N], OHNE Passphrase (muss im README stehen). Vorher nur nach Passwort-Eingabe, Verbindungen blieben auf "password"
- Nutzerentscheidung "Merken und aussetzen": nach Entfernen des Schlüssels (Statusansicht r, j) und nach 2 gescheiterten Versuchen (autoKeyMaxFailures) keine Vergabe mehr für diese Verbindung (AutoKeyOff/AutoKeyFailures, nicht im Export); Passwort-Anmeldung versucht es wieder und schaltet ein
- Code: connectionNeedsOwnKey + autoDeployKeyFunc (tui.go, Naht für Tests), autoAuthOrder (eigener Schlüssel vor Agent), autoKeyName (_2, _3 je Verbindung), updateKeyFields (schreibt nur eigene Felder), autoDeployMu; Sperr-Reihenfolge fmDeployMu -> autoDeployMu -> configUpdateMu -> configFileMu; tui_filemanager_keydeploy.go für den Datei-Manager
- c/e: Tunnel behalten Zielport und Ein/Aus (vorher 8080->80 still zu 8080->8080), e behält Schlüssel/Anmeldeart/Erstelldatum, c: Cursor am Feldende, lange Namen gekürzt (" #N" in 50 Zeichen)
- authorized_keys: keine Doppelten, fehlender Zeilenumbruch, Rechte 700/600, Entfernen auch als einziger Eintrag; geteilte Schlüssel werden nicht gelöscht
- Datei-Manager-Meldungen überschreiben sich nicht mehr
- Fable-Review Runde 2/3: Liste nach gescheitertem Versuch neu laden und vor jeder Vergabe frisch aus der Konfiguration prüfen (Aussetzen greift in der Sitzung, e setzt Zähler nicht zurück); auskommentierter Schlüssel zählt nicht als vorhanden; alte Temp-Datei kann authorized_keys nicht überschreiben; BusyBox-grep verschweigt Schreibfehler -> Zeilenabgleich vor dem Ersetzen (Fehler gab es schon vor Build 86)
- Bewusst offen: aktive Zeile mit from=/restrict,command= gilt als "schon vorhanden" (BUGS.md Offen); OPTIMIZE.md: fish/csh ungeprüft, neutraler Name für fmDeployMu/fmFreshConnection, 3 gofmt-Abweichungen in ssh_manager.go
- 823 Tests in 49 Dateien

## Build 87 (2026-10-31) - Verbindung merken, ein Datei-Manager, Update beim Schließen
- Nutzerentscheidung: Schalter v „Verbindung merken“ im Menü (m), Standard AUS. Nur wenn an, werden die zuletzt mit Enter gewählte Verbindung und die letzte Wahl links/rechts im Datei-Manager sofort gespeichert und beim Start vorausgewählt. Ausschalten löscht die Werte; gelöschte Verbindung in der Vorauswahl wird ignoriert; nicht im Export (AppConfig: RememberSelection, LastConnectionID, FileManagerLastLeft/-Right; rememberLocalPC = "local-pc", rememberNone = "none")
- Nutzerentscheidung: KEIN Platzhalter "(nicht ausgewählt)" mehr in der Datei-Manager-Auswahl (links wie rechts); Cursor auf dem ersten Eintrag bzw. der gemerkten Wahl
- Nutzerentscheidung: nur noch EIN Eintrag "Datei-Manager", nur ab 1 Verbindung; rein lokaler Datei-Manager ohne Verbindung entfällt; rechts "(keine)" = ein Fenster (ersetzt den Eintrag "eine Verbindung")
- Nutzerentscheidung: bei genau einer Verbindung wird rechts TROTZDEM gefragt (sonst kein Kopieren Server <-> PC)
- r löscht jetzt IMMER auch die gemerkte Auswahl; ohne steuerbare Konsole nur die Auswahl, mit Hinweis (txtSelectionResetOnly)
- Bugfix (Nutzerwunsch): Update wird auch beim Schließkreuz/Alt+F4 installiert, auch in der SSH-Sitzung. Ursachen: SIGHUP unter Linux unbehandelt; Windows CTRL_CLOSE_EVENT + 5-s-Grenze + UAC-Dialog blockiert; Bubbletea verwirft Signale während tea.Exec und ruft nach Run() signal.Stop. Fix: exit_signals*.go (closeSignals, exitTasks, watchCloseSignals, exitCodeFor), Updater: installOnExit/ApplyOnExit/applyMu, Windows-Helfer `--install-update` (updater_helper.go), icon_windows.go init() überspringt den Helfer. Grenze: Abmelden/Herunterfahren gibt Windows keine Gelegenheit. Unter Windows vom Nutzer noch zu bestätigen
- Speichern von Auto-Update-Schalter, Sprache, Datei-Manager-Optionen und Schriftgröße läuft über updateConfig (gesperrt)
- 7 neue Texte (txtFMEntry, txtFMNone, txtRememberOn/Off, txtHelpMenuExtra, txtConsoleResetAll, txtSelectionResetOnly), alte entfernt, alle 41 Sprachen
- 899 Tests in 54 Dateien

## Letzte Aenderung
- 2026-03-07: Build 3 - SSHManager, Reconnect, Agent, Keepalive, Logging, TUI-Split
