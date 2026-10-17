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
- [x] Einfuegen aus Zwischenablage in TUI-Formularfeldern (Strg+V / Shift+Einf), z.B. fuer Passwoerter (Build 42)
- [x] Fenster erhaelt beim Start den Fokus (Windows: SetForegroundWindow nach AllocConsole) (Build 42)
- [x] Alt+F4 beendet das Programm - im Menue und in der SSH-Session (wie PuTTY) (Build 42)
- [x] Scroll-Back springt bei Tastatureingabe zur Eingabezeile zurueck (Windows-Konsole, wie PuTTY) (Build 42)
- [x] Schriftgroesse per Strg+Hoch/Strg+Runter (Windows: SetCurrentConsoleFontEx, 8-72px, persistiert in font_size; Strg+Mausrad-Zoom wird beim Beenden uebernommen; Linux: Hinweis auf Terminal-Einstellungen) (Build 42/43)
- [x] OSC-52-Zwischenablage: Remote-Programme koennen in die lokale Zwischenablage kopieren (Build 44)
- [x] Build-Nummer im Hauptmenü (eingebettet, „ssh-easy v0.<Build> (Build <Build>)“) (Build 46)
- [x] Gruppen per Doppelpunkt im Namen („Abc:123“ → Gruppe Abc), auf-/zuklappbar (Build 48)
- [x] Scrollbare Verbindungsliste ab mehr als 15 Zeilen, Bild↑/Bild↓/Pos1/Ende (Build 48)
- [x] Auto-Update über GitHub-Releases (Taste `u`, Standard aus; Download beim Start, Installation beim Beenden, SHA-256-Pflicht) (Build 48)
- [x] DNS-Fallback 1.1.1.1 / 1.0.0.1 / 8.8.8.8 / 8.8.4.4 rotierend (Build 48)
- [x] 5-s-Timeout beim ersten Verbinden, Abbruch beim ersten Netzwerkfehler (Build 48)
- [x] Fenstergröße merken (Windows, nicht bei maximiert/Vollbild) (Build 48)
- [x] Tunnel-Kurzanzeige „+N weitere“ ab dem dritten Port, fehlgeschlagene Tunnel rot (Build 48)
- [x] Neue Verbindung nach dem Anlegen ausgewählt (Build 48)
- [x] Host und Benutzer bei der Passwortabfrage (Build 48)
- [x] Alle Texte in allen 41 Sprachen, Vollständigkeitstest + Claude-Code-Stop-Hook (Build 48)
- [x] Statusseite zeigt „Keine Tunnel“ statt leerer Tunnel-Überschrift (Build 50)
- [x] Export der Verbindungsliste: alle oder Auswahl (einzeln oder ganze Gruppe), Taste `E`, ohne SSH-Schlüssel und Schlüsselpfade, Format von connections.json, Rechte 0600, kein Überschreiben (Build 51)
- [x] Signierte Releases: Ed25519-signierte Prüfsummenliste, Auto-Update prüft gegen eingebetteten öffentlichen Schlüssel (Build 51)
- [x] Import von Verbindungen (Taste `I`, auch bei leerer Liste): Datei wählen, Auswahl wie beim Export, vorhandene Namen markiert und nur auf Wunsch ersetzt; Datei als nicht vertrauenswürdig geprüft (1 MiB, Validate, keine Steuerzeichen, keine Schlüsselpfade, neue IDs) (Build 52)
- [x] Bracketed Paste für Shift+Einfg in der SSH-Session (Remote-Modus `ESC[?2004h/l` wird erkannt, Inhalt in `ESC[200~ … ESC[201~` eingeschlossen, eingebettete Marker entfernt) (Build 47)

## Offen
- [ ] Optional: Schalter in der Konfiguration, um OSC 52 zu deaktivieren
- [ ] Suche/Filter in der Verbindungsliste
- [ ] Unterschiedliche lokale und remote Ports bei Tunneln (aktuell gleich)
