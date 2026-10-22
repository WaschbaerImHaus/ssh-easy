// Paket main - Tests für den Datei-Manager (MC-Modus, Build 53)
//
// Prüft die Rohfassung des Zwei-Fenster-Datei-Managers:
//   - Eintrag "Datei-Manager" ganz unten in der Verbindungsliste
//   - Auswahl von linker und rechter Verbindung nacheinander, q = zurück
//   - Verbindungsaufbau (Auto-Auth, Passwort, Fehler) über austauschbare
//     Verbindungsfunktion - ohne echtes Netzwerk
//   - Navigation: Verzeichnis betreten, "..", fehlende Rechte,
//     Verzeichnis-Symlinks (werden nicht verfolgt), Tab, F10
//   - Hilfsfunktionen: Sortierung, Größenformat, Namensbereinigung
//   - Darstellung: Bildschirm exakt so hoch/breit wie das Terminal
//
// @author Kurt Ingwer
// @date   2026-10-21 19:53
package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Testdoubles ---

// fakeFileInfo ist eine minimale os.FileInfo für das Fake-Dateisystem.
type fakeFileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return f.modTime }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return nil }

// fakeRemoteFS bildet ein entferntes Dateisystem im Speicher nach.
// dirs: Verzeichnispfad -> Einträge; links: Symlink-Pfad -> Ziel-Info
// (nil = defektes Ziel); denied: Pfade ohne Leserecht.
type fakeRemoteFS struct {
	home   string
	dirs   map[string][]os.FileInfo
	links  map[string]os.FileInfo
	denied map[string]bool
	closed bool
	reads  []string
}

func (f *fakeRemoteFS) Getwd() (string, error) { return f.home, nil }

// RealPath - der Fake kennt keine Symlinks im Pfad.
func (f *fakeRemoteFS) RealPath(p string) (string, error) { return path.Clean(p), nil }

func (f *fakeRemoteFS) ReadDir(p string) ([]os.FileInfo, error) {
	f.reads = append(f.reads, p)
	if f.denied[p] {
		return nil, os.ErrPermission
	}
	entries, ok := f.dirs[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	return entries, nil
}

func (f *fakeRemoteFS) Stat(p string) (os.FileInfo, error) {
	if info, ok := f.links[p]; ok {
		if info == nil {
			return nil, os.ErrNotExist
		}
		return info, nil
	}
	if _, ok := f.dirs[p]; ok {
		return fakeFileInfo{name: path.Base(p), mode: fs.ModeDir | 0o755}, nil
	}
	return nil, os.ErrNotExist
}

func (f *fakeRemoteFS) Close() error {
	f.closed = true
	return nil
}

// Schreib-/Kopiermethoden: fakeRemoteFS dient nur der Navigation, das
// Kopieren testet memFS (filemanager_copy_test.go).
func (f *fakeRemoteFS) Lstat(p string) (os.FileInfo, error)      { return nil, errors.ErrUnsupported }
func (f *fakeRemoteFS) ReadLink(p string) (string, error)        { return "", errors.ErrUnsupported }
func (f *fakeRemoteFS) OpenRead(p string) (io.ReadCloser, error) { return nil, errors.ErrUnsupported }
func (f *fakeRemoteFS) CreateExclusive(p string) (io.WriteCloser, error) {
	return nil, errors.ErrUnsupported
}
func (f *fakeRemoteFS) RenameOverwrite(o, n string) error      { return errors.ErrUnsupported }
func (f *fakeRemoteFS) Mkdir(p string) error                   { return errors.ErrUnsupported }
func (f *fakeRemoteFS) Remove(p string) error                  { return errors.ErrUnsupported }
func (f *fakeRemoteFS) Symlink(target, linkPath string) error  { return errors.ErrUnsupported }
func (f *fakeRemoteFS) Chmod(p string, mode os.FileMode) error { return errors.ErrUnsupported }
func (f *fakeRemoteFS) Chtimes(p string, a, m time.Time) error { return errors.ErrUnsupported }

// dirInfo / fileInfo / linkInfo erzeugen Einträge für das Fake-FS.
func dirInfo(name string) os.FileInfo {
	return fakeFileInfo{name: name, mode: fs.ModeDir | 0o755, size: 4096}
}

func fileInfo(name string, size int64) os.FileInfo {
	return fakeFileInfo{name: name, mode: 0o644, size: size}
}

func linkInfo(name string) os.FileInfo {
	return fakeFileInfo{name: name, mode: fs.ModeSymlink | 0o777, size: 12}
}

// newFakeFS liefert ein typisches Home-Verzeichnis:
//
//	/home/kurt: projekte/, geheim/ (kein Recht), logs -> /var/log (Symlink auf
//	Verzeichnis), aktuell.txt -> notes.txt (Symlink auf Datei), kaputt ->
//	(defekt), Notes.txt, b.txt
func newFakeFS() *fakeRemoteFS {
	return &fakeRemoteFS{
		home: "/home/kurt",
		dirs: map[string][]os.FileInfo{
			"/":     {dirInfo("home"), dirInfo("var")},
			"/home": {dirInfo("kurt"), dirInfo("anna")},
			"/home/kurt": {
				fileInfo("b.txt", 10),
				dirInfo("projekte"),
				fileInfo("Notes.txt", 2048),
				dirInfo("geheim"),
				linkInfo("logs"),
				linkInfo("aktuell.txt"),
				linkInfo("kaputt"),
			},
			"/home/kurt/projekte": {fileInfo("main.go", 100)},
			"/home/kurt/geheim":   {},
			"/var/log":            {fileInfo("syslog", 1)},
		},
		links: map[string]os.FileInfo{
			"/home/kurt/logs":        dirInfo("log"),
			"/home/kurt/aktuell.txt": fileInfo("Notes.txt", 2048),
			"/home/kurt/kaputt":      nil,
		},
		denied: map[string]bool{"/home/kurt/geheim": true},
	}
}

// fmConnectRecorder ersetzt den echten Verbindungsaufbau im Test.
type fmConnectRecorder struct {
	// Ergebnis pro Verbindungs-ID; fehlt die ID: neues newFakeFS()
	results map[string]error
	// Aufrufe als "id/passwort"
	calls []string
	// Zuletzt erzeugte Dateisysteme pro ID
	created map[string]*fakeRemoteFS
}

func newConnectRecorder() *fmConnectRecorder {
	return &fmConnectRecorder{results: map[string]error{}, created: map[string]*fakeRemoteFS{}}
}

func (r *fmConnectRecorder) connect(conn Connection, password string) (remoteFS, error) {
	r.calls = append(r.calls, conn.ID+"/"+password)
	if err := r.results[conn.ID]; err != nil {
		return nil, err
	}
	fsys := newFakeFS()
	r.created[conn.ID] = fsys
	return fsys, nil
}

// runCmd führt ein Kommando aus und speist die Nachricht(en) ins Modell.
// tea.Batch-Ergebnisse werden einzeln ausgeführt; Blink-Nachrichten u. ä.
// laufen ohne Wirkung durch.
func runCmd(t *testing.T, m AppModel, cmd tea.Cmd) AppModel {
	t.Helper()
	for i := 0; cmd != nil && i < 10000; i++ {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			var next tea.Cmd
			for _, c := range batch {
				if c == nil {
					continue
				}
				inner := c()
				if isFileManagerMsg(inner) {
					updated, follow := m.Update(inner)
					m = updated.(AppModel)
					next = follow
				}
			}
			cmd = next
			continue
		}
		if !isFileManagerMsg(msg) {
			return m
		}
		updated, follow := m.Update(msg)
		m = updated.(AppModel)
		cmd = follow
	}
	return m
}

// isFileManagerMsg erkennt die asynchronen Nachrichten des Datei-Managers.
func isFileManagerMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case fmConnectedMsg, fmDirLoadedMsg, fmCopyEventMsg, fmViewLoadedMsg, fmOpenExternalMsg, fmEditLoadedMsg, fmEditSavedMsg,
		fmDeleteEventMsg, fmMkdirDoneMsg:
		return true
	}
	return false
}

// pressRun sendet eine Taste und führt das Folgekommando aus.
func pressRun(t *testing.T, m AppModel, key string) AppModel {
	t.Helper()
	updated, cmd := m.Update(fmKey(key))
	return runCmd(t, updated.(AppModel), cmd)
}

