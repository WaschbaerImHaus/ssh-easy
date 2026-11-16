# Optimierungsvorschlaege - ssh-easy

## Umgesetzt (Build 3)

- **TUI aufgeteilt**: In tui_list.go, tui_form.go, tui_status.go, tui_keygen.go
- **SSHManager-Struct**: Zentrale Verbindungsverwaltung mit Mutex-Schutz
- **Connection-Pooling**: Verbindungen werden im Manager gehalten und wiederverwendet
- **Lazy-Loading**: ConfigCache laedt nur bei Dateiaenderungen neu
- **SSH-Agent**: Unterstuetzung ueber SSH_AUTH_SOCK
- **Keepalive**: SSH-Keepalive alle 30 Sekunden
- **Logging**: Datei-basiertes Logging mit Levels (Info, Warn, Error)

## Offen

- ~~**Test `TestSSHTerminalRegistersEmergencyRestore` zu locker**~~ (Build 88) – erledigt in Build 89: prüft den Text des Funktionsliterals bis `})` und verlangt im `defer` `term.Restore` vor `unregister()`.
- ~~**`watchCloseSignals`: erst `onSignal`, dann loggen**~~ (Build 88) – erledigt in Build 89: `onSignal` läuft als Allererstes, das Protokollieren danach.
- ~~**Kopf von `tests/exit_signals_test.go` veraltet**~~ (Build 88) – erledigt in Build 89: Kopf auf die neuen Signaturen und die neue Reihenfolge aktualisiert.
- ~~**UX: Speicherfehler der Datei-Manager-Wahl erst nach dem Schließen sichtbar**~~ (Build 88) – Nutzerentscheidung, erledigt in Build 89: Der Fehler erscheint zusätzlich sofort in der Meldungszeile des Datei-Managers (`fm.message`, gemerkt in `fm.pickErr`); er übersteht Verbindungsaufbau und Passwortabfrage, ein anderer Fehler hat Vorrang.
- ~~**Schlüsselvergabe: Server-Skripte unter fish/csh nicht geprüft**~~ (Build 86) – erledigt in Build 90: geprüft, kein Fehler, keine Codeänderung. Neuer Regressionstest `tests/key_scripts_login_shell_test.go` (`TestKeyScriptsRunUnderLoginShells`, `…LoginShellCounterProbe`, `…FreeOfFishBackslashPairs`) führt beide Skripte unter sh, dash, bash, zsh, ksh, mksh, fish, tcsh und csh aus; fehlende Shell = Skip. Grenze: die rc-Dateien des Servernutzers (`~/.cshrc`, `config.fish`) laufen bei jedem Befehl mit und können das Ergebnis stören.
- ~~**Schlüssel entfernen bricht bei ungültigem UTF-8 oder NUL-Byte in `authorized_keys` ab**~~ (Build 86) – erledigt in Build 89: jedes grep in den Skripten (`src/ssh.go`) läuft mit `LC_ALL=C` und `-a`; GNU grep und BusyBox-grep geprüft.
- ~~**Neutraler Name für `fmDeployMu`/`fmFreshConnection`**~~ (Build 86) – erledigt in Build 90: jetzt `deployMu` und `freshConnection`.
- ~~**`src/ssh_manager.go`: 3 alte gofmt-Abweichungen**~~ (Build 86) – erledigt in Build 90.
- ~~**Datei-Manager: Symlink-Ziele einzeln geprüft**~~ (Build 53) – erledigt in Build 90: `readPanelDir` prüft die Ziele mit bis zu `fmStatParallel` (32) gleichzeitigen Anfragen.
- **Datei-Manager: Meldungen nur in der Infozeile des aktiven Fensters** (Build 53): Lange Meldungen werden bei schmalen Fenstern gekürzt. Alternative: Meldungsdialog in Fenstermitte wie im mc (kommt ohnehin mit den Bestätigungsdialogen für Kopieren/Löschen).
- ~~**Datei-Manager: Verschieben auf demselben Server kopiert**~~ (Build 65) – erledigt in Build 90 (Nutzerentscheidung): Auf demselben Konto (Host, Port und Benutzer gleich, auch bei zwei Einträgen) benennt F6 per `RenameNoReplace` um (`renameMoved`, `sameAccount`/`fmSameAccount`, Prüfdatei); anderer Benutzer weiter Kopieren, wegen Besitzer und Rechten.
- **Datei-Manager: Löschen Eintrag für Eintrag** (Build 65) – in Build 90 teilweise gelöst: Alle Lesezugriffe (Lstat, ReadDir, Kettenprüfung) laufen vorab parallel (`prefetchDelete`, `deleteParallel` 32). Geprüft und gelöscht wird bewusst weiter ein Eintrag nach dem anderen, mit erneuter Kettenprüfung beim Betreten jedes Verzeichnisses (Sicherheitsaudit Build 90). Voll paralleles Löschen ist bewusst nicht gewollt (Nutzerentscheidung); schneller geht es nur über gebündelte Serverbefehle (siehe nächster Punkt).
- **Editor: Rückgängig (Undo) fehlt** (Build 62): mcedit kennt Strg+U; ein einfacher Undo-Stapel (Kopie der geänderten Zeilen je Schritt) wäre der nächste Komfortschritt.
- ~~**Editor: Suchzeile F7 ohne Einfügen**~~ (Build 89) – erledigt in Build 90: Alle Eingabezeilen des Editors (Suchen, Ersetzen) nehmen Strg+V, Shift+Einfg und Strg+Einfg an (`pasteIntoPrompt`, nur erste Zeile, Steuerzeichen entfallen, Tab → Leerzeichen, Längengrenze).
- ~~**Datei-Manager: Kopieren Datei für Datei**~~ (Build 57) – parallel seit Build 78 (8), 32 Dateien und parallele Verzeichnisse seit Build 81.
- **Datei-Manager: Daten laufen über den lokalen Rechner** (Build 57) – Vorschlag für Build 92, siehe nächster Punkt.
- **Datei-Manager: Massenaufrufe per Exec bündeln, wo verfügbar** (Nutzerwunsch Build 90, vorgemerkt für Build 91): Der `find`-Schnellweg zeigt, dass ein Exec-Befehl viele SFTP-Rundreisen ersetzt. Weitere Kandidaten, jeweils mit Rückfall auf SFTP und mit denselben Prüfungen wie heute:
  - Löschen: `rm -rf --one-file-system` bzw. `xargs -0 rm`/`rmdir` für ganze Teilbäume statt Eintrag für Eintrag (die Kettenprüfung gegen ausgetauschte Verzeichnisse darf nicht wegfallen).
  - Verzeichnisse anlegen: `xargs -0 mkdir -m 0700` statt je ein `Mkdir` + `chmod`.
  - `chmod`/`touch` gruppiert für Rechte und Zeiten am Ende (`finishDirs`, `setAttributes`).
  - Einlesen eines Fensters per `find -maxdepth 1`, Zählen vor F8 per `find`.
  - Bewusst KEIN tar-Strom über Exec: Dabei fielen Temp-Datei + Umbenennen, Namens- und Pfadprüfungen und die Rückfragen beim Überschreiben weg.
