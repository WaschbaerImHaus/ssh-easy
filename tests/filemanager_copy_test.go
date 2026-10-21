// Paket main - Tests für das Kopieren im Datei-Manager (F5, Build 57)
//
// Prüft die Kopierlogik (runCopy) und den Ablauf in der Oberfläche:
//   - Dateien, Verzeichnisse rekursiv, Symlinks als Symlink
//   - Rechte und Änderungszeit werden übernommen
//   - Rückfrage bei vorhandenem Ziel: j/n/a/k/Esc
//   - Fehler einzelner Dateien brechen nicht ab, Teil-Dateien werden gelöscht
//   - Abbruch, gleiche Datei auf demselben Server, Typkonflikte
//   - Dialoge: Bestätigung, Fortschritt, Überschreiben; Bildschirmgröße
//
// Grundlage ist memFS, ein vollständiges Dateisystem im Speicher.
//
// @author Kurt Ingwer
// @date   2026-10-21 21:03
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- memFS: Dateisystem im Speicher ---

// memNode ist ein Eintrag von memFS.
type memNode struct {
	mode    fs.FileMode
	data    []byte
	target  string
	modTime time.Time
}

// memFS ist ein threadsicheres Dateisystem im Speicher.
type memFS struct {
	mu    sync.Mutex
	home  string
	nodes map[string]*memNode
	// Pfade ohne Leserecht (ReadDir/OpenRead schlagen fehl)
	denied map[string]bool
	// Verzeichnis -> Nummer der neu angelegten Datei (1 = erste), deren
	// Schreiben nach failAfter Bytes scheitert
	failNth   map[string]int
	created   map[string]int
	failAfter int
	// Wird bei jedem Schreibaufruf aufgerufen (für Abbruch-Tests)
	onWrite func()
	// Wird bei jedem Schreibaufruf mit Pfad und Knoten aufgerufen (unter Sperre)
	onWriteNode func(p string, n *memNode)
	// Chmod/Chtimes verbieten (eingeschränkter Hoster)
	noAttrs bool
	closed  bool
}

// newMemFS erzeugt ein leeres Dateisystem mit "/" und dem Home-Verzeichnis.
func newMemFS(home string) *memFS {
	m := &memFS{
		home:    home,
		nodes:   map[string]*memNode{"/": {mode: fs.ModeDir | 0o755}},
		denied:  map[string]bool{},
		failNth: map[string]int{},
		created: map[string]int{},
	}
	m.mkdirAll(home)
	return m
}

func (m *memFS) mkdirAll(p string) {
	if p == "/" || p == "" {
		return
	}
	m.mkdirAll(path.Dir(p))
	if _, ok := m.nodes[p]; !ok {
		m.nodes[p] = &memNode{mode: fs.ModeDir | 0o755, modTime: time.Unix(1_000_000, 0)}
	}
}

// addFile legt eine Datei samt Elternverzeichnissen an.
func (m *memFS) addFile(p, content string, mode fs.FileMode, mtime time.Time) {
	m.mkdirAll(path.Dir(p))
	m.nodes[p] = &memNode{mode: mode, data: []byte(content), modTime: mtime}
}

// addDir legt ein Verzeichnis an.
func (m *memFS) addDir(p string, mode fs.FileMode) {
	m.mkdirAll(p)
	m.nodes[p].mode = fs.ModeDir | mode
}

// addLink legt einen Symlink an.
func (m *memFS) addLink(p, target string) {
	m.mkdirAll(path.Dir(p))
	m.nodes[p] = &memNode{mode: fs.ModeSymlink | 0o777, target: target}
}

// content liefert den Inhalt einer Datei (oder "" wenn nicht vorhanden).
func (m *memFS) content(p string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.nodes[p]
	if !ok || n.mode&fs.ModeType != 0 {
		return "", false
	}
	return string(n.data), true
}

// node liefert einen Eintrag (nil wenn nicht vorhanden).
func (m *memFS) node(p string) *memNode {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nodes[p]
}

func (m *memFS) info(p string, n *memNode) os.FileInfo {
	size := int64(len(n.data))
	if n.mode&fs.ModeSymlink != 0 {
		size = int64(len(n.target))
	}
	return fakeFileInfo{name: path.Base(p), size: size, mode: n.mode, modTime: n.modTime}
}

func (m *memFS) Getwd() (string, error) { return m.home, nil }

