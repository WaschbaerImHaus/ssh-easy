// Paket main - Tests: Auswahl im Datei-Manager ohne Platzhalter, mit
// "(keine)" und gemerkter Wahl (Build 87)
//
// Nutzerwunsch 2026-10-28: Es gibt nur noch EINEN Datei-Manager. Bei der Wahl
// der rechten Seite kommt "(keine)" als Auswahl hinzu - dann öffnet sich das
// Ein-Fenster-Fenster (fm.single). Der Platzhalter "(nicht ausgewählt)"
// entfällt links wie rechts. Bei eingeschaltetem "Verbindung merken" steht der
// Cursor beim nächsten Öffnen auf der zuletzt gewählten linken bzw. rechten
// Seite.
//
// Zeilen der Auswahl (so prüfen es die Tests):
//   - links:  Zeile 0 "Lokaler PC", danach die Verbindungen
//   - rechts: Zeile 0 "(keine)", dann "Lokaler PC" (nur wenn links nicht der
//     Lokale PC gewählt wurde), dann die Verbindungen ohne die linke
//
// Gemerkt werden Verbindungs-IDs, rememberLocalPC ("local-pc") und rechts
// zusätzlich rememberNone ("none"). Die Hilfsfunktionen restartModel,
// newRememberModel usw. stehen in remember_selection_test.go.
//
// @author Kurt Ingwer
// @date   2026-10-30 21:14
package main

import (
	"errors"
	"strings"
	"testing"
)

// --- Hilfsfunktionen ---

// pickHighlighted liefert den Namen der in der Auswahl markierten Zeile (ohne
// den Zusatz "  (Benutzer@Host)"): "Lokaler PC", "(keine)" oder der
// Verbindungsname. Geprüft wird, was der Nutzer sieht - es muss genau eine
// Zeile markiert sein. Die Terminalhöhe muss die ganze Auswahl zeigen, sonst
// ist die markierte Zeile ggf. nicht sichtbar (dann scheitert der Test hier
// absichtlich: gemerkter Eintrag nicht sichtbar).
func pickHighlighted(t *testing.T, m AppModel) string {
	t.Helper()
	withColorProfile(t)
	var found []string
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "48;5;57") {
			found = append(found, strings.TrimSpace(stripANSI(line)))
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d markierte Zeilen in der Auswahl, erwartet 1:\n%s", len(found), stripANSI(m.View()))
	}
	name := found[0]
	if i := strings.Index(name, "  ("); i >= 0 {
		name = name[:i]
	}
	return name
}

// assertPickOrder prüft, dass alle Texte in der Anzeige vorkommen und in
// dieser Reihenfolge von oben nach unten stehen.
func assertPickOrder(t *testing.T, text string, labels ...string) {
	t.Helper()
	last := -1
	for _, label := range labels {
		at := strings.Index(text, label)
		if at < 0 {
			t.Errorf("%q fehlt in der Auswahl:\n%s", label, text)
			return
		}
		if at < last {
			t.Errorf("%q steht nicht hinter dem Vorgänger:\n%s", label, text)
			return
		}
		last = at
	}
}

// connLine liefert den Anfang der Auswahlzeile einer Verbindung.
func connLine(name string) string {
	return "  " + name + "  ("
}

// fullPickModel baut ein Modell für die Auswahl mit hoher Terminalhöhe, damit
// alle Zeilen sichtbar sind.
func fullPickModel(t *testing.T, on bool, left, right string, names ...string) (AppModel, *fmConnectRecorder) {
	t.Helper()
	m, rec := newRememberModel(t, on, "", left, right, names...)
	m.termWidth, m.termHeight = 100, 60
	return m, rec
}

// --- Zeilen der Auswahl (F1) ---