// fmKey ergänzt keyMsgFor um die F-Tasten.
func fmKey(key string) tea.KeyMsg {
	switch key {
	case "f10":
		return tea.KeyMsg{Type: tea.KeyF10}
	case "f5":
		return tea.KeyMsg{Type: tea.KeyF5}
	case "f6":
		return tea.KeyMsg{Type: tea.KeyF6}
	case "f7":
		return tea.KeyMsg{Type: tea.KeyF7}
	case "f8":
		return tea.KeyMsg{Type: tea.KeyF8}
	case "f9":
		return tea.KeyMsg{Type: tea.KeyF9}
	case "ctrl+r":
		return tea.KeyMsg{Type: tea.KeyCtrlR}
	case "f2":
		return tea.KeyMsg{Type: tea.KeyF2}
	case "f3":
		return tea.KeyMsg{Type: tea.KeyF3}
	case "f4":
		return tea.KeyMsg{Type: tea.KeyF4}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "insert":
		return tea.KeyMsg{Type: tea.KeyInsert}
	case "nul":
		// Windows: Einfg kommt als Rune 0x00 (VK_INSERT nicht gemappt)
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0}}
	}
	return keyMsgFor(key)
}

// openFileManagerFor startet den Datei-Manager mit zwei Verbindungen
// ("Links" und "Rechts") und liefert das Modell in der Zwei-Fenster-Ansicht.
func openFileManagerFor(t *testing.T) (AppModel, *fmConnectRecorder) {
	t.Helper()
	m := newListTestModel(t, "Links", "Rechts")
	rec := newConnectRecorder()
	m.fmConnect = rec.connect
	m.termWidth, m.termHeight = 100, 30
	m = openPick(t, m)
	m = pickSide(t, m, "Links")
	m = pickSide(t, m, "Rechts") // -> beide verbinden
	if m.state != ViewFileManager {
		t.Fatalf("state = %v, erwartet ViewFileManager (Fehler: %q)", m.state, m.fm.message)
	}
	return m, rec
}

// openPick öffnet aus der Liste die Verbindungsauswahl des Datei-Managers.
func openPick(t *testing.T, m AppModel) AppModel {
	t.Helper()
	m = press(t, m, "end", "enter")
	if m.state != ViewFMPick {
		t.Fatalf("state = %v, erwartet ViewFMPick", m.state)
	}
	return m
}

// pickSide wählt in der Verbindungsauswahl per Tastatur einen Eintrag
// ("" = "(nicht ausgewählt)") und führt die Folgekommandos aus.
func pickSide(t *testing.T, m AppModel, name string) AppModel {
	t.Helper()
	if m.state != ViewFMPick {
		t.Fatalf("state = %v, erwartet ViewFMPick", m.state)
	}
	steps := 0
	if name != "" {
		steps = -1
		for i, conn := range m.fmPickConnections() {
			if conn.Name == name {
				steps = i + 1
			}
		}
		if steps < 0 {
			t.Fatalf("%q steht nicht in der Auswahl", name)
		}
	}
	m = press(t, m, "home")
	for i := 0; i < steps; i++ {
		m = pressRun(t, m, "down")
	}
	return pressRun(t, m, "enter")
}

// pickNames liefert die Verbindungsnamen der aktuellen Auswahl.
func pickNames(m AppModel) []string {
	var names []string
	for _, conn := range m.fmPickConnections() {
		names = append(names, conn.Name)
	}
	return names
}

// entryNames liefert die Namen der Einträge eines Panels.
func entryNames(p filePanel) []string {
	names := make([]string, len(p.entries))
	for i, e := range p.entries {
		names[i] = e.name
	}
	return names
}

// cursorTo setzt den Cursor des aktiven Panels auf einen Eintrag.
func cursorTo(t *testing.T, m AppModel, name string) AppModel {
	t.Helper()
	p := &m.fm.panels[m.fm.active]
	for i, e := range p.entries {
		if e.name == name {
			p.cursor = i
			return m
		}
	}
	t.Fatalf("Eintrag %q nicht gefunden in %v", name, entryNames(*p))
	return m
}

// --- Verbindungsliste ---

// TestFileManagerRowIsLastListRow prüft den Eintrag ganz unten.
func TestFileManagerRowIsLastListRow(t *testing.T) {
	m := newListTestModel(t, "Abc:1", "Abc:2", "Server")
	rows := m.listRows()
	last := rows[len(rows)-1]
	if last.kind != rowFileManager {
		t.Fatalf("letzte Zeile hat Typ %v, erwartet rowFileManager", last.kind)
	}
	for _, row := range rows[:len(rows)-1] {
		if row.kind == rowFileManager {
			t.Error("Datei-Manager-Zeile darf nur einmal (ganz unten) vorkommen")
		}
	}
}

// TestFileManagerRowGreyedWithoutConnection - ohne Verbindung ist der
// Eintrag ausgegraut, zeigt den Hinweis und öffnet nichts (seit Build 70
// genügt eine Verbindung, die andere Seite kann "Lokaler PC" sein).
func TestFileManagerRowGreyedWithoutConnection(t *testing.T) {
	m := newListTestModel(t)
	rows := m.listRows()
	if len(rows) != 1 || rows[0].kind != rowFileManager {
		t.Fatalf("Zeilen %v, erwartet nur den Datei-Manager", rows)
	}
	text := stripANSI(renderListText(m))
	if !strings.Contains(text, m.tr(txtFMListEntry)) || !strings.Contains(text, m.tr(txtFMNeedConnection)) {
		t.Errorf("Eintrag/Hinweis fehlt:\n%s", text)
	}
	if !strings.Contains(text, m.lang.NoConnections) {
		t.Errorf("Hinweis \"keine Verbindungen\" fehlt:\n%s", text)
	}
	m = press(t, m, "end", "enter")
	if m.state != ViewList {
		t.Errorf("Enter öffnet den Datei-Manager (state %v)", m.state)
	}
	// Auch e/d/x tun nichts
	for _, key := range []string{"e", "d", "x"} {
		if got := press(t, m, key); got.state != ViewList {
			t.Errorf("Taste %q auf ausgegrautem Eintrag wechselt nach %v", key, got.state)
		}
	}
}

// TestFileManagerRowEnabledFromTwoConnections - ab zwei Verbindungen kein
// Hinweis, Enter öffnet die Auswahl.
func TestFileManagerRowEnabledFromTwoConnections(t *testing.T) {
	m := newListTestModel(t, "A", "B")
	text := stripANSI(renderListText(m))
	if strings.Contains(text, m.tr(txtFMNeedConnection)) {
		t.Errorf("Hinweis trotz zwei Verbindungen:\n%s", text)
	}
	m = press(t, m, "end", "enter")
	if m.state != ViewFMPick {
		t.Errorf("state %v, erwartet ViewFMPick", m.state)
	}
}

// TestFileManagerRowGreyedStyle - ausgegraut in grauer Schrift (auch als
// Cursorzeile), aktiv in normaler Schrift.
func TestFileManagerRowGreyedStyle(t *testing.T) {
	withColorProfile(t)
	grey := "38;5;" + fmDisabledFg
	m := newListTestModel(t)
	line := selectedLine(t, renderListText(m), m.tr(txtFMListEntry))
	if !strings.Contains(line, grey) {
		t.Errorf("nicht ausgegraut: %q", line)
	}
	m = press(t, m, "end")
	line = selectedLine(t, renderListText(m), m.tr(txtFMListEntry))
	if !strings.Contains(line, grey) {
		t.Errorf("als Cursorzeile nicht ausgegraut: %q", line)
	}
	m = newListTestModel(t, "A")
	line = selectedLine(t, renderListText(m), m.tr(txtFMListEntry))
	if strings.Contains(line, grey) {
		t.Errorf("trotz Verbindung ausgegraut: %q", line)
	}
}

// TestFileManagerRowSeparatedByBlankLine prüft die Leerzeile davor.
func TestFileManagerRowSeparatedByBlankLine(t *testing.T) {
	m := newListTestModel(t, "Server")
	lines := strings.Split(stripANSI(renderListText(m)), "\n")
	entry := m.tr(txtFMListEntry)
	for i, line := range lines {
		if strings.Contains(line, entry) {
			if i == 0 || strings.TrimSpace(lines[i-1]) != "" {
				t.Errorf("vor %q fehlt die Leerzeile:\n%s", entry, strings.Join(lines, "\n"))
			}
			if !strings.Contains(lines[i-2], "Server") {
				t.Errorf("Leerzeile steht nicht direkt nach der letzten Verbindung:\n%s", strings.Join(lines, "\n"))
			}
			return
		}
	}
	t.Fatalf("Eintrag %q nicht gefunden:\n%s", entry, strings.Join(lines, "\n"))
}

