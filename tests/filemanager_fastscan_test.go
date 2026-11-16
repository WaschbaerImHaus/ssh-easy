// Paket main - Tests für den Schnellweg beim Erfassen (find über Exec-Kanal, Build 90)
//
// Prüft filemanager_fastscan.go, sftpFS.ListTree (nur über den Fake, der
// treeLister implementiert) und die Anbindung in scan (fastScan, sonst prefetch):
//   - Erfolg: Ergebnis von runCopy ist gleich dem SFTP-Weg (Zielinhalt,
//     Mkdir-Aufrufe, Fehlerliste, Summen); SFTP liest keine Verzeichnisse mehr
//   - Auswertung (parseTreeOutput/parseTreeRecord) als reine Funktionen:
//     jedes ungültige Feld, Grenzen, abgeschnittene Ausgabe
//   - stiller Rückfall auf SFTP bei jedem Zweifel (Startfehler, Exit-Code,
//     stderr, Müll, leere Ausgabe, Wurzel fehlt, anderes Dateisystem, falsche
//     Reihenfolge, doppelte Pfade, Byte-Grenze, Leerlauf)
//   - unsichere Namen aus find: gemeldet, ihr Inhalt wird nie kopiert
//   - Grenzen (Einträge, Tiefe), Abbruch, kein Schnellweg für den lokalen PC
//   - fester Befehlstext ohne Nutzerdaten, unter allen Login-Shells gleich
//
// Der Fake (fastFS) ist ein memFS, das treeLister implementiert und als Ausgabe
// entweder eine aus dem Baum erzeugte, korrekte find-Ausgabe oder manipulierte
// Bytes liefert. Wiederverwendet: memFS, fakeFileInfo, copyTestTime,
// newLocalTestFS, loginShellPath, loginShellNames.
//
// @author Kurt Ingwer
// @date   2026-11-16 12:22
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Fake: Quelle mit ListTree ---

// fastRecord ist eine Zeile der find-Ausgabe (sechs Felder).
type fastRecord struct {
	typ, perm, size, mtime, start, rel string
}

// fastOctal liefert %m wie find: Rechte oktal samt setuid/setgid/sticky.
func fastOctal(mode fs.FileMode) string {
	v := uint32(mode.Perm())
	if mode&fs.ModeSetuid != 0 {
		v |= 0o4000
	}
	if mode&fs.ModeSetgid != 0 {
		v |= 0o2000
	}
	if mode&fs.ModeSticky != 0 {
		v |= 0o1000
	}
	return fmt.Sprintf("%o", v)
}

// fastTypeChar liefert %y wie find.
func fastTypeChar(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "d"
	case mode&fs.ModeSymlink != 0:
		return "l"
	}
	return "f"
}

// fastGenRecords erzeugt die korrekte find-Ausgabe (-P, -mindepth 0,
// -maxdepth maxDepth) aus dem Baum. Die Geschwister kommen absichtlich in
// umgekehrter Namensreihenfolge (find sortiert nie): der Schnellweg muss
// selbst sortieren, damit das Ergebnis dem SFTP-Weg gleicht.
func fastGenRecords(m *memFS, starts []string, maxDepth int) []fastRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []fastRecord
	emit := func(start, rel, abs string, n *memNode) {
		size := int64(len(n.data))
		if n.mode&fs.ModeSymlink != 0 {
			size = int64(len(n.target))
		}
		out = append(out, fastRecord{
			typ:   fastTypeChar(n.mode),
			perm:  fastOctal(n.mode),
			size:  fmt.Sprint(size),
			mtime: fmt.Sprintf("%d.%010d", n.modTime.Unix(), n.modTime.Nanosecond()*10),
			start: start,
			rel:   rel,
		})
	}
	var walk func(start, rel, abs string, depth int)
	walk = func(start, rel, abs string, depth int) {
		if depth >= maxDepth {
			return
		}
		var names []string
		for p := range m.nodes {
			if p != "/" && path.Dir(p) == abs {
				names = append(names, path.Base(p))
			}
		}
		sort.Sort(sort.Reverse(sort.StringSlice(names)))
		for _, name := range names {
			childAbs := path.Join(abs, name)
			childRel := name
			if rel != "" {
				childRel = rel + "/" + name
			}
			n := m.nodes[childAbs]
			emit(start, childRel, childAbs, n)
			if n.mode.IsDir() {
				walk(start, childRel, childAbs, depth+1)
			}
		}
	}
	for _, start := range starts {
		n, ok := m.nodes[start]
		if !ok {
			continue
		}
		emit(start, "", start, n)
		if n.mode.IsDir() {
			walk(start, "", start, 0)
		}
	}
	return out
}

// fastSerialize schreibt Zeilen im Format von -printf "%y\0%m\0%s\0%T@\0%H\0%P\0".
func fastSerialize(recs []fastRecord) []byte {
	var buf bytes.Buffer
	for _, r := range recs {
		for _, f := range []string{r.typ, r.perm, r.size, r.mtime, r.start, r.rel} {
			buf.WriteString(f)
			buf.WriteByte(0)
		}
	}
	return buf.Bytes()
}

// fastFS ist ein memFS, das treeLister implementiert. Es zählt ReadDir-Aufrufe
// (so sieht man, welcher Weg lief) und merkt Aufrufe von ListTree, wait, Close.
type fastFS struct {
	*memFS
	mu sync.Mutex
	// ReadDir-Aufrufe je Pfad
	readDirs map[string]int
	// ListTree: Aufrufe, Startpfade, Tiefe, wait-Aufrufe, geschlossene Leser
	listCalls  int
	lastStarts []string
	lastDepth  int
	waits      int
	closes     int
	// Einstellungen für ListTree
	startErr error // ListTree scheitert sofort (Exec verboten)
	waitErr  error // wait meldet diesen Fehler
	raw      []byte
	rawSet   bool                                 // statt der erzeugten Ausgabe diese Bytes liefern
	mutate   func(recs []fastRecord) []fastRecord // erzeugte Zeilen verändern
	cut      int                                  // so viele Bytes am Ende der Ausgabe abschneiden
	delay    time.Duration                        // erste Daten erst nach dieser Zeit
	onList   func()                               // beim Start von ListTree aufrufen
	forceDep int                                  // find liefert bis zu dieser Tiefe, egal was verlangt wurde
	// RealPath ersetzen (nil = memFS)
	realPath func(p string) (string, error)
}

// newFastFS legt eine Quelle mit Home /home/a an.
func newFastFS() *fastFS {
	return &fastFS{memFS: newMemFS("/home/a"), readDirs: map[string]int{}}
}

func (f *fastFS) ReadDir(p string) ([]os.FileInfo, error) {
	f.mu.Lock()
	f.readDirs[p]++
	f.mu.Unlock()
	return f.memFS.ReadDir(p)
}

func (f *fastFS) RealPath(p string) (string, error) {
	if f.realPath != nil {
		return f.realPath(p)
	}
	return f.memFS.RealPath(p)
}

// readDirCount liefert, wie oft ReadDir für p aufgerufen wurde.
func (f *fastFS) readDirCount(p string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readDirs[p]
}

// readDirPaths liefert alle gelesenen Verzeichnisse (sortiert).
func (f *fastFS) readDirPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for p := range f.readDirs {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// counters liefert ListTree-Aufrufe, wait-Aufrufe und geschlossene Leser.
func (f *fastFS) counters() (list, waits, closes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listCalls, f.waits, f.closes
}

func (f *fastFS) ListTree(ctx context.Context, starts []string, maxDepth int) (io.ReadCloser, func() error, error) {
	f.mu.Lock()
	f.listCalls++
	f.lastStarts = append([]string(nil), starts...)
	f.lastDepth = maxDepth
	f.mu.Unlock()
	if f.onList != nil {
		f.onList()
	}
	if f.startErr != nil {
		return nil, nil, f.startErr
	}
	var data []byte
	if f.rawSet {
		data = f.raw
	} else {
		depth := maxDepth
		if f.forceDep > 0 {
			depth = f.forceDep
		}
		recs := fastGenRecords(f.memFS, starts, depth)
		if f.mutate != nil {
			recs = f.mutate(recs)
		}
		data = fastSerialize(recs)
	}
	if f.cut > 0 && len(data) >= f.cut {
		data = data[:len(data)-f.cut]
	}
	rd := &fastReader{ctx: ctx, fs: f, data: bytes.NewReader(data), delay: f.delay, done: make(chan struct{})}
	wait := func() error {
		f.mu.Lock()
		f.waits++
		f.mu.Unlock()
		return f.waitErr
	}
	return rd, wait, nil
}

// fastReader ist die Standardausgabe des Fakes. Wie ein geschlossener
// Exec-Kanal endet Read mit Fehler, sobald ctx endet oder Close gerufen wurde.
type fastReader struct {
	ctx     context.Context
	fs      *fastFS
	data    *bytes.Reader
	delay   time.Duration
	started bool
	done    chan struct{}
	once    sync.Once
}

func (r *fastReader) Read(b []byte) (int, error) {
	if !r.started {
		r.started = true
		if r.delay > 0 {
			select {
			case <-time.After(r.delay):
			case <-r.ctx.Done():
				return 0, io.ErrClosedPipe
			case <-r.done:
				return 0, io.ErrClosedPipe
			}
		}
	}
	select {
	case <-r.ctx.Done():
		return 0, io.ErrClosedPipe
	case <-r.done:
		return 0, io.ErrClosedPipe
	default:
	}
	return r.data.Read(b)
}

func (r *fastReader) Close() error {
	r.once.Do(func() {
		close(r.done)
		r.fs.mu.Lock()
		r.fs.closes++
		r.fs.mu.Unlock()
	})
	return nil
}

// --- Fake: Ziel ---

// fastDst ist ein memFS-Ziel, das die Reihenfolge der Mkdir-Aufrufe merkt.
type fastDst struct {
	*memFS
	mu    sync.Mutex
	order []string
}

func (d *fastDst) Mkdir(p string) error {
	err := d.memFS.Mkdir(p)
	if err == nil {
		d.mu.Lock()
		d.order = append(d.order, p)
		d.mu.Unlock()
	}
	return err
}

func (d *fastDst) mem() *memFS { return d.memFS }

func (d *fastDst) mkdirs() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.order...)
}

// fastStrictDst lehnt Namen wie das lokale Windows ab (nameValidator):
// Gerätenamen und Namen mit ":".
type fastStrictDst struct{ *fastDst }

func (d fastStrictDst) ValidName(name string) bool {
	return name != "CON" && name != "NUL" && !strings.Contains(name, ":")
}

// fastDstFS ist ein Ziel, dessen Zustand der Läufer auswerten kann.
type fastDstFS interface {
	remoteFS
	mem() *memFS
	mkdirs() []string
}

// --- Quellbaum ---

// fastSource liefert den Standard-Quellbaum:
//
//	/home/a/projekt            (0750)
//	  main.go run.sh aktuell->main.go leer/ lib/x.go lib/deep/y.go
//	/home/a/zwei/z.txt
//	/home/a/notiz.txt  /home/a/lnk -> projekt
func fastSource() *fastFS {
	src := newFastFS()
	src.addDir("/home/a/projekt", 0o750)
	src.nodes["/home/a/projekt"].modTime = copyTestTime
	// Bruchteil der Sekunde: muss bis auf die Nanosekunde ankommen
	src.addFile("/home/a/projekt/main.go", "package main", 0o644, copyTestTime.Add(123456789*time.Nanosecond))
	src.addFile("/home/a/projekt/run.sh", "#!/bin/sh", 0o755, copyTestTime)
	src.addLink("/home/a/projekt/aktuell", "main.go")
	src.addDir("/home/a/projekt/leer", 0o700)
	src.addFile("/home/a/projekt/lib/x.go", "x", 0o644, copyTestTime)
	src.addFile("/home/a/projekt/lib/deep/y.go", "yy", 0o600, copyTestTime)
	src.addFile("/home/a/zwei/z.txt", "zzz", 0o644, copyTestTime)
	src.addFile("/home/a/notiz.txt", "hallo", 0o600, copyTestTime)
	src.addLink("/home/a/lnk", "projekt")
	return src
}

// fastSourceUnsafe ergänzt unsichere Namen (Backslash, Steuerzeichen, ein
// Verzeichnis mit unsicherem Namen samt Inhalt) und Namen, die ein strenges
// Ziel ablehnt (Gerätename CON mit Inhalt, "a:b").
func fastSourceUnsafe() *fastFS {
	src := fastSource()
	src.addFile("/home/a/projekt/x\\y", "boese", 0o644, copyTestTime)
	src.addFile("/home/a/projekt/ctl\x01z", "boese", 0o644, copyTestTime)
	src.addFile("/home/a/projekt/lib/nl\nx", "boese", 0o644, copyTestTime)
	src.addFile("/home/a/projekt/bad\\dir/inner.txt", "inhalt", 0o644, copyTestTime)
	src.addFile("/home/a/projekt/bad\\dir/sub/deep.txt", "inhalt", 0o644, copyTestTime)
	src.addFile("/home/a/projekt/CON/inner.txt", "inhalt", 0o644, copyTestTime)
	src.addFile("/home/a/projekt/lib/a:b", "x", 0o644, copyTestTime)
	return src
}

// --- Läufer und Ergebnis ---