func (m *memFS) ReadDir(p string) ([]os.FileInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.denied[p] {
		return nil, os.ErrPermission
	}
	n, ok := m.nodes[p]
	if !ok || !n.mode.IsDir() {
		return nil, os.ErrNotExist
	}
	var out []os.FileInfo
	for name, child := range m.nodes {
		if name != "/" && path.Dir(name) == p {
			out = append(out, m.info(name, child))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

func (m *memFS) Lstat(p string) (os.FileInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.nodes[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	return m.info(p, n), nil
}

func (m *memFS) Stat(p string) (os.FileInfo, error) {
	m.mu.Lock()
	n, ok := m.nodes[p]
	m.mu.Unlock()
	if !ok {
		return nil, os.ErrNotExist
	}
	if n.mode&fs.ModeSymlink != 0 {
		target := n.target
		if !path.IsAbs(target) {
			target = path.Join(path.Dir(p), target)
		}
		return m.Stat(target)
	}
	return m.Lstat(p)
}

func (m *memFS) ReadLink(p string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.nodes[p]
	if !ok || n.mode&fs.ModeSymlink == 0 {
		return "", os.ErrInvalid
	}
	return n.target, nil
}

func (m *memFS) OpenRead(p string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.denied[p] {
		return nil, os.ErrPermission
	}
	n, ok := m.nodes[p]
	if !ok || n.mode&fs.ModeType != 0 {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), n.data...))), nil
}

// memWriter schreibt in einen memFS-Knoten.
type memWriter struct {
	fs      *memFS
	p       string
	written int
	fail    bool
}

func (w *memWriter) Write(b []byte) (int, error) {
	if w.fs.onWrite != nil {
		w.fs.onWrite()
	}
	w.fs.mu.Lock()
	defer w.fs.mu.Unlock()
	if w.fs.onWriteNode != nil {
		w.fs.onWriteNode(w.p, w.fs.nodes[w.p])
	}
	if w.fail && w.written+len(b) > w.fs.failAfter {
		ok := w.fs.failAfter - w.written
		if ok > 0 {
			w.fs.nodes[w.p].data = append(w.fs.nodes[w.p].data, b[:ok]...)
			w.written += ok
		}
		return ok, errors.New("disk full")
	}
	w.fs.nodes[w.p].data = append(w.fs.nodes[w.p].data, b...)
	w.written += len(b)
	return len(b), nil
}

func (w *memWriter) Close() error { return nil }

// CreateExclusive legt eine neue Datei an; existiert der Pfad (auch als
// Symlink), schlägt es fehl. Neue Dateien bekommen wie bei einem echten
// Server die Standardrechte 0644 (umask 022).
func (m *memFS) CreateExclusive(p string) (io.WriteCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dir := path.Dir(p)
	parent, ok := m.nodes[dir]
	if !ok || !parent.mode.IsDir() {
		return nil, os.ErrNotExist
	}
	if m.denied[dir] {
		return nil, os.ErrPermission
	}
	if _, ok := m.nodes[p]; ok {
		return nil, os.ErrExist
	}
	m.nodes[p] = &memNode{mode: 0o644}
	m.created[dir]++
	return &memWriter{fs: m, p: p, fail: m.failNth[dir] == m.created[dir]}, nil
}

// RenameOverwrite benennt um und ersetzt dabei ein vorhandenes Ziel
// (Datei oder Symlink - der Symlink selbst wird ersetzt, nicht sein Ziel).
func (m *memFS) RenameOverwrite(oldPath, newPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.nodes[oldPath]
	if !ok {
		return os.ErrNotExist
	}
	if existing, ok := m.nodes[newPath]; ok && existing.mode.IsDir() {
		return os.ErrExist
	}
	m.nodes[newPath] = n
	delete(m.nodes, oldPath)
	return nil
}

func (m *memFS) Mkdir(p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.nodes[p]; ok {
		return os.ErrExist
	}
	if m.denied[path.Dir(p)] {
		return os.ErrPermission
	}
	if parent, ok := m.nodes[path.Dir(p)]; !ok || !parent.mode.IsDir() {
		return os.ErrNotExist
	}
	m.nodes[p] = &memNode{mode: fs.ModeDir | 0o755}
	return nil
}

func (m *memFS) Remove(p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.nodes[p]; !ok {
		return os.ErrNotExist
	}
	delete(m.nodes, p)
	return nil
}

func (m *memFS) Symlink(target, linkPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.nodes[linkPath]; ok {
		return os.ErrExist
	}
	m.nodes[linkPath] = &memNode{mode: fs.ModeSymlink | 0o777, target: target}
	return nil
}

func (m *memFS) Chmod(p string, mode os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.noAttrs {
		return os.ErrPermission
	}
	n, ok := m.nodes[p]
	if !ok {
		return os.ErrNotExist
	}
	n.mode = n.mode&fs.ModeType | mode.Perm()
	return nil
}

func (m *memFS) Chtimes(p string, atime, mtime time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.noAttrs {
		return os.ErrPermission
	}
	n, ok := m.nodes[p]
	if !ok {
		return os.ErrNotExist
	}
	n.modTime = mtime
	return nil
}

func (m *memFS) Close() error {
	m.closed = true
	return nil
}

// --- Hilfsfunktionen ---

var copyTestTime = time.Date(2025, 3, 4, 5, 6, 0, 0, time.UTC)

// newCopySource liefert die Quelle:
//
//	/home/a/projekt/          (0750)
//	/home/a/projekt/main.go   "package main" (0644)
//	/home/a/projekt/run.sh    "#!/bin/sh"    (0755)
//	/home/a/projekt/lib/x.go  "x"
//	/home/a/projekt/aktuell -> main.go (Symlink)
//	/home/a/notiz.txt         "hallo"
func newCopySource() *memFS {
	src := newMemFS("/home/a")
	src.addDir("/home/a/projekt", 0o750)
	src.nodes["/home/a/projekt"].modTime = copyTestTime
	src.addFile("/home/a/projekt/main.go", "package main", 0o644, copyTestTime)
	src.addFile("/home/a/projekt/run.sh", "#!/bin/sh", 0o755, copyTestTime)
	src.addFile("/home/a/projekt/lib/x.go", "x", 0o644, copyTestTime)
	src.addLink("/home/a/projekt/aktuell", "main.go")
	src.addFile("/home/a/notiz.txt", "hallo", 0o600, copyTestTime)
	return src
}

// runCopySync führt runCopy aus und beantwortet Rückfragen mit answer.
// Liefert Ergebnis und die gestellten Rückfragen.
func runCopySync(t *testing.T, ctx context.Context, req copyRequest, answer func(string) copyConflictAnswer) (copyResult, []string) {
	t.Helper()
	events := make(chan copyEvent, 16)
	answers := make(chan copyConflictAnswer, 1)
	go runCopy(ctx, req, events, answers)
	var conflicts []string
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.result != nil {
				return *ev.result, conflicts
			}
			if ev.conflict != "" {
				conflicts = append(conflicts, ev.conflict)
				answers <- answer(ev.conflict)
			}
		case <-timeout:
			t.Fatal("runCopy hängt")
		}
	}
}

func noConflicts(t *testing.T) func(string) copyConflictAnswer {
	return func(p string) copyConflictAnswer {
		t.Errorf("unerwartete Rückfrage für %s", p)
		return copyAnswerAbort
	}
}

// --- Kopierlogik ---

// TestCopyRecursiveTree prüft das rekursive Kopieren samt Symlink, Rechten
// und Änderungszeit.
func TestCopyRecursiveTree(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt", "notiz.txt"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))

	if len(res.failures) != 0 || res.cancelled {
		t.Fatalf("Fehler %v, abgebrochen %v", res.failures, res.cancelled)
	}
	assertNoTempFiles(t, dst)
	for p, want := range map[string]string{
		"/srv/b/projekt/main.go":  "package main",
		"/srv/b/projekt/run.sh":   "#!/bin/sh",
		"/srv/b/projekt/lib/x.go": "x",
		"/srv/b/notiz.txt":        "hallo",
	} {
		if got, ok := dst.content(p); !ok || got != want {
			t.Errorf("%s = %q/%v, erwartet %q", p, got, ok, want)
		}
	}
	link := dst.node("/srv/b/projekt/aktuell")
	if link == nil || link.mode&fs.ModeSymlink == 0 || link.target != "main.go" {
		t.Errorf("Symlink nicht als Symlink kopiert: %+v", link)
	}
	if n := dst.node("/srv/b/projekt/run.sh"); n.mode.Perm() != 0o755 || !n.modTime.Equal(copyTestTime) {
		t.Errorf("run.sh: Rechte %v / Zeit %v nicht übernommen", n.mode.Perm(), n.modTime)
	}
	if n := dst.node("/srv/b/notiz.txt"); n.mode.Perm() != 0o600 {
		t.Errorf("notiz.txt: Rechte %v, erwartet 0600", n.mode.Perm())
	}
	if n := dst.node("/srv/b/projekt"); !n.mode.IsDir() || n.mode.Perm() != 0o750 || !n.modTime.Equal(copyTestTime) {
		t.Errorf("Verzeichnis: %v / %v", n.mode, n.modTime)
	}
	if res.files != 5 || res.bytes != int64(len("package main#!/bin/shxhallo")) {
		t.Errorf("Zähler: %d Dateien, %d Bytes", res.files, res.bytes)
	}
	if strings.Join(res.copiedNames, ",") != "projekt,notiz.txt" {
		t.Errorf("kopierte Namen = %v", res.copiedNames)
	}
}

// TestCopyIgnoresAttributeErrors - Hoster verbietet chmod/chtimes: Daten
// werden trotzdem kopiert, kein Fehler.
func TestCopyIgnoresAttributeErrors(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	dst.noAttrs = true
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 {
		t.Errorf("Fehler: %v", res.failures)
	}
	if got, _ := dst.content("/srv/b/projekt/lib/x.go"); got != "x" {
		t.Error("Daten fehlen")
	}
}

