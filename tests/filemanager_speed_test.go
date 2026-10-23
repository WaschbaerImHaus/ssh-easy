// Paket main - Tests: schnelleres Kopieren vieler kleiner Dateien (Build 78)
//
// Nutzerbericht 2026-10-23: 700 kleine Dateien (18 MB) zu kopieren dauerte
// lange, eine große Datei ging flott. Ursache: pro Datei rund 12
// Server-Rundreisen nacheinander (Lstat beim Erfassen, Lstat am Ziel,
// anlegen, chmod, schreiben, schließen, chmod, Zeit, umbenennen, öffnen,
// lesen, schließen) - die Wartezeit summiert sich. Abhilfe:
//   - mehrere Dateien gleichzeitig übertragen (copyParallel)
//   - beim Erfassen die Angaben aus ReadDir nutzen statt jede Datei per
//     Lstat abzufragen; am Ziel einmal ReadDir pro Verzeichnis statt Lstat
//     pro Datei (neu angelegte Verzeichnisse sind leer)
//   - Lesen und Schreiben einer Datei gleichzeitig (pipeCopy mit
//     begrenztem Lesevorlauf copyBudget)
//
// @author Kurt Ingwer
// @date   2026-10-23 09:20
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingFS zählt Aufrufe und gleichzeitig laufende Dateiübertragungen;
// delay simuliert die Laufzeit einer Server-Rundreise.
type countingFS struct {
	*memFS
	delay    time.Duration
	lstats   atomic.Int64
	readDirs atomic.Int64
	// gerade offene Schreibvorgänge und deren Höchststand
	open    atomic.Int64
	maxOpen atomic.Int64
}

func (c *countingFS) Lstat(p string) (os.FileInfo, error) {
	c.lstats.Add(1)
	time.Sleep(c.delay)
	return c.memFS.Lstat(p)
}

func (c *countingFS) ReadDir(p string) ([]os.FileInfo, error) {
	c.readDirs.Add(1)
	time.Sleep(c.delay)
	return c.memFS.ReadDir(p)
}

func (c *countingFS) CreateExclusive(p string) (io.WriteCloser, error) {
	time.Sleep(c.delay)
	w, err := c.memFS.CreateExclusive(p)
	if err != nil {
		return nil, err
	}
	n := c.open.Add(1)
	for {
		old := c.maxOpen.Load()
		if n <= old || c.maxOpen.CompareAndSwap(old, n) {
			break
		}
	}
	return &countingWriter{WriteCloser: w, fs: c}, nil
}

// countingWriter meldet das Schließen an countingFS zurück.
type countingWriter struct {
	io.WriteCloser
	fs *countingFS
}

func (w *countingWriter) Write(b []byte) (int, error) {
	time.Sleep(w.fs.delay)
	return w.WriteCloser.Write(b)
}

func (w *countingWriter) Close() error {
	w.fs.open.Add(-1)
	return w.WriteCloser.Close()
}

// manyFilesSource liefert eine Quelle mit n kleinen Dateien in /home/a/viele.
func manyFilesSource(n int) *memFS {
	src := newMemFS("/home/a")
	src.addDir("/home/a/viele", 0o755)
	src.nodes["/home/a/viele"].modTime = copyTestTime
	for i := 0; i < n; i++ {
		src.addFile(fmt.Sprintf("/home/a/viele/datei-%03d.txt", i), fmt.Sprintf("Inhalt %d", i), 0o640, copyTestTime)
	}
	return src
}

// TestCopyManyFilesInParallel - viele kleine Dateien werden gleichzeitig
// übertragen; Inhalt, Rechte und Zeit stimmen trotzdem bei jeder Datei,
// und es bleibt keine Temp-Datei liegen.
func TestCopyManyFilesInParallel(t *testing.T) {
	const n = 40
	src := manyFilesSource(n)
	dst := &countingFS{memFS: newMemFS("/srv/b"), delay: 2 * time.Millisecond}
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"viele"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 || res.files != n {
		t.Fatalf("Fehler %v, kopiert %d von %d", res.failures, res.files, n)
	}
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("/srv/b/viele/datei-%03d.txt", i)
		got, ok := dst.content(p)
		if !ok || got != fmt.Sprintf("Inhalt %d", i) {
			t.Errorf("%s = %q (vorhanden %v)", p, got, ok)
			continue
		}
		node := dst.node(p)
		if node.mode.Perm() != 0o640 || !node.modTime.Equal(copyTestTime) {
			t.Errorf("%s: Rechte %v, Zeit %v", p, node.mode.Perm(), node.modTime)
		}
	}
	if got := dst.maxOpen.Load(); got < 2 {
		t.Errorf("höchstens %d Datei(en) gleichzeitig übertragen, erwartet mehrere", got)
	}
	if got := dst.maxOpen.Load(); got > int64(copyParallel) {
		t.Errorf("%d Dateien gleichzeitig, Grenze ist %d", got, copyParallel)
	}
	assertNoTempFiles(t, dst.memFS)
}

