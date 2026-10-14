# Behobene Sicherheitsprobleme - ssh-easy

- **Abhängigkeits-Update Build 44 (2026-10-14)**:
  - **KRITISCH: `golang.org/x/crypto` v0.48.0 mit erreichbaren SSH-CVEs** (Scanner-Befund M1 / M-2026-09-29) – CVE-2026-42508 (@revoked in known_hosts nicht durchgesetzt), CVE-2026-39832/39833 (Agent-Constraints), CVE-2026-39831 (FIDO/U2F), CVE-2026-39830/39834 (DoS), CVE-2026-46595, CVE-2026-46597, CVE-2026-39829 sowie CVE-2026-56854/56855/78662. Behoben durch Update auf v0.58.0 (gefordert: ≥ 0.56.0). Zusätzlich `x/sys` 0.49.0, `x/term` 0.47.0, `x/text` 0.43.0 (schließt auch CVE-2026-56851, war nicht erreichbar).
  - **Go-Standardbibliothek** – GO-2026-5972 (encoding/asn1, Rekursionstiefe), GO-2026-4971 (net, Panic bei NUL-Byte unter Windows), GO-2026-4602 (os, FileInfo-Escape aus Root) waren laut govulncheck erreichbar. Behoben durch Build mit Go 1.27.2.
  - Nachweis: `govulncheck ./...` → „Your code is affected by 0 vulnerabilities“.
- **Security-Audit Build 41 (2026-04-19)** – Sechs Findings behoben:
  - **HOCH: Passwort im Klartext unbegrenzt im RAM** – `ManagedConnection.password` war `string` (immutable) und wurde nie überschrieben. Umgestellt auf `[]byte` + neue `clearPassword()`-Methode; `Disconnect()` nullt das Slice. Reduziert die Lebensdauer sensibler Daten im Speicher deutlich.
  - **HOCH: KeyboardInteractive-Callback mit zusätzlicher String-Kopie** – `pwCopy := password` entfernt, Closure referenziert das Passwort direkt (eine Kopie weniger im Speicher).
  - **MITTEL: Path-Traversal in `expandTilde`** – `~/../../etc/passwd` konnte aus dem Home-Verzeichnis ausbrechen (filepath.Join normalisiert `..`). Neue Guard: wenn das expandierte Ergebnis nicht mehr unter Home liegt, wird der Original-Pfad zurückgegeben.
  - **MITTEL: Symlink-Folge in SSH-Key-Auto-Discovery** – `~/.ssh/id_fake → /etc/shadow` konnte dazu führen dass fremde Dateien als potenzielle Keys geöffnet wurden. Neue `discoverSSHKeys()` prüft `entry.Type().IsRegular()` – Symlinks werden ignoriert.
  - **MITTEL: Blacklist-basierte Key-Erkennung unvollständig** – Blacklist (`known_hosts`, `config`, …) ließ andere Nicht-Key-Dateien durch. Umgestellt auf Whitelist: `looksLikePEMKey()` liest 11 Bytes und akzeptiert nur Dateien mit `-----BEGIN `-Präfix.
  - **MITTEL: Fragiles String-Parsing von Host-Key-Wechsel** – String-Matching gegen "HOST-KEY GEÄNDERT für " ersetzt durch typisierten `*HostKeyChangedError{Hostname}` + `errors.As()`. Robust gegen Nachrichtenänderungen und transparent für Error-Wrapping (`fmt.Errorf("%w", ...)`).
  - **MITTEL: Windows ignoriert 0600 für Log/Config/Keys** – Neue plattform-spezifische `restrictFilePermissions()`: Unix = `chmod 0600`, Windows = `SetNamedSecurityInfo` mit Owner-only DACL + `PROTECTED_DACL_SECURITY_INFORMATION` (blockiert Vererbung). Angewendet auf Log-Datei, Config, Auth-Cache, neue Private-Keys und known_hosts. Keine CGO-Abhängigkeit – verwendet `golang.org/x/sys/windows`.
- **time.Sleep ohne Abbruchmöglichkeit in Reconnect-Goroutine (Build 18, 2026-03-15)** – Ersetzt durch `select { case <-time.After(ReconnectDelay): case <-m.done: return }`. SSHManager hat jetzt eine `Shutdown()`-Methode und ein `done`-Channel für sauberes Programmende ohne Goroutine-Leak.
- Tunnel binden nur auf 127.0.0.1 (nicht 0.0.0.0) - Verhindert ungewollten Netzwerkzugriff
- Konfigurationsdatei mit 0600-Berechtigung - Nur Besitzer kann lesen/schreiben
- Passwoerter werden nicht gespeichert - Nur zur Laufzeit im Speicher
- Atomares Schreiben - Verhindert korrupte Konfiguration bei Absturz
- **InsecureIgnoreHostKey entfernt (Build 3)** - known_hosts wird erstellt falls noetig, unbekannte Hosts werden hinzugefuegt, geaenderte Keys werden mit MITM-Warnung abgelehnt
