// Paket main - Tests für das Hauptmenü ab Build 84
//
// Nutzerwunsch 2026-10-27:
//   - "m" blendet die Tastenhilfe unten ein und aus (Standard: aus); alle
//     Tasten wirken auch bei ausgeblendetem Menü.
//   - "c" legt eine neue Verbindung als Kopie der gewählten an: Name (mit
//     hochzählendem "#2"), Port, Benutzer und Tunnel - kein Host, keine Keys.
//   - "r" setzt Fenster- und Schriftgröße auf den Stand beim Programmstart
//     (Systemstandard) zurück.
//   - Fenster- und Schriftgröße werden bei der Änderung gespeichert, auch
//     während einer SSH-Sitzung - nicht erst beim Beenden.
//
// @author Kurt Ingwer
// @date   2026-10-27 17:00
package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// --- Menü ein-/ausblenden (m) ---

// TestListMenuHiddenByDefault prüft, dass die Tastenhilfe beim Start
// ausgeblendet ist und nur der Hinweis auf "m" erscheint.
//
// @date 2026-10-27 17:00
func TestListMenuHiddenByDefault(t *testing.T) {
	m := newListTestModel(t, "A")
	text := stripANSI(renderListText(m))
	if strings.Contains(text, strings.TrimSpace(m.lang.HelpList)) {
		t.Error("Tastenhilfe darf ohne m nicht angezeigt werden")
	}
	if strings.Contains(text, "Auto-Update") {
		t.Error("Auto-Update-Schalter gehört ins Menü und ist ohne m ausgeblendet")
	}
	if strings.Contains(text, strings.TrimSpace(m.tr(txtHelpMenuMore))) {
		t.Error("Zusatzzeile des Menüs darf ohne m nicht erscheinen")
	}
	lines := strings.Split(strings.TrimRight(text, "\n "), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != strings.TrimSpace(m.tr(txtMenuHint)) {
		t.Errorf("letzte Zeile = %q, erwartet den Hinweis %q", last, m.tr(txtMenuHint))
	}
}

// TestListMenuToggle prüft das Ein- und Ausblenden mit m. Das Menü bleibt
// unten, der Auto-Update-Schalter ist weiter die letzte Zeile.
//
// @date 2026-10-27 17:00
func TestListMenuToggle(t *testing.T) {
	m := newListTestModel(t, "A")
	m = press(t, m, "m")
	if !m.showMenu {
		t.Fatal("m blendet das Menü nicht ein")
	}
	text := stripANSI(renderListText(m))
	for _, want := range []string{m.lang.HelpList, m.tr(txtHelpMenuMore), m.tr(txtHelpExport)} {
		if !strings.Contains(text, strings.TrimSpace(want)) {
			t.Errorf("Menü enthält %q nicht", strings.TrimSpace(want))
		}
	}
	if strings.Contains(text, strings.TrimSpace(m.tr(txtMenuHint))+"\n") {
		t.Error("bei eingeblendetem Menü kein zusätzlicher m-Hinweis")
	}
	lines := strings.Split(strings.TrimRight(text, "\n "), "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, "Auto-Update: aus") {
		t.Errorf("letzte Menüzeile = %q, erwartet Auto-Update-Schalter", last)
	}

	m = press(t, m, "m")
	if m.showMenu {
		t.Fatal("zweites m blendet das Menü nicht aus")
	}
	if strings.Contains(stripANSI(renderListText(m)), strings.TrimSpace(m.lang.HelpList)) {
		t.Error("Menü nach dem zweiten m noch sichtbar")
	}
}

// TestListHotkeysWorkWithMenuHidden prüft, dass alle Tasten auch bei
// ausgeblendetem Menü wirken.
//
// @date 2026-10-27 17:00
func TestListHotkeysWorkWithMenuHidden(t *testing.T) {
	cases := map[string]ViewState{
		"n": ViewCreate,
		"c": ViewCreate,
		"e": ViewEdit,
		"d": ViewDelete,
		"l": ViewLanguage,
	}
	for key, want := range cases {
		m := newListTestModel(t, "A")
		if m.showMenu {
			t.Fatal("Menü muss beim Start ausgeblendet sein")
		}
		m = press(t, m, key)
		if m.state != want {
			t.Errorf("Taste %q bei ausgeblendetem Menü: Ansicht %v, erwartet %v", key, m.state, want)
		}
	}
}

// TestListMenuHiddenStillShowsUpdateStatus prüft, dass eine Meldung des
// Auto-Updates (z.B. "Update heruntergeladen") auch ohne Menü sichtbar ist.
// Nur die Tastenhilfe wird ausgeblendet, keine Information.
//
// @date 2026-10-27 17:00
func TestListMenuHiddenStillShowsUpdateStatus(t *testing.T) {
	m := newListTestModel(t, "A")
	m.autoUpdate = true
	m.updateStatus = "Update v0.99 bereit"
	if !strings.Contains(stripANSI(renderListText(m)), "Update v0.99 bereit") {
		t.Error("Update-Meldung fehlt bei ausgeblendetem Menü")
	}
}

// TestListMenuHiddenNotTallerThanShown prüft, dass die Liste ohne Menü nie
// mehr Zeilen braucht als mit Menü (Platzberechnung bleibt gültig).
//
// @date 2026-10-27 17:00
func TestListMenuHiddenNotTallerThanShown(t *testing.T) {
	m := newListTestModel(t, "A", "B", "grp:C")
	hidden := strings.Count(renderListText(m), "\n")
	m = press(t, m, "m")
	shown := strings.Count(renderListText(m), "\n")
	if hidden >= shown {
		t.Errorf("ohne Menü %d Zeilen, mit Menü %d - ausgeblendet muss kürzer sein", hidden, shown)
	}
}

// --- Verbindung kopieren (c) ---

// TestNextCopyName prüft den hochzählenden Namenszusatz " #N".
//
// @date 2026-10-27 17:00
func TestNextCopyName(t *testing.T) {
	cases := []struct {
		name     string
		existing []string
		want     string
	}{
		{"web", []string{"web"}, "web #2"},
		{"web", []string{"web", "web #2"}, "web #3"},
		{"web #2", []string{"web", "web #2"}, "web #3"},
		{"web #2", []string{"web #2"}, "web #3"},
		{"web", []string{"web", "web #3"}, "web #2"},
		{"web", []string{"web", "WEB #2"}, "web #3"},
		{"grp:web", []string{"grp:web"}, "grp:web #2"},
		{"web #1", []string{"web #1"}, "web #1 #2"},
		{"a#2", []string{"a#2"}, "a#2 #2"},
		{"web #10", []string{"web", "web #10"}, "web #2"},
		{"  web  ", []string{"web"}, "web #2"},
	}
	for _, c := range cases {
		var conns []Connection
		for _, n := range c.existing {
			conns = append(conns, Connection{Name: n})
		}
		if got := nextCopyName(c.name, conns); got != c.want {
			t.Errorf("nextCopyName(%q, %v) = %q, erwartet %q", c.name, c.existing, got, c.want)
		}
	}
}

// newCopySourceModel erstellt eine Liste mit einer Verbindung, die Port,
// Benutzer, Tunnel und einen Schlüssel hat.
//
// @param t - Testkontext
// @return AppModel - Modell mit Cursor auf der Verbindung
// @date 2026-10-27 17:00
func newCopySourceModel(t *testing.T) AppModel {
	t.Helper()
	m := newListTestModel(t)
	conn := NewConnection("web", "srv.example.org", 2222, "admin", AuthKey)
	conn.ID = "src-id"
	conn.KeyPath = "/home/x/.ssh/id_web"
	conn.Tunnels = []TunnelConfig{{LocalPort: 8080, RemotePort: 8080}, {LocalPort: 9090, RemotePort: 9090}}
	if err := AddConnection(m.configPath, conn); err != nil {
		t.Fatalf("AddConnection: %v", err)
	}
	m.configCache.Invalidate()
	m.reloadConfig()
	m.selectConnectionByID("src-id")
	return m
}

// TestCopyKeyPrefillsForm prüft das Formular nach c: Name mit #2, Port,
// Benutzer und Tunnel übernommen, Host leer und fokussiert.
//
// @date 2026-10-27 17:00
func TestCopyKeyPrefillsForm(t *testing.T) {
	m := press(t, newCopySourceModel(t), "c")
	if m.state != ViewCreate {
		t.Fatalf("Ansicht = %v, erwartet ViewCreate (neue Verbindung)", m.state)
	}
	want := map[int]string{
		fieldName:    "web #2",
		fieldHost:    "",
		fieldPort:    "2222",
		fieldUser:    "admin",
		fieldTunnels: "8080,9090",
	}
	for field, value := range want {
		if got := m.inputs[field].Value(); got != value {
			t.Errorf("Feld %d = %q, erwartet %q", field, got, value)
		}
	}
	if m.focusedInput != fieldHost {
		t.Errorf("Fokus auf Feld %d, erwartet Host (%d) - der muss eingetragen werden", m.focusedInput, fieldHost)
	}
}

// TestCopySavedAsNewConnection prüft das Speichern der Kopie: neue ID,
// kein Schlüssel, Quelle unverändert.
//
// @date 2026-10-27 17:00
func TestCopySavedAsNewConnection(t *testing.T) {
	m := press(t, newCopySourceModel(t), "c")
	m = typeText(t, m, "srv2.example.org")
	for m.focusedInput != fieldCount-1 {
		m = press(t, m, "tab")
	}
	m = press(t, m, "enter")
	if m.state != ViewList {
		t.Fatalf("nach dem Speichern Ansicht %v, Fehler %q", m.state, m.errorMsg)
	}
	cfg, err := LoadConfig(m.configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Connections) != 2 {
		t.Fatalf("%d Verbindungen, erwartet 2", len(cfg.Connections))
	}
	var src, cpy *Connection
	for i := range cfg.Connections {
		if cfg.Connections[i].ID == "src-id" {
			src = &cfg.Connections[i]
		} else {
			cpy = &cfg.Connections[i]
		}
	}
	if src == nil || cpy == nil {
		t.Fatal("Quelle oder Kopie fehlt")
	}
	if src.Name != "web" || src.Host != "srv.example.org" || src.KeyPath == "" {
		t.Errorf("Quelle verändert: %+v", *src)
	}
	if cpy.Name != "web #2" || cpy.Host != "srv2.example.org" || cpy.Port != 2222 || cpy.User != "admin" {
		t.Errorf("Kopie falsch: %+v", *cpy)
	}
	if len(cpy.Tunnels) != 2 || cpy.Tunnels[0].LocalPort != 8080 || cpy.Tunnels[1].LocalPort != 9090 {
		t.Errorf("Tunnel der Kopie: %+v", cpy.Tunnels)
	}
	if cpy.KeyPath != "" || cpy.AuthType == AuthKey {
		t.Errorf("Kopie darf keinen Schlüssel übernehmen: KeyPath=%q AuthType=%q", cpy.KeyPath, cpy.AuthType)
	}
}

// TestCopyIgnoredOnNonConnectionRows prüft, dass c auf Gruppen- und
// Datei-Manager-Zeilen sowie ohne Verbindungen nichts tut.
//
// @date 2026-10-27 17:00
func TestCopyIgnoredOnNonConnectionRows(t *testing.T) {
	// Keine Verbindungen: Cursor steht auf dem Datei-Manager-Eintrag
	m := press(t, newListTestModel(t), "c")
	if m.state != ViewList {
		t.Errorf("c ohne Verbindungen: Ansicht %v, erwartet ViewList", m.state)
	}
	// Gruppenzeile (zugeklappt)
	m = newListTestModel(t, "grp:A", "grp:B")
	m = press(t, m, "home", "c")
	if m.state != ViewList {
		t.Errorf("c auf Gruppenzeile: Ansicht %v, erwartet ViewList", m.state)
	}
	// Datei-Manager-Eintrag ganz unten
	m = newListTestModel(t, "A")
	m = press(t, m, "end", "c")
	if m.state != ViewList {
		t.Errorf("c auf Datei-Manager-Eintrag: Ansicht %v, erwartet ViewList", m.state)
	}
}

// --- Fenster- und Schriftgröße zurücksetzen (r) ---

// stubConsoleFont ersetzt Abfrage und Setzen der Schriftgröße für die
// Testdauer und protokolliert gesetzte Größen.
//
// @param t - Testkontext
// @param current - Rückgabe der Abfrage
// @return *[]int - gesetzte Größen
// @date 2026-10-27 17:00
func stubConsoleFont(t *testing.T, current int) *[]int {
	t.Helper()
	oldQuery, oldApply := queryFontSize, applyFontSize
	t.Cleanup(func() { queryFontSize, applyFontSize = oldQuery, oldApply })
	set := &[]int{}
	queryFontSize = func() int { return current }
	applyFontSize = func(size int) bool { *set = append(*set, size); return true }
	return set
}

// stubSetWindow protokolliert gesetzte Fenstergrößen.
//
// @param t - Testkontext
// @return *[][2]int - gesetzte Größen
// @date 2026-10-27 17:00
func stubSetWindow(t *testing.T) *[][2]int {
	t.Helper()
	old := setWindowSize
	t.Cleanup(func() { setWindowSize = old })
	got := &[][2]int{}
	setWindowSize = func(cols, rows int) bool { *got = append(*got, [2]int{cols, rows}); return true }
	return got
}

// TestResetConsoleSize prüft r: Schrift und Fenster auf den Startzustand,
// gespeicherte Werte gelöscht, Verbindungen bleiben.
//
// @date 2026-10-27 17:00
func TestResetConsoleSize(t *testing.T) {
	m := newListTestModel(t, "A")
	cfg, _ := LoadConfig(m.configPath)
	cfg.FontSize, cfg.WindowCols, cfg.WindowRows = 28, 200, 60
	if err := SaveConfig(m.configPath, cfg); err != nil {
		t.Fatal(err)
	}
	fonts := stubConsoleFont(t, 28)
	windows := stubSetWindow(t)
	stubWindowState(t, 200, 60, true)
	m.consoleDefaults = consoleDefaults{fontSize: 16, cols: 120, rows: 30}

	m = press(t, m, "r")

	if len(*fonts) != 1 || (*fonts)[0] != 16 {
		t.Errorf("gesetzte Schriftgrößen %v, erwartet [16]", *fonts)
	}
	if len(*windows) != 1 || (*windows)[0] != [2]int{120, 30} {
		t.Errorf("gesetzte Fenstergrößen %v, erwartet [[120 30]]", *windows)
	}
	cfg, _ = LoadConfig(m.configPath)
	if cfg.FontSize != 0 || cfg.WindowCols != 0 || cfg.WindowRows != 0 {
		t.Errorf("gespeicherte Werte nicht gelöscht: Schrift %d, Fenster %dx%d", cfg.FontSize, cfg.WindowCols, cfg.WindowRows)
	}
	if len(cfg.Connections) != 1 {
		t.Errorf("Verbindungen verloren: %d", len(cfg.Connections))
	}
	if m.successMsg != m.tr(txtConsoleReset) || m.errorMsg != "" {
		t.Errorf("Meldung = %q / Fehler %q", m.successMsg, m.errorMsg)
	}
}

// TestResetConsoleSizeUnsupported prüft r ohne steuerbare Konsole (Linux,
// Windows Terminal): Hinweis statt Wirkung, Konfiguration unverändert.
//
// @date 2026-10-27 17:00
func TestResetConsoleSizeUnsupported(t *testing.T) {
	m := newListTestModel(t, "A")
	cfg, _ := LoadConfig(m.configPath)
	cfg.FontSize = 20
	_ = SaveConfig(m.configPath, cfg)
	fonts := stubConsoleFont(t, 0)
	windows := stubSetWindow(t)
	m.consoleDefaults = consoleDefaults{}

	m = press(t, m, "r")

	if len(*fonts) != 0 || len(*windows) != 0 {
		t.Errorf("ohne Steuerung nichts setzen: Schrift %v, Fenster %v", *fonts, *windows)
	}
	if m.errorMsg != m.tr(txtConsoleResetUnsupported) {
		t.Errorf("Fehler = %q, erwartet Hinweis %q", m.errorMsg, m.tr(txtConsoleResetUnsupported))
	}
	if cfg, _ = LoadConfig(m.configPath); cfg.FontSize != 20 {
		t.Errorf("Konfiguration verändert: Schrift %d", cfg.FontSize)
	}
}

// TestResetConsoleSizeFontOnly prüft r, wenn nur die Schrift steuerbar ist
// (Fensterzustand beim Start unbekannt, z.B. maximiert gestartet).
//
// @date 2026-10-27 17:00
func TestResetConsoleSizeFontOnly(t *testing.T) {
	m := newListTestModel(t, "A")
	fonts := stubConsoleFont(t, 24)
	windows := stubSetWindow(t)
	stubWindowState(t, 0, 0, false)
	m.consoleDefaults = consoleDefaults{fontSize: 16}

	m = press(t, m, "r")

	if len(*fonts) != 1 || (*fonts)[0] != 16 {
		t.Errorf("Schriftgrößen %v, erwartet [16]", *fonts)
	}
	if len(*windows) != 0 {
		t.Errorf("unbekannte Fenstergröße darf nicht gesetzt werden: %v", *windows)
	}
	if m.successMsg != m.tr(txtConsoleReset) {
		t.Errorf("Meldung = %q", m.successMsg)
	}
}

// TestCaptureConsoleDefaults prüft das Festhalten des Startzustands, bevor
// gespeicherte Größen angewendet werden.
//
// @date 2026-10-27 17:00
func TestCaptureConsoleDefaults(t *testing.T) {
	stubConsoleFont(t, 16)
	stubWindowState(t, 120, 30, true)
	if got := captureConsoleDefaults(); got != (consoleDefaults{fontSize: 16, cols: 120, rows: 30}) {
		t.Errorf("captureConsoleDefaults = %+v", got)
	}
	// Maximiert gestartet: Fenstergröße nicht als Standard übernehmen
	stubWindowState(t, 240, 70, false)
	if got := captureConsoleDefaults(); got != (consoleDefaults{fontSize: 16}) {
		t.Errorf("maximiert: captureConsoleDefaults = %+v, erwartet nur Schrift", got)
	}
}

// --- Sofort speichern ---

// TestPersistConsoleState prüft das gemeinsame Speichern von Schrift- und
// Fenstergröße; Verbindungen bleiben erhalten.
//
// @date 2026-10-27 17:00
func TestPersistConsoleState(t *testing.T) {
	m := newListTestModel(t, "A")
	stubConsoleFont(t, 18)
	stubWindowState(t, 100, 40, true)
	if !persistConsoleState(m.configPath, consoleDefaults{}) {
		t.Fatal("persistConsoleState meldet nichts gespeichert")
	}
	cfg, _ := LoadConfig(m.configPath)
	if cfg.FontSize != 18 || cfg.WindowCols != 100 || cfg.WindowRows != 40 || len(cfg.Connections) != 1 {
		t.Errorf("Config: Schrift %d, Fenster %dx%d, Verbindungen %d", cfg.FontSize, cfg.WindowCols, cfg.WindowRows, len(cfg.Connections))
	}
	info1, _ := os.Stat(m.configPath)
	// Unverändert: nichts schreiben
	if persistConsoleState(m.configPath, consoleDefaults{}) {
		t.Error("unveränderter Zustand darf nicht erneut gespeichert werden")
	}
	info2, _ := os.Stat(m.configPath)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("Datei wurde trotz unveränderter Werte neu geschrieben")
	}
}