// fastKeepVars merkt die Paketvariablen des Schnellwegs, der Stichprobe und
// der Grenzen; der Rückgabewert stellt sie wieder her (defer fastKeepVars()()).
func fastKeepVars() func() {
	enabled, maxBytes, timeout, idle, maxStarts := fastScanEnabled, fastScanMaxBytes, fastScanTimeout, fastScanIdleTimeout, fastScanMaxStarts
	entries, depth, parallel := copyMaxEntries, copyMaxDepth, copyParallel
	sampleEntries, sampleDeep, sampleDirs, sampleChildren, abortGrace := fastScanSampleEntries, fastScanSampleDeep, fastScanSampleDirs, fastScanSampleMaxChildren, fastScanAbortGrace
	return func() {
		fastScanEnabled, fastScanMaxBytes, fastScanTimeout, fastScanIdleTimeout, fastScanMaxStarts = enabled, maxBytes, timeout, idle, maxStarts
		copyMaxEntries, copyMaxDepth, copyParallel = entries, depth, parallel
		fastScanSampleEntries, fastScanSampleDeep, fastScanSampleDirs, fastScanSampleMaxChildren, fastScanAbortGrace = sampleEntries, sampleDeep, sampleDirs, sampleChildren, abortGrace
	}
}

// fastOutcome ist alles, was ein Kopierlauf beobachtbar hinterlässt.
type fastOutcome struct {
	snap      map[string]string
	mkdirs    []string
	failures  []string
	failErrs  []error
	copied    []string
	files     int
	bytes     int64
	cancelled bool
	logs      []string
	// Summen der letzten Fortschrittsmeldung nach dem Erfassen
	totalFiles, totalDirs int
	totalBytes            int64
	haveTotals            bool
}

// fastSnapshot beschreibt den Zielbaum unter /srv/b (Art, Rechte, Zeit,
// Inhalt bzw. Linkziel) und meldet liegen gebliebene Temp-Dateien.
func fastSnapshot(t *testing.T, m *memFS) map[string]string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for p, n := range m.nodes {
		if !strings.HasPrefix(p, "/srv/b/") {
			continue
		}
		if strings.Contains(path.Base(p), copyTempPrefix) {
			t.Errorf("Temp-Datei liegen geblieben: %s", p)
		}
		switch {
		case n.mode.IsDir():
			out[p] = fmt.Sprintf("dir %v %d", n.mode, n.modTime.UnixNano())
		case n.mode&fs.ModeSymlink != 0:
			out[p] = "link " + n.target
		default:
			out[p] = fmt.Sprintf("file %v %d %d %q", n.mode, len(n.data), n.modTime.UnixNano(), n.data)
		}
	}
	return out
}

// fastRun führt runCopy von /home/a nach /srv/b aus und sammelt Ergebnis,
// Fortschritt und Protokoll. strict wählt ein Ziel mit nameValidator.
func fastRun(t *testing.T, ctx context.Context, src remoteFS, strict bool, names []string) fastOutcome {
	t.Helper()
	return fastRunReq(t, ctx, src, strict, names, false, 15*time.Second)
}

// fastRunReq ist fastRun mit Wahl von Verschieben (move) und der längsten
// Wartezeit auf das Ergebnis (limit; danach t.Fatal). Das Protokoll wird unter
// einer Sperre gesammelt und unter ihr kopiert: nach Esc kann runTreeList noch
// nach dem Ende von runCopy protokollieren (Hintergrund-Trennen), ohne dass
// der Test dabei ein Datenrennen sieht.
func fastRunReq(t *testing.T, ctx context.Context, src remoteFS, strict bool, names []string, move bool, limit time.Duration) fastOutcome {
	t.Helper()
	var dst fastDstFS
	base := &fastDst{memFS: newMemFS("/srv/b")}
	dst = base
	if strict {
		dst = fastStrictDst{base}
	}
	var out fastOutcome
	var logMu sync.Mutex
	var logs []string
	req := copyRequest{
		src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: names, move: move,
		logf: func(format string, args ...interface{}) {
			logMu.Lock()
			logs = append(logs, fmt.Sprintf(format, args...))
			logMu.Unlock()
		},
	}
	events := make(chan copyEvent, 256)
	answers := make(chan copyConflictAnswer, 1)
	go runCopy(ctx, req, events, answers)
	timeout := time.After(limit)
	var res *copyResult
	for res == nil {
		select {
		case ev := <-events:
			switch {
			case ev.result != nil:
				res = ev.result
			case ev.conflict != "":
				t.Errorf("unerwartete Rückfrage für %s", ev.conflict)
				answers <- copyAnswerAbort
			case ev.progress != nil && !ev.progress.scanning:
				out.totalFiles, out.totalDirs, out.totalBytes, out.haveTotals = ev.progress.totalFiles, ev.progress.totalDirs, ev.progress.totalBytes, true
			}
		case <-timeout:
			t.Fatal("runCopy hängt")
		}
	}
	out.snap = fastSnapshot(t, dst.mem())
	out.mkdirs = dst.mkdirs()
	for _, f := range res.failures {
		out.failures = append(out.failures, f.path+" | "+f.err.Error())
		out.failErrs = append(out.failErrs, f.err)
	}
	out.copied, out.files, out.bytes, out.cancelled = res.copiedNames, res.files, res.bytes, res.cancelled
	logMu.Lock()
	out.logs = append([]string(nil), logs...)
	logMu.Unlock()
	return out
}

// fastScanLog liefert die Protokollzeile zum Erfassen ("" = keine).
func fastScanLog(o fastOutcome) string {
	for _, l := range o.logs {
		if strings.HasPrefix(l, "Datei-Manager: Erfassen über ") {
			return l
		}
	}
	return ""
}

// fastAssertSame vergleicht zwei Läufe. Die Mkdir-Aufrufe vergleicht es als
// Menge: Verzeichnisse legt runCopy in Hintergrund-Aufträgen an, die
// Reihenfolge unter Geschwistern ist auch beim SFTP-Weg (und mit
// copyParallel = 1) nicht festgelegt. Festgelegt ist nur: Elternverzeichnis
// vor Kind (fastAssertParentsFirst).
func fastAssertSame(t *testing.T, label string, got, want fastOutcome) {
	t.Helper()
	if !reflect.DeepEqual(got.snap, want.snap) {
		t.Errorf("%s: Zielbaum weicht ab\n erhalten: %v\n erwartet: %v", label, got.snap, want.snap)
	}
	gm, wm := append([]string(nil), got.mkdirs...), append([]string(nil), want.mkdirs...)
	sort.Strings(gm)
	sort.Strings(wm)
	if !reflect.DeepEqual(gm, wm) {
		t.Errorf("%s: Mkdir-Aufrufe weichen ab\n erhalten: %q\n erwartet: %q", label, gm, wm)
	}
	fastAssertParentsFirst(t, label, got.mkdirs)
	if !reflect.DeepEqual(got.failures, want.failures) {
		t.Errorf("%s: Fehlerliste weicht ab\n erhalten: %q\n erwartet: %q", label, got.failures, want.failures)
	}
	if !reflect.DeepEqual(got.copied, want.copied) || got.files != want.files || got.bytes != want.bytes || got.cancelled != want.cancelled {
		t.Errorf("%s: Ergebnis weicht ab: copied %v/%v, files %d/%d, bytes %d/%d, cancelled %v/%v",
			label, got.copied, want.copied, got.files, want.files, got.bytes, want.bytes, got.cancelled, want.cancelled)
	}
	if got.totalFiles != want.totalFiles || got.totalDirs != want.totalDirs || got.totalBytes != want.totalBytes || got.haveTotals != want.haveTotals {
		t.Errorf("%s: Summen weichen ab: Dateien %d/%d, Verzeichnisse %d/%d, Bytes %d/%d",
			label, got.totalFiles, want.totalFiles, got.totalDirs, want.totalDirs, got.totalBytes, want.totalBytes)
	}
}

// fastAssertParentsFirst prüft, dass jedes angelegte Verzeichnis sein
// Elternverzeichnis schon vorfand (Zielordner /srv/b gab es vorher).
func fastAssertParentsFirst(t *testing.T, label string, order []string) {
	t.Helper()
	created := map[string]bool{"/srv/b": true}
	for _, p := range order {
		if !created[path.Dir(p)] {
			t.Errorf("%s: %s angelegt, bevor %s da war", label, p, path.Dir(p))
		}
		created[p] = true
	}
}

// fastAssertLimit prüft das Verhalten an einer Grenze (Einträge, Tiefe): wie
// beim SFTP-Weg genau ein errCopyLimit, nichts kopiert, gleiches Ergebnis.
// Der Pfad in der Meldung und die Summen beim Erfassen dürfen abweichen
// (der Schnellweg nennt das oberste Verzeichnis).
func fastAssertLimit(t *testing.T, label string, got, want fastOutcome) {
	t.Helper()
	if len(got.failures) != len(want.failures) {
		t.Errorf("%s: Fehler %q, SFTP-Weg: %q", label, got.failures, want.failures)
	}
	for i, e := range got.failErrs {
		if i < len(want.failErrs) && errors.Is(e, errCopyLimit) != errors.Is(want.failErrs[i], errCopyLimit) {
			t.Errorf("%s: Fehler %d: %v, SFTP-Weg: %v", label, i, e, want.failErrs[i])
		}
	}
	if !reflect.DeepEqual(got.snap, want.snap) || !reflect.DeepEqual(got.copied, want.copied) || got.files != want.files || got.bytes != want.bytes {
		t.Errorf("%s: Ergebnis weicht ab: Ziel %v/%v, copied %v/%v, files %d/%d, bytes %d/%d",
			label, got.snap, want.snap, got.copied, want.copied, got.files, want.files, got.bytes, want.bytes)
	}
}

// fastCompare führt denselben Auftrag erst über den SFTP-Weg (Schnellweg aus,
// Referenz) und dann mit Schnellweg aus. tune verändert vorher die Quelle des
// zweiten Laufs. Liefert beide Ergebnisse und die Quelle des zweiten Laufs.
func fastCompare(t *testing.T, build func() *fastFS, names []string, strict bool, tune func(f *fastFS)) (got, ref fastOutcome, src *fastFS) {
	t.Helper()
	fastScanEnabled = false
	ref = fastRun(t, context.Background(), build(), strict, names)
	fastScanEnabled = true
	src = build()
	if tune != nil {
		tune(src)
	}
	got = fastRun(t, context.Background(), src, strict, names)
	return got, ref, src
}

// fastCompareMove ist fastCompare für VERSCHIEBEN (F6): Die Quelle wird beim
// Lauf verändert, deshalb baut es sie je Lauf neu. Liefert zusätzlich die
// Quelle des zweiten Laufs (nach dem Verschieben).
func fastCompareMove(t *testing.T, build func() *fastFS, names []string, tune func(f *fastFS)) (got, ref fastOutcome, src *fastFS) {
	t.Helper()
	fastScanEnabled = false
	ref = fastRunReq(t, context.Background(), build(), false, names, true, 15*time.Second)
	fastScanEnabled = true
	src = build()
	if tune != nil {
		tune(src)
	}
	got = fastRunReq(t, context.Background(), src, false, names, true, 15*time.Second)
	return got, ref, src
}

// fastWantPath prüft per Protokoll, welcher Weg lief: "find" oder "SFTP".
func fastWantPath(t *testing.T, label string, o fastOutcome, want string) {
	t.Helper()
	l := fastScanLog(o)
	if l == "" {
		t.Errorf("%s: keine Protokollzeile zum Erfassen (logs: %q)", label, o.logs)
		return
	}
	if !strings.HasPrefix(l, "Datei-Manager: Erfassen über "+want) {
		t.Errorf("%s: Protokoll %q, erwartet Weg %q", label, l, want)
	}
}

// --- 1. Erfolg ---

// TestFastScanSameResultAsSFTP - Kern: Mit Schnellweg entsteht am Ziel
// dasselbe wie über den SFTP-Weg (Inhalt, Rechte, Zeiten bis zur Nanosekunde,
// Mkdir-Reihenfolge bei einem Arbeiter, Fehlerliste, Summen). Die Geschwister
// kommen von find in umgekehrter Reihenfolge, müssen also sortiert werden.
func TestFastScanSameResultAsSFTP(t *testing.T) {
	defer fastKeepVars()()
	got, ref, src := fastCompare(t, fastSource, []string{"projekt"}, false, nil)
	fastAssertSame(t, "Schnellweg", got, ref)
	fastWantPath(t, "Schnellweg", got, "find")
	if len(ref.snap) == 0 || len(ref.failures) != 0 {
		t.Fatalf("Referenz unbrauchbar: %d Einträge, Fehler %v", len(ref.snap), ref.failures)
	}
	if c, ok := src.content("/home/a/projekt/lib/deep/y.go"); !ok || c != "yy" {
		t.Error("Quelle verändert")
	}
	if want := "file -rw-r--r-- 12 " + fmt.Sprint(copyTestTime.Add(123456789*time.Nanosecond).UnixNano()) + ` "package main"`; got.snap["/srv/b/projekt/main.go"] != want {
		t.Errorf("main.go am Ziel: %q, erwartet %q (Nanosekunden!)", got.snap["/srv/b/projekt/main.go"], want)
	}
	if got.snap["/srv/b/projekt/aktuell"] != "link main.go" {
		t.Errorf("Symlink am Ziel: %q", got.snap["/srv/b/projekt/aktuell"])
	}
	if _, ok := got.snap["/srv/b/projekt/leer"]; !ok {
		t.Error("leeres Verzeichnis nicht angelegt")
	}
}

