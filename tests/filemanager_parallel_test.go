// Paket main - Tests: Parallele Abläufe im Datei-Manager (Build 90, Teil 1)
//
// Bisher liefen vier Abläufe Rundreise für Rundreise nacheinander. Bei einem
// Server im Internet (40 ms Ping) dauerte jedes davon Minuten. Jetzt gilt:
//   - readPanelDir prüft die Ziele von Symlinks gleichzeitig per Stat
//     (höchstens 32 gleichzeitig, fmStatParallel)
//   - runDelete liest den Baum vorab gleichzeitig (Lstat, ReadDir, Kettenprüfung;
//     höchstens deleteParallel = 32), prüft und löscht aber danach EINE
//     Goroutine in der Reihenfolge der sortierten Tiefensuche (Inhalt vor
//     Verzeichnis). Sicherheitsentscheidung: gleichzeitige Removes würden die
//     Zusage der Tests TestDeleteStopsWhenParentReplacedDuringDelete u. a.
//     aufweichen (nach einem Austausch fällt höchstens der eine laufende Eintrag)
//   - copyRun.scan liest die Verzeichnisse der Quelle gleichzeitig (höchstens
//     copyParallel), Reihenfolge der Ergebnisse und Fehler bleibt die der
//     sortierten Tiefensuche
//   - removeMovedDirs löscht die Quellverzeichnisse ebenenweise gleichzeitig
//     (tiefste Ebene zuerst)
//
// Die Tests prüfen jeweils zweierlei: dass wirklich gleichzeitig gearbeitet
// wird (Höchststand > 1, aber unter der Obergrenze) und dass sich am Ergebnis
// nichts ändert (Reihenfolge, Fehler, Sicherheitsgrenzen). Alles läuft auch
// mit -race; die Verzögerungen sind kurz (2-5 ms).
//
// Hinweis zum Vorher-Stand: Diese Tests prüfen den Höchststand der Lesezugriffe
// bzw. Verzeichnis-Löschungen und scheitern ohne die Änderung
// (ReadPanelDirParallelStatsSymlinks, ReadPanelDirStatLimitIsVariable,
// DeleteParallelRemovesManyAtOnce, DeleteParallelManyTopEntriesKeepInputOrder,
// DeletePrefetchReadsFasterThanSequential, DeleteParallelLimitIsVariable,
// CopyScanParallelReadsDirectories, MoveRemoveDirsParallelByLevel). Alle
// übrigen sichern das bisherige Verhalten ab (Wächter: Reihenfolge, Fehler,
// Grenzen, Abbruch) und müssen vorher wie nachher grün sein.
//
// @author Kurt Ingwer
// @date   2026-11-13 09:05
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// parGauge zählt gleichzeitig laufende Aufrufe und merkt den Höchststand.
type parGauge struct {
	active atomic.Int64
	peak   atomic.Int64
}

// enter meldet den Beginn eines Aufrufs.
func (g *parGauge) enter() { raiseMax(&g.peak, g.active.Add(1)) }

// leave meldet das Ende eines Aufrufs.
func (g *parGauge) leave() { g.active.Add(-1) }

// treeSnapshot liefert den Teilbaum unter root als Abbild: relativer Pfad ->
// "dir", "file:<Inhalt>" oder "link:<Ziel>". Damit lassen sich Quelle und
// Ziel eines Kopiervorgangs vergleichen, ohne Rechte und Zeiten zu betrachten.
func treeSnapshot(m *memFS, root string) map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for p, n := range m.nodes {
		if p != root && !strings.HasPrefix(p, root+"/") {
			continue
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
		switch {
		case n.mode.IsDir():
			out[rel] = "dir"
		case n.mode&os.ModeSymlink != 0:
			out[rel] = "link:" + n.target
		default:
			out[rel] = "file:" + string(n.data)
		}
	}
	return out
}

// --- A1: readPanelDir prüft Symlink-Ziele gleichzeitig ---

// parStatMax ist die Obergrenze gleichzeitiger Stat-Aufrufe (fmStatParallel).
const parStatMax = 32

// slowStatFS verzögert Stat und zählt gleichzeitige Aufrufe. Es liest nur
// Karten des Fakes (nie schreibend) und ist deshalb goroutinensicher.
type slowStatFS struct {
	*fakeRemoteFS
	delay time.Duration
	gauge parGauge
	mu    sync.Mutex
	stats []string
}

func (s *slowStatFS) Stat(p string) (os.FileInfo, error) {
	s.gauge.enter()
	defer s.gauge.leave()
	s.mu.Lock()
	s.stats = append(s.stats, p)
	s.mu.Unlock()
	time.Sleep(s.delay)
	return s.fakeRemoteFS.Stat(p)
}

// parSymlinkHome liefert ein Home-Verzeichnis mit n Symlinks link00 ... und
// vier gewöhnlichen Einträgen. Ziele: i%3==0 Verzeichnis, 1 Datei, 2 defekt.
func parSymlinkHome(n int) *fakeRemoteFS {
	home := "/home/kurt"
	f := &fakeRemoteFS{
		home:   home,
		dirs:   map[string][]os.FileInfo{"/": {dirInfo("home")}},
		links:  map[string]os.FileInfo{},
		denied: map[string]bool{},
	}
	// Absichtlich nicht in Anzeigereihenfolge: die Sortierung muss greifen
	entries := []os.FileInfo{fileInfo("b.txt", 10), dirInfo("Zdir"), fileInfo("A.txt", 5), dirInfo("adir")}
	for i := n - 1; i >= 0; i-- {
		name := fmt.Sprintf("link%02d", i)
		entries = append(entries, linkInfo(name))
		switch i % 3 {
		case 0:
			f.links[home+"/"+name] = dirInfo("ziel")
		case 1:
			f.links[home+"/"+name] = fileInfo("ziel", 1)
		default:
			f.links[home+"/"+name] = nil
		}
	}
	f.dirs[home] = entries
	return f
}

// parEntryView ist die für den Vergleich wichtige Sicht auf einen Eintrag.
type parEntryView struct {
	name                 string
	dir, symlink, broken bool
}

// parExpectedHome liefert die erwartete Anzeigereihenfolge für parSymlinkHome:
// ".." zuerst, dann Verzeichnisse (auch Symlinks darauf), dann Dateien, jeweils
// ohne Beachtung der Groß-/Kleinschreibung.
func parExpectedHome(n int) []parEntryView {
	want := []parEntryView{{name: "..", dir: true}, {name: "adir", dir: true}}
	for i := 0; i < n; i++ {
		if i%3 == 0 {
			want = append(want, parEntryView{name: fmt.Sprintf("link%02d", i), dir: true, symlink: true})
		}
	}
	want = append(want, parEntryView{name: "Zdir", dir: true}, parEntryView{name: "A.txt"}, parEntryView{name: "b.txt"})
	for i := 0; i < n; i++ {
		if i%3 != 0 {
			want = append(want, parEntryView{name: fmt.Sprintf("link%02d", i), symlink: true, broken: i%3 == 2})
		}
	}
	return want
}

// parViews wandelt Einträge in die Vergleichssicht um.
func parViews(entries []fmEntry) []parEntryView {
	out := make([]parEntryView, len(entries))
	for i, e := range entries {
		out[i] = parEntryView{name: e.name, dir: e.dir, symlink: e.symlink, broken: e.broken}
	}
	return out
}

