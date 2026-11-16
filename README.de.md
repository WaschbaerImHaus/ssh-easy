# ssh-easy

Ein einfacher SSH-Verbindungsmanager mit Terminal-UI (TUI) zum Verwalten und Aufbauen von SSH-Verbindungen mit Local-Port-Forwarding-Tunneln.

## Features

- SSH-Verbindungen speichern und verwalten (Name, Host, Port, User)
- Automatische Authentifizierung: SSH-Agent, alle Keys in `~/.ssh/`, Passwort-Fallback
- Local Port Forwarding (localhost:port -> remote:port)
- Automatischer Ed25519-Schlüssel je Verbindung nach erfolgreicher Anmeldung (ohne Passphrase, siehe *Automatischer SSH-Schlüssel*)
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
| `c` | Gewählte Verbindung als neue kopieren: Name (mit angehängtem `#2`, `#3` …; lange Namen werden auf 50 Zeichen gekürzt), Host, Port, Benutzer und Tunnel (Zielport und Ein/Aus) werden übernommen, SSH-Schlüssel nicht. Der Cursor steht am Feldende |
| `e` | Verbindung bearbeiten |
| `d` | Verbindung loeschen |
| `Enter` | Verbindung herstellen / Status anzeigen |
| `x` | Verbindung trennen |
| `j/k` oder Pfeiltasten | Navigation |
| `→` / `Enter` auf einer Gruppe | Gruppe aufklappen |
| `←` | Gruppe zuklappen (auch von einem Gruppeneintrag aus) |
| `Bild↑` / `Bild↓` / `Pos1` / `Ende` | In langen Listen blättern |
| `u` | Auto-Update ein-/ausschalten (letzte Menüzeile) |
| `v` | *Verbindung merken* ein-/ausschalten (Standard aus, steht im Menü; siehe *Gemerkte Auswahl*) |
| `E` | Verbindungen exportieren (alle oder Auswahl) |
| `I` | Verbindungen aus einer Exportdatei importieren |
| `Enter` auf *Datei-Manager* | Datei-Manager öffnen (letzter Listeneintrag, nur sichtbar, wenn es mindestens eine Verbindung gibt) |
| `Tab` | Naechstes Formularfeld |
| `Strg+V` / `Shift+Einf` | Zwischenablage in Formularfeld und Passwortabfrage einfügen (auch unter Windows) |
| `Strg+Hoch` / `Strg+Runter` | Schriftgröße ändern (Windows; Strg+Mausrad geht auch) |
| `r` | Fenster- und Schriftgröße auf den Systemstandard zurücksetzen (Windows-Konsole) und die gemerkte Auswahl löschen (ohne steuerbare Konsole nur die Auswahl, mit Hinweis); in der Statusansicht: SSH-Schlüssel entfernen (fragt zuerst, `j`/`y` bestätigt) |
| `Esc` | Zurueck / Abbrechen |
| `q` / `Strg+C` / `Alt+F4` | Beenden (Alt+F4 auch in laufender SSH-Session); ein heruntergeladenes Update wird bei jedem Beenden installiert, auch beim Schließen des Fensters mit X |

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

### Automatischer SSH-Schlüssel

Nach **jeder** erfolgreichen Anmeldung – per Passwort, über den SSH-Agent oder mit einem anderen Schlüssel aus `~/.ssh/` – bekommt eine Verbindung ohne eigenen Schlüssel ungefragt einen, in der Verbindungsliste und im Datei-Manager. Künftige Anmeldungen laufen dann ohne Passwort:

- ssh-easy erzeugt einen neuen Ed25519-Schlüssel `~/.ssh/id_ed25519_<Host>_<Benutzer>` (`_2`, `_3` …, falls der Name schon von einer anderen Verbindung belegt ist) und trägt ihn auf dem Server in `~/.ssh/authorized_keys` ein (keine doppelten Einträge; die Rechte 700 für `~/.ssh` und 600 für die Datei werden gesetzt).
- **Der Schlüssel hat keine Passphrase.** Wer die Datei lesen kann, kann sich damit am Server anmelden. Das gilt auch dann, wenn die auslösende Anmeldung über den SSH-Agent lief.
- Jede Verbindung bekommt eine eigene Schlüsseldatei. Der eigene Schlüssel wird vor dem SSH-Agent probiert.
- Verbindungen, die auf SSH-Agent stehen oder schon einen Schlüssel haben, bleiben unberührt.
- **Merken und aussetzen:** Entfernst du den Schlüssel in der Statusansicht (`r`, dann `j`/`y`) oder sind 2 Versuche gescheitert, bekommt diese Verbindung keinen automatischen Schlüssel mehr. Eine Anmeldung mit eingetipptem Passwort versucht es wieder und schaltet die automatische Vergabe erneut ein.
- Entfernen löscht den Schlüssel auf dem Server und auf diesem PC – nicht aber, solange eine andere Verbindung dieselbe Schlüsseldatei nutzt, und bei gleichem Host und Benutzer auch nicht auf dem Server.
- Die Einstellungen dafür gehören nicht zum Export.

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

