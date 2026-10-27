# ssh-easy

Ein einfacher SSH-Verbindungsmanager mit Terminal-UI (TUI) zum Verwalten und Aufbauen von SSH-Verbindungen mit Local-Port-Forwarding-Tunneln.

## Features

- SSH-Verbindungen speichern und verwalten (Name, Host, Port, User)
- Automatische Authentifizierung: SSH-Agent, alle Keys in `~/.ssh/`, Passwort-Fallback
- Local Port Forwarding (localhost:port -> remote:port)
- SSH-Key-Generierung (Ed25519) direkt aus der TUI
- Automatischer Reconnect bei Verbindungsabbruch (max. 5 Versuche)
- SSH-Keepalive (alle 30 Sekunden)
- Farbige TUI mit Statusanzeige
- Kopieren aus Remote-Programmen: OSC-52-Sequenzen (z.B. Claude Code, vim, tmux) landen in der lokalen Zwischenablage – auch in der Windows-Konsole, die OSC 52 selbst nicht beherrscht
- Gruppen: Ein Doppelpunkt im Namen gruppiert Verbindungen (`Büro:Server1`, `Büro:NAS` → Gruppe *Büro*), Gruppen lassen sich auf- und zuklappen
- Scrollbare Liste: Mehr als 15 Einträge werden in einem Bildlauf-Fenster gezeigt (`↑ +N` / `↓ +N` nennen die verborgenen Zeilen)
- Schnelle Meldung bei nicht erreichbarem Ziel: 5 Sekunden Verbindungs-Timeout, kein erneutes Warten pro SSH-Schlüssel
- DNS-Fallback: Versagt der DNS-Server des Netzwerks, wird über 1.1.1.1, 1.0.0.1, 8.8.8.8 und 8.8.4.4 aufgelöst (rotierend)
- Optionales Auto-Update über GitHub-Releases (Standard: aus, Taste `u`); jedes Release ist signiert (Ed25519)
- Export der Verbindungsliste – alle Verbindungen oder eine Auswahl, SSH-Schlüssel werden nie exportiert (`E`)
- Import von Verbindungen aus einer Exportdatei – Einträge auswählbar, vorhandene Namen werden erkannt (`I`)
- Merkt sich die Fenstergröße (Windows-Konsole, außer maximiert/Vollbild; Windows Terminal merkt sie selbst)
- 41 Oberflächensprachen
- Logging nach ~/.ssh-easy/ssh-easy.log
- Cross-Platform: Linux (x86/ARM), Windows (x86/ARM)

## Installation

Die kompilierten Dateien befinden sich im `build/`-Verzeichnis:

| Datei | Plattform |
|-------|-----------|
| `ssh-easy` | Linux x86_64 |
| `ssh-easy-linux-arm64` | Linux ARM64 |
| `ssh-easy-windows-amd64.exe` | Windows x86_64 |
| `ssh-easy-windows-arm64.exe` | Windows ARM64 |

## Benutzung

Programm starten:
```bash
./build/ssh-easy
```

### Tastenbelegung

