// Paket main - Tests: liegen gebliebene Temp-Dateien aufräumen (Build 82)
//
// Reißt die Verbindung mitten in einer Datei ab, lässt sich die Temp-Datei
// am Ziel nicht mehr löschen (Nutzerfall 2026-10-24: 623 MB in einem
// Log-Ordner). Kopiert man später wieder in dieses Verzeichnis, entfernt
// ssh-easy am Ende des Laufs eigene Temp-Dateien (".ssh-easy-copy-<16
// hex>.tmp", reguläre Datei, auf Servern Rechte 0600), die seit mindestens
// copyStaleTempAge nicht mehr geändert wurden und sich während des Laufs
// nicht verändert haben. Laufende Kopien einer zweiten ssh-easy-Instanz
// bleiben unberührt (Code-Review und Audit Build 82).
//
// @author Kurt Ingwer
// @date   2026-10-24 21:49
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const staleName = ".ssh-easy-copy-0123456789abcdef.tmp"

// staleTime liefert eine Änderungszeit, die als liegen geblieben gilt.
func staleTime() time.Time {
	return time.Now().Add(-copyStaleTempAge - time.Minute)
}

// TestIsStaleCopyTemp - nur genau das eigene Namensmuster, nur reguläre
// Dateien, nur wenn alt genug (Zeit in der Zukunft = nicht alt).
func TestIsStaleCopyTemp(t *testing.T) {
	now := time.Now()
	old := now.Add(-copyStaleTempAge - time.Second)
	cases := []struct {
		name  string
		mode  fs.FileMode
		mtime time.Time
		want  bool
	}{
		{staleName, 0o600, old, true},
		{".ssh-easy-copy-ffffffffffffffff.tmp", 0o644, old, true},
		{staleName, 0o600, now.Add(-copyStaleTempAge + time.Minute), false},
		{staleName, 0o600, now.Add(24 * time.Hour), false},
		{staleName, fs.ModeDir | 0o700, old, false},
		{staleName, fs.ModeSymlink | 0o777, old, false},
		{".ssh-easy-copy-0123456789ABCDEF.tmp", 0o600, old, false},
		{".ssh-easy-copy-0123456789abcde.tmp", 0o600, old, false},
		{".ssh-easy-copy-0123456789abcdef0.tmp", 0o600, old, false},
		{".ssh-easy-copy-0123456789abcdeg.tmp", 0o600, old, false},
		{".ssh-easy-copy-0123456789abcdef.tmp.bak", 0o600, old, false},
		{"x.ssh-easy-copy-0123456789abcdef.tmp", 0o600, old, false},
		{".ssh-easy-copy-0123456789abcdef.TMP", 0o600, old, false},
		{"bericht.txt", 0o600, old, false},
	}
	for _, c := range cases {
		info := fakeFileInfo{name: c.name, mode: c.mode, modTime: c.mtime}
		if got := isStaleCopyTemp(info, now, false); got != c.want {
			t.Errorf("%q (%v, %v): %v, erwartet %v", c.name, c.mode, now.Sub(c.mtime), got, c.want)
		}
		// Auf einem Server nur mit Rechten genau 0600 (so bleibt eine Datei
		// nach einem Abbruch liegen; erst ganz am Ende bekommt sie die echten
		// Rechte und kurz die alte Änderungszeit der Quelle)
		want := c.want && c.mode.Perm() == 0o600
		if got := isStaleCopyTemp(info, now, true); got != want {
			t.Errorf("%q (%v) nur 0600: %v, erwartet %v", c.name, c.mode, got, want)
		}
	}
}