// TestFastScanSameResultAsSFTPParallel - wie oben mit den Standard-Arbeitern:
// die Reihenfolge der Mkdir-Aufrufe unter Geschwistern ist dann offen, die
// Menge nicht. Dazu zwei Verzeichnisse und eine Datei gleichzeitig markiert.
func TestFastScanSameResultAsSFTPParallel(t *testing.T) {
	defer fastKeepVars()()
	for _, names := range [][]string{{"projekt"}, {"projekt", "zwei", "notiz.txt"}, {"zwei", "projekt"}} {
		got, ref, _ := fastCompare(t, fastSource, names, false, nil)
		fastAssertSame(t, fmt.Sprint(names), got, ref)
		fastWantPath(t, fmt.Sprint(names), got, "find")
	}
}

// TestFastScanReadsNoDirectoriesViaSFTP - Der Gewinn des Schnellwegs: SFTP
// liest KEIN markiertes Verzeichnis (bei einem Namen kein einziges ReadDir;
// bei mehreren nur srcDir für die Angaben der obersten Einträge). ListTree
// bekommt genau die kanonischen Startpfade der Verzeichnisse und die Tiefe
// copyMaxDepth; wait und Close laufen je einmal.
//
// Diese Zusagen gelten bei ABGESCHALTETER Stichprobe (Einträge und
// Verzeichnisse 0): Die Stichprobe (Build 91) liest bewusst bis zu
// fastScanSampleDirs Verzeichnisse per ReadDir. Mit Stichprobe gilt die
// Zusage "höchstens fastScanSampleDirs, Weg find" (zweiter Teil).
func TestFastScanReadsNoDirectoriesViaSFTP(t *testing.T) {
	defer fastKeepVars()()
	fastScanSampleEntries, fastScanSampleDirs = 0, 0
	_, _, src := fastCompare(t, fastSource, []string{"projekt"}, false, nil)
	if got := src.readDirPaths(); len(got) != 0 {
		t.Errorf("ReadDir-Pfade %q, erwartet keine", got)
	}
	if list, waits, closes := src.counters(); list != 1 || waits != 1 || closes != 1 {
		t.Errorf("ListTree %d, wait %d, Close %d - erwartet je 1", list, waits, closes)
	}
	if !reflect.DeepEqual(src.lastStarts, []string{"/home/a/projekt"}) || src.lastDepth != copyMaxDepth {
		t.Errorf("Startpfade %q, Tiefe %d", src.lastStarts, src.lastDepth)
	}

	// Zwei Verzeichnisse und eine Datei: Startpfade nur der Verzeichnisse, in
	// Reihenfolge der Markierung; ReadDir nur auf srcDir (sourceInfos)
	_, _, src = fastCompare(t, fastSource, []string{"zwei", "notiz.txt", "projekt"}, false, nil)
	if want := []string{"/home/a/zwei", "/home/a/projekt"}; !reflect.DeepEqual(src.lastStarts, want) {
		t.Errorf("Startpfade %q, erwartet %q", src.lastStarts, want)
	}
	if got, want := src.readDirPaths(), []string{"/home/a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ReadDir-Pfade %q, erwartet %q", got, want)
	}

	// Mit Stichprobe (Standardgrenzen): höchstens fastScanSampleDirs
	// ReadDir-Aufrufe unterhalb der markierten Verzeichnisse, Weg bleibt find
	fastScanSampleEntries, fastScanSampleDirs = 16, 8
	got, ref, src := fastCompare(t, fastSource, []string{"projekt"}, false, nil)
	fastWantPath(t, "mit Stichprobe", got, "find")
	fastAssertSame(t, "mit Stichprobe", got, ref)
	total := 0
	for _, p := range src.readDirPaths() {
		total += src.readDirCount(p)
		if !strings.HasPrefix(p, "/home/a/projekt") {
			t.Errorf("ReadDir außerhalb des markierten Verzeichnisses: %s", p)
		}
	}
	if total == 0 || total > fastScanSampleDirs {
		t.Errorf("ReadDir-Aufrufe der Stichprobe: %d, erwartet 1..%d", total, fastScanSampleDirs)
	}
	if list, waits, closes := src.counters(); list != 1 || waits != 1 || closes != 1 {
		t.Errorf("mit Stichprobe: ListTree %d, wait %d, Close %d - erwartet je 1", list, waits, closes)
	}
}

// TestFastScanProgressTotals - Summen nach dem Erfassen (Dateien inkl.
// Symlink, Verzeichnisse inkl. oberstem, Bytes nur regulärer Dateien) stimmen
// mit dem SFTP-Weg und mit dem Baum überein.
func TestFastScanProgressTotals(t *testing.T) {
	defer fastKeepVars()()
	got, ref, _ := fastCompare(t, fastSource, []string{"projekt"}, false, nil)
	// Dateien: aktuell, y.go, x.go, main.go, run.sh; Verzeichnisse: projekt,
	// leer, lib, deep; Bytes: 12 + 9 + 1 + 2
	for label, o := range map[string]fastOutcome{"find": got, "SFTP": ref} {
		if !o.haveTotals || o.totalFiles != 5 || o.totalDirs != 4 || o.totalBytes != 24 {
			t.Errorf("%s: Summen Dateien %d, Verzeichnisse %d, Bytes %d (erwartet 5/4/24)", label, o.totalFiles, o.totalDirs, o.totalBytes)
		}
	}
	if got.files != 5 || got.bytes != 24 {
		t.Errorf("Ergebnis: Dateien %d, Bytes %d", got.files, got.bytes)
	}
}

// TestFastScanRealPathCanonical - Startpfade kommen aus SFTP-RealPath von
// srcDir (mit path.Clean): ein RealPath mit "/" am Ende oder "//" ändert den
// Startpfad nicht.
func TestFastScanRealPathCanonical(t *testing.T) {
	defer fastKeepVars()()
	_, _, src := fastCompare(t, fastSource, []string{"projekt"}, false, func(f *fastFS) {
		f.realPath = func(p string) (string, error) { return "/home//a/", nil }
	})
	if !reflect.DeepEqual(src.lastStarts, []string{"/home/a/projekt"}) {
		t.Errorf("Startpfade %q", src.lastStarts)
	}
}

// --- 2. Auswertung als reine Funktionen ---

// fastValid ist eine gültige Zeile für die Tabellen.
func fastValid() (string, [5]string, map[string]bool) {
	return "f", [5]string{"755", "12", "1700000000.5", "/home/a", "x/y"}, map[string]bool{"/home/a": true}
}

// TestFastScanParseTreeRecordValid - gültige Zeilen: Name aus %P bzw. dem
// Startpfad, Typ, Rechte inkl. setuid/setgid/sticky, Größe, Zeit.
func TestFastScanParseTreeRecordValid(t *testing.T) {
	starts := map[string]bool{"/home/a": true, "/": true}
	cases := []struct {
		name  string
		typ   string
		f     [5]string
		mode  os.FileMode
		base  string
		isDir bool
	}{
		{"Datei", "f", [5]string{"644", "5", "1700000000.0", "/home/a", "x/y.txt"}, 0o644, "y.txt", false},
		{"Verzeichnis", "d", [5]string{"755", "4096", "1", "/home/a", "x"}, os.ModeDir | 0o755, "x", true},
		{"Startpfad selbst", "d", [5]string{"750", "0", "1", "/home/a", ""}, os.ModeDir | 0o750, "a", true},
		{"Wurzel als Start", "d", [5]string{"755", "0", "1", "/", ""}, os.ModeDir | 0o755, "/", true},
		{"Symlink", "l", [5]string{"777", "7", "1", "/home/a", "l"}, os.ModeSymlink | 0o777, "l", false},
		{"Pipe", "p", [5]string{"600", "0", "1", "/home/a", "p"}, os.ModeNamedPipe | 0o600, "p", false},
		{"Socket", "s", [5]string{"600", "0", "1", "/home/a", "s"}, os.ModeSocket | 0o600, "s", false},
		{"Zeichengerät", "c", [5]string{"600", "0", "1", "/home/a", "c"}, os.ModeDevice | os.ModeCharDevice | 0o600, "c", false},
		{"Blockgerät", "b", [5]string{"600", "0", "1", "/home/a", "b"}, os.ModeDevice | 0o600, "b", false},
		{"setuid", "f", [5]string{"4755", "1", "1", "/home/a", "su"}, os.ModeSetuid | 0o755, "su", false},
		{"setgid", "f", [5]string{"2755", "1", "1", "/home/a", "sg"}, os.ModeSetgid | 0o755, "sg", false},
		{"sticky", "d", [5]string{"1777", "1", "1", "/home/a", "tmp"}, os.ModeDir | os.ModeSticky | 0o777, "tmp", true},
		{"alle Sonderbits", "f", [5]string{"7777", "1", "1", "/home/a", "all"}, os.ModeSetuid | os.ModeSetgid | os.ModeSticky | 0o777, "all", false},
		{"Rechte 0", "f", [5]string{"0", "0", "0", "/home/a", "n"}, 0, "n", false},
		{"Name mit Zeilenumbruch", "f", [5]string{"644", "1", "1", "/home/a", "d/a\nb"}, 0o644, "a\nb", false},
		{"Name mit Leerzeichen und Anführungszeichen", "f", [5]string{"644", "1", "1", "/home/a", "it's \"x\""}, 0o644, "it's \"x\"", false},
	}
	for _, c := range cases {
		rec, ok := parseTreeRecord(c.typ, c.f, starts)
		if !ok {
			t.Errorf("%s: abgelehnt", c.name)
			continue
		}
		if rec.info.Mode() != c.mode || rec.info.Name() != c.base || rec.info.IsDir() != c.isDir {
			t.Errorf("%s: Rechte %v (erwartet %v), Name %q (erwartet %q), IsDir %v", c.name, rec.info.Mode(), c.mode, rec.info.Name(), c.base, rec.info.IsDir())
		}
		if rec.start != c.f[3] || rec.rel != c.f[4] {
			t.Errorf("%s: start %q rel %q", c.name, rec.start, rec.rel)
		}
	}
}

// TestFastScanParseTreeRecordInvalid - jedes ungültige Feld verwirft die
// Zeile (und damit den ganzen Schnellweg): Typzeichen, Rechte, Größe, Zeit,
// Startpfad, %P. Randfälle: Vorzeichen, Exponent, Unterstriche, Hex, zu viele
// Nachkommastellen, Pfadteile "", "." und "..".
func TestFastScanParseTreeRecordInvalid(t *testing.T) {
	cases := []struct {
		name string
		mut  func(typ *string, f *[5]string)
	}{
		{"Typ x", func(typ *string, f *[5]string) { *typ = "x" }},
		{"Typ leer", func(typ *string, f *[5]string) { *typ = "" }},
		{"Typ ff", func(typ *string, f *[5]string) { *typ = "ff" }},
		{"Typ groß", func(typ *string, f *[5]string) { *typ = "F" }},
		{"Typ Steuerzeichen", func(typ *string, f *[5]string) { *typ = "\x01" }},
		{"Rechte 8", func(typ *string, f *[5]string) { f[0] = "8" }},
		{"Rechte 77777", func(typ *string, f *[5]string) { f[0] = "77777" }},
		{"Rechte leer", func(typ *string, f *[5]string) { f[0] = "" }},
		{"Rechte -1", func(typ *string, f *[5]string) { f[0] = "-1" }},
		{"Rechte +7", func(typ *string, f *[5]string) { f[0] = "+7" }},
		{"Rechte Buchstabe", func(typ *string, f *[5]string) { f[0] = "7a" }},
		{"Rechte 0o7", func(typ *string, f *[5]string) { f[0] = "0o7" }},
		{"Größe -1", func(typ *string, f *[5]string) { f[1] = "-1" }},
		{"Größe +5", func(typ *string, f *[5]string) { f[1] = "+5" }},
		{"Größe 1e3", func(typ *string, f *[5]string) { f[1] = "1e3" }},
		{"Größe leer", func(typ *string, f *[5]string) { f[1] = "" }},
		{"Größe Unterstrich", func(typ *string, f *[5]string) { f[1] = "1_0" }},
		{"Größe hex", func(typ *string, f *[5]string) { f[1] = "0x10" }},
		{"Größe überläuft", func(typ *string, f *[5]string) { f[1] = "99999999999999999999" }},
		{"Größe mit Leerzeichen", func(typ *string, f *[5]string) { f[1] = " 5" }},
		{"Zeit abc", func(typ *string, f *[5]string) { f[2] = "abc" }},
		{"Zeit 11 Nachkommastellen", func(typ *string, f *[5]string) { f[2] = "1.12345678901" }},
		{"Zeit leer", func(typ *string, f *[5]string) { f[2] = "" }},
		{"Zeit .5", func(typ *string, f *[5]string) { f[2] = ".5" }},
		{"Zeit -", func(typ *string, f *[5]string) { f[2] = "-" }},
		{"Zeit +5", func(typ *string, f *[5]string) { f[2] = "+5" }},
		{"Zeit 1e3", func(typ *string, f *[5]string) { f[2] = "1e3" }},
		{"Zeit Bruchteil mit Buchstabe", func(typ *string, f *[5]string) { f[2] = "1.5a" }},
		{"Zeit zwei Punkte", func(typ *string, f *[5]string) { f[2] = "1.2.3" }},
		{"Zeit zu groß", func(typ *string, f *[5]string) { f[2] = "1099511627777" }},
		{"Zeit überläuft", func(typ *string, f *[5]string) { f[2] = "99999999999999999999.0" }},
		{"unbekannter Start", func(typ *string, f *[5]string) { f[3] = "/etc" }},
		{"Start leer", func(typ *string, f *[5]string) { f[3] = "" }},
		{"Start mit Endschrägstrich", func(typ *string, f *[5]string) { f[3] = "/home/a/" }},
		{"rel .", func(typ *string, f *[5]string) { f[4] = "." }},
		{"rel ..", func(typ *string, f *[5]string) { f[4] = ".." }},
		{"rel ../x", func(typ *string, f *[5]string) { f[4] = "../x" }},
		{"rel a/..", func(typ *string, f *[5]string) { f[4] = "a/.." }},
		{"rel a/../b", func(typ *string, f *[5]string) { f[4] = "a/../b" }},
		{"rel a/./b", func(typ *string, f *[5]string) { f[4] = "a/./b" }},
		{"rel /x", func(typ *string, f *[5]string) { f[4] = "/x" }},
		{"rel a//b", func(typ *string, f *[5]string) { f[4] = "a//b" }},
		{"rel a/", func(typ *string, f *[5]string) { f[4] = "a/" }},
		{"rel nur /", func(typ *string, f *[5]string) { f[4] = "/" }},
	}
	for _, c := range cases {
		typ, f, starts := fastValid()
		if _, ok := parseTreeRecord(typ, f, starts); !ok {
			t.Fatal("Grundzeile ungültig")
		}
		c.mut(&typ, &f)
		if _, ok := parseTreeRecord(typ, f, starts); ok {
			t.Errorf("%s: wurde akzeptiert", c.name)
		}
	}
}