| Taste | Aktion |
|-------|--------|
| `m` | Menü (Tastenhilfe unten) ein-/ausblenden; alle Tasten wirken auch bei ausgeblendetem Menü |
| `n` | Neue Verbindung anlegen |
| `c` | Gewählte Verbindung als neue kopieren: Name (mit angehängtem `#2`, `#3` …), Host, Port, Benutzer und Tunnel werden übernommen, SSH-Schlüssel nicht |
| `e` | Verbindung bearbeiten |
| `d` | Verbindung loeschen |
| `Enter` | Verbindung herstellen / Status anzeigen |
| `x` | Verbindung trennen |
| `j/k` oder Pfeiltasten | Navigation |
| `→` / `Enter` auf einer Gruppe | Gruppe aufklappen |
| `←` | Gruppe zuklappen (auch von einem Gruppeneintrag aus) |
| `Bild↑` / `Bild↓` / `Pos1` / `Ende` | In langen Listen blättern |
| `u` | Auto-Update ein-/ausschalten (letzte Menüzeile) |
| `E` | Verbindungen exportieren (alle oder Auswahl) |
| `I` | Verbindungen aus einer Exportdatei importieren |
| `Enter` auf *Datei-Manager (eine Verbindung)* | Ein-Fenster-Datei-Manager zum Ansehen und Bearbeiten öffnen (vorletzter Listeneintrag) |
| `Enter` auf *Datei-Manager (zwei Verbindungen)* | Zwei-Fenster-Datei-Manager öffnen (letzter Listeneintrag) |
| `Tab` | Naechstes Formularfeld |
| `Strg+V` / `Shift+Einf` | Zwischenablage in Formularfeld und Passwortabfrage einfügen (auch unter Windows) |
| `Strg+Hoch` / `Strg+Runter` | Schriftgröße ändern (Windows; Strg+Mausrad geht auch) |
| `r` | Fenster- und Schriftgröße auf den Systemstandard zurücksetzen (Windows-Konsole) |
| `Esc` | Zurueck / Abbrechen |
| `q` / `Strg+C` / `Alt+F4` | Beenden (Alt+F4 auch in laufender SSH-Session) |

### Verbindung anlegen

1. `n` druecken
2. Felder ausfuellen:
   - **Name**: Anzeigename (z.B. "Webserver")
   - **Host**: IP oder Hostname
   - **Port**: SSH-Port (Standard: 22)
   - **Benutzer**: SSH-Benutzername
   - **Tunnel-Ports**: Kommagetrennte Portliste (z.B. `3306,8080,5432`)
3. `Enter` zum Speichern

### Authentifizierung

Die Authentifizierung laeuft vollautomatisch – keine manuelle Auswahl noetig:

1. **SSH-Agent** – wird zuerst geprueft, falls ein laufender Agent vorhanden ist
2. **SSH-Keys** – alle Keys in `~/.ssh/` werden automatisch ausprobiert
3. **Passwort** – wird als Fallback abgefragt, falls alles andere scheitert (nie gespeichert)

Nach einem erfolgreichen Passwort-Login kann ssh-easy automatisch einen Ed25519-Key generieren und deployen, damit zukuenftige Logins passwortlos funktionieren.

### Tunnel

Tunnel-Ports werden als kommagetrennte Liste angegeben. Jeder Port wird als Local-Port-Forward eingerichtet:

```
localhost:3306 -> remote:3306
localhost:8080 -> remote:8080
```

### Gruppen

Ein Doppelpunkt im Verbindungsnamen trennt Gruppe und Eintrag. `Büro:Server1` und `Büro:NAS` erscheinen als Gruppe **Büro** mit den Einträgen *Server1* und *NAS*. Getrennt wird nur am ersten Doppelpunkt (`Büro:Rack:1` → Gruppe *Büro*, Eintrag *Rack:1*). Vor und nach dem Doppelpunkt muss Text stehen. Ein grüner Punkt an der Gruppe bedeutet: mindestens ein Eintrag ist verbunden. Eine neu angelegte Verbindung ist sofort ausgewählt (ihre Gruppe wird aufgeklappt).

### Tunnel-Anzeige

Die Liste zeigt die ersten zwei Tunnel-Ports, weitere nur noch als Anzahl: `[Tunnel: 3306,8080 +3 weitere]`. Nach dem Verbinden erscheinen Ports, deren Tunnel nicht aufgebaut werden konnte (z.B. Port belegt), rot – in der Liste und in der Statusansicht. Details stehen in der Log-Datei.

### Verbindungen exportieren

`E` (Shift+E) in der Liste drücken. Zunächst sind alle Verbindungen ausgewählt, Gruppen sind aufgeklappt:

- `Leertaste` schaltet den Eintrag unter dem Cursor um – auf einer Gruppe alle Mitglieder (`[~]` = teilweise gewählt)
- `a` wählt alle bzw. keine
- `Tab` wechselt ins Dateinamenfeld (Vorschlag: `ssh-easy-export-<Datum>-<Uhrzeit>.json` im Home-Ordner; relative Namen und `~` beziehen sich auf den Home-Ordner)
- `Enter` exportiert, `Esc` bricht ab

