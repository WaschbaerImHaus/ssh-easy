// Paket main - Tests: Upload über Internet-Latenz (Build 81)
//
// Nutzerbericht 2026-10-24: Kopieren von ~/x (11.869 Dateien, 3.763
// Verzeichnisse, 23 GB) auf ein Gerät im Internet lief mit 4-5 statt 40
// Mbit. Messung mit Wegwerf-sshd hinter einem Latenz-Proxy (40 Mbit):
//   - 2000 Dateien à 4 KB bei 40 ms Ping: 0,9 Mbit/s (rund 7 Rundreisen
//     pro Datei nacheinander, nur 8 Dateien gleichzeitig)
//   - 1 Datei mit 40 MB bei 120 ms Ping: 27 Mbit/s statt 38 (nach jedem
//     2-MiB-Block wartet Write auf alle Antworten, bevor der nächste startet)
//   - jedes Verzeichnis: Mkdir + chmod 0700 nacheinander im Ablauf, am
//     Ende chmod + Zeit nacheinander
//
// Abhilfe und hier geprüft:
//   - pipeCopyAt: mehrere Blöcke einer Datei gleichzeitig per WriteAt
//   - Rechte und Zeit nach dem Schreiben gleichzeitig setzen
//   - Verzeichnisse werden gleichzeitig angelegt (Geschwister parallel),
//     aber jedes erst, wenn sein Elternverzeichnis angelegt und auf 0700
//     gesetzt ist; Dateien darin ebenso (Sicherheitszusage Build 58)
//   - finishDirs setzt Rechte und Zeiten mehrerer Verzeichnisse gleichzeitig
//   - mehr Dateien gleichzeitig (copyParallel)
//   - watchConnection: tote Verbindung wird erkannt und geschlossen (sonst
//     hängt die Kopie ohne Ende, Esc wirkt nicht, die Temp-Datei bleibt liegen)
//
// @author Kurt Ingwer
// @date   2026-10-24 19:55
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Hilfen: Ziel mit Latenz, Schreiben an beliebiger Stelle ---

// atBuffer ist ein Ziel mit WriteAt, das gleichzeitige Schreibvorgänge zählt.
type atBuffer struct {
	mu      sync.Mutex
	data    []byte
	delay   time.Duration
	active  atomic.Int64
	maxAct  atomic.Int64
	failAt  int64
	read    *atomic.Int64
	written atomic.Int64
	// Höchster Vorlauf des Lesers vor dem Geschriebenen
	maxAhead atomic.Int64
}

func (b *atBuffer) WriteAt(p []byte, off int64) (int, error) {
	n := b.active.Add(1)
	defer b.active.Add(-1)
	raiseMax(&b.maxAct, n)
	if b.read != nil {
		raiseMax(&b.maxAhead, b.read.Load()-b.written.Load())
	}
	time.Sleep(b.delay)
	if b.failAt > 0 && off+int64(len(p)) > b.failAt {
		return 0, errors.New("Ziel voll")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if end := off + int64(len(p)); end > int64(len(b.data)) {
		b.data = append(b.data, make([]byte, end-int64(len(b.data)))...)
	}
	copy(b.data[off:], p)
	b.written.Add(int64(len(p)))
	return len(p), nil
}

func (b *atBuffer) bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data...)
}

// raiseMax setzt max auf n, wenn n größer ist.
func raiseMax(max *atomic.Int64, n int64) {
	for {
		old := max.Load()
		if n <= old || max.CompareAndSwap(old, n) {
			return
		}
	}
}

// latencyFS ist ein memFS mit Wartezeit pro Server-Anfrage. Seine Dateien
// können an beliebiger Stelle schreiben (WriteAt wie *sftp.File) und es
// zählt, welche Anfragen sich überlappen.
type latencyFS struct {
	*memFS
	delay      time.Duration
	attrDelay  time.Duration
	writeDelay time.Duration

	mu sync.Mutex
	// Laufende Rechte-/Zeit-Anfragen pro Pfad und deren Höchststand
	attrActive map[string]int
	attrMaxOne int
	// Pfade mit noch offenem Schreiber
	openWriters map[string]bool
	// Zeit gesetzt, während die Datei noch offen war
	timesWhileOpen []string
	// Gleichzeitige Rechte-/Zeit-Anfragen auf Verzeichnisse mit den echten
	// Werten (finishDirs) und deren Höchststand
	dirFinish    int
	dirFinishMax int
	// Verzeichnisse, deren echte Werte schon gesetzt werden, und Fälle, in
	// denen ein Elternverzeichnis vor seinem Unterverzeichnis dran war
	finishStarted map[string]bool
	parentFirst   []string
	// Neue Verzeichnisse: angelegt bzw. schon auf 0700 gesetzt; Anlegen von
	// Einträgen in einem angelegten, aber noch nicht gesperrten Verzeichnis
	mkdirDelay time.Duration
	madeDirs   map[string]bool
	lockedDirs map[string]bool
	unlocked   []string
	// Verzeichnisse, deren Mkdir scheitert
	failMkdir map[string]bool
	// Gleichzeitige Mkdir-Aufrufe (Höchststand)
	mkdirActive atomic.Int64
	mkdirMax    atomic.Int64
	// Gleichzeitige WriteAt-Aufrufe (Höchststand)
	writeActive atomic.Int64
	writeMax    atomic.Int64
	writeAts    atomic.Int64
}

