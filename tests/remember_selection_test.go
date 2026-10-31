// Paket main - Tests: Auswahl merken, ein Datei-Manager-Eintrag (Build 87)
//
// Nutzerwunsch 2026-10-28:
//   - Das Programm merkt sich die letzte Verbindung der Verbindungsliste als
//     Vorauswahl für den nächsten Start - aber nur, wenn der neue Schalter
//     "Verbindung merken" (Taste v, Standard aus) eingeschaltet ist.
//   - Wird eine Verbindung gelöscht, die noch als Vorauswahl gemerkt ist,
//     wird die Vorauswahl ignoriert (Cursor oben, keine Meldung).
//   - Der Datei-Manager erscheint nur noch, wenn es mindestens eine
//     Verbindung gibt, und zwar als EINE Zeile (kein "eine/zwei Verbindungen").
//   - Die Taste r setzt zusätzlich die gemerkte Auswahl zurück.
//
// Die Tests zur Auswahl im Datei-Manager (links/rechts, "(keine)", gemerkte
// Wahl) stehen in filemanager_pick_test.go.
//
// Aufbau: Alle Modelle starten mit NewAppModel auf einer Konfigurationsdatei
// (restartModel), damit die Tests nicht davon abhängen, WO das Modell den
// Schalter und die gemerkten Werte zwischenspeichert. Geprüft wird, was in
// der Datei steht und was der Nutzer sieht.
//
// @author Kurt Ingwer
// @date   2026-10-30 21:14
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// --- Hilfsfunktionen ---

// restartModel startet ein NEUES Modell auf der Konfigurationsdatei des
// übergebenen (wie ein Programmneustart). Die Verbindungsfunktionen sind
// Testdoubles: fmConnect ist der Rekorder; der Lokale PC ist ein Fake und
// trägt sich als "local" in rec.calls ein. Terminalgröße 100x30.
func restartModel(t *testing.T, m AppModel) (AppModel, *fmConnectRecorder) {
	t.Helper()
	n := NewAppModel(m.configPath, "87", NewSSHManager(NewLogger()))
	rec := newConnectRecorder()
	n.fmConnect = rec.connect
	n.fmLocalConnect = func() (remoteFS, error) {
		rec.calls = append(rec.calls, "local")
		return newFakeFS(), nil
	}
	n.termWidth, n.termHeight = 100, 30
	return n, rec
}

// newRememberModel legt eine Konfiguration mit den Verbindungen names
// (IDs id-0, id-1 ... in der Reihenfolge der Namen), dem Schalter und den
// drei gemerkten Werten an und startet ein Modell darauf. Die Namen sind
// alphabetisch sortiert zu übergeben, damit ID und Listenposition passen.
func newRememberModel(t *testing.T, on bool, conn, left, right string, names ...string) (AppModel, *fmConnectRecorder) {
	t.Helper()
	m := newListTestModel(t, names...)
	setRememberConfig(t, m.configPath, on, conn, left, right)
	return restartModel(t, m)
}

// setRememberConfig schreibt Schalter und gemerkte Werte in die Konfiguration
// (alles andere bleibt erhalten).
func setRememberConfig(t *testing.T, configPath string, on bool, conn, left, right string) {
	t.Helper()
	err := updateConfig(configPath, func(cfg *AppConfig) bool {
		cfg.RememberSelection = on
		cfg.LastConnectionID = conn
		cfg.FileManagerLastLeft = left
		cfg.FileManagerLastRight = right
		return true
	})
	if err != nil {
		t.Fatalf("updateConfig: %v", err)
	}
}

// configBytes liefert den Rohinhalt der Konfigurationsdatei.
func configBytes(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Konfiguration lesen: %v", err)
	}
	return string(data)
}

// backdateConfig setzt die Änderungszeit der Konfigurationsdatei eine Stunde
// zurück und liefert sie. Wird die Datei danach neu geschrieben, ist die Zeit
// eine andere - so lässt sich "nichts gespeichert" auch dann prüfen, wenn
// der neu geschriebene Inhalt zufällig gleich wäre.
func backdateConfig(t *testing.T, path string) time.Time {
	t.Helper()
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	return past
}

// assertConfigUntouched prüft, dass die Konfigurationsdatei weder neu
// geschrieben (Änderungszeit) noch inhaltlich verändert wurde.
func assertConfigUntouched(t *testing.T, path, before string, past time.Time, what string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.ModTime().Equal(past) {
		t.Errorf("%s: Konfiguration wurde neu geschrieben", what)
	}
	if got := configBytes(t, path); got != before {
		t.Errorf("%s: Konfiguration verändert:\nvorher  %s\nnachher %s", what, before, got)
	}
}

// resizeModel meldet dem Modell eine neue Terminalgröße (wie Bubbletea).
func resizeModel(t *testing.T, m AppModel, width, height int) AppModel {
	t.Helper()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(AppModel)
}

// currentName liefert den Namen der Verbindung unter dem Cursor ("" bei
// Gruppen- und Datei-Manager-Zeilen).
func currentName(m AppModel) string {
	conn, ok := m.selectedConnection()
	if !ok {
		return ""
	}
	return conn.Name
}

// --- Liste: Datei-Manager-Zeile (L1-L4) ---