// TestCopySavesRoundTrips - beim Erfassen kein Lstat pro Datei (die Angaben
// kommen aus ReadDir), am Ziel kein Lstat pro Datei: das Zielverzeichnis
// wurde neu angelegt und ist damit leer.
func TestCopySavesRoundTrips(t *testing.T) {
	const n = 30
	src := &countingFS{memFS: manyFilesSource(n)}
	dst := &countingFS{memFS: newMemFS("/srv/b")}
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"viele"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 || res.files != n {
		t.Fatalf("Fehler %v, kopiert %d", res.failures, res.files)
	}
	if got := src.lstats.Load(); got > 1 {
		t.Errorf("Quelle: %d Lstat-Aufrufe, erwartet höchstens 1 (nur der markierte Eintrag)", got)
	}
	if got := dst.lstats.Load(); got > 1 {
		t.Errorf("Ziel: %d Lstat-Aufrufe, erwartet höchstens 1", got)
	}
}

// TestCopyIntoExistingDirAsksOnlyForExisting - kopiert man in ein
// vorhandenes Verzeichnis, wird es einmal gelesen statt jede Datei einzeln
// abzufragen; gefragt wird genau bei den Dateien, die es dort schon gibt.
func TestCopyIntoExistingDirAsksOnlyForExisting(t *testing.T) {
	const n = 20
	src := manyFilesSource(n)
	dst := &countingFS{memFS: newMemFS("/srv/b")}
	dst.addDir("/srv/b/viele", 0o755)
	dst.addFile("/srv/b/viele/datei-003.txt", "alt", 0o644, copyTestTime)
	dst.addFile("/srv/b/viele/datei-017.txt", "alt", 0o644, copyTestTime)
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"viele"}}
	res, conflicts := runCopySync(t, context.Background(), req, func(string) copyConflictAnswer { return copyAnswerOverwrite })
	want := "/srv/b/viele/datei-003.txt,/srv/b/viele/datei-017.txt"
	if got := strings.Join(conflicts, ","); got != want {
		t.Errorf("Rückfragen %q, erwartet %q", got, want)
	}
	if len(res.failures) != 0 || res.files != n {
		t.Errorf("Fehler %v, kopiert %d", res.failures, res.files)
	}
	if got, _ := dst.content("/srv/b/viele/datei-017.txt"); got != "Inhalt 17" {
		t.Errorf("datei-017.txt = %q, erwartet überschrieben", got)
	}
	if got := dst.lstats.Load(); got > 1 {
		t.Errorf("Ziel: %d Lstat-Aufrufe, erwartet höchstens 1", got)
	}
}

// TestCopyAbortWhileParallel - Abbruch per Rückfrage, während andere
// Dateien noch übertragen werden: Ergebnis "abgebrochen", keine Temp-Datei
// bleibt liegen, und das vorhandene Ziel bleibt unverändert.
func TestCopyAbortWhileParallel(t *testing.T) {
	src := manyFilesSource(30)
	dst := &countingFS{memFS: newMemFS("/srv/b"), delay: time.Millisecond}
	dst.addDir("/srv/b/viele", 0o755)
	dst.addFile("/srv/b/viele/datei-020.txt", "alt", 0o644, copyTestTime)
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"viele"}}
	res, _ := runCopySync(t, context.Background(), req, func(string) copyConflictAnswer { return copyAnswerAbort })
	if !res.cancelled {
		t.Error("Ergebnis nicht als abgebrochen gemeldet")
	}
	if got, _ := dst.content("/srv/b/viele/datei-020.txt"); got != "alt" {
		t.Errorf("vorhandenes Ziel verändert: %q", got)
	}
	assertNoTempFiles(t, dst.memFS)
}