// TestWindowPollSavesFontSize prüft, dass die periodische Abfrage in der
// Oberfläche auch die Schriftgröße sofort speichert (z.B. nach Strg+Mausrad,
// das an der Anwendung vorbeiläuft) - nicht erst beim Beenden.
//
// @date 2026-10-27 17:00
func TestWindowPollSavesFontSize(t *testing.T) {
	m := newListTestModel(t, "A")
	stubConsoleFont(t, 22)
	stubWindowState(t, 0, 0, false)
	updated, _ := m.handleWindowPoll()
	m = updated.(AppModel)
	if cfg, _ := LoadConfig(m.configPath); cfg.FontSize != 22 {
		t.Errorf("Schriftgröße %d gespeichert, erwartet 22", cfg.FontSize)
	}
}

// TestConsoleStateSaverInterval prüft das Speichern während einer
// SSH-Sitzung: höchstens alle consoleSaveInterval, das erste Mal sofort.
//
// @date 2026-10-27 17:00
func TestConsoleStateSaverInterval(t *testing.T) {
	calls := 0
	saver := newConsoleStateSaver(func() { calls++ })
	start := time.Unix(1000, 0)
	saver.tick(start)
	if calls != 1 {
		t.Fatalf("erster Tick: %d Aufrufe, erwartet 1", calls)
	}
	saver.tick(start.Add(consoleSaveInterval / 2))
	if calls != 1 {
		t.Errorf("vor Ablauf des Intervalls gespeichert (%d)", calls)
	}
	saver.tick(start.Add(consoleSaveInterval))
	if calls != 2 {
		t.Errorf("nach Ablauf des Intervalls %d Aufrufe, erwartet 2", calls)
	}
	// Ohne Speicherfunktion (z.B. in Tests ohne Konfiguration) kein Absturz
	newConsoleStateSaver(nil).tick(start)
}