// TestFMPickLeftAndRightRows (F1): Links steht "Lokaler PC" vor den
// Verbindungen, kein Platzhalter, kein "(keine)". Rechts steht "(keine)"
// zuerst, dann "Lokaler PC" (nur wenn links nicht der Lokale PC gewählt
// wurde), dann die Verbindungen ohne die linke.
func TestFMPickLeftAndRightRows(t *testing.T) {
	m, _ := fullPickModel(t, false, "", "", "A", "B", "C")
	m = openPick(t, m)
	text := stripANSI(m.View())
	local := m.tr(txtFMLocalPC)
	assertPickOrder(t, text, local, connLine("A"), connLine("B"), connLine("C"))
	if strings.Contains(text, "(nicht ausgewählt)") || strings.Contains(text, m.tr(txtFMNone)) {
		t.Errorf("links darf weder Platzhalter noch \"(keine)\" stehen:\n%s", text)
	}
	if !strings.Contains(text, m.tr(txtFMPickLeft)) {
		t.Errorf("Titel der linken Seite fehlt:\n%s", text)
	}

	// Links Verbindung B -> rechts: (keine), Lokaler PC, A, C
	right := pickSide(t, m, "B")
	if right.state != ViewFMPick || right.fm.pickSide != 1 {
		t.Fatalf("nach der linken Wahl: state %v, Seite %d", right.state, right.fm.pickSide)
	}
	text = stripANSI(right.View())
	assertPickOrder(t, text, m.tr(txtFMNone), local, connLine("A"), connLine("C"))
	if strings.Contains(text, connLine("B")) || strings.Contains(text, "(nicht ausgewählt)") {
		t.Errorf("rechts steht die linke Verbindung B oder der Platzhalter:\n%s", text)
	}
	if !strings.Contains(text, m.tr(txtFMPickRight)) {
		t.Errorf("Titel der rechten Seite fehlt:\n%s", text)
	}

	// Links Lokaler PC -> rechts: (keine), A, B, C - ohne Lokalen PC
	rightOfLocal := pickSide(t, m, local)
	text = stripANSI(rightOfLocal.View())
	assertPickOrder(t, text, m.tr(txtFMNone), connLine("A"), connLine("B"), connLine("C"))
	if strings.Contains(text, local) {
		t.Errorf("rechts ist der Lokale PC noch einmal wählbar:\n%s", text)
	}
	if got := strings.Join(pickNames(rightOfLocal), ","); got != "A,B,C" {
		t.Errorf("rechts (Lokaler PC links): Verbindungen %s, erwartet A,B,C", got)
	}
}

// --- Ein-Fenster-Modus über "(keine)" (F2, F3) ---

// TestFMPickNoneOpensSingleWindow (F2): Rechts "(keine)" baut genau EINE
// Verbindung auf, fm.single ist gesetzt, ein Fenster über die ganze Breite;
// F5/F6 melden txtFMSingleNoCopy und fehlen in Tastenleiste und Menüs. Mit
// einer Verbindung rechts bleibt es dagegen bei zwei Fenstern.
func TestFMPickNoneOpensSingleWindow(t *testing.T) {
	m, rec := fullPickModel(t, false, "", "", "Server", "Zweit")
	m.termHeight = 30
	m = openPick(t, m)
	m = pickSingle(t, m, "Server")
	if m.state != ViewFileManager || !m.fm.single {
		t.Fatalf("state %v, single %v, erwartet Datei-Manager im Ein-Fenster-Modus", m.state, m.fm.single)
	}
	if len(rec.calls) != 1 || rec.calls[0] != "id-0/" {
		t.Errorf("Verbindungsaufrufe %v, erwartet genau [id-0/]", rec.calls)
	}
	if m.fm.panels[1].fs != nil || m.fm.panels[1].conn.ID != "" {
		t.Error("rechtes Fenster wurde belegt oder verbunden")
	}
	assertScreenSize(t, m)
	lines := strings.Split(stripANSI(m.View()), "\n")
	if top := lines[1]; strings.Count(top, "┌") != 1 || !strings.HasPrefix(top, "┌") || !strings.HasSuffix(top, "┐") {
		t.Errorf("erwartet ein Fenster über die ganze Breite: %q", top)
	}
	for _, key := range []string{"f5", "f6"} {
		got := pressRun(t, m, key)
		if got.fm.dialog != fmDialogNone || got.fm.message != m.tr(txtFMSingleNoCopy) {
			t.Errorf("%s: Dialog %v, Meldung %q, erwartet txtFMSingleNoCopy", key, got.fm.dialog, got.fm.message)
		}
	}
	bar := lines[len(lines)-1]
	for i, label := range strings.Split(m.tr(txtFMFKeys), "|") {
		if has, want := strings.Contains(bar, label), i != 4 && i != 5; has != want {
			t.Errorf("F%d %q in der Leiste: %v, erwartet %v", i+1, label, has, want)
		}
	}
	if n := len(m.fmMenus()); n != 3 {
		t.Errorf("%d Menüs, erwartet 3 (Fenster, Datei, Optionen)", n)
	}

	// Gegenprobe: eine Verbindung rechts = zwei Fenster, beide verbunden
	two, rec2 := fullPickModel(t, false, "", "", "Server", "Zweit")
	two.termHeight = 30
	two = openPick(t, two)
	two = pickSide(t, two, "Server")
	two = pickSide(t, two, "Zweit")
	if two.state != ViewFileManager || two.fm.single || len(two.fmMenus()) != 4 || strings.Join(rec2.calls, ",") != "id-0/,id-1/" {
		t.Errorf("zwei Verbindungen: state %v, single %v, Menüs %d, Aufrufe %v", two.state, two.fm.single, len(two.fmMenus()), rec2.calls)
	}
}