// TestCopyCancelWhileParallel - Abbruch über den Kontext mitten im
// Übertragen: runCopy endet, meldet "abgebrochen" und räumt Temp-Dateien weg.
func TestCopyCancelWhileParallel(t *testing.T) {
	src := manyFilesSource(60)
	dst := &countingFS{memFS: newMemFS("/srv/b"), delay: 2 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	dst.onWrite = func() { once.Do(func() { go cancel() }) }
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"viele"}}
	res, _ := runCopySync(t, ctx, req, noConflicts(t))
	if !res.cancelled {
		t.Error("Ergebnis nicht als abgebrochen gemeldet")
	}
	if res.files >= 60 {
		t.Error("trotz Abbruch alles kopiert")
	}
	assertNoTempFiles(t, dst.memFS)
}

// TestMoveManyFilesInParallel - Verschieben vieler Dateien: alle liegen am
// Ziel, die Quelle ist danach samt Verzeichnis weg.
func TestMoveManyFilesInParallel(t *testing.T) {
	const n = 25
	src := manyFilesSource(n)
	dst := &countingFS{memFS: newMemFS("/srv/b"), delay: time.Millisecond}
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"viele"}, move: true}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 || res.files != n {
		t.Fatalf("Fehler %v, verschoben %d", res.failures, res.files)
	}
	if src.node("/home/a/viele") != nil {
		t.Error("Quellverzeichnis noch vorhanden")
	}
	for i := 0; i < n; i++ {
		if _, ok := dst.content(fmt.Sprintf("/srv/b/viele/datei-%03d.txt", i)); !ok {
			t.Errorf("datei-%03d.txt fehlt am Ziel", i)
		}
	}
}

// TestCopyManyTopLevelNames - viele markierte Einträge im selben Fenster:
// die Quelle wird einmal gelesen statt jeden Eintrag einzeln abzufragen.
func TestCopyManyTopLevelNames(t *testing.T) {
	const n = 30
	plain := newMemFS("/home/a")
	var names []string
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("f%02d.txt", i)
		plain.addFile("/home/a/"+name, name, 0o644, copyTestTime)
		names = append(names, name)
	}
	src := &countingFS{memFS: plain}
	dst := newMemFS("/srv/b")
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: names}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 || res.files != n || len(res.copiedNames) != n {
		t.Fatalf("Fehler %v, kopiert %d, Namen %d", res.failures, res.files, len(res.copiedNames))
	}
	if got := src.lstats.Load(); got > 1 {
		t.Errorf("Quelle: %d Lstat-Aufrufe für %d markierte Einträge, erwartet höchstens 1", got, n)
	}
}

// --- pipeCopy: gleichzeitiges Lesen und Schreiben mit begrenztem Vorlauf ---

// slowWriter schreibt langsam und zählt, wie weit der Leser höchstens
// vorauslief.
type slowWriter struct {
	buf      bytes.Buffer
	read     *atomic.Int64
	written  int64
	maxAhead int64
	delay    time.Duration
	failAt   int64
}

func (w *slowWriter) Write(b []byte) (int, error) {
	time.Sleep(w.delay)
	if ahead := w.read.Load() - w.written; ahead > w.maxAhead {
		w.maxAhead = ahead
	}
	if w.failAt > 0 && w.written+int64(len(b)) > w.failAt {
		return 0, errors.New("Ziel voll")
	}
	w.written += int64(len(b))
	return w.buf.Write(b)
}

// countingReader zählt gelesene Bytes.
type countingReader struct {
	r    io.Reader
	read *atomic.Int64
	err  error
}

func (c *countingReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.read.Add(int64(n))
	if err == io.EOF && c.err != nil {
		return n, c.err
	}
	return n, err
}

// TestPipeCopyContent - Inhalt kommt vollständig und in Reihenfolge an,
// auch an Blockgrenzen und bei leeren Dateien.
func TestPipeCopyContent(t *testing.T) {
	const chunk = 8
	for _, size := range []int{0, 1, chunk - 1, chunk, chunk + 1, 3*chunk + 5, 100} {
		data := bytes.Repeat([]byte("abcdefghij"), 11)[:size]
		var read atomic.Int64
		w := &slowWriter{read: &read}
		n, err := pipeCopy(context.Background(), w, &countingReader{r: bytes.NewReader(data), read: &read}, newCopyBudget(4*chunk, chunk))
		if err != nil || n != int64(size) || !bytes.Equal(w.buf.Bytes(), data) {
			t.Errorf("Größe %d: n=%d err=%v, Inhalt gleich: %v", size, n, err, bytes.Equal(w.buf.Bytes(), data))
		}
	}
}