// TestReadPanelDirParallelStatsSymlinks - 40 Symlinks, Stat dauert 5 ms: es
// laufen mehrere Stat-Aufrufe gleichzeitig (sonst 200 ms Wartezeit statt 20),
// aber nie mehr als 32. Das Ergebnis (Sortierung, dir/broken je Eintrag:
// Ziel Verzeichnis, Datei oder defekt) ist dasselbe wie bei nacheinander.
func TestReadPanelDirParallelStatsSymlinks(t *testing.T) {
	const n = 40
	fsys := &slowStatFS{fakeRemoteFS: parSymlinkHome(n), delay: 5 * time.Millisecond}
	entries, err := readPanelDir(fsys, "/home/kurt")
	if err != nil {
		t.Fatalf("Fehler: %v", err)
	}
	if peak := fsys.gauge.peak.Load(); peak <= 1 || peak > parStatMax {
		t.Errorf("gleichzeitige Stat-Aufrufe: %d, erwartet 2..%d", peak, parStatMax)
	}
	if len(fsys.stats) != n {
		t.Errorf("%d Stat-Aufrufe, erwartet %d (einer je Symlink, keiner sonst)", len(fsys.stats), n)
	}
	if got, want := parViews(entries), parExpectedHome(n); !reflect.DeepEqual(got, want) {
		t.Errorf("Einträge falsch.\n erhalten: %v\n erwartet: %v", got, want)
	}
}

// TestReadPanelDirStatLimitIsVariable - fmStatParallel ist eine Variable:
// mit 3 laufen höchstens 3 Stat-Aufrufe gleichzeitig (aber mehr als einer), mit
// 1 genau einer (die Untergrenze: 0 oder negative Werte dürfen nicht
// hängen bleiben). Ergebnis jeweils unverändert. Der Wert wird per defer
// zurückgesetzt.
func TestReadPanelDirStatLimitIsVariable(t *testing.T) {
	defer func(limit int) { fmStatParallel = limit }(fmStatParallel)
	const n = 30
	for _, limit := range []int{3, 1, 0, -5} {
		fmStatParallel = limit
		fsys := &slowStatFS{fakeRemoteFS: parSymlinkHome(n), delay: 3 * time.Millisecond}
		entries, err := readPanelDir(fsys, "/home/kurt")
		if err != nil {
			t.Fatalf("Grenze %d: %v", limit, err)
		}
		peak := fsys.gauge.peak.Load()
		switch {
		case limit == 3 && (peak <= 1 || peak > 3):
			t.Errorf("Grenze 3: %d gleichzeitige Stat-Aufrufe, erwartet 2..3", peak)
		case limit <= 1 && peak != 1:
			t.Errorf("Grenze %d: %d gleichzeitige Stat-Aufrufe, erwartet genau 1", limit, peak)
		}
		if len(fsys.stats) != n {
			t.Errorf("Grenze %d: %d Stat-Aufrufe, erwartet %d", limit, len(fsys.stats), n)
		}
		if got, want := parViews(entries), parExpectedHome(n); !reflect.DeepEqual(got, want) {
			t.Errorf("Grenze %d: Einträge falsch.\n erhalten: %v\n erwartet: %v", limit, got, want)
		}
	}
}

// TestReadPanelDirResultStableUnderParallelism - mehrere Läufe liefern genau
// dasselbe: die Reihenfolge, in der die Stat-Antworten eintreffen, darf die
// Anzeige nicht verändern (Zufall der Goroutinen-Planung).
func TestReadPanelDirResultStableUnderParallelism(t *testing.T) {
	const n = 40
	var first []fmEntry
	for run := 0; run < 6; run++ {
		fsys := &slowStatFS{fakeRemoteFS: parSymlinkHome(n), delay: time.Millisecond}
		entries, err := readPanelDir(fsys, "/home/kurt")
		if err != nil {
			t.Fatalf("Lauf %d: %v", run, err)
		}
		if run == 0 {
			first = entries
			continue
		}
		if !reflect.DeepEqual(entries, first) {
			t.Fatalf("Lauf %d unterscheidet sich vom ersten:\n%v\n%v", run, parViews(entries), parViews(first))
		}
	}
}

// TestReadPanelDirSymlinkCounts - Randfälle der Anzahl: kein Symlink, einer,
// genau die Obergrenze und eins darüber. Immer korrekt, nie über 32
// gleichzeitig (Grenze, Off-by-one).
func TestReadPanelDirSymlinkCounts(t *testing.T) {
	for _, n := range []int{0, 1, 2, parStatMax - 1, parStatMax, parStatMax + 1} {
		fsys := &slowStatFS{fakeRemoteFS: parSymlinkHome(n), delay: 2 * time.Millisecond}
		entries, err := readPanelDir(fsys, "/home/kurt")
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if got, want := parViews(entries), parExpectedHome(n); !reflect.DeepEqual(got, want) {
			t.Errorf("n=%d: Einträge falsch.\n erhalten: %v\n erwartet: %v", n, got, want)
		}
		if peak := fsys.gauge.peak.Load(); peak > min(int64(n), parStatMax) {
			t.Errorf("n=%d: %d gleichzeitige Stat-Aufrufe, höchstens %d erlaubt", n, peak, min(n, parStatMax))
		}
	}
}

// TestReadPanelDirParallelSkipsUnsafeNames - ein bösartiger Server liefert
// Symlinks mit "/" im Namen. Sie werden weggelassen und nie per Stat angefasst
// (sonst führte "../x" aus dem Verzeichnis heraus) - auch nicht parallel.
func TestReadPanelDirParallelSkipsUnsafeNames(t *testing.T) {
	base := parSymlinkHome(6)
	base.dirs["/home/kurt"] = append(base.dirs["/home/kurt"], linkInfo("a/b"), linkInfo("../../etc"), linkInfo(".."), linkInfo("."))
	fsys := &slowStatFS{fakeRemoteFS: base, delay: time.Millisecond}
	entries, err := readPanelDir(fsys, "/home/kurt")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range fsys.stats {
		if strings.Contains(p, "a/b") || strings.Contains(p, "etc") || strings.HasSuffix(p, "/..") || strings.HasSuffix(p, "/.") {
			t.Errorf("Stat auf unsicheren Namen: %q", p)
		}
	}
	if got, want := parViews(entries), parExpectedHome(6); !reflect.DeepEqual(got, want) {
		t.Errorf("Einträge falsch.\n erhalten: %v\n erwartet: %v", got, want)
	}
}

// TestReadPanelDirReadErrorStillReported - scheitert das Lesen des
// Verzeichnisses selbst, kommt der Fehler unverändert zurück und es wird
// kein Stat versucht.
func TestReadPanelDirReadErrorStillReported(t *testing.T) {
	base := parSymlinkHome(5)
	base.denied["/home/kurt"] = true
	fsys := &slowStatFS{fakeRemoteFS: base, delay: time.Millisecond}
	entries, err := readPanelDir(fsys, "/home/kurt")
	if !errors.Is(err, os.ErrPermission) || entries != nil {
		t.Errorf("erwartet ErrPermission und keine Einträge, erhalten %v / %v", err, entries)
	}
	if len(fsys.stats) != 0 {
		t.Errorf("%d Stat-Aufrufe trotz Lesefehler", len(fsys.stats))
	}
}

// --- A2: runDelete liest vorab gleichzeitig, löscht aber der Reihe nach ---