// TestListWithoutConnectionsHasNoFileManager (L1): Ohne Verbindung gibt es
// keine einzige Zeile - auch keine Datei-Manager-Zeile und keinen Text dazu.
// Alle Tasten der Liste dürfen dann nicht abstürzen (Randfall: Cursor auf
// einer nicht vorhandenen Zeile) und nichts öffnen.
func TestListWithoutConnectionsHasNoFileManager(t *testing.T) {
	m := newListTestModel(t)
	if rows := m.listRows(); len(rows) != 0 {
		t.Fatalf("Zeilen %v, erwartet keine", rows)
	}
	for _, size := range [][2]int{{0, 0}, {100, 24}, {20, 8}} {
		got := resizeModel(t, m, size[0], size[1])
		text := stripANSI(renderListText(got))
		for _, entry := range []string{"Datei-Manager", "File manager"} {
			if strings.Contains(text, entry) {
				t.Errorf("Größe %v: Datei-Manager-Text %q ohne Verbindung:\n%s", size, entry, text)
			}
		}
		if !strings.Contains(text, got.lang.NoConnections) {
			t.Errorf("Größe %v: Hinweis auf fehlende Verbindungen fehlt:\n%s", size, text)
		}
	}
	for _, key := range []string{"end", "enter", "pgdown", "pgup", "home", "up", "down", "left", "right", "e", "d", "x", "c", "v", "m"} {
		got := press(t, m, key)
		if got.state != ViewList {
			t.Errorf("Taste %q ohne Verbindung: Ansicht %v, erwartet ViewList", key, got.state)
		}
		if got.cursor != 0 {
			t.Errorf("Taste %q ohne Verbindung: Cursor %d, erwartet 0", key, got.cursor)
		}
		_ = renderListText(got)
	}
}

// TestListFileManagerRowOnceAtEnd (L2): Ab einer Verbindung ist die letzte
// Zeile genau einmal der Datei-Manager (rowFileManager, Text txtFMEntry) mit
// einer Leerzeile davor; nicht ausgegraut, ohne Zusatztext "(eine/zwei
// Verbindungen)". Geprüft für einzelne, mehrere und gruppierte Verbindungen.
func TestListFileManagerRowOnceAtEnd(t *testing.T) {
	withColorProfile(t)
	cases := [][]string{{"Server"}, {"A", "B", "C"}, {"Abc:1", "Abc:2"}, {"Abc:1", "Solo"}}
	for _, names := range cases {
		m := newListTestModel(t, names...)
		rows := m.listRows()
		if last := rows[len(rows)-1]; last.kind != rowFileManager {
			t.Fatalf("%v: letzte Zeile hat Typ %v, erwartet rowFileManager", names, last.kind)
		}
		for _, row := range rows[:len(rows)-1] {
			if row.kind == rowFileManager {
				t.Errorf("%v: Datei-Manager-Zeile kommt mehrfach vor", names)
			}
		}
		raw := renderListText(m)
		text := stripANSI(raw)
		entry := m.tr(txtFMEntry)
		if n := strings.Count(text, entry); n != 1 {
			t.Errorf("%v: %q kommt %dmal vor, erwartet 1:\n%s", names, entry, n, text)
		}
		if strings.Contains(text, "zwei Verbindungen") || strings.Contains(text, "eine Verbindung)") {
			t.Errorf("%v: alter Zusatztext \"(eine/zwei Verbindungen)\" steht noch da:\n%s", names, text)
		}
		lines := strings.Split(text, "\n")
		at := -1
		for i, line := range lines {
			if strings.Contains(line, entry) {
				at = i
			}
		}
		if at < 2 || strings.TrimSpace(lines[at-1]) != "" || strings.TrimSpace(lines[at-2]) == "" {
			t.Fatalf("%v: Leerzeile vor %q fehlt:\n%s", names, entry, text)
		}
		if !strings.HasSuffix(strings.TrimSpace(lines[at]), entry) {
			t.Errorf("%v: Zeile %q hat Zusatztext hinter %q", names, lines[at], entry)
		}
		// Nicht ausgegraut (weder normal noch als Cursorzeile)
		for _, cursorOnRow := range []bool{false, true} {
			got := m
			if cursorOnRow {
				got = press(t, m, "end")
			}
			line := selectedLine(t, renderListText(got), entry)
			if strings.Contains(line, "38;5;244") {
				t.Errorf("%v: Datei-Manager ausgegraut (Cursor darauf: %v): %q", names, cursorOnRow, line)
			}
		}
	}
}

// TestListEnterOnFileManagerOpensLeftPick (L3): Enter auf der Datei-Manager-
// Zeile öffnet die Auswahl der linken Seite. openFileManager() hat keinen
// Parameter mehr (der Ein-Fenster-Modus entsteht erst rechts).
func TestListEnterOnFileManagerOpensLeftPick(t *testing.T) {
	m := newListTestModel(t, "A", "B")
	m = press(t, m, "end")
	if rows := m.listRows(); rows[m.cursor].kind != rowFileManager {
		t.Fatal("Cursor steht nicht auf der Datei-Manager-Zeile")
	}
	got := press(t, m, "enter")
	if got.state != ViewFMPick || got.fm.pickSide != 0 || got.fm.single {
		t.Errorf("Enter: state %v, Seite %d, single %v - erwartet linke Auswahl", got.state, got.fm.pickSide, got.fm.single)
	}
	updated, _ := m.openFileManager()
	opened := updated.(AppModel)
	if opened.state != ViewFMPick || opened.fm.pickSide != 0 || opened.fm.single {
		t.Errorf("openFileManager(): state %v, Seite %d, single %v", opened.state, opened.fm.pickSide, opened.fm.single)
	}
}