func newLatencyFS(home string) *latencyFS {
	return &latencyFS{memFS: newMemFS(home), attrActive: map[string]int{}, openWriters: map[string]bool{}, finishStarted: map[string]bool{}, madeDirs: map[string]bool{}, lockedDirs: map[string]bool{}, failMkdir: map[string]bool{}}
}

// isDir meldet, ob p im Speicher ein Verzeichnis ist.
func (l *latencyFS) isDir(p string) bool {
	l.memFS.mu.Lock()
	defer l.memFS.mu.Unlock()
	n := l.memFS.nodes[p]
	return n != nil && n.mode.IsDir()
}

// beginAttr verbucht eine Rechte-/Zeit-Anfrage; finish = echte Werte
// eines Verzeichnisses (nicht das chmod 0700 beim Anlegen). attrMaxOne
// zählt nur Dateien (Verzeichnisse setzt finishDirs ebenfalls gleichzeitig).
func (l *latencyFS) beginAttr(p string, finish bool) {
	dir := l.isDir(p)
	l.mu.Lock()
	defer l.mu.Unlock()
	if finish {
		l.dirFinish++
		l.dirFinishMax = max(l.dirFinishMax, l.dirFinish)
		for q := range l.finishStarted {
			if strings.HasPrefix(p, q+"/") {
				l.parentFirst = append(l.parentFirst, q+" vor "+p)
			}
		}
		l.finishStarted[p] = true
	}
	l.attrActive[p]++
	if !dir {
		l.attrMaxOne = max(l.attrMaxOne, l.attrActive[p])
	}
}

func (l *latencyFS) endAttr(p string, finish bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if finish {
		l.dirFinish--
	}
	l.attrActive[p]--
}

func (l *latencyFS) Chmod(p string, mode os.FileMode) error {
	finish := l.isDir(p) && mode.Perm() != 0o700
	l.beginAttr(p, finish)
	defer l.endAttr(p, finish)
	time.Sleep(l.attrDelay)
	err := l.memFS.Chmod(p, mode)
	if err == nil && mode.Perm() == 0o700 {
		l.mu.Lock()
		l.lockedDirs[p] = true
		l.mu.Unlock()
	}
	return err
}

func (l *latencyFS) Chtimes(p string, atime, mtime time.Time) error {
	finish := l.isDir(p)
	l.beginAttr(p, finish)
	defer l.endAttr(p, finish)
	l.mu.Lock()
	if l.openWriters[p] {
		l.timesWhileOpen = append(l.timesWhileOpen, p)
	}
	l.mu.Unlock()
	time.Sleep(l.attrDelay)
	return l.memFS.Chtimes(p, atime, mtime)
}

// checkLocked merkt, wenn p in einem neu angelegten, aber noch nicht auf
// 0700 gesetzten Verzeichnis angelegt wird.
func (l *latencyFS) checkLocked(p string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if dir := path.Dir(p); l.madeDirs[dir] && !l.lockedDirs[dir] {
		l.unlocked = append(l.unlocked, p)
	}
}

func (l *latencyFS) Mkdir(p string) error {
	l.checkLocked(p)
	raiseMax(&l.mkdirMax, l.mkdirActive.Add(1))
	defer l.mkdirActive.Add(-1)
	time.Sleep(l.delay + l.mkdirDelay)
	l.mu.Lock()
	fail := l.failMkdir[p]
	l.mu.Unlock()
	if fail {
		return os.ErrPermission
	}
	err := l.memFS.Mkdir(p)
	if err == nil {
		l.mu.Lock()
		l.madeDirs[p] = true
		l.mu.Unlock()
	}
	return err
}

func (l *latencyFS) CreateExclusive(p string) (io.WriteCloser, error) {
	l.checkLocked(p)
	time.Sleep(l.delay)
	if _, err := l.memFS.CreateExclusive(p); err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.openWriters[p] = true
	l.mu.Unlock()
	return &latencyWriter{fs: l, p: p}, nil
}

func (l *latencyFS) Symlink(target, linkPath string) error {
	l.checkLocked(linkPath)
	time.Sleep(l.delay)
	return l.memFS.Symlink(target, linkPath)
}

// latencyWriter schreibt in einen memFS-Knoten, auch per WriteAt.
type latencyWriter struct {
	fs  *latencyFS
	p   string
	off int64
}

func (w *latencyWriter) WriteAt(b []byte, off int64) (int, error) {
	w.fs.writeAts.Add(1)
	raiseMax(&w.fs.writeMax, w.fs.writeActive.Add(1))
	defer w.fs.writeActive.Add(-1)
	time.Sleep(w.fs.writeDelay)
	if w.fs.onWrite != nil {
		w.fs.onWrite()
	}
	m := w.fs.memFS
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.nodes[w.p]
	if end := off + int64(len(b)); end > int64(len(n.data)) {
		n.data = append(n.data, make([]byte, end-int64(len(n.data)))...)
	}
	copy(n.data[off:], b)
	return len(b), nil
}

func (w *latencyWriter) Write(b []byte) (int, error) {
	n, err := w.WriteAt(b, w.off)
	w.off += int64(n)
	return n, err
}

func (w *latencyWriter) Close() error {
	time.Sleep(w.fs.delay)
	w.fs.mu.Lock()
	delete(w.fs.openWriters, w.p)
	w.fs.mu.Unlock()
	return nil
}

