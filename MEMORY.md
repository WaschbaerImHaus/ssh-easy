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
- Letzter Listeneintrag (rowFileManager, Leerzeile davor) -> linke/rechte Verbindung wählen -> eigene SSH-Verbindungen ohne Tunnel (SSHManager.ConnectStandalone) + SFTP (github.com/pkg/sftp)
- Rohfassung: Navigation, Tab, F10/q zurück; Menü + F1-F9 nur optisch; 3 Farbmodi vorbereitet (Umschalten später über Optionen)
- Mockups: claude-generated/mc-mockup.html
- Nächste Schritte laut Nutzer: Kopieren, Verschieben, MkDir, Löschen, Markieren, Ansehen, Bearbeiten, Optionen-Menü

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

## Letzte Aenderung
- 2026-03-07: Build 3 - SSHManager, Reconnect, Agent, Keepalive, Logging, TUI-Split
