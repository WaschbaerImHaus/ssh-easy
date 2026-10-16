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

### Auto-Update: Vertrauen in GitHub-Konto und -API (akzeptiertes Risiko)

**Datei:** `src/updater.go`
**Schweregrad:** NIEDRIG (nur wenn vom Nutzer eingeschaltet, Standard: aus)

Prüfsumme und Download stammen beide von GitHub. Wer das GitHub-Konto bzw.
ein Release-Token kontrolliert, könnte eine manipulierte Binary samt passender
Prüfsumme veröffentlichen. Gegenmaßnahmen: TLS, nur Release-Adresse dieses
Repositorys, SHA-256-Pflicht (zweite Prüfung direkt vor der Installation gegen
Austausch auf der Platte), Größen- und Formatprüfung, Standard aus. Unter
Windows zeigt die Benutzerkontensteuerung beim Setup „Unbekannter Herausgeber“
(nicht signiert). Das GitHub-Token liegt im Klartext in lokalen Notizen
(`.claude/CLAUDE.md`, nicht im Repo) – wer es erbeutet, kann Releases
veröffentlichen, die alle Auto-Update-Nutzer installieren (unter Windows mit
Adminrechten). Token mit minimalen Rechten und kurzer Laufzeit verwenden.

Restrisiko Windows-Setup: Der Setup-Ordner gehört dem Benutzer. Schadsoftware
mit Benutzerrechten könnte im Bruchteil einer Sekunde zwischen Ordnerprüfung
und Start eine DLL ablegen; NSIS 3 lädt System-DLLs jedoch nur aus System32.

**Maßnahme:** Optional später: Releases signieren (Authenticode bzw. minisign)
und Signatur vor dem Start prüfen (OPTIMIZE.md).

### DNS-Fallback über öffentliche Server (akzeptiertes Risiko)

**Datei:** `src/net_dial.go`
**Schweregrad:** NIEDRIG

Antwortet der DNS-Server des Netzwerks nicht (Timeout/Serverfehler), gehen
Hostnamen-Anfragen unverschlüsselt an 1.1.1.1/1.0.0.1/8.8.8.8/8.8.4.4
(Datenschutz: die Betreiber sehen den Namen). Meldet der Netz-DNS „Name
unbekannt“ (NXDOMAIN), wird NICHT weitergefragt. Fällt der interne DNS aus,
kann ein interner Name öffentlich anders aufgelöst werden. Bekannte Hosts schützt known_hosts (abweichender Host-Key →
MITM-Warnung). Seit Build 49 wird einem noch unbekannten Host-Key nicht
automatisch vertraut, wenn die Adresse vom Fallback stammt; interne Namen
gehen nie an öffentliche Server. Die Anfrage selbst bleibt unverschlüsselt
(kein DoH/DoT).

**Maßnahme:** Akzeptiert (vom Nutzer gewünschte Funktion). Für interne Server
IP-Adressen oder einen funktionierenden lokalen DNS verwenden.

---

## Empfehlungen

- Immer eine aktuelle `~/.ssh/known_hosts`-Datei pflegen
- Bei erstem Verbindungsaufbau Host-Key manuell verifizieren
- SSH-Agent nur mit vertrauenswuerdigen Schluesselm laden
- Log-Datei regelmaessig rotieren/loeschen (~/.ssh-easy/ssh-easy.log)