// parDeleteMax ist die Obergrenze gleichzeitiger Lesezugriffe (deleteParallel).
const parDeleteMax = 32

// parDeleteFS verzögert Lstat, ReadDir und Remove und zählt gleichzeitige
// Aufrufe je Art (lstats, readDirs, beide zusammen in reads, Remove in
// removes). Bei jedem Remove eines Verzeichnisses prüft es, ob es noch Kinder
// hat (Verstoß: Verzeichnis vor seinem Inhalt entfernt). failRemove lässt
// einzelne Pfade scheitern; onRemove meldet die laufende Nummer des Aufrufs;
// removed merkt die erfolgreichen Remove-Aufrufe in ihrer Reihenfolge.
type parDeleteFS struct {
	*memFS
	lstatDelay, readDelay, removeDelay time.Duration
	// Verzögerung je Pfad für Lstat (überschreibt lstatDelay, wenn gesetzt)
	lstatDelayFor func(p string) time.Duration
	lstats        parGauge
	readDirs      parGauge
	reads         parGauge
	removes       parGauge
	calls         atomic.Int64
	// Zahl der verzögerten Lesezugriffe (Lstat + ReadDir) insgesamt
	delayedReads atomic.Int64
	onRemove     func(n int64)

	mu         sync.Mutex
	violations []string
	failRemove map[string]error
	removed    []string
}

func (f *parDeleteFS) Lstat(p string) (os.FileInfo, error) {
	f.lstats.enter()
	f.reads.enter()
	defer f.lstats.leave()
	defer f.reads.leave()
	f.delayedReads.Add(1)
	delay := f.lstatDelay
	if f.lstatDelayFor != nil {
		delay = f.lstatDelayFor(p)
	}
	time.Sleep(delay)
	return f.memFS.Lstat(p)
}

func (f *parDeleteFS) ReadDir(p string) ([]os.FileInfo, error) {
	f.readDirs.enter()
	f.reads.enter()
	defer f.readDirs.leave()
	defer f.reads.leave()
	f.delayedReads.Add(1)
	time.Sleep(f.readDelay)
	return f.memFS.ReadDir(p)
}

func (f *parDeleteFS) Remove(p string) error {
	n := f.calls.Add(1)
	if f.onRemove != nil {
		f.onRemove(n)
	}
	f.removes.enter()
	defer f.removes.leave()
	if node := f.memFS.node(p); node != nil && node.mode.IsDir() {
		if kids, _ := f.memFS.ReadDir(p); len(kids) > 0 {
			f.mu.Lock()
			f.violations = append(f.violations, fmt.Sprintf("%s hat noch %d Kinder", p, len(kids)))
			f.mu.Unlock()
		}
	}
	time.Sleep(f.removeDelay)
	f.mu.Lock()
	err := f.failRemove[p]
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if err := f.memFS.Remove(p); err != nil {
		return err
	}
	f.mu.Lock()
	f.removed = append(f.removed, p)
	f.mu.Unlock()
	return nil
}

// removedPaths liefert die erfolgreichen Remove-Aufrufe in ihrer Reihenfolge.
func (f *parDeleteFS) removedPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.removed...)
}

// assertReadBounds prüft die Gleichzeitigkeit der Lesezugriffe (Lstat und
// ReadDir je einzeln und zusammen: mehr als einer, höchstens limit) und dass
// Remove nie gleichzeitig lief - die Sicherheitszusage des Löschens.
func (f *parDeleteFS) assertReadBounds(t *testing.T, limit int) {
	t.Helper()
	if peak := f.lstats.peak.Load(); peak <= 1 || peak > int64(limit) {
		t.Errorf("gleichzeitige Lstat-Aufrufe: %d, erwartet 2..%d", peak, limit)
	}
	if peak := f.reads.peak.Load(); peak > int64(limit) {
		t.Errorf("gleichzeitige Lesezugriffe (Lstat + ReadDir): %d, höchstens %d erlaubt", peak, limit)
	}
	if peak := f.removes.peak.Load(); peak != 1 {
		t.Errorf("gleichzeitige Remove-Aufrufe: %d, erwartet genau 1 (nur eine Goroutine löscht)", peak)
	}
}

// newParDeleteFS umhüllt ein memFS mit Home /home/a.
func newParDeleteFS() *parDeleteFS {
	return &parDeleteFS{memFS: newMemFS("/home/a"), failRemove: map[string]error{}}
}

// parAddTree legt unter root einen Baum an: je Verzeichnis zwei Dateien und
// fan Unterverzeichnisse, depth Ebenen tief. Liefert die Zahl aller Einträge
// (Verzeichnisse und Dateien, root eingeschlossen).
func parAddTree(m *memFS, root string, depth, fan int) int {
	m.addDir(root, 0o755)
	count := 1
	for i := 1; i <= 2; i++ {
		m.addFile(fmt.Sprintf("%s/datei%d.txt", root, i), "x", 0o644, copyTestTime)
		count++
	}
	if depth > 0 {
		for i := 0; i < fan; i++ {
			count += parAddTree(m, fmt.Sprintf("%s/u%d", root, i), depth-1, fan)
		}
	}
	return count
}

// parNodeCount zählt alle Einträge des memFS.
func parNodeCount(m *memFS) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.nodes)
}

// TestDeleteParallelRemovesManyAtOnce - ein Verzeichnis mit 40 Dateien, Lstat
// und Remove dauern je 3 ms: die Lstat-Aufrufe laufen gleichzeitig (mehr als
// einer, höchstens deleteParallel), Remove aber NIE gleichzeitig (Höchststand
// genau 1: Sicherheitsentscheidung, siehe Dateikopf). Alles ist weg, die
// Zählung stimmt, jedes Remove sah vorher eine frische Prüfung.
func TestDeleteParallelRemovesManyAtOnce(t *testing.T) {
	fsys := newParDeleteFS()
	fsys.lstatDelay, fsys.removeDelay = 3*time.Millisecond, 3*time.Millisecond
	fsys.addDir("/home/a/baum", 0o755)
	for i := 0; i < 40; i++ {
		fsys.addFile(fmt.Sprintf("/home/a/baum/f%02d.txt", i), "x", 0o644, copyTestTime)
	}
	result := runDeleteSync(t, context.Background(), fsys, "baum")
	if len(result.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(result.failures))
	}
	fsys.assertReadBounds(t, deleteParallel)
	if fsys.node("/home/a/baum") != nil {
		t.Error("Verzeichnis nicht gelöscht")
	}
	if result.deleted != 41 || !reflect.DeepEqual(result.deletedNames, []string{"baum"}) {
		t.Errorf("deleted=%d, deletedNames=%v; erwartet 41 und [baum]", result.deleted, result.deletedNames)
	}
	if len(fsys.violations) != 0 {
		t.Errorf("Verzeichnis vor seinem Inhalt entfernt: %v", fsys.violations)
	}
	if got := len(fsys.removedPaths()); got != 41 {
		t.Errorf("%d Remove-Aufrufe, erwartet 41", got)
	}
}