// TestCopyRemovesStaleTemps - beim Kopieren in ein vorhandenes Verzeichnis
// verschwinden alte eigene Temp-Dateien; frische, fremde Namen,
// Verzeichnisse und Symlinks bleiben. Auch in zusammengeführten
// Unterverzeichnissen und in Verzeichnissen, deren Namen einzeln per
// Lstat geprüft werden (Umlaut).
func TestCopyRemovesStaleTemps(t *testing.T) {
	src := newMemFS("/home/a")
	src.addFile("/home/a/a.txt", "neu", 0o644, copyTestTime)
	src.addFile("/home/a/projekt/x.txt", "x", 0o644, copyTestTime)
	src.addFile("/home/a/ärger/y.txt", "y", 0o644, copyTestTime)
	dst := newMemFS("/srv/b")
	fresh := ".ssh-easy-copy-00000000000000aa.tmp"
	keep := []string{
		"/srv/b/" + fresh,
		"/srv/b/.ssh-easy-copy-0123456789abcdef.tmp.bak",
		"/srv/b/.ssh-easy-copy-kaputt.tmp",
		"/srv/b/.ssh-easy-copy-00000000000000bb.tmp",
		"/srv/b/.ssh-easy-copy-00000000000000cc.tmp",
	}
	dst.addFile("/srv/b/"+staleName, "halb", 0o600, staleTime())
	dst.addFile("/srv/b/"+fresh, "läuft", 0o600, time.Now())
	dst.addFile("/srv/b/.ssh-easy-copy-0123456789abcdef.tmp.bak", "x", 0o600, staleTime())
	dst.addFile("/srv/b/.ssh-easy-copy-kaputt.tmp", "x", 0o600, staleTime())
	dst.addDir("/srv/b/.ssh-easy-copy-00000000000000bb.tmp", 0o700)
	dst.nodes["/srv/b/.ssh-easy-copy-00000000000000bb.tmp"].modTime = staleTime()
	dst.addLink("/srv/b/.ssh-easy-copy-00000000000000cc.tmp", "/etc/passwd")
	dst.nodes["/srv/b/.ssh-easy-copy-00000000000000cc.tmp"].modTime = staleTime()
	dst.addFile("/srv/b/projekt/"+staleName, "halb", 0o600, staleTime())
	dst.addFile("/srv/b/ärger/"+staleName, "halb", 0o600, staleTime())

	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"a.txt", "projekt", "ärger"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 || res.files != 3 {
		t.Fatalf("Fehler %v, Dateien %d", res.failures, res.files)
	}
	for _, p := range []string{"/srv/b/" + staleName, "/srv/b/projekt/" + staleName, "/srv/b/ärger/" + staleName} {
		if dst.node(p) != nil {
			t.Errorf("alte Temp-Datei nicht entfernt: %s", p)
		}
	}
	for _, p := range keep {
		if dst.node(p) == nil {
			t.Errorf("fälschlich entfernt: %s", p)
		}
	}
	if got, _ := dst.content("/srv/b/a.txt"); got != "neu" {
		t.Errorf("a.txt = %q", got)
	}
}

// TestCopyStaleTempRemoveErrorIgnored - lässt sich eine alte Temp-Datei
// nicht löschen, läuft die Kopie trotzdem ohne Fehler durch.
func TestCopyStaleTempRemoveErrorIgnored(t *testing.T) {
	src := newMemFS("/home/a")
	src.addFile("/home/a/a.txt", "neu", 0o644, copyTestTime)
	dst := newMemFS("/srv/b")
	dst.addFile("/srv/b/"+staleName, "halb", 0o600, staleTime())
	dst.noRemove["/srv/b/"+staleName] = true
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"a.txt"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 || res.files != 1 {
		t.Fatalf("Fehler %v, Dateien %d", res.failures, res.files)
	}
}

// TestCopySourceNamedLikeStaleTemp - heißt eine Quelldatei wie eine alte
// Temp-Datei am Ziel, kommt die normale Rückfrage; die eben kopierte Datei
// wird danach nicht als "alt" gelöscht (sie hat sich verändert).
func TestCopySourceNamedLikeStaleTemp(t *testing.T) {
	src := newMemFS("/home/a")
	src.addFile("/home/a/"+staleName, "quelle", 0o600, staleTime())
	dst := newMemFS("/srv/b")
	dst.addFile("/srv/b/"+staleName, "halb", 0o600, staleTime())
	dst.addFile("/srv/b/anderes.txt", "x", 0o644, copyTestTime)
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{staleName}}
	asked := 0
	res, _ := runCopySync(t, context.Background(), req, func(string) copyConflictAnswer {
		asked++
		return copyAnswerOverwrite
	})
	if asked != 1 {
		t.Errorf("%d Rückfragen, erwartet 1", asked)
	}
	if len(res.failures) != 0 || res.files != 1 {
		t.Fatalf("Fehler %v, Dateien %d", res.failures, res.files)
	}
	if got, _ := dst.content("/srv/b/" + staleName); got != "quelle" {
		t.Errorf("Ziel = %q", got)
	}
}

