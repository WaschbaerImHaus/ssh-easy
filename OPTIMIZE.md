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

- **Datei-Manager: Symlink-Ziele einzeln geprüft** (Build 53): `readPanelDir` ruft für jeden Symlink ein eigenes SFTP-`Stat` auf (ein Roundtrip pro Link). In Verzeichnissen mit sehr vielen Symlinks (z. B. `/usr/lib`, `/etc/alternatives`) über langsame Leitungen spürbar. Möglich: Stat-Aufrufe parallel (SFTP erlaubt mehrere offene Anfragen) oder erst beim Anwählen prüfen.
- **Datei-Manager: Meldungen nur in der Infozeile des aktiven Fensters** (Build 53): Lange Meldungen werden bei schmalen Fenstern gekürzt. Alternative: Meldungsdialog in Fenstermitte wie im mc (kommt ohnehin mit den Bestätigungsdialogen für Kopieren/Löschen).
- **Datei-Manager: Verschieben auf demselben Server kopiert** (Build 65): F6 kopiert immer und löscht dann die Quelle, auch wenn beide Fenster auf demselben Server liegen. Ein SFTP-Rename wäre dort sofort fertig, ändert aber Besitzer/Rechte-Semantik (Datei behält den Besitzer der Quellverbindung) und braucht dieselbe Prüfung "Ziel = Quelle". Für große Dateien auf einem Server lohnend.
- **Datei-Manager: Löschen Eintrag für Eintrag** (Build 65): pro Eintrag Lstat + Remove (bei Verzeichnissen zusätzlich ReadDir). Große Bäume über langsame Leitungen dauern; SFTP erlaubt mehrere offene Anfragen, die Löschungen eines Verzeichnisses könnten parallel laufen.
- **Editor: Rückgängig (Undo) fehlt** (Build 62): mcedit kennt Strg+U; ein einfacher Undo-Stapel (Kopie der geänderten Zeilen je Schritt) wäre der nächste Komfortschritt.
- ~~**Datei-Manager: Kopieren Datei für Datei**~~ (Build 57) – parallel seit Build 78 (8), 32 Dateien und parallele Verzeichnisse seit Build 81.
- **Datei-Manager: Daten laufen über den lokalen Rechner** (Build 57): Bei zwei Servern im selben Rechenzentrum wäre eine direkte Übertragung (z. B. `scp`/`rsync` von Server zu Server per Agent-Forwarding) schneller, setzt aber Werkzeuge und gegenseitige Erreichbarkeit voraus. Bewusst nicht umgesetzt (Hoster-Einschränkungen).
- ~~**Datei-Manager: keine Keepalives**~~ (Build 53) – Keepalive mit Timeout seit Build 81 (`watchConnection`). Offen: automatischer Reconnect des Dateifensters nach einem Abriss.
- **Datei-Manager: Download mit Pause nach jedem Block** (Build 81): Beim Lesen vom Server wartet `sftp.File.Read` je 2-MiB-Block auf alle Antworten, wie beim Schreiben vor Build 81 (bei 120 ms Ping rund 27 statt 38 Mbit/s). Abhilfe wie `pipeCopyAt`: mehrere `ReadAt` gleichzeitig, Blöcke der Reihe nach weitergeben.
- **Datei-Manager: Rundreisen pro Datei** (Build 81): Anlegen, chmod 0600, Schreiben, Schließen, Rechte+Zeit, Umbenennen = 5 Rundreisen nacheinander. pkg/sftp kann beim Anlegen (OPEN) und Mkdir keine Rechte mitsenden; mit einem eigenen Paket (ATTRS im OPEN) fiele chmod 0600 weg – dann aber ohne Rückfallprüfung `errCannotProtect`, falls der Server die Rechte ignoriert.

- **Log-Rotation**: Automatische Rotation/Komprimierung der Log-Datei
- **Config-Backup**: Vor Aenderungen Backup der Config-Datei erstellen
- **Terminal-Groesse**: TUI an Fenstergroesse anpassen (responsive Layout)
- **Verbindungs-Timeout konfigurierbar**: Seit Build 48 fest 5 s (erster Verbindungsaufbau) bzw. 10 s (Reconnect) – ggf. in connections.json einstellbar machen
- **Auth-Methoden in einem Handshake**: ConnectAuto baut für jeden Schlüssel eine eigene SSH-Verbindung auf (wegen Auth-Cache-Zuordnung). Ein einziger Handshake mit allen Methoden + PublicKeysCallback, der den erfolgreichen Signer merkt, spart bei vielen Keys deutlich Zeit
- ~~**Auto-Update signieren**~~ – erledigt in Build 51 (Ed25519-signierte Prüfsummenliste, `src/releasesig`). Offen: Schlüsselrotation (zweiten öffentlichen Schlüssel einbetten, damit ein Schlüsselwechsel ohne Neuinstallation möglich ist) und Authenticode-Signatur der Windows-EXEs gegen die UAC-Meldung „Unbekannter Herausgeber“
- **Fenstergröße unter Linux**: Optional per xterm-Sequenz `CSI 8;h;w t` wiederherstellen (nur Terminals, die das unterstützen)
- **i18n vereinheitlichen**: Translations-Struct (41 × ~80 Felder) schrittweise in die Schlüssel-Tabelle aus i18n_extra.go überführen