// TestPipeCopyBudgetLimitsReadAhead - der Leser läuft dem Schreiber höchstens
// um das Budget voraus (Speicher bleibt begrenzt).
func TestPipeCopyBudgetLimitsReadAhead(t *testing.T) {
	const chunk, budget = 16, 64
	data := bytes.Repeat([]byte("x"), 50*chunk)
	var read atomic.Int64
	w := &slowWriter{read: &read, delay: time.Millisecond}
	if _, err := pipeCopy(context.Background(), w, &countingReader{r: bytes.NewReader(data), read: &read}, newCopyBudget(budget, chunk)); err != nil {
		t.Fatal(err)
	}
	if w.maxAhead > budget+chunk {
		t.Errorf("Leser lief %d Bytes voraus, Budget %d", w.maxAhead, budget)
	}
	if w.maxAhead < chunk {
		t.Errorf("Leser lief nur %d Bytes voraus - kein gleichzeitiges Lesen", w.maxAhead)
	}
}

// TestPipeCopyErrors - Schreib- und Lesefehler kommen beim Aufrufer an, ein
// Abbruch beendet pipeCopy ohne Hänger, und das Budget ist danach wieder
// vollständig frei.
func TestPipeCopyErrors(t *testing.T) {
	const chunk = 8
	b := newCopyBudget(4*chunk, chunk)
	data := bytes.Repeat([]byte("y"), 40*chunk)

	var read atomic.Int64
	w := &slowWriter{read: &read, failAt: 3 * chunk}
	if _, err := pipeCopy(context.Background(), w, &countingReader{r: bytes.NewReader(data), read: &read}, b); err == nil || !strings.Contains(err.Error(), "voll") {
		t.Errorf("Schreibfehler nicht gemeldet: %v", err)
	}

	read.Store(0)
	readErr := errors.New("Verbindung weg")
	if _, err := pipeCopy(context.Background(), &slowWriter{read: &read}, &countingReader{r: bytes.NewReader(data[:5*chunk]), read: &read, err: readErr}, b); !errors.Is(err, readErr) {
		t.Errorf("Lesefehler nicht gemeldet: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	read.Store(0)
	done := make(chan error, 1)
	go func() {
		_, err := pipeCopy(ctx, &slowWriter{read: &read, delay: time.Millisecond}, &countingReader{r: bytes.NewReader(data), read: &read}, b)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Abbruch: Fehler %v, erwartet context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pipeCopy hängt nach Abbruch")
	}
	if got := b.free(); got != 4 {
		t.Errorf("Budget danach %d Blöcke frei, erwartet 4 (Leck)", got)
	}
}

// checkOnRemoveFS prüft bei jedem Löschen in der Quelle, ob die Datei am
// Ziel schon vollständig und unter ihrem endgültigen Namen liegt.
type checkOnRemoveFS struct {
	*memFS
	dst     *countingFS
	srcDir  string
	dstDir  string
	mu      sync.Mutex
	early   []string
	removed int
}

func (c *checkOnRemoveFS) Remove(p string) error {
	if info, err := c.memFS.Lstat(p); err == nil && info.Mode().IsRegular() {
		want, _ := c.memFS.content(p)
		got, ok := c.dst.content(c.dstDir + strings.TrimPrefix(p, c.srcDir))
		c.mu.Lock()
		c.removed++
		if !ok || got != want {
			c.early = append(c.early, p)
		}
		c.mu.Unlock()
	}
	return c.memFS.Remove(p)
}

// failRenameFS lässt das Umbenennen auf einen bestimmten Zielnamen scheitern.
type failRenameFS struct {
	*countingFS
	failFor string
}

func (f *failRenameFS) RenameOverwrite(oldPath, newPath string) error {
	if newPath == f.failFor {
		return errors.New("Umbenennen verweigert")
	}
	return f.countingFS.RenameOverwrite(oldPath, newPath)
}

// TestMoveDeletesSourceOnlyAfterWritten - Nutzerhinweis: beim Verschieben
// erst löschen, wenn alles geschrieben wurde, nicht wenn alles gelesen
// wurde. Jede Quelldatei wird erst gelöscht, wenn sie am Ziel vollständig
// unter ihrem Namen liegt; scheitert das Ablegen (hier: Umbenennen), bleibt
// die Quelle erhalten.
func TestMoveDeletesSourceOnlyAfterWritten(t *testing.T) {
	const n = 20
	dst := &countingFS{memFS: newMemFS("/srv/b"), delay: time.Millisecond}
	src := &checkOnRemoveFS{memFS: manyFilesSource(n), dst: dst, srcDir: "/home/a", dstDir: "/srv/b"}
	failing := &failRenameFS{countingFS: dst, failFor: "/srv/b/viele/datei-007.txt"}
	req := copyRequest{src: src, dst: failing, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"viele"}, move: true}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(src.early) != 0 {
		t.Errorf("Quelle gelöscht, bevor das Ziel vollständig da war: %v", src.early)
	}
	if src.removed != n-1 {
		t.Errorf("%d Quelldateien gelöscht, erwartet %d", src.removed, n-1)
	}
	if got, ok := src.memFS.content("/home/a/viele/datei-007.txt"); !ok || got != "Inhalt 7" {
		t.Errorf("Quelle der gescheiterten Datei verloren: %q (vorhanden %v)", got, ok)
	}
	if len(res.failures) != 1 {
		t.Errorf("Fehler %v, erwartet genau einen (datei-007.txt)", res.failures)
	}
	if src.memFS.node("/home/a/viele") == nil {
		t.Error("Quellverzeichnis gelöscht, obwohl eine Datei darin geblieben ist")
	}
	assertNoTempFiles(t, dst.memFS)
}

// --- Review-Funde Build 78: Groß-/Kleinschreibung, besondere Namen ---

// foldFS verhält sich wie ein Windows-Ziel (Windows-PC, Windows-SFTP-
// Server): Groß-/Kleinschreibung zählt nicht, Punkte und Leerzeichen am
// Ende eines Namens fallen weg. Jeder Pfad wird so abgelegt und gesucht.
type foldFS struct{ *memFS }

func fold(p string) string {
	parts := strings.Split(strings.ToLower(p), "/")
	for i, part := range parts {
		if trimmed := strings.TrimRight(part, ". "); trimmed != "" {
			parts[i] = trimmed
		}
	}
	return strings.Join(parts, "/")
}

func (f *foldFS) Lstat(p string) (os.FileInfo, error)     { return f.memFS.Lstat(fold(p)) }
func (f *foldFS) Stat(p string) (os.FileInfo, error)      { return f.memFS.Stat(fold(p)) }
func (f *foldFS) ReadDir(p string) ([]os.FileInfo, error) { return f.memFS.ReadDir(fold(p)) }
func (f *foldFS) RealPath(p string) (string, error)       { return f.memFS.RealPath(fold(p)) }
func (f *foldFS) ReadLink(p string) (string, error)       { return f.memFS.ReadLink(fold(p)) }
func (f *foldFS) OpenRead(p string) (io.ReadCloser, error) {
	return f.memFS.OpenRead(fold(p))
}
func (f *foldFS) CreateExclusive(p string) (io.WriteCloser, error) {
	return f.memFS.CreateExclusive(fold(p))
}
func (f *foldFS) RenameOverwrite(o, n string) error { return f.memFS.RenameOverwrite(fold(o), fold(n)) }
func (f *foldFS) Mkdir(p string) error              { return f.memFS.Mkdir(fold(p)) }
func (f *foldFS) Remove(p string) error             { return f.memFS.Remove(fold(p)) }
func (f *foldFS) Symlink(t, l string) error         { return f.memFS.Symlink(t, fold(l)) }
func (f *foldFS) Chmod(p string, m os.FileMode) error {
	return f.memFS.Chmod(fold(p), m)
}
func (f *foldFS) Chtimes(p string, a, m time.Time) error { return f.memFS.Chtimes(fold(p), a, m) }

// caseSource liefert eine Linux-Quelle mit README und readme (und je einem
// Verzeichnis Docs/docs) im selben Ordner - wie xt_CONNMARK.h/xt_connmark.h
// im Linux-Kernel.
func caseSource() *memFS {
	src := newMemFS("/home/a")
	src.addDir("/home/a/d", 0o755)
	src.addFile("/home/a/d/README", "GROSS", 0o644, copyTestTime)
	src.addFile("/home/a/d/readme", "klein", 0o644, copyTestTime)
	for i := 0; i < 10; i++ {
		src.addFile(fmt.Sprintf("/home/a/d/z%02d.txt", i), "z", 0o644, copyTestTime)
	}
	return src
}

// TestCopyCaseCollisionAsks - Review-Fund 1: kopiert man README und readme
// auf ein Ziel ohne Groß-/Kleinschreibung, darf die zweite Datei die erste
// nicht still überschreiben: es kommt die normale Rückfrage.
func TestCopyCaseCollisionAsks(t *testing.T) {
	for i := 0; i < 10; i++ { // mehrfach: die Arbeiter laufen nebenläufig
		dst := &foldFS{memFS: newMemFS("/srv/b")}
		req := copyRequest{src: caseSource(), dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"d"}}
		res, conflicts := runCopySync(t, context.Background(), req, func(string) copyConflictAnswer { return copyAnswerSkip })
		if len(conflicts) != 1 || !strings.HasSuffix(strings.ToLower(conflicts[0]), "/d/readme") {
			t.Fatalf("Lauf %d: Rückfragen %v, erwartet genau eine für readme", i, conflicts)
		}
		if got, _ := dst.content("/srv/b/d/readme"); got != "GROSS" {
			t.Fatalf("Lauf %d: Ziel = %q, erwartet GROSS (zweite Datei übersprungen)", i, got)
		}
		if res.files != 11 {
			t.Errorf("Lauf %d: %d Dateien kopiert, erwartet 11", i, res.files)
		}
	}
}

