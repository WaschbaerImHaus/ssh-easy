# Behobene Sicherheitsprobleme - ssh-easy

- **time.Sleep ohne Abbruchmöglichkeit in Reconnect-Goroutine (Build 18, 2026-03-15)** – Ersetzt durch `select { case <-time.After(ReconnectDelay): case <-m.done: return }`. SSHManager hat jetzt eine `Shutdown()`-Methode und ein `done`-Channel für sauberes Programmende ohne Goroutine-Leak.
- Tunnel binden nur auf 127.0.0.1 (nicht 0.0.0.0) - Verhindert ungewollten Netzwerkzugriff
- Konfigurationsdatei mit 0600-Berechtigung - Nur Besitzer kann lesen/schreiben
- Passwoerter werden nicht gespeichert - Nur zur Laufzeit im Speicher
- Atomares Schreiben - Verhindert korrupte Konfiguration bei Absturz
- **InsecureIgnoreHostKey entfernt (Build 3)** - known_hosts wird erstellt falls noetig, unbekannte Hosts werden hinzugefuegt, geaenderte Keys werden mit MITM-Warnung abgelehnt
