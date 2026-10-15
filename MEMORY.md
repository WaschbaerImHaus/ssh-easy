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

## Bekannte Probleme
- /tmp auf dem LXC kann volllaufen (Cronjob eingerichtet)
- Locale de_DE.UTF-8 musste manuell generiert werden

## Build
- Build-Nummer in src/build.txt (hochzaehlen bei Aenderung)
- Output: build/ssh-easy (+ Varianten fuer ARM/Windows)

## Letzte Aenderung
- 2026-03-07: Build 3 - SSHManager, Reconnect, Agent, Keepalive, Logging, TUI-Split
