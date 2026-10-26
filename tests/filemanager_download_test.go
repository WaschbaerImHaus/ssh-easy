// Paket main - Tests: Lesen mehrerer Blöcke gleichzeitig (Build 82)
//
// Vom Server auf den PC lief es wie vor Build 81 beim Hochladen: Read auf
// einer *sftp.File teilt einen 2-MiB-Block in viele Anfragen und wartet
// auf alle Antworten, bevor der nächste Block startet - eine Pause von
// einer Rundreise pro Block. Quellen mit io.ReaderAt (sftp.File, os.File)
// liest pipeCopy/pipeCopyAt jetzt mit bis zu copyReadsInFlight Blöcken
// gleichzeitig, in der richtigen Reihenfolge an den Schreiber.
//
// @author Kurt Ingwer
// @date   2026-10-24 21:49
package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// atReader ist eine Quelle mit ReadAt (wie *sftp.File), die gleichzeitige
// Lesevorgänge zählt. Sequentielles Read wird nur gezählt und scheitert -
// so fällt auf, wenn der schnelle Weg nicht genommen wird.
type atReader struct {
	data  []byte
	delay time.Duration
	// Laufende Lesevorgänge und deren Höchststand
	active atomic.Int64
	maxAct atomic.Int64
	// Gelesene Bytes (für den Vorlauf, gemeinsam mit dem Schreiber)
	read *atomic.Int64
	// Lesen über diese Stelle scheitert (0 = nie)
	failAt int64
	// Höchstens 5 Bytes pro Aufruf ohne Fehler (bricht den ReaderAt-Vertrag)
	short bool
	// Liefert 0 Bytes ohne Fehler (würde ewig laufen)
	stuck bool
	// Aufrufe von Read (sequentiell)
	seqReads atomic.Int64
	// Lesevorgänge, die nach Close noch liefen bzw. begannen
	afterClose atomic.Int64
	closed     atomic.Bool
	// Aufrufe von ReadAt
	calls atomic.Int64
}

func (r *atReader) ReadAt(p []byte, off int64) (int, error) {
	r.calls.Add(1)
	n := r.active.Add(1)
	defer r.active.Add(-1)
	raiseMax(&r.maxAct, n)
	if r.closed.Load() {
		r.afterClose.Add(1)
	}
	time.Sleep(r.delay)
	if r.closed.Load() {
		r.afterClose.Add(1)
	}
	if r.stuck {
		return 0, nil
	}
	if r.failAt > 0 && off+int64(len(p)) > r.failAt {
		return 0, errors.New("Verbindung weg")
	}
	if off >= int64(len(r.data)) {
		return 0, io.EOF
	}
	k := copy(p, r.data[off:])
	if r.short && k > 5 {
		k = 5
	}
	if r.read != nil {
		r.read.Add(int64(k))
	}
	if k < len(p) && !r.short {
		return k, io.EOF
	}
	return k, nil
}

func (r *atReader) Read([]byte) (int, error) {
	r.seqReads.Add(1)
	return 0, errors.New("sequentiell gelesen")
}

func (r *atReader) Close() error {
	r.closed.Store(true)
	return nil
}

// TestPipeCopyReadsConcurrently - aus einer Quelle mit ReadAt sind mehrere
// Blöcke gleichzeitig unterwegs (keine Pause nach jedem Block), höchstens
// copyReadsInFlight; der Inhalt bleibt in der richtigen Reihenfolge. Gilt
// für beide Wege (Ziel mit und ohne WriteAt).
func TestPipeCopyReadsConcurrently(t *testing.T) {
	const chunk = 16
	data := bigContent(40*chunk + 3)
	for _, at := range []bool{false, true} {
		src := &atReader{data: data, delay: 3 * time.Millisecond}
		var got []byte
		var err error
		if at {
			w := &atBuffer{}
			_, err = pipeCopyAt(context.Background(), w, src, newCopyBudget(32*chunk, chunk), nil)
			got = w.bytes()
		} else {
			var buf bytes.Buffer
			_, err = pipeCopy(context.Background(), &buf, src, newCopyBudget(32*chunk, chunk))
			got = buf.Bytes()
		}
		if err != nil || !bytes.Equal(got, data) {
			t.Errorf("WriteAt %v: err=%v, Inhalt gleich: %v", at, err, bytes.Equal(got, data))
		}
		if n := src.maxAct.Load(); n < 2 {
			t.Errorf("WriteAt %v: höchstens %d Block gleichzeitig gelesen, erwartet mehrere", at, n)
		}
		if n := src.maxAct.Load(); n > copyReadsInFlight {
			t.Errorf("WriteAt %v: %d Blöcke gleichzeitig gelesen, Grenze %d", at, n, copyReadsInFlight)
		}
		if src.seqReads.Load() != 0 {
			t.Errorf("WriteAt %v: sequentiell gelesen statt ReadAt", at)
		}
	}
}