// TestMoveCaseCollisionKeepsSource - Review-Fund 1 beim Verschieben: die
// übersprungene Datei bleibt in der Quelle (vorher gingen beide verloren).
func TestMoveCaseCollisionKeepsSource(t *testing.T) {
	for i := 0; i < 10; i++ {
		src := caseSource()
		dst := &foldFS{memFS: newMemFS("/srv/b")}
		req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"d"}, move: true}
		_, conflicts := runCopySync(t, context.Background(), req, func(string) copyConflictAnswer { return copyAnswerSkip })
		if len(conflicts) != 1 {
			t.Fatalf("Lauf %d: Rückfragen %v, erwartet eine", i, conflicts)
		}
		if got, ok := src.content("/home/a/d/readme"); !ok || got != "klein" {
			t.Fatalf("Lauf %d: übersprungene Quelle verloren (%q, %v)", i, got, ok)
		}
		if _, ok := src.content("/home/a/d/README"); ok {
			t.Errorf("Lauf %d: verschobene Quelle README noch da", i)
		}
	}
}

// TestCopyCaseCollisionDirsMerge - Docs und docs auf einem Ziel ohne
// Groß-/Kleinschreibung werden wie früher zusammengeführt (nicht als Fehler
// übersprungen).
func TestCopyCaseCollisionDirsMerge(t *testing.T) {
	src := newMemFS("/home/a")
	src.addDir("/home/a/Docs", 0o755)
	src.addFile("/home/a/Docs/a.txt", "a", 0o644, copyTestTime)
	src.addDir("/home/a/docs", 0o755)
	src.addFile("/home/a/docs/b.txt", "b", 0o644, copyTestTime)
	dst := &foldFS{memFS: newMemFS("/srv/b")}
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"Docs", "docs"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 {
		t.Errorf("Fehler %v, erwartet Zusammenführen", res.failures)
	}
	for _, p := range []string{"/srv/b/docs/a.txt", "/srv/b/docs/b.txt"} {
		if _, ok := dst.content(p); !ok {
			t.Errorf("%s fehlt", p)
		}
	}
}