// bigContent liefert gut unterscheidbaren Inhalt (Fehler an Blockgrenzen
// fallen so auf).
func bigContent(n int) []byte {
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(i*7 + i/251)
	}
	return data
}

// --- pipeCopyAt ---

// TestPipeCopyAtContent - Inhalt kommt vollständig und an der richtigen
// Stelle an, auch an Blockgrenzen und bei leeren Dateien.
func TestPipeCopyAtContent(t *testing.T) {
	const chunk = 8
	for _, size := range []int{0, 1, chunk - 1, chunk, chunk + 1, 3*chunk + 5, 100} {
		data := bigContent(size)
		w := &atBuffer{}
		var reported atomic.Int64
		n, err := pipeCopyAt(context.Background(), w, bytes.NewReader(data), newCopyBudget(16*chunk, chunk), func(k int) { reported.Add(int64(k)) })
		if err != nil || n != int64(size) || !bytes.Equal(w.bytes(), data) {
			t.Errorf("Größe %d: n=%d err=%v, Inhalt gleich: %v", size, n, err, bytes.Equal(w.bytes(), data))
		}
		if reported.Load() != int64(size) {
			t.Errorf("Größe %d: Fortschritt %d gemeldet", size, reported.Load())
		}
	}
}

// TestPipeCopyAtOverlapsWrites - mehrere Blöcke einer Datei sind
// gleichzeitig unterwegs (keine Pause nach jedem Block), aber höchstens
// copyWritesInFlight.
func TestPipeCopyAtOverlapsWrites(t *testing.T) {
	const chunk = 16
	data := bigContent(40 * chunk)
	w := &atBuffer{delay: 3 * time.Millisecond}
	if _, err := pipeCopyAt(context.Background(), w, bytes.NewReader(data), newCopyBudget(32*chunk, chunk), nil); err != nil {
		t.Fatal(err)
	}
	if got := w.maxAct.Load(); got < 2 {
		t.Errorf("höchstens %d Block gleichzeitig geschrieben, erwartet mehrere", got)
	}
	if got := w.maxAct.Load(); got > copyWritesInFlight {
		t.Errorf("%d Blöcke gleichzeitig, Grenze %d", got, copyWritesInFlight)
	}
	if !bytes.Equal(w.bytes(), data) {
		t.Error("Inhalt verfälscht")
	}
}

// TestPipeCopyAtErrors - Schreib- und Lesefehler kommen beim Aufrufer an,
// ein Abbruch beendet pipeCopyAt ohne Hänger, und das Budget ist danach
// wieder vollständig frei (kein Leck, auch bei laufenden Schreibvorgängen).
func TestPipeCopyAtErrors(t *testing.T) {
	const chunk = 8
	b := newCopyBudget(8*chunk, chunk)
	data := bigContent(40 * chunk)

	w := &atBuffer{failAt: 5 * chunk, delay: time.Millisecond}
	if _, err := pipeCopyAt(context.Background(), w, bytes.NewReader(data), b, nil); err == nil || !strings.Contains(err.Error(), "voll") {
		t.Errorf("Schreibfehler nicht gemeldet: %v", err)
	}

	var read atomic.Int64
	readErr := errors.New("Verbindung weg")
	if _, err := pipeCopyAt(context.Background(), &atBuffer{}, &countingReader{r: bytes.NewReader(data[:5*chunk]), read: &read, err: readErr}, b, nil); !errors.Is(err, readErr) {
		t.Errorf("Lesefehler nicht gemeldet: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := pipeCopyAt(ctx, &atBuffer{delay: 2 * time.Millisecond}, bytes.NewReader(data), b, nil)
		done <- err
	}()
	time.Sleep(5 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Abbruch: Fehler %v, erwartet context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pipeCopyAt hängt nach Abbruch")
	}
	if got := b.free(); got != 8 {
		t.Errorf("Budget danach %d Blöcke frei, erwartet 8 (Leck)", got)
	}
}

// TestPipeCopyAtPerFileLimit - eine Datei belegt auch mit gleichzeitigen
// Schreibvorgängen höchstens copyPipeChunks Blöcke des gemeinsamen Budgets.
func TestPipeCopyAtPerFileLimit(t *testing.T) {
	const chunk = 16
	data := bigContent(60 * chunk)
	var read atomic.Int64
	w := &atBuffer{delay: time.Millisecond, read: &read}
	if _, err := pipeCopyAt(context.Background(), w, &countingReader{r: bytes.NewReader(data), read: &read}, newCopyBudget(100*chunk, chunk), nil); err != nil {
		t.Fatal(err)
	}
	if limit := int64(copyPipeChunks+1) * chunk; w.maxAhead.Load() > limit {
		t.Errorf("Leser lief %d Bytes voraus, Grenze %d", w.maxAhead.Load(), limit)
	}
}

// --- runCopy über ein Ziel mit Latenz ---

// TestCopyLargeFileUsesConcurrentWriteAt - eine große Datei wird mit
// mehreren gleichzeitigen WriteAt-Aufrufen geschrieben; Inhalt, Rechte und
// Zeit stimmen, keine Temp-Datei bleibt liegen.
func TestCopyLargeFileUsesConcurrentWriteAt(t *testing.T) {
	data := bigContent(5*copyChunkSize + 123)
	src := newMemFS("/home/a")
	src.addFile("/home/a/gross.bin", string(data), 0o640, copyTestTime)
	dst := newLatencyFS("/srv/b")
	dst.writeDelay = 5 * time.Millisecond
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"gross.bin"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 || res.files != 1 || res.bytes != int64(len(data)) {
		t.Fatalf("Fehler %v, Dateien %d, Bytes %d", res.failures, res.files, res.bytes)
	}
	got, _ := dst.content("/srv/b/gross.bin")
	if got != string(data) {
		t.Error("Inhalt verfälscht")
	}
	if dst.writeMax.Load() < 2 {
		t.Errorf("höchstens %d WriteAt gleichzeitig, erwartet mehrere", dst.writeMax.Load())
	}
	node := dst.node("/srv/b/gross.bin")
	if node.mode.Perm() != 0o640 || !node.modTime.Equal(copyTestTime) {
		t.Errorf("Rechte %v, Zeit %v", node.mode.Perm(), node.modTime)
	}
	assertNoTempFiles(t, dst.memFS)
}

// TestCopyCancelWithWriteAtRemovesTemp - Abbruch mitten in gleichzeitigen
// Schreibvorgängen: Ergebnis "abgebrochen", keine Temp-Datei bleibt liegen.
func TestCopyCancelWithWriteAtRemovesTemp(t *testing.T) {
	data := bigContent(8 * copyChunkSize)
	src := newMemFS("/home/a")
	src.addFile("/home/a/gross.bin", string(data), 0o644, copyTestTime)
	dst := newLatencyFS("/srv/b")
	dst.writeDelay = 2 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var once sync.Once
	dst.onWrite = func() { once.Do(cancel) }
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"gross.bin"}}
	res, _ := runCopySync(t, ctx, req, noConflicts(t))
	if !res.cancelled {
		t.Errorf("nicht als abgebrochen gemeldet: %+v", res)
	}
	if _, ok := dst.content("/srv/b/gross.bin"); ok {
		t.Error("unvollständige Datei liegt unter dem Zielnamen")
	}
	assertNoTempFiles(t, dst.memFS)
}