- **Datei-Manager: `find` nach dem Neuverbinden erneut versuchen** (Nutzerwunsch 2026-11-16, vorgemerkt für Build 91; Fund 1 aus dem Fable-Review des Build-90-Nachtrags): Fällt die Leitung während des `find`-Erfassens aus, wertet ssh-easy das heute wie einen hängenden `find`-Kanal. `inheritFastScanOff` (`tui_filemanager_reconnect.go`) überträgt die Abschaltung auf die neue, gesunde Verbindung, und der Schnellweg bleibt bis zum Schließen des Datei-Managers aus. Große Bäume brauchen dann wieder Minuten statt Sekunden.
  - Vorschlag des Nutzers: Nach dem Neuverbinden bekommt `find` genau einen neuen Versuch. Unbedenklich, weil `find` nur liest.
  - Hängt `find` auch auf der neuen Verbindung, bleibt der Schnellweg bis zum Sitzungsende aus. Dann liegt es wirklich am Server, und jeder weitere Versuch kostete wieder 30 + 5 s, eine Trennung und bei Passwort-Anmeldung eine neue Passwortabfrage. Deshalb nur ein Versuch, nicht beliebig viele.
  - Umsetzung: `inheritFastScanOff` übernimmt statt des Schalters einen Zähler (Fehlversuche je Datei-Manager-Sitzung). Ab 2 Fehlversuchen ist der Schnellweg aus. Esc zählt weiterhin nicht.
  - Optional zusätzlich: vor `AbortTreeList` kurz prüfen, ob die Verbindung noch antwortet (Keepalive innerhalb der Schonfrist). Ein echter Netzabriss zählt dann gar nicht als Fehlversuch.
  - Tests: Neuverbinden nach einem hängenden `find`, danach läuft `find` wieder; zweites Hängen schaltet ab; Esc zählt nicht. Jeweils mit Gegenprobe, auch über den In-Prozess-Server (`tests/filemanager_sftpfs_test.go`).