// TestCopyStaleTempNotRemovedInForeignDir - taucht zwischen Einlesen und
// Mkdir am Ziel ein fremder Ordner auf (untergeschobener Symlink auf einen
// Opferordner), scheitert Mkdir. Darin wird nichts gelöscht - auch keine
// Datei, die wie eine alte eigene Temp-Datei aussieht -, weder direkt
// darin noch in seinen Unterordnern (einfache Namen und Umlaut).
func TestCopyStaleTempNotRemovedInForeignDir(t *testing.T) {
	for _, file := range []string{"sub/geheim.txt", "sub/plain/geheim.txt", "sub/\u00fc/geheim.txt"} {
		src := newMemFS("/home/a")
		src.addFile("/home/a/"+file, "geheim", 0o600, copyTestTime)
		dst := &hiddenDirFS{memFS: newMemFS("/srv/b"), hide: "/srv/b/sub"}
		stale := "/srv/b/" + path.Dir(file) + "/" + staleName
		dst.addFile(stale, "fremd", 0o600, staleTime())
		req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"sub"}}
		res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
		if len(res.failures) != 1 {
			t.Errorf("%s: Fehler %v, erwartet genau einen (Mkdir)", file, res.failures)
		}
		if dst.node(stale) == nil {
			t.Errorf("%s: Datei im fremden Ordner gelöscht", file)
		}
	}
}

// hiddenDirFS verschweigt ein vorhandenes Verzeichnis in der Liste seines
// Elternverzeichnisses, und Mkdir darauf scheitert - wie ein Ordner, den
// ein Angreifer nach dem Einlesen untergeschoben hat.
type hiddenDirFS struct {
	*memFS
	hide string
}

func (f *hiddenDirFS) ReadDir(p string) ([]os.FileInfo, error) {
	infos, err := f.memFS.ReadDir(p)
	out := infos[:0]
	for _, info := range infos {
		if path.Join(p, info.Name()) != f.hide {
			out = append(out, info)
		}
	}
	return out, err
}

func (f *hiddenDirFS) Mkdir(p string) error {
	if p == f.hide {
		return os.ErrExist
	}
	return f.memFS.Mkdir(p)
}