// TestListDeleteLastConnectionRemovesFileManagerRow (L4): Wird die letzte
// Verbindung gelöscht, verschwindet die Datei-Manager-Zeile; der Cursor
// steht auf 0, nichts stürzt ab. Gleiches gilt, wenn die gelöschte
// Verbindung gemerkt war.
func TestListDeleteLastConnectionRemovesFileManagerRow(t *testing.T) {
	for _, remembered := range []bool{false, true} {
		m, _ := newRememberModel(t, remembered, "id-0", "", "", "Server")
		if remembered && currentName(m) != "Server" {
			t.Fatalf("Vorbedingung: gemerkte Verbindung nicht gewählt")
		}
		m = press(t, m, "d", "y")
		if m.state != ViewList {
			t.Fatalf("gemerkt=%v: state %v nach dem Löschen", remembered, m.state)
		}
		if rows := m.listRows(); len(rows) != 0 || m.cursor != 0 {
			t.Errorf("gemerkt=%v: %d Zeilen, Cursor %d - erwartet keine Zeilen und Cursor 0", remembered, len(rows), m.cursor)
		}
		text := stripANSI(renderListText(m))
		if strings.Contains(text, m.tr(txtFMEntry)) {
			t.Errorf("gemerkt=%v: Datei-Manager-Text nach dem Löschen der letzten Verbindung:\n%s", remembered, text)
		}
		for _, key := range []string{"end", "enter", "pgdown", "e", "d", "x", "c", "v"} {
			if got := press(t, m, key); got.state != ViewList {
				t.Errorf("gemerkt=%v: Taste %q nach dem Löschen wechselt nach %v", remembered, key, got.state)
			}
		}
		// Neustart auf der Konfiguration mit der verwaisten ID: kein Absturz
		again, _ := restartModel(t, m)
		if again.cursor != 0 || again.errorMsg != "" {
			t.Errorf("gemerkt=%v: Neustart: Cursor %d, Fehler %q", remembered, again.cursor, again.errorMsg)
		}
	}
}

// --- Liste: Verbindung merken (L5-L11) ---

// TestRememberEnterSavesAndRestores (L5): Bei eingeschaltetem Schalter
// speichert Enter auf einer Verbindung deren ID; ein neues Modell auf
// derselben Konfiguration steht auf dieser Verbindung. Auch über den
// Schalter selbst (v) eingeschaltet - nicht nur über die Konfiguration.
func TestRememberEnterSavesAndRestores(t *testing.T) {
	m, _ := newRememberModel(t, true, "", "", "", "A", "B", "C")
	m = press(t, m, "down", "enter")
	if m.state != ViewConnecting {
		t.Fatalf("Enter auf B: state %v, erwartet ViewConnecting", m.state)
	}
	if got := loadTestConfig(t, m).LastConnectionID; got != "id-1" {
		t.Fatalf("LastConnectionID = %q, erwartet id-1", got)
	}
	again, _ := restartModel(t, m)
	if currentName(again) != "B" || again.cursor != 1 {
		t.Errorf("neues Modell: Verbindung %q, Cursor %d, erwartet B auf 1", currentName(again), again.cursor)
	}

	// Den Schalter im laufenden Programm einschalten (Taste v), dann wählen
	fresh := newListTestModel(t, "A", "B", "C")
	fresh = press(t, fresh, "v", "down", "down", "enter")
	if got := loadTestConfig(t, fresh).LastConnectionID; got != "id-2" {
		t.Errorf("nach v und Enter auf C: LastConnectionID = %q, erwartet id-2", got)
	}
}

// TestRememberOffSavesNothing (L5b): Bei ausgeschaltetem Schalter wird nichts
// gespeichert, und eine (z. B. von Hand eingetragene) ID in der Konfiguration
// wird beim Start nicht angewendet: Cursor 0.
func TestRememberOffSavesNothing(t *testing.T) {
	m, _ := newRememberModel(t, false, "", "", "", "A", "B")
	before := configBytes(t, m.configPath)
	past := backdateConfig(t, m.configPath)
	m = press(t, m, "down", "enter")
	if m.state != ViewConnecting {
		t.Fatalf("Enter auf B: state %v", m.state)
	}
	assertConfigUntouched(t, m.configPath, before, past, "Enter bei ausgeschaltetem Merken")
	again, _ := restartModel(t, m)
	if again.cursor != 0 {
		t.Errorf("Neustart: Cursor %d, erwartet 0", again.cursor)
	}

	// Handeingetragene ID bei ausgeschaltetem Schalter: ignoriert
	manual, _ := newRememberModel(t, false, "id-1", "id-1", "none", "A", "B")
	if manual.cursor != 0 || currentName(manual) != "A" {
		t.Errorf("Schalter aus, ID in der Datei: Cursor %d auf %q, erwartet 0 auf A", manual.cursor, currentName(manual))
	}
}

// TestRememberSavesOnlyOnChange: Dieselbe Verbindung noch einmal zu wählen
// schreibt die Konfiguration nicht erneut ("nur bei Änderung"). Enter auf
// einer Gruppe oder dem Datei-Manager ist keine Verbindungswahl und speichert
// ebenfalls nichts.
func TestRememberSavesOnlyOnChange(t *testing.T) {
	m, _ := newRememberModel(t, true, "id-2", "", "", "Grp:A", "Grp:B", "Solo")
	if currentName(m) != "Solo" {
		t.Fatalf("Vorbedingung: gemerkt ist Solo, Cursor auf %q", currentName(m))
	}
	before := configBytes(t, m.configPath)
	past := backdateConfig(t, m.configPath)
	// Enter auf Solo = dieselbe Verbindung wie gemerkt
	press(t, m, "enter")
	assertConfigUntouched(t, m.configPath, before, past, "Enter auf die schon gemerkte Verbindung")
	// Enter auf die Gruppe (klappt nur auf) und auf den Datei-Manager
	group := press(t, m, "home", "enter")
	if group.state != ViewList {
		t.Errorf("Enter auf Gruppe: state %v", group.state)
	}
	assertConfigUntouched(t, m.configPath, before, past, "Enter auf eine Gruppe")
	fm := press(t, m, "end", "enter")
	if fm.state != ViewFMPick {
		t.Errorf("Enter auf Datei-Manager: state %v", fm.state)
	}
	assertConfigUntouched(t, m.configPath, before, past, "Enter auf den Datei-Manager")
}