// TestCopyMergesExistingDirectory - vorhandenes Zielverzeichnis wird
// zusammengeführt, fremde Dateien darin bleiben.
func TestCopyMergesExistingDirectory(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	dst.addFile("/srv/b/projekt/eigen.txt", "bleibt", 0o644, copyTestTime)
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 {
		t.Fatalf("Fehler: %v", res.failures)
	}
	if got, _ := dst.content("/srv/b/projekt/eigen.txt"); got != "bleibt" {
		t.Error("vorhandene Datei im Zielverzeichnis verändert")
	}
	if got, _ := dst.content("/srv/b/projekt/main.go"); got != "package main" {
		t.Error("main.go nicht kopiert")
	}
}

// TestCopyConflictAnswers prüft j / n / a / k bei vorhandenen Dateien.
func TestCopyConflictAnswers(t *testing.T) {
	setup := func() (*memFS, *memFS, copyRequest) {
		src, dst := newCopySource(), newMemFS("/srv/b")
		dst.addFile("/srv/b/projekt/main.go", "alt1", 0o644, copyTestTime)
		dst.addFile("/srv/b/projekt/run.sh", "alt2", 0o644, copyTestTime)
		return src, dst, copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt"}}
	}
	cases := []struct {
		name      string
		answers   []copyConflictAnswer
		questions int
		main, run string
	}{
		{"ja", []copyConflictAnswer{copyAnswerOverwrite, copyAnswerOverwrite}, 2, "package main", "#!/bin/sh"},
		{"nein", []copyConflictAnswer{copyAnswerSkip, copyAnswerSkip}, 2, "alt1", "alt2"},
		{"gemischt", []copyConflictAnswer{copyAnswerSkip, copyAnswerOverwrite}, 2, "alt1", "#!/bin/sh"},
		{"alle", []copyConflictAnswer{copyAnswerOverwriteAll}, 1, "package main", "#!/bin/sh"},
		{"keine", []copyConflictAnswer{copyAnswerSkipAll}, 1, "alt1", "alt2"},
	}
	for _, c := range cases {
		_, dst, req := setup()
		i := 0
		res, questions := runCopySync(t, context.Background(), req, func(string) copyConflictAnswer {
			a := c.answers[i]
			i++
			return a
		})
		if len(questions) != c.questions {
			t.Errorf("%s: %d Rückfragen %v, erwartet %d", c.name, len(questions), questions, c.questions)
		}
		if got, _ := dst.content("/srv/b/projekt/main.go"); got != c.main {
			t.Errorf("%s: main.go = %q, erwartet %q", c.name, got, c.main)
		}
		if got, _ := dst.content("/srv/b/projekt/run.sh"); got != c.run {
			t.Errorf("%s: run.sh = %q, erwartet %q", c.name, got, c.run)
		}
		// Neue Dateien werden immer kopiert
		if got, _ := dst.content("/srv/b/projekt/lib/x.go"); got != "x" {
			t.Errorf("%s: lib/x.go fehlt", c.name)
		}
		// Übersprungene Einträge gelten nicht als vollständig kopiert
		skipped := c.main == "alt1" || c.run == "alt2"
		if skipped == (len(res.copiedNames) == 1) {
			t.Errorf("%s: kopierte Namen %v (übersprungen: %v)", c.name, res.copiedNames, skipped)
		}
	}
}

// TestCopyConflictPolicies prüft "alle, wenn neuer" und "alle, wenn größer":
// Die Regel gilt für die aktuelle und alle folgenden Rückfragen.
func TestCopyConflictPolicies(t *testing.T) {
	older := copyTestTime.Add(-time.Hour)
	newer := copyTestTime.Add(time.Hour)
	cases := []struct {
		name      string
		answer    copyConflictAnswer
		main, run string
	}{
		// main.go: Ziel älter + kleiner -> überschreiben; run.sh: Ziel neuer + größer -> behalten
		{"neuer", copyAnswerOverwriteNewer, "package main", "langer alter Inhalt"},
		{"größer", copyAnswerOverwriteLarger, "package main", "langer alter Inhalt"},
	}
	for _, c := range cases {
		src, dst := newCopySource(), newMemFS("/srv/b")
		dst.addFile("/srv/b/projekt/main.go", "alt", 0o644, older)
		dst.addFile("/srv/b/projekt/run.sh", "langer alter Inhalt", 0o644, newer)
		req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt"}}
		res, questions := runCopySync(t, context.Background(), req, func(string) copyConflictAnswer { return c.answer })
		if len(questions) != 1 {
			t.Errorf("%s: %d Rückfragen, erwartet 1 (Regel gilt für alle)", c.name, len(questions))
		}
		if got, _ := dst.content("/srv/b/projekt/main.go"); got != c.main {
			t.Errorf("%s: main.go = %q, erwartet %q", c.name, got, c.main)
		}
		if got, _ := dst.content("/srv/b/projekt/run.sh"); got != c.run {
			t.Errorf("%s: run.sh = %q, erwartet %q", c.name, got, c.run)
		}
		if len(res.failures) != 0 {
			t.Errorf("%s: Fehler %v", c.name, res.failures)
		}
	}
}

// TestCopyPolicyEqualNotOverwritten - gleich alt bzw. gleich groß gilt nicht
// als "neuer"/"größer" (Zeitvergleich sekundengenau wie bei SFTP).
func TestCopyPolicyEqualNotOverwritten(t *testing.T) {
	for _, answer := range []copyConflictAnswer{copyAnswerOverwriteNewer, copyAnswerOverwriteLarger} {
		src, dst := newMemFS("/home/a"), newMemFS("/srv/b")
		src.addFile("/home/a/f.txt", "abc", 0o644, copyTestTime.Add(400*time.Millisecond))
		dst.addFile("/srv/b/f.txt", "xyz", 0o644, copyTestTime)
		req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"f.txt"}}
		runCopySync(t, context.Background(), req, func(string) copyConflictAnswer { return answer })
		if got, _ := dst.content("/srv/b/f.txt"); got != "xyz" {
			t.Errorf("Antwort %v: gleich alte/große Datei überschrieben", answer)
		}
	}
}

// TestCopyConflictEventDetails - die Rückfrage liefert Größe und Zeit von
// Quelle und Ziel.
func TestCopyConflictEventDetails(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	older := copyTestTime.Add(-time.Hour)
	dst.addFile("/srv/b/notiz.txt", "alt!!!!", 0o644, older)
	events := make(chan copyEvent, 16)
	answers := make(chan copyConflictAnswer, 1)
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"notiz.txt"}}
	go runCopy(context.Background(), req, events, answers)
	for ev := range events {
		if ev.conflict == "" {
			continue
		}
		info := ev.conflictInfo
		if info.srcSize != 5 || info.dstSize != 7 || !info.srcTime.Equal(copyTestTime) || !info.dstTime.Equal(older) {
			t.Errorf("Details = %+v", info)
		}
		answers <- copyAnswerSkip
		break
	}
	for ev := range events {
		if ev.result != nil {
			break
		}
	}
}

// TestCopyConflictAbort - Esc in der Rückfrage bricht den Vorgang ab.
func TestCopyConflictAbort(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	dst.addFile("/srv/b/projekt/main.go", "alt", 0o644, copyTestTime)
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt", "notiz.txt"}}
	res, _ := runCopySync(t, context.Background(), req, func(string) copyConflictAnswer { return copyAnswerAbort })
	if !res.cancelled {
		t.Error("nicht als abgebrochen gemeldet")
	}
	if got, _ := dst.content("/srv/b/projekt/main.go"); got != "alt" {
		t.Error("Datei trotz Abbruch überschrieben")
	}
	if _, ok := dst.content("/srv/b/notiz.txt"); ok {
		t.Error("nach dem Abbruch wurde weiterkopiert")
	}
}