// TestFMPickLocalAndNone (F3): "Lokaler PC" links und "(keine)" rechts ergibt
// das lokale Ein-Fenster; nur der Lokale PC wird geöffnet (kein Server-
// aufruf), Kopieren/Verschieben melden wie im Ein-Fenster-Modus.
func TestFMPickLocalAndNone(t *testing.T) {
	m, rec := fullPickModel(t, false, "", "", "Server")
	m.termHeight = 30
	m = openPick(t, m)
	m = pickSingle(t, m, m.tr(txtFMLocalPC))
	if m.state != ViewFileManager || !m.fm.single || m.fm.panels[0].conn.ID != fmLocalConnID {
		t.Fatalf("state %v, single %v, links %q", m.state, m.fm.single, m.fm.panels[0].conn.ID)
	}
	if strings.Join(rec.calls, ",") != "local" {
		t.Errorf("Aufrufe %v, erwartet nur den Lokalen PC", rec.calls)
	}
	assertScreenSize(t, m)
	if got := pressRun(t, m, "f5"); got.fm.message != m.tr(txtFMSingleNoCopy) {
		t.Errorf("F5: Meldung %q", got.fm.message)
	}
}

// --- Cursor und Abbruch (F4, F5) ---

// TestFMPickStartsOnFirstEntryWithoutMemory (F4): Ohne gemerkte Wahl bzw. mit
// ausgeschaltetem Schalter startet der Cursor links und rechts auf Zeile 0 -
// auch wenn von Hand Werte in der Konfiguration stehen. Es gibt keinen
// Platzhalter mehr: Enter-Enter öffnet "Lokaler PC" im Ein-Fenster-Modus.
func TestFMPickStartsOnFirstEntryWithoutMemory(t *testing.T) {
	for _, stored := range [][2]string{{"", ""}, {"id-1", "id-0"}} {
		m, rec := fullPickModel(t, false, stored[0], stored[1], "A", "B")
		m.termHeight = 30
		m = openPick(t, m)
		if m.fm.pickCursor != 0 || pickHighlighted(t, m) != m.tr(txtFMLocalPC) {
			t.Fatalf("Werte %v: links Cursor %d auf %q, erwartet 0 auf Lokaler PC", stored, m.fm.pickCursor, pickHighlighted(t, m))
		}
		// Enter-Enter: Lokaler PC + (keine)
		right := press(t, m, "enter")
		if right.fm.pickSide != 1 || right.fm.pickCursor != 0 || pickHighlighted(t, right) != m.tr(txtFMNone) {
			t.Fatalf("Werte %v: rechts Seite %d, Cursor %d auf %q, erwartet 0 auf (keine)", stored, right.fm.pickSide, right.fm.pickCursor, pickHighlighted(t, right))
		}
		done := pressRun(t, m, "enter")
		done = pressRun(t, done, "enter")
		if done.state != ViewFileManager || !done.fm.single || done.fm.panels[0].conn.ID != fmLocalConnID || strings.Join(rec.calls, ",") != "local" {
			t.Errorf("Werte %v: Enter-Enter: state %v, single %v, Aufrufe %v", stored, done.state, done.fm.single, rec.calls)
		}
	}
}

// TestFMPickCancelKeepsConfigAndCursor (F5): q und Esc in der Auswahl (links
// wie rechts) führen zur Liste, der Cursor steht auf der Datei-Manager-Zeile,
// und die Konfiguration bleibt unverändert - auch bei eingeschaltetem Merken
// und schon getroffener linker Wahl.
func TestFMPickCancelKeepsConfigAndCursor(t *testing.T) {
	for _, keys := range [][]string{{"q"}, {"esc"}, {"down", "enter", "q"}, {"down", "enter", "esc"}, {"enter", "down", "esc"}} {
		m, _ := newRememberModel(t, true, "id-0", "id-0", "id-1", "A", "B")
		before := configBytes(t, m.configPath)
		past := backdateConfig(t, m.configPath)
		got := press(t, m, "end", "enter")
		got = press(t, got, keys...)
		if got.state != ViewList {
			t.Errorf("Tasten %v: state %v, erwartet ViewList", keys, got.state)
			continue
		}
		if rows := got.listRows(); got.cursor >= len(rows) || rows[got.cursor].kind != rowFileManager {
			t.Errorf("Tasten %v: Cursor %d nicht auf der Datei-Manager-Zeile", keys, got.cursor)
		}
		assertConfigUntouched(t, m.configPath, before, past, "Abbruch "+strings.Join(keys, ","))
	}
}

// --- Merken (F6-F9, F14) ---