// TestCopyRemovesStaleTempsLocal - auch auf dem lokalen PC (Abbruch durch
// Absturz oder Beenden) werden alte Temp-Dateien im Zielordner entfernt,
// frische bleiben.
func TestCopyRemovesStaleTempsLocal(t *testing.T) {
	local, dir := newLocalTestFS(t)
	home, _ := local.Getwd()
	old := filepath.Join(dir, staleName)
	fresh := filepath.Join(dir, ".ssh-easy-copy-00000000000000aa.tmp")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("halb"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(old, staleTime(), staleTime()); err != nil {
		t.Fatal(err)
	}
	src := newMemFS("/srv/a")
	src.addFile("/srv/a/a.txt", "neu", 0o644, copyTestTime)
	req := copyRequest{src: src, dst: local, srcDir: "/srv/a", dstDir: home, names: []string{"a.txt"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 || res.files != 1 {
		t.Fatalf("Fehler %v, Dateien %d", res.failures, res.files)
	}
	if _, err := os.Lstat(old); !os.IsNotExist(err) {
		t.Errorf("alte Temp-Datei nicht entfernt: %v", err)
	}
	if _, err := os.Lstat(fresh); err != nil {
		t.Errorf("frische Temp-Datei entfernt: %v", err)
	}
}

// TestCopyStaleTempNeeds0600Remote - auf einem Server bleibt eine alte
// Temp-Datei mit anderen Rechten als 0600 liegen (kann eine gerade
// fertige Datei einer zweiten Instanz kurz vor dem Umbenennen sein).
func TestCopyStaleTempNeeds0600Remote(t *testing.T) {
	src := newMemFS("/home/a")
	src.addFile("/home/a/a.txt", "neu", 0o644, copyTestTime)
	dst := newMemFS("/srv/b")
	dst.addFile("/srv/b/"+staleName, "fertig", 0o644, staleTime())
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"a.txt"}}
	runCopySync(t, context.Background(), req, noConflicts(t))
	if dst.node("/srv/b/"+staleName) == nil {
		t.Error("Temp-Datei mit 0644 entfernt")
	}
}

// TestCopyStaleTempAttemptsBounded - Code-Review Build 82: höchstens
// copyMaxStaleTemps Löschversuche pro Lauf, über mehrere Verzeichnisse
// hinweg; ohne Löschrecht (fremde Dateien im Ordner mit Sticky-Bit,
// bösartiger Server) nur ein Versuch pro Verzeichnis.
func TestCopyStaleTempAttemptsBounded(t *testing.T) {
	for _, removable := range []bool{false, true} {
		src := newMemFS("/home/a")
		dst := newMemFS("/srv/b")
		var names []string
		for _, dir := range []string{"x", "y"} {
			src.addFile("/home/a/"+dir+"/a.txt", "neu", 0o644, copyTestTime)
			dst.addDir("/srv/b/"+dir, 0o755)
			for i := 0; i < copyMaxStaleTemps; i++ {
				p := fmt.Sprintf("/srv/b/%s/.ssh-easy-copy-%016x.tmp", dir, i)
				dst.addFile(p, "halb", 0o600, staleTime())
				dst.noRemove[p] = !removable
			}
			names = append(names, dir)
		}
		var attempts atomic.Int64
		dst.onRemove = func() { attempts.Add(1) }
		req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: names}
		res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
		if len(res.failures) != 0 || res.files != 2 {
			t.Fatalf("löschbar %v: Fehler %v, Dateien %d", removable, res.failures, res.files)
		}
		want := int64(copyMaxStaleTemps)
		if !removable {
			want = 1
		}
		if got := attempts.Load(); got != want {
			t.Errorf("löschbar %v: %d Löschversuche, erwartet genau %d", removable, got, want)
		}
	}
}

// TestCopyStaleTempStopsOnCancel - Esc während des Aufräumens hält es an.
func TestCopyStaleTempStopsOnCancel(t *testing.T) {
	src := newMemFS("/home/a")
	src.addFile("/home/a/a.txt", "neu", 0o644, copyTestTime)
	dst := newMemFS("/srv/b")
	for i := 0; i < 50; i++ {
		dst.addFile(fmt.Sprintf("/srv/b/.ssh-easy-copy-%016x.tmp", i), "halb", 0o600, staleTime())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts atomic.Int64
	dst.onRemove = func() {
		attempts.Add(1)
		cancel()
	}
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"a.txt"}}
	runCopySync(t, ctx, req, noConflicts(t))
	if got := attempts.Load(); got != 1 {
		t.Errorf("%d Löschversuche nach Abbruch, erwartet 1", got)
	}
}

// TestCopyStaleTempLogged - jede entfernte Temp-Datei steht im Log (der
// Nutzer soll sehen, was ssh-easy auf dem Server gelöscht hat);
// Steuerzeichen im Pfad werden bereinigt.
func TestCopyStaleTempLogged(t *testing.T) {
	src := newMemFS("/home/a")
	src.addFile("/home/a/a.txt", "neu", 0o644, copyTestTime)
	dst := newMemFS("/srv/b\x1b[31m")
	dst.addFile("/srv/b\x1b[31m/"+staleName, "halb", 0o600, staleTime())
	var logged []string
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b\x1b[31m", names: []string{"a.txt"},
		logf: func(format string, args ...interface{}) { logged = append(logged, fmt.Sprintf(format, args...)) }}
	runCopySync(t, context.Background(), req, noConflicts(t))
	if len(logged) != 1 || !strings.Contains(logged[0], staleName) {
		t.Fatalf("Log %q, erwartet einen Eintrag mit %s", logged, staleName)
	}
	if strings.ContainsRune(logged[0], 0x1b) {
		t.Errorf("Steuerzeichen im Log: %q", logged[0])
	}
}