// TestFileManagerRowIgnoresConnectionKeys - e/d/x wirken nicht auf den Eintrag.
func TestFileManagerRowIgnoresConnectionKeys(t *testing.T) {
	m := newListTestModel(t, "Server")
	m = press(t, m, "end")
	if _, ok := m.selectedConnection(); ok {
		t.Fatal("Datei-Manager-Zeile darf keine Verbindung liefern")
	}
	for _, key := range []string{"e", "d", "x"} {
		if got := press(t, m, key); got.state != ViewList {
			t.Errorf("Taste %q auf Datei-Manager-Zeile wechselt nach %v", key, got.state)
		}
	}
}

// TestSelectConnectionByIDSkipsFileManagerRow - darf nicht abstürzen
// (Datei-Manager-Zeile hat keinen Verbindungsindex).
func TestSelectConnectionByIDSkipsFileManagerRow(t *testing.T) {
	m := newListTestModel(t, "A", "B")
	m.selectConnectionByID("unbekannt")
	m.selectConnectionByID("id-1")
	if conn, ok := m.selectedConnection(); !ok || conn.ID != "id-1" {
		t.Errorf("Auswahl = %v/%v, erwartet id-1", conn.ID, ok)
	}
}

// --- Verbindungsauswahl ---

// TestFileManagerPickBothSides prüft Auswahl links, dann rechts.
func TestFileManagerPickBothSides(t *testing.T) {
	m := newListTestModel(t, "Links", "Rechts")
	m.fmConnect = newConnectRecorder().connect
	m = openPick(t, m)
	if m.fm.pickSide != 0 {
		t.Fatalf("Seite %d, erwartet links", m.fm.pickSide)
	}
	text := stripANSI(m.View())
	if !strings.Contains(text, m.tr(txtFMPickLeft)) {
		t.Errorf("Titel für linke Seite fehlt:\n%s", text)
	}
	// Auswahl ist flach: Gruppen werden nicht zusammengeklappt, Tunnel egal
	if !strings.Contains(text, "Links") || !strings.Contains(text, "Rechts") {
		t.Errorf("Verbindungen fehlen in der Auswahl:\n%s", text)
	}
	// Erster Punkt ist "(nicht ausgewählt)", der Cursor steht darauf
	lines := strings.Split(text, "\n")
	first := -1
	for i, line := range lines {
		if strings.Contains(line, m.tr(txtFMNotSelected)) {
			first = i
			break
		}
	}
	if first < 0 || first > strings.Index(text, "Links") || m.fm.pickCursor != 0 {
		t.Errorf("\"(nicht ausgewählt)\" ist nicht der erste, gewählte Punkt:\n%s", text)
	}
	if strings.Index(text, m.tr(txtFMNotSelected)) > strings.Index(text, "Links") {
		t.Errorf("\"(nicht ausgewählt)\" steht nicht vor den Verbindungen:\n%s", text)
	}

	// Zeile 1 ist "Lokaler PC", Zeile 2 die erste Verbindung
	m = press(t, m, "down", "down", "enter")
	if m.state != ViewFMPick || m.fm.pickSide != 1 {
		t.Fatalf("nach erster Wahl: state = %v/Seite %d, erwartet rechte Seite", m.state, m.fm.pickSide)
	}
	if !strings.Contains(stripANSI(m.View()), m.tr(txtFMPickRight)) {
		t.Error("Titel für rechte Seite fehlt")
	}
	if m.fm.panels[0].conn.Name != "Links" {
		t.Errorf("linke Verbindung = %q, erwartet Links", m.fm.panels[0].conn.Name)
	}
}

// TestFileManagerPickShowsGroupedNamesFlat - Gruppenmitglieder erscheinen
// mit vollem Namen, auch wenn die Gruppe in der Liste zugeklappt ist.
func TestFileManagerPickShowsGroupedNamesFlat(t *testing.T) {
	m := newListTestModel(t, "Büro:Server1", "Büro:Server2")
	m = openPick(t, m)
	text := stripANSI(m.View())
	for _, name := range []string{"Büro:Server1", "Büro:Server2"} {
		if !strings.Contains(text, name) {
			t.Errorf("%q fehlt in der Auswahl:\n%s", name, text)
		}
	}
}

// TestFileManagerPickQuitReturnsToFileManagerRow - q bricht ab.
func TestFileManagerPickQuitReturnsToFileManagerRow(t *testing.T) {
	m := newListTestModel(t, "Links", "Rechts")
	for _, keys := range [][]string{{"q"}, {"down", "enter", "q"}, {"enter", "q"}, {"esc"}} {
		got := press(t, m, "end", "enter")
		got = press(t, got, keys...)
		if got.state != ViewList {
			t.Errorf("Tasten %v: state = %v, erwartet ViewList", keys, got.state)
			continue
		}
		rows := got.listRows()
		if rows[got.cursor].kind != rowFileManager {
			t.Errorf("Tasten %v: Cursor steht nicht auf dem Datei-Manager-Eintrag", keys)
		}
	}
}

// TestFileManagerPickExcludesLeftConnection - eine Verbindung darf nicht
// mit sich selbst verbunden werden: Die links gewählte fehlt rechts.
func TestFileManagerPickExcludesLeftConnection(t *testing.T) {
	m := newListTestModel(t, "A", "B", "C")
	m.fmConnect = newConnectRecorder().connect
	m = openPick(t, m)
	local := m.tr(txtFMLocalPC)
	if got := strings.Join(pickNames(m), ","); got != local+",A,B,C" {
		t.Fatalf("links: Auswahl = %s, erwartet %s,A,B,C", got, local)
	}
	m = pickSide(t, m, "B")
	if got := strings.Join(pickNames(m), ","); got != local+",A,C" {
		t.Errorf("rechts: Auswahl = %s, erwartet %s,A,C (ohne die linke B)", got, local)
	}
	text := stripANSI(m.View())
	if strings.Contains(text, "  B  (") {
		t.Errorf("linke Verbindung B steht rechts noch zur Wahl:\n%s", text)
	}
	if !strings.Contains(text, m.tr(txtFMNotSelected)) {
		t.Errorf("\"(nicht ausgewählt)\" fehlt rechts:\n%s", text)
	}
	// Cursor beginnt rechts wieder oben auf "(nicht ausgewählt)"
	if m.fm.pickCursor != 0 {
		t.Errorf("Cursor rechts = %d, erwartet 0", m.fm.pickCursor)
	}
}

// TestFileManagerPickPlaceholderForcesChoice - "(nicht ausgewählt)" ist
// nur ein Platzhalter: Enter darauf bewirkt nichts, damit man nicht mit
// Enter-Enter versehentlich im Datei-Manager landet.
func TestFileManagerPickPlaceholderForcesChoice(t *testing.T) {
	m := newListTestModel(t, "Links", "Rechts")
	rec := newConnectRecorder()
	m.fmConnect = rec.connect
	m = openPick(t, m)
	for i := 0; i < 3; i++ {
		updated, cmd := m.Update(keyMsgFor("enter"))
		m = updated.(AppModel)
		if cmd != nil || m.state != ViewFMPick || m.fm.pickSide != 0 {
			t.Fatalf("Enter auf Platzhalter links: state %v, Seite %d", m.state, m.fm.pickSide)
		}
	}
	m = pickSide(t, m, "Links")
	if m.fm.pickSide != 1 {
		t.Fatalf("nach Wahl links: Seite %d, erwartet rechts", m.fm.pickSide)
	}
	for i := 0; i < 3; i++ {
		updated, cmd := m.Update(keyMsgFor("enter"))
		m = updated.(AppModel)
		if cmd != nil || m.state != ViewFMPick || m.fm.pickSide != 1 {
			t.Fatalf("Enter auf Platzhalter rechts: state %v, Seite %d", m.state, m.fm.pickSide)
		}
	}
	if len(rec.calls) != 0 {
		t.Errorf("Verbindungsaufbau ohne Wahl: %v", rec.calls)
	}
	m = pickSide(t, m, "Rechts")
	if m.state != ViewFileManager {
		t.Errorf("nach bewusster Wahl: state %v, erwartet ViewFileManager", m.state)
	}
}

