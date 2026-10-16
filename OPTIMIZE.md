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
- **Verbindungs-Timeout konfigurierbar**: Seit Build 48 fest 5 s (erster Verbindungsaufbau) bzw. 10 s (Reconnect) – ggf. in connections.json einstellbar machen
- **Auth-Methoden in einem Handshake**: ConnectAuto baut für jeden Schlüssel eine eigene SSH-Verbindung auf (wegen Auth-Cache-Zuordnung). Ein einziger Handshake mit allen Methoden + PublicKeysCallback, der den erfolgreichen Signer merkt, spart bei vielen Keys deutlich Zeit
- **Auto-Update signieren**: Releases zusätzlich signieren (z.B. minisign/cosign) und den öffentlichen Schlüssel einbetten – schützt auch bei kompromittiertem GitHub-Konto
- **Fenstergröße unter Linux**: Optional per xterm-Sequenz `CSI 8;h;w t` wiederherstellen (nur Terminals, die das unterstützen)
- **i18n vereinheitlichen**: Translations-Struct (41 × ~80 Felder) schrittweise in die Schlüssel-Tabelle aus i18n_extra.go überführen