// TestDeleteParallelDirectoryOnlyAfterContent - tiefer Baum mit Geschwistern
// auf mehreren Ebenen: ein Verzeichnis wird erst entfernt, wenn alles darunter
// weg ist (kein Remove auf ein Verzeichnis mit Kindern, keine Fehler). Drei
// Bäume gleichzeitig als oberste Einträge.
func TestDeleteParallelDirectoryOnlyAfterContent(t *testing.T) {
	fsys := newParDeleteFS()
	fsys.removeDelay = 2 * time.Millisecond
	total := 0
	for _, root := range []string{"/home/a/t1", "/home/a/t2", "/home/a/t3"} {
		total += parAddTree(fsys.memFS, root, 3, 2)
	}
	fsys.addFile("/home/a/bleibt.txt", "x", 0o644, copyTestTime)
	result := runDeleteSync(t, context.Background(), fsys, "t1", "t2", "t3")
	if len(result.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(result.failures))
	}
	if len(fsys.violations) != 0 {
		t.Errorf("Verzeichnis vor seinem Inhalt entfernt: %v", fsys.violations)
	}
	if result.deleted != total {
		t.Errorf("deleted = %d, erwartet %d", result.deleted, total)
	}
	if !reflect.DeepEqual(result.deletedNames, []string{"t1", "t2", "t3"}) {
		t.Errorf("deletedNames = %v", result.deletedNames)
	}
	// Nur die nicht gewählte Datei und die Grundverzeichnisse sind übrig
	if left := parNodeCount(fsys.memFS); left != 4 { // "/", "/home", "/home/a", bleibt.txt
		t.Errorf("%d Einträge übrig, erwartet 4 (/, /home, /home/a, bleibt.txt)", left)
	}
}

// parMixedTop legt 30 Dateien f00..f29 und 5 Verzeichnisse d0..d4 (je mit den
// Dateien in1 und in2) in /home/a an und liefert die Namen in verdrehter
// Reihenfolge (Eingabe ist nicht sortiert) sowie die Zahl aller Einträge (45).
func parMixedTop(m *memFS) ([]string, int) {
	var names []string
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("f%02d", i)
		m.addFile("/home/a/"+name, "x", 0o644, copyTestTime)
		names = append(names, name)
	}
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("d%d", i)
		m.addFile("/home/a/"+name+"/in1", "x", 0o644, copyTestTime)
		m.addFile("/home/a/"+name+"/in2", "x", 0o644, copyTestTime)
		// Verzeichnisse zwischen die Dateien mischen
		pos := (i*7 + 3) % len(names)
		names = append(names[:pos], append([]string{name}, names[pos:]...)...)
	}
	for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
		names[i], names[j] = names[j], names[i]
	}
	return names, 45
}

// TestDeleteParallelManyTopEntriesKeepInputOrder - 30 Dateien und 5
// Verzeichnisse als oberste Einträge, in gemischter Reihenfolge: alle werden
// gelöscht, deletedNames hat genau die Reihenfolge der Eingabe. Die obersten
// Einträge werden gleichzeitig gelesen (Lstat), aber nie gleichzeitig entfernt.
func TestDeleteParallelManyTopEntriesKeepInputOrder(t *testing.T) {
	fsys := newParDeleteFS()
	fsys.lstatDelay, fsys.removeDelay = 3*time.Millisecond, 3*time.Millisecond
	names, total := parMixedTop(fsys.memFS)
	result := runDeleteSync(t, context.Background(), fsys, names...)
	if len(result.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(result.failures))
	}
	if !reflect.DeepEqual(result.deletedNames, names) {
		t.Errorf("deletedNames in falscher Reihenfolge:\n erhalten: %v\n erwartet: %v", result.deletedNames, names)
	}
	if result.deleted != total {
		t.Errorf("deleted = %d, erwartet %d", result.deleted, total)
	}
	fsys.assertReadBounds(t, deleteParallel)
}

// TestDeleteParallelLimitIsVariable - deleteParallel ist eine Variable: mit 4
// laufen höchstens 4 Lesezugriffe gleichzeitig (Lstat und ReadDir, auch
// zusammen), aber mehr als einer; mit 1 genau einer. Remove bleibt immer bei
// genau 1. Der Wert wird per defer zurückgesetzt. Das Ergebnis ist in allen
// Fällen dasselbe: 1 Verzeichnis mit 60 leeren Unterverzeichnissen weg.
func TestDeleteParallelLimitIsVariable(t *testing.T) {
	defer func(limit int) { deleteParallel = limit }(deleteParallel)
	for _, limit := range []int{4, 1} {
		deleteParallel = limit
		fsys := newParDeleteFS()
		fsys.lstatDelay, fsys.readDelay = 2*time.Millisecond, 2*time.Millisecond
		for i := 0; i < 60; i++ {
			fsys.addDir(fmt.Sprintf("/home/a/baum/d%02d", i), 0o755)
		}
		result := runDeleteSync(t, context.Background(), fsys, "baum")
		if len(result.failures) != 0 || result.deleted != 61 || !reflect.DeepEqual(result.deletedNames, []string{"baum"}) {
			t.Fatalf("Grenze %d: deleted=%d, Namen=%v, Fehler: %s", limit, result.deleted, result.deletedNames, failureErrors(result.failures))
		}
		if peak := fsys.reads.peak.Load(); peak > int64(limit) {
			t.Errorf("Grenze %d: %d gleichzeitige Lesezugriffe", limit, peak)
		}
		if limit > 1 {
			if peak := fsys.lstats.peak.Load(); peak <= 1 {
				t.Errorf("Grenze %d: Lstat nie gleichzeitig (Höchststand %d)", limit, peak)
			}
			if peak := fsys.readDirs.peak.Load(); peak <= 1 {
				t.Errorf("Grenze %d: ReadDir nie gleichzeitig (Höchststand %d)", limit, peak)
			}
		} else if peak := fsys.reads.peak.Load(); peak != 1 {
			t.Errorf("Grenze 1: Höchststand %d, erwartet genau 1", peak)
		}
		if peak := fsys.removes.peak.Load(); peak != 1 {
			t.Errorf("Grenze %d: %d gleichzeitige Remove-Aufrufe, erwartet 1", limit, peak)
		}
		if parNodeCount(fsys.memFS) != 3 { // "/", "/home", "/home/a"
			t.Errorf("Grenze %d: Baum nicht vollständig gelöscht", limit)
		}
	}
}

// TestDeletePrefetchReadsFasterThanSequential - 1 Verzeichnis mit 60 leeren
// Unterverzeichnissen, jeder Lstat und jedes ReadDir dauert 3 ms: ReadDir und
// Lstat laufen gleichzeitig (Höchststand > 1), und die Gesamtzeit liegt
// deutlich unter der Summe aller Lesezugriffe nacheinander (großzügig:
// höchstens 80 % davon; erwartet sind etwa 25 %, weil das Löschen selbst
// mit seiner frischen Prüfung je Eintrag der Reihe nach bleibt).
func TestDeletePrefetchReadsFasterThanSequential(t *testing.T) {
	fsys := newParDeleteFS()
	const delay = 3 * time.Millisecond
	fsys.lstatDelay, fsys.readDelay = delay, delay
	for i := 0; i < 60; i++ {
		fsys.addDir(fmt.Sprintf("/home/a/baum/d%02d", i), 0o755)
	}
	start := time.Now()
	result := runDeleteSync(t, context.Background(), fsys, "baum")
	elapsed := time.Since(start)
	if len(result.failures) != 0 || result.deleted != 61 {
		t.Fatalf("deleted=%d, Fehler: %s", result.deleted, failureErrors(result.failures))
	}
	if peak := fsys.readDirs.peak.Load(); peak <= 1 {
		t.Errorf("ReadDir nie gleichzeitig (Höchststand %d)", peak)
	}
	if peak := fsys.lstats.peak.Load(); peak <= 1 {
		t.Errorf("Lstat nie gleichzeitig (Höchststand %d)", peak)
	}
	sequential := time.Duration(fsys.delayedReads.Load()) * delay
	if elapsed > sequential*8/10 {
		t.Errorf("Löschen dauerte %v, die Lesezugriffe allein nacheinander %v (erwartet höchstens 80 %%)", elapsed, sequential)
	}
}