// TestCopySetsAttributesTogether - Rechte und Zeit einer Datei werden nach
// dem Schreiben gleichzeitig gesetzt (eine Rundreise statt zwei), aber die
// Zeit erst, wenn die Datei geschlossen ist.
func TestCopySetsAttributesTogether(t *testing.T) {
	src := manyFilesSource(6)
	dst := newLatencyFS("/srv/b")
	dst.attrDelay = 15 * time.Millisecond
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"viele"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 || res.files != 6 {
		t.Fatalf("Fehler %v, kopiert %d", res.failures, res.files)
	}
	if dst.attrMaxOne < 2 {
		t.Errorf("Rechte und Zeit einer Datei nacheinander gesetzt (höchstens %d gleichzeitig)", dst.attrMaxOne)
	}
	if len(dst.timesWhileOpen) > 0 {
		t.Errorf("Zeit gesetzt, während die Datei noch offen war: %v", dst.timesWhileOpen)
	}
	for _, p := range []string{"/srv/b/viele/datei-000.txt", "/srv/b/viele/datei-005.txt"} {
		node := dst.node(p)
		if node == nil || node.mode.Perm() != 0o640 || !node.modTime.Equal(copyTestTime) {
			t.Errorf("%s: %+v", p, node)
		}
	}
}

// dirTreeSource liefert n Verzeichnisse mit eigenen Rechten und Zeiten
// unter /home/a/baum (je eine Datei darin), das erste mit zwei Ebenen
// Unterverzeichnissen.
func dirTreeSource(n int) *memFS {
	src := newMemFS("/home/a")
	src.addDir("/home/a/baum", 0o751)
	src.nodes["/home/a/baum"].modTime = copyTestTime
	for i := 0; i < n; i++ {
		d := "/home/a/baum/d" + string(rune('a'+i))
		src.addDir(d, 0o750)
		src.nodes[d].modTime = copyTestTime.Add(time.Duration(i) * time.Hour)
		src.addFile(d+"/f.txt", "x", 0o600, copyTestTime)
	}
	src.addDir("/home/a/baum/da/innen", 0o711)
	src.addDir("/home/a/baum/da/innen/tief", 0o755)
	return src
}

// TestFinishDirsInParallel - finishDirs setzt Rechte und Zeiten mehrerer
// Verzeichnisse gleichzeitig (bei 3.763 Verzeichnissen und 40 ms Ping
// sonst 5 Minuten Wartezeit), aber jedes Unterverzeichnis vor seinem
// Elternverzeichnis (Rechte ohne x-Bit sperrten sonst den Weg nach innen).
// Am Ende stimmen Rechte und Zeiten genau.
func TestFinishDirsInParallel(t *testing.T) {
	const n = 8
	src := dirTreeSource(n)
	dst := newLatencyFS("/srv/b")
	dst.attrDelay = 20 * time.Millisecond
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"baum"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 || res.files != n {
		t.Fatalf("Fehler %v, kopiert %d", res.failures, res.files)
	}
	if dst.dirFinishMax < 3 {
		t.Errorf("finishDirs: höchstens %d Anfragen gleichzeitig, erwartet mehrere", dst.dirFinishMax)
	}
	if len(dst.parentFirst) > 0 {
		t.Errorf("Elternverzeichnis vor Unterverzeichnis gesetzt: %v", dst.parentFirst)
	}
	for p, want := range map[string]fs.FileMode{"/srv/b/baum/da/innen": 0o711, "/srv/b/baum/da/innen/tief": 0o755} {
		if node := dst.node(p); node == nil || node.mode.Perm() != want {
			t.Errorf("%s: %+v, erwartet %v", p, node, want)
		}
	}
	if node := dst.node("/srv/b/baum"); node.mode.Perm() != 0o751 || !node.modTime.Equal(copyTestTime) {
		t.Errorf("baum: Rechte %v, Zeit %v", node.mode.Perm(), node.modTime)
	}
	for i := 0; i < n; i++ {
		d := "/srv/b/baum/d" + string(rune('a'+i))
		node := dst.node(d)
		want := copyTestTime.Add(time.Duration(i) * time.Hour)
		if node == nil || node.mode != fs.ModeDir|0o750 || !node.modTime.Equal(want) {
			t.Errorf("%s: %+v", d, node)
		}
	}
}