// TestCopyOverwriteSymlinkTarget - steht am Ziel ein Symlink, wird er beim
// Überschreiben ersetzt und NICHT durch ihn hindurch geschrieben.
func TestCopyOverwriteSymlinkTarget(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	dst.addFile("/etc/wichtig", "geheim", 0o644, copyTestTime)
	dst.addLink("/srv/b/notiz.txt", "/etc/wichtig")
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"notiz.txt"}}
	res, questions := runCopySync(t, context.Background(), req, func(string) copyConflictAnswer { return copyAnswerOverwrite })
	if len(questions) != 1 || len(res.failures) != 0 {
		t.Fatalf("Rückfragen %v, Fehler %v", questions, res.failures)
	}
	if got, _ := dst.content("/etc/wichtig"); got != "geheim" {
		t.Error("Symlink-Ziel wurde überschrieben")
	}
	if got, ok := dst.content("/srv/b/notiz.txt"); !ok || got != "hallo" {
		t.Errorf("notiz.txt = %q/%v", got, ok)
	}
}

// TestCopyTypeMismatch - Datei auf Verzeichnis bzw. Verzeichnis auf Datei:
// Fehler, kein Überschreiben, Inhalt des Verzeichnisses wird übersprungen.
func TestCopyTypeMismatch(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	dst.addFile("/srv/b/projekt", "ich bin eine Datei", 0o644, copyTestTime)
	dst.addDir("/srv/b/notiz.txt", 0o755)
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt", "notiz.txt"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 2 {
		t.Fatalf("Fehler = %v, erwartet 2", res.failures)
	}
	for _, f := range res.failures {
		if !errors.Is(f.err, errTargetTypeMismatch) {
			t.Errorf("%s: Fehler %v, erwartet Typkonflikt", f.path, f.err)
		}
	}
	if got, _ := dst.content("/srv/b/projekt"); got != "ich bin eine Datei" {
		t.Error("Datei durch Verzeichnis ersetzt")
	}
	if len(res.copiedNames) != 0 {
		t.Errorf("kopierte Namen = %v, erwartet keine", res.copiedNames)
	}
}

// TestCopyContinuesAfterErrors - unlesbares Unterverzeichnis und
// Schreibfehler: übrige Dateien werden kopiert, Teil-Datei gelöscht.
func TestCopyContinuesAfterErrors(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	src.denied["/home/a/projekt/lib"] = true
	dst.failNth["/srv/b/projekt"] = 1 // erste neue Datei dort ist main.go
	dst.failAfter = 3
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt", "notiz.txt"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 2 {
		t.Fatalf("Fehler = %v, erwartet 2 (lib, main.go)", res.failures)
	}
	if _, ok := dst.content("/srv/b/projekt/main.go"); ok {
		t.Error("halb geschriebene Datei nicht gelöscht")
	}
	assertNoTempFiles(t, dst)
	if got, _ := dst.content("/srv/b/projekt/run.sh"); got != "#!/bin/sh" {
		t.Error("run.sh fehlt")
	}
	if got, _ := dst.content("/srv/b/notiz.txt"); got != "hallo" {
		t.Error("notiz.txt fehlt")
	}
	if strings.Join(res.copiedNames, ",") != "notiz.txt" {
		t.Errorf("kopierte Namen = %v, erwartet nur notiz.txt", res.copiedNames)
	}
}

// TestCopyCancelRemovesPartialFile - Abbruch mitten in einer Datei.
func TestCopyCancelRemovesPartialFile(t *testing.T) {
	src, dst := newMemFS("/home/a"), newMemFS("/srv/b")
	src.addFile("/home/a/gross.bin", strings.Repeat("x", 3<<20), 0o644, copyTestTime)
	src.addFile("/home/a/danach.txt", "später", 0o644, copyTestTime)
	ctx, cancel := context.WithCancel(context.Background())
	dst.onWrite = cancel // beim ersten Schreiben abbrechen
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"gross.bin", "danach.txt"}}
	res, _ := runCopySync(t, ctx, req, noConflicts(t))
	if !res.cancelled {
		t.Error("nicht als abgebrochen gemeldet")
	}
	if _, ok := dst.content("/srv/b/gross.bin"); ok {
		t.Error("Teil-Datei nach Abbruch nicht gelöscht")
	}
	assertNoTempFiles(t, dst)
	if _, ok := dst.content("/srv/b/danach.txt"); ok {
		t.Error("nach Abbruch weiterkopiert")
	}
}

// TestCopySameFileOnSameHost - gleicher Server, gleicher Pfad: nicht auf
// sich selbst kopieren (würde die Quelle leeren).
func TestCopySameFileOnSameHost(t *testing.T) {
	src := newCopySource()
	req := copyRequest{src: src, dst: src, srcDir: "/home/a", dstDir: "/home/a", names: []string{"notiz.txt"}, sameHost: true}
	res, _ := runCopySync(t, context.Background(), req, func(string) copyConflictAnswer { return copyAnswerOverwrite })
	if len(res.failures) != 1 || !errors.Is(res.failures[0].err, errSameFile) {
		t.Errorf("Fehler = %v, erwartet errSameFile", res.failures)
	}
	if got, _ := src.content("/home/a/notiz.txt"); got != "hallo" {
		t.Errorf("Quelle verändert: %q", got)
	}
}

// TestCopyProgressEvents - Fortschritt meldet Gesamtzahlen.
func TestCopyProgressEvents(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	events := make(chan copyEvent, 64)
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt"}}
	go runCopy(context.Background(), req, events, make(chan copyConflictAnswer))
	var last *copyProgress
	for ev := range events {
		if ev.progress != nil {
			p := *ev.progress
			last = &p
		}
		if ev.result != nil {
			break
		}
	}
	if last == nil {
		t.Fatal("kein Fortschritt gemeldet")
	}
	if last.totalFiles != 4 || last.totalBytes != int64(len("package main#!/bin/shx")) {
		t.Errorf("Gesamt: %d Dateien, %d Bytes", last.totalFiles, last.totalBytes)
	}
}

// --- Oberfläche ---

// openCopyManager öffnet den Datei-Manager mit zwei memFS (links Quelle,
// rechts Ziel) auf verschiedenen Hosts.
func openCopyManager(t *testing.T, src, dst *memFS) AppModel {
	t.Helper()
	m := newListTestModel(t, "Links", "Rechts")
	m.connections[1].Host = "10.0.0.2"
	m.fmConnect = func(conn Connection, password string) (remoteFS, error) {
		if conn.Name == "Links" {
			return src, nil
		}
		return dst, nil
	}
	m.termWidth, m.termHeight = 100, 30
	m = openPick(t, m)
	m = pickSide(t, m, "Links")
	m = pickSide(t, m, "Rechts")
	if m.state != ViewFileManager {
		t.Fatalf("state = %v (%q)", m.state, m.fm.message)
	}
	return m
}