// TestPipeCopyReaderAtContent - Inhalt vollständig und in Reihenfolge, an
// Blockgrenzen, bei leeren Dateien und bei einer Quelle, die weniger als
// verlangt liefert, ohne einen Fehler zu melden.
func TestPipeCopyReaderAtContent(t *testing.T) {
	const chunk = 8
	for _, short := range []bool{false, true} {
		for _, size := range []int{0, 1, chunk - 1, chunk, chunk + 1, 3*chunk + 5, 2 * copyReadsInFlight * chunk, 100} {
			data := bigContent(size)
			var buf bytes.Buffer
			n, err := pipeCopy(context.Background(), &buf, &atReader{data: data, short: short}, newCopyBudget(16*chunk, chunk))
			if err != nil || n != int64(size) || !bytes.Equal(buf.Bytes(), data) {
				t.Errorf("pipeCopy kurz %v, Größe %d: n=%d err=%v", short, size, n, err)
			}
			w := &atBuffer{}
			var reported atomic.Int64
			n, err = pipeCopyAt(context.Background(), w, &atReader{data: data, short: short}, newCopyBudget(16*chunk, chunk), func(k int) { reported.Add(int64(k)) })
			if err != nil || n != int64(size) || !bytes.Equal(w.bytes(), data) || reported.Load() != int64(size) {
				t.Errorf("pipeCopyAt kurz %v, Größe %d: n=%d err=%v gemeldet %d", short, size, n, err, reported.Load())
			}
		}
	}
}

// TestPipeCopyReaderAtErrors - Lesefehler mitten in der Datei, eine Quelle
// ohne Fortschritt, ein Schreibfehler und ein Abbruch kommen beim Aufrufer
// an. Danach läuft kein Lesevorgang mehr (die Quelle darf geschlossen
// werden - pkg/sftp verträgt ReadAt nicht gleichzeitig mit Close), und das
// Budget ist vollständig frei.
func TestPipeCopyReaderAtErrors(t *testing.T) {
	const chunk = 8
	data := bigContent(40 * chunk)
	b := newCopyBudget(16*chunk, chunk)
	check := func(name string, src *atReader, err error, want func(error) bool) {
		t.Helper()
		if !want(err) {
			t.Errorf("%s: Fehler %v", name, err)
		}
		if n := src.active.Load(); n != 0 {
			t.Errorf("%s: nach der Rückkehr laufen noch %d Lesevorgänge", name, n)
		}
		if got := b.free(); got != 16 {
			t.Errorf("%s: Budget danach %d Blöcke frei, erwartet 16 (Leck)", name, got)
		}
	}
	isLost := func(err error) bool { return err != nil && strings.Contains(err.Error(), "Verbindung weg") }

	for _, at := range []bool{false, true} {
		run := func(ctx context.Context, src *atReader, w *atBuffer) error {
			if at {
				_, err := pipeCopyAt(ctx, w, src, b, nil)
				return err
			}
			_, err := pipeCopy(ctx, &writerOnly{w: w}, src, b)
			return err
		}
		src := &atReader{data: data, failAt: 13 * chunk, delay: time.Millisecond}
		check("Lesefehler", src, run(context.Background(), src, &atBuffer{}), isLost)

		src = &atReader{data: data, stuck: true}
		check("ohne Fortschritt", src, run(context.Background(), src, &atBuffer{}), func(err error) bool { return errors.Is(err, io.ErrNoProgress) })

		src = &atReader{data: data, delay: 2 * time.Millisecond}
		check("Schreibfehler", src, run(context.Background(), src, &atBuffer{failAt: 5 * chunk}), func(err error) bool {
			return err != nil && strings.Contains(err.Error(), "voll")
		})

		ctx, cancel := context.WithCancel(context.Background())
		src = &atReader{data: data, delay: 2 * time.Millisecond}
		done := make(chan error, 1)
		go func() { done <- run(ctx, src, &atBuffer{delay: time.Millisecond}) }()
		time.Sleep(5 * time.Millisecond)
		cancel()
		select {
		case err := <-done:
			check("Abbruch", src, err, func(err error) bool { return errors.Is(err, context.Canceled) })
		case <-time.After(5 * time.Second):
			t.Fatal("hängt nach Abbruch")
		}
	}
}