// TestFMPickRemembersChoice (F6): Bei eingeschaltetem Schalter speichert der
// Abschluss der Wahl beide Werte - schon wenn der Verbindungsaufbau beginnt.
// Beim erneuten Öffnen (auch nach einem Neustart) steht der Cursor links auf
// A, nach Enter rechts auf B. Gemerkt werden auch Lokaler PC und "(keine)"
// in ihrer Konfigurationsschreibweise.
func TestFMPickRemembersChoice(t *testing.T) {
	m, rec := fullPickModel(t, true, "", "", "A", "B", "C")
	m.termHeight = 30
	m = openPick(t, m)
	m = pickSide(t, m, "A")
	// Letzter Enter ohne Folgekommando: gespeichert wird beim Aufbaustart
	m = press(t, m, "home", "down", "down", "enter")
	if m.state != ViewFMConnecting {
		t.Fatalf("state %v, erwartet ViewFMConnecting", m.state)
	}
	cfg := loadTestConfig(t, m)
	if cfg.FileManagerLastLeft != "id-0" || cfg.FileManagerLastRight != "id-1" {
		t.Fatalf("gemerkt links %q, rechts %q, erwartet id-0 / id-1", cfg.FileManagerLastLeft, cfg.FileManagerLastRight)
	}
	if len(rec.calls) != 0 {
		t.Errorf("Verbindung vor dem Aufbau-Kommando aufgerufen: %v", rec.calls)
	}

	// Erneut öffnen: im selben Programm und nach einem Neustart
	for _, restart := range []bool{false, true} {
		again := m
		if restart {
			again, _ = restartModel(t, m)
			again.termHeight = 30
		} else {
			again = m.closeFileManager()
		}
		again = press(t, again, "end", "enter")
		if again.state != ViewFMPick || pickHighlighted(t, again) != "A" {
			t.Fatalf("Neustart=%v: links markiert %q, erwartet A", restart, pickHighlighted(t, again))
		}
		again = press(t, again, "enter")
		if again.fm.pickSide != 1 || pickHighlighted(t, again) != "B" {
			t.Errorf("Neustart=%v: rechts Seite %d markiert %q, erwartet B", restart, again.fm.pickSide, pickHighlighted(t, again))
		}
	}

	// Schreibweise für Lokaler PC und (keine)
	for _, c := range []struct {
		left, right         string
		wantLeft, wantRight string
	}{
		{m.tr(txtFMLocalPC), "", rememberLocalPC, rememberNone},
		{"A", m.tr(txtFMLocalPC), "id-0", rememberLocalPC},
		{m.tr(txtFMLocalPC), "C", rememberLocalPC, "id-2"},
		{"B", "", "id-1", rememberNone},
	} {
		x, _ := fullPickModel(t, true, "", "", "A", "B", "C")
		x = openPick(t, x)
		x = pickSide(t, x, c.left)
		x = pickSide(t, x, c.right)
		cfg := loadTestConfig(t, x)
		if cfg.FileManagerLastLeft != c.wantLeft || cfg.FileManagerLastRight != c.wantRight {
			t.Errorf("Wahl %q/%q: gemerkt %q/%q, erwartet %q/%q", c.left, c.right, cfg.FileManagerLastLeft, cfg.FileManagerLastRight, c.wantLeft, c.wantRight)
		}
	}
}

// TestFMPickRestoresNoneAndLocal (F7): Gemerkt (A, none) -> rechts steht der
// Cursor auf "(keine)". Gemerkt (B, local-pc) -> rechts auf "Lokaler PC".
// Gemerkt (local-pc, C) -> links auf "Lokaler PC", rechts auf C.
func TestFMPickRestoresNoneAndLocal(t *testing.T) {
	cases := []struct {
		left, right         string
		wantLeft, wantRight string
	}{
		{"id-0", rememberNone, "A", "(keine)"},
		{"id-1", rememberLocalPC, "B", "Lokaler PC"},
		{rememberLocalPC, "id-2", "Lokaler PC", "C"},
		{"id-2", "id-0", "C", "A"},
	}
	for _, c := range cases {
		m, _ := fullPickModel(t, true, c.left, c.right, "A", "B", "C")
		m = openPick(t, m)
		if got := pickHighlighted(t, m); got != c.wantLeft {
			t.Errorf("gemerkt %q/%q: links markiert %q, erwartet %q", c.left, c.right, got, c.wantLeft)
			continue
		}
		m = press(t, m, "enter")
		if m.fm.pickSide != 1 {
			t.Fatalf("gemerkt %q/%q: nach Enter Seite %d", c.left, c.right, m.fm.pickSide)
		}
		if got := pickHighlighted(t, m); got != c.wantRight {
			t.Errorf("gemerkt %q/%q: rechts markiert %q, erwartet %q", c.left, c.right, got, c.wantRight)
		}
	}
}