- **Datei-Manager: Direktübertragung Server → Server** (Nutzerwunsch Build 90, vorgemerkt für Build 92; ersetzt den alten Punkt „Daten laufen über den lokalen Rechner“): Server 1 verbindet sich selbst mit Server 2 (`rsync` über SSH), die Daten laufen nicht mehr über den PC.
  - Anmeldung über einen eingeschränkten, weitergereichten Agenten: nur mit dem einen Schlüssel für Server 2 und nur für die Dauer der Übertragung; vorher deutlicher Risikohinweis (Nutzer entscheidet, wie bei den F9-Endungen).
  - Die `known_hosts`-Einträge von Server 2 werden mitgegeben (kein ungeprüftes Vertrauen).
  - Rückfall auf den bisherigen Weg über den PC bei: nur Passwort-Anmeldung, Server 2 von Server 1 nicht erreichbar, kein `rsync` auf Server 1, reiner SFTP-Zugang ohne Exec.
- **Datei-Manager: Einträge je Verzeichnis nach dem Kopieren abgleichen** (Build 90, aus SECURITY_RISKS.md „find-Schnellweg glaubt einer Stichprobe“): Der `find`-Schnellweg prüft nur eine Stichprobe per SFTP (16 Einträge per Lstat, 8 Verzeichnisse per ReadDir); bei großen Bäumen bleibt eine Abweichung außerhalb unentdeckt. Nach dem Kopieren die Zahl der Einträge je Verzeichnis in Quelle und Ziel vergleichen (und beim Verschieben vor dem Löschen der Quellverzeichnisse) schlösse die Lücke; Kosten: ein ReadDir je Verzeichnis, also das, was der Schnellweg gerade spart. Alternativ: Zahl nur stichprobenartig oder nur bei Verschieben prüfen (`specialist-filemanager`).
- ~~**Datei-Manager: keine Keepalives**~~ (Build 53) – Keepalive mit Timeout seit Build 81 (`watchConnection`); automatischer Reconnect des Dateifensters nach einem Abriss seit Build 90 (`tui_filemanager_reconnect.go`: beim nächsten Einlesen oder Strg+R, genau ein Versuch, Passwortdialog, `lostAfter` erkennt auch eine gestorbene SFTP-Sitzung).
- ~~**Datei-Manager: Download mit Pause nach jedem Block**~~ (Build 81) – seit Build 82 `readChunksAt` (bis zu 4 `ReadAt` gleichzeitig, langsamer Start für kleine Dateien).
- **Datei-Manager: Zielverzeichnis nur zum Aufräumen gelesen** (Build 82): Auf dem lokalen PC und im Zweig mit Einzelprüfung wird ein vorhandenes Zielverzeichnis jetzt einmal ganz gelesen, nur um liegen gebliebene Temp-Dateien vorzumerken (bei 200.000 Einträgen spürbar). Möglich: nur Verzeichnisse lesen, die ohnehin gelesen werden, oder eine Obergrenze vor dem Lesen.
- **Datei-Manager: Rundreisen pro Datei** (Build 81): Anlegen, chmod 0600, Schreiben, Schließen, Rechte+Zeit, Umbenennen = 5 Rundreisen nacheinander. pkg/sftp kann beim Anlegen (OPEN) und Mkdir keine Rechte mitsenden; mit einem eigenen Paket (ATTRS im OPEN) fiele chmod 0600 weg – dann aber ohne Rückfallprüfung `errCannotProtect`, falls der Server die Rechte ignoriert.

- **Log-Rotation**: Automatische Rotation/Komprimierung der Log-Datei
- **Config-Backup**: Vor Aenderungen Backup der Config-Datei erstellen
- **Terminal-Groesse**: TUI an Fenstergroesse anpassen (responsive Layout)
- **Verbindungs-Timeout konfigurierbar**: Seit Build 48 fest 5 s (erster Verbindungsaufbau) bzw. 10 s (Reconnect) – ggf. in connections.json einstellbar machen
- **Auth-Methoden in einem Handshake**: ConnectAuto baut für jeden Schlüssel eine eigene SSH-Verbindung auf (wegen Auth-Cache-Zuordnung). Ein einziger Handshake mit allen Methoden + PublicKeysCallback, der den erfolgreichen Signer merkt, spart bei vielen Keys deutlich Zeit
- ~~**Auto-Update signieren**~~ – erledigt in Build 51 (Ed25519-signierte Prüfsummenliste, `src/releasesig`).
  - ~~Schlüsselrotation~~ – erledigt in Build 90: Ersatzschlüssel eingebettet (`releaseBackupSigningPublicKeysHex`, `releasesig.VerifyAny`), siehe rules/build-release.md.
  - Authenticode-Signatur der Windows-EXEs gegen die UAC-Meldung „Unbekannter Herausgeber“: in Build 90 in `build.sh` vorbereitet (optional, `osslsigncode`), **Zertifikat fehlt** (siehe `research/authenticode-optionen.md`).
- **Fenstergröße unter Linux**: Optional per xterm-Sequenz `CSI 8;h;w t` wiederherstellen (nur Terminals, die das unterstützen)
- **i18n vereinheitlichen**: Translations-Struct (41 × ~80 Felder) schrittweise in die Schlüssel-Tabelle aus i18n_extra.go überführen