// TestFileManagerF5NothingSelected - Cursor auf "..", nichts markiert.
func TestFileManagerF5NothingSelected(t *testing.T) {
	m := openCopyManager(t, newCopySource(), newMemFS("/srv/b"))
	m = cursorTo(t, m, "..")
	m = pressRun(t, m, "f5")
	if m.fm.dialog != fmDialogNone {
		t.Error("Dialog geöffnet, obwohl nichts gewählt ist")
	}
	if m.fm.message != m.tr(txtFMNothingSelected) {
		t.Errorf("Meldung = %q", m.fm.message)
	}
}

// TestFileManagerF5ConfirmAndCancel - Bestätigung erscheint, Esc/q brechen ab.
func TestFileManagerF5ConfirmAndCancel(t *testing.T) {
	for _, key := range []string{"esc", "q"} {
		src, dst := newCopySource(), newMemFS("/srv/b")
		m := openCopyManager(t, src, dst)
		m = cursorTo(t, m, "notiz.txt")
		m = pressRun(t, m, "f5")
		if m.fm.dialog != fmDialogCopyConfirm {
			t.Fatalf("%s: Dialog = %v, erwartet Bestätigung", key, m.fm.dialog)
		}
		text := stripANSI(m.View())
		if !strings.Contains(text, "notiz.txt") || !strings.Contains(text, "10.0.0.2:/srv/b") {
			t.Errorf("%s: Bestätigung ohne Quelle/Ziel:\n%s", key, text)
		}
		assertScreenSize(t, m)
		m = pressRun(t, m, key)
		if m.fm.dialog != fmDialogNone || m.state != ViewFileManager {
			t.Errorf("%s: Dialog %v / state %v nach Abbruch", key, m.fm.dialog, m.state)
		}
		if _, ok := dst.content("/srv/b/notiz.txt"); ok {
			t.Errorf("%s: trotz Abbruch kopiert", key)
		}
	}
}

// TestFileManagerF5CopiesMarked - markierte Einträge werden kopiert, das
// Zielfenster neu gelesen, die Markierungen der Quelle aufgehoben.
func TestFileManagerF5CopiesMarked(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	m := openCopyManager(t, src, dst)
	m = cursorTo(t, m, "projekt")
	m = pressRun(t, m, "insert")
	m = cursorTo(t, m, "notiz.txt")
	m = pressRun(t, m, "insert")
	m = pressRun(t, m, "f5")
	if !strings.Contains(stripANSI(m.View()), "2") {
		t.Error("Bestätigung nennt die Anzahl nicht")
	}
	m = pressRun(t, m, "enter")
	if m.fm.dialog != fmDialogNone {
		t.Fatalf("Dialog nach dem Kopieren noch offen: %v", m.fm.dialog)
	}
	if got, _ := dst.content("/srv/b/projekt/lib/x.go"); got != "x" {
		t.Error("rekursiv nicht kopiert")
	}
	if names := strings.Join(entryNames(m.fm.panels[1]), ","); !strings.Contains(names, "projekt") || !strings.Contains(names, "notiz.txt") {
		t.Errorf("Zielfenster nicht neu gelesen: %s", names)
	}
	if got := markedNames(m.fm.panels[0]); len(got) != 0 {
		t.Errorf("Markierungen nach dem Kopieren: %v", got)
	}
	if want := fmt.Sprintf(m.tr(txtFMCopyDone), 5, formatSizeWithUnit(27)); m.fm.message != want {
		t.Errorf("Meldung = %q, erwartet %q", m.fm.message, want)
	}
}

// TestFileManagerF5CopiesCursorEntryWithoutMarks - ohne Markierung wird der
// Eintrag unter dem Cursor kopiert.
func TestFileManagerF5CopiesCursorEntryWithoutMarks(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	m := openCopyManager(t, src, dst)
	m = cursorTo(t, m, "notiz.txt")
	m = pressRun(t, m, "f5")
	m = pressRun(t, m, "enter")
	if got, _ := dst.content("/srv/b/notiz.txt"); got != "hallo" {
		t.Error("notiz.txt nicht kopiert")
	}
	if _, ok := dst.content("/srv/b/projekt/main.go"); ok {
		t.Error("nicht gewählter Eintrag kopiert")
	}
}

// TestFileManagerF5FromRightToLeft - Richtung folgt dem aktiven Fenster.
func TestFileManagerF5FromRightToLeft(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	dst.addFile("/srv/b/zurueck.txt", "retour", 0o644, copyTestTime)
	m := openCopyManager(t, src, dst)
	m = pressRun(t, m, "tab")
	m = cursorTo(t, m, "zurueck.txt")
	m = pressRun(t, m, "f5")
	m = pressRun(t, m, "enter")
	if got, _ := src.content("/home/a/zurueck.txt"); got != "retour" {
		t.Error("nicht nach links kopiert")
	}
}

// TestFileManagerOverwriteDialogKeys - Rückfrage im Dialog: j / n / a / k.
func TestFileManagerOverwriteDialogKeys(t *testing.T) {
	cases := []struct {
		keys      []string
		main, run string
	}{
		{[]string{"j", "j"}, "package main", "#!/bin/sh"},
		{[]string{"y", "n"}, "package main", "alt2"},
		{[]string{"n", "n"}, "alt1", "alt2"},
		{[]string{"a"}, "package main", "#!/bin/sh"},
		{[]string{"k"}, "alt1", "alt2"},
		// main.go Ziel älter, run.sh Ziel neuer (siehe unten)
		{[]string{"u"}, "package main", "alt2"},
		// main.go Ziel kleiner ("alt1" < 12 Bytes), run.sh Ziel größer
		{[]string{"g"}, "package main", "alt2"},
	}
	for _, c := range cases {
		src, dst := newCopySource(), newMemFS("/srv/b")
		dst.addFile("/srv/b/projekt/main.go", "alt1", 0o644, copyTestTime.Add(-time.Hour))
		dst.addFile("/srv/b/projekt/run.sh", "alt2", 0o644, copyTestTime.Add(time.Hour))
		// run.sh im Ziel größer machen als die Quelle (9 Bytes)
		dst.nodes["/srv/b/projekt/run.sh"].data = []byte("alt2" + strings.Repeat(" ", 20))
		m := openCopyManager(t, src, dst)
		m = cursorTo(t, m, "projekt")
		m = pressRun(t, m, "f5")
		m = pressRun(t, m, "enter")
		for _, key := range c.keys {
			if m.fm.dialog != fmDialogOverwrite {
				t.Fatalf("%v: keine Rückfrage vor %q (Dialog %v)", c.keys, key, m.fm.dialog)
			}
			if !strings.Contains(stripANSI(m.View()), "/srv/b/projekt/") {
				t.Errorf("%v: Rückfrage nennt die Datei nicht", c.keys)
			}
			assertScreenSize(t, m)
			// Andere Tasten werden ignoriert
			m = pressRun(t, m, "x")
			if m.fm.dialog != fmDialogOverwrite {
				t.Fatalf("%v: fremde Taste schließt die Rückfrage", c.keys)
			}
			m = pressRun(t, m, key)
		}
		if m.fm.dialog != fmDialogNone {
			t.Fatalf("%v: Dialog %v nach den Antworten", c.keys, m.fm.dialog)
		}
		if got, _ := dst.content("/srv/b/projekt/main.go"); got != c.main {
			t.Errorf("%v: main.go = %q, erwartet %q", c.keys, got, c.main)
		}
		if got, _ := dst.content("/srv/b/projekt/run.sh"); strings.TrimSpace(got) != c.run {
			t.Errorf("%v: run.sh = %q, erwartet %q", c.keys, got, c.run)
		}
	}
}