// --- Verbindungsaufbau ---

// TestFileManagerConnectsBothWithoutTunnels prüft den Aufbau.
func TestFileManagerConnectsBothWithoutTunnels(t *testing.T) {
	m, rec := openFileManagerFor(t)
	if want := []string{"id-0/", "id-1/"}; strings.Join(rec.calls, ",") != strings.Join(want, ",") {
		t.Errorf("Aufrufe = %v, erwartet %v", rec.calls, want)
	}
	for side, p := range m.fm.panels {
		if p.path != "/home/kurt" {
			t.Errorf("Seite %d: Pfad = %q, erwartet Home /home/kurt", side, p.path)
		}
		if len(p.entries) == 0 || p.entries[0].name != ".." {
			t.Errorf("Seite %d: erster Eintrag = %v, erwartet ..", side, entryNames(p))
		}
	}
	if m.fm.active != 0 {
		t.Errorf("aktive Seite = %d, erwartet links (0)", m.fm.active)
	}
	// Datei-Manager-Verbindungen gehören nicht in den normalen Manager
	if m.sshManager.IsConnected("id-0") || m.sshManager.IsConnected("id-1") {
		t.Error("Datei-Manager-Verbindungen dürfen nicht als normale Verbindung gelten")
	}
}

// TestFileManagerConnectionStripsTunnels prüft, dass die Verbindungsdaten
// für den Datei-Manager keine Tunnel enthalten.
func TestFileManagerConnectionStripsTunnels(t *testing.T) {
	m := newListTestModel(t, "Server", "Zweit")
	m.connections[0].Tunnels = []TunnelConfig{{LocalPort: 3306, RemotePort: 3306, Enabled: true}}
	var seen []Connection
	m.fmConnect = func(conn Connection, password string) (remoteFS, error) {
		seen = append(seen, conn)
		return newFakeFS(), nil
	}
	m = openPick(t, m)
	m = pickSide(t, m, "Server")
	m = pickSide(t, m, "Zweit")
	if len(seen) != 2 {
		t.Fatalf("Verbindungen = %d, erwartet 2", len(seen))
	}
	for _, conn := range seen {
		if len(conn.Tunnels) != 0 {
			t.Errorf("Datei-Manager-Verbindung hat Tunnel: %v", conn.Tunnels)
		}
	}
	if len(m.connections[0].Tunnels) != 1 {
		t.Error("Tunnel der gespeicherten Verbindung dürfen nicht verändert werden")
	}
}

// TestFileManagerAsksPasswordAfterAuthFailure prüft die Passwortabfrage.
func TestFileManagerAsksPasswordAfterAuthFailure(t *testing.T) {
	m := newListTestModel(t, "Links", "Rechts")
	rec := newConnectRecorder()
	authFailed := errors.New("ssh: unable to authenticate")
	rec.results["id-1"] = authFailed
	m.fmConnect = rec.connect
	m = openPick(t, m)
	m = pickSide(t, m, "Links")
	m = pickSide(t, m, "Rechts")
	if m.state != ViewFMPassword || m.fm.passwordSide != 1 {
		t.Fatalf("state = %v/Seite %d, erwartet Passwortabfrage rechts", m.state, m.fm.passwordSide)
	}
	if !strings.Contains(stripANSI(m.View()), "Rechts") {
		t.Error("Passwortabfrage nennt die Verbindung nicht")
	}
	// q ist im Passwortfeld ein normales Zeichen
	m = press(t, m, "q")
	if m.state != ViewFMPassword {
		t.Fatalf("q im Passwortfeld bricht ab (state %v)", m.state)
	}
	// Passwort stimmt -> verbunden
	delete(rec.results, "id-1")
	m = press(t, m, "x")
	m = pressRun(t, m, "enter")
	if m.state != ViewFileManager {
		t.Fatalf("state = %v, erwartet ViewFileManager", m.state)
	}
	if got := rec.calls[len(rec.calls)-1]; got != "id-1/qx" {
		t.Errorf("letzter Aufruf = %q, erwartet id-1/qx", got)
	}
}

// TestFileManagerWrongPasswordAsksAgain - falsches Passwort: erneut fragen.
func TestFileManagerWrongPasswordAsksAgain(t *testing.T) {
	m := newListTestModel(t, "Server", "Zweit")
	rec := newConnectRecorder()
	rec.results["id-0"] = errors.New("ssh: unable to authenticate")
	m.fmConnect = rec.connect
	m = openPick(t, m)
	m = pickSide(t, m, "Server")
	m = pickSide(t, m, "Zweit")
	m = press(t, m, "a")
	m = pressRun(t, m, "enter")
	if m.state != ViewFMPassword {
		t.Fatalf("state = %v, erwartet erneute Passwortabfrage", m.state)
	}
	if m.fm.message == "" {
		t.Error("falsches Passwort ohne Meldung")
	}
	if m.fm.passwordInput.Value() != "" {
		t.Error("Passwortfeld muss nach Fehlversuch leer sein")
	}
}

// TestFileManagerPasswordEscAborts - Esc bricht die Passwortabfrage ab und
// schließt die bereits aufgebaute linke Verbindung.
func TestFileManagerPasswordEscAborts(t *testing.T) {
	m := newListTestModel(t, "Links", "Rechts")
	rec := newConnectRecorder()
	rec.results["id-1"] = errors.New("ssh: unable to authenticate")
	m.fmConnect = rec.connect
	m = openPick(t, m)
	m = pickSide(t, m, "Links")
	m = pickSide(t, m, "Rechts")
	m = press(t, m, "esc")
	if m.state != ViewList {
		t.Fatalf("state = %v, erwartet ViewList", m.state)
	}
	if !rec.created["id-0"].closed {
		t.Error("linke Verbindung wurde beim Abbruch nicht geschlossen")
	}
}

// TestFileManagerNetworkErrorReturnsToList - Server nicht erreichbar.
func TestFileManagerNetworkErrorReturnsToList(t *testing.T) {
	m := newListTestModel(t, "Links", "Rechts")
	rec := newConnectRecorder()
	rec.results["id-1"] = &networkError{err: errors.New("dial tcp 10.0.0.1:22: i/o timeout")}
	m.fmConnect = rec.connect
	m = openPick(t, m)
	m = pickSide(t, m, "Links")
	m = pickSide(t, m, "Rechts")
	if m.state != ViewList {
		t.Fatalf("state = %v, erwartet ViewList", m.state)
	}
	if m.errorMsg == "" || !strings.Contains(m.errorMsg, "Rechts") {
		t.Errorf("Fehlermeldung = %q, erwartet Hinweis auf Rechts", m.errorMsg)
	}
	if !rec.created["id-0"].closed {
		t.Error("linke Verbindung wurde nicht geschlossen")
	}
	if rows := m.listRows(); rows[m.cursor].kind != rowFileManager {
		t.Error("Cursor steht nicht auf dem Datei-Manager-Eintrag")
	}
}

// TestFileManagerHostKeyChangedReturnsToList - geänderter Host-Key wird
// nicht im Datei-Manager bestätigt, sondern gemeldet.
func TestFileManagerHostKeyChangedReturnsToList(t *testing.T) {
	m := newListTestModel(t, "Server", "Zweit")
	rec := newConnectRecorder()
	rec.results["id-0"] = &HostKeyChangedError{Hostname: "10.0.0.1"}
	m.fmConnect = rec.connect
	m = openPick(t, m)
	m = pickSide(t, m, "Server")
	m = pickSide(t, m, "Zweit")
	if m.state != ViewList {
		t.Fatalf("state = %v, erwartet ViewList", m.state)
	}
	if !strings.Contains(m.errorMsg, "10.0.0.1") {
		t.Errorf("Fehlermeldung = %q, erwartet Host", m.errorMsg)
	}
}

