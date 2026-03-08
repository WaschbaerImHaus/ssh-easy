# Features - ssh-easy

## Implementiert

- [x] TUI mit Bubbletea (Verbindungsliste, Formulare, Status)
- [x] SSH-Verbindung per Go-Library (golang.org/x/crypto/ssh)
- [x] Passwort-Authentifizierung
- [x] SSH-Key-Authentifizierung (mit/ohne Passphrase)
- [x] SSH-Agent-Unterstuetzung
- [x] Local Port Forwarding (localhost:port -> remote:port)
- [x] Verbindungen als JSON speichern/laden
- [x] Verbindung erstellen, bearbeiten, loeschen
- [x] Farbige Statusanzeige (verbunden/getrennt)
- [x] Host-Key-Verifizierung (known_hosts, kein InsecureIgnoreHostKey)
- [x] Atomares Speichern (Temp-Datei + Rename)
- [x] Cross-Compilation (Linux/Windows x86/ARM)
- [x] Tunnel-Status pro Port anzeigen
- [x] SSH-Key-Generierung (Ed25519) mit optionaler Passphrase
- [x] Public Key Anzeige nach Generierung
- [x] Auto-Reconnect bei Verbindungsabbruch (max. 5 Versuche, 3s Intervall)
- [x] SSH-Keepalive (alle 30 Sekunden)
- [x] SSHManager-Struct (statt globale Funktionen)
- [x] ConfigCache mit Lazy-Loading
- [x] Datei-basiertes Logging (~/.ssh-easy/ssh-easy.log)
- [x] TUI aufgeteilt in Einzeldateien (tui_list, tui_form, tui_status, tui_keygen)

## Offen

- [ ] Verbindungen exportieren/importieren
- [ ] Suche/Filter in der Verbindungsliste
- [ ] Gruppen/Ordner fuer Verbindungen
- [ ] Unterschiedliche lokale und remote Ports bei Tunneln (aktuell gleich)
