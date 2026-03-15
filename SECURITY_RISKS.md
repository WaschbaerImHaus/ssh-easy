# Sicherheitsrisiken - ssh-easy

## Offen

### SHA1 in known_hosts-Verifikation (akzeptiertes Risiko)

**Datei:** `src/ssh.go` – Funktion `matchesHashedKnownHost()`
**Schweregrad:** NIEDRIG (False Positive des Scanners)

OpenSSH hasht bekannte Hostnamen mit HMAC-SHA1 im Format `|1|SALT|HASH` (RFC-Format).
Die Funktion liest dieses Format nur – sie erzeugt keine eigenen SHA1-Signaturen.
SHA1 hier durch SHA256 zu ersetzen würde alle gehashten known_hosts-Einträge
unlesbar machen (Inkompatibilität mit OpenSSH). Das ist ein Protokoll-Requirement,
keine freie Designentscheidung.

**Maßnahme:** Keine Änderung möglich ohne OpenSSH-Kompatibilität zu brechen.

---

### io.Copy ohne Größenlimit in Tunnel-Handler (akzeptiertes Risiko)

**Datei:** `src/ssh.go` – Funktion `handleTunnelConnection()`
**Schweregrad:** NIEDRIG (False Positive des Scanners)

Der bidirektionale TCP-Tunnel kopiert Daten ohne künstliches Datenlimit.
Für einen Port-Forwarding-Tunnel ist das korrekt – ein Datenbank-Dump oder
Dateitransfer über den Tunnel kann mehrere Gigabyte umfassen. Ein `io.LimitReader`
würde die primäre Nutzfunktion des Tools zerstören.

Der SSH-Protokoll-Stack (`golang.org/x/crypto/ssh`) implementiert eigenständig
Flow-Control (SSH-Window-Size). DoS über den Tunnel ist damit nur möglich, wenn
der Benutzer einen bösartigen SSH-Server verbunden hat – was außerhalb des
Bedrohungsmodells dieses Tools liegt.

**Maßnahme:** Keine Änderung sinnvoll.

## Empfehlungen

- Immer eine aktuelle `~/.ssh/known_hosts`-Datei pflegen
- Bei erstem Verbindungsaufbau Host-Key manuell verifizieren
- SSH-Agent nur mit vertrauenswuerdigen Schluesselm laden
- Log-Datei regelmaessig rotieren/loeschen (~/.ssh-easy/ssh-easy.log)