// TestCopyFoldedListingHitUsesLstat - Sicherheitsnetz: die Ziel-Liste kennt
// "Readme.md", kopiert wird "README.md". Dann wird einzeln gefragt - auf
// einem Ziel ohne Groß-/Kleinschreibung gibt es die Datei, also Rückfrage;
// auf einem normalen Ziel nicht.
func TestCopyFoldedListingHitUsesLstat(t *testing.T) {
	src := newMemFS("/home/a")
	src.addDir("/home/a/d", 0o755)
	src.addFile("/home/a/d/README.md", "neu", 0o644, copyTestTime)

	folding := &foldFS{memFS: newMemFS("/srv/b")}
	folding.addDir("/srv/b/d", 0o755)
	folding.addFile("/srv/b/d/readme.md", "alt", 0o644, copyTestTime)
	_, conflicts := runCopySync(t, context.Background(), copyRequest{src: src, dst: folding, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"d"}},
		func(string) copyConflictAnswer { return copyAnswerSkip })
	if len(conflicts) != 1 {
		t.Errorf("Ziel ohne Groß-/Kleinschreibung: Rückfragen %v, erwartet eine", conflicts)
	}

	plain := &countingFS{memFS: newMemFS("/srv/b")}
	plain.addDir("/srv/b/d", 0o755)
	plain.addFile("/srv/b/d/Readme.md", "alt", 0o644, copyTestTime)
	_, conflicts = runCopySync(t, context.Background(), copyRequest{src: src, dst: plain, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"d"}}, noConflicts(t))
	if len(conflicts) != 0 {
		t.Errorf("normales Ziel: Rückfragen %v, erwartet keine", conflicts)
	}
	if got := plain.lstats.Load(); got < 1 {
		t.Error("Treffer nur ohne Groß-/Kleinschreibung: es wurde nicht einzeln nachgefragt")
	}
}