// TestFileManagerOverwriteDialogShowsDetails - Rückfrage zeigt Größe und
// Datum von Quelle und Ziel sowie die Tasten u und g.
func TestFileManagerOverwriteDialogShowsDetails(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	dst.addFile("/srv/b/notiz.txt", "alt!!!!", 0o644, copyTestTime.Add(-time.Hour))
	m := openCopyManager(t, src, dst)
	m = cursorTo(t, m, "notiz.txt")
	m = pressRun(t, m, "f5")
	m = pressRun(t, m, "enter")
	text := stripANSI(m.View())
	for _, want := range []string{
		m.tr(txtFMSource), m.tr(txtFMTarget), "5 B", "7 B",
		copyTestTime.Local().Format(fmDateFormat), copyTestTime.Add(-time.Hour).Local().Format(fmDateFormat),
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Rückfrage ohne %q:\n%s", want, text)
		}
	}
	m = pressRun(t, m, "n")
}

// TestFileManagerOverwriteDialogEscAborts - Esc in der Rückfrage bricht ab.
func TestFileManagerOverwriteDialogEscAborts(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	dst.addFile("/srv/b/projekt/main.go", "alt", 0o644, copyTestTime)
	m := openCopyManager(t, src, dst)
	m = cursorTo(t, m, "projekt")
	m = pressRun(t, m, "f5")
	m = pressRun(t, m, "enter")
	m = pressRun(t, m, "esc")
	if m.fm.dialog != fmDialogNone {
		t.Fatalf("Dialog %v nach Esc", m.fm.dialog)
	}
	if m.fm.message != m.tr(txtFMCopyCancelled) {
		t.Errorf("Meldung = %q", m.fm.message)
	}
	if got, _ := dst.content("/srv/b/projekt/main.go"); got != "alt" {
		t.Error("trotz Abbruch überschrieben")
	}
}

// TestFileManagerCopyProgressDialog - Fortschrittsdialog: Esc bricht ab,
// F10 wird während des Kopierens ignoriert, Bildschirmgröße bleibt exakt.
func TestFileManagerCopyProgressDialog(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	m := openCopyManager(t, src, dst)
	m = cursorTo(t, m, "notiz.txt")
	m = pressRun(t, m, "f5")
	// Kopieren starten, aber Ereignisse noch nicht abholen
	updated, cmd := m.Update(keyMsgFor("enter"))
	m = updated.(AppModel)
	if m.fm.dialog != fmDialogCopying || cmd == nil {
		t.Fatalf("Dialog %v, Kommando %v - erwartet Fortschritt", m.fm.dialog, cmd != nil)
	}
	assertScreenSize(t, m)
	updated, _ = m.Update(fmKey("f10"))
	m = updated.(AppModel)
	if m.state != ViewFileManager || m.fm.dialog != fmDialogCopying {
		t.Error("F10 beendet den Datei-Manager während des Kopierens")
	}
	m = runCmd(t, m, cmd)
	if m.fm.dialog != fmDialogNone {
		t.Errorf("Dialog %v nach Ende", m.fm.dialog)
	}
}

// TestFileManagerCopyErrorMessage - Fehler werden am Ende gemeldet.
func TestFileManagerCopyErrorMessage(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	src.denied["/home/a/projekt/lib"] = true
	m := openCopyManager(t, src, dst)
	m = cursorTo(t, m, "projekt")
	m = pressRun(t, m, "f5")
	m = pressRun(t, m, "enter")
	if !strings.Contains(m.fm.message, "/home/a/projekt/lib") {
		t.Errorf("Meldung ohne fehlerhaften Pfad: %q", m.fm.message)
	}
	prefix := strings.SplitN(m.tr(txtFMCopyErrors), "%", 2)[0]
	if !strings.HasPrefix(m.fm.message, prefix) {
		t.Errorf("Meldung = %q, erwartet Fehlertext", m.fm.message)
	}
	// Teilweise kopiert -> Markierung bleibt bei Fehlern erhalten
	if got, _ := dst.content("/srv/b/projekt/main.go"); got != "package main" {
		t.Error("übrige Dateien nicht kopiert")
	}
}

// TestFileManagerCopyTypeMismatchMessage - Typkonflikt mit Übersetzung.
func TestFileManagerCopyTypeMismatchMessage(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	dst.addDir("/srv/b/notiz.txt", 0o755)
	m := openCopyManager(t, src, dst)
	m = cursorTo(t, m, "notiz.txt")
	m = pressRun(t, m, "f5")
	m = pressRun(t, m, "enter")
	want := fmt.Sprintf(m.tr(txtFMErrTargetType), "/srv/b/notiz.txt")
	if !strings.Contains(m.fm.message, want) {
		t.Errorf("Meldung = %q, erwartet %q", m.fm.message, want)
	}
}

// TestFileManagerCopySanitizesNames - Dateinamen im Dialog bereinigt.
func TestFileManagerCopySanitizesNames(t *testing.T) {
	src, dst := newMemFS("/home/a"), newMemFS("/srv/b")
	src.addFile("/home/a/böse\x1b[2J", "x", 0o644, copyTestTime)
	dst.addFile("/srv/b/böse\x1b[2J", "y", 0o644, copyTestTime)
	m := openCopyManager(t, src, dst)
	m = pressRun(t, m, "end")
	m = pressRun(t, m, "f5")
	if strings.Contains(m.View(), "\x1b[2J") {
		t.Error("Steuersequenz im Bestätigungsdialog")
	}
	// Namen mit Steuerzeichen werden seit Build 58 gar nicht kopiert
	m = pressRun(t, m, "enter")
	if m.fm.dialog != fmDialogNone {
		t.Fatalf("Dialog %v, erwartet Ende mit Fehlermeldung", m.fm.dialog)
	}
	if strings.Contains(m.View(), "\x1b[2J") || strings.Contains(m.fm.message, "\x1b") {
		t.Error("Steuersequenz in der Fehlermeldung")
	}
	if !strings.Contains(m.fm.message, fmt.Sprintf(m.tr(txtFMErrUnsafeName), "böse?[2J")) {
		t.Errorf("Meldung = %q, erwartet unzulässigen Namen", m.fm.message)
	}
	if got, _ := dst.content("/srv/b/böse\x1b[2J"); got != "y" {
		t.Error("Ziel trotz unzulässigem Namen verändert")
	}
}

