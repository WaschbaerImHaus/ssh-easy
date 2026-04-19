# Bugs - ssh-easy

## Offen

Keine bekannten Bugs.

## Behoben

- [Build 40] ssh-easy startete unter Linux (Mint/Nemo) per Doppelklick nicht. Ursache: ssh-easy ist eine TUI, kein echtes GUI – ohne TTY kann Bubbletea die Oberfläche nicht rendern. Fix: `ensureTerminal()` prüft beim Start, ob ein TTY fehlt und eine GUI-Session aktiv ist, und re-launcht sich in einem verfügbaren Terminal-Emulator. Zusätzlich `.desktop`-Datei + PNG-Icon für saubere Dateimanager-/Menü-Integration.
- [Build 39] Shift+Einf fügte im SSH-Terminal auf Windows nichts aus dem Clipboard ein. Ursache: conhost/Windows Terminal liefert Shift+Insert als VT-Sequenz ESC[2;2~ an die Anwendung, die ssh-easy roh an den Remote-Shell weiterreichte. Fix: eigener stdin-Forwarder erkennt die Sequenz und schreibt stattdessen den Clipboard-Inhalt in die SSH-Session.