// TestFileManagerQuitWhileConnectingClosesLateResult - q während des
// Verbindungsaufbaus: später eintreffende Verbindung wird geschlossen.
func TestFileManagerQuitWhileConnectingClosesLateResult(t *testing.T) {
	m := newListTestModel(t, "Links", "Rechts")
	m.fmConnect = newConnectRecorder().connect
	m = openPick(t, m)
	m = pickSide(t, m, "Links")
	m = press(t, m, "down") // "Rechts"
	updated, cmd := m.Update(keyMsgFor("enter"))
	m = updated.(AppModel)
	if m.state != ViewFMConnecting {
		t.Fatalf("state = %v, erwartet ViewFMConnecting", m.state)
	}
	m = press(t, m, "q")
	if m.state != ViewList {
		t.Fatalf("q beim Verbinden: state = %v, erwartet ViewList", m.state)
	}
	msg := cmd()
	connected, ok := msg.(fmConnectedMsg)
	if !ok {
		t.Fatalf("Nachricht %T, erwartet fmConnectedMsg", msg)
	}
	updated, _ = m.Update(connected)
	m = updated.(AppModel)
	if m.state != ViewList {
		t.Errorf("veraltete Verbindung öffnet den Datei-Manager (state %v)", m.state)
	}
	if !connected.fs.(*fakeRemoteFS).closed {
		t.Error("veraltete Verbindung wurde nicht geschlossen")
	}
}

// --- Navigation ---

// TestFileManagerSortsEntries prüft: "..", Verzeichnisse (inkl. Symlinks
// auf Verzeichnisse), dann Dateien - jeweils ohne Groß-/Kleinschreibung.
func TestFileManagerSortsEntries(t *testing.T) {
	m, _ := openFileManagerFor(t)
	got := entryNames(m.fm.panels[0])
	want := []string{"..", "geheim", "logs", "projekte", "aktuell.txt", "b.txt", "kaputt", "Notes.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Reihenfolge = %v, erwartet %v", got, want)
	}
}

// TestFileManagerEnterDirectory prüft das Betreten eines Verzeichnisses.
func TestFileManagerEnterDirectory(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = cursorTo(t, m, "projekte")
	m = pressRun(t, m, "enter")
	p := m.fm.panels[0]
	if p.path != "/home/kurt/projekte" {
		t.Fatalf("Pfad = %q, erwartet /home/kurt/projekte", p.path)
	}
	if p.cursor != 0 {
		t.Errorf("Cursor = %d, erwartet 0", p.cursor)
	}
	if got := entryNames(p); strings.Join(got, ",") != "..,main.go" {
		t.Errorf("Einträge = %v", got)
	}
	// Rechte Seite bleibt unverändert
	if m.fm.panels[1].path != "/home/kurt" {
		t.Errorf("rechte Seite hat sich verändert: %q", m.fm.panels[1].path)
	}
}

// TestFileManagerParentKeepsCursorOnPreviousDir prüft ".." wie im mc:
// Der Cursor steht danach auf dem Verzeichnis, aus dem man kam.
func TestFileManagerParentKeepsCursorOnPreviousDir(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = cursorTo(t, m, "..")
	m = pressRun(t, m, "enter")
	p := m.fm.panels[0]
	if p.path != "/home" {
		t.Fatalf("Pfad = %q, erwartet /home", p.path)
	}
	if p.entries[p.cursor].name != "kurt" {
		t.Errorf("Cursor auf %q, erwartet kurt", p.entries[p.cursor].name)
	}
}

// TestFileManagerRootHasNoParentEntry - im Wurzelverzeichnis kein "..".
func TestFileManagerRootHasNoParentEntry(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = cursorTo(t, m, "..")
	m = pressRun(t, m, "enter")
	m = cursorTo(t, m, "..")
	m = pressRun(t, m, "enter")
	p := m.fm.panels[0]
	if p.path != "/" {
		t.Fatalf("Pfad = %q, erwartet /", p.path)
	}
	for _, name := range entryNames(p) {
		if name == ".." {
			t.Error("Wurzelverzeichnis darf kein .. haben")
		}
	}
	if p.entries[p.cursor].name != "home" {
		t.Errorf("Cursor auf %q, erwartet home", p.entries[p.cursor].name)
	}
}

// TestFileManagerPermissionDeniedStays - ohne Rechte bleibt man im
// bisherigen Verzeichnis und bekommt eine Meldung.
func TestFileManagerPermissionDeniedStays(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = cursorTo(t, m, "geheim")
	before := m.fm.panels[0]
	m = pressRun(t, m, "enter")
	p := m.fm.panels[0]
	if p.path != "/home/kurt" {
		t.Errorf("Pfad = %q, erwartet unverändert /home/kurt", p.path)
	}
	if p.entries[p.cursor].name != "geheim" {
		t.Errorf("Cursor auf %q, erwartet geheim", p.entries[p.cursor].name)
	}
	if len(p.entries) != len(before.entries) {
		t.Error("Liste hat sich verändert")
	}
	if !strings.Contains(m.fm.message, "/home/kurt/geheim") {
		t.Errorf("Meldung = %q, erwartet Pfad", m.fm.message)
	}
	want := fmt.Sprintf(m.tr(txtFMErrPermission), "/home/kurt/geheim")
	if m.fm.message != want {
		t.Errorf("Meldung = %q, erwartet %q", m.fm.message, want)
	}
}

// TestFileManagerSymlinkDirNotFollowed - Symlink auf Verzeichnis: Meldung,
// kein Verzeichniswechsel, kein Lesezugriff auf das Ziel.
func TestFileManagerSymlinkDirNotFollowed(t *testing.T) {
	m, rec := openFileManagerFor(t)
	m = cursorTo(t, m, "logs")
	m = pressRun(t, m, "enter")
	if p := m.fm.panels[0]; p.path != "/home/kurt" {
		t.Errorf("Pfad = %q, Symlink wurde verfolgt", p.path)
	}
	if want := fmt.Sprintf(m.tr(txtFMErrSymlink), "logs"); m.fm.message != want {
		t.Errorf("Meldung = %q, erwartet %q", m.fm.message, want)
	}
	for _, read := range rec.created["id-0"].reads {
		if read == "/home/kurt/logs" || read == "/var/log" {
			t.Errorf("Symlink-Ziel wurde gelesen: %s", read)
		}
	}
}

// TestFileManagerEnterOnFileDoesNothing - Enter auf Datei / Datei-Symlink /
// defektem Symlink wechselt nichts (Ansehen kommt später mit F3).
func TestFileManagerEnterOnFileDoesNothing(t *testing.T) {
	for _, name := range []string{"b.txt", "aktuell.txt", "kaputt"} {
		m, _ := openFileManagerFor(t)
		m = cursorTo(t, m, name)
		m = pressRun(t, m, "enter")
		if p := m.fm.panels[0]; p.path != "/home/kurt" || p.entries[p.cursor].name != name {
			t.Errorf("%s: Pfad %q / Cursor %q verändert", name, p.path, p.entries[p.cursor].name)
		}
	}
}

// TestFileManagerMessageClearedOnNextKey - Meldung verschwindet beim
// nächsten Tastendruck.
func TestFileManagerMessageClearedOnNextKey(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = cursorTo(t, m, "logs")
	m = pressRun(t, m, "enter")
	if m.fm.message == "" {
		t.Fatal("keine Meldung")
	}
	m = pressRun(t, m, "down")
	if m.fm.message != "" {
		t.Errorf("Meldung bleibt stehen: %q", m.fm.message)
	}
}

// TestFileManagerTabSwitchesSide prüft den Seitenwechsel.
func TestFileManagerTabSwitchesSide(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = pressRun(t, m, "tab")
	if m.fm.active != 1 {
		t.Fatalf("aktive Seite = %d, erwartet 1", m.fm.active)
	}
	m = pressRun(t, m, "down")
	if m.fm.panels[1].cursor != 1 || m.fm.panels[0].cursor != 0 {
		t.Errorf("Cursor links %d / rechts %d, erwartet 0 / 1", m.fm.panels[0].cursor, m.fm.panels[1].cursor)
	}
	m = pressRun(t, m, "tab")
	if m.fm.active != 0 {
		t.Errorf("aktive Seite = %d, erwartet 0", m.fm.active)
	}
}

// TestFileManagerCursorKeys prüft Pfeile, Pos1/Ende und Bildtasten.
func TestFileManagerCursorKeys(t *testing.T) {
	m, _ := openFileManagerFor(t)
	last := len(m.fm.panels[0].entries) - 1
	steps := []struct {
		key  string
		want int
	}{
		{"up", 0}, {"down", 1}, {"end", last}, {"down", last}, {"home", 0},
		{"pgdown", last}, {"pgup", 0},
	}
	for _, step := range steps {
		m = pressRun(t, m, step.key)
		if got := m.fm.panels[0].cursor; got != step.want {
			t.Errorf("nach %q: Cursor = %d, erwartet %d", step.key, got, step.want)
		}
	}
}