// TestTerminalCmdSavesConsoleState prüft, dass das SSH-Terminal über die
// Fabrikmethode der Oberfläche eine Speicherfunktion für Fenster und Schrift
// mitbekommt (Review-Fund Build 84: vorher setzte der Test sie selbst).
//
// @date 2026-10-27 18:00
func TestTerminalCmdSavesConsoleState(t *testing.T) {
	m := newListTestModel(t, "A")
	stubConsoleFont(t, 26)
	stubWindowState(t, 90, 35, true)
	m.termWidth, m.termHeight = 80, 24
	cmd := m.newTerminalCmd(nil)
	if cmd.width != 80 || cmd.height != 24 {
		t.Errorf("Größe %dx%d, erwartet 80x24", cmd.width, cmd.height)
	}
	if cmd.saveConsoleState == nil {
		t.Fatal("keine Speicherfunktion")
	}
	cmd.saveConsoleState()
	if cfg, _ := LoadConfig(m.configPath); cfg.FontSize != 26 || cfg.WindowCols != 90 {
		t.Errorf("während der Sitzung nicht gespeichert: Schrift %d, Fenster %d", cfg.FontSize, cfg.WindowCols)
	}
}

// TestPersistConsoleStateDefaultsStoreZero prüft: Entspricht die aktuelle
// Größe dem Systemstandard, wird 0 gespeichert ("nichts anwenden") - sonst
// würde die 2-s-Abfrage nach r sofort wieder feste Zahlen speichern, die
// spätere Änderungen am Konsolen-Standard überstimmen (Review-Fund Build 84).
//
// @date 2026-10-27 18:00
func TestPersistConsoleStateDefaultsStoreZero(t *testing.T) {
	m := newListTestModel(t, "A")
	cfg, _ := LoadConfig(m.configPath)
	cfg.FontSize, cfg.WindowCols, cfg.WindowRows = 20, 150, 50
	_ = SaveConfig(m.configPath, cfg)
	stubConsoleFont(t, 16)
	stubWindowState(t, 120, 30, true)
	defaults := consoleDefaults{fontSize: 16, cols: 120, rows: 30}
	if !persistConsoleState(m.configPath, defaults) {
		t.Fatal("Rückkehr zum Standard wurde nicht gespeichert")
	}
	cfg, _ = LoadConfig(m.configPath)
	if cfg.FontSize != 0 || cfg.WindowCols != 0 || cfg.WindowRows != 0 {
		t.Errorf("Standardgröße muss als 0 gespeichert werden: Schrift %d, Fenster %dx%d", cfg.FontSize, cfg.WindowCols, cfg.WindowRows)
	}
	if persistConsoleState(m.configPath, defaults) {
		t.Error("weiterhin Standard: nichts mehr speichern")
	}
}