Die Datei hat das Format von `connections.json` und kann damit auf einem anderen Rechner als Konfiguration dienen. Sie wird mit den Rechten `0600` geschrieben und überschreibt nie eine vorhandene Datei. **SSH-Schlüssel sind nicht Teil des Exports** – weder die Schlüsseldateien noch ihr Pfad; Verbindungen mit Schlüssel werden als Passwort-Verbindung exportiert (die automatische Anmeldung probiert SSH-Agent und alle Schlüssel in `~/.ssh/` trotzdem zuerst).

### Verbindungen importieren

`I` (Shift+I) in der Liste drücken – das geht auch bei leerer Liste, z. B. auf einem neuen Rechner.

1. Dateinamen eingeben (Vorschlag: die neueste `ssh-easy-export-*.json` im Home-Ordner) und `Enter` drücken.
2. Die Verbindungen der Datei erscheinen mit Häkchen wie beim Export (`Leertaste`, `a`, Gruppen schalten alle Mitglieder). Namen, die es schon gibt, sind mit **(vorhanden)** markiert und anfangs *nicht* gewählt. Wer sie wählt, ersetzt die vorhandene Verbindung (Host, Port, Benutzer und Tunnel kommen aus der Datei; die lokale ID und eine lokale SSH-Schlüssel-Einstellung bleiben). `Enter` importiert, `Esc` geht zurück.

Die Datei gilt als nicht vertrauenswürdig: höchstens 1 MiB, jeder Eintrag wird geprüft (Name, Host, Port, Benutzer, Tunnel), Einträge mit Steuerzeichen (z. B. Terminal-Escape-Sequenzen) und doppelte Namen innerhalb der Datei werden übersprungen, Schlüsselpfade werden nie übernommen. Importiert werden nur Verbindungen – Einstellungen wie Sprache oder Auto-Update bleiben unverändert.

### Datei-Manager (eine Verbindung)

Der vorletzte Eintrag der Verbindungsliste fragt nur nach **einer** Verbindung (auch
*Lokaler PC*, er funktioniert also auch ohne gespeicherte Verbindung), baut nur diese auf
und zeigt ein Fenster über die ganze Breite. Alles aus dem folgenden Abschnitt gilt –
Ansehen (`F3`), Bearbeiten (`F4`), Verzeichnis anlegen (`F7`), Löschen (`F8`), lokal öffnen,
Menü (`F9`: *Fenster*, *Datei*, *Optionen*) –, nur Kopieren und Verschieben (`F5`/`F6`)
entfallen, weil es kein zweites Fenster als Ziel gibt. `Tab` hat hier keine Wirkung.

### Datei-Manager (zwei Verbindungen)

Der letzte Eintrag der Verbindungsliste öffnet einen
reduzierten Datei-Manager im Stil des Midnight Commander, um mit zwei SSH-Servern
nebeneinander zu arbeiten:

