# ssh-easy

Ein einfacher SSH-Verbindungsmanager mit Terminal-UI (TUI) zum Verwalten und Aufbauen von SSH-Verbindungen mit Local-Port-Forwarding-Tunneln.

## Features

- SSH-Verbindungen speichern und verwalten (Name, Host, Port, User)
- Automatische Authentifizierung: SSH-Agent, alle Keys in `~/.ssh/`, Passwort-Fallback
- Local Port Forwarding (localhost:port -> remote:port)
- SSH-Key-Generierung (Ed25519) direkt aus der TUI
- Automatischer Reconnect bei Verbindungsabbruch (max. 5 Versuche)
- SSH-Keepalive (alle 30 Sekunden)
- Farbige TUI mit Statusanzeige
- Logging nach ~/.ssh-easy/ssh-easy.log
- Cross-Platform: Linux (x86/ARM), Windows (x86/ARM)

## Installation

Die kompilierten Dateien befinden sich im `build/`-Verzeichnis:

| Datei | Plattform |
|-------|-----------|
| `ssh-easy` | Linux x86_64 |
| `ssh-easy-linux-arm64` | Linux ARM64 |
| `ssh-easy-windows-amd64.exe` | Windows x86_64 |
| `ssh-easy-windows-arm64.exe` | Windows ARM64 |

## Benutzung

Programm starten:
```bash
./build/ssh-easy
```

### Tastenbelegung

| Taste | Aktion |
|-------|--------|
| `n` | Neue Verbindung anlegen |
| `e` | Verbindung bearbeiten |
| `d` | Verbindung loeschen |
| `Enter` | Verbindung herstellen / Status anzeigen |
| `x` | Verbindung trennen |
| `g` | SSH-Key generieren (Ed25519) |
| `j/k` oder Pfeiltasten | Navigation |
| `Tab` | Naechstes Formularfeld |
| `Esc` | Zurueck / Abbrechen |
| `q` / `Ctrl+C` | Beenden |

### Verbindung anlegen

1. `n` druecken
2. Felder ausfuellen:
   - **Name**: Anzeigename (z.B. "Webserver")
   - **Host**: IP oder Hostname
   - **Port**: SSH-Port (Standard: 22)
   - **Benutzer**: SSH-Benutzername
   - **Tunnel-Ports**: Kommagetrennte Portliste (z.B. `3306,8080,5432`)
3. `Enter` zum Speichern

### Authentifizierung

Die Authentifizierung laeuft vollautomatisch – keine manuelle Auswahl noetig:

1. **SSH-Agent** – wird zuerst geprueft, falls ein laufender Agent vorhanden ist
2. **SSH-Keys** – alle Keys in `~/.ssh/` werden automatisch ausprobiert
3. **Passwort** – wird als Fallback abgefragt, falls alles andere scheitert (nie gespeichert)

Nach einem erfolgreichen Passwort-Login kann ssh-easy automatisch einen Ed25519-Key generieren und deployen, damit zukuenftige Logins passwortlos funktionieren.

### Tunnel

Tunnel-Ports werden als kommagetrennte Liste angegeben. Jeder Port wird als Local-Port-Forward eingerichtet:

```
localhost:3306 -> remote:3306
localhost:8080 -> remote:8080
```

### SSH-Key generieren

1. `g` druecken
2. **Dateipfad** eingeben (z.B. `~/.ssh/id_ed25519_myserver`)
3. Optional eine **Passphrase** eingeben
4. `Enter` zum Generieren
5. Der Public Key wird angezeigt und kann auf dem Zielserver in `~/.ssh/authorized_keys` eingetragen werden

### Auto-Reconnect

Bei einem Verbindungsabbruch versucht ssh-easy automatisch, die Verbindung wiederherzustellen (max. 5 Versuche mit 3 Sekunden Abstand).

## Konfiguration

Verbindungen werden gespeichert unter:
- Linux: `~/.ssh-easy/connections.json`
- Windows: `%USERPROFILE%\.ssh-easy\connections.json`

Log-Datei:
- `~/.ssh-easy/ssh-easy.log`

Passwoerter werden **nicht** gespeichert und muessen bei jedem Verbindungsaufbau eingegeben werden.

## Sicherheit

- Passwoerter werden nur zur Laufzeit im Speicher gehalten
- Tunnel binden ausschliesslich auf 127.0.0.1
- Host-Keys werden gegen `~/.ssh/known_hosts` geprueft (kein InsecureIgnoreHostKey)
- Unbekannte Hosts werden nach Bestaetigung zur known_hosts hinzugefuegt
- Geaenderte Host-Keys werden mit MITM-Warnung abgelehnt
- Konfigurationsdatei hat Berechtigung 0600
- Atomares Schreiben der Konfiguration