// TestMoveWithParallelFinishDirs - Verschieben mit Verzeichnissen: auch
// mit parallelem finishDirs ist die Quelle danach leer und das Ziel stimmt.
func TestMoveWithParallelFinishDirs(t *testing.T) {
	src := dirTreeSource(4)
	dst := newLatencyFS("/srv/b")
	dst.attrDelay = 5 * time.Millisecond
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"baum"}, move: true}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 || res.files != 4 {
		t.Fatalf("Fehler %v, kopiert %d", res.failures, res.files)
	}
	if src.node("/home/a/baum") != nil {
		t.Error("Quelle nach dem Verschieben nicht gelöscht")
	}
	if got, _ := dst.content("/srv/b/baum/dd/f.txt"); got != "x" {
		t.Errorf("Ziel: %q", got)
	}
}

// TestCopyParallelSuitsInternetLatency - bei 40 ms Ping braucht eine kleine
// Datei rund 5 Rundreisen = 200 ms. Erst mit vielen gleichzeitigen Dateien
// wird der Upload nicht von der Wartezeit bestimmt (Messung 2026-10-24).
func TestCopyParallelSuitsInternetLatency(t *testing.T) {
	if copyParallel < 32 {
		t.Errorf("copyParallel = %d, für Internet-Latenz mindestens 32 nötig", copyParallel)
	}
}

// --- watchConnection: tote Verbindungen erkennen ---

// TestWatchConnectionClosesSilentConnection - antwortet die Gegenseite
// nicht mehr (Leitung tot, kein TCP-Reset), wird die Verbindung nach
// spätestens Intervall + Timeout geschlossen. Erst dann kehren hängende
// SFTP-Anfragen zurück.
func TestWatchConnectionClosesSilentConnection(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	killed := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchConnection(ctx, func() error { <-block; return nil }, func() { close(killed) }, func() time.Time { return time.Time{} }, 5*time.Millisecond, 20*time.Millisecond)
	select {
	case <-killed:
	case <-time.After(2 * time.Second):
		t.Fatal("tote Verbindung nicht geschlossen")
	}
}

// TestWatchConnectionClosesOnError - ein Fehler beim Keepalive schließt
// die Verbindung sofort.
func TestWatchConnectionClosesOnError(t *testing.T) {
	killed := make(chan struct{})
	go watchConnection(context.Background(), func() error { return io.EOF }, func() { close(killed) }, time.Now, 5*time.Millisecond, time.Second)
	select {
	case <-killed:
	case <-time.After(2 * time.Second):
		t.Fatal("Verbindung trotz Keepalive-Fehler nicht geschlossen")
	}
}

// TestWatchConnectionKeepsHealthyConnection - solange die Gegenseite
// antwortet, bleibt die Verbindung offen; nach Ende des Kontexts (Close)
// hört die Überwachung auf, ohne zu schließen.
func TestWatchConnectionKeepsHealthyConnection(t *testing.T) {
	var pings atomic.Int64
	var killed atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watchConnection(ctx, func() error { pings.Add(1); return nil }, func() { killed.Store(true) }, func() time.Time { return time.Time{} }, 2*time.Millisecond, 50*time.Millisecond)
		close(done)
	}()
	time.Sleep(40 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Überwachung endet nicht mit dem Kontext")
	}
	if killed.Load() {
		t.Error("gesunde Verbindung geschlossen")
	}
	if pings.Load() < 3 {
		t.Errorf("nur %d Keepalives gesendet", pings.Load())
	}
}

// TestFMKeepaliveTimingsFitSlowLinks - eine tote Leitung fällt nach
// spätestens einer Minute auf; der Timeout lässt einer langsamen Leitung
// Zeit (ein volles SSH-Fenster von 2 MiB braucht bei 1 Mbit/s 17 s - noch
// langsamere Leitungen hält TestWatchConnectionKeepsSlowActiveLink offen).
func TestFMKeepaliveTimingsFitSlowLinks(t *testing.T) {
	if fmKeepaliveTimeout < 30*time.Second {
		t.Errorf("fmKeepaliveTimeout = %v, zu kurz für langsame Leitungen", fmKeepaliveTimeout)
	}
	if total := fmKeepaliveInterval + fmKeepaliveTimeout; total > time.Minute {
		t.Errorf("tote Leitung erst nach %v erkannt", total)
	}
}

// --- Meldung bei verlorener Verbindung ---

// lostFS meldet eine verlorene Verbindung (wie sftpFS, nachdem
// watchConnection sie geschlossen hat oder sie abgerissen ist).
type lostFS struct {
	*memFS
	lost atomic.Bool
}

func (l *lostFS) connectionLost() bool { return l.lost.Load() }