// TestFastScanParseTreeTime - Zeit mit Bruchteil wird auf die Nanosekunde
// genau gelesen (bis 9 Stellen; die 10. Stelle von find wird abgeschnitten);
// negative Zeiten (vor 1970) behalten das Vorzeichen.
func TestFastScanParseTreeTime(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
	}{
		{"1700000000.1234567890", time.Unix(1700000000, 123456789)},
		{"1700000000.5", time.Unix(1700000000, 500000000)},
		{"1700000000", time.Unix(1700000000, 0)},
		{"0.0000000015", time.Unix(0, 1)},
		{"0.0", time.Unix(0, 0)},
		{"1700000000.000000001", time.Unix(1700000000, 1)},
		{"-5.25", time.Unix(-5, -250000000)},
		{"-62135596800.0000000000", time.Time{}},
	}
	for _, c := range cases {
		got, ok := parseTreeTime(c.in)
		if !ok {
			t.Errorf("%q abgelehnt", c.in)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("%q: %v (%d ns), erwartet %v", c.in, got, got.UnixNano(), c.want)
		}
	}
}

// TestFastScanParseTreePerm - Rechte oktal bis 4 Stellen; Sonderbits werden
// auf os.ModeSetuid/ModeSetgid/ModeSticky abgebildet, nicht in Perm gemischt.
func TestFastScanParseTreePerm(t *testing.T) {
	for in, want := range map[string]os.FileMode{
		"0": 0, "7": 7, "644": 0o644, "0644": 0o644, "4755": os.ModeSetuid | 0o755, "2755": os.ModeSetgid | 0o755,
		"1777": os.ModeSticky | 0o777, "6000": os.ModeSetuid | os.ModeSetgid, "7777": os.ModeSetuid | os.ModeSetgid | os.ModeSticky | 0o777,
	} {
		got, ok := parseTreePerm(in)
		if !ok || got != want {
			t.Errorf("%q: %v ok=%v, erwartet %v", in, got, ok, want)
		}
		if got.Perm()&^0o777 != 0 {
			t.Errorf("%q: Perm enthält Sonderbits", in)
		}
	}
	for _, in := range []string{"", "8", "9", "77777", "00000", "-1", "+1", "0x1", "1 ", " 1", "7\x00"} {
		if _, ok := parseTreePerm(in); ok {
			t.Errorf("%q wurde akzeptiert", in)
		}
	}
}

// fastRecs ist eine kleine gültige Ausgabe mit n Zeilen unter /home/a.
func fastRecs(n int) []fastRecord {
	recs := []fastRecord{{"d", "755", "0", "1.0000000000", "/home/a", ""}}
	for i := 1; i < n; i++ {
		recs = append(recs, fastRecord{"f", "644", "3", "1.0000000000", "/home/a", fmt.Sprintf("f%d", i)})
	}
	return recs
}

// TestFastScanParseTreeOutputValid - mehrere Zeilen kommen in Reihenfolge
// an; leere Ausgabe ergibt keine Zeilen und keinen Fehler (die Prüfung
// "Wurzel fehlt" macht buildTrees).
func TestFastScanParseTreeOutputValid(t *testing.T) {
	starts := map[string]bool{"/home/a": true}
	recs, err := parseTreeOutput(bytes.NewReader(fastSerialize(fastRecs(4))), starts, 100, 1<<20)
	if err != nil || len(recs) != 4 {
		t.Fatalf("%d Zeilen, Fehler %v", len(recs), err)
	}
	for i, want := range []string{"", "f1", "f2", "f3"} {
		if recs[i].rel != want {
			t.Errorf("Zeile %d: rel %q, erwartet %q", i, recs[i].rel, want)
		}
	}
	recs, err = parseTreeOutput(bytes.NewReader(nil), starts, 100, 1<<20)
	if err != nil || len(recs) != 0 {
		t.Errorf("leere Ausgabe: %d Zeilen, Fehler %v", len(recs), err)
	}
}

// TestFastScanParseTreeOutputTruncated - eine abgeschnittene letzte Zeile
// (Verbindung riss mitten im Feld, Feld ohne NUL, zu wenige Felder, nur ein
// NUL) ist ungültig und wird nie als vollständige Zeile gewertet.
func TestFastScanParseTreeOutputTruncated(t *testing.T) {
	starts := map[string]bool{"/home/a": true}
	full := fastSerialize(fastRecs(2))
	last := fastSerialize(fastRecs(2)[1:])
	for cut := 1; cut <= len(last); cut++ {
		data := full[:len(full)-cut]
		recs, err := parseTreeOutput(bytes.NewReader(data), starts, 100, 1<<20)
		// Ganze letzte Zeile weg (cut == len(last)) ist eine saubere Ausgabe
		if cut == len(last) {
			if err != nil || len(recs) != 1 {
				t.Errorf("Zeile komplett abgeschnitten: %d Zeilen, Fehler %v", len(recs), err)
			}
			continue
		}
		if !errors.Is(err, errFastScanInvalid) {
			t.Errorf("%d Bytes abgeschnitten: Fehler %v, erwartet errFastScanInvalid", cut, err)
		}
	}
	for name, data := range map[string]string{
		"nur NUL":            "\x00",
		"zwei NUL":           "\x00\x00",
		"Typ ohne Rest":      "f\x00",
		"fünf Felder":        "f\x00644\x001\x001.0\x00/home/a\x00",
		"Typ ohne NUL":       "f",
		"NUL statt Typ":      "\x00" + "644\x001\x001.0\x00/home/a\x00\x00",
		"Feld ohne NUL (%P)": "f\x00644\x001\x001.0\x00/home/a\x00abc",
	} {
		if _, err := parseTreeOutput(strings.NewReader(data), starts, 100, 1<<20); !errors.Is(err, errFastScanInvalid) {
			t.Errorf("%s: Fehler %v, erwartet errFastScanInvalid", name, err)
		}
	}
}

// TestFastScanParseTreeOutputFieldLength - zu lange Felder (kurze Felder
// über fastScanMaxShortField, Pfadfelder über fastScanMaxPathField) sind
// ungültig; genau die Grenze noch nicht. Schutz vor Speicherfüllen durch
// einen bösartigen Server.
func TestFastScanParseTreeOutputFieldLength(t *testing.T) {
	starts := map[string]bool{"/home/a": true}
	line := func(typ, perm, size, mtime, start, rel string) []byte {
		return fastSerialize([]fastRecord{{typ, perm, size, mtime, start, rel}})
	}
	long := strings.Repeat("f", fastScanMaxShortField+1)
	for name, data := range map[string][]byte{
		"Typ":    line(long, "644", "1", "1.0", "/home/a", "x"),
		"Rechte": line("f", strings.Repeat("7", fastScanMaxShortField+1), "1", "1.0", "/home/a", "x"),
		"Größe":  line("f", "644", strings.Repeat("1", fastScanMaxShortField+1), "1.0", "/home/a", "x"),
		"Zeit":   line("f", "644", "1", strings.Repeat("1", fastScanMaxShortField+1), "/home/a", "x"),
		"Start":  line("f", "644", "1", "1.0", strings.Repeat("s", fastScanMaxPathField+1), "x"),
		"rel":    line("f", "644", "1", "1.0", "/home/a", strings.Repeat("a", fastScanMaxPathField+1)),
	} {
		if _, err := parseTreeOutput(bytes.NewReader(data), starts, 100, 1<<30); !errors.Is(err, errFastScanInvalid) {
			t.Errorf("zu langes Feld %s: Fehler %v, erwartet errFastScanInvalid", name, err)
		}
	}
	// Genau an der Grenze: Pfadfeld mit fastScanMaxPathField Bytes ist ok
	okLine := line("f", "644", "1", "1.0", "/home/a", strings.Repeat("a", fastScanMaxPathField))
	if recs, err := parseTreeOutput(bytes.NewReader(okLine), starts, 100, 1<<30); err != nil || len(recs) != 1 {
		t.Errorf("rel an der Grenze: %d Zeilen, Fehler %v", len(recs), err)
	}
	// Kurzes Feld an der Grenze ist lang genug zu lesen, aber als Zeit zu lang
	// für die Auswertung: ungültig, aber nicht wegen der Feldlänge
	if _, err := parseTreeOutput(bytes.NewReader(line("f", "644", "1", strings.Repeat("1", fastScanMaxShortField), "/home/a", "x")), starts, 100, 1<<30); !errors.Is(err, errFastScanInvalid) {
		t.Errorf("Zeit mit 32 Ziffern: Fehler %v", err)
	}
}

// TestFastScanParseTreeOutputLimits - Byte-Grenze -> errFastScanTooLarge
// (genau die Ausgabelänge ist noch erlaubt, ein Byte weniger nicht);
// Eintragsgrenze -> errFastScanTooMany (n Zeilen ok, n+1 nicht; bei 0 schon
// die erste).
func TestFastScanParseTreeOutputLimits(t *testing.T) {
	starts := map[string]bool{"/home/a": true}
	data := fastSerialize(fastRecs(5))
	if _, err := parseTreeOutput(bytes.NewReader(data), starts, 100, int64(len(data))); err != nil {
		t.Errorf("Byte-Grenze = Länge: Fehler %v", err)
	}
	if _, err := parseTreeOutput(bytes.NewReader(data), starts, 100, int64(len(data))-1); !errors.Is(err, errFastScanTooLarge) {
		t.Errorf("Byte-Grenze = Länge-1: Fehler %v, erwartet errFastScanTooLarge", err)
	}
	if _, err := parseTreeOutput(bytes.NewReader(data), starts, 100, 0); !errors.Is(err, errFastScanTooLarge) {
		t.Errorf("Byte-Grenze 0: Fehler %v", err)
	}
	if recs, err := parseTreeOutput(bytes.NewReader(data), starts, 5, 1<<20); err != nil || len(recs) != 5 {
		t.Errorf("Eintragsgrenze = 5: %d Zeilen, Fehler %v", len(recs), err)
	}
	if _, err := parseTreeOutput(bytes.NewReader(data), starts, 4, 1<<20); !errors.Is(err, errFastScanTooMany) {
		t.Errorf("Eintragsgrenze 4 bei 5 Zeilen: Fehler %v, erwartet errFastScanTooMany", err)
	}
	if _, err := parseTreeOutput(bytes.NewReader(data), starts, 0, 1<<20); !errors.Is(err, errFastScanTooMany) {
		t.Errorf("Eintragsgrenze 0: Fehler %v", err)
	}
}

// TestFastScanParseTreeOutputInvalidStops - eine ungültige Zeile mitten in
// der Ausgabe verwirft alles (kein Teilergebnis).
func TestFastScanParseTreeOutputInvalidStops(t *testing.T) {
	recs := fastRecs(4)
	recs[2].perm = "9"
	got, err := parseTreeOutput(bytes.NewReader(fastSerialize(recs)), map[string]bool{"/home/a": true}, 100, 1<<20)
	if !errors.Is(err, errFastScanInvalid) || got != nil {
		t.Errorf("Ergebnis %v, Fehler %v", got, err)
	}
}

// --- 3. Rückfall auf SFTP ---