// TestFileManagerScrollsLongDirectory prüft das Scrollen langer Listen.
func TestFileManagerScrollsLongDirectory(t *testing.T) {
	m, rec := openFileManagerFor(t)
	var many []os.FileInfo
	for i := 0; i < 200; i++ {
		many = append(many, fileInfo(fmt.Sprintf("datei%03d.txt", i), int64(i)))
	}
	rec.created["id-0"].dirs["/home/kurt/projekte"] = many
	m = cursorTo(t, m, "projekte")
	m = pressRun(t, m, "enter")
	m = pressRun(t, m, "end")
	text := stripANSI(m.View())
	if !strings.Contains(text, "datei199.txt") {
		t.Errorf("letzter Eintrag nach Ende nicht sichtbar:\n%s", text)
	}
	if strings.Contains(text, "datei000.txt") {
		t.Error("erster Eintrag dürfte nach Ende nicht mehr sichtbar sein")
	}
	assertScreenSize(t, m)
}

// TestFileManagerF10ReturnsToList - F10 schließt beide Verbindungen und
// kehrt mit Cursor auf dem Datei-Manager-Eintrag zur Liste zurück.
func TestFileManagerF10ReturnsToList(t *testing.T) {
	for _, key := range []string{"f10", "q"} {
		m, rec := openFileManagerFor(t)
		m = pressRun(t, m, key)
		if m.state != ViewList {
			t.Fatalf("%s: state = %v, erwartet ViewList", key, m.state)
		}
		if rows := m.listRows(); rows[m.cursor].kind != rowFileManager {
			t.Errorf("%s: Cursor steht nicht auf dem Datei-Manager-Eintrag", key)
		}
		for id, fsys := range rec.created {
			if !fsys.closed {
				t.Errorf("%s: Verbindung %s nicht geschlossen", key, id)
			}
		}
	}
}

// TestFileManagerOtherFKeysNotYet - noch nicht umgesetzte F-Tasten melden
// sich, ändern aber nichts.
func TestFileManagerOtherFKeysNotYet(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = pressRun(t, m, "f2")
	if m.state != ViewFileManager {
		t.Fatalf("state = %v", m.state)
	}
	if m.fm.message != m.tr(txtFMNotYet) {
		t.Errorf("Meldung = %q, erwartet Hinweis 'noch nicht verfügbar'", m.fm.message)
	}
}

// TestFileManagerStaleDirResultIgnored - Ergebnis einer alten Sitzung
// (nach F10) darf den Zustand nicht verändern.
func TestFileManagerStaleDirResultIgnored(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = cursorTo(t, m, "projekte")
	updated, cmd := m.Update(keyMsgFor("enter"))
	m = updated.(AppModel)
	m = pressRun(t, m, "f10")
	updated, _ = m.Update(cmd())
	m = updated.(AppModel)
	if m.state != ViewList {
		t.Errorf("veraltetes Verzeichnis-Ergebnis ändert die Ansicht: %v", m.state)
	}
}

// TestFileManagerIgnoresKeysWhileLoading - während ein Verzeichnis lädt,
// startet Enter keinen zweiten Wechsel.
func TestFileManagerIgnoresKeysWhileLoading(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = cursorTo(t, m, "projekte")
	updated, cmd := m.Update(keyMsgFor("enter"))
	m = updated.(AppModel)
	if !m.fm.panels[0].loading {
		t.Fatal("Panel lädt nicht")
	}
	updated, second := m.Update(keyMsgFor("enter"))
	m = updated.(AppModel)
	if second != nil {
		t.Error("zweites Enter während des Ladens startet einen weiteren Wechsel")
	}
	m = runCmd(t, m, cmd)
	if m.fm.panels[0].path != "/home/kurt/projekte" {
		t.Errorf("Pfad = %q", m.fm.panels[0].path)
	}
}

// --- Darstellung ---

// assertScreenSize prüft: genau termHeight Zeilen, jede genau termWidth breit.
func assertScreenSize(t *testing.T, m AppModel) {
	t.Helper()
	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.termHeight {
		t.Errorf("Zeilen = %d, erwartet %d", len(lines), m.termHeight)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w != m.termWidth {
			t.Errorf("Zeile %d ist %d breit, erwartet %d: %q", i, w, m.termWidth, stripANSI(line))
		}
	}
}

// TestFileManagerScreenLayout prüft Menüleiste, Panels und F-Tasten-Leiste.
func TestFileManagerScreenLayout(t *testing.T) {
	m, _ := openFileManagerFor(t)
	for _, size := range [][2]int{{100, 30}, {81, 24}, {80, 25}, {40, 12}, {200, 60}} {
		m.termWidth, m.termHeight = size[0], size[1]
		assertScreenSize(t, m)
	}
	m.termWidth, m.termHeight = 100, 30
	lines := strings.Split(stripANSI(m.View()), "\n")
	menu := strings.Split(m.tr(txtFMMenuBar), "|")
	for _, item := range menu {
		if !strings.Contains(lines[0], item) {
			t.Errorf("Menüleiste ohne %q: %q", item, lines[0])
		}
	}
	bottom := lines[len(lines)-1]
	if !strings.HasPrefix(bottom, "1") || !strings.Contains(bottom, "10") {
		t.Errorf("F-Tasten-Leiste unerwartet: %q", bottom)
	}
	for _, label := range strings.Split(m.tr(txtFMFKeys), "|") {
		if !strings.Contains(bottom, label) {
			t.Errorf("F-Tasten-Leiste ohne %q: %q", label, bottom)
		}
	}
	if !strings.Contains(lines[1], "root@10.0.0.1:/home/kurt") {
		t.Errorf("Panel-Titel fehlt: %q", lines[1])
	}
}

// TestFileManagerUnknownSizeFallback - ohne Fenstergröße 80x24.
func TestFileManagerUnknownSizeFallback(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m.termWidth, m.termHeight = 0, 0
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 24 || lipgloss.Width(lines[0]) != 80 {
		t.Errorf("Fallback = %dx%d, erwartet 80x24", lipgloss.Width(lines[0]), len(lines))
	}
}

// TestFileManagerShowsMessage - Meldungen erscheinen auf dem Bildschirm.
func TestFileManagerShowsMessage(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = cursorTo(t, m, "logs")
	m = pressRun(t, m, "enter")
	// Meldung kann gekürzt werden - der Anfang muss sichtbar sein
	text := stripANSI(m.View())
	if !strings.Contains(text, m.fm.message[:20]) {
		t.Errorf("Meldung nicht sichtbar:\n%s", text)
	}
	assertScreenSize(t, m)
}

// TestFileManagerAllThemesRender - alle Farbmodi erzeugen ein gültiges Bild.
func TestFileManagerAllThemesRender(t *testing.T) {
	withColorProfile(t)
	m, _ := openFileManagerFor(t)
	for i := range fmThemes {
		m.fm.theme = i
		assertScreenSize(t, m)
	}
	if len(fmThemes) != 3 {
		t.Errorf("Farbmodi = %d, erwartet 3 (klassisch, ssh-easy, monochrom)", len(fmThemes))
	}
}

// --- Hilfsfunktionen ---

// TestSanitizeFileName - Steuerzeichen aus fremden Dateinamen dürfen das
// Terminal nicht steuern (z. B. ESC-Sequenzen im Namen).
func TestSanitizeFileName(t *testing.T) {
	cases := map[string]string{
		"normal.txt":      "normal.txt",
		"böse\x1b[2J.txt": "böse?[2J.txt",
		"zeile\numbruch":  "zeile?umbruch",
		"tab\there":       "tab?here",
		"c1\u009bsequenz": "c1?sequenz",
		"del\x7f":         "del?",
		"ungültig\xff":    "ungültig?",
		"日本語.txt":         "日本語.txt",
		// Unsichtbare Formatzeichen (Täuschung durch Richtungsumkehr u. ä.)
		"rechnung\u202efdp.exe": "rechnung?fdp.exe",
		"a\u200bb":              "a?b",
		"\u2066x\u2069":         "?x?",
		"\ufeffbom":             "?bom",
		// Joiner sind Teil normaler Schrift (z. B. Persisch) und bleiben
		"می\u200cخواهم": "می\u200cخواهم",
	}
	for in, want := range cases {
		if got := sanitizeFileName(in); got != want {
			t.Errorf("sanitizeFileName(%q) = %q, erwartet %q", in, got, want)
		}
	}
}

