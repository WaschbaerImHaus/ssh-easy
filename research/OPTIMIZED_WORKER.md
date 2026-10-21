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

## Stand 2026-10-16 (Build 48)

### Gelernt
- Bubbletea v1 unter Windows: VK_INSERT ist nicht gemappt – Einfg, Shift+Einfg und Strg+Plus kommen alle als Rune 0x00. Modifier nur über GetAsyncKeyState ermittelbar.
- tea.Exec-Kindprozesse mit eigener stdin-Goroutine MÜSSEN ihre Eingabe vor der Rückkehr stoppen (cancelreader / Peek-Polling), sonst frisst die hängende Goroutine die nächste Taste. Puffer leeren (Build 38) reicht nicht.
- lipgloss: Ein separat gerendertes Teilstück innerhalb einer Render()-Zeile beendet mit ESC[0m den äußeren Stil. Zeilen mit mehreren Farben immer segmentweise mit gemeinsamem Grundstil rendern.
- ConnectAuto probiert jeden Schlüssel mit eigener Verbindung – bei Netzwerkfehlern sofort abbrechen; ohne Schlüssel vorher Erreichbarkeit prüfen.
- GitHub-Releases-API liefert pro Asset `digest: sha256:…` – für Update-Prüfung nutzbar.

### Arbeitsweise
- Neue UI-Texte nur über Schlüssel in i18n_extra.go und sofort in alle 41 Sprachen; Stop-Hook prüft das.
- Für jedes Feature: Test zuerst, danach Gegenprobe (Fix zurücknehmen → Test muss rot werden).
- Nach Implementierung echten TUI-Lauf in tmux mit temporärem HOME (capture-pane) – findet UX-Lücken, die Unit-Tests nicht sehen.

## Stand 2026-10-17 (Build 51)

### Gelernt
- Eine Prüfsumme aus derselben Quelle wie die Datei (GitHub-API) schützt nur gegen Übertragungsfehler, nicht gegen ein kompromittiertes Konto. Vertrauensanker muss außerhalb von GitHub liegen: eingebetteter Ed25519-Schlüssel, privater Schlüssel nur auf dem Build-Rechner.
- Signierte Listen immer an das Release binden (Kopfzeile mit Tag) und mit Kontext-Präfix signieren – sonst lassen sich alte, echte Listen wiederverwenden.
- Scanner-Funde zu „Zero-Width Characters“ in Übersetzungen sind bei indischen Schriften/Persisch fast immer ZWNJ (U+200C) und legitim. Statt sie zu entfernen (bricht die Schreibweise): Test, der Bidi/ZWSP/BOM verbietet und ZWNJ/ZWJ auf Übersetzungstexte begrenzt.
- `git checkout -- <datei>` beim Zurücknehmen einer Mutationsprobe verwirft ALLE uncommitteten Änderungen der Datei. Für Mutationsproben vorher eine Sicherung im Scratchpad anlegen und daraus zurückkopieren.
- Export-Dateien: O_EXCL + 0600 (kein Überschreiben, kein Symlink-Folgen), relative Pfade gegen den Home-Ordner auflösen (unter Windows ist das Arbeitsverzeichnis oft der Programme-Ordner).

### Arbeitsweise
- Bei Scanner-Berichten zuerst die Zeichen/Code-Stellen selbst prüfen, dann über triage_cli.py pro Befund (nicht per globaler Regel) bewerten.

## Stand 2026-10-17 (Build 52)

### Gelernt
- `\uXXXX` in Tool-Parametern (Write/Edit) wird beim JSON-Dekodieren zum echten Zeichen. Für unsichtbare Zeichen in Go-Quelltext danach per Skript in Go-Escapes umwandeln – der Hygiene-Test (`tests/source_hygiene_test.go`) fängt es zuverlässig.
- Import-Dateien wie Netzwerkeingaben behandeln: Größenlimit, strenge Prüfung je Eintrag, Steuerzeichen ablehnen (TUI-Ausgabe!), Pfadfelder (key_path) nie übernehmen, IDs neu vergeben.
- Gemeinsame UI-Bausteine (connChecklist) als eigener Typ mit Wertsemantik + Map halten den Bubbletea-Modellcode klein und sind direkt testbar.

## Stand 2026-10-21 (Build 53)

- **Datei-Manager (MC-Modus)** ist ein schrittweise wachsendes Teilprojekt. Nutzer will „Funktion für Funktion“: erst Rohfassung (Navigation), dann Kopieren/Verschieben/MkDir/Löschen/Markieren/Ansehen/Bearbeiten, Optionen-Menü mit drei Farbmodi (classic, ssh-easy, mono). Motivation: Schlüssel/Passwörter nicht doppelt in WinSCP pflegen; unter Linux kein WinSCP; der echte mc läuft auf eingeschränkten Hoster-Servern schlecht → alles muss per SFTP gehen, nichts auf dem Server voraussetzen.
- **Architektur**: Fachlogik ohne Bubbletea in `filemanager_panel.go` hinter `remoteFS`-Interface (Fake im Test). Alles Netzwerk asynchron als tea.Cmd mit Sitzungsnummer – veraltete Ergebnisse werden verworfen und Verbindungen geschlossen. Entfernte Pfade nur mit `path` (POSIX), nie `filepath` (Windows!).
- **Gelernt**: Eine neue Listenzeile bricht Tests, die „letzte Zeile = Verbindung“ annehmen – und deckt echte Folgefehler auf (Cursor nach Löschen). Zusatzzeilen immer über `connIndex < 0` abfragen, nicht über `kind == rowGroup`.
- **Für die nächsten Schritte**: Kopieren zwischen zwei Servern läuft lokal durch (SFTP-Read → SFTP-Write, gestreamt), Fortschrittsanzeige + Abbruch nötig; Bestätigungsdialoge wie im mc (Fenstermitte). Einfg ist unter Windows Rune 0x00 (siehe isNulKey) – fürs Markieren zusätzlich Leertaste anbieten. F-Tasten kommen unter Windows an; unter Linux fangen manche Terminals F10 ab (q funktioniert immer).