// TestCopyFailureAfterConnectionLoss - scheitert eine Datei, nachdem die
// Verbindung verloren ging, wird der Fehler als errConnectionLost gemeldet
// (statt eines rätselhaften "EOF"); die ursprüngliche Ursache bleibt im Text.
func TestCopyFailureAfterConnectionLoss(t *testing.T) {
	src := manyFilesSource(3)
	dst := &lostFS{memFS: newMemFS("/srv/b")}
	dst.failContent, dst.failAfter = "Inhalt 1", 2
	dst.lost.Store(true)
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"viele"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) == 0 {
		t.Fatal("kein Fehler gemeldet")
	}
	err := res.failures[0].err
	if !errors.Is(err, errConnectionLost) || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("Fehler %v, erwartet errConnectionLost mit Ursache", err)
	}
}

// TestCopyFailureWithLiveConnection - ohne Verbindungsverlust bleibt der
// Fehler unverändert (Gegenprobe).
func TestCopyFailureWithLiveConnection(t *testing.T) {
	src := manyFilesSource(3)
	dst := &lostFS{memFS: newMemFS("/srv/b")}
	dst.failContent, dst.failAfter = "Inhalt 1", 2
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"viele"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 1 || errors.Is(res.failures[0].err, errConnectionLost) {
		t.Errorf("Fehler %v", res.failures)
	}
}

// TestFileManagerConnectionLostMessage - die Meldung nennt die Datei und
// sagt in der Sprache des Nutzers, dass die Verbindung unterbrochen wurde.
func TestFileManagerConnectionLostMessage(t *testing.T) {
	m := openCopyManager(t, newCopySource(), newMemFS("/srv/b"))
	got := m.fmDescribeFailure(copyFailure{path: "/srv/b/x.iso", err: errors.Join(errConnectionLost, io.EOF)})
	if want := fmt.Sprintf(m.tr(txtFMErrConnectionLost), "/srv/b/x.iso"); got != want {
		t.Errorf("Meldung %q, erwartet %q", got, want)
	}
}

// TestConnectionLostErrorText - der Text nennt den Verlust und die
// Ursache, ohne "connection lost" zu doppeln (pkg/sftp meldet es selbst).
func TestConnectionLostErrorText(t *testing.T) {
	cases := map[string]string{
		"disk full":                           "connection lost: disk full",
		"connection lost":                     "connection lost",
		"sftp: connection lost while writing": "sftp: connection lost while writing",
	}
	for cause, want := range cases {
		err := &connectionLostError{err: errors.New(cause)}
		if err.Error() != want {
			t.Errorf("%q: %q, erwartet %q", cause, err.Error(), want)
		}
		if !errors.Is(err, errConnectionLost) {
			t.Errorf("%q: errors.Is(errConnectionLost) = false", cause)
		}
	}
	if !errors.Is(&connectionLostError{err: io.EOF}, io.EOF) {
		t.Error("ursprünglicher Fehler nicht mehr erkennbar")
	}
}

// --- Verzeichnisse gleichzeitig anlegen ---

// TestCopyCreatesSiblingDirsConcurrently - Geschwister-Verzeichnisse werden
// gleichzeitig angelegt (bei 3.763 Verzeichnissen und 120 ms Ping sonst
// über 15 Minuten Wartezeit); in keinem neuen Verzeichnis wird etwas
// angelegt, bevor es auf 0700 gesetzt ist. Am Ende stimmt alles.
func TestCopyCreatesSiblingDirsConcurrently(t *testing.T) {
	const n = 8
	src := dirTreeSource(n)
	dst := newLatencyFS("/srv/b")
	dst.mkdirDelay = 15 * time.Millisecond
	dst.attrDelay = 5 * time.Millisecond
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"baum"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 || res.files != n {
		t.Fatalf("Fehler %v, kopiert %d", res.failures, res.files)
	}
	if got := dst.mkdirMax.Load(); got < 3 {
		t.Errorf("höchstens %d Verzeichnis(se) gleichzeitig angelegt, erwartet mehrere", got)
	}
	if len(dst.unlocked) > 0 {
		t.Errorf("angelegt in Verzeichnis vor chmod 0700: %v", dst.unlocked)
	}
	for i := 0; i < n; i++ {
		p := "/srv/b/baum/d" + string(rune('a'+i)) + "/f.txt"
		if got, _ := dst.content(p); got != "x" {
			t.Errorf("%s = %q", p, got)
		}
	}
	if node := dst.node("/srv/b/baum/da/innen/tief"); node == nil || node.mode.Perm() != 0o755 {
		t.Errorf("tief: %+v", node)
	}
	assertNoTempFiles(t, dst.memFS)
}

