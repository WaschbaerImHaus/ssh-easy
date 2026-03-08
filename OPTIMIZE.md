# Optimierungsvorschlaege - ssh-easy

## Umgesetzt (Build 3)

- **TUI aufgeteilt**: In tui_list.go, tui_form.go, tui_status.go, tui_keygen.go
- **SSHManager-Struct**: Zentrale Verbindungsverwaltung mit Mutex-Schutz
- **Connection-Pooling**: Verbindungen werden im Manager gehalten und wiederverwendet
- **Lazy-Loading**: ConfigCache laedt nur bei Dateiaenderungen neu
- **SSH-Agent**: Unterstuetzung ueber SSH_AUTH_SOCK
- **Keepalive**: SSH-Keepalive alle 30 Sekunden
- **Logging**: Datei-basiertes Logging mit Levels (Info, Warn, Error)

## Offen

- **Log-Rotation**: Automatische Rotation/Komprimierung der Log-Datei
- **Config-Backup**: Vor Aenderungen Backup der Config-Datei erstellen
- **Terminal-Groesse**: TUI an Fenstergroesse anpassen (responsive Layout)
- **Verbindungs-Timeout konfigurierbar**: Aktuell fest 10 Sekunden