// TestFMPickDeletedRememberedConnection (F8): Ist die gemerkte Verbindung
// gelöscht, steht der Cursor auf Zeile 0 - links wie rechts, ohne Meldung
// und ohne Absturz. Der Rest der gemerkten Wahl bleibt gültig.
func TestFMPickDeletedRememberedConnection(t *testing.T) {
	// Nur die linke gemerkte Verbindung (A) ist gelöscht: links Zeile 0, die
	// rechte gemerkte (B) gilt weiter
	m, _ := fullPickModel(t, true, "id-0", "id-1", "A", "B", "C")
	if err := DeleteConnection(m.configPath, "id-0"); err != nil {
		t.Fatal(err)
	}
	m, _ = restartModel(t, m)
	m.termHeight = 60
	m = openPick(t, m)
	if m.fm.pickCursor != 0 || pickHighlighted(t, m) != m.tr(txtFMLocalPC) {
		t.Errorf("links: Cursor %d auf %q, erwartet 0 auf Lokaler PC (A ist gelöscht)", m.fm.pickCursor, pickHighlighted(t, m))
	}
	m = press(t, m, "enter")
	if got := pickHighlighted(t, m); got != "B" {
		t.Errorf("rechts markiert %q, erwartet B (noch vorhanden)", got)
	}

	// Beide gemerkten Verbindungen gelöscht: links wie rechts Zeile 0,
	// keine Meldung
	both, _ := fullPickModel(t, true, "id-0", "id-1", "A", "B", "C")
	for _, id := range []string{"id-0", "id-1"} {
		if err := DeleteConnection(both.configPath, id); err != nil {
			t.Fatal(err)
		}
	}
	both, _ = restartModel(t, both)
	both.termHeight = 60
	both = openPick(t, both)
	if both.fm.pickCursor != 0 || pickHighlighted(t, both) != both.tr(txtFMLocalPC) {
		t.Errorf("links: Cursor %d auf %q, erwartet 0 auf Lokaler PC", both.fm.pickCursor, pickHighlighted(t, both))
	}
	both = press(t, both, "enter")
	if both.fm.pickCursor != 0 || pickHighlighted(t, both) != both.tr(txtFMNone) {
		t.Errorf("rechts: Cursor %d auf %q, erwartet 0 auf (keine)", both.fm.pickCursor, pickHighlighted(t, both))
	}
	if both.errorMsg != "" || both.successMsg != "" {
		t.Errorf("Meldung bei gelöschter Verbindung: %q / %q", both.errorMsg, both.successMsg)
	}

	// Gemerkt wurde nur die gelöschte rechte Verbindung: links gilt weiter
	m2, _ := fullPickModel(t, true, "id-2", "id-1", "A", "B", "C")
	if err := DeleteConnection(m2.configPath, "id-1"); err != nil {
		t.Fatal(err)
	}
	m2, _ = restartModel(t, m2)
	m2.termHeight = 60
	m2 = press(t, m2, "end", "enter")
	if got := pickHighlighted(t, m2); got != "C" {
		t.Fatalf("links markiert %q, erwartet C", got)
	}
	m2 = press(t, m2, "enter")
	if m2.fm.pickCursor != 0 || pickHighlighted(t, m2) != m2.tr(txtFMNone) {
		t.Errorf("rechts: Cursor %d auf %q, erwartet 0 auf (keine)", m2.fm.pickCursor, pickHighlighted(t, m2))
	}
}

// TestFMPickRememberedRightEqualsLeft (F9): Gemerkt (A, B), links wird jetzt B
// gewählt: B fehlt rechts - der Cursor steht auf Zeile 0, nichts stürzt ab.
// Ebenso, wenn rechts "Lokaler PC" gemerkt ist und links jetzt Lokaler PC.
func TestFMPickRememberedRightEqualsLeft(t *testing.T) {
	m, _ := fullPickModel(t, true, "id-0", "id-1", "A", "B", "C")
	m = openPick(t, m)
	m = pickSide(t, m, "B")
	if m.fm.pickSide != 1 || m.fm.pickCursor != 0 || pickHighlighted(t, m) != m.tr(txtFMNone) {
		t.Errorf("links B, gemerkt rechts B: Seite %d, Cursor %d auf %q, erwartet 0 auf (keine)", m.fm.pickSide, m.fm.pickCursor, pickHighlighted(t, m))
	}
	if strings.Contains(stripANSI(m.View()), connLine("B")) {
		t.Error("B steht rechts zur Wahl")
	}

	l, _ := fullPickModel(t, true, "id-0", rememberLocalPC, "A", "B")
	l = openPick(t, l)
	l = pickSide(t, l, l.tr(txtFMLocalPC))
	if l.fm.pickCursor != 0 || pickHighlighted(t, l) != l.tr(txtFMNone) {
		t.Errorf("links Lokaler PC, gemerkt rechts Lokaler PC: Cursor %d auf %q, erwartet 0 auf (keine)", l.fm.pickCursor, pickHighlighted(t, l))
	}
}

