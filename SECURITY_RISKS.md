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

### OSC 52: Remote-Server kann die lokale Zwischenablage beschreiben (akzeptiertes Risiko)

**Datei:** `src/osc52.go` – `osc52Writer`
**Schweregrad:** NIEDRIG

Seit Build 44 schreibt ssh-easy OSC-52-Sequenzen aus der SSH-Ausgabe in die
lokale Zwischenablage – das ist der Zweck der Funktion (Kopieren aus Claude Code,
vim, tmux). Ein bösartiger oder kompromittierter Server könnte damit unbemerkt
Text in die Zwischenablage legen (z.B. einen Befehl, den der Nutzer später
lokal einfügt). Gleiches Verhalten wie Windows Terminal, kitty, WezTerm u.a.

Gegenmaßnahmen:
- Steuerzeichen (außer Tab/CR/LF) werden entfernt – kein Paste-Jacking über
  `ESC[201~` (Build 45)
- Nur Schreiben – Leseanfragen (`?`) werden nie beantwortet, der Server kann
  die Zwischenablage nicht auslesen
- Größenlimit 1 MiB, übergroße Sequenzen werden verworfen
- Nur in einer interaktiven Session, die der Nutzer selbst geöffnet hat, zu
  einem per known_hosts verifizierten Host

**Maßnahme:** Akzeptiert. Optional später: Schalter in der Konfiguration zum
Deaktivieren von OSC 52.

---

## Empfehlungen

- Immer eine aktuelle `~/.ssh/known_hosts`-Datei pflegen
- Bei erstem Verbindungsaufbau Host-Key manuell verifizieren
- SSH-Agent nur mit vertrauenswuerdigen Schluesselm laden
- Log-Datei regelmaessig rotieren/loeschen (~/.ssh-easy/ssh-easy.log)