// fastFallbackCase ist ein Fall, in dem der Schnellweg aufgeben muss.
type fastFallbackCase struct {
	name string
	tune func(f *fastFS)
	// start: ListTree scheitert schon beim Start (kein wait/Close)
	start bool
	// vars verändert Paketvariablen (vor dem Lauf)
	vars func()
}

// fastSetRec ändert die Zeile mit dem relativen Pfad rel.
func fastSetRec(rel string, change func(r *fastRecord)) func(f *fastFS) {
	return func(f *fastFS) {
		f.mutate = func(recs []fastRecord) []fastRecord {
			for i := range recs {
				if recs[i].rel == rel {
					change(&recs[i])
				}
			}
			return recs
		}
	}
}

// fastDropRec entfernt die Zeile mit dem relativen Pfad rel.
func fastDropRec(rel string) func(f *fastFS) {
	return func(f *fastFS) {
		f.mutate = func(recs []fastRecord) []fastRecord {
			var out []fastRecord
			for _, r := range recs {
				if r.rel != rel {
					out = append(out, r)
				}
			}
			return out
		}
	}
}

func fastFallbackCases() []fastFallbackCase {
	return []fastFallbackCase{
		{name: "Startfehler (Exec verboten)", start: true, tune: func(f *fastFS) { f.startErr = errors.New("ssh: exec request failed") }},
		{name: "kein Exec (ErrUnsupported)", start: true, tune: func(f *fastFS) { f.startErr = errors.ErrUnsupported }},
		{name: "wait-Fehler (Exit-Code 123)", tune: func(f *fastFS) { f.waitErr = errors.New("Process exited with status 123") }},
		{name: "stderr-Text trotz gültiger Ausgabe", tune: func(f *fastFS) { f.waitErr = errors.New("stderr: find: Permission denied") }},
		{name: "Müll statt Ausgabe", tune: func(f *fastFS) { f.raw, f.rawSet = []byte("Welcome to a restricted shell\n"), true }},
		{name: "sftp-server-Paket (ForceCommand internal-sftp)", tune: func(f *fastFS) {
			f.raw, f.rawSet = []byte{0, 0, 0, 5, 2, 0, 0, 0, 3}, true
		}},
		{name: "leere Ausgabe (internal-sftp: keine Zeilen)", tune: func(f *fastFS) { f.raw, f.rawSet = []byte{}, true }},
		{name: "ungültiges Typzeichen", tune: fastSetRec("main.go", func(r *fastRecord) { r.typ = "x" })},
		{name: "ungültige Rechte", tune: fastSetRec("main.go", func(r *fastRecord) { r.perm = "8" })},
		{name: "ungültige Größe", tune: fastSetRec("main.go", func(r *fastRecord) { r.size = "-1" })},
		{name: "ungültige Zeit", tune: fastSetRec("main.go", func(r *fastRecord) { r.mtime = "abc" })},
		{name: "unbekannter Startpfad", tune: fastSetRec("main.go", func(r *fastRecord) { r.start = "/etc" })},
		{name: "%P mit ..", tune: fastSetRec("main.go", func(r *fastRecord) { r.rel = "../main.go" })},
		{name: "abgeschnittene letzte Zeile", tune: func(f *fastFS) { f.cut = 3 }},
		{name: "Zeile für das Wurzelverzeichnis fehlt", tune: fastDropRec("")},
		{name: "oberste Ebene: andere Rechte", tune: fastSetRec("", func(r *fastRecord) { r.perm = "700" })},
		{name: "oberste Ebene: setuid-Bit mehr", tune: fastSetRec("", func(r *fastRecord) { r.perm = "4750" })},
		{name: "oberste Ebene: anderer Typ (Datei)", tune: fastSetRec("", func(r *fastRecord) { r.typ = "f" })},
		{name: "oberste Ebene: anderer Typ (Symlink)", tune: fastSetRec("", func(r *fastRecord) { r.typ = "l" })},
		{name: "oberste Ebene: andere Größe", tune: fastSetRec("", func(r *fastRecord) { r.size = "4096" })},
		{name: "oberste Ebene: andere Zeit (Sekunden)", tune: fastSetRec("", func(r *fastRecord) { r.mtime = "1700000000.0000000000" })},
		{name: "oberste Ebene: Zeit vor 1970", tune: fastSetRec("", func(r *fastRecord) { r.mtime = "-5.0000000000" })},
		{name: "Kind vor seinem Elternverzeichnis", tune: func(f *fastFS) {
			f.mutate = func(recs []fastRecord) []fastRecord {
				// lib/x.go vor lib verschieben
				var x fastRecord
				var out []fastRecord
				for _, r := range recs {
					if r.rel == "lib/x.go" {
						x = r
					}
				}
				for _, r := range recs {
					switch r.rel {
					case "lib/x.go":
						continue
					case "lib":
						out = append(out, x)
					}
					out = append(out, r)
				}
				return out
			}
		}},
		{name: "Enkel ohne Elternverzeichnis", tune: fastDropRec("lib/deep")},
		{name: "doppelter Pfad", tune: func(f *fastFS) {
			f.mutate = func(recs []fastRecord) []fastRecord {
				for _, r := range recs {
					if r.rel == "main.go" {
						return append(recs, r)
					}
				}
				return recs
			}
		}},
		{name: "doppelte Wurzel", tune: func(f *fastFS) {
			f.mutate = func(recs []fastRecord) []fastRecord { return append(recs, recs[0]) }
		}},
		{name: "Byte-Grenze", vars: func() { fastScanMaxBytes = 60 }},
		{name: "Ausgabe tiefer als copyMaxDepth", vars: func() { copyMaxDepth = 2 }, tune: func(f *fastFS) { f.forceDep = 50 }},
	}
}

// TestFastScanTopTimeIgnoresSubSeconds - SFTP v3 kennt nur ganze Sekunden:
// weicht find beim obersten Verzeichnis nur im Bruchteil der Sekunde ab, ist
// das kein Zweifel, der Schnellweg bleibt (und das Ergebnis ist gleich).
func TestFastScanTopTimeIgnoresSubSeconds(t *testing.T) {
	defer fastKeepVars()()
	got, ref, _ := fastCompare(t, fastSource, []string{"projekt"}, false, fastSetRec("", func(r *fastRecord) {
		r.mtime = fmt.Sprintf("%d.5000000000", copyTestTime.Unix())
	}))
	fastWantPath(t, "Bruchteil", got, "find")
	// Das oberste Verzeichnis bekommt seine Zeit weiter aus SFTP; der Rest gleicht
	fastAssertSame(t, "Bruchteil", got, ref)
}

// TestFastScanFallsBackToSFTP - bei jedem Zweifel läuft stillschweigend der
// SFTP-Weg: das Ergebnis ist trotzdem korrekt und gleich dem der Referenz,
// das Protokoll nennt SFTP, SFTP liest die tieferen Verzeichnisse selbst,
// wait und Close wurden gerufen (kein Prozess bleibt hängen).
func TestFastScanFallsBackToSFTP(t *testing.T) {
	for _, c := range fastFallbackCases() {
		t.Run(c.name, func(t *testing.T) {
			defer fastKeepVars()()
			if c.vars != nil {
				c.vars()
			}
			got, ref, src := fastCompare(t, fastSource, []string{"projekt"}, false, c.tune)
			fastAssertSame(t, c.name, got, ref)
			fastWantPath(t, c.name, got, "SFTP")
			list, waits, closes := src.counters()
			if list != 1 {
				t.Errorf("ListTree %d-mal gerufen, erwartet 1", list)
			}
			wantEnd := 1
			if c.start {
				wantEnd = 0
			}
			if waits != wantEnd || closes != wantEnd {
				t.Errorf("wait %d, Close %d - erwartet je %d", waits, closes, wantEnd)
			}
			// Der SFTP-Weg hat das tiefere Verzeichnis selbst gelesen
			deepLimit := 128
			if c.vars != nil {
				deepLimit = copyMaxDepth
			}
			if deepLimit > 1 && src.readDirCount("/home/a/projekt/lib") < 1 {
				t.Error("kein Rückfall: /home/a/projekt/lib nicht per ReadDir gelesen")
			}
			if len(ref.snap) == 0 && c.vars == nil {
				t.Error("Referenz leer")
			}
		})
	}
}

// TestFastScanFallbackErrorsStaySilent - ein Rückfall ist kein Fehler: die
// Fehlerliste des Laufs enthält nichts vom find-Befehl (kein Text des
// Servers, kein errFastScan*).
func TestFastScanFallbackErrorsStaySilent(t *testing.T) {
	defer fastKeepVars()()
	for _, c := range fastFallbackCases() {
		if c.vars != nil {
			continue
		}
		got, _, _ := fastCompare(t, fastSource, []string{"projekt"}, false, c.tune)
		if len(got.failures) != 0 {
			t.Errorf("%s: Fehler gemeldet: %q", c.name, got.failures)
		}
		for _, e := range got.failErrs {
			if errors.Is(e, errFastScanInvalid) || errors.Is(e, errFastScanTooLarge) || errors.Is(e, errFastScanTooMany) {
				t.Errorf("%s: interner Fehler nach außen: %v", c.name, e)
			}
		}
	}
}

// TestFastScanFallbackOnIdle - liefert find (oder ein ForceCommand) lange
// nichts, bricht der Leerlauf-Wächter ab und der SFTP-Weg übernimmt - nach
// fastScanIdleTimeout, nicht erst nach fastScanTimeout (10 Minuten). Der Fake
// liefert die Daten erst nach 30 s.
func TestFastScanFallbackOnIdle(t *testing.T) {
	defer fastKeepVars()()
	fastScanIdleTimeout = 100 * time.Millisecond
	start := time.Now()
	got, ref, src := fastCompare(t, fastSource, []string{"projekt"}, false, func(f *fastFS) { f.delay = 30 * time.Second })
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Rückfall dauerte %v", elapsed)
	}
	fastAssertSame(t, "Leerlauf", got, ref)
	fastWantPath(t, "Leerlauf", got, "SFTP")
	if list, waits, closes := src.counters(); list != 1 || waits != 1 || closes != 1 {
		t.Errorf("ListTree %d, wait %d, Close %d - erwartet je 1", list, waits, closes)
	}
}

// TestFastScanFallbackOnTotalTimeout - das Gesamtzeitlimit wirkt auch dann,
// wenn der Leerlauf-Wächter nie anschlägt (Server liefert nie etwas).
func TestFastScanFallbackOnTotalTimeout(t *testing.T) {
	defer fastKeepVars()()
	fastScanTimeout = 150 * time.Millisecond
	fastScanIdleTimeout = time.Hour
	start := time.Now()
	got, ref, _ := fastCompare(t, fastSource, []string{"projekt"}, false, func(f *fastFS) { f.delay = 30 * time.Second })
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Rückfall dauerte %v", elapsed)
	}
	fastAssertSame(t, "Zeitlimit", got, ref)
	fastWantPath(t, "Zeitlimit", got, "SFTP")
}

// TestFastScanFallbackRealPath - Startpfade brauchen einen brauchbaren
// SFTP-RealPath (absolut, ohne NUL); sonst wird find gar nicht erst gestartet.
func TestFastScanFallbackRealPath(t *testing.T) {
	for name, rp := range map[string]func(string) (string, error){
		"Fehler":     func(p string) (string, error) { return "", errors.New("realpath: failure") },
		"relativ":    func(p string) (string, error) { return "home/a", nil },
		"leer":       func(p string) (string, error) { return "", nil },
		"mit NUL":    func(p string) (string, error) { return "/home/a\x00/etc", nil },
		"Punktpunkt": func(p string) (string, error) { return "..", nil },
	} {
		t.Run(name, func(t *testing.T) {
			defer fastKeepVars()()
			got, ref, src := fastCompare(t, fastSource, []string{"projekt"}, false, func(f *fastFS) { f.realPath = rp })
			fastAssertSame(t, name, got, ref)
			fastWantPath(t, name, got, "SFTP")
			if list, _, _ := src.counters(); list != 0 {
				t.Errorf("ListTree trotz unbrauchbarem RealPath gerufen (%d-mal)", list)
			}
		})
	}
}

// TestFastScanFallbackTooManyStarts - mehr markierte Verzeichnisse als
// fastScanMaxStarts: SFTP-Weg, find wird nicht gestartet (Schutz vor
// riesiger stdin-Eingabe).
func TestFastScanFallbackTooManyStarts(t *testing.T) {
	defer fastKeepVars()()
	fastScanMaxStarts = 1
	got, ref, src := fastCompare(t, fastSource, []string{"projekt", "zwei"}, false, nil)
	fastAssertSame(t, "zu viele Starts", got, ref)
	if list, _, _ := src.counters(); list != 0 {
		t.Errorf("ListTree trotz Startgrenze gerufen (%d-mal)", list)
	}
	// Genau an der Grenze: Schnellweg
	fastScanMaxStarts = 2
	got, ref, src = fastCompare(t, fastSource, []string{"projekt", "zwei"}, false, nil)
	fastAssertSame(t, "Starts = Grenze", got, ref)
	fastWantPath(t, "Starts = Grenze", got, "find")
	if list, _, _ := src.counters(); list != 1 {
		t.Errorf("ListTree %d-mal gerufen, erwartet 1", list)
	}
}

// --- 4. Unsichere Namen ---

