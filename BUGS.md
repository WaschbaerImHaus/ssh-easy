# Bugs - ssh-easy

## Offen

Keine bekannten Bugs.

## Behoben

- [Build 46] Hauptmenü zeigte in installierten Versionen „ssh-easy vdev“ statt des Builds. Ursache: `readBuildNumber()` las `build.txt` zur Laufzeit neben der EXE bzw. unter `../src/` – die Setups liefern diese Datei nicht mit. Fix: `build.txt` wird per `go:embed` in die Binary eingebaut; Titel jetzt „ssh-easy v0.46 (Build 46)“.
- [Build 44] Kopieren aus Programmen auf dem Remote-Server (z.B. Login-Link in Claude Code) kam nicht in der Zwischenablage an. Ursache: Solche Programme kopieren per OSC-52-Escape-Sequenz; ssh-easy reichte die SSH-Ausgabe roh an die Windows-Konsole (conhost) weiter, die OSC 52 nicht kennt und kommentarlos verwirft (Nutzertest: `printf '\033]52;c;aGFsbG8=\007'` → Strg+V in Notepad lieferte nichts). Fix: neuer `osc52Writer` vor stdout/stderr der SSH-Session fängt OSC 52 ab und schreibt den Inhalt selbst in die Zwischenablage. Vom Nutzer unter Windows bestätigt (2026-10-14).
- [Build 43] Strg+Plus/Strg+Minus änderte die Schriftgröße unter Windows nicht. Ursache: Bubbletea v1.x verwirft im Windows-coninput-Pfad (key_windows.go) den Ctrl-Modifier bei Zeichentasten – Strg+Plus liefert VK_OEM_PLUS mit Char 0x00, ein "ctrl++"-KeyMsg kann konstruktionsbedingt nie entstehen (KeyMsg hat nur ein Alt-Flag). Fix: Schriftgröße primär über Strg+Pfeil-hoch/-runter (VK_UP/VK_DOWN mit Ctrl sind explizit als KeyCtrlUp/KeyCtrlDown gemappt und kommen zuverlässig an). Zusätzlich wird die Schriftgröße jetzt beim Beenden persistiert, sodass auch der native conhost-Zoom per Strg+Mausrad den Neustart überlebt.

- [Build 40] ssh-easy startete unter Linux (Mint/Nemo) per Doppelklick nicht. Ursache: ssh-easy ist eine TUI, kein echtes GUI – ohne TTY kann Bubbletea die Oberfläche nicht rendern. Fix: `ensureTerminal()` prüft beim Start, ob ein TTY fehlt und eine GUI-Session aktiv ist, und re-launcht sich in einem verfügbaren Terminal-Emulator. Zusätzlich `.desktop`-Datei + PNG-Icon für saubere Dateimanager-/Menü-Integration.
- [Build 39] Shift+Einf fügte im SSH-Terminal auf Windows nichts aus dem Clipboard ein. Ursache: conhost/Windows Terminal liefert Shift+Insert als VT-Sequenz ESC[2;2~ an die Anwendung, die ssh-easy roh an den Remote-Shell weiterreichte. Fix: eigener stdin-Forwarder erkennt die Sequenz und schreibt stattdessen den Clipboard-Inhalt in die SSH-Session.