// TestResetThenPollKeepsDefaults prüft den Ablauf r -> Abfrage alle 2 s:
// Die gespeicherten Werte bleiben 0.
//
// @date 2026-10-27 18:00
func TestResetThenPollKeepsDefaults(t *testing.T) {
	m := newListTestModel(t, "A")
	cfg, _ := LoadConfig(m.configPath)
	cfg.FontSize, cfg.WindowCols, cfg.WindowRows = 28, 200, 60
	_ = SaveConfig(m.configPath, cfg)
	stubConsoleFont(t, 16)
	stubSetWindow(t)
	stubWindowState(t, 120, 30, true)
	m.consoleDefaults = consoleDefaults{fontSize: 16, cols: 120, rows: 30}
	m = press(t, m, "r")
	updated, _ := m.handleWindowPoll()
	m = updated.(AppModel)
	cfg, _ = LoadConfig(m.configPath)
	if cfg.FontSize != 0 || cfg.WindowCols != 0 || cfg.WindowRows != 0 {
		t.Errorf("nach r und Abfrage: Schrift %d, Fenster %dx%d - erwartet 0", cfg.FontSize, cfg.WindowCols, cfg.WindowRows)
	}
}

// TestUpdateConfigSerialized prüft, dass gleichzeitige Lese-Ändern-Schreib-
// Vorgänge (Größen-Watcher der SSH-Sitzung und Schlüssel-Hintergrund-
// befehle) keine Änderung verlieren (Review-Fund Build 84).
//
// @date 2026-10-27 18:00
func TestUpdateConfigSerialized(t *testing.T) {
	m := newListTestModel(t, "A")
	const perWorker = 40
	done := make(chan struct{})
	for w := 0; w < 2; w++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := 0; i < perWorker; i++ {
				if err := updateConfig(m.configPath, func(cfg *AppConfig) bool {
					cfg.FontSize++
					return true
				}); err != nil {
					t.Errorf("updateConfig: %v", err)
					return
				}
			}
		}()
	}
	<-done
	<-done
	cfg, _ := LoadConfig(m.configPath)
	if cfg.FontSize != 2*perWorker {
		t.Errorf("Zähler %d, erwartet %d - Änderungen gingen verloren", cfg.FontSize, 2*perWorker)
	}
	// UpdateConnection gehört zu den gesperrten Schreibern
	conn := m.connections[0]
	conn.User = "neu"
	if err := UpdateConnection(m.configPath, conn); err != nil {
		t.Fatal(err)
	}
	if cfg, _ = LoadConfig(m.configPath); cfg.FontSize != 2*perWorker || cfg.Connections[0].User != "neu" {
		t.Errorf("nach UpdateConnection: Schrift %d, Benutzer %q", cfg.FontSize, cfg.Connections[0].User)
	}
}

