// Paket main - Tests: Datei-Manager mit einer Verbindung (Build 82)
//
// Build 87: Es gibt nur noch EINEN Datei-Manager-Eintrag. Der Ein-Fenster-
// Modus entsteht, wenn rechts "(keine)" gewählt wird (pickSingle). Die
// Listenzeilen und die Auswahl stehen in remember_selection_test.go und
// filemanager_pick_test.go.
//
// Nutzerwunsch 2026-10-26: Option "Datei-Manager (eine Verbindung)", die
// nur lesen und bearbeiten kann; Kopieren und Verschieben entfallen ohne
// zweite Verbindung. Es wird nur EINE Verbindung gewählt und aufgebaut,
// ein Fenster füllt die ganze Breite. F3 (Ansehen), F4 (Bearbeiten), F7,
// F8 und das Menü funktionieren wie gewohnt; F5/F6 melden, dass eine zweite
// Verbindung nötig ist, und fehlen in F-Tasten-Leiste und Datei-Menü.
//
// @author Kurt Ingwer
// @date   2026-10-26 21:04
package main

import (
	"strings"
	"testing"
)

// openSingleManager öffnet den Datei-Manager mit einer Verbindung auf "Server":
// links "Server", rechts "(keine)" (seit Build 87 der einzige Weg dorthin).
func openSingleManager(t *testing.T) (AppModel, *fmConnectRecorder) {
	t.Helper()
	rec := newConnectRecorder()
	return openSingleManagerWith(t, rec.connect), rec
}

// openSingleManagerWith öffnet ihn mit einer eigenen Verbindungsfunktion.
func openSingleManagerWith(t *testing.T, connect fmConnectFunc) AppModel {
	t.Helper()
	m := newListTestModel(t, "Server", "Zweit")
	m.fmConnect = connect
	m.termWidth, m.termHeight = 100, 30
	m = openPick(t, m)
	m = pickSingle(t, m, "Server")
	if m.state != ViewFileManager || !m.fm.single {
		t.Fatalf("state = %v, erwartet ViewFileManager (Fehler: %q)", m.state, m.fm.message)
	}
	return m
}

// TestFileManagerSingleConnectsOnce - nur die gewählte Verbindung wird
// aufgebaut; ein Fenster über die ganze Breite, Bildschirm exakt gefüllt.
func TestFileManagerSingleConnectsOnce(t *testing.T) {
	m, rec := openSingleManager(t)
	if len(rec.calls) != 1 || rec.calls[0] != "id-0/" {
		t.Errorf("Verbindungen %v, erwartet nur Server", rec.calls)
	}
	if m.fm.panels[1].fs != nil || m.fm.active != 0 {
		t.Errorf("zweites Fenster verbunden bzw. aktiv")
	}
	assertScreenSize(t, m)
	lines := strings.Split(stripANSI(m.View()), "\n")
	if top := lines[1]; strings.Count(top, "┌") != 1 || !strings.HasPrefix(top, "┌") || !strings.HasSuffix(top, "┐") {
		t.Errorf("erwartet ein Fenster über die ganze Breite: %q", top)
	}
	if !strings.Contains(lines[1], "/home/kurt") {
		t.Errorf("Pfad fehlt im Titel: %q", lines[1])
	}
	// Tab wechselt nicht in ein leeres Fenster
	m = press(t, m, "tab")
	if m.fm.active != 0 {
		t.Errorf("Tab: aktives Fenster %d", m.fm.active)
	}
}

// TestFileManagerSingleNoCopyMove - F5/F6 öffnen keinen Dialog, sondern
// melden, dass eine zweite Verbindung nötig ist; auch mit Markierung.
func TestFileManagerSingleNoCopyMove(t *testing.T) {
	for _, key := range []string{"f5", "f6"} {
		m, _ := openSingleManager(t)
		m = cursorTo(t, m, "b.txt")
		m = press(t, m, "insert")
		m = pressRun(t, m, key)
		if m.fm.dialog != fmDialogNone {
			t.Errorf("%s: Dialog %v geöffnet", key, m.fm.dialog)
		}
		if m.fm.message != m.tr(txtFMSingleNoCopy) {
			t.Errorf("%s: Meldung %q", key, m.fm.message)
		}
		if !strings.Contains(stripANSI(m.View()), m.tr(txtFMSingleNoCopy)) {
			t.Errorf("%s: Meldung nicht sichtbar", key)
		}
	}
}