// TestCopyUnusualNamesUseLstat - Review-Fund 3: Namen, die ein Server selbst
// umdeuten kann (Punkt/Leerzeichen am Ende, ~ wie in 8.3-Kurznamen,
// Nicht-ASCII wegen Unicode-Normalisierung), werden am Ziel einzeln
// abgefragt; einfache Namen nicht.
func TestCopyUnusualNamesUseLstat(t *testing.T) {
	unusual := []string{"a.txt.", "b.txt ", "LONGFI~1.TXT", "ärger.txt"}
	src := newMemFS("/home/a")
	src.addDir("/home/a/d", 0o755)
	for _, name := range append([]string{"einfach.txt", "auch-einfach_2.md"}, unusual...) {
		src.addFile("/home/a/d/"+name, name, 0o644, copyTestTime)
	}
	dst := &countingFS{memFS: newMemFS("/srv/b")}
	dst.addDir("/srv/b/d", 0o755)
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"d"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 {
		t.Fatalf("Fehler %v", res.failures)
	}
	// 1 Lstat für "d" selbst ist erlaubt
	if got := dst.lstats.Load(); got != int64(len(unusual)) && got != int64(len(unusual))+1 {
		t.Errorf("Ziel: %d Lstat-Aufrufe, erwartet %d (je ungewöhnlichem Namen einer)", got, len(unusual))
	}
}

// zeroModeFS liefert in ReadDir keine Rechte-Angaben (Mode 0), Lstat schon -
// wie ein Server, der in READDIR nur Namen schickt.
type zeroModeFS struct{ *memFS }

func (z *zeroModeFS) ReadDir(p string) ([]os.FileInfo, error) {
	infos, err := z.memFS.ReadDir(p)
	out := make([]os.FileInfo, len(infos))
	for i, info := range infos {
		out[i] = fakeFileInfo{name: info.Name(), size: info.Size(), modTime: info.ModTime()}
	}
	return out, err
}

// TestCopyReadDirWithoutModeFallsBack - Review-Fund 6: fehlen in der
// Verzeichnisliste die Rechte, wird einzeln per Lstat gefragt - sonst würden
// Verzeichnisse als Dateien behandelt und Rechte 000 gesetzt.
func TestCopyReadDirWithoutModeFallsBack(t *testing.T) {
	src := &zeroModeFS{memFS: newCopySource()}
	dst := newMemFS("/srv/b")
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 {
		t.Fatalf("Fehler %v", res.failures)
	}
	if n := dst.node("/srv/b/projekt/lib"); n == nil || !n.mode.IsDir() {
		t.Error("lib wurde nicht als Verzeichnis angelegt")
	}
	if n := dst.node("/srv/b/projekt/run.sh"); n == nil || n.mode.Perm() != 0o755 {
		t.Errorf("run.sh: Rechte falsch (%v)", n)
	}
}