// TestRememberGroupMemberRestoredWithGroupOpen (L6): Ist ein Gruppenmitglied
// gemerkt, ist beim Start genau diese Gruppe aufgeklappt und der Cursor
// steht auf dem Mitglied; andere Gruppen bleiben zu.
func TestRememberGroupMemberRestoredWithGroupOpen(t *testing.T) {
	m, _ := newRememberModel(t, true, "id-3", "", "", "Abc:1", "Abc:2", "Zeta:1", "Zeta:2")
	if currentName(m) != "Zeta:2" {
		t.Fatalf("Cursor auf %q, erwartet Zeta:2", currentName(m))
	}
	if !m.expandedGroups["zeta"] || m.expandedGroups["abc"] || len(m.expandedGroups) != 1 {
		t.Errorf("aufgeklappte Gruppen %v, erwartet nur zeta", m.expandedGroups)
	}
	text := stripANSI(renderListText(m))
	if !strings.Contains(text, "▼ Zeta (2)") || !strings.Contains(text, "► Abc (2)") || !strings.Contains(text, "└ 2") {
		t.Errorf("Anzeige zeigt nicht Zeta offen und Abc zu:\n%s", text)
	}
	// Das Mitglied steht als markierte Zeile in der Anzeige
	withColorProfile(t)
	if line := selectedLine(t, renderListText(m), "└ 2"); !strings.Contains(line, "48;5;57") {
		t.Errorf("Mitglied ist nicht als Cursorzeile markiert: %q", line)
	}
}

// TestRememberUnknownIDIgnored (L7): Eine unbekannte oder gelöschte ID, ein
// leerer Wert und manipulierte Werte (Escape-Sequenz, 10.000 Zeichen, Pfad,
// NUL) ergeben Cursor 0, keine Meldung und keine aufgeklappte Gruppe.
func TestRememberUnknownIDIgnored(t *testing.T) {
	evil := []string{
		"",
		"id-99",
		"\x1b[31mrot\x1b[0m",
		"\x1b]0;pwned\x07",
		strings.Repeat("A", 10000),
		"../../etc/passwd",
		"id-0\x00",
		"none",
		"local-pc",
	}
	for _, id := range evil {
		m, _ := newRememberModel(t, true, id, "", "", "Grp:A", "Grp:B", "Solo")
		if m.cursor != 0 {
			t.Errorf("ID %.20q: Cursor %d, erwartet 0", id, m.cursor)
		}
		if m.errorMsg != "" || m.successMsg != "" {
			t.Errorf("ID %.20q: Meldung %q / %q, erwartet keine", id, m.errorMsg, m.successMsg)
		}
		if len(m.expandedGroups) != 0 {
			t.Errorf("ID %.20q: Gruppen aufgeklappt: %v", id, m.expandedGroups)
		}
		raw := renderListText(m)
		if strings.Contains(raw, "\x1b]0;") || strings.Contains(raw, "AAAAAAAAAA") {
			t.Errorf("ID %.20q: gemerkter Wert erscheint in der Anzeige", id)
		}
	}

	// Verbindung nach dem Merken gelöscht: ID verwaist, Cursor 0, keine Meldung
	m, _ := newRememberModel(t, true, "id-1", "", "", "A", "B")
	if err := DeleteConnection(m.configPath, "id-1"); err != nil {
		t.Fatal(err)
	}
	again, _ := restartModel(t, m)
	if again.cursor != 0 || again.errorMsg != "" || again.successMsg != "" {
		t.Errorf("gelöschte Verbindung: Cursor %d, Meldung %q/%q", again.cursor, again.errorMsg, again.successMsg)
	}
}

// TestRememberSelectionVisibleInSmallWindow (L8): 40 Verbindungen, die
// gemerkte ist Nr. 35, Terminalhöhe 24: Sie ist sichtbar (Scroll-Fenster
// folgt), markiert, und die Liste scrollt von dort weiter in beide Richtungen.
func TestRememberSelectionVisibleInSmallWindow(t *testing.T) {
	withColorProfile(t)
	for _, height := range []int{24, 17, 40, 12} {
		m, _ := newRememberModel(t, true, "id-34", "", "", namesN(40)...)
		m = resizeModel(t, m, 100, height)
		if currentName(m) != "Host35" || m.cursor != 34 {
			t.Fatalf("Höhe %d: Cursor %d auf %q, erwartet Host35 auf 34", height, m.cursor, currentName(m))
		}
		limit := visibleRowLimit(height)
		if m.listOffset > m.cursor || m.cursor >= m.listOffset+limit {
			t.Errorf("Höhe %d: Cursor %d außerhalb des Fensters %d..%d", height, m.cursor, m.listOffset, m.listOffset+limit)
		}
		raw := renderListText(m)
		if line := selectedLine(t, raw, "Host35"); !strings.Contains(line, "48;5;57") {
			t.Errorf("Höhe %d: Host35 sichtbar, aber nicht markiert: %q", height, line)
		}
		if !strings.Contains(stripANSI(raw), "↑ +") {
			t.Errorf("Höhe %d: Hinweis auf verborgene Zeilen oben fehlt", height)
		}
	}
}

// TestListNavigationNeverSavesConfig (L9): Bloße Cursorbewegung (hoch, runter,
// Ende, Anfang, Bild, Gruppe auf/zu, Menü) speichert NIE - die Datei bleibt
// Byte für Byte gleich und wird nicht neu geschrieben.
func TestListNavigationNeverSavesConfig(t *testing.T) {
	m, _ := newRememberModel(t, true, "id-1", "id-0", "id-1", "Grp:A", "Grp:B", "Solo")
	before := configBytes(t, m.configPath)
	past := backdateConfig(t, m.configPath)
	keys := []string{"up", "down", "end", "home", "pgdown", "pgup", "right", "left", "j", "k", "m", "m", "down", "down", "up"}
	for _, key := range keys {
		m = press(t, m, key)
		assertConfigUntouched(t, m.configPath, before, past, "Taste "+key)
	}
	// Auch eine bereits geöffnete Gruppe und die Wahl des Datei-Managers
	// (nur Zeile, noch keine Auswahl) ändern nichts
	m = press(t, m, "home", "enter", "end")
	assertConfigUntouched(t, m.configPath, before, past, "Gruppe öffnen und Ende")
}