1. Verbindung für das **linke** Fenster wählen, dann die für das **rechte** Fenster.
   *Lokaler PC* (erster Eintrag) zeigt das Dateisystem deines eigenen Rechners, so kopierst
   du zwischen PC und Server; unter Windows zeigt `..` oberhalb von `C:\` die Laufwerksliste
   (`q` führt jederzeit zurück zur Liste). Die Auswahl beginnt auf dem Platzhalter
   *(nicht ausgewählt)*, der sich nicht bestätigen lässt – beide Verbindungen müssen
   bewusst gewählt werden. Die linke Verbindung steht rechts nicht mehr zur Wahl.
2. Beide Verbindungen werden als eigene SSH-Verbindungen **ohne Tunnel** aufgebaut
   (zuerst SSH-Agent/Schlüssel, bei Bedarf Passwortabfrage; `Esc` bricht sie ab).
   Auf den Servern muss nichts installiert sein – die Dateilisten kommen per SFTP.
3. Beide Fenster starten im Home-Verzeichnis des Users.

| Taste | Aktion |
|-------|--------|
| `↑` / `↓` / `Bild↑` / `Bild↓` / `Pos1` / `Ende` | Cursor bewegen |
| `Enter` | In das Verzeichnis bzw. `..` wechseln |
| `Tab` | Zwischen linkem und rechtem Fenster wechseln |
| `Einfg` | Eintrag unter dem Cursor markieren/entmarkieren und eine Zeile weiter |
| `*` | Markierung aller Einträge (Dateien und Verzeichnisse) umkehren |
| `F3` | Datei unter dem Cursor ansehen – Tasten wie im mc-Betrachter: `F1` Hilfe, `F2` Umbruch, `F4` Hex/Text, `F5` Gehe zu, `F6` Suche mit regulärem Ausdruck, `F7` Suchen (`n` weiter), `F9` mit dem Standardprogramm öffnen, `F3`/`F10`/`q`/`Esc` schließen |
| `F4` | Datei unter dem Cursor bearbeiten – Tasten wie im mcedit: `F2` speichern, `F3` Block markieren, `F4` ersetzen, `F5`/`F6`/`F8` Block kopieren/verschieben/löschen, `F7` suchen, `F10`/`Esc` beenden, `Strg+Y` Zeile löschen; Binärdateien als bearbeitbare Hex-Ansicht |
| `F5` | Markierte Einträge (bzw. den unter dem Cursor) ins andere Fenster kopieren |
| `F6` | Markierte Einträge (bzw. den unter dem Cursor) ins andere Fenster verschieben |
| `F7` | Verzeichnis im aktiven Fenster anlegen |
| `F9` | Menü öffnen (Links, Datei, Optionen, Rechts) – `←`/`→`/`↑`/`↓`, `Enter`, `Esc` |
| `Strg+R` | Aktives Fenster neu einlesen |
| `F8` | Markierte Einträge (bzw. den unter dem Cursor) löschen, Verzeichnisse samt Inhalt – mit `j`/`y` bestätigen |
| `F10` / `q` | Beide Verbindungen schließen, zurück zur Liste |

Kennzeichen vor den Namen: `/` Verzeichnis, `~` Symlink auf ein Verzeichnis, `@` Symlink
auf eine Datei, `!` defekter Symlink. Symlinks auf Verzeichnisse werden nicht verfolgt
(es erscheint eine Meldung). Fehlen die Rechte für ein Verzeichnis, bleibt man im
bisherigen und bekommt eine Meldung.
Markierte Einträge werden hervorgehoben; die Infozeile des Fensters zeigt ihre Anzahl und
die Gesamtgröße der markierten Dateien. Markierungen gehören zu ihrem Fenster und
verfallen beim Verzeichniswechsel.

**Kopieren (`F5`)** kopiert ins aktuelle Verzeichnis des anderen Fensters – Verzeichnisse
rekursiv, Symlinks als Symlink, mit Rechten und Änderungszeit. Nach einer Bestätigung
erscheint ein Fortschrittsfenster (`Esc` bricht ab). Existiert ein Ziel schon, wird mit
Größe und Datum beider Dateien nachgefragt: `j` überschreiben, `n` überspringen,
`a` alle, `k` keine, `u` alle, wenn neuer, `g` alle, wenn größer, `Esc` abbrechen. Die
Daten laufen über deinen Rechner, die Server müssen sich nicht gegenseitig erreichen.
Jede Datei wird erst unter einem Temp-Namen nur für dich lesbar geschrieben und am Ende
umbenannt: Private Dateien sind auf dem Ziel nie für andere lesbar, eine vorhandene
Datei wird erst ersetzt, wenn die neue vollständig ist. Bleibt nach einem Abbruch (z. B.
Verbindungsabriss) eine solche Temp-Datei `.ssh-easy-copy-….tmp` liegen, räumt ssh-easy sie
beim nächsten Kopieren in denselben Ordner weg, sobald sie mindestens 24 Stunden unverändert
ist (auf Servern nur mit Rechten 0600); jede Löschung steht im Log.

**Verschieben (`F6`)** läuft wie das Kopieren; jede Datei wird in der Quelle erst gelöscht,
wenn ihre Kopie vollständig angekommen ist. Was fehlschlägt oder übersprungen wird, bleibt
in der Quelle und bleibt markiert. Ein Ziel, das die Quelle selbst ist (z. B. zwei
Verbindungseinträge für denselben Server), wird abgelehnt.

**Verzeichnis anlegen (`F7`)** fragt nach einem einzelnen Namen (ohne `/` und `\`); `Esc`
bricht ab.

**Löschen (`F8`)** entfernt Verzeichnisse samt Inhalt. Die Rückfrage beantwortest du mit
`j` oder `y` – `Enter` allein löscht nicht. Symlinks werden selbst entfernt, nie ihr Ziel.
`Esc` stoppt ein laufendes Löschen.

**Bearbeiten (`F4`)** funktioniert wie im mcedit. Zeilenenden (auch Windows-CRLF) bleiben
erhalten. Binärdateien öffnen sich als Hex-Ansicht, in der Hex-Ziffern Bytes überschreiben
(`Tab` wechselt in die Zeichenspalte); die Größe ändert sich nie. Gespeichert wird über eine
Temp-Datei, die am Ende umbenannt wird; die Rechte bleiben, und wurde die Datei inzwischen
auf dem Server geändert, wird vorher nachgefragt.

**Lokal öffnen (`F9` im Betrachter)** lädt die Datei in einen privaten Temp-Ordner und
übergibt sie dem Standardprogramm deines Rechners (Bildbetrachter, Browser, PDF-Leser …).
Geöffnet werden nur bekannte Dokument- und Medientypen (Bilder, PDF, Text, HTML,
Audio/Video, Archive); Office-Dokumente jeder Art, Programme, Skripte, Verknüpfungen und
unbekannte Typen werden abgelehnt, weil „Öffnen“ dort „Ausführen“ heißen könnte. Unter Windows wird die Kopie als „aus dem
Internet“ gekennzeichnet (geschützte Ansicht, SmartScreen). Die Kopien werden beim Beenden
von ssh-easy gelöscht.

Der Datei-Manager mit zwei Verbindungen braucht mindestens eine gespeicherte Verbindung
(die andere Seite kann *Lokaler PC* sein); bis dahin ist sein Eintrag ausgegraut. Vom Server auf den PC kopierte Dateien werden
als heruntergeladen gekennzeichnet (Windows-SmartScreen warnt vor dem Ausführen).

**Optionen (`F9` → Optionen):** *Aussehen* schaltet zwischen den Farbmodi klassisch (mc),
ssh-easy und monochrom um. *Endungen für F9* zeigt als sortierte Liste, welche Dateitypen
`F9` im Betrachter lokal öffnet (anfangs die sichere Standardliste). `↑`/`↓`, `Bild↑`/`Bild↓`,
`Pos1`/`Ende` blättern, `Entf` entfernt eine Endung nach Rückfrage (`j`/`y`), `Tab` oder
`Einfg` springen ins Feld *Neue Endung* – erlaubt sind nur `a`–`z` und `0`–`9`, `Enter` fügt
hinzu. Typen außerhalb der sicheren Liste (Programme, Skripte, Office …) werden nur nach einem
deutlichen Sicherheitshinweis übernommen, und beim Öffnen einer solchen Datei fragt ssh-easy
noch einmal nach. Eine leere Liste bedeutet: `F9` öffnet nichts. Alle Änderungen werden sofort
gespeichert.

### Passwortabfrage

Schlägt die automatische Anmeldung fehl, fragt ssh-easy nach dem Passwort und zeigt dabei Host und – in der Zeile darunter – den Benutzernamen, damit klar ist, welches Passwort gemeint ist.

### Auto-Update

Standardmäßig aus. Mit `u` im Hauptmenü einschalten (der Zustand steht in der letzten Menüzeile, die `m` einblendet, und wird gespeichert). Eingeschaltet prüft ssh-easy beim Start das neueste GitHub-Release und lädt die passende Datei im Hintergrund; installiert wird beim Beenden:

- Linux oder Windows mit Schreibrecht im Programmordner: Die Programmdatei wird ersetzt
- Windows-Installation unter *Programme*: Das Setup läuft still mit Administratorrechten (Windows fragt per Benutzerkontensteuerung); die Desktop-Verknüpfung bleibt erhalten

Jedes Release enthält eine mit Ed25519 signierte Prüfsummenliste (`ssh-easy-SHA256SUMS` + `ssh-easy-SHA256SUMS.sig`). ssh-easy prüft die Signatur mit dem im Programm eingebauten öffentlichen Schlüssel und installiert nur eine Datei, deren SHA-256 in der signierten Liste steht – selbst wer Zugriff auf das GitHub-Konto hat, kann so kein manipuliertes Update unterschieben. Dazu kommen Größen- und Formatprüfung – direkt nach dem Download und noch einmal unmittelbar vor der Installation. Unsignierte Releases werden abgelehnt.

### Auto-Reconnect

Bei einem Verbindungsabbruch versucht ssh-easy automatisch, die Verbindung wiederherzustellen (max. 5 Versuche mit 3 Sekunden Abstand).

## Konfiguration

Verbindungen werden gespeichert unter:
- Linux: `~/.ssh-easy/connections.json`
- Windows: `%USERPROFILE%\.ssh-easy\connections.json`

Log-Datei:
- `~/.ssh-easy/ssh-easy.log`

Passwoerter werden **nicht** gespeichert und muessen bei jedem Verbindungsaufbau eingegeben werden.

Weitere Einstellungen in `connections.json`: `language`, `font_size`, `window_cols` / `window_rows` (letzte Fenstergröße, nur Windows) und `auto_update`.

## Sicherheit

- Passwoerter werden nur zur Laufzeit im Speicher gehalten
- Tunnel binden ausschliesslich auf 127.0.0.1
- Host-Keys werden gegen `~/.ssh/known_hosts` geprueft (kein InsecureIgnoreHostKey)
- Unbekannte Hosts werden nach Bestaetigung zur known_hosts hinzugefuegt
- Geaenderte Host-Keys werden mit MITM-Warnung abgelehnt
- Konfigurationsdatei hat Berechtigung 0600
- Atomares Schreiben der Konfiguration
- OSC-52-Zwischenablage: Remote-Programme duerfen nur **schreiben** (max. 1 MiB); Leseanfragen werden nie beantwortet
- Auto-Update: lädt nur aus den GitHub-Releases dieses Repositorys; die SHA-256 muss zur Ed25519-signierten Prüfsummenliste genau dieses Releases passen (öffentlicher Schlüssel eingebaut, privater nur auf dem Build-Rechner), vor der Installation erneut geprüft, dazu Größen- und Formatprüfung
- Export: keine SSH-Schlüssel oder Schlüsselpfade, Dateirechte `0600`, überschreibt nie und folgt keinem Symlink
- Import: Größenlimit, jeder Eintrag geprüft, keine Steuer-/Formatzeichen, Schlüsselpfade aus der Datei werden ignoriert, neue IDs
- Die DNS-Fallback-Server werden nur genutzt, wenn der DNS des Netzwerks nicht antwortet, und nie für lokale Namen (`.local`, `.lan`, Namen ohne Punkt …); einem unbekannten Host-Key, der über den Fallback-DNS erreicht wurde, wird nicht automatisch vertraut
- Einfügen per Shift+Einfg nutzt Bracketed Paste, sobald die Remote-Shell es aktiviert (`ESC[?2004h`): Der Text wird in `ESC[200~ … ESC[201~` eingeschlossen, eingebettete Marker werden entfernt – mehrzeiliger Inhalt wird nicht Zeile für Zeile sofort ausgeführt