// TestFMPickRememberOffSavesNothing (F14): Bei ausgeschaltetem Schalter wird
// auch eine vollständige Wahl nicht gespeichert (Inhalt und Änderungszeit der
// Datei bleiben) - weder bei zwei Fenstern noch bei "(keine)".
func TestFMPickRememberOffSavesNothing(t *testing.T) {
	for _, none := range []bool{false, true} {
		m, _ := fullPickModel(t, false, "", "", "A", "B")
		m = openPick(t, m)
		m = pickSide(t, m, "A")
		if none {
			m = pickSide(t, m, "")
		} else {
			m = pickSide(t, m, "B")
		}
		if m.state != ViewFileManager {
			t.Fatalf("(keine)=%v: state %v", none, m.state)
		}
		m = pressRun(t, m, "f10")
		// rememberFMPath (zuletzt geöffnetes Verzeichnis) darf schreiben; die
		// Auswahl selbst darf in der Datei nicht auftauchen
		cfg := loadTestConfig(t, m)
		if cfg.RememberSelection || cfg.LastConnectionID != "" || cfg.FileManagerLastLeft != "" || cfg.FileManagerLastRight != "" {
			t.Errorf("(keine)=%v: Auswahl gespeichert: %v %q %q %q", none, cfg.RememberSelection, cfg.LastConnectionID, cfg.FileManagerLastLeft, cfg.FileManagerLastRight)
		}
	}
	// Beginnt der Verbindungsaufbau (ohne dass er läuft), bleibt die Datei
	// Byte für Byte und in der Änderungszeit unberührt
	m, _ := fullPickModel(t, false, "", "", "A", "B")
	before := configBytes(t, m.configPath)
	past := backdateConfig(t, m.configPath)
	m = openPick(t, m)
	m = pickSide(t, m, "A")
	m = press(t, m, "home", "down", "down", "enter")
	if m.state != ViewFMConnecting {
		t.Fatalf("state %v", m.state)
	}
	assertConfigUntouched(t, m.configPath, before, past, "Wahl bei ausgeschaltetem Merken")
}

// --- Sichtbarkeit und Fehler (F10, F11) ---

// TestFMPickRememberedEntryVisible (F10): 30 Verbindungen, Terminalhöhe 20,
// der gemerkte Eintrag liegt weit unten: Er ist sichtbar und markiert -
// links wie rechts (pickOffset hält den Cursor im Fenster).
func TestFMPickRememberedEntryVisible(t *testing.T) {
	for _, height := range []int{20, 17, 12, 40} {
		m, _ := newRememberModel(t, true, "", "id-24", "id-27", namesN(30)...)
		m.termHeight = height
		m = openPick(t, m)
		limit := visibleRowLimit(height)
		if got := pickHighlighted(t, m); got != "Host25" {
			t.Fatalf("Höhe %d: links markiert %q, erwartet Host25", height, got)
		}
		if m.fm.pickCursor < m.fm.pickOffset || m.fm.pickCursor >= m.fm.pickOffset+limit {
			t.Errorf("Höhe %d: Cursor %d außerhalb des Fensters %d..%d", height, m.fm.pickCursor, m.fm.pickOffset, m.fm.pickOffset+limit)
		}
		m = press(t, m, "enter")
		if got := pickHighlighted(t, m); got != "Host28" {
			t.Fatalf("Höhe %d: rechts markiert %q, erwartet Host28", height, got)
		}
		if m.fm.pickCursor < m.fm.pickOffset || m.fm.pickCursor >= m.fm.pickOffset+limit {
			t.Errorf("Höhe %d: rechts Cursor %d außerhalb des Fensters %d..%d", height, m.fm.pickCursor, m.fm.pickOffset, m.fm.pickOffset+limit)
		}
		// Nach oben/unten bleibt der Cursor sichtbar
		m = press(t, m, "home", "end")
		_ = pickHighlighted(t, m)
	}
}