// TestFileManagerSingleKeyBar - F5 und F6 stehen ohne Beschriftung in der
// Leiste, die übrigen wie gewohnt.
func TestFileManagerSingleKeyBar(t *testing.T) {
	m, _ := openSingleManager(t)
	lines := strings.Split(stripANSI(m.View()), "\n")
	bar := lines[len(lines)-1]
	labels := strings.Split(m.tr(txtFMFKeys), "|")
	for i, label := range labels {
		has := strings.Contains(bar, label)
		if want := i != 4 && i != 5; has != want {
			t.Errorf("F%d %q in der Leiste: %v, erwartet %v (%q)", i+1, label, has, want, bar)
		}
	}
}

// TestFileManagerSingleMenus - drei Menüs (Fenster, Datei, Optionen), im
// Datei-Menü weder Kopieren noch Verschieben; F9 öffnet das Fenster-Menü
// und Neu einlesen wirkt auf das einzige Fenster.
func TestFileManagerSingleMenus(t *testing.T) {
	m, _ := openSingleManager(t)
	menus := m.fmMenus()
	if len(menus) != 3 {
		t.Fatalf("%d Menüs, erwartet 3", len(menus))
	}
	bar := strings.Split(m.tr(txtFMMenuBar), "|")
	if menus[0].title != m.tr(txtFMMenuPanel) || menus[1].title != bar[1] || menus[2].title != bar[2] {
		t.Errorf("Titel %q/%q/%q", menus[0].title, menus[1].title, menus[2].title)
	}
	for _, item := range menus[1].items {
		if item.action == fmActCopy || item.action == fmActMove {
			t.Errorf("Datei-Menü enthält %v", item.label)
		}
	}
	if !strings.Contains(stripANSI(m.View()), m.tr(txtFMMenuPanel)) {
		t.Error("Menüleiste zeigt den Fenster-Titel nicht")
	}
	m = press(t, m, "f9")
	if !m.fm.menuOpen || m.fm.menuIndex != 0 {
		t.Fatalf("F9: offen %v, Menü %d", m.fm.menuOpen, m.fm.menuIndex)
	}
	// Links um und wieder zurück bleibt innerhalb der drei Menüs
	m = press(t, m, "left")
	if m.fm.menuIndex != 2 {
		t.Errorf("links vom ersten Menü: %d, erwartet 2", m.fm.menuIndex)
	}
	m = press(t, m, "right")
	assertScreenSize(t, m)
}

// TestFileManagerSingleMenuRescan - "Fenster → Neu einlesen" liest das
// einzige Fenster wirklich neu ein (nicht das leere zweite).
func TestFileManagerSingleMenuRescan(t *testing.T) {
	fsys := newMemFS("/home/kurt")
	fsys.addFile("/home/kurt/a.txt", "a", 0o644, copyTestTime)
	m := openSingleManagerWith(t, func(Connection, string) (remoteFS, error) { return fsys, nil })
	fsys.mu.Lock()
	fsys.addFile("/home/kurt/neu.txt", "n", 0o644, copyTestTime)
	fsys.mu.Unlock()
	m = press(t, m, "f9")
	m = pressRun(t, m, "enter")
	if m.fm.menuOpen || m.state != ViewFileManager {
		t.Fatalf("offen %v, state %v", m.fm.menuOpen, m.state)
	}
	found := false
	for _, e := range m.fm.panels[0].entries {
		found = found || e.name == "neu.txt"
	}
	if !found {
		t.Errorf("neu.txt nach Neu einlesen nicht da: %v", entryNames(m.fm.panels[0]))
	}
}