// TestCopyProgressReaches100AfterError - Review: scheitert eine Datei
// mittendrin, wird ihr Teilfortschritt zurückgenommen und sie als erledigt
// gezählt; am Ende stimmen Dateien und Bytes genau.
func TestCopyProgressReaches100AfterError(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	dst.failContent = "package main"
	dst.failAfter = 3
	events := make(chan copyEvent, 1024)
	answers := make(chan copyConflictAnswer, 1)
	go runCopy(context.Background(), copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt"}}, events, answers)
	var last copyProgress
	for ev := range events {
		if ev.progress != nil {
			last = *ev.progress
		}
		if ev.result != nil {
			break
		}
	}
	if last.doneFiles != last.totalFiles || last.doneBytes != last.totalBytes {
		t.Errorf("Fortschritt am Ende %d/%d Dateien, %d/%d Bytes", last.doneFiles, last.totalFiles, last.doneBytes, last.totalBytes)
	}
}

// TestPipeCopyPerFileLimit - Review-Fund 4: eine einzelne Datei belegt
// höchstens copyPipeChunks Blöcke, damit kleine Dateien daneben nicht warten.
func TestPipeCopyPerFileLimit(t *testing.T) {
	const chunk = 16
	b := newCopyBudget(1000*chunk, chunk)
	data := bytes.Repeat([]byte("z"), 200*chunk)
	var read atomic.Int64
	w := &slowWriter{read: &read, delay: time.Millisecond}
	if _, err := pipeCopy(context.Background(), w, &countingReader{r: bytes.NewReader(data), read: &read}, b); err != nil {
		t.Fatal(err)
	}
	if limit := int64(copyPipeChunks+1) * chunk; w.maxAhead > limit {
		t.Errorf("Leser lief %d Bytes voraus, pro Datei erlaubt %d", w.maxAhead, limit)
	}
}

// TestCopyNormalizedCollisionAsks - Sicherheitsreview Build 78: der
// Vergleich im Lauf darf nicht nur klein schreiben. "a.txt." landet auf
// Windows als "a.txt", das Kelvin-Zeichen K wird zu k gefaltet. Egal in
// welcher Reihenfolge die Einträge markiert sind: der zweite darf den
// ersten nicht still ersetzen, sondern es kommt die Rückfrage.
func TestCopyNormalizedCollisionAsks(t *testing.T) {
	pairs := [][2]string{
		{"a.txt.", "a.txt"},
		{"a.txt", "a.txt."},
		{"b.txt ", "b.txt"},
		{"\u212aey.txt", "key.txt"},
		{"key.txt", "\u212aey.txt"},
	}
	for _, pair := range pairs {
		for _, move := range []bool{false, true} {
			src := newMemFS("/home/a")
			src.addFile("/home/a/"+pair[0], "erste", 0o644, copyTestTime)
			src.addFile("/home/a/"+pair[1], "zweite", 0o644, copyTestTime)
			dst := &foldFS{memFS: newMemFS("/srv/b")}
			req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{pair[0], pair[1]}, move: move}
			_, conflicts := runCopySync(t, context.Background(), req, func(string) copyConflictAnswer { return copyAnswerSkip })
			if len(conflicts) != 1 {
				t.Errorf("%q dann %q (verschieben %v): Rückfragen %v, erwartet eine", pair[0], pair[1], move, conflicts)
				continue
			}
			if got, _ := dst.content(fold("/srv/b/" + pair[0])); got != "erste" {
				t.Errorf("%q dann %q: Ziel %q, erwartet erste (zweite übersprungen)", pair[0], pair[1], got)
			}
			if move {
				if got, ok := src.content("/home/a/" + pair[1]); !ok || got != "zweite" {
					t.Errorf("%q dann %q: übersprungene Quelle verloren", pair[0], pair[1])
				}
			}
		}
	}
}

// TestLooseTargetKey - der Vergleichsschlüssel faltet wie das großzügigste
// Ziel: Groß-/Kleinschreibung (auch Unicode wie das Kelvin-Zeichen) und
// Punkte/Leerzeichen am Ende jedes Namensteils.
func TestLooseTargetKey(t *testing.T) {
	same := [][2]string{
		{"Docs/README.md", "docs/readme.md"},
		{"a.txt.", "a.txt"},
		{"a.txt . .", "A.TXT"},
		{"dir./x", "DIR/x"},
		{"\u212aey", "key"},
		{"\u017fun", "SUN"},
	}
	for _, c := range same {
		a, _ := looseTargetKey(c[0])
		b, _ := looseTargetKey(c[1])
		if a != b {
			t.Errorf("looseTargetKey(%q) = %q, looseTargetKey(%q) = %q - müssen gleich sein", c[0], a, c[1], b)
		}
	}
	if _, simple := looseTargetKey("ordner/datei.txt"); !simple {
		t.Error("einfacher Name als ungewöhnlich eingestuft")
	}
	for _, rel := range []string{"a.txt.", "x/b ", "LONGFI~1.TXT", "\u00e4.txt", "\u212aey"} {
		if _, simple := looseTargetKey(rel); simple {
			t.Errorf("%q als einfach eingestuft", rel)
		}
	}
}