// TestFMPickConnectErrorKeepsChoice (F11): Scheitert die Verbindung (Netz,
// Host-Key, Abbruch der Passwortabfrage), bleibt die Wahl gespeichert - der
// nächste Versuch beginnt auf denselben Einträgen.
func TestFMPickConnectErrorKeepsChoice(t *testing.T) {
	errs := map[string]error{
		"netz":     &networkError{err: errors.New("dial tcp 10.0.0.1:22: i/o timeout")},
		"hostkey":  &HostKeyChangedError{Hostname: "10.0.0.1"},
		"passwort": errors.New("ssh: unable to authenticate"),
	}
	for name, connErr := range errs {
		m, rec := fullPickModel(t, true, "", "", "A", "B")
		m.termHeight = 30
		rec.results["id-1"] = connErr
		m = openPick(t, m)
		m = pickSide(t, m, "A")
		m = pickSide(t, m, "B")
		if name == "passwort" {
			if m.state != ViewFMPassword {
				t.Fatalf("%s: state %v, erwartet Passwortabfrage", name, m.state)
			}
			m = press(t, m, "esc")
		}
		if m.state != ViewList {
			t.Fatalf("%s: state %v, erwartet ViewList", name, m.state)
		}
		cfg := loadTestConfig(t, m)
		if cfg.FileManagerLastLeft != "id-0" || cfg.FileManagerLastRight != "id-1" {
			t.Errorf("%s: gemerkt %q/%q, erwartet id-0/id-1", name, cfg.FileManagerLastLeft, cfg.FileManagerLastRight)
		}
		again := press(t, m, "enter")
		if got := pickHighlighted(t, again); got != "A" {
			t.Errorf("%s: erneut geöffnet: links markiert %q, erwartet A", name, got)
		}
	}

	// Die linke Seite scheitert (Lokaler PC/Server): Wahl ebenfalls gespeichert
	m, rec := fullPickModel(t, true, "", "", "A", "B")
	m.termHeight = 30
	rec.results["id-0"] = &networkError{err: errors.New("i/o timeout")}
	m = openPick(t, m)
	m = pickSingle(t, m, "A")
	if m.state != ViewList {
		t.Fatalf("state %v, erwartet ViewList", m.state)
	}
	if cfg := loadTestConfig(t, m); cfg.FileManagerLastLeft != "id-0" || cfg.FileManagerLastRight != rememberNone {
		t.Errorf("gemerkt %q/%q, erwartet id-0/none", cfg.FileManagerLastLeft, cfg.FileManagerLastRight)
	}
}

// --- Manipulierte Werte, eine Verbindung (F12, F13) ---

// TestFMPickManipulatedRememberedValues (F12): Manipulierte gemerkte Werte
// (Escape-Sequenz, 10.000 Zeichen, "none" links, Pfade, NUL, der interne
// Name des Lokalen PCs) werden ignoriert und nie angezeigt: Cursor 0, keine
// Meldung, kein Absturz.
func TestFMPickManipulatedRememberedValues(t *testing.T) {
	evil := []string{
		"\x1b[31mrot\x1b[0m",
		"\x1b]0;pwned\x07",
		strings.Repeat("B", 10000),
		"../../etc/passwd",
		"id-0\x00",
		fmLocalConnID,
		"",
	}
	for _, value := range evil {
		for _, sides := range [][2]string{{value, value}, {value, rememberNone}, {"id-0", value}} {
			m, _ := fullPickModel(t, true, sides[0], sides[1], "A", "B")
			m = openPick(t, m)
			wantLeft := m.tr(txtFMLocalPC)
			if sides[0] == "id-0" {
				wantLeft = "A"
			}
			if got := pickHighlighted(t, m); got != wantLeft {
				t.Errorf("Werte %.12q/%.12q: links markiert %q, erwartet %q", sides[0], sides[1], got, wantLeft)
			}
			m = press(t, m, "enter")
			// Rechts ist nie etwas Gültiges gemerkt: "(keine)" (Zeile 0)
			want := m.tr(txtFMNone)
			if got := pickHighlighted(t, m); got != want {
				t.Errorf("Werte %.12q/%.12q: rechts markiert %q, erwartet %q", sides[0], sides[1], got, want)
			}
			raw := m.View()
			if strings.Contains(raw, "\x1b]0;") || strings.Contains(raw, "BBBBBBBBBB") || strings.Contains(raw, "etc/passwd") || strings.Contains(raw, "pwned") {
				t.Errorf("Werte %.12q/%.12q: gemerkter Wert erscheint in der Anzeige", sides[0], sides[1])
			}
			if m.errorMsg != "" || m.successMsg != "" {
				t.Errorf("Werte %.12q/%.12q: Meldung %q / %q", sides[0], sides[1], m.errorMsg, m.successMsg)
			}
		}
	}
	// "none" links ist ungültig (das gibt es nur rechts), "local-pc" rechts
	// bei Lokalem PC links ebenfalls: Zeile 0
	m, _ := fullPickModel(t, true, rememberNone, rememberNone, "A")
	m = openPick(t, m)
	if m.fm.pickCursor != 0 {
		t.Errorf("\"none\" links: Cursor %d, erwartet 0", m.fm.pickCursor)
	}
}