// parOrderTree legt den Baum für TestDeleteRemoveOrderMatchesSequentialDepthFirst
// an und liefert die oberste Eingabe (Reihenfolge von req.names) sowie die
// erwartete Reihenfolge der Remove-Aufrufe.
func parOrderTree(m *memFS) (names, wantOrder []string) {
	for _, f := range []string{
		"zeta/b.txt", "zeta/B/y.txt", "zeta/B/x/deep.txt", "zeta/a/2", "zeta/a/1", "zeta/a.txt",
		"alpha.txt", "mid/m.txt",
	} {
		m.addFile("/home/a/"+f, "x", 0o644, copyTestTime)
	}
	m.addDir("/home/a/mid/n", 0o755) // leeres Verzeichnis zwischen Dateien
	// Post-Order der nach Namen (Bytes: "B" vor "a") sortierten Tiefensuche,
	// Inhalt vor dem Verzeichnis; oberste Einträge wie eingegeben
	wantOrder = []string{
		"zeta/B/x/deep.txt", "zeta/B/x", "zeta/B/y.txt", "zeta/B",
		"zeta/a/1", "zeta/a/2", "zeta/a",
		"zeta/a.txt", "zeta/b.txt", "zeta",
		"alpha.txt",
		"mid/m.txt", "mid/n", "mid",
	}
	for i := range wantOrder {
		wantOrder[i] = "/home/a/" + wantOrder[i]
	}
	return []string{"zeta", "alpha.txt", "mid"}, wantOrder
}

// TestDeleteRemoveOrderMatchesSequentialDepthFirst - Die Reihenfolge der
// Remove-Aufrufe ist exakt die der alten rein sequentiellen Version: Post-
// Order der nach Namen sortierten Tiefensuche (Inhalt vor Verzeichnis,
// Geschwister nach Namen, Dateien und Verzeichnisse gemischt), oberste
// Einträge in der Reihenfolge von req.names. Egal, wie schnell die Lesezugriffe
// je Pfad fertig werden (hier absichtlich verschieden und verdreht): fünf
// Läufe mit unterschiedlichem Zeitverhalten ergeben immer dieselbe Liste.
func TestDeleteRemoveOrderMatchesSequentialDepthFirst(t *testing.T) {
	for run := 0; run < 5; run++ {
		fsys := newParDeleteFS()
		fsys.readDelay = time.Millisecond
		fsys.lstatDelayFor = func(p string) time.Duration {
			sum := run
			for _, c := range []byte(p) {
				sum += int(c)
			}
			return time.Duration(sum%5) * time.Millisecond
		}
		names, want := parOrderTree(fsys.memFS)
		result := runDeleteSync(t, context.Background(), fsys, names...)
		if len(result.failures) != 0 {
			t.Fatalf("Lauf %d: Fehler: %s", run, failureErrors(result.failures))
		}
		if got := fsys.removedPaths(); !reflect.DeepEqual(got, want) {
			t.Fatalf("Lauf %d: Reihenfolge der Remove-Aufrufe:\n erhalten: %q\n erwartet: %q", run, got, want)
		}
		if !reflect.DeepEqual(result.deletedNames, names) || result.deleted != len(want) {
			t.Errorf("Lauf %d: deletedNames=%v, deleted=%d", run, result.deletedNames, result.deleted)
		}
	}
}

// parWithout liefert names ohne skip (Reihenfolge bleibt).
func parWithout(names []string, skip ...string) []string {
	var out []string
	for _, n := range names {
		keep := true
		for _, s := range skip {
			keep = keep && n != s
		}
		if keep {
			out = append(out, n)
		}
	}
	return out
}

// TestDeleteParallelTopEntryRemoveErrorKeepsOthers - scheitert Remove bei
// einer obersten Datei, fehlt nur sie in deletedNames; genau ein Fehler mit
// ihrem Pfad, alles andere wird gelöscht und in Eingabereihenfolge gemeldet.
func TestDeleteParallelTopEntryRemoveErrorKeepsOthers(t *testing.T) {
	fsys := newParDeleteFS()
	fsys.removeDelay = 3 * time.Millisecond
	names, total := parMixedTop(fsys.memFS)
	fsys.failRemove["/home/a/f07"] = errors.New("gesperrt")
	result := runDeleteSync(t, context.Background(), fsys, names...)
	if len(result.failures) != 1 || result.failures[0].path != "/home/a/f07" {
		t.Fatalf("erwartet genau 1 Fehler für f07, erhalten: %s", failureErrors(result.failures))
	}
	if want := parWithout(names, "f07"); !reflect.DeepEqual(result.deletedNames, want) {
		t.Errorf("deletedNames:\n erhalten: %v\n erwartet: %v", result.deletedNames, want)
	}
	if result.deleted != total-1 {
		t.Errorf("deleted = %d, erwartet %d", result.deleted, total-1)
	}
	if fsys.node("/home/a/f07") == nil {
		t.Error("f07 trotz Fehler verschwunden")
	}
}

// TestDeleteParallelNestedErrorKeepsDirectory - scheitert das Löschen einer
// Datei in einem Unterverzeichnis, bleibt dieses Verzeichnis stehen, ohne
// eigenen zweiten Fehler (wie bisher); die Geschwister (in2) und die anderen
// Einträge werden gelöscht, d2 fehlt in deletedNames.
func TestDeleteParallelNestedErrorKeepsDirectory(t *testing.T) {
	fsys := newParDeleteFS()
	fsys.removeDelay = 3 * time.Millisecond
	names, total := parMixedTop(fsys.memFS)
	fsys.failRemove["/home/a/d2/in1"] = errors.New("gesperrt")
	result := runDeleteSync(t, context.Background(), fsys, names...)
	if len(result.failures) != 1 || result.failures[0].path != "/home/a/d2/in1" {
		t.Fatalf("erwartet genau 1 Fehler für d2/in1, erhalten: %s", failureErrors(result.failures))
	}
	if want := parWithout(names, "d2"); !reflect.DeepEqual(result.deletedNames, want) {
		t.Errorf("deletedNames:\n erhalten: %v\n erwartet: %v", result.deletedNames, want)
	}
	if result.deleted != total-2 { // d2/in1 und d2 bleiben
		t.Errorf("deleted = %d, erwartet %d", result.deleted, total-2)
	}
	if fsys.node("/home/a/d2") == nil || fsys.node("/home/a/d2/in1") == nil {
		t.Error("d2 oder d2/in1 verschwunden")
	}
	if fsys.node("/home/a/d2/in2") != nil {
		t.Error("in2 neben der gescheiterten Datei nicht gelöscht")
	}
}