// TestRememberSaveKeepsConcurrentKeyPath (L10): Hat die Schlüsselvergabe im
// Hintergrund nach dem Modellstart einen KeyPath gespeichert
// (UpdateConnection), überschreibt das Speichern der Auswahl ihn nicht - es
// lädt die Datei frisch und ändert nur sein Feld (updateConfig).
func TestRememberSaveKeepsConcurrentKeyPath(t *testing.T) {
	m, _ := newRememberModel(t, true, "", "", "", "A", "B")
	conn := connByName(t, m, "B")
	conn.AuthType = AuthKey
	conn.KeyPath = "/home/x/.ssh/id_ed25519_b"
	if err := UpdateConnection(m.configPath, conn); err != nil {
		t.Fatalf("UpdateConnection: %v", err)
	}
	m = press(t, m, "down", "enter")
	cfg := loadTestConfig(t, m)
	if got := testConnByID(t, cfg, "id-1"); got.KeyPath != conn.KeyPath || got.AuthType != AuthKey {
		t.Errorf("KeyPath/Auth nach dem Speichern der Auswahl: %q / %v, erwartet %q / key", got.KeyPath, got.AuthType, conn.KeyPath)
	}
	if cfg.LastConnectionID != "id-1" {
		t.Errorf("LastConnectionID = %q, erwartet id-1", cfg.LastConnectionID)
	}
	if len(cfg.Connections) != 2 {
		t.Errorf("%d Verbindungen, erwartet 2", len(cfg.Connections))
	}
}

// TestRememberFieldsJSONNames: Die vier neuen Felder heißen in der Datei
// remember_selection, last_connection_id, file_manager_last_left und
// file_manager_last_right und fehlen, solange sie leer sind (omitempty) -
// alte Konfigurationen bleiben unverändert lesbar.
func TestRememberFieldsJSONNames(t *testing.T) {
	cfg := NewAppConfig()
	empty, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RememberSelection = true
	cfg.LastConnectionID = "a"
	cfg.FileManagerLastLeft = rememberLocalPC
	cfg.FileManagerLastRight = rememberNone
	full, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"remember_selection", "last_connection_id", "file_manager_last_left", "file_manager_last_right"} {
		if strings.Contains(string(empty), key) {
			t.Errorf("leere Konfiguration enthält %q", key)
		}
		if !strings.Contains(string(full), `"`+key+`"`) {
			t.Errorf("gefüllte Konfiguration enthält %q nicht: %s", key, full)
		}
	}
	if rememberLocalPC != "local-pc" || rememberNone != "none" {
		t.Errorf("Konstanten %q / %q, erwartet local-pc / none", rememberLocalPC, rememberNone)
	}
}