// TestFMPickAsksRightWithOneConnection (F13): Mit genau einer Verbindung wird
// rechts trotzdem gefragt - sonst gäbe es kein Kopieren zwischen Server und
// PC. Links Server: rechts (keine) und Lokaler PC. Links Lokaler PC: rechts
// (keine) und Server. Es wird erst nach der rechten Wahl verbunden.
func TestFMPickAsksRightWithOneConnection(t *testing.T) {
	m, rec := fullPickModel(t, false, "", "", "Server")
	m = openPick(t, m)
	local := m.tr(txtFMLocalPC)

	right := pickSide(t, m, "Server")
	if right.state != ViewFMPick || right.fm.pickSide != 1 || len(rec.calls) != 0 {
		t.Fatalf("links Server: state %v, Seite %d, Aufrufe %v, erwartet Frage rechts ohne Verbindung", right.state, right.fm.pickSide, rec.calls)
	}
	text := stripANSI(right.View())
	assertPickOrder(t, text, m.tr(txtFMNone), local)
	if strings.Contains(text, connLine("Server")) {
		t.Errorf("Server steht rechts zur Wahl:\n%s", text)
	}
	// Server + Lokaler PC: zwei Fenster, links Server, rechts Lokaler PC
	both := pickSide(t, right, local)
	if both.state != ViewFileManager || both.fm.single || both.fm.panels[0].conn.Name != "Server" || both.fm.panels[1].conn.ID != fmLocalConnID {
		t.Errorf("Server + Lokaler PC: state %v, single %v", both.state, both.fm.single)
	}

	rightOfLocal := pickSide(t, m, local)
	if rightOfLocal.state != ViewFMPick || rightOfLocal.fm.pickSide != 1 {
		t.Fatalf("links Lokaler PC: state %v, Seite %d", rightOfLocal.state, rightOfLocal.fm.pickSide)
	}
	text = stripANSI(rightOfLocal.View())
	assertPickOrder(t, text, m.tr(txtFMNone), connLine("Server"))
	if strings.Contains(text, local) {
		t.Errorf("Lokaler PC steht rechts noch einmal zur Wahl:\n%s", text)
	}
	// Lokaler PC + Server: zwei Fenster
	both = pickSide(t, rightOfLocal, "Server")
	if both.state != ViewFileManager || both.fm.single || both.fm.panels[0].conn.ID != fmLocalConnID || both.fm.panels[1].conn.Name != "Server" {
		t.Errorf("Lokaler PC + Server: state %v, single %v", both.state, both.fm.single)
	}
}

// TestFMPickSaveKeepsKeyPath: Hat die Schlüsselvergabe zwischen Modellstart
// und Ende der Wahl einen KeyPath gespeichert, überschreibt das Speichern der
// Wahl ihn nicht (updateConfig statt Speichern des alten Stands).
func TestFMPickSaveKeepsKeyPath(t *testing.T) {
	m, _ := fullPickModel(t, true, "", "", "A", "B")
	m.termHeight = 30
	m = openPick(t, m)
	conn := connByName(t, m, "B")
	conn.AuthType, conn.KeyPath = AuthKey, "/home/x/.ssh/id_b"
	if err := UpdateConnection(m.configPath, conn); err != nil {
		t.Fatal(err)
	}
	m = pickSide(t, m, "A")
	m = press(t, m, "home", "down", "down", "enter")
	cfg := loadTestConfig(t, m)
	if got := testConnByID(t, cfg, "id-1"); got.KeyPath != "/home/x/.ssh/id_b" || got.AuthType != AuthKey {
		t.Errorf("KeyPath nach dem Speichern der Wahl: %q / %v", got.KeyPath, got.AuthType)
	}
	if cfg.FileManagerLastLeft != "id-0" || cfg.FileManagerLastRight != "id-1" {
		t.Errorf("gemerkt %q/%q", cfg.FileManagerLastLeft, cfg.FileManagerLastRight)
	}
}

// TestFMPickSmallScreens: Die Auswahl (links und rechts, mit und ohne
// gemerkte Wahl) bricht in kleinen Terminals nichts: kein Absturz, und die
// markierte Zeile bleibt sichtbar (auch 20x8 mit der Mindestzeilenzahl).
func TestFMPickSmallScreens(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {40, 12}, {20, 8}, {0, 0}} {
		m, _ := newRememberModel(t, true, "", "id-6", "id-8", namesN(10)...)
		m.termWidth, m.termHeight = size[0], size[1]
		m = openPick(t, m)
		if got := pickHighlighted(t, m); got != "Host07" {
			t.Errorf("Größe %v: links markiert %q, erwartet Host07", size, got)
		}
		m = press(t, m, "enter")
		if got := pickHighlighted(t, m); got != "Host09" {
			t.Errorf("Größe %v: rechts markiert %q, erwartet Host09", size, got)
		}
	}
}