// TestDeleteParallelEntryLimitGivesExactlyOneError - Grenze copyMaxEntries
// beim gleichzeitigen Löschen: genau EIN errCopyLimit-Fehler (nicht einer je
// Arbeiter), höchstens copyMaxEntries Einträge gelöscht, nichts außerhalb
// der gewählten Einträge angefasst, keiner der beiden zu großen Bäume gilt
// als vollständig gelöscht. Mehrere Läufe, weil der Zeitpunkt, zu dem die
// Arbeiter die Grenze erreichen, zufällig ist.
func TestDeleteParallelEntryLimitGivesExactlyOneError(t *testing.T) {
	defer func(entries int) { copyMaxEntries = entries }(copyMaxEntries)
	copyMaxEntries = 10
	for run := 0; run < 5; run++ {
		fsys := newParDeleteFS()
		fsys.removeDelay = 2 * time.Millisecond
		for i := 0; i < 30; i++ {
			fsys.addFile(fmt.Sprintf("/home/a/gross/f%02d", i), "x", 0o644, copyTestTime)
		}
		for i := 0; i < 20; i++ {
			fsys.addFile(fmt.Sprintf("/home/a/zweit/g%02d", i), "x", 0o644, copyTestTime)
		}
		for i := 0; i < 5; i++ {
			fsys.addFile(fmt.Sprintf("/home/a/fremd/h%d", i), "x", 0o644, copyTestTime)
		}
		fsys.addFile("/home/a/lose.txt", "x", 0o644, copyTestTime)
		before := parNodeCount(fsys.memFS)
		result := runDeleteSync(t, context.Background(), fsys, "gross", "zweit")
		if len(result.failures) != 1 || !errors.Is(result.failures[0].err, errCopyLimit) {
			t.Fatalf("Lauf %d: erwartet genau 1 errCopyLimit, erhalten: %s", run, failureErrors(result.failures))
		}
		if result.deleted > copyMaxEntries {
			t.Errorf("Lauf %d: %d Einträge gelöscht, Grenze %d", run, result.deleted, copyMaxEntries)
		}
		if gone := before - parNodeCount(fsys.memFS); gone != result.deleted {
			t.Errorf("Lauf %d: %d Einträge verschwunden, aber deleted = %d", run, gone, result.deleted)
		}
		if len(result.deletedNames) != 0 {
			t.Errorf("Lauf %d: deletedNames = %v, erwartet leer", run, result.deletedNames)
		}
		for i := 0; i < 5; i++ {
			if fsys.node(fmt.Sprintf("/home/a/fremd/h%d", i)) == nil {
				t.Errorf("Lauf %d: nicht gewählter Eintrag fremd/h%d gelöscht", run, i)
			}
		}
		if fsys.node("/home/a/lose.txt") == nil {
			t.Errorf("Lauf %d: lose.txt gelöscht", run)
		}
	}
}

// TestDeleteParallelDepthLimitGivesExactlyOneError - Tiefengrenze: acht
// Geschwister auf der verbotenen Ebene erreichen sie gleichzeitig, gemeldet
// wird trotzdem nur einmal; das oberste Verzeichnis bleibt, nichts darunter
// auf der verbotenen Ebene wird gelöscht.
func TestDeleteParallelDepthLimitGivesExactlyOneError(t *testing.T) {
	defer func(depth int) { copyMaxDepth = depth }(copyMaxDepth)
	copyMaxDepth = 2
	fsys := newParDeleteFS()
	fsys.removeDelay = 2 * time.Millisecond
	for i := 0; i < 8; i++ {
		fsys.addFile(fmt.Sprintf("/home/a/oben/a/b%d/c/datei", i), "x", 0o644, copyTestTime)
	}
	result := runDeleteSync(t, context.Background(), fsys, "oben")
	if len(result.failures) != 1 || !errors.Is(result.failures[0].err, errCopyLimit) {
		t.Fatalf("erwartet genau 1 errCopyLimit, erhalten: %s", failureErrors(result.failures))
	}
	if fsys.node("/home/a/oben") == nil || len(result.deletedNames) != 0 {
		t.Errorf("oberstes Verzeichnis gelöscht oder gemeldet: %v", result.deletedNames)
	}
	for i := 0; i < 8; i++ {
		if fsys.node(fmt.Sprintf("/home/a/oben/a/b%d/c/datei", i)) == nil {
			t.Errorf("b%d/c/datei jenseits der Tiefengrenze gelöscht", i)
		}
	}
}

// TestDeleteParallelCancelStopsEarly - der Abbruch während des Löschens hält
// die Arbeit an: gelöscht wird nur von einer Goroutine, nach dem Abbruch (im
// 10. Remove) läuft also höchstens noch dieses eine Remove zu Ende. Der Baum
// bleibt zum großen Teil stehen, das Ergebnis meldet den Abbruch und stimmt
// mit dem Dateisystem überein.
func TestDeleteParallelCancelStopsEarly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fsys := newParDeleteFS()
	fsys.removeDelay = 3 * time.Millisecond
	fsys.onRemove = func(n int64) {
		if n == 10 {
			cancel()
		}
	}
	for i := 0; i < 200; i++ {
		fsys.addFile(fmt.Sprintf("/home/a/baum/f%03d", i), "x", 0o644, copyTestTime)
	}
	before := parNodeCount(fsys.memFS)
	start := time.Now()
	result := runDeleteSync(t, ctx, fsys, "baum")
	if !result.cancelled {
		t.Error("Abbruch nicht gemeldet")
	}
	if calls := fsys.calls.Load(); calls > 10 {
		t.Errorf("%d Remove-Aufrufe, nach dem Abbruch beim 10. darf keiner mehr folgen", calls)
	}
	if fsys.node("/home/a/baum") == nil || len(result.deletedNames) != 0 {
		t.Errorf("abgebrochener Baum als gelöscht gemeldet: %v", result.deletedNames)
	}
	if gone := before - parNodeCount(fsys.memFS); gone != result.deleted {
		t.Errorf("%d Einträge verschwunden, aber deleted = %d", gone, result.deleted)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Abbruch dauerte %v", elapsed)
	}
}

// TestDeleteParallelStableWithoutDelay - ohne Verzögerung (Wettlauf der
// Goroutinen beim Vorlesen am engsten) 10-mal 200 Dateien: immer alles weg,
// immer dieselbe Zählung. Mit -race prüft das die gemeinsamen Zähler.
func TestDeleteParallelStableWithoutDelay(t *testing.T) {
	for run := 0; run < 10; run++ {
		fsys := newParDeleteFS()
		for i := 0; i < 200; i++ {
			fsys.addFile(fmt.Sprintf("/home/a/f%03d", i), "x", 0o644, copyTestTime)
		}
		fsys.addFile("/home/a/ordner/x/y", "x", 0o644, copyTestTime)
		names := []string{"ordner"}
		for i := 0; i < 200; i++ {
			names = append(names, fmt.Sprintf("f%03d", i))
		}
		result := runDeleteSync(t, context.Background(), fsys, names...)
		if len(result.failures) != 0 || result.deleted != 200+3 || len(result.deletedNames) != 201 {
			t.Fatalf("Lauf %d: deleted=%d, Namen=%d, Fehler: %s", run, result.deleted, len(result.deletedNames), failureErrors(result.failures))
		}
		if left := parNodeCount(fsys.memFS); left != 3 { // "/", "/home", "/home/a"
			t.Fatalf("Lauf %d: %d Einträge übrig", run, left)
		}
	}
}

// --- B-b: scan liest Verzeichnisse gleichzeitig ---

// parReadDirFS verzögert ReadDir (fest oder je Pfad), zählt gleichzeitige und
// alle Aufrufe und ruft optional einen Haken mit der laufenden Nummer auf.
type parReadDirFS struct {
	*memFS
	readDelay time.Duration
	delayFor  func(p string) time.Duration
	reads     parGauge
	calls     atomic.Int64
	onReadDir func(n int64)
}