// TestCopyFailedDirSkipsContentOnly - scheitert das Anlegen eines
// Verzeichnisses, wird genau dieser eine Fehler gemeldet; sein Inhalt und
// seine Unterverzeichnisse werden übersprungen (der Fortschritt erreicht
// trotzdem 100 %), die Geschwister werden normal kopiert.
func TestCopyFailedDirSkipsContentOnly(t *testing.T) {
	const n = 4
	src := dirTreeSource(n)
	dst := newLatencyFS("/srv/b")
	dst.mkdirDelay = 5 * time.Millisecond
	dst.failMkdir["/srv/b/baum/da"] = true
	events := make(chan copyEvent, 1024)
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"baum"}}
	go runCopy(context.Background(), req, events, make(chan copyConflictAnswer, 1))
	var res *copyResult
	var last copyProgress
	timeout := time.After(10 * time.Second)
	for res == nil {
		select {
		case ev := <-events:
			if ev.progress != nil {
				last = *ev.progress
			}
			res = ev.result
		case <-timeout:
			t.Fatal("runCopy hängt")
		}
	}
	if len(res.failures) != 1 || res.failures[0].path != "/srv/b/baum/da" {
		t.Fatalf("Fehler %v, erwartet genau /srv/b/baum/da", res.failures)
	}
	if res.files != n-1 {
		t.Errorf("%d Dateien kopiert, erwartet %d", res.files, n-1)
	}
	if dst.node("/srv/b/baum/da/innen") != nil || dst.node("/srv/b/baum/da/f.txt") != nil {
		t.Error("Inhalt des gescheiterten Verzeichnisses trotzdem angelegt")
	}
	if last.doneFiles != last.totalFiles || last.doneBytes != last.totalBytes {
		t.Errorf("Fortschritt am Ende %d/%d Dateien, %d/%d Bytes", last.doneFiles, last.totalFiles, last.doneBytes, last.totalBytes)
	}
	assertNoTempFiles(t, dst.memFS)
}

// TestMoveFailedEmptyDirKeepsSource - Verschieben: scheitert das Anlegen
// eines (leeren) Verzeichnisses, bleibt es in der Quelle erhalten.
func TestMoveFailedEmptyDirKeepsSource(t *testing.T) {
	src := dirTreeSource(2)
	src.addDir("/home/a/baum/leer", 0o755)
	dst := newLatencyFS("/srv/b")
	dst.failMkdir["/srv/b/baum/leer"] = true
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"baum"}, move: true}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 1 {
		t.Fatalf("Fehler %v", res.failures)
	}
	if src.node("/home/a/baum/leer") == nil {
		t.Error("leeres Quellverzeichnis gelöscht, obwohl es am Ziel nicht angelegt wurde")
	}
	if src.node("/home/a/baum/db/f.txt") != nil {
		t.Error("erfolgreich verschobene Datei liegt noch in der Quelle")
	}
}

// --- Sicherheitsaudit Build 81: Mkdir scheitert, fremder Ordner ---

// trapFS lässt Mkdir eines Verzeichnisses scheitern, nachdem dort ein
// fremder Ordner mit Unterverzeichnissen aufgetaucht ist - wie ein
// Angreifer, der zwischen Einlesen und Mkdir einen Symlink bzw. eine
// Junction auf einen Opferordner unterschiebt.
type trapFS struct {
	*memFS
	trap    string
	victims []string
}

func (f *trapFS) Mkdir(p string) error {
	if p != f.trap {
		return f.memFS.Mkdir(p)
	}
	f.memFS.mu.Lock()
	defer f.memFS.mu.Unlock()
	f.memFS.nodes[p] = &memNode{mode: fs.ModeDir | 0o755}
	for _, v := range f.victims {
		f.memFS.nodes[v] = &memNode{mode: fs.ModeDir | 0o755}
	}
	return os.ErrExist
}

// TestCopyFailedMkdirNeverWritesIntoForeignDir - scheitert das Anlegen
// eines Verzeichnisses, wird in dem dort aufgetauchten fremden Ordner
// nichts angelegt (keine Datei, keine Temp- oder Prüfdatei, keine
// Rückfrage), auch nicht über Namen, die einzeln per Lstat geprüft werden
// (Umlaut, "~", Groß-/Kleinschreibung). Es gibt genau einen Fehler; beim
// Verschieben bleibt die Quelle vollständig erhalten. Gegenprobe zum Fund
// des Sicherheitsaudits (Build 80 übersprang den Teilbaum).
func TestCopyFailedMkdirNeverWritesIntoForeignDir(t *testing.T) {
	cases := []struct {
		name    string
		files   []string
		victims []string
	}{
		{"Umlaut", []string{"sub/\u00fc/geheim.txt"}, []string{"/srv/b/sub/\u00fc"}},
		{"Tilde", []string{"sub/a~b/geheim.txt"}, []string{"/srv/b/sub/a~b"}},
		{"Kollision", []string{"sub/Docs/a.txt", "sub/docs/b.txt"}, []string{"/srv/b/sub/Docs", "/srv/b/sub/docs"}},
	}
	for _, c := range cases {
		for _, move := range []bool{false, true} {
			src := newMemFS("/home/a")
			for _, f := range c.files {
				src.addFile("/home/a/"+f, "geheim", 0o600, copyTestTime)
			}
			dst := &trapFS{memFS: newMemFS("/srv/b"), trap: "/srv/b/sub"}
			dst.victims = c.victims
			req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"sub"}, move: move}
			res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
			if len(res.failures) != 1 || res.failures[0].path != "/srv/b/sub" {
				t.Errorf("%s (verschieben %v): Fehler %v, erwartet genau /srv/b/sub", c.name, move, res.failures)
			}
			dst.memFS.mu.Lock()
			for p := range dst.memFS.nodes {
				for _, v := range dst.victims {
					if strings.HasPrefix(p, v+"/") {
						t.Errorf("%s (verschieben %v): im fremden Ordner angelegt: %s", c.name, move, p)
					}
				}
			}
			dst.memFS.mu.Unlock()
			for _, f := range c.files {
				if _, ok := src.content("/home/a/" + f); !ok {
					t.Errorf("%s (verschieben %v): Quelle %s gelöscht", c.name, move, f)
				}
			}
		}
	}
}

