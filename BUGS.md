# Bugs - ssh-easy

## Offen

Keine bekannten Bugs.

## Behoben

- [Build 39] Shift+Einf fügte im SSH-Terminal auf Windows nichts aus dem Clipboard ein. Ursache: conhost/Windows Terminal liefert Shift+Insert als VT-Sequenz ESC[2;2~ an die Anwendung, die ssh-easy roh an den Remote-Shell weiterreichte. Fix: eigener stdin-Forwarder erkennt die Sequenz und schreibt stattdessen den Clipboard-Inhalt in die SSH-Session.