// TestFastScanUnsafeNamesReportedNeverCopied - Namen mit Backslash oder
// Steuerzeichen (auch ein Verzeichnis mit unsicherem Namen und Inhalt) werden
// wie beim SFTP-Weg gemeldet (errUnsafeName, gleiche Pfade, gleiche
// Reihenfolge); ihr Inhalt wird NIE kopiert, obwohl find ihn mitliefert.
// Der Schnellweg bleibt dabei aktiv (kein Rückfall wegen der Namen).
func TestFastScanUnsafeNamesReportedNeverCopied(t *testing.T) {
	defer fastKeepVars()()
	got, ref, src := fastCompare(t, fastSourceUnsafe, []string{"projekt"}, false, nil)
	fastAssertSame(t, "unsichere Namen", got, ref)
	fastWantPath(t, "unsichere Namen", got, "find")
	unsafe := 0
	for _, e := range got.failErrs {
		if errors.Is(e, errUnsafeName) {
			unsafe++
		}
	}
	// x\y, ctl\x01z, bad\dir, lib/nl\nx - CON und a:b sind für dieses Ziel gültig
	if unsafe != 4 || len(got.failures) != 4 {
		t.Errorf("errUnsafeName %d von %d Fehlern, erwartet 4: %q", unsafe, len(got.failures), got.failures)
	}
	for p := range got.snap {
		if strings.Contains(p, "bad") || strings.Contains(p, "deep.txt") || strings.ContainsAny(p, "\\\x01\n") {
			t.Errorf("unsicherer Name oder Inhalt am Ziel: %q", p)
		}
	}
	if _, ok := got.snap["/srv/b/projekt/CON/inner.txt"]; !ok {
		t.Error("CON/inner.txt fehlt am Ziel ohne Namensprüfung")
	}
	// Unter dem unsicheren Verzeichnis liest auch der Rückfall nie
	if src.readDirCount("/home/a/projekt/bad\\dir") != 0 {
		t.Errorf("unsicheres Verzeichnis gelesen (%d)", src.readDirCount("/home/a/projekt/bad\\dir"))
	}
	// Die Fehlerpfade nennen den Namen ohne ".." aufzulösen
	for _, f := range got.failures {
		if !strings.HasPrefix(f, "/home/a/projekt/") {
			t.Errorf("Fehlerpfad außerhalb: %q", f)
		}
	}
}

// TestFastScanUnsafeDirectoryContentNeverWalked - find liefert tief unter
// einem unsicheren Verzeichnis viele Einträge; keiner davon taucht am Ziel
// auf, in den Summen oder in der Fehlerliste (nur das Verzeichnis selbst
// wird gemeldet, genau einmal).
func TestFastScanUnsafeDirectoryContentNeverWalked(t *testing.T) {
	defer fastKeepVars()()
	build := func() *fastFS {
		src := fastSource()
		for i := 0; i < 20; i++ {
			src.addFile(fmt.Sprintf("/home/a/projekt/a\\b/d%d/e/f.txt", i), "x", 0o644, copyTestTime)
		}
		return src
	}
	got, ref, _ := fastCompare(t, build, []string{"projekt"}, false, nil)
	fastAssertSame(t, "Inhalt unter unsicherem Verzeichnis", got, ref)
	fastWantPath(t, "Inhalt unter unsicherem Verzeichnis", got, "find")
	if len(got.failures) != 1 || !errors.Is(got.failErrs[0], errUnsafeName) {
		t.Errorf("Fehler: %q", got.failures)
	}
	if got.totalFiles != 5 || got.totalDirs != 4 {
		t.Errorf("Summen Dateien %d, Verzeichnisse %d (erwartet 5/4)", got.totalFiles, got.totalDirs)
	}
}

// TestFastScanDestinationNameValidator - ein Ziel mit nameValidator
// (lokales Windows) lehnt "CON", "NUL" und "a:b" ab: Meldung wie beim SFTP-Weg,
// Inhalt von "CON/" wird nie angelegt.
func TestFastScanDestinationNameValidator(t *testing.T) {
	defer fastKeepVars()()
	got, ref, _ := fastCompare(t, fastSourceUnsafe, []string{"projekt"}, true, nil)
	fastAssertSame(t, "strenges Ziel", got, ref)
	fastWantPath(t, "strenges Ziel", got, "find")
	// 4 unsichere Namen plus CON (Verzeichnis) und a:b
	if len(got.failures) != 6 {
		t.Errorf("%d Fehler, erwartet 6: %q", len(got.failures), got.failures)
	}
	for p := range got.snap {
		if strings.Contains(p, "CON") || strings.Contains(p, "a:b") {
			t.Errorf("abgelehnter Name am Ziel: %q", p)
		}
	}
	// Auch ein oberster Name wird geprüft (ohne Schnellweg, ohne ListTree)
	src := fastSource()
	src.addFile("/home/a/CON/x.txt", "x", 0o644, copyTestTime)
	o := fastRun(t, context.Background(), src, true, []string{"CON"})
	if len(o.failures) != 1 || !errors.Is(o.failErrs[0], errUnsafeName) || len(o.snap) != 0 {
		t.Errorf("oberster Name CON: Fehler %q, Ziel %v", o.failures, o.snap)
	}
	if list, _, _ := src.counters(); list != 0 {
		t.Errorf("ListTree für einen abgelehnten obersten Namen gerufen (%d-mal)", list)
	}
}

// --- 5. Grenzen ---

// TestFastScanEntryLimitSameAsSFTP - copyMaxEntries klein: wie beim SFTP-Weg
// genau ein errCopyLimit und nichts kopiert, für jede Grenze von 1 bis 11
// (der Baum hat 9 Einträge; an der Kante gleiches Verhalten).
func TestFastScanEntryLimitSameAsSFTP(t *testing.T) {
	for limit := 1; limit <= 11; limit++ {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			defer fastKeepVars()()
			copyMaxEntries = limit
			got, ref, _ := fastCompare(t, fastSource, []string{"projekt"}, false, nil)
			fastAssertLimit(t, fmt.Sprint("Grenze ", limit), got, ref)
			if limit < 9 {
				if len(got.failures) != 1 || !errors.Is(got.failErrs[0], errCopyLimit) {
					t.Errorf("Fehler %q, erwartet genau ein errCopyLimit", got.failures)
				}
				if len(got.snap) != 0 {
					t.Errorf("trotz Grenze kopiert: %v", got.snap)
				}
				if len(got.copied) != 0 {
					t.Errorf("copiedNames %v trotz Grenze", got.copied)
				}
			}
			if limit >= 11 && len(got.failures) != 0 {
				t.Errorf("Grenze %d reicht, Fehler %q", limit, got.failures)
			}
		})
	}
}

// TestFastScanDepthLimitSameAsSFTP - copyMaxDepth klein: wie beim SFTP-Weg
// genau ein errCopyLimit (der Baum ist 3 Ebenen tief), nichts kopiert; ListTree
// bekommt jeweils die aktuelle Tiefe.
func TestFastScanDepthLimitSameAsSFTP(t *testing.T) {
	for depth := 0; depth <= 5; depth++ {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			defer fastKeepVars()()
			copyMaxDepth = depth
			got, ref, src := fastCompare(t, fastSource, []string{"projekt"}, false, nil)
			fastAssertLimit(t, fmt.Sprint("Tiefe ", depth), got, ref)
			if depth <= 3 {
				if len(got.failures) != 1 || !errors.Is(got.failErrs[0], errCopyLimit) {
					t.Errorf("Fehler %q, erwartet genau ein errCopyLimit", got.failures)
				}
				if len(got.snap) != 0 {
					t.Errorf("trotz Grenze kopiert: %v", got.snap)
				}
			} else if len(got.failures) != 0 {
				t.Errorf("Tiefe %d reicht, Fehler %q", depth, got.failures)
			}
			if list, _, _ := src.counters(); list == 1 && src.lastDepth != depth {
				t.Errorf("ListTree mit Tiefe %d, erwartet %d", src.lastDepth, depth)
			}
		})
	}
}

// TestFastScanCommandUsesCopyMaxDepth - der Befehl trägt die Tiefengrenze
// als Zahl; negative Werte werden zu 0.
func TestFastScanCommandUsesCopyMaxDepth(t *testing.T) {
	defer fastKeepVars()()
	for _, depth := range []int{0, 1, 5, 128, 100000} {
		if cmd := treeListCommand(depth); !strings.Contains(cmd, fmt.Sprintf("-maxdepth %d ", depth)) {
			t.Errorf("treeListCommand(%d) = %q ohne -maxdepth", depth, cmd)
		}
	}
	if cmd := treeListCommand(-3); !strings.Contains(cmd, "-maxdepth 0 ") {
		t.Errorf("treeListCommand(-3) = %q", cmd)
	}
	copyMaxDepth = 7
	_, _, src := fastCompare(t, fastSource, []string{"projekt"}, false, nil)
	if src.lastDepth != 7 {
		t.Errorf("ListTree mit Tiefe %d, erwartet copyMaxDepth = 7", src.lastDepth)
	}
}

// --- 6. Abbruch ---

// TestFastScanCancelDuringListTree - Esc während find läuft: runCopy endet
// schnell mit cancelled, am Ziel entsteht nichts, es gibt keinen Rückfall
// ohne Ende (kein Lesen tiefer Verzeichnisse), wait und Close wurden gerufen.
func TestFastScanCancelDuringListTree(t *testing.T) {
	defer fastKeepVars()()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := fastSource()
	src.delay = time.Minute
	src.onList = func() { go func() { time.Sleep(50 * time.Millisecond); cancel() }() }
	start := time.Now()
	o := fastRun(t, ctx, src, false, []string{"projekt"})
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Abbruch dauerte %v", elapsed)
	}
	if !o.cancelled {
		t.Error("Abbruch nicht gemeldet")
	}
	if len(o.snap) != 0 || len(o.mkdirs) != 0 {
		t.Errorf("nach Abbruch am Ziel: %v, Mkdir %v", o.snap, o.mkdirs)
	}
	if len(o.copied) != 0 {
		t.Errorf("copiedNames %v nach Abbruch", o.copied)
	}
	if _, waits, closes := src.counters(); waits != 1 || closes != 1 {
		t.Errorf("wait %d, Close %d - erwartet je 1", waits, closes)
	}
	if n := src.readDirCount("/home/a/projekt/lib"); n != 0 {
		t.Errorf("nach dem Abbruch wurde noch tief gelesen (%d)", n)
	}
}

// --- 6a. Esc und Zeitgrenzen bei einem Server, der den Kanal nie schließt ---

// fastHangMode wählt, wo der Fake hängt.
type fastHangMode int

const (
	// hangInList: schon ListTree kehrt nicht zurück
	hangInList fastHangMode = iota
	// hangInRead: der Leser liefert nie Daten
	hangInRead
	// hangInWait: die Ausgabe ist vollständig, wait kehrt nicht zurück
	hangInWait
)

// fastHangFS ist ein fastFS, dessen find-Befehl hängt: Kontext und Close
// werden IGNORIERT (ein Server, der das Schließen des Exec-Kanals nie
// bestätigt). Erst AbortTreeList (oder releaseAll im Aufräumen des Tests)
// gibt Leser und wait frei. Er zählt AbortTreeList-Aufrufe.
type fastHangFS struct {
	*fastFS
	mode    fastHangMode
	release chan struct{}
	once    sync.Once
	aborts  atomic.Int32
	// exited wird geschlossen, sobald der hängende Aufruf (ListTree, Read oder
	// wait) nach der Freigabe zurückkehrt (exitOnce: genau einmal)
	exited   chan struct{}
	exitOnce sync.Once
}

func newFastHangFS(mode fastHangMode) *fastHangFS {
	return &fastHangFS{fastFS: fastSource(), mode: mode, release: make(chan struct{}), exited: make(chan struct{})}
}

// releaseAll gibt alles Hängende frei (ohne AbortTreeList zu zählen).
func (h *fastHangFS) releaseAll() { h.once.Do(func() { close(h.release) }) }

// shutdown gibt frei und wartet (höchstens 2 s), bis der hängende Aufruf
// zurückgekehrt ist. Das gibt dem Datenrennen-Wächter die Ordnung, die er
// braucht: Die Hintergrund-Goroutine hat die Paketvariablen (Grenzen) vor dem
// Hängen gelesen, bevor fastKeepVars sie zurückschreibt.
func (h *fastHangFS) shutdown() {
	h.releaseAll()
	select {
	case <-h.exited:
	case <-time.After(2 * time.Second):
	}
}

// left meldet das Ende des hängenden Aufrufs.
func (h *fastHangFS) left() { h.exitOnce.Do(func() { close(h.exited) }) }

// AbortTreeList trennt "die Verbindung": zählt und gibt frei.
func (h *fastHangFS) AbortTreeList() {
	h.aborts.Add(1)
	h.releaseAll()
}

func (h *fastHangFS) ListTree(ctx context.Context, starts []string, maxDepth int) (io.ReadCloser, func() error, error) {
	if h.mode == hangInList {
		<-h.release
		h.left()
		return nil, nil, io.ErrClosedPipe
	}
	wait := func() error {
		<-h.release
		if h.mode == hangInWait {
			h.left()
		}
		return io.ErrClosedPipe
	}
	if h.mode == hangInWait {
		data := fastSerialize(fastGenRecords(h.memFS, starts, maxDepth))
		return io.NopCloser(bytes.NewReader(data)), wait, nil
	}
	return &fastHangReader{release: h.release, left: h.left}, wait, nil
}