// --- Rückfrage vor dem Entfernen des Schlüssels (r in der Statusansicht) ---

// newKeyStatusModel erstellt eine verbundene Verbindung mit Schlüssel in der
// Statusansicht.
//
// @param t - Testkontext
// @return AppModel - Modell in ViewStatus
// @date 2026-10-27 18:00
func newKeyStatusModel(t *testing.T) AppModel {
	t.Helper()
	m := newListTestModel(t, "Server")
	m.connections[0].KeyPath = "/home/x/.ssh/id_server"
	m.connections[0].AuthType = AuthKey
	markConnected(m, m.connections[0], nil)
	m.activeID = m.connections[0].ID
	m.state = ViewStatus
	return m
}

// TestStatusKeyRemoveAsksFirst prüft: r in der Statusansicht entfernt den
// Schlüssel erst nach j/y. Vorher löschte ein Tastendruck den Schlüssel vom
// Server und vom PC - und r heißt in der Liste seit Build 84 "zurücksetzen"
// (Review-Fund, Projektregel: Löschen nur mit j/y).
//
// @date 2026-10-27 18:00
func TestStatusKeyRemoveAsksFirst(t *testing.T) {
	m := newKeyStatusModel(t)
	updated, cmd := m.Update(keyMsgFor("r"))
	m = updated.(AppModel)
	if cmd != nil {
		t.Fatal("r darf den Schlüssel nicht sofort entfernen")
	}
	if !m.confirmKeyRemove {
		t.Fatal("r muss eine Rückfrage öffnen")
	}
	if !strings.Contains(stripANSI(m.View()), "/home/x/.ssh/id_server") {
		t.Error("Rückfrage nennt den Schlüssel nicht")
	}

	// Enter, n und Esc brechen ab
	for _, key := range []string{"enter", "n", "esc"} {
		m = press(t, m, "r")
		updated, cmd = m.Update(keyMsgFor(key))
		m = updated.(AppModel)
		if cmd != nil || m.confirmKeyRemove {
			t.Errorf("Taste %q muss abbrechen (cmd=%v, Rückfrage=%v)", key, cmd != nil, m.confirmKeyRemove)
		}
		if m.state != ViewStatus {
			t.Errorf("Taste %q: Ansicht %v, erwartet ViewStatus", key, m.state)
		}
	}

	// j und y bestätigen
	for _, key := range []string{"j", "y"} {
		m = press(t, m, "r")
		updated, cmd = m.Update(keyMsgFor(key))
		m = updated.(AppModel)
		if cmd == nil || m.confirmKeyRemove {
			t.Errorf("Taste %q muss bestätigen (cmd=%v, Rückfrage=%v)", key, cmd != nil, m.confirmKeyRemove)
		}
	}
}

// TestStatusKeyRemoveWithoutKey prüft r ohne Schlüssel: Hinweis, keine Rückfrage.
//
// @date 2026-10-27 18:00
func TestStatusKeyRemoveWithoutKey(t *testing.T) {
	m := newListTestModel(t, "Server")
	markConnected(m, m.connections[0], nil)
	m.activeID = m.connections[0].ID
	m.state = ViewStatus
	m = press(t, m, "r")
	if m.confirmKeyRemove {
		t.Error("ohne Schlüssel keine Rückfrage")
	}
	if m.errorMsg != m.lang.NoKeyMsg {
		t.Errorf("Fehler = %q, erwartet %q", m.errorMsg, m.lang.NoKeyMsg)
	}
}