func (f *parReadDirFS) ReadDir(p string) ([]os.FileInfo, error) {
	n := f.calls.Add(1)
	if f.onReadDir != nil {
		f.onReadDir(n)
	}
	f.reads.enter()
	defer f.reads.leave()
	delay := f.readDelay
	if f.delayFor != nil {
		delay = f.delayFor(p)
	}
	time.Sleep(delay)
	return f.memFS.ReadDir(p)
}

// parWideTree legt /home/a/wurzel mit dirs leeren Unterverzeichnissen d00 ...
// an, dazu ein paar Ebenen tiefer (d00/e0/e1/e2) und wenige Dateien. Liefert
// die Zahl der Verzeichnisse unter wurzel (ohne wurzel).
func parWideTree(m *memFS, dirs int) int {
	m.addDir("/home/a/wurzel", 0o755)
	count := 0
	for i := 0; i < dirs; i++ {
		m.addDir(fmt.Sprintf("/home/a/wurzel/d%02d", i), 0o755)
		count++
	}
	m.addDir("/home/a/wurzel/d00/e0/e1/e2", 0o755)
	count += 3
	m.addFile("/home/a/wurzel/oben.txt", "oben", 0o644, copyTestTime)
	m.addFile("/home/a/wurzel/d01/f.txt", "eins", 0o644, copyTestTime)
	m.addFile("/home/a/wurzel/d00/e0/g.txt", "tief", 0o600, copyTestTime)
	return count
}

// TestCopyScanParallelReadsDirectories - 60 leere Unterverzeichnisse, ReadDir
// dauert 3 ms: mehrere ReadDir laufen gleichzeitig (höchstens copyParallel),
// das Ziel ist danach vollständig und identisch, ohne Fehler. Mit move
// zusätzlich: die Quelle ist weg.
func TestCopyScanParallelReadsDirectories(t *testing.T) {
	for _, move := range []bool{false, true} {
		t.Run(fmt.Sprintf("move=%v", move), func(t *testing.T) {
			src := &parReadDirFS{memFS: newMemFS("/home/a"), readDelay: 3 * time.Millisecond}
			parWideTree(src.memFS, 60)
			want := treeSnapshot(src.memFS, "/home/a/wurzel")
			dst := newMemFS("/srv/b")
			req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"wurzel"}, move: move}
			res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
			if len(res.failures) != 0 {
				t.Fatalf("Fehler: %s", failureErrors(res.failures))
			}
			if peak := src.reads.peak.Load(); peak <= 1 || peak > int64(copyParallel) {
				t.Errorf("gleichzeitige ReadDir-Aufrufe: %d, erwartet 2..%d", peak, copyParallel)
			}
			if got := treeSnapshot(dst, "/srv/b/wurzel"); !reflect.DeepEqual(got, want) {
				t.Errorf("Ziel weicht von der Quelle ab (%d statt %d Einträge)", len(got), len(want))
			}
			if !reflect.DeepEqual(res.copiedNames, []string{"wurzel"}) {
				t.Errorf("copiedNames = %v", res.copiedNames)
			}
			if move && src.node("/home/a/wurzel") != nil {
				t.Error("Quelle nach dem Verschieben noch da")
			}
			if !move && src.node("/home/a/wurzel/d01/f.txt") == nil {
				t.Error("Kopieren hat die Quelle verändert")
			}
			assertNoTempFiles(t, dst)
		})
	}
}

// parMkdirLogFS merkt die Reihenfolge erfolgreicher Mkdir-Aufrufe.
type parMkdirLogFS struct {
	*memFS
	mu    sync.Mutex
	order []string
}

func (f *parMkdirLogFS) Mkdir(p string) error {
	err := f.memFS.Mkdir(p)
	if err == nil {
		f.mu.Lock()
		f.order = append(f.order, p)
		f.mu.Unlock()
	}
	return err
}

// TestCopyScanParallelKeepsDepthFirstOrder - zwei Dinge, die der parallele
// Scan nicht verändern darf (Wächter, vorher wie nachher grün):
//  1. Am Ziel kommt jedes Elternverzeichnis vor seinen Kindern.
//  2. Die Fehlermeldungen zu unsicheren Namen (Backslash) in drei
//     Unterverzeichnissen stehen in der Reihenfolge der sortierten
//     Tiefensuche - auch wenn die späten Verzeichnisse schneller gelesen
//     werden als die frühen (Verzögerung sinkt mit dem Namen).
func TestCopyScanParallelKeepsDepthFirstOrder(t *testing.T) {
	src := &parReadDirFS{memFS: newMemFS("/home/a")}
	src.delayFor = func(p string) time.Duration {
		var idx int
		if _, err := fmt.Sscanf(path.Base(p), "d%d", &idx); err != nil {
			return 0
		}
		return time.Duration(20-idx) * time.Millisecond
	}
	src.addDir("/home/a/wurzel", 0o755)
	for i := 0; i < 20; i++ {
		src.addDir(fmt.Sprintf("/home/a/wurzel/d%02d/tief/tiefer", i), 0o755)
		src.addFile(fmt.Sprintf("/home/a/wurzel/d%02d/ok.txt", i), "ok", 0o644, copyTestTime)
	}
	for _, i := range []int{3, 11, 17} {
		src.addFile(fmt.Sprintf("/home/a/wurzel/d%02d/x\\y", i), "böse", 0o644, copyTestTime)
	}
	dst := &parMkdirLogFS{memFS: newMemFS("/srv/b")}
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"wurzel"}}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))

	var got []string
	for _, f := range res.failures {
		if !errors.Is(f.err, errUnsafeName) {
			t.Errorf("unerwarteter Fehler: %s: %v", f.path, f.err)
		}
		got = append(got, f.path)
	}
	want := []string{"/home/a/wurzel/d03/x\\y", "/home/a/wurzel/d11/x\\y", "/home/a/wurzel/d17/x\\y"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Reihenfolge der Fehler:\n erhalten: %q\n erwartet: %q", got, want)
	}
	created := map[string]bool{"/srv/b": true}
	for _, p := range dst.order {
		if !created[path.Dir(p)] {
			t.Errorf("%s angelegt, bevor sein Elternverzeichnis %s da war", p, path.Dir(p))
		}
		created[p] = true
	}
	if len(dst.order) != 1+20*3 { // wurzel, je dNN/tief/tiefer
		t.Errorf("%d Verzeichnisse angelegt, erwartet %d", len(dst.order), 1+20*3)
	}
	if _, ok := dst.content("/srv/b/wurzel/d05/ok.txt"); !ok {
		t.Error("ok.txt nicht kopiert")
	}
	if dst.node("/srv/b/wurzel/d03/x\\y") != nil {
		t.Error("unsicherer Name am Ziel angelegt")
	}
}