// fastHangReader blockiert in Read bis zur Freigabe; Close tut nichts.
type fastHangReader struct {
	release chan struct{}
	left    func()
}

func (r *fastHangReader) Read(b []byte) (int, error) {
	<-r.release
	r.left()
	return 0, io.ErrClosedPipe
}

func (r *fastHangReader) Close() error { return nil }

// fastAbortCountFS ist ein normal antwortender fastFS (Leser endet bei ctx
// und Close) mit AbortTreeList-Zähler.
type fastAbortCountFS struct {
	*fastFS
	aborts   atomic.Int32
	exited   chan struct{}
	exitOnce sync.Once
}

func newFastAbortCountFS() *fastAbortCountFS {
	return &fastAbortCountFS{fastFS: fastSource(), exited: make(chan struct{})}
}

func (a *fastAbortCountFS) AbortTreeList() { a.aborts.Add(1) }

// ListTree reicht an fastFS weiter und meldet das Ende von wait (der
// Hintergrund hat die Paketvariablen dann längst gelesen).
func (a *fastAbortCountFS) ListTree(ctx context.Context, starts []string, maxDepth int) (io.ReadCloser, func() error, error) {
	rd, wait, err := a.fastFS.ListTree(ctx, starts, maxDepth)
	if err != nil {
		return rd, wait, err
	}
	return rd, func() error {
		defer a.exitOnce.Do(func() { close(a.exited) })
		return wait()
	}, nil
}

// shutdown wartet (höchstens 2 s), bis der Hintergrund wait erreicht hat; vor
// dem Zurückschreiben der Paketvariablen (Ordnung für den -race-Wächter).
func (a *fastAbortCountFS) shutdown() {
	select {
	case <-a.exited:
	case <-time.After(2 * time.Second):
	}
}

// fastWaitAborts wartet bis zu limit, bis der Zähler want erreicht; danach
// noch kurz, damit ein zweiter, unerwarteter Aufruf auffällt. Liefert den Stand.
func fastWaitAborts(count *atomic.Int32, want int32, limit time.Duration) int32 {
	deadline := time.Now().Add(limit)
	for count.Load() < want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	return count.Load()
}

// TestFastScanEscDoesNotWaitForHangingFind - Esc, während find hängt und
// weder Kontext noch Close beachtet (Leser, wait oder schon ListTree): runCopy
// endet binnen einer Sekunde mit cancelled (nicht erst nach der Schonfrist
// oder nie), danach trennt der Hintergrund AbortTreeList genau einmal (bei
// hängendem ListTree gibt es keinen Kanal: 0-mal, kein Trennen). Am
// Ziel entsteht nichts, es gibt keinen Rückfall (kein ReadDir von lib).
func TestFastScanEscDoesNotWaitForHangingFind(t *testing.T) {
	for name, mode := range map[string]fastHangMode{"Leser hängt": hangInRead, "wait hängt": hangInWait, "ListTree hängt": hangInList} {
		t.Run(name, func(t *testing.T) {
			defer fastKeepVars()()
			fastScanAbortGrace = 200 * time.Millisecond
			src := newFastHangFS(mode)
			defer src.shutdown()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			time.AfterFunc(50*time.Millisecond, cancel)
			start := time.Now()
			o := fastRunReq(t, ctx, src, false, []string{"projekt"}, false, 3*time.Second)
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Errorf("Esc wirkte erst nach %v (erwartet < 1 s)", elapsed)
			}
			if !o.cancelled {
				t.Error("Abbruch nicht gemeldet")
			}
			if len(o.snap) != 0 || len(o.copied) != 0 {
				t.Errorf("nach Abbruch am Ziel: %v, copied %v", o.snap, o.copied)
			}
			if n := src.readDirCount("/home/a/projekt/lib"); n != 0 {
				t.Errorf("nach Esc wurde noch gelesen (%d)", n)
			}
			// Der Hintergrund trennt nach der Schonfrist genau einmal - außer
			// wenn schon ListTree hängt: dann gibt es keinen Kanal, den man
			// schließen oder trennen könnte (nur der Schnellweg wird abgeschaltet)
			wantAborts := int32(1)
			if mode == hangInList {
				wantAborts = 0
			}
			wait := 2 * time.Second
			if wantAborts == 0 {
				// Es soll nichts kommen: nicht die ganze Zeit warten
				wait = 400 * time.Millisecond
			}
			if n := fastWaitAborts(&src.aborts, 1, wait); n != wantAborts {
				t.Errorf("AbortTreeList %d-mal, erwartet genau %d", n, wantAborts)
			}
		})
	}
}

// TestFastScanIdleHangAbortsAndReportsConnectionLost - Leerlauf (oder
// Zeitlimit) bei einem find mit Kanal, das auch nach dem Abbruch nicht endet
// (hängendes ListTree: eigener Test TestFastScanHangingListTreeFallsBack): Nach der
// Schonfrist trennt AbortTreeList (genau einmal), fastScan meldet einen Fehler
// mit errConnectionLost und kopiert NICHTS - kein stiller SFTP-Rückfall über
// die gerade getrennte Leitung (kein ReadDir von lib). Das Ergebnis kommt
// binnen grob Schonfrist + 1 s.
func TestFastScanIdleHangAbortsAndReportsConnectionLost(t *testing.T) {
	cases := []struct {
		name string
		mode fastHangMode
		vars func()
	}{
		{"Leser hängt (Leerlauf)", hangInRead, func() { fastScanIdleTimeout = 50 * time.Millisecond }},
		{"wait hängt (Leerlauf)", hangInWait, func() { fastScanIdleTimeout = 50 * time.Millisecond }},
		{"Leser hängt (Zeitlimit)", hangInRead, func() { fastScanTimeout = 100 * time.Millisecond; fastScanIdleTimeout = time.Hour }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer fastKeepVars()()
			fastScanAbortGrace = 200 * time.Millisecond
			c.vars()
			src := newFastHangFS(c.mode)
			defer src.shutdown()
			start := time.Now()
			o := fastRunReq(t, context.Background(), src, false, []string{"projekt"}, false, 3*time.Second)
			if elapsed := time.Since(start); elapsed > fastScanAbortGrace+time.Second {
				t.Errorf("Ergebnis nach %v (erwartet < Schonfrist + 1 s)", elapsed)
			}
			if n := fastWaitAborts(&src.aborts, 1, time.Second); n != 1 {
				t.Errorf("AbortTreeList %d-mal, erwartet genau 1", n)
			}
			lost := false
			for _, e := range o.failErrs {
				if errors.Is(e, errConnectionLost) {
					lost = true
				}
			}
			if !lost {
				t.Errorf("kein Fehler mit errConnectionLost: %q", o.failures)
			}
			if len(o.snap) != 0 || len(o.copied) != 0 || len(o.mkdirs) != 0 {
				t.Errorf("trotz getrennter Verbindung kopiert: %v, copied %v", o.snap, o.copied)
			}
			if n := src.readDirCount("/home/a/projekt/lib"); n != 0 {
				t.Errorf("SFTP-Rückfall über die getrennte Leitung: lib %d-mal gelesen", n)
			}
			if o.cancelled {
				t.Error("als abgebrochen gemeldet, es war kein Esc")
			}
			if !strings.Contains(strings.Join(o.logs, "\n"), "Verbindung getrennt") {
				t.Errorf("Protokoll nennt das Trennen nicht: %q", o.logs)
			}
		})
	}
}

// TestFastScanIdleWithNormalServerFallsBackToSFTP - Gegenstück: Antwortet der
// Server normal (der Leser endet bei Abbruch und Close, wait kehrt sofort
// zurück), ist der Leerlauf nur ein Rückfall auf SFTP binnen der Schonfrist:
// Ergebnis gleich der Referenz, KEIN AbortTreeList (die Verbindung bleibt
// heil), kein Fehler. Auch nach der Schonfrist kommt kein später Aufruf.
func TestFastScanIdleWithNormalServerFallsBackToSFTP(t *testing.T) {
	defer fastKeepVars()()
	fastScanIdleTimeout = 50 * time.Millisecond
	fastScanAbortGrace = 200 * time.Millisecond
	fastScanEnabled = false
	ref := fastRun(t, context.Background(), fastSource(), false, []string{"projekt"})
	fastScanEnabled = true
	src := newFastAbortCountFS()
	defer src.shutdown()
	src.delay = 30 * time.Second
	start := time.Now()
	got := fastRunReq(t, context.Background(), src, false, []string{"projekt"}, false, 5*time.Second)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Rückfall dauerte %v", elapsed)
	}
	fastWantPath(t, "Leerlauf, normaler Server", got, "SFTP")
	fastAssertSame(t, "Leerlauf, normaler Server", got, ref)
	if n := fastWaitAborts(&src.aborts, 1, 400*time.Millisecond); n != 0 {
		t.Errorf("AbortTreeList %d-mal bei normal antwortendem Server, erwartet 0", n)
	}
	if len(got.failures) != 0 {
		t.Errorf("Fehler: %q", got.failures)
	}
}

// TestFastScanEscWithNormalServerNeverAborts - Esc bei einem normal
// antwortenden Server (Leser endet bei ctx): sofort cancelled, und das
// Hintergrund-Trennen findet nichts mehr zu tun - AbortTreeList 0-mal.
func TestFastScanEscWithNormalServerNeverAborts(t *testing.T) {
	defer fastKeepVars()()
	fastScanAbortGrace = 200 * time.Millisecond
	src := newFastAbortCountFS()
	defer src.shutdown()
	src.delay = time.Minute
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	o := fastRunReq(t, ctx, src, false, []string{"projekt"}, false, 3*time.Second)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Esc wirkte erst nach %v", elapsed)
	}
	if !o.cancelled || len(o.snap) != 0 {
		t.Errorf("cancelled %v, Ziel %v", o.cancelled, o.snap)
	}
	if n := fastWaitAborts(&src.aborts, 1, 500*time.Millisecond); n != 0 {
		t.Errorf("AbortTreeList %d-mal, erwartet 0", n)
	}
}

// TestFastScanAlreadyCancelledNeverStartsFind - ist der Kontext schon
// beendet, startet find gar nicht.
func TestFastScanAlreadyCancelledNeverStartsFind(t *testing.T) {
	defer fastKeepVars()()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := fastSource()
	o := fastRun(t, ctx, src, false, []string{"projekt"})
	if list, _, _ := src.counters(); list != 0 {
		t.Errorf("ListTree trotz Abbruch gerufen (%d-mal)", list)
	}
	if !o.cancelled || len(o.snap) != 0 {
		t.Errorf("cancelled %v, Ziel %v", o.cancelled, o.snap)
	}
}

// --- 7. Wann der Schnellweg nie läuft ---

// fastNoLister verdeckt ListTree: der Typ implementiert treeLister nicht
// (eingebettet ist nur das Interface remoteFS).
type fastNoLister struct {
	remoteFS
}

// TestFastScanNotUsedWithoutLister - eine Quelle ohne treeLister (wie der
// lokale PC) nutzt nie den Schnellweg: SFTP liest alle Verzeichnisse, kein
// Protokoll zum Erfassen, gleiches Ergebnis.
func TestFastScanNotUsedWithoutLister(t *testing.T) {
	defer fastKeepVars()()
	fastScanEnabled = true
	base := fastSource()
	src := &fastNoLister{remoteFS: base}
	if _, ok := remoteFS(src).(treeLister); ok {
		t.Fatal("Testaufbau: fastNoLister implementiert treeLister")
	}
	o := fastRun(t, context.Background(), src, false, []string{"projekt"})
	fastScanEnabled = false
	ref := fastRun(t, context.Background(), fastSource(), false, []string{"projekt"})
	fastAssertSame(t, "ohne treeLister", o, ref)
	if fastScanLog(o) != "" {
		t.Errorf("Protokoll zum Erfassen ohne Schnellweg: %q", o.logs)
	}
	if list, _, _ := base.counters(); list != 0 {
		t.Errorf("ListTree gerufen (%d-mal)", list)
	}
	if base.readDirCount("/home/a/projekt/lib/deep") == 0 {
		t.Error("tiefes Verzeichnis nicht per ReadDir gelesen")
	}
}

