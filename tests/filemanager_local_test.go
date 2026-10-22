// Paket main - Tests für "Lokaler PC" im Datei-Manager (Build 70)
//
// Prüft das lokale Dateisystem (localFS) und seine Einbindung:
//   - Pfadumrechnung Fensterpfad <-> Windows-Pfad (/C:/x <-> C:\x,
//     Laufwerksliste unter "/") als reine Funktionen
//   - Windows-taugliche Namen (keine Gerätenamen, kein ":" usw.)
//   - Dateioperationen auf einem echten temporären Verzeichnis
//   - "Lokaler PC" in der Verbindungsauswahl, Kopieren/Verschieben/Löschen
//     zwischen PC und Server, Laufwerksliste schützt vor F5-F8
//
// @author Kurt Ingwer
// @date   2026-10-22 06:40
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLocalNativePathWindows - Fensterpfad -> Windows-Pfad.
func TestLocalNativePathWindows(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"/", "", true},
		{"/C:", `C:\`, true},
		{"/c:", `C:\`, true},
		{"/C:/Users/x", `C:\Users\x`, true},
		{"/D:/a/../b", `D:\b`, true},
		{"/C:/..", "", true},
		{"/foo", "", false},
		{"/CC:", "", false},
		{"/1:", "", false},
		{"relativ", "", false},
	}
	for _, c := range cases {
		got, err := localNativePath(c.in, true)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("localNativePath(%q) = %q, %v; erwartet %q, ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
}

// TestLocalNativePathUnix - unter Linux/macOS sind Fensterpfade schon echte Pfade.
func TestLocalNativePathUnix(t *testing.T) {
	for in, want := range map[string]string{"/": "/", "/home/a": "/home/a", "/home/a/../b": "/home/b"} {
		if got, err := localNativePath(in, false); err != nil || got != want {
			t.Errorf("localNativePath(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := localNativePath("relativ", false); err == nil {
		t.Error("relativer Pfad akzeptiert")
	}
}

// TestLocalPanelPath - echter Pfad -> Fensterpfad.
func TestLocalPanelPath(t *testing.T) {
	win := map[string]string{
		`C:\`:              "/C:",
		`C:`:               "/C:",
		`c:\Users\x`:       "/C:/Users/x",
		`D:\a\b\`:          "/D:/a/b",
		`\\server\share\x`: "/",
		`relativ`:          "/",
	}
	for in, want := range win {
		if got := localPanelPath(in, true); got != want {
			t.Errorf("Windows localPanelPath(%q) = %q, erwartet %q", in, got, want)
		}
	}
	if got := localPanelPath("/home/a/", false); got != "/home/a" {
		t.Errorf("Unix localPanelPath = %q", got)
	}
}

// TestValidWindowsName - Namen, die unter Windows nicht oder anders
// angelegt würden, werden abgelehnt.
func TestValidWindowsName(t *testing.T) {
	for _, bad := range []string{"CON", "con.txt", "Nul.tar.gz", "COM1", "lpt9.x", "aux", "PRN.", "com¹",
		"a:b", "datei.txt:strom", "x.", "x ", "a?b", "a<b", "a>b", "a|b", "a*b", "a\"b", "a\x01b", ""} {
		if validWindowsName(bad) {
			t.Errorf("%q als gültig erkannt", bad)
		}
	}
	for _, good := range []string{"console", "com10", "normal.txt", "ä ö", ".hidden", "CONx.txt", "a..b"} {
		if !validWindowsName(good) {
			t.Errorf("%q abgelehnt", good)
		}
	}
}

// newLocalTestFS liefert ein localFS mit Home in einem Temp-Verzeichnis.
func newLocalTestFS(t *testing.T) (*localFS, string) {
	t.Helper()
	dir := t.TempDir()
	// Echter Pfad ohne Symlinks (macOS: /var -> /private/var)
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &localFS{home: filepath.ToSlash(real)}, real
}

// TestLocalFSOperations - Grundoperationen auf einem echten Verzeichnis.
func TestLocalFSOperations(t *testing.T) {
	fsys, dir := newLocalTestFS(t)
	home, _ := fsys.Getwd()
	if home != filepath.ToSlash(dir) {
		t.Fatalf("Getwd = %q", home)
	}
	if err := fsys.Mkdir(home + "/sub"); err != nil {
		t.Fatal(err)
	}
	w, err := fsys.CreateExclusive(home + "/sub/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(w, "hallo")
	w.Close()
	if _, err := fsys.CreateExclusive(home + "/sub/a.txt"); err == nil {
		t.Error("CreateExclusive auf vorhandene Datei erlaubt")
	}
	// Neue Datei ist nur für den Besitzer lesbar
	if info, _ := os.Stat(filepath.Join(dir, "sub", "a.txt")); info.Mode().Perm()&0o077 != 0 {
		t.Errorf("Rechte %v, erwartet ohne Gruppe/Andere", info.Mode().Perm())
	}
	w, _ = fsys.CreateExclusive(home + "/sub/b.tmp")
	io.WriteString(w, "neu")
	w.Close()
	if err := fsys.RenameOverwrite(home+"/sub/b.tmp", home+"/sub/a.txt"); err != nil {
		t.Fatal(err)
	}
	r, _ := fsys.OpenRead(home + "/sub/a.txt")
	data, _ := io.ReadAll(r)
	r.Close()
	if string(data) != "neu" {
		t.Errorf("Inhalt nach RenameOverwrite = %q", data)
	}
	when := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := fsys.Chtimes(home+"/sub/a.txt", when, when); err != nil {
		t.Fatal(err)
	}
	if err := fsys.Chmod(home+"/sub/a.txt", 0o640); err != nil {
		t.Fatal(err)
	}
	if info, _ := fsys.Lstat(home + "/sub/a.txt"); !info.ModTime().Equal(when) || info.Mode().Perm() != 0o640 {
		t.Errorf("Zeit/Rechte = %v / %v", info.ModTime(), info.Mode().Perm())
	}
	// Nicht leeres Verzeichnis lässt sich nicht löschen
	if err := fsys.Remove(home + "/sub"); err == nil {
		t.Error("nicht leeres Verzeichnis gelöscht")
	}
	// Symlink: ReadDir und Lstat folgen ihm nicht, RealPath löst ihn auf
	if err := fsys.Symlink("sub", home+"/link"); err != nil {
		t.Fatal(err)
	}
	infos, err := fsys.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	foundLink := false
	for _, info := range infos {
		if info.Name() == "link" {
			foundLink = info.Mode()&os.ModeSymlink != 0
		}
	}
	if !foundLink {
		t.Error("Symlink in ReadDir nicht als Symlink gemeldet")
	}
	if target, _ := fsys.ReadLink(home + "/link"); target != "sub" {
		t.Errorf("ReadLink = %q", target)
	}
	if real, _ := fsys.RealPath(home + "/link"); real != home+"/sub" {
		t.Errorf("RealPath = %q", real)
	}
	if err := fsys.Remove(home + "/link"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "a.txt")); err != nil {
		t.Error("Löschen des Symlinks hat das Ziel gelöscht")
	}
	if fsys.ValidName("CON") != true {
		t.Error("unter Linux ist CON ein gültiger Name")
	}
}

// TestLocalFSWindowsDriveRoot - "/" ist unter Windows die Laufwerksliste:
// ein Verzeichnis, aber nicht beschreibbar.
func TestLocalFSWindowsDriveRoot(t *testing.T) {
	fsys := &localFS{home: "/", windows: true}
	info, err := fsys.Lstat("/")
	if err != nil || !info.IsDir() {
		t.Fatalf("Lstat(/) = %v, %v", info, err)
	}
	if err := fsys.Mkdir("/neu"); err == nil {
		t.Error("Mkdir in der Laufwerksliste erlaubt")
	}
	if _, err := fsys.CreateExclusive("/x.txt"); err == nil {
		t.Error("Datei in der Laufwerksliste angelegt")
	}
	if fsys.ValidName("CON") || fsys.ValidName("a:b") {
		t.Error("Windows-Namensprüfung fehlt")
	}
}

// --- Kopieren: Namensprüfung und Herkunftskennzeichnung ---

// strictFS ist ein Ziel mit Windows-Namensregeln (wie lokales Windows).
type strictFS struct{ *memFS }

func (s *strictFS) ValidName(name string) bool { return validWindowsName(name) }

// TestCopyRefusesNamesInvalidOnTarget - Namen, die am Ziel (Windows)
// nicht gültig sind, werden nicht kopiert; der Rest schon.
func TestCopyRefusesNamesInvalidOnTarget(t *testing.T) {
	src, dst := newMemFS("/home/a"), newMemFS("/srv/b")
	src.addFile("/home/a/ok/CON", "x", 0o644, copyTestTime)
	src.addFile("/home/a/ok/a:b", "x", 0o644, copyTestTime)
	src.addFile("/home/a/ok/gut.txt", "g", 0o644, copyTestTime)
	src.addFile("/home/a/nul.txt", "x", 0o644, copyTestTime)
	req := copyRequest{src: src, dst: &strictFS{dst}, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"ok", "nul.txt"}}
	result, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	unsafe := 0
	for _, f := range result.failures {
		if errors.Is(f.err, errUnsafeName) {
			unsafe++
		}
	}
	if unsafe != 3 {
		t.Errorf("errUnsafeName = %d, erwartet 3: %s", unsafe, failureErrors(result.failures))
	}
	if got, _ := dst.content("/srv/b/ok/gut.txt"); got != "g" {
		t.Error("gültige Datei nicht kopiert")
	}
	for _, p := range []string{"/srv/b/ok/CON", "/srv/b/ok/a:b", "/srv/b/nul.txt"} {
		if dst.node(p) != nil {
			t.Errorf("%s trotz ungültigem Namen angelegt", p)
		}
	}
}

// markerFS merkt sich, welche Dateien als heruntergeladen markiert wurden
// (als Knoten - markiert wird die Temp-Datei, die danach umbenannt wird).
type markerFS struct {
	*memFS
	marked map[*memNode]bool
	fail   bool
}

func (m *markerFS) MarkDownloaded(p string) error {
	if m.fail {
		return errors.New("mark failed")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.marked == nil {
		m.marked = map[*memNode]bool{}
	}
	// Markieren muss vor dem Setzen der Originalrechte passieren (sonst
	// scheitert es unter Windows an schreibgeschützten Dateien)
	if n := m.nodes[p]; n != nil {
		if n.mode.Perm() != 0o600 {
			return errors.New("marked after chmod")
		}
		m.marked[n] = true
	}
	return nil
}

// TestCopyMarksDownloadedFiles - mit markDownloads werden kopierte Dateien
// (nicht Verzeichnisse/Symlinks) gekennzeichnet, und zwar als Temp-Datei
// vor dem Setzen der Rechte; ohne markDownloads gar nicht.
func TestCopyMarksDownloadedFiles(t *testing.T) {
	for _, mark := range []bool{true, false} {
		src := newCopySource()
		// Schreibgeschützte Quelle: Markieren nach dem chmod schlüge fehl
		src.nodes["/home/a/projekt/main.go"].mode = 0o444
		dst := &markerFS{memFS: newMemFS("/srv/b")}
		req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt", "notiz.txt"}, markDownloads: mark}
		result, _ := runCopySync(t, context.Background(), req, noConflicts(t))
		if len(result.failures) != 0 {
			t.Fatalf("mark=%v: Fehler %s", mark, failureErrors(result.failures))
		}
		for _, p := range []string{"/srv/b/projekt/main.go", "/srv/b/projekt/run.sh", "/srv/b/projekt/lib/x.go", "/srv/b/notiz.txt"} {
			if got := dst.marked[dst.node(p)]; got != mark {
				t.Errorf("mark=%v: %s markiert = %v", mark, p, got)
			}
		}
		if len(dst.marked) != map[bool]int{true: 4, false: 0}[mark] {
			t.Errorf("mark=%v: %d markiert", mark, len(dst.marked))
		}
	}
}

// TestCopyMarkFailureIsAnError - lässt sich eine Datei nicht markieren,
// wird sie nicht abgelegt (sonst läge eine unmarkierte .exe vom Server da).
func TestCopyMarkFailureIsAnError(t *testing.T) {
	src := newCopySource()
	dst := &markerFS{memFS: newMemFS("/srv/b"), fail: true}
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"notiz.txt"}, markDownloads: true}
	result, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(result.failures) != 1 {
		t.Fatalf("Fehler = %s, erwartet einen", failureErrors(result.failures))
	}
	if dst.node("/srv/b/notiz.txt") != nil {
		t.Error("unmarkierte Datei abgelegt")
	}
	assertNoTempFiles(t, dst.memFS)
}

// TestCopyRefusesLinksToLocal - mit safeLinksOnly (Server -> PC) wird kein
// Symlink angelegt, auch kein harmlos aussehender: Ein Ziel wie ".ssh"
// zeigt im Zielordner ~ auf das vorhandene ~/.ssh, und über einen dort
// schon liegenden Symlink führte auch ein Ziel ohne ".." hinaus. Die
// Links werden gemeldet, Dateien normal kopiert.
func TestCopyRefusesLinksToLocal(t *testing.T) {
	src := newCopySource()
	src.addLink("/home/a/projekt/ssh", ".ssh")
	src.addLink("/home/a/projekt/raus", "../../etc")
	dst := newMemFS("/srv/b")
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt"}, safeLinksOnly: true}
	result, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	unsafe := 0
	for _, f := range result.failures {
		if errors.Is(f.err, errUnsafeSymlink) {
			unsafe++
		}
	}
	if unsafe != 3 {
		t.Errorf("errUnsafeSymlink = %d, erwartet 3 (aktuell, ssh, raus): %s", unsafe, failureErrors(result.failures))
	}
	for _, p := range []string{"/srv/b/projekt/ssh", "/srv/b/projekt/raus", "/srv/b/projekt/aktuell"} {
		if dst.node(p) != nil {
			t.Errorf("%s als Symlink auf dem PC angelegt", p)
		}
	}
	if got, _ := dst.content("/srv/b/projekt/main.go"); got != "package main" {
		t.Error("Dateien nicht kopiert")
	}
	// Ohne safeLinksOnly (Server -> Server) bleiben Links unverändert
	dst2 := newMemFS("/srv/b")
	req.dst, req.safeLinksOnly = dst2, false
	runCopySync(t, context.Background(), req, noConflicts(t))
	if n := dst2.node("/srv/b/projekt/ssh"); n == nil || n.target != ".ssh" {
		t.Error("Server -> Server: Link verändert")
	}
}

// TestMoveKeepsLinksOnServerWhenRefused - beim Verschieben auf den PC
// bleibt ein nicht übernommener Symlink in der Quelle.
func TestMoveKeepsLinksOnServerWhenRefused(t *testing.T) {
	src := newCopySource()
	dst := newMemFS("/srv/b")
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt"}, safeLinksOnly: true, move: true}
	runCopySync(t, context.Background(), req, noConflicts(t))
	if n := src.node("/home/a/projekt/aktuell"); n == nil || n.target != "main.go" {
		t.Error("nicht übernommener Symlink aus der Quelle gelöscht")
	}
	if src.node("/home/a/projekt/main.go") != nil {
		t.Error("Datei nicht verschoben")
	}
}

// TestUntrustedToLocal - Schutz greift nur vom Server auf den PC.
func TestUntrustedToLocal(t *testing.T) {
	local, remote := &localFS{home: "/"}, newMemFS("/srv")
	if !fmUntrustedToLocal(remote, local) || fmUntrustedToLocal(local, remote) || fmUntrustedToLocal(remote, newMemFS("/x")) {
		t.Error("falsche Richtung")
	}
}

// TestErrUnsafeSymlinkMessage - übersetzte Meldung.
func TestErrUnsafeSymlinkMessage(t *testing.T) {
	m := openCopyManager(t, newCopySource(), newMemFS("/srv/b"))
	got := m.fmDescribeFailure(copyFailure{path: "/home/a/x", err: errUnsafeSymlink})
	if want := fmt.Sprintf(m.tr(txtFMErrUnsafeLink), "/home/a/x"); got != want {
		t.Errorf("Meldung = %q, erwartet %q", got, want)
	}
}

// TestShouldMarkDownloads - markiert wird nur beim Kopieren vom Server auf
// den lokalen PC.
func TestShouldMarkDownloads(t *testing.T) {
	local, remote := &localFS{home: "/"}, newMemFS("/srv")
	if !fmShouldMarkDownloads(remote, local) {
		t.Error("Server -> PC nicht markiert")
	}
	if fmShouldMarkDownloads(local, remote) || fmShouldMarkDownloads(remote, newMemFS("/x")) {
		t.Error("falsche Richtung markiert")
	}
}

// --- Oberfläche ---

// openLocalManager öffnet den Datei-Manager mit "Lokaler PC" links (Temp-
// Verzeichnis) und einem memFS-Server rechts.
func openLocalManager(t *testing.T, remote *memFS) (AppModel, *localFS, string) {
	t.Helper()
	local, dir := newLocalTestFS(t)
	m := newListTestModel(t, "Rechts")
	m.fmConnect = func(conn Connection, password string) (remoteFS, error) { return remote, nil }
	m.fmLocalConnect = func() (remoteFS, error) { return local, nil }
	m.termWidth, m.termHeight = 100, 30
	m = openPick(t, m)
	m = pickSide(t, m, m.tr(txtFMLocalPC))
	m = pickSide(t, m, "Rechts")
	if m.state != ViewFileManager {
		t.Fatalf("state = %v (%q / %q)", m.state, m.fm.message, m.errorMsg)
	}
	return m, local, dir
}

// TestFileManagerEnabledWithOneConnection - mit "Lokaler PC" genügt eine
// Verbindung; ohne Verbindung ausgegraut mit Hinweis.
func TestFileManagerEnabledWithOneConnection(t *testing.T) {
	m := newListTestModel(t, "Server")
	text := stripANSI(renderListText(m))
	if strings.Contains(text, m.tr(txtFMNeedConnection)) {
		t.Errorf("Hinweis trotz Verbindung:\n%s", text)
	}
	m = press(t, m, "end", "enter")
	if m.state != ViewFMPick {
		t.Errorf("state %v, erwartet ViewFMPick", m.state)
	}
	m = newListTestModel(t)
	if text := stripANSI(renderListText(m)); !strings.Contains(text, m.tr(txtFMNeedConnection)) {
		t.Errorf("Hinweis fehlt ohne Verbindung:\n%s", text)
	}
	if m = press(t, m, "end", "enter"); m.state != ViewList {
		t.Errorf("ohne Verbindung geöffnet (state %v)", m.state)
	}
}

// TestFileManagerPickOffersLocalPC - "Lokaler PC" steht direkt nach dem
// Platzhalter; links gewählt, fehlt er rechts.
func TestFileManagerPickOffersLocalPC(t *testing.T) {
	m := newListTestModel(t, "A", "B")
	m = openPick(t, m)
	conns := m.fmPickConnections()
	if len(conns) != 3 || conns[0].ID != fmLocalConnID || conns[0].Name != m.tr(txtFMLocalPC) {
		t.Fatalf("Auswahl = %v", conns)
	}
	if m.fm.pickCursor != 0 {
		t.Error("Cursor startet nicht auf dem Platzhalter")
	}
	if text := stripANSI(m.View()); !strings.Contains(text, m.tr(txtFMLocalPC)) {
		t.Errorf("Lokaler PC fehlt in der Anzeige:\n%s", text)
	}
	m = press(t, m, "down", "enter")
	for _, conn := range m.fmPickConnections() {
		if conn.ID == fmLocalConnID {
			t.Error("Lokaler PC rechts noch einmal wählbar")
		}
	}
}

// TestFileManagerLocalTitle - Fenstertitel nennt "Lokaler PC".
func TestFileManagerLocalTitle(t *testing.T) {
	m, _, dir := openLocalManager(t, newMemFS("/srv/b"))
	want := m.tr(txtFMLocalPC) + ":" + filepath.ToSlash(dir)
	if got := m.fm.panels[0].title(); got != want {
		t.Errorf("Titel = %q, erwartet %q", got, want)
	}
}

// TestFileManagerCopyLocalToServerAndBack - F5 vom PC zum Server, F6 vom
// Server auf den PC, F7 und F8 lokal.
func TestFileManagerCopyLocalToServerAndBack(t *testing.T) {
	remote := newMemFS("/srv/b")
	remote.addFile("/srv/b/vom-server.txt", "server", 0o644, copyTestTime)
	m, _, dir := openLocalManager(t, remote)
	if err := os.WriteFile(filepath.Join(dir, "lokal.txt"), []byte("pc"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Fenster neu einlesen: Tab hin und zurück genügt nicht, also Verzeichnis neu betreten
	m = cursorTo(t, m, "..")
	m = pressRun(t, m, "enter")
	m = cursorTo(t, m, filepath.Base(dir))
	m = pressRun(t, m, "enter")

	// F5: PC -> Server
	m = cursorTo(t, m, "lokal.txt")
	m = pressRun(t, m, "f5")
	m = pressRun(t, m, "enter")
	if got, _ := remote.content("/srv/b/lokal.txt"); got != "pc" {
		t.Fatalf("nicht auf den Server kopiert (%q)", m.fm.message)
	}

	// F6: Server -> PC
	m = pressRun(t, m, "tab")
	m = cursorTo(t, m, "vom-server.txt")
	m = pressRun(t, m, "f6")
	m = pressRun(t, m, "enter")
	if data, err := os.ReadFile(filepath.Join(dir, "vom-server.txt")); err != nil || string(data) != "server" {
		t.Fatalf("nicht auf den PC verschoben: %v (%q)", err, m.fm.message)
	}
	if remote.node("/srv/b/vom-server.txt") != nil {
		t.Error("Quelle auf dem Server nicht gelöscht")
	}
	assertNoLocalTemp(t, dir)

	// F7 + F8 lokal
	m = pressRun(t, m, "tab")
	m = pressRun(t, m, "f7")
	m = typeText(t, m, "neu")
	m = pressRun(t, m, "enter")
	if info, err := os.Stat(filepath.Join(dir, "neu")); err != nil || !info.IsDir() {
		t.Fatalf("Verzeichnis nicht angelegt: %v (%q)", err, m.fm.message)
	}
	os.WriteFile(filepath.Join(dir, "neu", "x"), []byte("x"), 0o644)
	m = cursorTo(t, m, "neu")
	m = pressRun(t, m, "f8")
	m = pressRun(t, m, "j")
	if _, err := os.Stat(filepath.Join(dir, "neu")); err == nil {
		t.Errorf("Verzeichnis nicht gelöscht (%q)", m.fm.message)
	}
}

// assertNoLocalTemp prüft, dass keine Temp-Dateien im Verzeichnis liegen.
func assertNoLocalTemp(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), copyTempPrefix) {
			t.Errorf("Temp-Datei liegen geblieben: %s", e.Name())
		}
	}
}

// TestFileManagerDriveListRefusesFileOps - in der Windows-Laufwerksliste
// sind Kopieren, Verschieben, Anlegen und Löschen gesperrt (sonst würde
// F8 auf "C:" ein ganzes Laufwerk leeren).
func TestFileManagerDriveListRefusesFileOps(t *testing.T) {
	m := newListTestModel(t, "Rechts")
	m.fmConnect = func(conn Connection, password string) (remoteFS, error) { return newCopySource(), nil }
	drives := &driveListFS{localFS: &localFS{home: "/", windows: true}}
	m.fmLocalConnect = func() (remoteFS, error) { return drives, nil }
	m.termWidth, m.termHeight = 100, 30
	m = openPick(t, m)
	m = pickSide(t, m, m.tr(txtFMLocalPC))
	m = pickSide(t, m, "Rechts")
	if m.state != ViewFileManager {
		t.Fatalf("state = %v", m.state)
	}
	if names := strings.Join(entryNames(m.fm.panels[0]), ","); names != "C:,D:" {
		t.Fatalf("Laufwerksliste = %q", names)
	}
	for _, key := range []string{"f5", "f6", "f7", "f8"} {
		m = cursorTo(t, m, "C:")
		m = pressRun(t, m, key)
		if m.fm.dialog != fmDialogNone || m.fm.message != m.tr(txtFMDriveList) {
			t.Errorf("%s: Dialog %v / Meldung %q", key, m.fm.dialog, m.fm.message)
		}
	}
	// Auch als Ziel gesperrt (vom Server in die Laufwerksliste kopieren)
	m = pressRun(t, m, "tab")
	m = cursorTo(t, m, "notiz.txt")
	for _, key := range []string{"f5", "f6"} {
		m = pressRun(t, m, key)
		if m.fm.dialog != fmDialogNone || m.fm.message != m.tr(txtFMDriveList) {
			t.Errorf("Ziel %s: Dialog %v / Meldung %q", key, m.fm.dialog, m.fm.message)
		}
	}
}

// driveListFS liefert unter Linux eine feste Laufwerksliste (C:, D:).
type driveListFS struct{ *localFS }

func (d *driveListFS) ReadDir(p string) ([]os.FileInfo, error) {
	switch path.Clean(p) {
	case "/":
		return []os.FileInfo{localDirInfo{name: "C:"}, localDirInfo{name: "D:"}}, nil
	case "/C:", "/D:":
		// Laufwerkswurzel (unter Linux nicht echt vorhanden): leer
		return nil, nil
	}
	return d.localFS.ReadDir(p)
}

func (d *driveListFS) Lstat(p string) (os.FileInfo, error) {
	switch path.Clean(p) {
	case "/C:", "/D:":
		return localDirInfo{name: path.Base(p)}, nil
	}
	return d.localFS.Lstat(p)
}

func (d *driveListFS) Stat(p string) (os.FileInfo, error) { return d.Lstat(p) }

// TestFileManagerLocalConnectError - Fehler beim lokalen Zugriff führt
// zurück zur Liste (keine Passwortabfrage).
func TestFileManagerLocalConnectError(t *testing.T) {
	m := newListTestModel(t, "Rechts")
	m.fmLocalConnect = func() (remoteFS, error) { return nil, errors.New("kein Home") }
	m = openPick(t, m)
	m = pickSide(t, m, m.tr(txtFMLocalPC))
	m = pickSide(t, m, "Rechts")
	if m.state != ViewList {
		t.Fatalf("state = %v, erwartet Liste", m.state)
	}
	if want := fmt.Sprintf(m.tr(txtFMErrConnect), m.tr(txtFMLocalPC), "kein Home"); m.errorMsg != want {
		t.Errorf("Meldung = %q, erwartet %q", m.errorMsg, want)
	}
}

// TestFileManagerDriveUpFromDriveRoot - von C:\ führt ".." eine Ebene
// höher in die Laufwerksliste; der Cursor steht dort auf C:.
func TestFileManagerDriveUpFromDriveRoot(t *testing.T) {
	m := newListTestModel(t, "Rechts")
	m.fmConnect = func(conn Connection, password string) (remoteFS, error) { return newCopySource(), nil }
	drives := &driveListFS{localFS: &localFS{home: "/C:", windows: true}}
	m.fmLocalConnect = func() (remoteFS, error) { return drives, nil }
	m.termWidth, m.termHeight = 100, 30
	m = openPick(t, m)
	m = pickSide(t, m, m.tr(txtFMLocalPC))
	m = pickSide(t, m, "Rechts")
	if m.state != ViewFileManager || m.fm.panels[0].path != "/C:" {
		t.Fatalf("state %v, Pfad %q", m.state, m.fm.panels[0].path)
	}
	if names := entryNames(m.fm.panels[0]); len(names) == 0 || names[0] != ".." {
		t.Fatalf("C:\\ ohne \"..\": %v", names)
	}
	m = cursorTo(t, m, "..")
	m = pressRun(t, m, "enter")
	panel := m.fm.panels[0]
	if panel.path != "/" || strings.Join(entryNames(panel), ",") != "C:,D:" {
		t.Fatalf("Pfad %q, Einträge %v - erwartet Laufwerksliste", panel.path, entryNames(panel))
	}
	if entry, _ := panel.current(); entry.name != "C:" {
		t.Errorf("Cursor auf %q, erwartet C:", entry.name)
	}
	// Anderes Laufwerk öffnen
	m = cursorTo(t, m, "D:")
	m = pressRun(t, m, "enter")
	if m.fm.panels[0].path != "/D:" {
		t.Errorf("Pfad %q, erwartet /D:", m.fm.panels[0].path)
	}
}
