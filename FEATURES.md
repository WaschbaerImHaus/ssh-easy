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
- [x] Clipboard-Paste via Shift+Einf im SSH-Terminal (Windows: fängt VT-Sequenz ESC[2;2~ ab und fügt System-Clipboard ein)
- [x] Auto-Relaunch im Terminal-Emulator bei Start ohne TTY (Linux/macOS Dateimanager-Doppelklick)
- [x] `.desktop`-Datei + PNG-Icon für Dateimanager-Integration unter Linux

## Offen

- [ ] Schriftgroesse im Menue konfigurierbar (Windows: Console-Font via SetCurrentConsoleFontEx; Linux: nicht steuerbar, Terminal-Emulator bestimmt die Schrift)
- [ ] Alt+F4 schliesst das Fenster (Windows-Konsole faengt Alt+F4 nicht ab)
- [ ] Fenster erhaelt beim Start den Fokus (Windows: SetForegroundWindow nach AllocConsole; aktuell muss man erst ins Fenster klicken)
- [ ] Scroll-Back-Verhalten wie PuTTY: bei Tastatureingabe im SSH-Terminal automatisch zur aktuellen Eingabezeile zurueckspringen
- [ ] Einfuegen aus Zwischenablage in TUI-Formularfeldern (Shift+Einf / Strg+V), z.B. fuer Passwoerter (SSH-Terminal kann es seit Build 39, die Bubbletea-Textfelder noch nicht)
- [ ] Verbindungen exportieren/importieren
- [ ] Suche/Filter in der Verbindungsliste
- [ ] Gruppen/Ordner fuer Verbindungen
- [ ] Unterschiedliche lokale und remote Ports bei Tunneln (aktuell gleich)
