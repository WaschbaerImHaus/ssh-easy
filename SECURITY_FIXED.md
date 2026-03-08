# Behobene Sicherheitsprobleme - ssh-easy

- Tunnel binden nur auf 127.0.0.1 (nicht 0.0.0.0) - Verhindert ungewollten Netzwerkzugriff
- Konfigurationsdatei mit 0600-Berechtigung - Nur Besitzer kann lesen/schreiben
- Passwoerter werden nicht gespeichert - Nur zur Laufzeit im Speicher
- Atomares Schreiben - Verhindert korrupte Konfiguration bei Absturz
- **InsecureIgnoreHostKey entfernt (Build 3)** - known_hosts wird erstellt falls noetig, unbekannte Hosts werden hinzugefuegt, geaenderte Keys werden mit MITM-Warnung abgelehnt