// TestFormatFileSize prüft die Größenanzeige (max. 7 Zeichen).
func TestFormatFileSize(t *testing.T) {
	cases := map[int64]string{
		0:             "0",
		999:           "999",
		9999999:       "9999999",
		10000000:      "9765K",
		1048576 * 500: "500M",
		1 << 40:       "1024G",
		-1:            "0",
	}
	for in, want := range cases {
		got := formatFileSize(in)
		if got != want {
			t.Errorf("formatFileSize(%d) = %q, erwartet %q", in, got, want)
		}
		if len(got) > 7 {
			t.Errorf("formatFileSize(%d) = %q ist länger als 7 Zeichen", in, got)
		}
	}
}

// TestParentDir prüft die Elternverzeichnisse entfernter (POSIX-)Pfade -
// unabhängig vom lokalen Betriebssystem (auch unter Windows "/").
func TestParentDir(t *testing.T) {
	cases := map[string]string{
		"/home/kurt": "/home",
		"/home":      "/",
		"/":          "/",
		"/a/b/c/":    "/a/b",
		"":           "/",
	}
	for in, want := range cases {
		if got := remoteParentDir(in); got != want {
			t.Errorf("remoteParentDir(%q) = %q, erwartet %q", in, got, want)
		}
	}
}

// TestReadPanelDirMarksEntryKinds prüft die Einordnung der Einträge.
func TestReadPanelDirMarksEntryKinds(t *testing.T) {
	entries, err := readPanelDir(newFakeFS(), "/home/kurt")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]fmEntry{}
	for _, e := range entries {
		kinds[e.name] = e
	}
	if e := kinds["logs"]; !e.symlink || !e.dir || e.broken {
		t.Errorf("logs = %+v, erwartet Symlink auf Verzeichnis", e)
	}
	if e := kinds["aktuell.txt"]; !e.symlink || e.dir || e.broken {
		t.Errorf("aktuell.txt = %+v, erwartet Symlink auf Datei", e)
	}
	if e := kinds["kaputt"]; !e.symlink || !e.broken {
		t.Errorf("kaputt = %+v, erwartet defekten Symlink", e)
	}
	if e := kinds["projekte"]; e.symlink || !e.dir {
		t.Errorf("projekte = %+v, erwartet Verzeichnis", e)
	}
	if e := kinds[".."]; !e.parent || !e.dir {
		t.Errorf(".. = %+v, erwartet Elterneintrag", e)
	}
}

// TestReadPanelDirError liefert den Fehler unverändert weiter.
func TestReadPanelDirError(t *testing.T) {
	_, err := readPanelDir(newFakeFS(), "/home/kurt/geheim")
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("Fehler = %v, erwartet ErrPermission", err)
	}
}

// TestFileManagerI18nLists - Menü, Spaltenköpfe und F-Tasten haben in jeder
// Sprache die richtige Anzahl Teile (mit "|" getrennt).
func TestFileManagerI18nLists(t *testing.T) {
	// Menüleiste seit Build 73 ohne "Befehl": Links, Datei, Optionen, Rechts
	parts := map[textKey]int{txtFMMenuBar: 4, txtFMFKeys: 10, txtFMColumns: 3}
	for _, opt := range AvailableLanguages {
		for key, want := range parts {
			text := extraText(opt.Code, key)
			got := strings.Split(text, "|")
			if len(got) != want {
				t.Errorf("%s %s: %d Teile, erwartet %d (%q)", opt.Code, key, len(got), want, text)
			}
			for _, part := range got {
				if strings.TrimSpace(part) == "" {
					t.Errorf("%s %s: leerer Teil in %q", opt.Code, key, text)
				}
			}
		}
	}
}

// TestFMEntriesSortStable prüft die Sortierfunktion direkt.
func TestFMEntriesSortStable(t *testing.T) {
	entries := []fmEntry{
		{name: "b"}, {name: "A", dir: true}, {name: "..", dir: true, parent: true},
		{name: "a"}, {name: "c", dir: true, symlink: true},
	}
	sortFMEntries(entries)
	var got []string
	for _, e := range entries {
		got = append(got, e.name)
	}
	if want := []string{"..", "A", "c", "a", "b"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Sortierung = %v, erwartet %v", got, want)
	}
	if !sort.SliceIsSorted(got[1:3], func(i, j int) bool { return strings.ToLower(got[1+i]) < strings.ToLower(got[1+j]) }) {
		t.Error("Verzeichnisse nicht alphabetisch")
	}
}

// TestFileManagerConnectErrorsSanitized - Fehlertexte von SSH/SFTP können
// Text vom Server enthalten (z. B. aus dem Handshake) und dürfen keine
// Steuerzeichen ins Terminal bringen (Security-Review Build 53).
func TestFileManagerConnectErrorsSanitized(t *testing.T) {
	evil := "server says \x1b]0;pwned\x07\x1b[2J"
	cases := map[string]error{
		"netz":    &networkError{err: errors.New("i/o timeout " + evil)},
		"sftp":    &fmSFTPError{err: errors.New(evil)},
		"hostkey": &HostKeyChangedError{Hostname: "host\x1b[2J"},
	}
	for name, connErr := range cases {
		m := newListTestModel(t, "Server", "Zweit")
		rec := newConnectRecorder()
		rec.results["id-0"] = connErr
		m.fmConnect = rec.connect
		m = openPick(t, m)
		m = pickSide(t, m, "Server")
		m = pickSide(t, m, "Zweit")
		if m.state != ViewList {
			t.Fatalf("%s: state = %v, erwartet ViewList", name, m.state)
		}
		if strings.ContainsAny(m.errorMsg, "\x1b\x07") {
			t.Errorf("%s: Fehlermeldung enthält Steuerzeichen: %q", name, m.errorMsg)
		}
		if strings.ContainsAny(stripANSI(renderListText(m)), "\x07") || strings.Contains(renderListText(m), "\x1b]0;") {
			t.Errorf("%s: Steuersequenz erreicht die Ausgabe", name)
		}
	}
}

// TestConnectErrorSanitized - dasselbe für den normalen Verbindungsaufbau.
func TestConnectErrorSanitized(t *testing.T) {
	m := newListTestModel(t, "Server")
	updated, _ := m.Update(sshErrorMsg{id: "id-0", err: errors.New("banner \x1b[2J\x1b]0;x\x07")})
	m = updated.(AppModel)
	if strings.ContainsAny(m.errorMsg, "\x1b\x07") {
		t.Errorf("Fehlermeldung enthält Steuerzeichen: %q", m.errorMsg)
	}
}

// --- Markieren (Build 56) ---

// markedNames liefert die markierten Einträge eines Panels.
func markedNames(p filePanel) []string {
	var names []string
	for _, e := range p.entries {
		if e.marked {
			names = append(names, e.name)
		}
	}
	return names
}

// TestFileManagerInsertTogglesAndMovesDown - Einfg markiert den Eintrag
// unter dem Cursor und springt eine Zeile weiter; erneut = entmarkieren.
func TestFileManagerInsertTogglesAndMovesDown(t *testing.T) {
	for _, key := range []string{"insert", "nul"} {
		m, _ := openFileManagerFor(t)
		m = cursorTo(t, m, "geheim")
		start := m.fm.panels[0].cursor
		m = pressRun(t, m, key)
		p := m.fm.panels[0]
		if got := strings.Join(markedNames(p), ","); got != "geheim" {
			t.Errorf("%s: markiert = %q, erwartet geheim", key, got)
		}
		if p.cursor != start+1 {
			t.Errorf("%s: Cursor = %d, erwartet %d", key, p.cursor, start+1)
		}
		m = cursorTo(t, m, "geheim")
		m = pressRun(t, m, key)
		if got := markedNames(m.fm.panels[0]); len(got) != 0 {
			t.Errorf("%s: nach zweitem Druck noch markiert: %v", key, got)
		}
	}
}

// TestFileManagerInsertSkipsParentEntry - ".." ist nicht markierbar, der
// Cursor springt trotzdem weiter.
func TestFileManagerInsertSkipsParentEntry(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = cursorTo(t, m, "..")
	m = pressRun(t, m, "insert")
	if got := markedNames(m.fm.panels[0]); len(got) != 0 {
		t.Errorf("markiert = %v, \"..\" darf nicht markierbar sein", got)
	}
	if m.fm.panels[0].cursor != 1 {
		t.Errorf("Cursor = %d, erwartet 1", m.fm.panels[0].cursor)
	}
}

