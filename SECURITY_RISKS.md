# Sicherheitsrisiken - ssh-easy

## Offen

### Datei-Manager: Austausch eines Verzeichnisses während des Kopierens (Restrisiko)

**Dateien:** `src/filemanager_copy.go`
**Schweregrad:** NIEDRIG

Wer im Zielverzeichnis schreiben darf, könnte ein gerade angelegtes bzw. vorhandenes
Unterverzeichnis während des Kopierens umbenennen und durch einen Symlink ersetzen.
Folgende Dateien landen dann im Ziel des Links. SFTP bietet keine `openat()`/`O_NOFOLLOW`-
Operationen für Pfadbestandteile, daher nicht vollständig vermeidbar. Abschwächung: neue
Verzeichnisse sind während des Kopierens 0700, jede Datei wird exklusiv (`O_EXCL`) unter
Temp-Namen angelegt. Nur relevant bei Zielverzeichnissen, in die Dritte schreiben dürfen.

### Datei-Manager: Austausch eines Verzeichnisses während des Löschens (Restrisiko)

**Dateien:** `src/filemanager_delete.go`
**Schweregrad:** NIEDRIG

Zwischen `Lstat` (Eintrag ist ein Verzeichnis) und `ReadDir`/`Remove` seines Inhalts könnte
jemand mit Schreibrecht im betroffenen Verzeichnis es durch einen Symlink auf ein fremdes
Verzeichnis ersetzen; dessen Inhalt würde dann mit den Rechten des Nutzers gelöscht.
`rm -rf` vermeidet das mit `openat()`/`unlinkat()`, SFTP kennt dafür keine Operationen.
Abschwächung: Symlinks selbst werden nie verfolgt, Namen mit `/`, `\` oder Steuerzeichen
werden abgelehnt; seit Build 67 muss nach jedem Einlesen und vor jedem Löschen der kanonische
Pfad (SFTP `realpath`) des Verzeichnisses dem erwarteten entsprechen (ganze Kette), ein
Austausch bricht das Löschen dort ab. Übrig bleibt nur das kurze Fenster zwischen Prüfung und `Remove`. Nur
relevant beim Löschen in Verzeichnissen, in die Dritte schreiben dürfen.

### Datei-Manager: unbegrenzte Verzeichnisliste beim Anzeigen (Restrisiko)

**Dateien:** `src/filemanager_panel.go` (`readPanelDir`), `github.com/pkg/sftp` `ReadDir`
**Schweregrad:** NIEDRIG

Ein bösartiger Server kann beim Öffnen eines Verzeichnisses beliebig viele Einträge
liefern; `ReadDir` sammelt sie vollständig im Speicher. Beim Kopieren gilt eine Grenze
(1 000 000 Einträge / 128 Ebenen), beim Anzeigen nicht. Abhilfe wäre ein eigenes,
begrenztes Lesen über die SFTP-Pakete. Betrifft nur Verbindungen zu bösartigen Servern.

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

### Auto-Update: Signaturschlüssel und Altversionen (akzeptiertes Risiko)

**Datei:** `src/updater.go`, `src/releasesig/`
**Schweregrad:** NIEDRIG (nur wenn vom Nutzer eingeschaltet, Standard: aus)

Seit Build 51 ist jedes Release mit Ed25519 signiert; ein gestohlener
GitHub-Token allein reicht nicht mehr für ein Schad-Update. Verbleibend:

- **Privater Schlüssel auf dem Build-Rechner** (`~/.config/ssh-easy/release-signing.key`,
  0600, nicht im Repo, nicht bei GitHub). Wer diesen LXC übernimmt, hat Schlüssel
  und Token zugleich. Geht der Schlüssel verloren, können installierte Versionen
  keine Updates mehr prüfen – sie müssen einmal von Hand aktualisiert werden.
  **Schlüssel sicher (offline) sichern.** Eine Rotation ohne Neuinstallation
  ist noch nicht vorgesehen (OPTIMIZE.md).
- **Builds bis einschließlich 50** kennen die Signatur noch nicht und prüfen nur
  die SHA-256 der GitHub-API. Für sie gilt das alte Risiko (Konto/Token →
  manipuliertes Update), bis sie einmal auf Build 51+ aktualisiert sind.
- Das GitHub-Token liegt weiterhin im Klartext in lokalen Notizen
  (`.claude/CLAUDE.md`, nicht im Repo). Token mit minimalen Rechten und kurzer
  Laufzeit verwenden.
- Unter Windows zeigt die Benutzerkontensteuerung beim Setup „Unbekannter
  Herausgeber“ (keine Authenticode-Signatur).

Restrisiko Windows-Setup: Der Setup-Ordner gehört dem Benutzer. Schadsoftware
mit Benutzerrechten könnte im Bruchteil einer Sekunde zwischen Ordnerprüfung
und Start eine DLL ablegen; NSIS 3 lädt System-DLLs jedoch nur aus System32.

**Maßnahme:** Signatur umgesetzt (Build 51). Optional später: Schlüsselrotation
und Authenticode (OPTIMIZE.md).

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