// TestFileManagerDialogsAllThemesAndSizes - Dialoge passen in jede Größe.
func TestFileManagerDialogsAllThemesAndSizes(t *testing.T) {
	withColorProfile(t)
	src, dst := newCopySource(), newMemFS("/srv/b")
	m := openCopyManager(t, src, dst)
	m = cursorTo(t, m, "notiz.txt")
	m = pressRun(t, m, "f5")
	for theme := range fmThemes {
		m.fm.theme = theme
		for _, size := range [][2]int{{100, 30}, {80, 24}, {40, 12}, {200, 60}} {
			m.termWidth, m.termHeight = size[0], size[1]
			assertScreenSize(t, m)
		}
	}
	m.fm.dialog = fmDialogCopying
	m.fm.copy.copyProgress = copyProgress{current: "/home/a/" + strings.Repeat("lang", 40), doneFiles: 3, totalFiles: 7, doneBytes: 300, totalBytes: 700}
	for _, size := range [][2]int{{100, 30}, {80, 24}, {40, 12}} {
		m.termWidth, m.termHeight = size[0], size[1]
		assertScreenSize(t, m)
	}
	m.fm.dialog = fmDialogOverwrite
	m.fm.copy.conflictPath = "/srv/b/" + strings.Repeat("x", 300)
	m.termWidth, m.termHeight = 80, 24
	assertScreenSize(t, m)
}

// TestProgressBar prüft den Fortschrittsbalken.
func TestProgressBar(t *testing.T) {
	cases := []struct {
		done, total int64
		width       int
		want        string
	}{
		{0, 100, 10, "░░░░░░░░░░"},
		{50, 100, 10, "█████░░░░░"},
		{100, 100, 10, "██████████"},
		{150, 100, 4, "████"},
		{0, 0, 4, "████"},
		{5, 10, 0, ""},
	}
	for _, c := range cases {
		if got := progressBar(c.done, c.total, c.width); got != c.want {
			t.Errorf("progressBar(%d,%d,%d) = %q, erwartet %q", c.done, c.total, c.width, got, c.want)
		}
	}
}

// evilFS liefert in ReadDir zusätzlich gefährliche Namen, wie es ein
// bösartiger Server tun könnte.
type evilFS struct {
	*memFS
	names []string
}

func (e *evilFS) ReadDir(p string) ([]os.FileInfo, error) {
	infos, err := e.memFS.ReadDir(p)
	for _, name := range e.names {
		infos = append(infos, fakeFileInfo{name: name, mode: 0o644, size: 1})
	}
	return infos, err
}

func (e *evilFS) Lstat(p string) (os.FileInfo, error) {
	for _, name := range e.names {
		if strings.HasSuffix(p, "/"+name) {
			return fakeFileInfo{name: name, mode: 0o644, size: 1}, nil
		}
	}
	return e.memFS.Lstat(p)
}

// TestCopyRejectsUnsafeNames - Namen mit "/" oder ".." aus einem
// Verzeichnis der Quelle dürfen am Ziel nicht aus dem Zielverzeichnis
// hinausführen (Path Traversal durch bösartigen Server).
func TestCopyRejectsUnsafeNames(t *testing.T) {
	src := &evilFS{memFS: newCopySource(), names: []string{"../../../etc/boese", "..", ".", "a/b"}}
	dst := newMemFS("/srv/b")
	dst.addDir("/etc", 0o755)
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	for p := range dst.nodes {
		if p != "/" && p != "/srv" && p != "/srv/b" && p != "/etc" && !strings.HasPrefix(p, "/srv/b/") {
			t.Errorf("außerhalb des Ziels geschrieben: %s", p)
		}
	}
	unsafe := 0
	for _, f := range res.failures {
		if errors.Is(f.err, errUnsafeName) {
			unsafe++
		}
	}
	// evilFS liefert die Namen in jedem Verzeichnis (projekt und lib):
	// je "../../../etc/boese" und "a/b"; "." und ".." werden still übersprungen
	if unsafe != 4 {
		t.Errorf("unsichere Namen gemeldet: %d (%v), erwartet 4", unsafe, res.failures)
	}
	// Auch oberste Namen werden geprüft
	req.names = []string{"../srv"}
	res, _ = runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 1 || !errors.Is(res.failures[0].err, errUnsafeName) {
		t.Errorf("oberster Name ../srv: %v", res.failures)
	}
}

// TestReadPanelDirSkipsUnsafeNames - Namen mit "/" erscheinen gar nicht
// erst in der Liste.
func TestReadPanelDirSkipsUnsafeNames(t *testing.T) {
	src := &evilFS{memFS: newCopySource(), names: []string{"../../etc", "a/b"}}
	entries, err := readPanelDir(src, "/home/a")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.name, "/") {
			t.Errorf("unsicherer Name in der Liste: %q", e.name)
		}
	}
}

// --- Sicherheit (Build 58) ---

// assertNoTempFiles prüft, dass keine Temp-Dateien liegen geblieben sind.
func assertNoTempFiles(t *testing.T, m *memFS) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for p := range m.nodes {
		if strings.Contains(path.Base(p), copyTempPrefix) {
			t.Errorf("Temp-Datei liegen geblieben: %s", p)
		}
	}
}

// TestCopyPrivateFileNeverReadable - eine private Datei (0600) ist auf dem
// Ziel zu keinem Zeitpunkt für Gruppe/Andere lesbar, auch nicht während des
// Schreibens; neue Verzeichnisse sind währenddessen 0700.
func TestCopyPrivateFileNeverReadable(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	src.addFile("/home/a/projekt/geheim.key", strings.Repeat("k", 100), 0o600, copyTestTime)
	writes := 0
	dst.onWriteNode = func(p string, n *memNode) {
		writes++
		if strings.HasPrefix(p, "/srv/b/projekt/") {
			if dir := dst.nodes["/srv/b/projekt"]; dir.mode.Perm() != 0o700 {
				t.Errorf("neues Verzeichnis während des Kopierens %v, erwartet 0700", dir.mode.Perm())
			}
		}
		if n.mode.Perm()&0o077 != 0 {
			t.Errorf("%s während des Schreibens mit Rechten %v", p, n.mode.Perm())
		}
	}
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt", "notiz.txt"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 || writes == 0 {
		t.Fatalf("Fehler %v, Schreibaufrufe %d", res.failures, writes)
	}
	if n := dst.node("/srv/b/projekt/geheim.key"); n == nil || n.mode.Perm() != 0o600 {
		t.Errorf("geheim.key am Ende: %+v", n)
	}
	if n := dst.node("/srv/b/projekt/run.sh"); n.mode.Perm() != 0o755 {
		t.Errorf("run.sh am Ende %v, erwartet 0755", n.mode.Perm())
	}
	if n := dst.node("/srv/b/projekt"); n.mode.Perm() != 0o750 {
		t.Errorf("Verzeichnis am Ende %v, erwartet 0750", n.mode.Perm())
	}
}

// TestCopyPrivateFileChmodForbidden - kann der Server die Rechte nicht
// einschränken, wird eine private Datei nicht kopiert (sie wäre sonst für
// andere lesbar); nicht private Dateien werden trotzdem kopiert.
func TestCopyPrivateFileChmodForbidden(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	dst.noAttrs = true
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"notiz.txt", "projekt"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 1 || !errors.Is(res.failures[0].err, errCannotProtect) {
		t.Fatalf("Fehler = %v, erwartet errCannotProtect für notiz.txt", res.failures)
	}
	if _, ok := dst.content("/srv/b/notiz.txt"); ok {
		t.Error("private Datei trotz fehlender Rechte kopiert")
	}
	if got, _ := dst.content("/srv/b/projekt/main.go"); got != "package main" {
		t.Error("nicht private Datei nicht kopiert")
	}
	assertNoTempFiles(t, dst)
}