// TestWatchConnectionKeepsSlowActiveLink - Code-Review Build 81: auf einer
// sehr langsamen Leitung (gedrosselter Mobilfunk) steckt die Antwort auf
// das Keepalive hinter bis zu 2 MiB Daten und kommt erst nach dem Timeout.
// Solange vom Server laufend etwas ankommt, lebt die Verbindung und wird
// nicht geschlossen; erst wenn auch das ausbleibt.
func TestWatchConnectionKeepsSlowActiveLink(t *testing.T) {
	release := make(chan struct{})
	var killed atomic.Bool
	var quiet atomic.Bool
	lastActive := func() time.Time {
		if quiet.Load() {
			return time.Now().Add(-time.Hour)
		}
		return time.Now()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		watchConnection(ctx, func() error { <-release; return nil }, func() { killed.Store(true) }, lastActive, 2*time.Millisecond, 10*time.Millisecond)
		close(done)
	}()
	time.Sleep(80 * time.Millisecond)
	if killed.Load() {
		t.Fatal("langsame, aber aktive Verbindung geschlossen")
	}
	// Jetzt kommt nichts mehr an: tot
	quiet.Store(true)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stille Verbindung nicht geschlossen")
	}
	close(release)
	if !killed.Load() {
		t.Error("kill nicht aufgerufen")
	}
}

// TestActivityReaderMarksActivity - jedes vom Server gelesene Byte zählt
// als Lebenszeichen der Verbindung.
func TestActivityReaderMarksActivity(t *testing.T) {
	var last atomic.Int64
	r := &activityReader{r: strings.NewReader("abc"), last: &last}
	if data, err := io.ReadAll(r); err != nil || string(data) != "abc" {
		t.Fatalf("%q, %v", data, err)
	}
	if got := time.Unix(0, last.Load()); time.Since(got) > time.Second {
		t.Errorf("letzte Aktivität %v", got)
	}
}

// TestCopyCancelAfterDispatchReportsCancelled - Code-Review Build 81: Esc,
// nachdem der Ablauf alle Einträge verteilt hat, aber bevor ein leeres
// Verzeichnis angelegt ist: Ergebnis "abgebrochen", nicht "fertig".
func TestCopyCancelAfterDispatchReportsCancelled(t *testing.T) {
	src := newMemFS("/home/a")
	src.addDir("/home/a/baum", 0o755)
	src.addDir("/home/a/baum/leer", 0o755)
	dst := newLatencyFS("/srv/b")
	dst.mkdirDelay = 80 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"baum"}}
	res, _ := runCopySync(t, ctx, req, noConflicts(t))
	if dst.node("/srv/b/baum/leer") == nil && !res.cancelled {
		t.Errorf("leeres Verzeichnis fehlt, gemeldet als fertig: %+v", res)
	}
}

// TestCopySymlinkWaitsForLockedDir - Code-Review Build 81: ein Symlink in
// einem neuen Verzeichnis wird erst angelegt, wenn das Verzeichnis 0700
// ist; scheitert das Verzeichnis, wird der Symlink übersprungen.
func TestCopySymlinkWaitsForLockedDir(t *testing.T) {
	for _, fail := range []bool{false, true} {
		src := newMemFS("/home/a")
		src.addDir("/home/a/baum", 0o755)
		src.addLink("/home/a/baum/link", "ziel.txt")
		dst := newLatencyFS("/srv/b")
		dst.mkdirDelay = 20 * time.Millisecond
		dst.attrDelay = 20 * time.Millisecond
		if fail {
			dst.failMkdir["/srv/b/baum"] = true
		}
		req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"baum"}}
		res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
		if len(dst.unlocked) > 0 {
			t.Errorf("Symlink vor chmod 0700 angelegt: %v", dst.unlocked)
		}
		link := dst.node("/srv/b/baum/link")
		switch {
		case fail && (link != nil || len(res.failures) != 1):
			t.Errorf("gescheitertes Verzeichnis: Link %+v, Fehler %v", link, res.failures)
		case !fail && (link == nil || len(res.failures) != 0):
			t.Errorf("Link %+v, Fehler %v", link, res.failures)
		}
	}
}

// TestCopyDirJobsBounded - Code-Review Build 81: der Ablauf startet nicht
// beliebig viele wartende Verzeichnis-Aufträge auf einmal (bei einer
// Million Verzeichnissen sonst GB an Goroutinen).
func TestCopyDirJobsBounded(t *testing.T) {
	src := newMemFS("/home/a")
	src.addDir("/home/a/baum", 0o755)
	for i := 0; i < 600; i++ {
		src.addDir(fmt.Sprintf("/home/a/baum/d%03d", i), 0o755)
	}
	dst := newLatencyFS("/srv/b")
	dst.mkdirDelay = 2 * time.Millisecond
	var peak atomic.Int64
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				raiseMax(&peak, int64(runtime.NumGoroutine()))
				time.Sleep(200 * time.Microsecond)
			}
		}
	}()
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"baum"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	close(stop)
	if len(res.failures) != 0 || dst.node("/srv/b/baum/d599") == nil {
		t.Fatalf("Fehler %v", res.failures)
	}
	if limit := int64(copyMaxPendingDirs + 2*copyParallel + 50); peak.Load() > limit {
		t.Errorf("bis zu %d Goroutinen, Grenze etwa %d", peak.Load(), limit)
	}
}
