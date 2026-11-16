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
- Code: connectionNeedsOwnKey + autoDeployKeyFunc (tui.go, Naht für Tests), autoAuthOrder (eigener Schlüssel vor Agent), autoKeyName (_2, _3 je Verbindung), updateKeyFields (schreibt nur eigene Felder), autoDeployMu; Sperr-Reihenfolge deployMu (bis Build 89 fmDeployMu, ab Build 90 umbenannt) -> autoDeployMu -> configUpdateMu -> configFileMu; tui_filemanager_keydeploy.go für den Datei-Manager
- c/e: Tunnel behalten Zielport und Ein/Aus (vorher 8080->80 still zu 8080->8080), e behält Schlüssel/Anmeldeart/Erstelldatum, c: Cursor am Feldende, lange Namen gekürzt (" #N" in 50 Zeichen)
- authorized_keys: keine Doppelten, fehlender Zeilenumbruch, Rechte 700/600, Entfernen auch als einziger Eintrag; geteilte Schlüssel werden nicht gelöscht
- Datei-Manager-Meldungen überschreiben sich nicht mehr
- Fable-Review Runde 2/3: Liste nach gescheitertem Versuch neu laden und vor jeder Vergabe frisch aus der Konfiguration prüfen (Aussetzen greift in der Sitzung, e setzt Zähler nicht zurück); auskommentierter Schlüssel zählt nicht als vorhanden; alte Temp-Datei kann authorized_keys nicht überschreiben; BusyBox-grep verschweigt Schreibfehler -> Zeilenabgleich vor dem Ersetzen (Fehler gab es schon vor Build 86)
- Bewusst offen: aktive Zeile mit from=/restrict,command= gilt als "schon vorhanden" (BUGS.md Offen); OPTIMIZE.md: fish/csh ungeprüft, neutraler Name für fmDeployMu/fmFreshConnection, 3 gofmt-Abweichungen in ssh_manager.go (alle drei in Build 90 erledigt, siehe unten)
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

## Build 88 (2026-11-01) - Nacharbeiten aus dem Fable-Review von Build 87
- Keine neuen Texte, keine neuen Nutzerentscheidungen
- Notfall-Schließen (`runEmergency(deadline)`): Terminal wiederherstellen -> apply -> disconnect/cleanup/persist in EINER Goroutine mit gemeinsamem Zeitlimit `emergencyStepsTimeout` (750 ms, vorher `emergencyPersistTimeout`). Hängende Verbindung hält das Update nicht mehr auf
- `registerEmergencyRestore`/`runEmergencyRestore` (exit_signals.go): ssh_terminal.go meldet nach `term.MakeRaw` die Wiederherstellung an (Raw- und VT-Modus), Abmelden im Defer. Shell bleibt bei Kreuz/kill nicht mehr im Raw-Modus
- `watchCloseSignals(sigs, onSignal, quit, ...)`: `onSignal` setzt `closeRequested` synchron VOR `go quit()` (Linux: kein gelegentlicher Exit-Code 1 mehr). Falle: `TestMainSetsCloseRequestedInOnSignal` sucht das erste `p.Run()` in main.go - nie in Kommentaren davor schreiben
- Update-Helfer: `touchSetupDir` (Änderungszeit des Setup-Ordners beim Helfer-Start), Ordnervergleich exakt (kein `EqualFold`), Log „Update-Helfer für v0.N gestartet“ statt „installiert“
- Datei-Manager-Wahl: Speicherfehler (`fmSavePickChoice` gibt error zurück) wird angezeigt, Verbindung wird trotzdem aufgebaut. Import (`applyImport`) speichert über `updateConfig`
- 931 Tests in 54 Dateien