// writerOnly versteckt WriteAt (für pipeCopy mit einem atBuffer).
type writerOnly struct{ w *atBuffer }

func (w *writerOnly) Write(p []byte) (int, error) {
	return w.w.WriteAt(p, w.w.written.Load())
}

// budgetWatcher ist ein Ziel (mit und ohne WriteAt), das bei jedem
// Schreiben festhält, wie viele Marken des Budgets höchstens belegt waren.
type budgetWatcher struct {
	w       *atBuffer
	budget  *copyBudget
	total   int
	maxUsed atomic.Int64
}

func (b *budgetWatcher) note() {
	raiseMax(&b.maxUsed, int64(b.total-b.budget.free()))
}

func (b *budgetWatcher) WriteAt(p []byte, off int64) (int, error) {
	b.note()
	return b.w.WriteAt(p, off)
}

// budgetWriter ist budgetWatcher ohne WriteAt (für pipeCopy).
type budgetWriter struct{ b *budgetWatcher }

func (w budgetWriter) Write(p []byte) (int, error) {
	w.b.note()
	return w.b.w.WriteAt(p, w.b.w.written.Load())
}

// TestPipeCopyReaderAtPerFileLimit - auch mit gleichzeitigen Lese- und
// Schreibvorgängen belegt eine Datei höchstens copyPipeChunks Marken des
// gemeinsamen Budgets (große Dateien bremsen kleine daneben nicht aus).
// Gezählt werden die Marken selbst, auch die von Lesevorgängen hinter dem
// Dateiende.
func TestPipeCopyReaderAtPerFileLimit(t *testing.T) {
	const chunk = 16
	data := bigContent(60 * chunk)
	for _, at := range []bool{false, true} {
		b := newCopyBudget(100*chunk, chunk)
		watch := &budgetWatcher{w: &atBuffer{delay: time.Millisecond}, budget: b, total: 100}
		src := &atReader{data: data, delay: time.Millisecond}
		var err error
		if at {
			_, err = pipeCopyAt(context.Background(), watch, src, b, nil)
		} else {
			_, err = pipeCopy(context.Background(), budgetWriter{watch}, src, b)
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := watch.maxUsed.Load(); got > copyPipeChunks {
			t.Errorf("WriteAt %v: %d Marken belegt, Grenze %d", at, got, copyPipeChunks)
		}
		if !bytes.Equal(watch.w.bytes(), data) {
			t.Errorf("WriteAt %v: Inhalt verfälscht", at)
		}
	}
}

// TestPipeCopySmallFileReadsOnce - Code-Review Build 82: eine Datei, die
// in einen Block passt, kostet genau einen Lesevorgang und eine Marke -
// sonst belegen Lesevorgänge hinter dem Dateiende das gemeinsame Budget,
// und viele kleine Dateien werden langsamer statt schneller. Erst wenn der
// erste Block voll zurückkommt, lesen mehrere gleichzeitig.
func TestPipeCopySmallFileReadsOnce(t *testing.T) {
	const chunk = 64
	for _, size := range []int{0, 1, 10, chunk - 1} {
		for _, at := range []bool{false, true} {
			b := newCopyBudget(32*chunk, chunk)
			watch := &budgetWatcher{w: &atBuffer{}, budget: b, total: 32}
			src := &atReader{data: bigContent(size), delay: time.Millisecond}
			var err error
			if at {
				_, err = pipeCopyAt(context.Background(), watch, src, b, nil)
			} else {
				_, err = pipeCopy(context.Background(), budgetWriter{watch}, src, b)
			}
			if err != nil {
				t.Fatal(err)
			}
			if n := src.calls.Load(); n != 1 {
				t.Errorf("Größe %d, WriteAt %v: %d Lesevorgänge, erwartet 1", size, at, n)
			}
			if got := watch.maxUsed.Load(); got > 1 {
				t.Errorf("Größe %d, WriteAt %v: %d Marken belegt, erwartet 1", size, at, got)
			}
		}
	}
}

// readAtFS ist ein memFS, dessen Dateien sich wie *sftp.File an
// beliebiger Stelle lesen lassen (mit Wartezeit pro Block).
type readAtFS struct {
	*memFS
	delay   time.Duration
	readers []*atReader
}

func (f *readAtFS) OpenRead(p string) (io.ReadCloser, error) {
	rc, err := f.memFS.OpenRead(p)
	if err != nil {
		return nil, err
	}
	data, _ := io.ReadAll(rc)
	r := &atReader{data: data, delay: f.delay}
	f.memFS.mu.Lock()
	f.readers = append(f.readers, r)
	f.memFS.mu.Unlock()
	return r, nil
}

// TestCopyLargeDownloadReadsConcurrently - eine große Datei vom Server
// wird mit mehreren gleichzeitigen ReadAt gelesen; Inhalt, Rechte und
// Zeit stimmen, und die Quelle wird erst geschlossen, wenn kein
// Lesevorgang mehr läuft.
func TestCopyLargeDownloadReadsConcurrently(t *testing.T) {
	data := bigContent(5*copyChunkSize + 77)
	src := &readAtFS{memFS: newMemFS("/home/a"), delay: 5 * time.Millisecond}
	src.addFile("/home/a/gross.bin", string(data), 0o640, copyTestTime)
	dst := newMemFS("/srv/b")
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"gross.bin"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 || res.files != 1 || res.bytes != int64(len(data)) {
		t.Fatalf("Fehler %v, Dateien %d, Bytes %d", res.failures, res.files, res.bytes)
	}
	if got, _ := dst.content("/srv/b/gross.bin"); got != string(data) {
		t.Error("Inhalt verfälscht")
	}
	if len(src.readers) != 1 {
		t.Fatalf("%d Mal geöffnet", len(src.readers))
	}
	r := src.readers[0]
	if r.maxAct.Load() < 2 {
		t.Errorf("höchstens %d ReadAt gleichzeitig, erwartet mehrere", r.maxAct.Load())
	}
	if !r.closed.Load() || r.afterClose.Load() != 0 {
		t.Errorf("geschlossen %v, Lesevorgänge nach Close %d", r.closed.Load(), r.afterClose.Load())
	}
	node := dst.node("/srv/b/gross.bin")
	if node.mode.Perm() != 0o640 || !node.modTime.Equal(copyTestTime) {
		t.Errorf("Rechte %v, Zeit %v", node.mode.Perm(), node.modTime)
	}
	assertNoTempFiles(t, dst)
}

// TestCopyReadErrorMidDownload - reißt das Lesen mitten in einer großen
// Datei ab, gibt es genau einen Fehler, kein Ziel und keine Temp-Datei.
func TestCopyReadErrorMidDownload(t *testing.T) {
	data := bigContent(6 * copyChunkSize)
	src := &failingReadAtFS{readAtFS: readAtFS{memFS: newMemFS("/home/a"), delay: time.Millisecond}, failAt: 3*copyChunkSize + 5}
	src.addFile("/home/a/gross.bin", string(data), 0o644, copyTestTime)
	dst := newMemFS("/srv/b")
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"gross.bin"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 1 || res.files != 0 {
		t.Fatalf("Fehler %v, Dateien %d", res.failures, res.files)
	}
	if _, ok := dst.content("/srv/b/gross.bin"); ok {
		t.Error("unvollständiges Ziel abgelegt")
	}
	assertNoTempFiles(t, dst)
}

// failingReadAtFS lässt das Lesen ab failAt scheitern.
type failingReadAtFS struct {
	readAtFS
	failAt int64
}

func (f *failingReadAtFS) OpenRead(p string) (io.ReadCloser, error) {
	rc, err := f.readAtFS.OpenRead(p)
	if err == nil {
		rc.(*atReader).failAt = f.failAt
	}
	return rc, err
}