// TestFastScanNotUsedForLocalPC - der lokale PC (localFS) kann kein ListTree;
// Kopieren vom PC auf einen Server funktioniert ohne Schnellweg und ohne
// Protokollzeile zum Erfassen.
func TestFastScanNotUsedForLocalPC(t *testing.T) {
	defer fastKeepVars()()
	fastScanEnabled = true
	local, dir := newLocalTestFS(t)
	if _, ok := remoteFS(local).(treeLister); ok {
		t.Fatal("localFS implementiert treeLister")
	}
	if err := os.MkdirAll(filepath.Join(dir, "projekt", "lib", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "projekt", "lib", "deep", "y.go"), []byte("yy"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := &fastDst{memFS: newMemFS("/srv/b")}
	var logs []string
	var mu sync.Mutex
	req := copyRequest{
		src: local, dst: dst, srcDir: local.home, dstDir: "/srv/b", names: []string{"projekt"},
		logf: func(format string, args ...interface{}) {
			mu.Lock()
			logs = append(logs, fmt.Sprintf(format, args...))
			mu.Unlock()
		},
	}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 {
		t.Fatalf("Fehler: %v", res.failures)
	}
	if c, ok := dst.content("/srv/b/projekt/lib/deep/y.go"); !ok || c != "yy" {
		t.Errorf("y.go am Ziel: %q ok=%v", c, ok)
	}
	for _, l := range logs {
		if strings.Contains(l, "Erfassen über") {
			t.Errorf("Protokoll zum Erfassen beim lokalen PC: %q", l)
		}
	}
}

// TestFastScanDisabledSwitch - fastScanEnabled = false: ListTree wird nie
// gerufen, auch wenn die Quelle es könnte; SFTP liest die tiefen Verzeichnisse
// und das Protokoll nennt den SFTP-Weg.
func TestFastScanDisabledSwitch(t *testing.T) {
	defer fastKeepVars()()
	fastScanEnabled = false
	src := fastSource()
	o := fastRun(t, context.Background(), src, false, []string{"projekt"})
	if list, _, _ := src.counters(); list != 0 {
		t.Errorf("ListTree trotz Schalter gerufen (%d-mal)", list)
	}
	// Die Quelle könnte find: das Protokoll nennt den SFTP-Weg
	fastWantPath(t, "Schalter aus", o, "SFTP")
	if src.readDirCount("/home/a/projekt/lib/deep") == 0 || len(o.failures) != 0 {
		t.Errorf("SFTP-Weg unvollständig: Fehler %q", o.failures)
	}
}

// TestFastScanSkippedWithoutDirectories - sind nur Dateien und Symlinks
// markiert (auch ein Symlink auf ein Verzeichnis), startet find nie: es gibt
// nichts zu durchsuchen, und Symlinks werden nie verfolgt.
func TestFastScanSkippedWithoutDirectories(t *testing.T) {
	defer fastKeepVars()()
	for _, names := range [][]string{{"notiz.txt"}, {"lnk"}, {"notiz.txt", "lnk"}} {
		got, ref, src := fastCompare(t, fastSource, names, false, nil)
		fastAssertSame(t, fmt.Sprint(names), got, ref)
		if list, _, _ := src.counters(); list != 0 {
			t.Errorf("%v: ListTree gerufen (%d-mal)", names, list)
		}
		if len(got.failures) != 0 || len(got.snap) == 0 {
			t.Errorf("%v: Fehler %q, Ziel %v", names, got.failures, got.snap)
		}
	}
	got, _, _ := fastCompare(t, fastSource, []string{"lnk"}, false, nil)
	if got.snap["/srv/b/lnk"] != "link projekt" {
		t.Errorf("Symlink auf Verzeichnis: %q (muss Symlink bleiben, nicht verfolgt)", got.snap["/srv/b/lnk"])
	}
}

// TestFastScanMissingTopEntry - ein markierter Name, den es in der Quelle
// nicht (mehr) gibt, wird wie beim SFTP-Weg gemeldet; die übrigen werden
// kopiert (find bekommt nur die vorhandenen Verzeichnisse).
func TestFastScanMissingTopEntry(t *testing.T) {
	defer fastKeepVars()()
	got, ref, src := fastCompare(t, fastSource, []string{"projekt", "gibtsnicht"}, false, nil)
	fastAssertSame(t, "fehlender Name", got, ref)
	if len(got.failures) != 1 || !strings.Contains(got.failures[0], "gibtsnicht") {
		t.Errorf("Fehler: %q", got.failures)
	}
	if !reflect.DeepEqual(src.lastStarts, []string{"/home/a/projekt"}) {
		t.Errorf("Startpfade %q", src.lastStarts)
	}
}

// --- 8. Befehlstext ---

// TestFastScanCommandIsFixedText - der Befehl ist ein fester Text: nur die
// Tiefe ist eine Zahl. Kein "!" (csh-History), kein Backslash-Apostroph (fish),
// kein Zeilenumbruch (csh), kein NUL/Wagenrücklauf, keine Pfade, genau zwei
// einfache Anführungszeichen (Anfang und Ende des sh -c-Blocks).
func TestFastScanCommandIsFixedText(t *testing.T) {
	cmd := treeListCommand(128)
	for _, bad := range []string{"!", "\\'", "\n", "\r", "\x00", "/home", "/tmp", "~", "`", "$("} {
		if strings.Contains(cmd, bad) {
			t.Errorf("Befehl enthält %q: %s", bad, cmd)
		}
	}
	if n := strings.Count(cmd, "'"); n != 2 {
		t.Errorf("%d einfache Anführungszeichen, erwartet 2: %s", n, cmd)
	}
	if !strings.HasPrefix(cmd, "sh -c '") || !strings.HasSuffix(cmd, "'") {
		t.Errorf("Befehl nicht als sh -c '…' eingepackt: %s", cmd)
	}
	for _, want := range []string{"LC_ALL=C", "xargs -0", "find -P", "-mindepth 0", "-maxdepth 128", "-printf"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("Befehl ohne %q: %s", want, cmd)
		}
	}
	// Nur die Zahl unterscheidet zwei Befehle
	a, b := treeListCommand(1), treeListCommand(22)
	if strings.Replace(a, "-maxdepth 1 ", "-maxdepth N ", 1) != strings.Replace(b, "-maxdepth 22 ", "-maxdepth N ", 1) {
		t.Errorf("Befehle unterscheiden sich außer in der Tiefe:\n%s\n%s", a, b)
	}
	// Die Zeile hat die sechs Felder, die parseTreeOutput erwartet
	if !strings.Contains(cmd, `"%y\0%m\0%s\0%T@\0%H\0%P\0"`) {
		t.Errorf("Formatangabe stimmt nicht mit parseTreeOutput überein: %s", cmd)
	}
}

// TestFastScanInputIsNulSeparated - die Startpfade gehen NUL-getrennt (jeder
// mit NUL am Ende) über stdin, nie über die Befehlszeile; Zeilenumbrüche und
// Anführungszeichen im Pfad bleiben unverändert.
func TestFastScanInputIsNulSeparated(t *testing.T) {
	if got := treeListInput(nil); len(got) != 0 {
		t.Errorf("ohne Startpfade: %q", got)
	}
	starts := []string{"/home/a/projekt", "/home/a/b c", "/home/a/nl\nx", "/home/a/it's \"q\""}
	got := treeListInput(starts)
	want := "/home/a/projekt\x00/home/a/b c\x00/home/a/nl\nx\x00/home/a/it's \"q\"\x00"
	if string(got) != want {
		t.Errorf("Eingabe %q, erwartet %q", got, want)
	}
	if parts := bytes.Split(got, []byte{0}); len(parts) != len(starts)+1 || len(parts[len(parts)-1]) != 0 {
		t.Errorf("Zerlegung: %q", parts)
	}
	for _, s := range starts {
		if strings.Contains(treeListCommand(3), s) {
			t.Errorf("Startpfad %q steht im Befehl", s)
		}
	}
}

// --- 9. Login-Shells ---

// fastShellRun startet "<shell> -c script" wie sshd (Umgebung HOME, PATH,
// LANG=C.UTF-8) mit stdin und liefert stdout, stderr und Exit-Code.
func fastShellRun(shell, script, home string, stdin []byte) (stdout, stderr []byte, code int) {
	cmd := exec.Command(shell, "-c", script)
	cmd.Dir = home
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8"}
	cmd.Stdin = bytes.NewReader(stdin)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	if err == nil {
		return so.Bytes(), se.Bytes(), 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return so.Bytes(), se.Bytes(), exitErr.ExitCode()
	}
	return so.Bytes(), se.Bytes(), 127
}

// fastShellTree legt für jeden unsicheren Namen ein Verzeichnis mit
// Unterverzeichnis "sub", Datei "sub/in ner.txt" und Datei "line\nbreak.txt"
// an und liefert die Startpfade.
func fastShellTree(t *testing.T) []string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var starts []string
	for _, name := range []string{"a b", "it's", "q\"x", "nl\nx", "back\\slash", "bang!x", "dollar$HOME", "{}", "-dash", "*star"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		for _, f := range []string{filepath.Join(dir, "sub", "in ner.txt"), filepath.Join(dir, "line\nbreak.txt")} {
			if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
				t.Fatalf("%q: %v", f, err)
			}
		}
		starts = append(starts, dir)
	}
	return starts
}

// TestFastScanLoginShells - der Befehl läuft unter jeder Login-Shell gleich:
// "<shell> -c treeListCommand(5)" mit stdin = treeListInput(starts) auf einem
// Baum mit Namen voller Sonderzeichen (Leerzeichen, ' " \ ! $ {} - * und
// Zeilenumbruch). Erwartet: Exit 0, leeres stderr, byte-gleiche Ausgabe wie
// unter sh, parseTreeOutput fehlerfrei und je Start genau 4 Einträge. Ohne
// -printf im lokalen find (BusyBox) wird übersprungen; BusyBox-sh nimmt sein
// eigenes find und scheitert - das ist der Rückfall und hier nicht gemeint.
func TestFastScanLoginShells(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Login-Shell-Tests nur unter Unix")
	}
	starts := fastShellTree(t)
	if out, err := exec.Command("find", starts[0], "-maxdepth", "0", "-printf", "%y").CombinedOutput(); err != nil || string(out) != "d" {
		t.Skipf("lokales find ohne -printf: %q (%v)", out, err)
	}
	if _, err := exec.LookPath("xargs"); err != nil {
		t.Skip("xargs fehlt")
	}
	allowed := map[string]bool{}
	for _, s := range starts {
		allowed[s] = true
	}
	script, input := treeListCommand(5), treeListInput(starts)
	shPath := loginShellPath(t, "sh")
	baseline, berr, code := fastShellRun(shPath, script, filepath.Dir(starts[0]), input)
	if code != 0 || len(berr) != 0 {
		t.Skipf("sh selbst scheitert (Exit %d, stderr %q): lokales find/xargs nicht GNU-kompatibel", code, berr)
	}
	wantRels := []string{"", "line\nbreak.txt", "sub", "sub/in ner.txt"}
	for _, name := range loginShellNames {
		t.Run(name, func(t *testing.T) {
			shell := loginShellPath(t, name)
			out, errOut, code := fastShellRun(shell, script, filepath.Dir(starts[0]), input)
			if code != 0 || len(errOut) != 0 {
				t.Fatalf("Exit %d, stderr %q", code, errOut)
			}
			if !bytes.Equal(out, baseline) {
				t.Errorf("Ausgabe weicht von sh ab:\n %q\n %q", out, baseline)
			}
			recs, err := parseTreeOutput(bytes.NewReader(out), allowed, 1000, 1<<20)
			if err != nil {
				t.Fatalf("parseTreeOutput: %v", err)
			}
			rels := map[string][]string{}
			for _, r := range recs {
				rels[r.start] = append(rels[r.start], r.rel)
			}
			if len(recs) != 4*len(starts) {
				t.Errorf("%d Einträge, erwartet %d", len(recs), 4*len(starts))
			}
			for _, s := range starts {
				got := append([]string(nil), rels[s]...)
				sort.Strings(got)
				if !reflect.DeepEqual(got, wantRels) {
					t.Errorf("Start %q: %q, erwartet %q", s, got, wantRels)
				}
			}
		})
	}
}

// --- 10. Protokoll ---

// TestFastScanLogsTheWayTaken - req.logf erhält beim Schnellweg genau eine
// Zeile mit "find" (samt Zahl der Einträge), beim Rückfall genau eine mit
// "SFTP"; ohne logf (nil) läuft beides ohne Absturz. Texte vom Server
// (Fehlertext im Rückfall) stehen bereinigt im Protokoll: keine Steuerzeichen.
func TestFastScanLogsTheWayTaken(t *testing.T) {
	defer fastKeepVars()()
	got, _, _ := fastCompare(t, fastSource, []string{"projekt"}, false, nil)
	n := 0
	for _, l := range got.logs {
		if strings.HasPrefix(l, "Datei-Manager: Erfassen über ") {
			n++
		}
	}
	if n != 1 || !strings.Contains(fastScanLog(got), "find (9 Einträge") {
		t.Errorf("Schnellweg: Protokoll %q", got.logs)
	}

	evil := "stderr: find: \x1b[31mboese\x1b[0m\nzweite Zeile\x07"
	got, _, _ = fastCompare(t, fastSource, []string{"projekt"}, false, func(f *fastFS) { f.waitErr = errors.New(evil) })
	l := fastScanLog(got)
	if !strings.Contains(l, "SFTP") {
		t.Errorf("Rückfall: Protokoll %q", got.logs)
	}
	for _, r := range l {
		if r < 0x20 || r == 0x7f {
			t.Errorf("Steuerzeichen %q im Protokoll: %q", r, l)
			break
		}
	}

	// Ohne logf: kein Absturz, gleiches Ergebnis
	for _, tune := range []func(f *fastFS){nil, func(f *fastFS) { f.startErr = errors.New("x") }} {
		src := fastSource()
		if tune != nil {
			tune(src)
		}
		dst := &fastDst{memFS: newMemFS("/srv/b")}
		req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt"}}
		res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
		if len(res.failures) != 0 {
			t.Errorf("ohne logf: Fehler %v", res.failures)
		}
	}
}