// TestFileManagerInsertAtLastEntry - am Ende markieren, Cursor bleibt.
func TestFileManagerInsertAtLastEntry(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = pressRun(t, m, "end")
	last := m.fm.panels[0].cursor
	m = pressRun(t, m, "insert")
	p := m.fm.panels[0]
	if p.cursor != last || !p.entries[last].marked {
		t.Errorf("Cursor %d (erwartet %d), markiert %v", p.cursor, last, p.entries[last].marked)
	}
}

// TestFileManagerStarInvertsAll - * kehrt die Markierung aller Einträge
// (Dateien und Verzeichnisse, ohne "..") um; zweimal * = wie vorher.
func TestFileManagerStarInvertsAll(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = cursorTo(t, m, "b.txt")
	m = pressRun(t, m, "insert")
	cursor := m.fm.panels[0].cursor
	m = pressRun(t, m, "*")
	got := strings.Join(markedNames(m.fm.panels[0]), ",")
	want := "geheim,logs,projekte,aktuell.txt,kaputt,Notes.txt"
	if got != want {
		t.Errorf("nach *: markiert = %s, erwartet %s", got, want)
	}
	if m.fm.panels[0].cursor != cursor {
		t.Error("* darf den Cursor nicht bewegen")
	}
	m = pressRun(t, m, "*")
	if got := strings.Join(markedNames(m.fm.panels[0]), ","); got != "b.txt" {
		t.Errorf("nach zweitem *: markiert = %s, erwartet b.txt", got)
	}
	// Die andere Seite bleibt unberührt
	if got := markedNames(m.fm.panels[1]); len(got) != 0 {
		t.Errorf("rechte Seite markiert: %v", got)
	}
}

// TestFileManagerMarksOnlyActivePanelAndSurviveTab - Markierungen gelten
// pro Fenster und bleiben beim Seitenwechsel erhalten.
func TestFileManagerMarksOnlyActivePanelAndSurviveTab(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = pressRun(t, m, "*")
	m = pressRun(t, m, "tab")
	m = cursorTo(t, m, "b.txt")
	m = pressRun(t, m, "insert")
	m = pressRun(t, m, "tab")
	if n := len(markedNames(m.fm.panels[0])); n != 7 {
		t.Errorf("links markiert = %d, erwartet 7", n)
	}
	if got := strings.Join(markedNames(m.fm.panels[1]), ","); got != "b.txt" {
		t.Errorf("rechts markiert = %s, erwartet b.txt", got)
	}
}

// TestFileManagerMarksClearedOnDirChange - Verzeichniswechsel verwirft
// die Markierungen (wie im mc); misslingt der Wechsel, bleiben sie.
func TestFileManagerMarksClearedOnDirChange(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = pressRun(t, m, "*")
	m = cursorTo(t, m, "geheim") // kein Zugriff
	m = pressRun(t, m, "enter")
	if n := len(markedNames(m.fm.panels[0])); n != 7 {
		t.Errorf("nach misslungenem Wechsel markiert = %d, erwartet 7", n)
	}
	m = cursorTo(t, m, "projekte")
	m = pressRun(t, m, "enter")
	m = cursorTo(t, m, "..")
	m = pressRun(t, m, "enter")
	if got := markedNames(m.fm.panels[0]); len(got) != 0 {
		t.Errorf("nach Verzeichniswechsel noch markiert: %v", got)
	}
}

// TestFileManagerMarkedInfoLine - die Infozeile zeigt Anzahl und Größe der
// markierten Einträge (Größe nur aus Dateien).
func TestFileManagerMarkedInfoLine(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = cursorTo(t, m, "b.txt") // 10 Bytes
	m = pressRun(t, m, "insert")
	m = cursorTo(t, m, "Notes.txt") // 2048 Bytes
	m = pressRun(t, m, "insert")
	m = cursorTo(t, m, "projekte") // Verzeichnis: zählt mit, Größe nicht
	m = pressRun(t, m, "insert")
	want := fmt.Sprintf(m.tr(txtFMMarkedInfo), 3, "2058 B")
	text := stripANSI(m.View())
	if !strings.Contains(text, want) {
		t.Errorf("Infozeile ohne %q:\n%s", want, text)
	}
	assertScreenSize(t, m)
	// Meldungen haben Vorrang, danach wieder die Markierungsinfo
	m = cursorTo(t, m, "logs")
	m = pressRun(t, m, "enter")
	if !strings.Contains(stripANSI(m.View()), m.fm.message[:20]) {
		t.Error("Meldung wird von der Markierungsinfo verdeckt")
	}
}

// TestFileManagerMarkedEntriesHighlighted - markierte Einträge erscheinen in
// der Markierungsfarbe, auch in der Cursorzeile und im inaktiven Fenster.
func TestFileManagerMarkedEntriesHighlighted(t *testing.T) {
	withColorProfile(t)
	m, _ := openFileManagerFor(t)
	m = cursorTo(t, m, "b.txt")
	m = pressRun(t, m, "insert")
	m = cursorTo(t, m, "b.txt") // Cursor auf markierter Zeile
	// Erwartete Escape-Folge eines Stils = alles vor dem Text
	prefix := func(style lipgloss.Style) string {
		out := style.Render("X")
		return out[:strings.Index(out, "X")]
	}
	for theme := range fmThemes {
		m.fm.theme = theme
		st := fmThemeStyles(theme)
		for _, active := range []int{0, 1} {
			m.fm.active = active
			want := prefix(st.mark)
			if active == 0 {
				want = prefix(st.cursorMark) // Cursor steht auf b.txt
			}
			left := leftHalf(selectedLine(t, m.View(), " b.txt"), m.termWidth/2)
			if !strings.Contains(left, want+" b.txt") {
				t.Errorf("Modus %s, aktiv %d: markierte Zeile ohne Markierungsstil %q: %q", fmThemes[theme].name, active, want, left)
			}
			plain := leftHalf(selectedLine(t, m.View(), " Notes.txt"), m.termWidth/2)
			if strings.Contains(plain, prefix(st.mark)) {
				t.Errorf("Modus %s: nicht markierte Zeile im Markierungsstil", fmThemes[theme].name)
			}
		}
	}
}

// leftHalf liefert die ersten width sichtbaren Zellen einer gerenderten
// Zeile samt Escape-Sequenzen (linkes Fenster).
func leftHalf(line string, width int) string {
	visible := 0
	for i := 0; i < len(line); i++ {
		if line[i] == 0x1b {
			for i < len(line) && line[i] != 'm' {
				i++
			}
			continue
		}
		if line[i]&0xc0 != 0x80 {
			visible++
			if visible > width {
				return line[:i]
			}
		}
	}
	return line
}

// TestFileManagerMarkKeysOnEmptyPanel - Markiertasten auf leerem Fenster
// (unlesbares Startverzeichnis im Wurzelverzeichnis) stürzen nicht ab.
func TestFileManagerMarkKeysOnEmptyPanel(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m.fm.panels[0].entries = nil
	m.fm.panels[0].cursor = 0
	m = pressRun(t, m, "insert")
	m = pressRun(t, m, "*")
	assertScreenSize(t, m)
}

// TestFileManagerMultiNulDoesNotMark - nur genau eine NUL-Rune gilt als
// Einfg (Windows); mehrere NUL-Runen markieren nicht.
func TestFileManagerMultiNulDoesNotMark(t *testing.T) {
	m, _ := openFileManagerFor(t)
	m = cursorTo(t, m, "b.txt")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0, 0}})
	m = updated.(AppModel)
	if got := markedNames(m.fm.panels[0]); len(got) != 0 {
		t.Errorf("mehrere NUL-Runen markieren: %v", got)
	}
}

// TestFormatSizeWithUnit - Größe mit Einheit für die Infozeile.
func TestFormatSizeWithUnit(t *testing.T) {
	cases := map[int64]string{
		0:             "0 B",
		12:            "12 B",
		9999999:       "9999999 B",
		10000000:      "9765 KB",
		1048576 * 500: "500 MB",
		1 << 40:       "1024 GB",
	}
	for in, want := range cases {
		if got := formatSizeWithUnit(in); got != want {
			t.Errorf("formatSizeWithUnit(%d) = %q, erwartet %q", in, got, want)
		}
	}
}