// TestCopySymlinkPlantedDuringCopy - wird zwischen Prüfung und Schreiben am
// Ziel ein Symlink untergeschoben, wird NICHT durch ihn hindurch geschrieben.
func TestCopySymlinkPlantedDuringCopy(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	dst.addFile("/etc/wichtig", "geheim", 0o644, copyTestTime)
	planted := false
	dst.onWriteNode = func(p string, n *memNode) {
		if !planted {
			planted = true
			dst.nodes["/srv/b/notiz.txt"] = &memNode{mode: fs.ModeSymlink | 0o777, target: "/etc/wichtig"}
		}
	}
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"notiz.txt"}}
	runCopySync(t, context.Background(), req, noConflicts(t))
	if got, _ := dst.content("/etc/wichtig"); got != "geheim" {
		t.Errorf("durch untergeschobenen Symlink geschrieben: /etc/wichtig = %q", got)
	}
	if got, ok := dst.content("/srv/b/notiz.txt"); !ok || got != "hallo" {
		t.Errorf("notiz.txt = %q/%v, erwartet reguläre Datei", got, ok)
	}
}

// TestCopyOntoItselfViaOtherHostName - zwei Verbindungseinträge (Name/IP)
// auf denselben Server, gleiches Verzeichnis: die Quelle wird nicht geleert.
func TestCopyOntoItselfViaOtherHostName(t *testing.T) {
	src := newCopySource()
	req := copyRequest{src: src, dst: src, srcDir: "/home/a", dstDir: "/home/a", names: []string{"notiz.txt", "projekt"}, sameHost: false}
	runCopySync(t, context.Background(), req, func(string) copyConflictAnswer { return copyAnswerOverwriteAll })
	if got, _ := src.content("/home/a/notiz.txt"); got != "hallo" {
		t.Errorf("Quelle verändert: %q", got)
	}
	if got, _ := src.content("/home/a/projekt/main.go"); got != "package main" {
		t.Errorf("Quelle im Verzeichnis verändert: %q", got)
	}
	assertNoTempFiles(t, src)
}

// TestCopyRejectsWindowsAndControlNames - Backslash (Pfadtrenner auf
// Windows-Servern) und Steuerzeichen werden beim Kopieren abgelehnt; ein
// Doppelpunkt ist unter Linux ein normales Zeichen und bleibt erlaubt.
func TestCopyRejectsWindowsAndControlNames(t *testing.T) {
	src := &evilFS{memFS: newMemFS("/home/a"), names: []string{"..\\..\\x", "steuer\x01zeichen"}}
	src.addFile("/home/a/a:b", "doppelpunkt", 0o644, copyTestTime)
	src.addDir("/home/a/d", 0o755)
	dst := newMemFS("/srv/b")
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"d", "a:b"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	unsafe := 0
	for _, f := range res.failures {
		if errors.Is(f.err, errUnsafeName) {
			unsafe++
		}
	}
	if unsafe != 2 {
		t.Errorf("unsichere Namen = %d (%v), erwartet 2", unsafe, res.failures)
	}
	if got, _ := dst.content("/srv/b/a:b"); got != "doppelpunkt" {
		t.Error("Name mit Doppelpunkt nicht kopiert")
	}
	for p := range dst.nodes {
		if strings.Contains(p, "\\") {
			t.Errorf("Name mit Backslash angelegt: %q", p)
		}
	}
}

// TestCopyInsideTargetDir prüft die Zusatzkontrolle nach dem Zusammensetzen.
func TestCopyInsideTargetDir(t *testing.T) {
	cases := map[[2]string]bool{
		{"/srv/b", "/srv/b/x"}:    true,
		{"/srv/b", "/srv/b/x/y"}:  true,
		{"/srv/b", "/srv/b"}:      false,
		{"/srv/b", "/srv/bx"}:     false,
		{"/srv/b", "/srv/b/../x"}: false,
		{"/", "/x"}:               true,
		{"/srv/b/", "/srv/b/x"}:   true,
	}
	for in, want := range cases {
		if got := insideDir(in[0], in[1]); got != want {
			t.Errorf("insideDir(%q, %q) = %v, erwartet %v", in[0], in[1], got, want)
		}
	}
}

// TestCopyScanLimits - riesige oder zu tiefe Bäume (bösartiger Server)
// brechen das Erfassen ab, ohne etwas zu kopieren.
func TestCopyScanLimits(t *testing.T) {
	defer func(entries, depth int) { copyMaxEntries, copyMaxDepth = entries, depth }(copyMaxEntries, copyMaxDepth)

	copyMaxEntries, copyMaxDepth = 3, 100
	src, dst := newCopySource(), newMemFS("/srv/b")
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 1 || !errors.Is(res.failures[0].err, errCopyLimit) {
		t.Errorf("Eintragsgrenze: Fehler %v", res.failures)
	}
	if n := dst.node("/srv/b/projekt"); n != nil {
		t.Error("trotz Grenze kopiert")
	}

	copyMaxEntries, copyMaxDepth = 1000, 1
	res, _ = runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 1 || !errors.Is(res.failures[0].err, errCopyLimit) {
		t.Errorf("Tiefengrenze: Fehler %v", res.failures)
	}
}

// hugeFS meldet absurde Dateigrößen (bösartiger Server).
type hugeFS struct{ *memFS }

func (h *hugeFS) Lstat(p string) (os.FileInfo, error) {
	info, err := h.memFS.Lstat(p)
	if err != nil || info.IsDir() {
		return info, err
	}
	return fakeFileInfo{name: info.Name(), size: math.MaxInt64, mode: info.Mode(), modTime: info.ModTime()}, nil
}

// TestCopyHugeSizesNoCrash - Summen laufen nicht über, kein Absturz in der
// Anzeige.
func TestCopyHugeSizesNoCrash(t *testing.T) {
	src, dst := &hugeFS{newCopySource()}, newMemFS("/srv/b")
	events := make(chan copyEvent, 64)
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt", "notiz.txt"}}
	go runCopy(context.Background(), req, events, make(chan copyConflictAnswer, 1))
	for ev := range events {
		if ev.progress != nil && (ev.progress.totalBytes < 0 || ev.progress.doneBytes < 0) {
			t.Errorf("Überlauf: %+v", *ev.progress)
		}
		if ev.result != nil {
			if ev.result.bytes < 0 {
				t.Errorf("Ergebnis-Bytes übergelaufen: %d", ev.result.bytes)
			}
			break
		}
	}
	for _, c := range [][3]int64{
		{math.MaxInt64, math.MaxInt64, 10}, {math.MaxInt64, 1, 10}, {-5, 10, 10},
		{5, -10, 10}, {math.MinInt64, math.MaxInt64, 10}, {1, math.MaxInt64, 10},
	} {
		bar := progressBar(c[0], c[1], int(c[2]))
		if n := len([]rune(bar)); n != 10 {
			t.Errorf("progressBar(%d,%d) hat %d Zeichen", c[0], c[1], n)
		}
	}
	if got := satAdd(math.MaxInt64, 5); got != math.MaxInt64 {
		t.Errorf("satAdd = %d", got)
	}
	if got := satAdd(5, -10); got != 0 {
		t.Errorf("satAdd negativ = %d, erwartet 0", got)
	}
}