### Datei-Manager

Der letzte Eintrag der Verbindungsliste (nur sichtbar, wenn es mindestens eine Verbindung gibt) öffnet einen
reduzierten Datei-Manager im Stil des Midnight Commander, um mit zwei SSH-Servern
nebeneinander zu arbeiten – oder mit einem:

1. Verbindung für das **linke** Fenster wählen, dann die für das **rechte** Fenster.
   *Lokaler PC* (links erster Eintrag) zeigt das Dateisystem deines eigenen Rechners, so kopierst
   du zwischen PC und Server; unter Windows zeigt `..` oberhalb von `C:\` die Laufwerksliste
   (`q` führt jederzeit zurück zur Liste). Es gibt keinen Platzhalter: Der Cursor steht
   auf dem ersten Eintrag (oder auf der gemerkten Wahl, siehe unten) – vor `Enter` also hinsehen.
   Die linke Verbindung steht rechts nicht mehr zur Wahl. Für das rechte Fenster ist der
   erste Eintrag *(keine)*: Dann wird nur die linke Verbindung geöffnet, und der Datei-Manager
   zeigt **ein Fenster** über die ganze Breite. Alles Folgende gilt – Ansehen (`F3`),
   Bearbeiten (`F4`), Verzeichnis anlegen (`F7`), Löschen (`F8`), lokal öffnen,
   Menü (`F9`: *Fenster*, *Datei*, *Optionen*) –, nur Kopieren und Verschieben (`F5`/`F6`)
   entfallen, weil es kein zweites Fenster als Ziel gibt; `Tab` hat dort keine Wirkung.
   Bei genau einer gespeicherten Verbindung wird rechts trotzdem gefragt, damit das
   Kopieren zwischen Server und PC (*Lokaler PC*) möglich bleibt.
2. Beide Verbindungen werden als eigene SSH-Verbindungen **ohne Tunnel** aufgebaut
   (zuerst SSH-Agent/Schlüssel, bei Bedarf Passwortabfrage; `Esc` bricht sie ab).
   Auf den Servern muss nichts installiert sein – die Dateilisten kommen per SFTP.
   Nur zum Erfassen eines großen Baums vor dem Kopieren oder Verschieben fragt ssh-easy den Server
   zusätzlich mit einem einzigen `find`-Befehl über dieselbe Verbindung nach dem ganzen Baum (siehe
   *Kopieren*); ohne `find` oder ohne Erlaubnis, Befehle auszuführen, fällt es selbstständig auf
   SFTP zurück.
3. Beide Fenster starten im Home-Verzeichnis des Users.

| Taste | Aktion |
|-------|--------|
| `↑` / `↓` / `Bild↑` / `Bild↓` / `Pos1` / `Ende` | Cursor bewegen |
| `Enter` | In das Verzeichnis bzw. `..` wechseln |
| `Tab` | Zwischen linkem und rechtem Fenster wechseln |
| `Einfg` | Eintrag unter dem Cursor markieren/entmarkieren und eine Zeile weiter |
| `*` | Markierung aller Einträge (Dateien und Verzeichnisse) umkehren |
| `F3` | Datei unter dem Cursor ansehen – Tasten wie im mc-Betrachter: `F1` Hilfe, `F2` Umbruch, `F4` Hex/Text, `F5` Gehe zu, `F6` Suche mit regulärem Ausdruck, `F7` Suchen (`n` weiter), `F9` mit dem Standardprogramm öffnen, `F3`/`F10`/`q`/`Esc` schließen |
| `F4` | Datei unter dem Cursor bearbeiten – Tasten wie im mcedit: `F2` speichern, `F3` Block markieren, `F4` ersetzen, `F5`/`F6`/`F8` Block kopieren/verschieben/löschen, `F7` suchen, `F10`/`Esc` beenden, `Strg+Y` Zeile löschen, `Strg+V` / `Shift+Einfg` / `Strg+Einfg` aus der Zwischenablage einfügen (Textmodus); Binärdateien als bearbeitbare Hex-Ansicht |
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

Vor dem Kopieren erfasst ssh-easy alles, was kopiert werden soll (das Fenster zeigt
*Erfasse … N Dateien, M Verzeichnisse*). Ist die Quelle ein Server, liest es den ganzen Baum
zuerst mit **einem** `find`-Befehl über einen eigenen Kanal derselben SSH-Verbindung; die
Startpfade gehen über stdin, nie über die Befehlszeile. Die Antwort wird Feld für Feld
geprüft, in Größe, Eintragszahl und Zeit begrenzt und mit einer kleinen, per SFTP gelesenen
Stichprobe verglichen. Bei jedem Zweifel (kein `find -printf`, z. B. BusyBox; keine Erlaubnis
für Befehle; abweichende Sicht auf das Dateisystem; hängender Kanal) fällt ssh-easy auf das
Lesen per SFTP zurück, das mit bis zu 32 Anfragen gleichzeitig arbeitet. `Esc` bricht das
Erfassen sofort ab. Hängt oder stockt `find`, wird es für diese Verbindung bis zum Ende der
Sitzung ausgelassen. Gemessen bei etwa 30 ms Latenz: 30.000 leere Verzeichnisse brauchen mit
`find` 0,34 s, mit dem parallelen SFTP-Rückfall 119 s; vor Build 90 wurde derselbe Baum
streng Verzeichnis für Verzeichnis gelesen, was noch weit länger dauerte.

**Verschieben (`F6`)** läuft wie das Kopieren; jede Datei wird in der Quelle erst gelöscht,
wenn ihre Kopie vollständig angekommen ist. Was fehlschlägt oder übersprungen wird, bleibt
in der Quelle und bleibt markiert. Ein Ziel, das die Quelle selbst ist (z. B. zwei
Verbindungseinträge für denselben Server), wird abgelehnt. Nutzen beide Fenster dasselbe
**Konto** (gleicher Host, Port und Benutzer – auch bei zwei verschiedenen Verbindungseinträgen),
kopiert das Verschieben nicht: Der Server benennt die Einträge in einem Schritt um. Das dauert
Millisekunden, behält Besitzer und Rechte und ersetzt nie ein vorhandenes Ziel (dann wird wie
bisher kopiert, ebenso immer bei verschiedenen Benutzern). Eine Prüfdatei im Zielordner stellt
sicher, dass beide Verbindungen wirklich dasselbe Dateisystem sehen. Das Ergebnis lautet
*„Name“ verschoben* bzw. *N Einträge verschoben*. Danach werden die leeren Quellverzeichnisse
ebenenweise gleichzeitig entfernt (das Fenster zeigt den Fortschritt); dabei fallen nur
Verzeichnisse, nie eine Datei.

**Verzeichnis anlegen (`F7`)** fragt nach einem einzelnen Namen (ohne `/` und `\`); `Esc`
bricht ab.

**Löschen (`F8`)** entfernt Verzeichnisse samt Inhalt. Die Rückfrage beantwortest du mit
`j` oder `y` – `Enter` allein löscht nicht. Symlinks werden selbst entfernt, nie ihr Ziel.
`Esc` stoppt ein laufendes Löschen. Der Inhalt der Verzeichnisse wird vorab parallel gelesen;
Prüfen und Löschen laufen weiter ein Eintrag nach dem anderen, und beim Betreten jedes
Verzeichnisses wird die ganze Verzeichniskette erneut geprüft.

**Verbindungsabriss:** Reißt die Verbindung eines Fensters ab (auch wenn nur die SFTP-Sitzung
stirbt), verbindet das Fenster beim nächsten Einlesen einmal neu – `Enter` in ein Verzeichnis,
`Strg+R` oder das Neuladen nach Kopieren, Löschen und Anlegen. Braucht die Anmeldung ein
Passwort, fragt ein Dialog danach; es gilt nur für diesen einen Versuch und wird nie gespeichert.
`known_hosts` bleibt Pflicht: Bei geändertem Host-Key gibt es nur eine Meldung. Nichts
wiederholt sich von selbst, und ein unterbrochenes Kopieren wird nie fortgesetzt.

**Bearbeiten (`F4`)** funktioniert wie im mcedit. Zeilenenden (auch Windows-CRLF) bleiben
erhalten. Binärdateien öffnen sich als Hex-Ansicht, in der Hex-Ziffern Bytes überschreiben
(`Tab` wechselt in die Zeichenspalte); die Größe ändert sich nie. Gespeichert wird über eine
Temp-Datei, die am Ende umbenannt wird; die Rechte bleiben, und wurde die Datei inzwischen
auf dem Server geändert, wird vorher nachgefragt.

**Einfügen im Editor:** `Strg+V`, `Shift+Einfg` und `Strg+Einfg` fügen die Zwischenablage an der Cursorposition ein (nur im Textmodus; in den Datei-Fenstern markiert `Strg+Einfg` weiter). Zeilenenden werden zu LF, Steuerzeichen außer Tab und Zeilenumbruch entfallen. Würde die Datei dadurch größer als 16 MiB, lehnt der Editor das Einfügen mit einer Meldung ab. Eingabezeilen (Suchen `F7`, Ersetzen `F4`) nehmen dieselben Tasten an: Es kommt nur die erste Zeile an, Steuerzeichen entfallen, ein Tab wird zum Leerzeichen, und die Zeile behält ihre Längengrenze; eine noch unberührte Vorbelegung wird ersetzt, wie beim ersten getippten Zeichen. In der Hex-Ansicht, in der Hilfe und in Ja/Nein-Rückfragen passiert beim Einfügen nichts (eine Eingabezeile in der Hex-Ansicht nimmt es an).

**Lokal öffnen (`F9` im Betrachter)** lädt die Datei in einen privaten Temp-Ordner und
übergibt sie dem Standardprogramm deines Rechners (Bildbetrachter, Browser, PDF-Leser …).
Geöffnet werden nur bekannte Dokument- und Medientypen (Bilder, PDF, Text, HTML,
Audio/Video, Archive); Office-Dokumente jeder Art, Programme, Skripte, Verknüpfungen und
unbekannte Typen werden abgelehnt, weil „Öffnen“ dort „Ausführen“ heißen könnte. Unter Windows wird die Kopie als „aus dem
Internet“ gekennzeichnet (geschützte Ansicht, SmartScreen). Die Kopien werden beim Beenden
von ssh-easy gelöscht.

Der Datei-Manager braucht mindestens eine gespeicherte Verbindung
(die andere Seite kann *Lokaler PC* sein); bis dahin wird sein Eintrag nicht angezeigt. Vom Server auf den PC kopierte Dateien werden
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

### Gemerkte Auswahl

Standardmäßig aus. Der Schalter `v` *Verbindung merken* im Menü (`m`) schaltet sie ein. Dann werden die zuletzt mit `Enter` gewählte Verbindung und die letzte Wahl links/rechts im Datei-Manager (auch *Lokaler PC* und *(keine)*) sofort gespeichert und beim nächsten Start vorausgewählt. Ausschalten löscht die gemerkten Werte; `r` löscht sie ebenfalls. Eine inzwischen gelöschte Verbindung wird ignoriert. Die Werte stehen nicht im Export.

### Auto-Update

Standardmäßig aus. Mit `u` im Hauptmenü einschalten (der Zustand steht in der letzten Menüzeile, die `m` einblendet, und wird gespeichert). Eingeschaltet prüft ssh-easy beim Start das neueste GitHub-Release und lädt die passende Datei im Hintergrund; solange kein Update bereitliegt, wird alle 6 Stunden erneut gesucht, ein Release, das erscheint, während ssh-easy läuft, wird also ebenfalls gefunden (beim Beenden wird nicht gesucht); installiert wird beim Beenden – mit `q`, `Strg+C`, `Alt+F4` und auch beim Schließen des Fensters mit dem Kreuz, selbst während einer SSH-Sitzung (Windows-Konsole und Windows Terminal, Linux-Terminalfenster):

- Linux oder Windows mit Schreibrecht im Programmordner: Die Programmdatei wird ersetzt
- Windows-Installation unter *Programme*: Das Setup läuft still mit Administratorrechten (Windows fragt per Benutzerkontensteuerung); die Desktop-Verknüpfung bleibt erhalten

Jedes Release enthält eine mit Ed25519 signierte Prüfsummenliste (`ssh-easy-SHA256SUMS` + `ssh-easy-SHA256SUMS.sig`). ssh-easy prüft die Signatur mit dem im Programm eingebauten öffentlichen Schlüssel und installiert nur eine Datei, deren SHA-256 in der signierten Liste steht – selbst wer Zugriff auf das GitHub-Konto hat, kann so kein manipuliertes Update unterschieben. Dazu kommen Größen- und Formatprüfung – direkt nach dem Download und noch einmal unmittelbar vor der Installation. Unsignierte Releases werden abgelehnt. Neben dem Hauptschlüssel ist ein zweiter öffentlicher Schlüssel (Ersatzschlüssel) eingebaut, damit sich der Signaturschlüssel ohne Neuinstallation austauschen lässt; die Signatur jedes der beiden gilt. Schwache Schlüssel (Punkte kleiner Ordnung) werden abgelehnt. Zusätzlich kann `build.sh` die Windows-Programme und Setups mit Authenticode signieren, sobald ein Zertifikat eingerichtet ist (optional; noch nicht im Einsatz).

Unter Windows startet ssh-easy für die Installation beim Beenden einen kurzen Helfer ohne Fenster; er wartet, bis das Programm beendet ist, und startet dann das geprüfte Setup (mit Benutzerkontensteuerung). Grenze: Beim Abmelden oder Herunterfahren gibt Windows Programmen keine Gelegenheit zu reagieren – das Update wird dann beim nächsten regulären Beenden installiert. Bis Build 90 beendete das Schließen mit dem Kreuz unter Windows das Programm sofort, ohne zu installieren (nur `q` funktionierte); seit Build 91 behoben.

### Auto-Reconnect

Bei einem Verbindungsabbruch versucht ssh-easy automatisch, die Verbindung wiederherzustellen (max. 5 Versuche mit 3 Sekunden Abstand).

## Konfiguration

Verbindungen werden gespeichert unter:
- Linux: `~/.ssh-easy/connections.json`
- Windows: `%USERPROFILE%\.ssh-easy\connections.json`

Log-Datei:
- `~/.ssh-easy/ssh-easy.log`

Das Log nennt beim Start und beim Ende Version und Prozessnummer („ssh-easy v0.91 gestartet (windows/amd64, PID 1234)“) und hält das Ergebnis jeder Update-Suche fest. Unter Windows PowerShell 5 zeigt `Get-Content` das UTF-8-Log falsch an („Ã¼“ statt „ü“). So wird es richtig gelesen:

```powershell
Get-Content $env:USERPROFILE\.ssh-easy\ssh-easy.log -Tail 40 -Encoding UTF8
```

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
- Auto-Update: lädt nur aus den GitHub-Releases dieses Repositorys; die SHA-256 muss zur Ed25519-signierten Prüfsummenliste genau dieses Releases passen (öffentliche Schlüssel eingebaut – Hauptschlüssel plus Ersatzschlüssel für eine Schlüsselrotation; private nur auf dem Build-Rechner), vor der Installation erneut geprüft, dazu Größen- und Formatprüfung
- Export: keine SSH-Schlüssel oder Schlüsselpfade, Dateirechte `0600`, überschreibt nie und folgt keinem Symlink
- Import: Größenlimit, jeder Eintrag geprüft, keine Steuer-/Formatzeichen, Schlüsselpfade aus der Datei werden ignoriert, neue IDs
- Die DNS-Fallback-Server werden nur genutzt, wenn der DNS des Netzwerks nicht antwortet, und nie für lokale Namen (`.local`, `.lan`, Namen ohne Punkt …); einem unbekannten Host-Key, der über den Fallback-DNS erreicht wurde, wird nicht automatisch vertraut
- Einfügen per Shift+Einfg nutzt Bracketed Paste, sobald die Remote-Shell es aktiviert (`ESC[?2004h`): Der Text wird in `ESC[200~ … ESC[201~` eingeschlossen, eingebettete Marker werden entfernt – mehrzeiliger Inhalt wird nicht Zeile für Zeile sofort ausgeführt

## Lizenz

MIT – siehe die Datei [LICENSE](LICENSE). Copyright (c) 2026 Kurt Ingwer.