// changingFS ändert eine vorgemerkte Temp-Datei während des Laufs (wie
// eine zweite Instanz, die weiterschreibt oder sie gerade umbenennt).
type changingFS struct {
	*memFS
	target string
	rename bool
}

func (f *changingFS) CreateExclusive(p string) (io.WriteCloser, error) {
	f.memFS.mu.Lock()
	if n := f.memFS.nodes[f.target]; n != nil {
		if f.rename {
			f.memFS.nodes["/srv/b/fertig.bin"] = n
			delete(f.memFS.nodes, f.target)
		} else {
			n.data = append(n.data, "mehr"...)
		}
	}
	f.memFS.mu.Unlock()
	return f.memFS.CreateExclusive(p)
}

// TestCopyStaleTempChangedDuringRunKept - Audit Build 82: eine Temp-Datei,
// die beim Einlesen alt aussah, sich bis zum Ende des Laufs aber verändert
// hat (weitergeschrieben) oder verschwunden ist (umbenannt), wird nicht
// gelöscht; die fertige Datei der anderen Instanz bleibt unangetastet.
func TestCopyStaleTempChangedDuringRunKept(t *testing.T) {
	for _, rename := range []bool{false, true} {
		src := newMemFS("/home/a")
		src.addFile("/home/a/a.txt", "neu", 0o644, copyTestTime)
		dst := &changingFS{memFS: newMemFS("/srv/b"), target: "/srv/b/" + staleName, rename: rename}
		dst.addFile(dst.target, "halb", 0o600, staleTime())
		req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"a.txt"}}
		res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
		if len(res.failures) != 0 {
			t.Fatalf("umbenannt %v: Fehler %v", rename, res.failures)
		}
		kept := dst.target
		if rename {
			kept = "/srv/b/fertig.bin"
		}
		if dst.node(kept) == nil {
			t.Errorf("umbenannt %v: %s gelöscht", rename, kept)
		}
	}
}

// vanishProbeFS ist dasselbe Verzeichnis über zwei Verbindungseinträge, in
// dem ein fremder Aufräumer jede neue Prüfdatei sofort wieder löscht.
type vanishProbeFS struct{ *memFS }

func (f *vanishProbeFS) CreateExclusive(p string) (io.WriteCloser, error) {
	w, err := f.memFS.CreateExclusive(p)
	if err == nil && strings.HasPrefix(path.Base(p), copyProbePrefix) {
		_ = f.memFS.Remove(p)
	}
	return w, err
}

// TestMoveProbeVanishedKeepsSource - Sicherheitsaudit Build 82: verschwindet
// die Prüfdatei "gleicher Ordner?", während sie über die Quelle gesucht
// wird, darf das nicht als "verschieden" gelten - sonst würde die Datei auf
// sich selbst kopiert und danach die einzige Kopie gelöscht. Stattdessen
// Fehler, die Quelle bleibt. Die Prüfdatei passt nie auf das Muster der
// liegen gebliebenen Temp-Dateien.
func TestMoveProbeVanishedKeepsSource(t *testing.T) {
	shared := newMemFS("/srv/b")
	shared.addFile("/srv/b/einzig.txt", "einzige Kopie", 0o644, copyTestTime)
	req := copyRequest{src: shared, dst: &vanishProbeFS{shared}, srcDir: "/srv/b", dstDir: "/srv/b",
		names: []string{"einzig.txt"}, move: true}
	res, _ := runCopySync(t, context.Background(), req, func(string) copyConflictAnswer { return copyAnswerOverwrite })
	if got, ok := shared.content("/srv/b/einzig.txt"); !ok || got != "einzige Kopie" {
		t.Fatalf("Quelle verloren: %q, %v", got, ok)
	}
	if len(res.failures) != 1 || !errors.Is(res.failures[0].err, errMoveCheckFailed) {
		t.Errorf("Fehler %v, erwartet errMoveCheckFailed", res.failures)
	}
	probe := fakeFileInfo{name: path.Base(tempProbeName("/srv/b/x")), mode: 0o600, modTime: staleTime()}
	if isStaleCopyTemp(probe, time.Now(), true) {
		t.Errorf("Prüfdatei %q gilt als alte Temp-Datei", probe.name)
	}
}