// TestRememberFieldsNotExportedNorImported (L11): Der Export enthält keinen
// der vier Schlüssel (sie gehören zu diesem Rechner); ein Import ändert sie
// nicht - auch nicht, wenn die Importdatei sie enthält (Datei ist
// nicht vertrauenswürdig).
func TestRememberFieldsNotExportedNorImported(t *testing.T) {
	m, _ := newRememberModel(t, true, "id-1", "id-0", "id-1", "A", "B")
	m.exportHomeDir = t.TempDir()
	m = press(t, m, "E")
	if m.state != ViewExport {
		t.Fatalf("E: state %v", m.state)
	}
	m = press(t, m, "enter")
	entries, _ := os.ReadDir(m.exportHomeDir)
	if len(entries) != 1 {
		t.Fatalf("erwartet genau eine Exportdatei, gefunden: %v", entries)
	}
	exported, err := os.ReadFile(filepath.Join(m.exportHomeDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"remember_selection", "last_connection_id", "file_manager_last_left", "file_manager_last_right"} {
		if strings.Contains(string(exported), key) {
			t.Errorf("Export enthält %q:\n%s", key, exported)
		}
	}

	// Import: Datei mit eingeschmuggelten Werten
	evil := NewAppConfig()
	evil.RememberSelection = false
	evil.LastConnectionID = "boese"
	evil.FileManagerLastLeft = "boese-links"
	evil.FileManagerLastRight = "boese-rechts"
	neu := importConn("Neu", "neu.lan")
	evil.Connections = []Connection{neu}
	data, _ := json.MarshalIndent(evil, "", "  ")
	file := filepath.Join(t.TempDir(), "evil.json")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	conns, _, err := readImportFile(file)
	if err != nil {
		t.Fatalf("readImportFile: %v", err)
	}
	res, err := applyImport(m.configPath, conns, map[string]bool{conns[0].ID: true})
	if err != nil || res.added != 1 {
		t.Fatalf("applyImport: %+v, %v", res, err)
	}
	cfg := loadTestConfig(t, m)
	if !cfg.RememberSelection || cfg.LastConnectionID != "id-1" || cfg.FileManagerLastLeft != "id-0" || cfg.FileManagerLastRight != "id-1" {
		t.Errorf("Import hat die gemerkte Auswahl verändert: %v %q %q %q",
			cfg.RememberSelection, cfg.LastConnectionID, cfg.FileManagerLastLeft, cfg.FileManagerLastRight)
	}
}

// --- Schalter "Verbindung merken" (S1-S5) ---

// TestRememberToggleKeySavesAtOnce (S1): v schaltet den Schalter um und
// speichert sofort. Schalten allein sichert keine Auswahl und lässt alles
// andere in der Konfiguration (Verbindungen, KeyPath, Größen, gemerkte
// Verzeichnisse) unberührt.
func TestRememberToggleKeySavesAtOnce(t *testing.T) {
	m := newListTestModel(t, "A", "B")
	conn := connByName(t, m, "A")
	conn.AuthType, conn.KeyPath = AuthKey, "/home/x/.ssh/id_a"
	if err := UpdateConnection(m.configPath, conn); err != nil {
		t.Fatal(err)
	}
	err := updateConfig(m.configPath, func(cfg *AppConfig) bool {
		cfg.FontSize = 22
		cfg.FileManagerLastPaths = map[string]string{"id-0": "/srv"}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	m, _ = restartModel(t, m)
	if loadTestConfig(t, m).RememberSelection {
		t.Fatal("Standard muss AUS sein")
	}
	m = press(t, m, "v")
	cfg := loadTestConfig(t, m)
	if !cfg.RememberSelection {
		t.Fatal("v schaltet nicht ein")
	}
	if cfg.LastConnectionID != "" || cfg.FileManagerLastLeft != "" || cfg.FileManagerLastRight != "" {
		t.Errorf("Einschalten hat Werte gespeichert: %q %q %q", cfg.LastConnectionID, cfg.FileManagerLastLeft, cfg.FileManagerLastRight)
	}
	if got := testConnByID(t, cfg, "id-0"); got.KeyPath != "/home/x/.ssh/id_a" || len(cfg.Connections) != 2 || cfg.FontSize != 22 || cfg.FileManagerLastPaths["id-0"] != "/srv" {
		t.Errorf("andere Einstellungen verändert: KeyPath %q, %d Verbindungen, Schrift %d, Pfade %v", got.KeyPath, len(cfg.Connections), cfg.FontSize, cfg.FileManagerLastPaths)
	}
	// Ein neuer Start liest den Schalter
	again, _ := restartModel(t, m)
	again = press(t, again, "m")
	if text := stripANSI(renderListText(again)); !strings.Contains(text, strings.TrimSpace(again.tr(txtRememberOn))) {
		t.Errorf("nach Neustart steht der Schalter nicht auf an:\n%s", text)
	}
	m = press(t, m, "v")
	if loadTestConfig(t, m).RememberSelection {
		t.Error("zweites v schaltet nicht aus")
	}
}

// TestRememberToggleOffClearsStoredValues (S2): Beim Ausschalten werden
// LastConnectionID, FileManagerLastLeft und FileManagerLastRight gelöscht -
// sonst käme beim Wiedereinschalten ein alter Stand zurück. Nach erneutem
// Einschalten startet der Cursor oben.
func TestRememberToggleOffClearsStoredValues(t *testing.T) {
	m, _ := newRememberModel(t, true, "id-1", "id-0", rememberNone, "A", "B")
	if currentName(m) != "B" {
		t.Fatalf("Vorbedingung: Cursor auf %q, erwartet B", currentName(m))
	}
	m = press(t, m, "v")
	cfg := loadTestConfig(t, m)
	if cfg.RememberSelection || cfg.LastConnectionID != "" || cfg.FileManagerLastLeft != "" || cfg.FileManagerLastRight != "" {
		t.Errorf("nach dem Ausschalten: %v %q %q %q", cfg.RememberSelection, cfg.LastConnectionID, cfg.FileManagerLastLeft, cfg.FileManagerLastRight)
	}
	if len(cfg.Connections) != 2 {
		t.Errorf("%d Verbindungen, erwartet 2", len(cfg.Connections))
	}
	m = press(t, m, "v")
	cfg = loadTestConfig(t, m)
	if !cfg.RememberSelection || cfg.LastConnectionID != "" || cfg.FileManagerLastLeft != "" || cfg.FileManagerLastRight != "" {
		t.Errorf("nach dem Wiedereinschalten: %v %q %q %q", cfg.RememberSelection, cfg.LastConnectionID, cfg.FileManagerLastLeft, cfg.FileManagerLastRight)
	}
	again, _ := restartModel(t, m)
	if again.cursor != 0 {
		t.Errorf("Neustart nach Aus/An: Cursor %d, erwartet 0 (alter Stand darf nicht zurückkommen)", again.cursor)
	}
	// Die Taste v ändert die Cursorposition der laufenden Sitzung nicht
	m2, _ := newRememberModel(t, true, "id-1", "", "", "A", "B")
	if got := press(t, m2, "v"); got.cursor != 1 || got.state != ViewList {
		t.Errorf("v: Cursor %d, state %v, erwartet 1/ViewList", got.cursor, got.state)
	}
}

// TestRememberMenuLine (S3): Die Menüzeile txtRememberOn/txtRememberOff steht
// nur bei eingeblendetem Menü (m) direkt vor der Auto-Update-Zeile; ohne Menü
// fehlt sie. Der Text folgt dem Schalter.
func TestRememberMenuLine(t *testing.T) {
	m := newListTestModel(t, "A")
	on := strings.TrimSpace(m.tr(txtRememberOn))
	off := strings.TrimSpace(m.tr(txtRememberOff))
	hidden := stripANSI(renderListText(m))
	if strings.Contains(hidden, on) || strings.Contains(hidden, off) {
		t.Errorf("Menüzeile ohne eingeblendetes Menü sichtbar:\n%s", hidden)
	}
	m = press(t, m, "m")
	for _, wantOn := range []bool{false, true} {
		if wantOn {
			m = press(t, m, "v")
		}
		text := stripANSI(renderListText(m))
		want, other := off, on
		if wantOn {
			want, other = on, off
		}
		if !strings.Contains(text, want) || strings.Contains(text, other) {
			t.Fatalf("Schalter an=%v: erwartet %q, nicht %q:\n%s", wantOn, want, other, text)
		}
		lines := strings.Split(strings.TrimRight(text, "\n "), "\n")
		at := -1
		for i, line := range lines {
			if strings.Contains(line, want) {
				at = i
			}
		}
		if at < 0 || at+1 >= len(lines) || !strings.Contains(lines[at+1], "Auto-Update") {
			t.Errorf("Schalter an=%v: Zeile nach %q ist nicht die Auto-Update-Zeile:\n%s", wantOn, want, text)
		}
		if !strings.Contains(lines[len(lines)-1], "Auto-Update") {
			t.Errorf("Schalter an=%v: letzte Zeile ist nicht mehr der Auto-Update-Schalter:\n%s", wantOn, text)
		}
	}
	// Menü wieder aus: Zeile weg, Schalter bleibt aber an
	m = press(t, m, "m")
	if text := stripANSI(renderListText(m)); strings.Contains(text, on) {
		t.Errorf("Menüzeile nach dem Ausblenden noch sichtbar:\n%s", text)
	}
}

// TestListLayoutFitsWithRememberLine (S4): Mit der zusätzlichen Menüzeile
// passt die Liste weiter in das Terminal (listChromeLines): Bei eingeblendetem
// Menü, Gruppenhilfe, beiden Bildlauf-Hinweisen und einer Meldung ist die
// Ausgabe nie höher als das Terminal. Unter 17 Zeilen greift die Mindest-
// Zeilenzahl der Liste und das Menü passt prinzipiell nicht mehr.
func TestListLayoutFitsWithRememberLine(t *testing.T) {
	names := append(namesN(37), "Grp:A", "Grp:B")
	for _, height := range []int{17, 18, 20, 24, 30, 40, 60} {
		for _, msg := range []string{"", "ok", "fehler"} {
			m, _ := newRememberModel(t, true, "id-20", "", "", names...)
			m = resizeModel(t, m, 100, height)
			m = press(t, m, "m")
			switch msg {
			case "ok":
				m.successMsg = "gespeichert"
			case "fehler":
				m.errorMsg = "kaputt"
			}
			text := m.View()
			if !strings.Contains(stripANSI(text), strings.TrimSpace(m.tr(txtRememberOn))) {
				t.Fatalf("Höhe %d: Menüzeile \"Verbindung merken\" fehlt:\n%s", height, stripANSI(text))
			}
			if lines := strings.Count(text, "\n") + 1; lines > height {
				t.Errorf("Höhe %d, Meldung %q: %d Zeilen, erwartet höchstens %d (listChromeLines zu klein?):\n%s", height, msg, lines, height, stripANSI(text))
			}
		}
	}
}

// TestRememberToggleWorksWithMenuHidden (S5): v wirkt auch bei
// ausgeblendetem Menü (wie alle Tasten der Liste). In Eingabefeldern ist
// v dagegen ein normaler Buchstabe und schaltet nichts um.
func TestRememberToggleWorksWithMenuHidden(t *testing.T) {
	m := newListTestModel(t, "A")
	if m.showMenu {
		t.Fatal("Menü muss beim Start ausgeblendet sein")
	}
	m = press(t, m, "v")
	if !loadTestConfig(t, m).RememberSelection {
		t.Error("v bei ausgeblendetem Menü schaltet nicht ein")
	}
	// Im Formular (neue Verbindung) ist v ein Buchstabe
	form := press(t, m, "n")
	form = typeText(t, form, "vvv")
	if form.state != ViewCreate {
		t.Fatalf("n: state %v", form.state)
	}
	if got := form.inputs[fieldName].Value(); got != "vvv" {
		t.Errorf("Namensfeld = %q, erwartet vvv", got)
	}
	if !loadTestConfig(t, form).RememberSelection {
		t.Error("v im Formular hat den Schalter umgeschaltet")
	}
	// Im Lösch-Dialog tut v nichts
	del := press(t, m, "d", "v")
	if del.state != ViewDelete || !loadTestConfig(t, del).RememberSelection {
		t.Errorf("v im Lösch-Dialog: state %v", del.state)
	}
}

// --- Zurücksetzen mit r (R1-R4) ---

// resetFixture baut ein Modell mit gemerkter Auswahl, Fenster-/Schriftwerten
// und gemerkten Verzeichnissen. Die Konsole ist steuerbar, wenn
// controllable gesetzt ist (Schrift 16 als Systemstandard, Fenster 120x30).
func resetFixture(t *testing.T, controllable bool, remember bool) (AppModel, *[]int, *[][2]int) {
	t.Helper()
	m := newListTestModel(t, "A", "B")
	setRememberConfig(t, m.configPath, remember, "id-1", "id-0", "id-1")
	err := updateConfig(m.configPath, func(cfg *AppConfig) bool {
		cfg.FontSize, cfg.WindowCols, cfg.WindowRows = 28, 200, 60
		cfg.FileManagerLastPaths = map[string]string{"id-0": "/srv", "local-pc": "/tmp"}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	m, _ = restartModel(t, m)
	if controllable {
		fonts := stubConsoleFont(t, 28)
		windows := stubSetWindow(t)
		stubWindowState(t, 200, 60, true)
		m.consoleDefaults = consoleDefaults{fontSize: 16, cols: 120, rows: 30}
		return m, fonts, windows
	}
	fonts := stubConsoleFont(t, 0)
	windows := stubSetWindow(t)
	stubWindowState(t, 0, 0, false)
	m.consoleDefaults = consoleDefaults{}
	return m, fonts, windows
}

// TestResetClearsRememberedSelectionAndWindow (R1): Mit steuerbarer Konsole
// setzt r Fenster und Schrift zurück UND löscht die drei gemerkten Werte.
// Verbindungen, Schalter und die gemerkten Verzeichnisse (Build 77) bleiben.
// Die Meldung ist txtConsoleResetAll.
func TestResetClearsRememberedSelectionAndWindow(t *testing.T) {
	m, fonts, windows := resetFixture(t, true, true)
	m = press(t, m, "r")
	if len(*fonts) != 1 || (*fonts)[0] != 16 || len(*windows) != 1 || (*windows)[0] != [2]int{120, 30} {
		t.Errorf("Schrift %v, Fenster %v, erwartet [16] und [[120 30]]", *fonts, *windows)
	}
	cfg := loadTestConfig(t, m)
	if cfg.FontSize != 0 || cfg.WindowCols != 0 || cfg.WindowRows != 0 {
		t.Errorf("Größen nicht gelöscht: %d, %dx%d", cfg.FontSize, cfg.WindowCols, cfg.WindowRows)
	}
	if cfg.LastConnectionID != "" || cfg.FileManagerLastLeft != "" || cfg.FileManagerLastRight != "" {
		t.Errorf("gemerkte Auswahl nicht gelöscht: %q %q %q", cfg.LastConnectionID, cfg.FileManagerLastLeft, cfg.FileManagerLastRight)
	}
	if !cfg.RememberSelection {
		t.Error("r darf den Schalter nicht ausschalten")
	}
	if len(cfg.Connections) != 2 || cfg.FileManagerLastPaths["id-0"] != "/srv" || cfg.FileManagerLastPaths["local-pc"] != "/tmp" {
		t.Errorf("Verbindungen/Verzeichnisse verändert: %d, %v", len(cfg.Connections), cfg.FileManagerLastPaths)
	}
	if m.successMsg != m.tr(txtConsoleResetAll) || m.errorMsg != "" {
		t.Errorf("Meldung = %q / Fehler %q, erwartet %q", m.successMsg, m.errorMsg, m.tr(txtConsoleResetAll))
	}
}

// TestResetWithoutConsoleControlStillClearsSelection (R2): Ist die Konsole
// nicht steuerbar (Linux, Windows Terminal), bleiben Schrift und Fenster in
// der Konfiguration stehen wie bisher, die gemerkte Auswahl wird trotzdem
// gelöscht. Die Meldung ist ein Erfolg (txtSelectionResetOnly), kein Fehler.
// Das gilt auch bei ausgeschaltetem Schalter (von Hand eingetragene Werte).
func TestResetWithoutConsoleControlStillClearsSelection(t *testing.T) {
	for _, remember := range []bool{true, false} {
		m, fonts, windows := resetFixture(t, false, remember)
		m = press(t, m, "r")
		if len(*fonts) != 0 || len(*windows) != 0 {
			t.Errorf("ohne Steuerung nichts setzen: Schrift %v, Fenster %v", *fonts, *windows)
		}
		cfg := loadTestConfig(t, m)
		if cfg.FontSize != 28 || cfg.WindowCols != 200 || cfg.WindowRows != 60 {
			t.Errorf("Größen verändert: %d, %dx%d, erwartet 28, 200x60", cfg.FontSize, cfg.WindowCols, cfg.WindowRows)
		}
		if cfg.LastConnectionID != "" || cfg.FileManagerLastLeft != "" || cfg.FileManagerLastRight != "" {
			t.Errorf("Schalter %v: gemerkte Auswahl nicht gelöscht: %q %q %q", remember, cfg.LastConnectionID, cfg.FileManagerLastLeft, cfg.FileManagerLastRight)
		}
		if cfg.RememberSelection != remember || len(cfg.Connections) != 2 || cfg.FileManagerLastPaths["id-0"] != "/srv" {
			t.Errorf("Schalter %v: Schalter/Verbindungen/Verzeichnisse verändert: %v, %d, %v", remember, cfg.RememberSelection, len(cfg.Connections), cfg.FileManagerLastPaths)
		}
		if m.successMsg != m.tr(txtSelectionResetOnly) || m.errorMsg != "" {
			t.Errorf("Schalter %v: Meldung = %q / Fehler %q, erwartet Erfolg %q", remember, m.successMsg, m.errorMsg, m.tr(txtSelectionResetOnly))
		}
	}
}

// TestResetThenRestartStartsOnTop (R3): Nach r startet ein neues Modell mit
// Cursor 0, und der Datei-Manager beginnt links wie rechts auf Zeile 0.
func TestResetThenRestartStartsOnTop(t *testing.T) {
	for _, controllable := range []bool{true, false} {
		m, _, _ := resetFixture(t, controllable, true)
		// Vor dem Zurücksetzen steht der Neustart auf der gemerkten Verbindung
		if before, _ := restartModel(t, m); before.cursor != 1 {
			t.Fatalf("Vorbedingung: Cursor %d, erwartet 1 (gemerkte Verbindung B)", before.cursor)
		}
		m = press(t, m, "r")
		again, _ := restartModel(t, m)
		if again.cursor != 0 || currentName(again) != "A" {
			t.Errorf("steuerbar=%v: nach r Neustart auf %q (Cursor %d), erwartet A (0)", controllable, currentName(again), again.cursor)
		}
		again = press(t, again, "end", "enter")
		if again.state != ViewFMPick || again.fm.pickCursor != 0 {
			t.Fatalf("steuerbar=%v: Auswahl links: state %v, Cursor %d", controllable, again.state, again.fm.pickCursor)
		}
		again = press(t, again, "enter")
		if again.fm.pickSide != 1 || again.fm.pickCursor != 0 {
			t.Errorf("steuerbar=%v: rechts: Seite %d, Cursor %d, erwartet 1/0", controllable, again.fm.pickSide, again.fm.pickCursor)
		}
	}
}

// TestResetKeepsRunningCursor (R4): Im laufenden Programm bleibt der Cursor
// stehen (r springt nicht nach oben) - erst der nächste Start beginnt oben.
// Danach merkt sich der eingeschaltete Schalter eine neue Wahl wieder.
func TestResetKeepsRunningCursor(t *testing.T) {
	m, _, _ := resetFixture(t, true, true)
	if m.cursor != 1 {
		t.Fatalf("Vorbedingung: Cursor %d, erwartet 1", m.cursor)
	}
	m = press(t, m, "r")
	if m.cursor != 1 || m.state != ViewList || currentName(m) != "B" {
		t.Errorf("nach r: Cursor %d auf %q, state %v - erwartet B auf 1 in der Liste", m.cursor, currentName(m), m.state)
	}
	m = press(t, m, "up", "enter")
	if got := loadTestConfig(t, m).LastConnectionID; got != "id-0" {
		t.Errorf("nach r und neuer Wahl: LastConnectionID = %q, erwartet id-0", got)
	}
}
