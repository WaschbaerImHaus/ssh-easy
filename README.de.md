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
- Kopieren aus Remote-Programmen: OSC-52-Sequenzen (z.B. Claude Code, vim, tmux) landen in der lokalen Zwischenablage – auch in der Windows-Konsole, die OSC 52 selbst nicht beherrscht
- Gruppen: Ein Doppelpunkt im Namen gruppiert Verbindungen (`Büro:Server1`, `Büro:NAS` → Gruppe *Büro*), Gruppen lassen sich auf- und zuklappen
- Scrollbare Liste: Mehr als 15 Einträge werden in einem Bildlauf-Fenster gezeigt (`↑ +N` / `↓ +N` nennen die verborgenen Zeilen)
- Schnelle Meldung bei nicht erreichbarem Ziel: 5 Sekunden Verbindungs-Timeout, kein erneutes Warten pro SSH-Schlüssel
- DNS-Fallback: Versagt der DNS-Server des Netzwerks, wird über 1.1.1.1, 1.0.0.1, 8.8.8.8 und 8.8.4.4 aufgelöst (rotierend)
- Optionales Auto-Update über GitHub-Releases (Standard: aus, Taste `u`)
- Merkt sich die Fenstergröße (Windows-Konsole, außer maximiert/Vollbild; Windows Terminal merkt sie selbst)
- 41 Oberflächensprachen
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
| `→` / `Enter` auf einer Gruppe | Gruppe aufklappen |
| `←` | Gruppe zuklappen (auch von einem Gruppeneintrag aus) |
| `Bild↑` / `Bild↓` / `Pos1` / `Ende` | In langen Listen blättern |
| `u` | Auto-Update ein-/ausschalten (letzte Menüzeile) |
| `Tab` | Naechstes Formularfeld |
| `Strg+V` / `Shift+Einf` | Zwischenablage in Formularfeld und Passwortabfrage einfügen (auch unter Windows) |
| `Strg+Hoch` / `Strg+Runter` | Schriftgroesse aendern (Windows, wird gespeichert; Strg+Mausrad geht auch) |
| `Esc` | Zurueck / Abbrechen |
| `q` / `Strg+C` / `Alt+F4` | Beenden (Alt+F4 auch in laufender SSH-Session) |

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

### Gruppen

Ein Doppelpunkt im Verbindungsnamen trennt Gruppe und Eintrag. `Büro:Server1` und `Büro:NAS` erscheinen als Gruppe **Büro** mit den Einträgen *Server1* und *NAS*. Getrennt wird nur am ersten Doppelpunkt (`Büro:Rack:1` → Gruppe *Büro*, Eintrag *Rack:1*). Vor und nach dem Doppelpunkt muss Text stehen. Ein grüner Punkt an der Gruppe bedeutet: mindestens ein Eintrag ist verbunden. Eine neu angelegte Verbindung ist sofort ausgewählt (ihre Gruppe wird aufgeklappt).

### Tunnel-Anzeige

Die Liste zeigt die ersten zwei Tunnel-Ports, weitere nur noch als Anzahl: `[Tunnel: 3306,8080 +3 weitere]`. Nach dem Verbinden erscheinen Ports, deren Tunnel nicht aufgebaut werden konnte (z.B. Port belegt), rot – in der Liste und in der Statusansicht. Details stehen in der Log-Datei.

### Passwortabfrage

Schlägt die automatische Anmeldung fehl, fragt ssh-easy nach dem Passwort und zeigt dabei Host und – in der Zeile darunter – den Benutzernamen, damit klar ist, welches Passwort gemeint ist.

### Auto-Update

Standardmäßig aus. Mit `u` im Hauptmenü einschalten (der Zustand steht in der letzten Menüzeile und wird gespeichert). Eingeschaltet prüft ssh-easy beim Start das neueste GitHub-Release und lädt die passende Datei im Hintergrund; installiert wird beim Beenden:

- Linux oder Windows mit Schreibrecht im Programmordner: Die Programmdatei wird ersetzt
- Windows-Installation unter *Programme*: Das Setup läuft still mit Administratorrechten (Windows fragt per Benutzerkontensteuerung); die Desktop-Verknüpfung bleibt erhalten

Jeder Download wird gegen die von GitHub veröffentlichte SHA-256-Prüfsumme, die Größe und das Dateiformat geprüft – direkt nach dem Download und noch einmal unmittelbar vor der Installation.

### Auto-Reconnect

Bei einem Verbindungsabbruch versucht ssh-easy automatisch, die Verbindung wiederherzustellen (max. 5 Versuche mit 3 Sekunden Abstand).

## Konfiguration

Verbindungen werden gespeichert unter:
- Linux: `~/.ssh-easy/connections.json`
- Windows: `%USERPROFILE%\.ssh-easy\connections.json`

Log-Datei:
- `~/.ssh-easy/ssh-easy.log`

Passwoerter werden **nicht** gespeichert und muessen bei jedem Verbindungsaufbau eingegeben werden.

Weitere Einstellungen in `connections.json`: `language`, `font_size`, `window_cols` / `window_rows` (letzte Fenstergröße, nur Windows) und `auto_update`.

## Sicherheit

- Passwoerter werden nur zur Laufzeit im Speicher gehalten
- Tunnel binden ausschliesslich auf 127.0.0.1
- Host-Keys werden gegen `~/.ssh/known_hosts` geprueft (kein InsecureIgnoreHostKey)
- Unbekannte Hosts werden nach Bestaetigung zur known_hosts hinzugefuegt
- Geaenderte Host-Keys werden mit MITM-Warnung abgelehnt
- Konfigurationsdatei hat Berechtigung 0600
- Atomares Schreiben der Konfiguration
- OSC-52-Zwischenablage: Remote-Programme duerfen nur **schreiben** (max. 1 MiB); Leseanfragen werden nie beantwortet
- Auto-Update: lädt nur aus den GitHub-Releases dieses Repositorys, SHA-256-Prüfung ist Pflicht (vor der Installation erneut), dazu Größen- und Formatprüfung
- Die DNS-Fallback-Server werden nur genutzt, wenn der DNS des Netzwerks versagt; Host-Keys werden weiterhin gegen known_hosts geprüft
- Einfügen per Shift+Einfg nutzt Bracketed Paste, sobald die Remote-Shell es aktiviert (`ESC[?2004h`): Der Text wird in `ESC[200~ … ESC[201~` eingeschlossen, eingebettete Marker werden entfernt – mehrzeiliger Inhalt wird nicht Zeile für Zeile sofort ausgeführt