// TestCopyScanParallelCancelStopsReading - der Abbruch während des Erfassens
// (im 5. ReadDir) beendet runCopy schnell: nach dem Abbruch werden höchstens
// die schon laufenden ReadDir-Aufrufe fertig (keine 2000), das Ergebnis meldet
// den Abbruch, am Ziel entsteht nichts (auch keine Temp-Dateien).
func TestCopyScanParallelCancelStopsReading(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := &parReadDirFS{memFS: newMemFS("/home/a"), readDelay: 3 * time.Millisecond}
	src.addDir("/home/a/wurzel", 0o755)
	for i := 0; i < 2000; i++ {
		src.addDir(fmt.Sprintf("/home/a/wurzel/d%04d", i), 0o755)
	}
	src.onReadDir = func(n int64) {
		if n == 5 {
			cancel()
		}
	}
	dst := newMemFS("/srv/b")
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"wurzel"}}
	start := time.Now()
	res, _ := runCopySync(t, ctx, req, noConflicts(t))
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Abbruch dauerte %v", elapsed)
	}
	if !res.cancelled {
		t.Error("Abbruch nicht gemeldet")
	}
	if calls := src.calls.Load(); calls > int64(5+3*copyParallel) {
		t.Errorf("%d ReadDir-Aufrufe, nach dem Abbruch (bei 5) höchstens %d weitere erwartet", calls, 3*copyParallel)
	}
	if n := parNodeCount(dst); n != 3 { // "/", "/srv", "/srv/b"
		t.Errorf("am Ziel %d Einträge, erwartet 3 (nichts angelegt)", n)
	}
	if len(res.copiedNames) != 0 {
		t.Errorf("copiedNames = %v nach Abbruch", res.copiedNames)
	}
	assertNoTempFiles(t, dst)
}

// --- B-d: removeMovedDirs löscht ebenenweise gleichzeitig ---

// parRemoveDirFS verzögert das Löschen von Verzeichnissen (RemoveDir), zählt
// gleichzeitige Aufrufe und merkt, wenn ein Verzeichnis mit Kindern entfernt
// werden soll oder ein Verzeichnis per Remove statt RemoveDir gelöscht wird.
type parRemoveDirFS struct {
	*memFS
	delay      time.Duration
	dirRemoves parGauge
	mu         sync.Mutex
	violations []string
}

// Remove auf ein Verzeichnis ist eine Verletzung: removeMovedDirs darf
// Verzeichnisse nur noch per RemoveDir (rmdir) löschen, nie per Remove
// (das bei einer Datei oder einem Symlink unlink ausführen würde).
func (f *parRemoveDirFS) Remove(p string) error {
	if node := f.memFS.node(p); node != nil && node.mode.IsDir() {
		f.mu.Lock()
		f.violations = append(f.violations, fmt.Sprintf("%s: Remove auf ein Verzeichnis statt RemoveDir", p))
		f.mu.Unlock()
	}
	return f.memFS.Remove(p)
}

// RemoveDir zählt die gleichzeitigen Verzeichnis-Löschungen, meldet ein
// Verzeichnis mit Kindern als Verletzung und verzögert wie ein Server.
func (f *parRemoveDirFS) RemoveDir(p string) error {
	f.dirRemoves.enter()
	defer f.dirRemoves.leave()
	if node := f.memFS.node(p); node != nil && node.mode.IsDir() {
		if kids, _ := f.memFS.ReadDir(p); len(kids) > 0 {
			f.mu.Lock()
			f.violations = append(f.violations, fmt.Sprintf("%s hat noch %d Kinder", p, len(kids)))
			f.mu.Unlock()
		}
	}
	time.Sleep(f.delay)
	return f.memFS.RemoveDir(p)
}

// TestMoveRemoveDirsParallelByLevel - Verschieben eines Baums mit 60 leeren
// Unterverzeichnissen (und einer tieferen Kette): die Quellverzeichnisse
// werden gleichzeitig gelöscht (Höchststand > 1, höchstens copyParallel), nie
// ein Verzeichnis, das noch Kinder hat. Danach ist die Quelle weg, das Ziel
// vollständig, ohne Fehler.
func TestMoveRemoveDirsParallelByLevel(t *testing.T) {
	src := &parRemoveDirFS{memFS: newMemFS("/home/a"), delay: 3 * time.Millisecond}
	parWideTree(src.memFS, 60)
	want := treeSnapshot(src.memFS, "/home/a/wurzel")
	dst := newMemFS("/srv/b")
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"wurzel"}, move: true}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(res.failures))
	}
	if peak := src.dirRemoves.peak.Load(); peak <= 1 || peak > int64(copyParallel) {
		t.Errorf("gleichzeitige Verzeichnis-Löschungen: %d, erwartet 2..%d", peak, copyParallel)
	}
	if len(src.violations) != 0 {
		t.Errorf("Verzeichnis mit Inhalt entfernt: %v", src.violations)
	}
	if src.node("/home/a/wurzel") != nil {
		t.Error("Quellverzeichnis nach dem Verschieben noch da")
	}
	if got := treeSnapshot(dst, "/srv/b/wurzel"); !reflect.DeepEqual(got, want) {
		t.Errorf("Ziel weicht von der Quelle ab (%d statt %d Einträge)", len(got), len(want))
	}
	if !reflect.DeepEqual(res.copiedNames, []string{"wurzel"}) {
		t.Errorf("copiedNames = %v", res.copiedNames)
	}
}

// TestMoveRemoveDirsParallelKeepsNonEmptyParents - eine Datei tief unten
// lässt sich in der Quelle nicht löschen (Remove scheitert). Dann bleiben
// genau ihre Elternverzeichnisse stehen, die übrigen leeren Verzeichnisse
// werden entfernt. Es gibt nur den Fehler der Datei, keinen zusätzlichen für
// die nicht leeren Verzeichnisse; der oberste Eintrag ist nicht in
// copiedNames (unvollständig). Das Ziel hat trotzdem alles.
func TestMoveRemoveDirsParallelKeepsNonEmptyParents(t *testing.T) {
	src := &parRemoveDirFS{memFS: newMemFS("/home/a"), delay: 2 * time.Millisecond}
	parWideTree(src.memFS, 60)
	src.addFile("/home/a/wurzel/d05/sub/keep.txt", "bleibt", 0o644, copyTestTime)
	src.noRemove["/home/a/wurzel/d05/sub/keep.txt"] = true
	want := treeSnapshot(src.memFS, "/home/a/wurzel")
	dst := newMemFS("/srv/b")
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"wurzel"}, move: true}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 1 || res.failures[0].path != "/home/a/wurzel/d05/sub/keep.txt" {
		t.Fatalf("erwartet genau 1 Fehler für keep.txt, erhalten: %s", failureErrors(res.failures))
	}
	if len(res.copiedNames) != 0 {
		t.Errorf("copiedNames = %v, erwartet leer (unvollständig)", res.copiedNames)
	}
	for _, p := range []string{"/home/a/wurzel", "/home/a/wurzel/d05", "/home/a/wurzel/d05/sub", "/home/a/wurzel/d05/sub/keep.txt"} {
		if src.node(p) == nil {
			t.Errorf("%s fehlt, müsste stehen bleiben", p)
		}
	}
	var left []string
	for p := range treeSnapshot(src.memFS, "/home/a/wurzel") {
		left = append(left, p)
	}
	sort.Strings(left)
	if wantLeft := []string{"", "d05", "d05/sub", "d05/sub/keep.txt"}; !reflect.DeepEqual(left, wantLeft) {
		t.Errorf("in der Quelle übrig: %q, erwartet %q", left, wantLeft)
	}
	if got := treeSnapshot(dst, "/srv/b/wurzel"); !reflect.DeepEqual(got, want) {
		t.Errorf("Ziel unvollständig (%d statt %d Einträge)", len(got), len(want))
	}
}