// TestFileManagerSingleViewEditMkdirDelete - Ansehen, Bearbeiten,
// Verzeichnis anlegen und Löschen funktionieren mit einer Verbindung.
func TestFileManagerSingleViewEditMkdirDelete(t *testing.T) {
	fsys := newMemFS("/home/kurt")
	fsys.addFile("/home/kurt/b.txt", "Hallo", 0o644, copyTestTime)
	m := openSingleManagerWith(t, func(Connection, string) (remoteFS, error) { return fsys, nil })
	m = cursorTo(t, m, "b.txt")
	if got := pressRun(t, m, "f3"); !got.fm.viewing {
		t.Error("F3 öffnet den Betrachter nicht")
	}
	if got := pressRun(t, m, "f4"); !got.fm.editing {
		t.Error("F4 öffnet den Editor nicht")
	}
	if got := pressRun(t, m, "f7"); got.fm.dialog != fmDialogMkdir {
		t.Errorf("F7: Dialog %v", got.fm.dialog)
	}
	if got := pressRun(t, m, "f8"); got.fm.dialog != fmDialogDeleteConfirm {
		t.Errorf("F8: Dialog %v", got.fm.dialog)
	}
}

// TestFileManagerSingleCloseReturnsToRow - q, F10 und Abbruch in der
// Auswahl führen zurück auf den (einzigen) Datei-Manager-Eintrag und
// schließen die Verbindung; danach öffnet Enter wieder die Auswahl der
// linken Seite, ohne Ein-Fenster-Modus.
func TestFileManagerSingleCloseReturnsToRow(t *testing.T) {
	for _, key := range []string{"q", "f10"} {
		m, rec := openSingleManager(t)
		m = pressRun(t, m, key)
		if m.state != ViewList {
			t.Fatalf("%s: state %v", key, m.state)
		}
		if rows := m.listRows(); rows[m.cursor].kind != rowFileManager {
			t.Errorf("%s: Cursor nicht auf dem Datei-Manager-Eintrag", key)
		}
		if !rec.created["id-0"].closed {
			t.Errorf("%s: Verbindung nicht geschlossen", key)
		}
		if m.fm.single {
			t.Errorf("%s: Modus bleibt nach dem Schließen", key)
		}
		m = press(t, m, "enter")
		if m.state != ViewFMPick || m.fm.single || m.fm.pickSide != 0 {
			t.Errorf("%s: danach: state %v, single %v, Seite %d", key, m.state, m.fm.single, m.fm.pickSide)
		}
	}
	// Esc in der Auswahl (links und rechts)
	for _, keys := range [][]string{{"esc"}, {"enter", "esc"}} {
		m := newListTestModel(t, "Server")
		m = openPick(t, m)
		m = press(t, m, keys...)
		if rows := m.listRows(); m.state != ViewList || rows[m.cursor].kind != rowFileManager {
			t.Errorf("Tasten %v in der Auswahl: state %v, Cursor %d", keys, m.state, m.cursor)
		}
	}
}

// TestFileManagerSingleSmallScreens - jede Größe und jeder Farbmodus füllt
// den Bildschirm exakt, auch mit offenem Menü.
func TestFileManagerSingleSmallScreens(t *testing.T) {
	m, _ := openSingleManager(t)
	for theme := range fmThemes {
		for _, size := range [][2]int{{100, 30}, {40, 12}, {20, 8}} {
			got := m
			got.fm.theme = theme
			got.termWidth, got.termHeight = size[0], size[1]
			assertScreenSize(t, got)
			got = press(t, got, "f9")
			assertScreenSize(t, got)
		}
	}
}

// TestFileManagerTwoConnectionsUnchanged - mit einer Verbindung rechts
// (statt "(keine)") bleibt es beim Zwei-Fenster-Modus mit Kopieren.
func TestFileManagerTwoConnectionsUnchanged(t *testing.T) {
	m, _ := openFileManagerFor(t)
	if m.fm.single || len(m.fmMenus()) != 4 || m.fm.panels[1].fs == nil {
		t.Fatalf("single %v, Menüs %d", m.fm.single, len(m.fmMenus()))
	}
	m = cursorTo(t, m, "b.txt")
	if got := pressRun(t, m, "f5"); got.fm.dialog != fmDialogCopyConfirm {
		t.Errorf("F5: Dialog %v", got.fm.dialog)
	}
}
