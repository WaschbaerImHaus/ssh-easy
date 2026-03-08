# Optimized Worker - ssh-easy

## Projektwissen

ssh-easy ist ein SSH-Verbindungsmanager mit TUI. Das Programm nutzt die Go SSH-Library
direkt (kein system-ssh), was plattformuebergreifende Kompatibilitaet sicherstellt.

Seit Build 3 nutzt es ein SSHManager-Pattern mit automatischem Reconnect, SSH-Agent-
Unterstuetzung und Keepalive. Die TUI ist modular in Views aufgeteilt.

## Architekturentscheidungen

- **Bubbletea statt tview**: Elm-Architektur ermoeglicht bessere Testbarkeit (reine Update-Funktionen)
- **Atomares Schreiben**: Config wird in Temp-Datei geschrieben und dann renamed - verhindert Korruption
- **Kein Passwort-Speicher**: Bewusste Sicherheitsentscheidung - Passwoerter nur zur Laufzeit
- **SSHManager-Struct**: Zentrale Verbindungsverwaltung mit Mutex statt globaler Funktionen
- **ConfigCache**: Lazy-Loading anhand Datei-Aenderungszeit statt bei jedem Zugriff neu laden
- **Kein InsecureIgnoreHostKey**: known_hosts ist Pflicht, unbekannte Hosts werden hinzugefuegt
- **Auto-Reconnect**: Max 5 Versuche mit 3s Delay, nur wenn Connection nicht manuell getrennt

## Recherche-Themen

- SSH-Multiplexing (ControlMaster-Aequivalent in Go)
- SSH-Agent-Forwarding ueber golang.org/x/crypto/ssh/agent (teilweise umgesetzt)
- ProxyJump / Jump-Host-Unterstuetzung
- SOCKS5-Proxy ueber SSH
- Terminal-Sharing (tmux-Integration)

## Brainstorming

- "Reverse Tunnel" - Koennte nuetzlich sein fuer NAT-Traversal
- "SFTP" - Dateiuebertragung ueber bestehende SSH-Verbindung
- "Config-Import" - OpenSSH ~/.ssh/config importieren
- "Clipboard" - Verbindungsdetails in Zwischenablage kopieren
- "Notifications" - Desktop-Benachrichtigung bei Verbindungsabbruch
- "Dynamic Port Forwarding" - SOCKS5-Proxy statt fester Port-Liste
- "Remote Command" - Einzelbefehle auf Remote ausfuehren ohne Shell