## Build 89 (2026-11-07) - Notfall-Reihenfolge, Editor-Einfügen, LC_ALL=C
- Notfall-Schließen (`exit_signals*.go`): Reihenfolge restore → apply → cleanup → persist → disconnect (disconnect zuletzt). Danach wartet `runEmergency(deadline, allDone)` im Restbudget auf `allDone`; kommt es nicht: Logeintrag „ssh-easy beendet (Notfall-Ablauf)“, zweites Restore aus der Momentaufnahme, unter Unix zusätzlich `emergencyResetSequence` (Bracketed Paste aus, Alt-Screen verlassen, Maus-Modi aus, Cursor zeigen) über `emergencyTerminalReset`, Limit `emergencyResetWriteTimeout` 250 ms, dann `exit(0)`. Budget 2,5 s (Windows 2,25 s, kein Terminalfolgen-Write). `watchCloseSignals` ruft `onSignal` vor dem ersten Logeintrag. Behebt die beiden NIEDRIG-Bugs aus Build 88
- `src/ssh.go`: jedes grep in den authorized_keys-Skripten mit `LC_ALL=C` und `-a` (Latin-1, NUL-Bytes; GNU und BusyBox geprüft)
- Datei-Manager-Wahl: Speicherfehler zusätzlich sofort in `fm.message` (Nutzerentscheidung), gemerkt in `fm.pickErr`, übersteht Verbindungsaufbau und Passwortabfrage; ein anderer Fehler hat Vorrang
- F4-Editor Einfügen (Nutzerwunsch): Strg+V, Shift+Einfg, Strg+Einfg (`src/tui_filemanager_editpaste.go`). Unix: ESC[2;2~ / ESC[2;5~ in `tui.go` an den Editor; Windows: NUL-Rune + `GetAsyncKeyState` (`ctrlInsertPressed`). `cleanEditPaste`: CRLF/CR → LF, Steuerzeichen außer Tab/LF raus (auch bei Bracketed Paste). Grenze `fmViewMaxBytes` (16 MiB in Bytes, CRLF zählt doppelt), frühe Ablehnung über 4× die Grenze, Meldung `txtFMEditPasteTooLarge`. Nur Textmodus; in den Fenstern markiert Strg+Einfg weiter
- Editor-Anzeige: U+2028/U+2029 als „?“ (Inhalt unverändert)
- Windows, Strg+Einfg bei langem Halten von Strg: Nutzer hat unter Windows 11 geprüft (2026-11-07), fügt nur einmal ein; vermuteter Bug geschlossen (nicht reproduzierbar)
- Offen: OPTIMIZE: Suchzeile F7 im Editor ohne Einfügen
- Tests: Testschärfe `TestSSHTerminalRegistersEmergencyRestore`, Kopf von `tests/exit_signals_test.go` aktualisiert
- 977 Tests in 56 Dateien

## Build 90 (2026-11-16; Arbeiten 12.-15.11.) - Schnelles Erfassen, Umbenennen beim Verschieben, Neu verbinden, Signatur
- Nutzerentscheidung: Datei `LICENSE` (MIT, Copyright (c) 2026 Kurt Ingwer); README.md/README.de.md verweisen im Abschnitt License/Lizenz darauf. Vorher stand nur "MIT" im README
- Umbenennung ohne Verhaltensänderung: `fmDeployMu` -> `deployMu`, `fmFreshConnection` -> `freshConnection` (beide werden auch vom Hauptablauf genutzt); `src/ssh_manager.go` gofmt-sauber. OPTIMIZE.md: beide Punkte erledigt
- Schlüsselskripte (`deployKeyScript`/`removeKeyScript`) unter fish/tcsh/csh geprüft: kein Fehler, keine Codeänderung. Neuer Regressionstest `tests/key_scripts_login_shell_test.go` (`TestKeyScriptsRunUnderLoginShells`, `…LoginShellCounterProbe`, `…FreeOfFishBackslashPairs`; fehlende Shell = Skip). Grenze: rc-Dateien des Servernutzers (`~/.cshrc`, `config.fish`) laufen mit
- Ersatz-Signaturschlüssel (Rotation): `releaseBackupSigningPublicKeysHex` in `src/updater.go`, `releasesig.VerifyAny` + `releasesig.ParsePublicKeysHex`, `Updater.signingKeys` (vorher `signingKey`). Gültig ist die Signatur jedes eingebetteten Schlüssels. Privater Ersatzschlüssel: `~/.config/ssh-easy/release-signing-backup.key` (Nutzer sichert ihn offline und löscht ihn danach aus dem LXC). NIE neu erzeugen, nie ausgeben. Rotation: Release mit dem Ersatz signieren (`SSH_EASY_SIGNING_KEY`), im selben Release neuen Ersatz einbetten
- `build.sh`: optionale Authenticode-Signatur (`osslsigncode`, SHA-256, RFC 3161, `verify`) über `SSH_EASY_CODESIGN_PKCS12`/`_PASSFILE`/`_PASSWORD`/`_TSA`/`_CAFILE`. Ohne Zertifikat übersprungen, Fehlkonfiguration bricht ab. Zertifikat fehlt noch (Optionen: `research/authenticode-optionen.md`)
- Editor F4: Einfügen (Strg+V, Shift+Einfg, Strg+Einfg) auch in die Eingabezeilen (F7 Suchen, F4 Ersetzen; `pasteIntoPrompt`): nur erste Zeile, Steuerzeichen weg, Tab -> Leerzeichen, `CharLimit` bleibt, unberührte Vorbelegung wird ersetzt; nicht in Ja/Nein-Dialogen; in der Hex-Ansicht nimmt die Eingabezeile es an (Text nicht). OPTIMIZE-Punkt erledigt
- Erfassen vor F5/F6 (Nutzerbericht: 30.000 leere Verzeichnisse, "Zählen dauerte unendlich"): `filemanager_scan.go` (zwei Schritte: paralleles Vorlesen mit `copyParallel` = 32 Arbeitern in einen `scanEntry`-Baum, dann `assemble` in EINER Goroutine in der alten Reihenfolge mit Grenzen, Namen, Fehlern). `filemanager_fastscan.go`: ein `find -P … -printf` über einen Exec-Kanal derselben SSH-Verbindung (`ListTree`), Startpfade NUL-getrennt über stdin an `xargs -0` (kein Quoting, fish/csh-fest), Befehlstext fest ohne Nutzerdaten; Ausgabe untrusted (Feld-, Gesamt-, Eintrags-, Tiefen-, Zeitgrenzen, `fastScanMaxBytes` 256 MiB, `fastScanTimeout` 10 min, `fastScanIdleTimeout` 30 s); jeder Name durch `safeCopyName`/`fsAcceptsName`; Startpfade per SFTP-RealPath kanonisch und in `validTreeStarts` geprüft (kein `-delete`); Rückfall auf SFTP bei kein Exec, BusyBox ohne -printf, Exit-Code, stderr, Limits, Abweichung. `filemanager_fastscan_sample.go`: Stichprobe (16 Lstat, davon 4 tiefste; 8 ReadDir bis 256 Kinder), jede Abweichung -> SFTP
- E2E-Messwerte (ca. 30 ms Latenz): 30.000 Verzeichnisse mit find 0,34 s, mit SFTP 119 s; vorher streng seriell weit länger (der Auftraggeber nannte etwa das 60-Fache des SFTP-Rückfalls von 119 s; Bezug genannt, aber durch keine Messung belegt). Anzeige `txtFMCopyScanned` ("Erfasse … %d Dateien, %d Verzeichnisse", ersetzt `txtFMCopyScanning`) und `txtFMMoveCleanup` ("Entferne leere Quellverzeichnisse … %d/%d"); Zeilen werden umbrochen
- Nachtrag (Punkt 8 des Auftrags): Leerlauf-Wächter (`idleReader`) läuft schon VOR `ListTree` (hängt schon das Öffnen des Exec-Kanals); nach hängendem Kanal, Leerlauf- oder Zeitlimit schaltet `disableFastScan` den Schnellweg für die Verbindung bis Sitzungsende ab (Interface `fastScanSwitch`, `sftpFS.noFastScan`/`disableFastScanForSession`/`fastScanDisabled`), sonst kostete jedes F5/F6 wieder bis 30 s + 5 s. Neu verbinden übernimmt die Abschaltung (`inheritFastScanOff`); Esc wirkt sofort (`runTreeList` in eigener Goroutine); hängt der Kanal nach `fastScanAbortGrace` (5 s) noch, trennt `AbortTreeList` die Verbindung (`errTreeListHung` -> `errConnectionLost`); find-Daten zählen als Lebenszeichen für `watchConnection`
- F6 auf demselben Konto (Nutzerentscheidung, auch bei zwei Einträgen): `copyRequest.sameAccount` (`fmSameAccount`: Host ohne Groß-/Kleinschreibung, Port, Benutzer), `renameMoved` in `filemanager_move.go`, Schnittstelle `noReplaceRenamer`/`RenameNoReplace` (sftpFS: SSH_FXP_RENAME, bewusst NICHT posix-rename@openssh.com, das ersetzt; localFS: `renameat2` RENAME_NOREPLACE in `filemanager_rename_linux.go`, `MoveFileEx` ohne Ersetzen unter Windows, `filemanager_rename_other.go` meldet nicht unterstützt). Prüfdatei `.ssh-easy-probe-…` einmal pro Lauf beweist dasselbe Dateisystem; jeder Fehler -> still Kopierweg; Ziel vorhanden -> Kopierweg (nie ersetzen). `copyResult.renamed`, `copiedNames` behält umbenannte Namen auch nach Abbruch; Texte `txtFMMoveDoneOne`/`txtFMMoveDoneEntries`. Anderer Benutzer: weiter kopieren (Besitzer/Rechte). OPTIMIZE-Punkt erledigt
- Schneller und sicherer: `remoteFS.RemoveDir` (SFTP RMDIR bzw. rmdir; kein unlink-Versuch wie `Remove`), `removeMovedDirs` löscht leere Quellverzeichnisse ebenenweise parallel, tiefste zuerst, nur per `RemoveDir`; `readPanelDir` prüft Symlink-Ziele parallel (`fmStatParallel` 32); F8 liest vorab parallel (`prefetchDelete`, `deleteParallel` 32), Prüfen und `Remove` in EINER Goroutine, Kettenprüfung `unchanged(top, p, true)` beim Betreten jedes Verzeichnisses (Audit F1); nur sichere Namen zählen zur Eintragsgrenze `copyMaxEntries` (SFTP, find und Löschen); Geschwister nach Namen sortiert
- Neu verbinden nach Verbindungsabriss (`tui_filemanager_reconnect.go`): `fmReadPanelCmd` liest ein Fenster und verbindet bei Bedarf neu (genau ein Versuch pro Aktion, Pfad bleibt); `sftpFS.lostAfter` bewertet Fehler (SFTP-Status = Verbindung lebt; EOF/Netzfehler bis `fmLostGrace` 1 s auf die Überwachung warten; `lostCh`/`markLost`), erkennt auch eine gestorbene reine SFTP-Sitzung; Anmeldung über `m.fmConnect` = `ConnectStandalone` (known_hosts Pflicht, Auto-Auth), sonst Dialog `fmDialogReconnect` (Passwort nur für diesen Versuch); Host-Key geändert/Netzfehler nur Meldung; Texte `txtFMReconnected/Failed/Title/Prompt/Help`. Laufende oder abgebrochene Kopien werden nie fortgesetzt. OPTIMIZE-Punkt erledigt
- Bugs (BUGS.md): Abbruch beim Erfassen galt als Erfolg; `overlayAt` ersetzt ein an der rechten Kastenkante halb verdecktes breites Zeichen (CJK) durch ein Leerzeichen (Dialogzeilen waren 1 Spalte zu breit, seit Build 89 oder früher); `osslsigncode` schreibt nicht in eine vorhandene Datei (Signatur wäre nie gelungen)
- Sicherheitsaudit Build 90 (Details SECURITY_FIXED.md, nicht von worker-docs gepflegt): F8-Kettenprüfung, `RemoveDir`, Stichprobe, Esc/Zeitgrenzen des Schnellwegs unabhängig vom Server, `validTreeStarts`, `releasesig.ValidPublicKey` (schwache Schlüssel abgelehnt), Signierpasswort nicht mehr in der Umgebung der Kindprozesse. `build.sh`: frühe Prüfung `codesign_check_config` ganz am Anfang von main (vor Tests und Builds), Uninstall.exe wird über NSIS `!uninstfinalize` (NSIS >= 3.08 Pflicht, `-DSIGN_UNINSTALLER=1`, `SSH_EASY_SIGN_SCRIPT`) mitsigniert, `SSH_EASY_CODESIGN_PASSFILE` bevorzugt, `SSH_EASY_BUILD_SOURCE_ONLY=1` lädt das Skript für Tests ohne Bau
- Vorgemerkt (OPTIMIZE.md; in Build 91 auf 92/93 verschoben): Build 91 Massenaufrufe per Exec bündeln (rm/rmdir, mkdir, chmod/touch, find -maxdepth 1, Zählen vor F8; kein tar-Strom); Build 92 Direktübertragung Server -> Server (rsync mit eingeschränktem, weitergereichtem Agenten, Risikohinweis, Rückfall); Einträge je Verzeichnis nach dem Kopieren abgleichen (gegen die Stichproben-Grenze)
- Bewusst offen: Zertifikat für Authenticode fehlt (`research/authenticode-optionen.md`); voll paralleles Löschen nicht gewollt (Nutzerentscheidung)
- 1240 Tests in 72 Dateien (gezählt am 2026-11-16, nach den Tests von specialist-filemanager: `filemanager_fastscan_switch_test.go`, `filemanager_sftpfs_test.go` mit In-Prozess-SSH/SFTP-Server)

## Build 91 (2026-11-16) - Windows-Kreuz installiert Update, Wiederholungssuche, Log
- Nutzerbericht 2026-11-16: Schließen mit dem Kreuz unter Windows installierte kein Update, `q` schon. Ursache (belegt): `AllocConsole` (`icon_windows.go` `init()`, nötig für das Taskleisten-Icon) setzt laut Microsoft die Ctrl-Handler-Tabelle zurück. Gos Handler war vorher registriert (`runtime.goenvs`), niemand meldete neu an. Windows beendete den Prozess beim Kreuz daher sofort (seit Build 35): kein Trennen, kein Aufräumen der F9-Kopien, kein Speichern von Schrift/Fenstergröße, kein Update, kein Logeintrag
- Die Annahme aus Build 87 (BUGS.md Punkt 3, "Das Signal erreichte main.go") war unter Windows nie geprüft und ist falsch; in BUGS.md vermerkt. Lehre: Windows-Verhalten nie aus der Go-Dokumentation ableiten, solange `AllocConsole`/`FreeConsole` im Spiel ist
- Fix: `src/exit_signals_console.go` (plattformneutral, unter Linux testbar: `consoleCtrlEventSignal`, `closeEventRelay`, `consoleCloseRelay`, `handleConsoleCtrlEvent`, `consoleCtrlHoldTimeout` 10 s, `logConsoleCtrlRegistration`), Anmeldung `registerConsoleCtrlHandler` in `icon_windows.go` nach `AllocConsole` (`consoleCtrlRegistrationState`). CLOSE/LOGOFF/SHUTDOWN = SIGTERM über das Relay an `watchCloseSignals` (LOGOFF/SHUTDOWN kommen vermutlich nie an: user32.dll geladen); Strg+C/Strg+Pause unverändert. Log "Konsolen-Handler registriert" (bei Fehler Warnung)
- Unter Windows vom Nutzer noch zu bestätigen: Kreuz im Hauptmenü, im Datei-Manager und in der SSH-Sitzung, je in conhost und Windows Terminal; Alt+F4; `q` unverändert
- Update-Suche wiederholt sich alle 6 h, solange kein Update bereitliegt (`updateRecheckInterval`, `scheduleRecheckCmd`, `handleRecheckTick`; `updateResultMsg{tick, gen}`, `Updater.recheckGen` gegen doppelte Wecker, `cancelRechecks` beim Umschalten von Auto-Update). Keine Suche beim Schließen; ein schon vorgemerktes Update wird nicht durch ein neueres ersetzt
- Logs: Startzeile "ssh-easy v0.N gestartet (os/arch, PID n)", Endzeile mit PID (`formatStartLogLine`/`formatExitLogLine` in main.go), Ergebnis jeder Update-Suche, `ApplyOnExit` meldet "Kein Update vorgemerkt" bzw. "Auto-Update aus, nichts installiert" einmal je Beenden
- README (beide Sprachen): Abschnitt Auto-Update (6 h, Kreuz-Fehler bis Build 90) und Log-Datei (PowerShell 5: `Get-Content ... -Tail 40 -Encoding UTF8`, sonst "Ã¼" statt "ü")
- Build 91 ist ein Bugfix-Build: OPTIMIZE.md/FEATURES.md verschoben - Massenaufrufe per Exec und `find` nach Neuverbinden erneut versuchen auf Build 92, Direktübertragung Server -> Server auf Build 93
- 1296 Tests in 75 Dateien (gezählt 2026-11-16; neu: `exit_signals_console_test.go` 13, `updater_recheck_test.go` 35, `startup_log_line_test.go` 6)

## Letzte Aenderung
- 2026-03-07: Build 3 - SSHManager, Reconnect, Agent, Keepalive, Logging, TUI-Split
