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
- **Schlüsselvergabe: Server-Skripte unter fish/csh nicht geprüft** (Build 86): Die Skripte zum Eintragen und Entfernen in `authorized_keys` laufen über die Login-Shell des Servers. Getestet sind sh/bash/BusyBox-sh; bei fish oder csh als Login-Shell ist das Verhalten ungeprüft (E2E mit `helper-e2e-sshd` möglich).
- ~~**Schlüssel entfernen bricht bei ungültigem UTF-8 oder NUL-Byte in `authorized_keys` ab**~~ (Build 86) – erledigt in Build 89: jedes grep in den Skripten (`src/ssh.go`) läuft mit `LC_ALL=C` und `-a`; GNU grep und BusyBox-grep geprüft.
- **Neutraler Name für `fmDeployMu`/`fmFreshConnection`** (Build 86): Beide werden jetzt auch vom Hauptablauf genutzt, nicht nur vom Datei-Manager; ein neutraler Name (z. B. `deployMu`) wäre passender (`worker-refactorer`).
- **`src/ssh_manager.go`: 3 alte gofmt-Abweichungen** (Build 86): Vorhanden schon vor Build 86; nur diese Datei formatieren (`worker-refactorer`), nie `gofmt -w *.go`.
- **Datei-Manager: Symlink-Ziele einzeln geprüft** (Build 53): `readPanelDir` ruft für jeden Symlink ein eigenes SFTP-`Stat` auf (ein Roundtrip pro Link). In Verzeichnissen mit sehr vielen Symlinks (z. B. `/usr/lib`, `/etc/alternatives`) über langsame Leitungen spürbar. Möglich: Stat-Aufrufe parallel (SFTP erlaubt mehrere offene Anfragen) oder erst beim Anwählen prüfen.
- **Datei-Manager: Meldungen nur in der Infozeile des aktiven Fensters** (Build 53): Lange Meldungen werden bei schmalen Fenstern gekürzt. Alternative: Meldungsdialog in Fenstermitte wie im mc (kommt ohnehin mit den Bestätigungsdialogen für Kopieren/Löschen).
- **Datei-Manager: Verschieben auf demselben Server kopiert** (Build 65): F6 kopiert immer und löscht dann die Quelle, auch wenn beide Fenster auf demselben Server liegen. Ein SFTP-Rename wäre dort sofort fertig, ändert aber Besitzer/Rechte-Semantik (Datei behält den Besitzer der Quellverbindung) und braucht dieselbe Prüfung "Ziel = Quelle". Für große Dateien auf einem Server lohnend.
- **Datei-Manager: Löschen Eintrag für Eintrag** (Build 65): pro Eintrag Lstat + Remove (bei Verzeichnissen zusätzlich ReadDir). Große Bäume über langsame Leitungen dauern; SFTP erlaubt mehrere offene Anfragen, die Löschungen eines Verzeichnisses könnten parallel laufen.
- **Editor: Rückgängig (Undo) fehlt** (Build 62): mcedit kennt Strg+U; ein einfacher Undo-Stapel (Kopie der geänderten Zeilen je Schritt) wäre der nächste Komfortschritt.
- **Editor: Suchzeile F7 ohne Einfügen** (Build 89): Strg+V, Shift+Einfg und Strg+Einfg wirken nur im Text, nicht in der Suchzeile (F7) und den anderen Eingabezeilen des Editors (`fmEditorAcceptsPaste` verlangt `prompt == edPromptNone`). Suchbegriffe ließen sich so nicht aus der Zwischenablage einfügen; die Eingabezeile bräuchte dieselbe Bereinigung (`cleanEditPaste`, einzeilig).
- ~~**Datei-Manager: Kopieren Datei für Datei**~~ (Build 57) – parallel seit Build 78 (8), 32 Dateien und parallele Verzeichnisse seit Build 81.
- **Datei-Manager: Daten laufen über den lokalen Rechner** (Build 57): Bei zwei Servern im selben Rechenzentrum wäre eine direkte Übertragung (z. B. `scp`/`rsync` von Server zu Server per Agent-Forwarding) schneller, setzt aber Werkzeuge und gegenseitige Erreichbarkeit voraus. Bewusst nicht umgesetzt (Hoster-Einschränkungen).
- ~~**Datei-Manager: keine Keepalives**~~ (Build 53) – Keepalive mit Timeout seit Build 81 (`watchConnection`). Offen: automatischer Reconnect des Dateifensters nach einem Abriss.
- ~~**Datei-Manager: Download mit Pause nach jedem Block**~~ (Build 81) – seit Build 82 `readChunksAt` (bis zu 4 `ReadAt` gleichzeitig, langsamer Start für kleine Dateien).
- **Datei-Manager: Zielverzeichnis nur zum Aufräumen gelesen** (Build 82): Auf dem lokalen PC und im Zweig mit Einzelprüfung wird ein vorhandenes Zielverzeichnis jetzt einmal ganz gelesen, nur um liegen gebliebene Temp-Dateien vorzumerken (bei 200.000 Einträgen spürbar). Möglich: nur Verzeichnisse lesen, die ohnehin gelesen werden, oder eine Obergrenze vor dem Lesen.
- **Datei-Manager: Rundreisen pro Datei** (Build 81): Anlegen, chmod 0600, Schreiben, Schließen, Rechte+Zeit, Umbenennen = 5 Rundreisen nacheinander. pkg/sftp kann beim Anlegen (OPEN) und Mkdir keine Rechte mitsenden; mit einem eigenen Paket (ATTRS im OPEN) fiele chmod 0600 weg – dann aber ohne Rückfallprüfung `errCannotProtect`, falls der Server die Rechte ignoriert.

- **Log-Rotation**: Automatische Rotation/Komprimierung der Log-Datei
- **Config-Backup**: Vor Aenderungen Backup der Config-Datei erstellen
- **Terminal-Groesse**: TUI an Fenstergroesse anpassen (responsive Layout)
- **Verbindungs-Timeout konfigurierbar**: Seit Build 48 fest 5 s (erster Verbindungsaufbau) bzw. 10 s (Reconnect) – ggf. in connections.json einstellbar machen
- **Auth-Methoden in einem Handshake**: ConnectAuto baut für jeden Schlüssel eine eigene SSH-Verbindung auf (wegen Auth-Cache-Zuordnung). Ein einziger Handshake mit allen Methoden + PublicKeysCallback, der den erfolgreichen Signer merkt, spart bei vielen Keys deutlich Zeit
- ~~**Auto-Update signieren**~~ – erledigt in Build 51 (Ed25519-signierte Prüfsummenliste, `src/releasesig`). Offen: Schlüsselrotation (zweiten öffentlichen Schlüssel einbetten, damit ein Schlüsselwechsel ohne Neuinstallation möglich ist) und Authenticode-Signatur der Windows-EXEs gegen die UAC-Meldung „Unbekannter Herausgeber“
- **Fenstergröße unter Linux**: Optional per xterm-Sequenz `CSI 8;h;w t` wiederherstellen (nur Terminals, die das unterstützen)
- **i18n vereinheitlichen**: Translations-Struct (41 × ~80 Felder) schrittweise in die Schlüssel-Tabelle aus i18n_extra.go überführen
