// Paket main - Tests: Eintragsgrenze, Sortierung, Abbruch und Umbenennen
// (Build 90, Nachrunde nach dem Fable-Review und der Mutationsprüfung)
//
// Bündelt Testfälle zu Grenzen und Reihenfolgen beim Erfassen (scan), beim
// Löschen (runDelete) und beim Verschieben (runCopy mit move):
//   - Eintragsgrenze copyMaxEntries: genau N Einträge gehen, N+1 nicht
//     (SFTP-Erfassen ohne find und Löschen); die ersten Einträge eines zu
//     großen Verzeichnisses werden beim Löschen wie bis Build 89 noch gelöscht
//     (Lesen bei Bedarf), dann folgt genau EIN errCopyLimit
//   - Unsichere Namen zählen nicht zur Grenze: SFTP-Erfassen, find-Weg
//     (auch ein unsicheres Verzeichnis mit sehr vielen Einträgen) und Löschen
//   - Geschwister werden nach Namen sortiert, auch wenn der Server unsortiert
//     liefert (ReadDir ist bei SFTP nicht sortiert)
//   - Abbruch mitten in der Lösch-Phase des Verschiebens (removeMovedDirs)
//   - Umbenannte Einträge bleiben nach einem Abbruch in copiedNames
//
// @author Kurt Ingwer
// @date   2026-11-15 15:14
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
)

// --- Hilfen ---

// limitKeep merkt die Grenzen und Arbeiterzahlen; der Rückgabewert stellt sie
// wieder her (defer limitKeep()()).
func limitKeep() func() {
	entries, depth, parallel, delParallel := copyMaxEntries, copyMaxDepth, copyParallel, deleteParallel
	return func() {
		copyMaxEntries, copyMaxDepth, copyParallel, deleteParallel = entries, depth, parallel, delParallel
	}
}

// limitFlat legt unter /home/a/proj ein Verzeichnis mit n Einträgen INSGESAMT
// an (das Verzeichnis selbst mitgezählt): proj und n-1 Dateien f00, f01 ...
func limitFlat(m *memFS, n int) {
	m.addDir("/home/a/proj", 0o755)
	for i := 0; i < n-1; i++ {
		m.addFile(fmt.Sprintf("/home/a/proj/f%02d", i), "x", 0o644, copyTestTime)
	}
}

// limitNested legt unter /home/a/proj n Einträge INSGESAMT in zwei Ebenen an:
// proj, proj/sub und n-2 Dateien, abwechselnd in proj und proj/sub (n >= 2).
func limitNested(m *memFS, n int) {
	m.addDir("/home/a/proj", 0o755)
	m.addDir("/home/a/proj/sub", 0o755)
	for i := 2; i < n; i++ {
		dir := "/home/a/proj"
		if i%2 == 0 {
			dir += "/sub"
		}
		m.addFile(fmt.Sprintf("%s/f%02d", dir, i), "x", 0o644, copyTestTime)
	}
}

// limitCopy kopiert (oder verschiebt) names von /home/a nach /srv/b.
func limitCopy(t *testing.T, src, dst remoteFS, move bool, names ...string) copyResult {
	t.Helper()
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: names, move: move}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	return res
}

// countErr zählt die Fehler eines Ergebnisses, die err sind.
func countErr(failures []copyFailure, err error) int {
	n := 0
	for _, f := range failures {
		if errors.Is(f.err, err) {
			n++
		}
	}
	return n
}

// --- 5. Exakte Eintragsgrenze: N geht, N+1 nicht ---

// TestCopyScanEntryLimitExactBoundary - SFTP-Erfassen (das Fake hat KEIN
// ListTree, also kein find): Mit copyMaxEntries = N wird ein Baum mit genau N
// Einträgen (das oberste Verzeichnis mitgezählt) vollständig kopiert, einer mit
// N+1 nicht: genau ein errCopyLimit und nichts kopiert. Randfälle: N = 1 (nur
// das Verzeichnis selbst), flacher und verschachtelter Baum, mehrere oberste
// Einträge (Dateien nebeneinander). Fängt eine Verschiebung der Grenze um eins
// (> statt >= in prefetchQueue.count).
func TestCopyScanEntryLimitExactBoundary(t *testing.T) {
	defer limitKeep()()
	for _, limit := range []int{1, 2, 3, 5, 12} {
		for _, shape := range []string{"flach", "verschachtelt", "oberste Dateien"} {
			if shape == "verschachtelt" && limit < 2 {
				continue
			}
			for _, extra := range []int{0, 1} {
				n := limit + extra
				t.Run(fmt.Sprintf("N=%d/%s/%d Einträge", limit, shape, n), func(t *testing.T) {
					copyMaxEntries = limit
					src, dst := newMemFS("/home/a"), newMemFS("/srv/b")
					var names []string
					switch shape {
					case "flach":
						limitFlat(src, n)
						names = []string{"proj"}
					case "verschachtelt":
						limitNested(src, n)
						names = []string{"proj"}
					default:
						for i := 0; i < n; i++ {
							name := fmt.Sprintf("t%02d.txt", i)
							src.addFile("/home/a/"+name, "x", 0o644, copyTestTime)
							names = append(names, name)
						}
					}
					res := limitCopy(t, src, dst, false, names...)
					copied := len(treeSnapshot(dst, "/srv/b")) - 1 // ohne /srv/b selbst
					if extra == 0 {
						if len(res.failures) != 0 {
							t.Fatalf("Fehler bei genau %d Einträgen: %s", n, failureErrors(res.failures))
						}
						if copied != n {
							t.Errorf("%d Einträge am Ziel, erwartet %d", copied, n)
						}
						if len(res.copiedNames) != len(names) {
							t.Errorf("copiedNames = %v, erwartet alle %d", res.copiedNames, len(names))
						}
						return
					}
					if len(res.failures) != 1 || !errors.Is(res.failures[0].err, errCopyLimit) {
						t.Fatalf("erwartet genau 1 errCopyLimit bei %d Einträgen, erhalten: %s", n, failureErrors(res.failures))
					}
					if copied != 0 || len(res.copiedNames) != 0 {
						t.Errorf("trotz Grenze kopiert: %d Einträge, copiedNames %v", copied, res.copiedNames)
					}
				})
			}
		}
	}
}

// TestDeleteEntryLimitExactBoundary - Löschen: Mit copyMaxEntries = N werden
// genau N Einträge (Verzeichnis + N-1 Dateien) vollständig gelöscht; bei N+1
// werden die ersten N-1 Dateien gelöscht, dann folgt genau ein errCopyLimit,
// das Verzeichnis bleibt (Build-89-Verhalten: Zählung der besuchten Einträge
// samt oberstem Verzeichnis). Fängt eine Verschiebung der Grenze um eins in
// remove (visited > copyMaxEntries).
func TestDeleteEntryLimitExactBoundary(t *testing.T) {
	defer limitKeep()()
	for _, limit := range []int{1, 2, 5, 12} {
		for _, extra := range []int{0, 1} {
			n := limit + extra
			t.Run(fmt.Sprintf("N=%d/%d Einträge", limit, n), func(t *testing.T) {
				copyMaxEntries = limit
				fsys := newMemFS("/home/a")
				limitFlat(fsys, n)
				res := runDeleteSync(t, context.Background(), fsys, "proj")
				if extra == 0 {
					if len(res.failures) != 0 || res.deleted != n || !reflect.DeepEqual(res.deletedNames, []string{"proj"}) {
						t.Errorf("deleted=%d names=%v Fehler=%s, erwartet %d, [proj], keine", res.deleted, res.deletedNames, failureErrors(res.failures), n)
					}
					if fsys.node("/home/a/proj") != nil {
						t.Error("proj nicht gelöscht")
					}
					return
				}
				if len(res.failures) != 1 || !errors.Is(res.failures[0].err, errCopyLimit) {
					t.Fatalf("erwartet genau 1 errCopyLimit, erhalten: %s", failureErrors(res.failures))
				}
				if res.deleted != limit-1 || len(res.deletedNames) != 0 {
					t.Errorf("deleted=%d names=%v, erwartet %d und []", res.deleted, res.deletedNames, limit-1)
				}
				if fsys.node("/home/a/proj") == nil {
					t.Error("proj trotz Grenze gelöscht")
				}
			})
		}
	}
	// Verschachtelt: auch über zwei Ebenen werden genau N Einträge gelöscht
	t.Run("verschachtelt N Einträge", func(t *testing.T) {
		copyMaxEntries = 9
		fsys := newMemFS("/home/a")
		limitNested(fsys, 9)
		res := runDeleteSync(t, context.Background(), fsys, "proj")
		if len(res.failures) != 0 || res.deleted != 9 || fsys.node("/home/a/proj") != nil {
			t.Errorf("deleted=%d Fehler=%s, erwartet 9 ohne Fehler", res.deleted, failureErrors(res.failures))
		}
	})
}

// --- 4a. F8: Verzeichnis über der Grenze wird bis zur Grenze gelöscht ---

// TestDeleteLargeDirectoryDeletesUntilLimit - Ein Verzeichnis mit mehr als
// copyMaxEntries Einträgen (Grenze 5, Verzeichnis mit 12 Dateien): Das
// Vorlesen hört an der Grenze auf, remove liest bei Bedarf weiter und löscht
// wie bis Build 89 der Reihe nach (Namensreihenfolge), bis visited die Grenze
// überschreitet. visited zählt das oberste Verzeichnis mit: 4 Dateien (f00 bis
// f03) werden gelöscht, dann genau EIN errCopyLimit (für f04); das Verzeichnis
// bleibt samt der übrigen 8 Dateien. Ohne das Lesen bei Bedarf bräche der Lauf
// sofort ab und nichts würde gelöscht.
func TestDeleteLargeDirectoryDeletesUntilLimit(t *testing.T) {
	defer limitKeep()()
	copyMaxEntries = 5
	fsys := newMemFS("/home/a")
	limitFlat(fsys, 13) // proj + 12 Dateien f00 .. f11
	res := runDeleteSync(t, context.Background(), fsys, "proj")
	if len(res.failures) != 1 || !errors.Is(res.failures[0].err, errCopyLimit) {
		t.Fatalf("erwartet genau 1 errCopyLimit, erhalten: %s", failureErrors(res.failures))
	}
	if res.failures[0].path != "/home/a/proj/f04" {
		t.Errorf("Grenze bei %q gemeldet, erwartet /home/a/proj/f04", res.failures[0].path)
	}
	if res.deleted != 4 {
		t.Errorf("deleted = %d, erwartet 4", res.deleted)
	}
	for i := 0; i < 12; i++ {
		p := fmt.Sprintf("/home/a/proj/f%02d", i)
		if gone := fsys.node(p) == nil; gone != (i < 4) {
			t.Errorf("%s gelöscht = %v, erwartet %v", p, gone, i < 4)
		}
	}
	if fsys.node("/home/a/proj") == nil || len(res.deletedNames) != 0 || res.cancelled {
		t.Errorf("proj weg=%v, deletedNames=%v, cancelled=%v", fsys.node("/home/a/proj") == nil, res.deletedNames, res.cancelled)
	}
}

// --- 4b. Unsichere Namen zählen nicht zur Grenze ---

// TestCopyScanUnsafeNamesDoNotCountTowardLimit - SFTP-Erfassen: Ein Baum mit
// genau copyMaxEntries SICHEREN Einträgen (das oberste Verzeichnis mitgezählt)
// plus einem unsicheren Namen (Backslash) wird kopiert; gemeldet wird nur der
// unsichere Name (errUnsafeName), nie errCopyLimit. Mit einem sicheren Eintrag
// mehr greift die Grenze (errCopyLimit, nichts kopiert). Der unsichere Name
// darf auch tiefer im Baum stehen. Fängt q.count(len(children)) statt
// q.count(safe) in fetchEntry.
func TestCopyScanUnsafeNamesDoNotCountTowardLimit(t *testing.T) {
	defer limitKeep()()
	const limit = 5
	copyMaxEntries = limit
	for _, tc := range []struct {
		name    string
		safe    int
		deepBad bool
		tooMany bool
	}{
		{"N sichere und ein unsicherer", limit, false, false},
		{"N sichere, unsicherer tiefer", limit, true, false},
		{"N+1 sichere und ein unsicherer", limit + 1, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, dst := newMemFS("/home/a"), newMemFS("/srv/b")
			limitFlat(src, tc.safe)
			if tc.deepBad {
				// Der unsichere Name liegt eine Ebene tiefer: f00 wird ein
				// Verzeichnis (zählt als sicherer Eintrag, die Summe bleibt gleich)
				// und enthält den unsicheren Namen
				src.nodes["/home/a/proj/f00"] = &memNode{mode: os.ModeDir | 0o755}
				src.addFile("/home/a/proj/f00/zz\\bad", "boese", 0o644, copyTestTime)
			} else {
				src.addFile("/home/a/proj/a\\b", "boese", 0o644, copyTestTime)
			}
			res := limitCopy(t, src, dst, false, "proj")
			if tc.tooMany {
				if countErr(res.failures, errCopyLimit) != 1 {
					t.Fatalf("erwartet genau 1 errCopyLimit, erhalten: %s", failureErrors(res.failures))
				}
				if got := len(treeSnapshot(dst, "/srv/b")) - 1; got != 0 {
					t.Errorf("trotz Grenze %d Einträge kopiert", got)
				}
				return
			}
			if countErr(res.failures, errCopyLimit) != 0 {
				t.Fatalf("errCopyLimit trotz nur %d sicheren Einträgen: %s", limit, failureErrors(res.failures))
			}
			if len(res.failures) != 1 || !errors.Is(res.failures[0].err, errUnsafeName) {
				t.Fatalf("erwartet genau 1 errUnsafeName, erhalten: %s", failureErrors(res.failures))
			}
			if got := len(treeSnapshot(dst, "/srv/b")) - 1; got != limit {
				t.Errorf("%d Einträge am Ziel, erwartet %d", got, limit)
			}
		})
	}
}

// TestCopyScanUnsafeNameDoesNotStopReadingChain - Auch das VORLESEN zählt nur
// sichere Namen: Eine Kette proj/d1/d2/d3/d4 (genau copyMaxEntries = 5 sichere
// Verzeichnisse) mit einem unsicheren Namen neben proj/d1 wird vollständig
// gelesen und kopiert. Zählte das Vorlesen den unsicheren Namen mit, höre es
// eine Ebene zu früh auf (d4 bliebe ungelesen) und assemble meldete fälschlich
// errCopyLimit. Die flachen Bäume in TestCopyScanUnsafeNamesDoNotCountTowardLimit
// fangen das nicht, weil ihre Dateien schon aus der Liste vollständig bekannt
// sind. Fängt q.count(len(children)) statt q.count(safe) in fetchEntry.
func TestCopyScanUnsafeNameDoesNotStopReadingChain(t *testing.T) {
	defer limitKeep()()
	const limit = 5
	copyMaxEntries = limit
	src, dst := newMemFS("/home/a"), newMemFS("/srv/b")
	src.addDir("/home/a/proj/d1/d2/d3/d4", 0o755)
	src.addFile("/home/a/proj/a\\b", "boese", 0o644, copyTestTime)
	res := limitCopy(t, src, dst, false, "proj")
	if countErr(res.failures, errCopyLimit) != 0 {
		t.Fatalf("errCopyLimit trotz nur %d sicheren Einträgen: %s", limit, failureErrors(res.failures))
	}
	if len(res.failures) != 1 || !errors.Is(res.failures[0].err, errUnsafeName) {
		t.Fatalf("erwartet genau 1 errUnsafeName, erhalten: %s", failureErrors(res.failures))
	}
	if dst.node("/srv/b/proj/d1/d2/d3/d4") == nil {
		t.Errorf("Kette nicht vollständig kopiert: %v", sortedKeys(treeSnapshot(dst, "/srv/b")))
	}
}

// sortedKeys liefert die Schlüssel einer Karte sortiert (für Fehlermeldungen).
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// limitFindSource baut für den find-Weg: /home/a/proj mit limit-1 sicheren
// Dateien (also limit sichere Einträge samt Verzeichnis), einem unsicheren
// Dateinamen und einem unsicheren VERZEICHNIS "bad\dir" mit badEntries
// Einträgen darunter.
func limitFindSource(limit, badEntries int) *fastFS {
	src := newFastFS()
	src.addDir("/home/a/proj", 0o755)
	for i := 0; i < limit-1; i++ {
		src.addFile(fmt.Sprintf("/home/a/proj/f%02d", i), "x", 0o644, copyTestTime)
	}
	src.addFile("/home/a/proj/a\\b", "boese", 0o644, copyTestTime)
	src.addDir("/home/a/proj/bad\\dir", 0o755)
	for i := 0; i < badEntries; i++ {
		src.addFile(fmt.Sprintf("/home/a/proj/bad\\dir/e%02d", i), "boese", 0o644, copyTestTime)
	}
	return src
}

// TestFastScanUnsafeNamesDoNotCountTowardLimit - find-Weg (Fake mit
// ListTree): genau copyMaxEntries sichere Einträge plus ein unsicherer Name
// und ein unsicheres Verzeichnis werden kopiert, nur die beiden unsicheren
// Namen werden gemeldet (errUnsafeName), nie errCopyLimit - wie beim
// SFTP-Referenzlauf. Hat das unsichere Verzeichnis so viele Einträge, dass die
// Ausgabe von find die Zeilengrenze (2 mal N plus Startpfade) sprengt, ist das
// KEINE Eintragsgrenze: Rückfall auf den SFTP-Weg (Protokoll), gleiches
// Ergebnis. Mit N+1 sicheren Einträgen greift die Grenze auch hier. Fängt
// (1) das Mitzählen unsicherer Namen in buildTrees und (2) das Entfernen der
// Umwandlung von errFastScanTooMany in fastScan.
func TestFastScanUnsafeNamesDoNotCountTowardLimit(t *testing.T) {
	const limit = 5
	for _, tc := range []struct {
		name      string
		badInside int
		wantPath  string
	}{
		{"unsicheres Verzeichnis mit wenigen Einträgen", 3, "find"},
		{"unsicheres Verzeichnis mit sehr vielen Einträgen", 40, "SFTP"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer fastKeepVars()()
			copyMaxEntries = limit
			got, ref, _ := fastCompare(t, func() *fastFS { return limitFindSource(limit, tc.badInside) }, []string{"proj"}, false, nil)
			fastAssertSame(t, tc.name, got, ref)
			fastWantPath(t, tc.name, got, tc.wantPath)
			for _, e := range got.failErrs {
				if errors.Is(e, errCopyLimit) {
					t.Errorf("errCopyLimit trotz nur %d sicheren Einträgen: %q", limit, got.failures)
				}
			}
			if len(got.failures) != 2 {
				t.Fatalf("Fehler %q, erwartet genau 2 (a\\b und bad\\dir, beide unsicher)", got.failures)
			}
			for _, e := range got.failErrs {
				if !errors.Is(e, errUnsafeName) {
					t.Errorf("Fehler %v, erwartet errUnsafeName", e)
				}
			}
			// proj samt seinen limit-1 sicheren Dateien kam an, nichts aus bad\dir
			if len(got.snap) != limit {
				t.Errorf("%d Einträge am Ziel, erwartet %d: %v", len(got.snap), limit, got.snap)
			}
			for p := range got.snap {
				if strings.Contains(p, "bad") || strings.Contains(p, "a\\b") {
					t.Errorf("unsicherer Name am Ziel: %s", p)
				}
			}
		})
	}
	t.Run("N+1 sichere Einträge", func(t *testing.T) {
		defer fastKeepVars()()
		copyMaxEntries = limit
		got, ref, _ := fastCompare(t, func() *fastFS { return limitFindSource(limit+1, 3) }, []string{"proj"}, false, nil)
		fastAssertLimit(t, "N+1", got, ref)
		limitErrs := 0
		for _, e := range got.failErrs {
			if errors.Is(e, errCopyLimit) {
				limitErrs++
			}
		}
		if limitErrs != 1 || len(got.snap) != 0 {
			t.Errorf("errCopyLimit %d-mal, %d Einträge am Ziel; erwartet 1 und 0 (Fehler %q)", limitErrs, len(got.snap), got.failures)
		}
	})
}

// TestDeleteUnsafeNamesDoNotCountTowardLimit - Löschen: genau copyMaxEntries
// sichere Einträge (Verzeichnis + N-1 Dateien) plus ein unsicherer Name: alle
// sicheren werden gelöscht, der unsichere bleibt (errUnsafeName), das
// Verzeichnis bleibt darum auch, aber nie errCopyLimit. Mit einem sicheren
// Eintrag mehr greift die Grenze. Fängt das Mitzählen unsicherer Namen in
// remove (visited).
func TestDeleteUnsafeNamesDoNotCountTowardLimit(t *testing.T) {
	defer limitKeep()()
	const limit = 5
	copyMaxEntries = limit
	t.Run("N sichere und ein unsicherer", func(t *testing.T) {
		fsys := newMemFS("/home/a")
		limitFlat(fsys, limit)
		fsys.addFile("/home/a/proj/a\\b", "boese", 0o644, copyTestTime)
		res := runDeleteSync(t, context.Background(), fsys, "proj")
		if countErr(res.failures, errCopyLimit) != 0 {
			t.Fatalf("errCopyLimit trotz nur %d sicheren Einträgen: %s", limit, failureErrors(res.failures))
		}
		if len(res.failures) != 1 || !errors.Is(res.failures[0].err, errUnsafeName) {
			t.Fatalf("erwartet genau 1 errUnsafeName, erhalten: %s", failureErrors(res.failures))
		}
		if res.deleted != limit-1 {
			t.Errorf("deleted = %d, erwartet %d (alle sicheren Dateien)", res.deleted, limit-1)
		}
		for i := 0; i < limit-1; i++ {
			if p := fmt.Sprintf("/home/a/proj/f%02d", i); fsys.node(p) != nil {
				t.Errorf("%s nicht gelöscht", p)
			}
		}
		if fsys.node("/home/a/proj/a\\b") == nil || fsys.node("/home/a/proj") == nil || len(res.deletedNames) != 0 {
			t.Error("unsicherer Eintrag oder sein Verzeichnis gelöscht bzw. proj als gelöscht gemeldet")
		}
	})
	t.Run("N+1 sichere und ein unsicherer", func(t *testing.T) {
		fsys := newMemFS("/home/a")
		limitFlat(fsys, limit+1)
		fsys.addFile("/home/a/proj/a\\b", "boese", 0o644, copyTestTime)
		res := runDeleteSync(t, context.Background(), fsys, "proj")
		if countErr(res.failures, errCopyLimit) != 1 {
			t.Errorf("erwartet genau 1 errCopyLimit, erhalten: %s", failureErrors(res.failures))
		}
		if fsys.node("/home/a/proj") == nil {
			t.Error("proj trotz Grenze gelöscht")
		}
	})
}

// --- 5. Sortierung der Geschwister bei unsortiertem ReadDir ---

// revReadFS liefert ReadDir in UMGEKEHRTER Namensreihenfolge (ein SFTP-Server
// sortiert nicht; memFS tut es) und merkt Remove-Aufrufe in ihrer Reihenfolge.
type revReadFS struct {
	*memFS
	mu      sync.Mutex
	removed []string
}

func (f *revReadFS) ReadDir(p string) ([]os.FileInfo, error) {
	infos, err := f.memFS.ReadDir(p)
	for i, j := 0, len(infos)-1; i < j; i, j = i+1, j-1 {
		infos[i], infos[j] = infos[j], infos[i]
	}
	return infos, err
}

func (f *revReadFS) Remove(p string) error {
	err := f.memFS.Remove(p)
	if err == nil {
		f.mu.Lock()
		f.removed = append(f.removed, p)
		f.mu.Unlock()
	}
	return err
}

// orderDst ist ein Ziel, das die Reihenfolge von Mkdir und dem endgültigen
// Umbenennen der Dateien (RenameOverwrite) merkt.
type orderDst struct {
	*memFS
	mu     sync.Mutex
	mkdirs []string
	files  []string
}

func (d *orderDst) Mkdir(p string) error {
	err := d.memFS.Mkdir(p)
	if err == nil {
		d.mu.Lock()
		d.mkdirs = append(d.mkdirs, p)
		d.mu.Unlock()
	}
	return err
}

func (d *orderDst) RenameOverwrite(oldPath, newPath string) error {
	err := d.memFS.RenameOverwrite(oldPath, newPath)
	if err == nil {
		d.mu.Lock()
		d.files = append(d.files, newPath)
		d.mu.Unlock()
	}
	return err
}

// TestCopyScanSortsSiblingsFromUnsortedServer - Liefert der Server ReadDir
// unsortiert (hier: umgekehrt), sortiert das Erfassen die Geschwister nach
// Namen: Mit einem Arbeiter (copyParallel = 1) entstehen die Dateien am Ziel in
// der Reihenfolge der sortierten Tiefensuche (Verzeichnis vor Inhalt),
// Verzeichnisse stets nach ihrem Elternverzeichnis, und die Fehler für
// unsichere Namen kommen in Namensreihenfolge. Sonst hinge die Reihenfolge
// von der Laune des Servers ab (und Meldungen würden zwischen Läufen
// wechseln). Fängt das Entfernen von sort.Slice in fetchEntry.
func TestCopyScanSortsSiblingsFromUnsortedServer(t *testing.T) {
	defer limitKeep()()
	copyParallel = 1
	src := &revReadFS{memFS: newMemFS("/home/a")}
	for _, f := range []string{"b.txt", "a.txt", "a2.txt", "c/z.txt", "c/y.txt", "c/m/k.txt", "c/m/j.txt"} {
		src.addFile("/home/a/proj/"+f, f, 0o644, copyTestTime)
	}
	badNames := []string{"b\\1", "a\\2", "c\\3", "a\\0"}
	for _, b := range badNames {
		src.addFile("/home/a/proj/"+b, "boese", 0o644, copyTestTime)
	}
	dst := &orderDst{memFS: newMemFS("/srv/b")}
	res := limitCopy(t, src, dst, false, "proj")
	// Dateien: sortierte Tiefensuche; Verzeichnisse vor ihrem Inhalt gelistet
	wantFiles := []string{
		"/srv/b/proj/a.txt", "/srv/b/proj/a2.txt", "/srv/b/proj/b.txt",
		"/srv/b/proj/c/m/j.txt", "/srv/b/proj/c/m/k.txt", "/srv/b/proj/c/y.txt", "/srv/b/proj/c/z.txt",
	}
	// "c/m" kommt vor "c/y": 'm' < 'y' - die Reihenfolge von Datei und Unterverzeichnis
	// mischt sich nach dem Namen, nicht nach der Art
	if !reflect.DeepEqual(dst.files, wantFiles) {
		t.Errorf("Reihenfolge der Dateien:\n erhalten: %q\n erwartet: %q", dst.files, wantFiles)
	}
	created := map[string]bool{"/srv/b": true}
	for _, p := range dst.mkdirs {
		if !created[path.Dir(p)] {
			t.Errorf("%s angelegt, bevor %s da war", p, path.Dir(p))
		}
		created[p] = true
	}
	// Fehler unsicherer Namen in Namensreihenfolge
	sorted := append([]string(nil), badNames...)
	sort.Strings(sorted)
	var gotBad []string
	for _, f := range res.failures {
		if !errors.Is(f.err, errUnsafeName) {
			t.Errorf("unerwarteter Fehler: %v", f.err)
		}
		gotBad = append(gotBad, strings.TrimPrefix(f.path, "/home/a/proj/"))
	}
	if !reflect.DeepEqual(gotBad, sorted) {
		t.Errorf("Reihenfolge der Fehler:\n erhalten: %q\n erwartet: %q", gotBad, sorted)
	}
}

// TestDeleteSortsSiblingsFromUnsortedServer - Löschen bei unsortiertem
// ReadDir: Die Remove-Aufrufe folgen exakt der sortierten Tiefensuche (Inhalt
// vor dem Verzeichnis, Geschwister nach Namen), auch die Fehler für unsichere
// Namen kommen sortiert. Fängt das Entfernen von sort.Slice in
// fetchDeleteEntry (die vorhandenen Tests nutzen das schon sortierte memFS).
func TestDeleteSortsSiblingsFromUnsortedServer(t *testing.T) {
	src := &revReadFS{memFS: newMemFS("/home/a")}
	names, want := parOrderTree(src.memFS)
	badNames := []string{"zeta/q\\2", "zeta/p\\1"}
	for _, b := range badNames {
		src.addFile("/home/a/"+b, "boese", 0o644, copyTestTime)
	}
	res := runDeleteSync(t, context.Background(), src, names...)
	if got := src.removed; !reflect.DeepEqual(got, wantWithout(want, "/home/a/zeta")) {
		t.Errorf("Reihenfolge der Remove-Aufrufe:\n erhalten: %q\n erwartet: %q", got, wantWithout(want, "/home/a/zeta"))
	}
	// zeta bleibt (unsichere Namen), die Fehler stehen in Namensreihenfolge
	var gotBad []string
	for _, f := range res.failures {
		if !errors.Is(f.err, errUnsafeName) {
			t.Errorf("unerwarteter Fehler: %v", f.err)
		}
		gotBad = append(gotBad, f.path)
	}
	if want := []string{"/home/a/zeta/p\\1", "/home/a/zeta/q\\2"}; !reflect.DeepEqual(gotBad, want) {
		t.Errorf("Reihenfolge der Fehler:\n erhalten: %q\n erwartet: %q", gotBad, want)
	}
}

// wantWithout liefert die Pfade ohne skip (Reihenfolge bleibt).
func wantWithout(paths []string, skip string) []string {
	var out []string
	for _, p := range paths {
		if p != skip {
			out = append(out, p)
		}
	}
	return out
}

// --- 5. Abbruch in der Lösch-Phase des Verschiebens ---

// cancelOnRemoveDirFS bricht beim n-ten RemoveDir ab (der Aufruf selbst läuft
// noch durch) und merkt, welche Verzeichnisse wirklich weg sind.
type cancelOnRemoveDirFS struct {
	*memFS
	at     int32
	cancel context.CancelFunc
	calls  atomic.Int32
}

func (f *cancelOnRemoveDirFS) RemoveDir(p string) error {
	if f.calls.Add(1) == f.at {
		f.cancel()
	}
	return f.memFS.RemoveDir(p)
}

// TestMoveCancelledDuringRemoveMovedDirs - Abbruch mitten im Löschen der
// leeren Quellverzeichnisse (removeMovedDirs, 3. RemoveDir): Eine Kette
// kette/d1/d2/d3/d4/d5 wird verschoben (kopiert, nicht umbenannt). Die ersten
// drei Verzeichnisse (d5, d4, d3: tiefste Ebene zuerst) sind weg; danach
// werden d2, d1 und kette NICHT mehr angefasst (kein weiteres RemoveDir), das
// Ergebnis meldet cancelled, und keine Datei ist verloren: die Datei liegt
// vollständig am Ziel. Fängt das Entfernen der ctx-Prüfung in removeMovedDir
// und des Setzens von result.cancelled.
func TestMoveCancelledDuringRemoveMovedDirs(t *testing.T) {
	defer limitKeep()()
	copyParallel = 1
	base := newMemFS("/home/a")
	base.addFile("/home/a/kette/d1/d2/d3/d4/d5/daten.txt", "wichtig", 0o644, copyTestTime)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := &cancelOnRemoveDirFS{memFS: base, at: 3, cancel: cancel}
	dst := newMemFS("/srv/b")
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"kette"}, move: true}
	res, _ := runCopySync(t, ctx, req, noConflicts(t))
	if !res.cancelled {
		t.Error("Abbruch beim Löschen der Verzeichnisse nicht gemeldet (cancelled = false)")
	}
	if n := src.calls.Load(); n != 3 {
		t.Errorf("%d RemoveDir-Aufrufe, erwartet genau 3 (nach dem Abbruch keiner mehr)", n)
	}
	for _, p := range []string{"/home/a/kette/d1/d2/d3", "/home/a/kette/d1/d2/d3/d4", "/home/a/kette/d1/d2/d3/d4/d5"} {
		if base.node(p) != nil {
			t.Errorf("%s nach dem Löschen noch da", p)
		}
	}
	for _, p := range []string{"/home/a/kette", "/home/a/kette/d1", "/home/a/kette/d1/d2"} {
		if base.node(p) == nil {
			t.Errorf("%s nach dem Abbruch gelöscht", p)
		}
	}
	// Keine Datei verloren: die Quelle wurde erst nach dem Kopieren gelöscht
	if got, _ := dst.content("/srv/b/kette/d1/d2/d3/d4/d5/daten.txt"); got != "wichtig" {
		t.Errorf("Datei am Ziel = %q, erwartet %q", got, "wichtig")
	}
	if len(res.failures) != 0 {
		t.Errorf("Fehler nach dem Abbruch: %s", failureErrors(res.failures))
	}
	assertNoTempFiles(t, dst)
}

// --- 5. Umbenannte Einträge nach einem Abbruch ---

// TestMoveRenamedEntryStaysInCopiedNamesAfterCancel - Zwei Einträge: notiz.txt
// wird umbenannt (Ziel fehlt), projekt gehört zum Kopierweg (das Ziel gibt es
// schon, also zusammenführen). Beim Kopieren von projekt wird abgebrochen
// (beim ersten Schreiben). Dann gilt: cancelled, und copiedNames enthält GENAU
// den umbenannten Eintrag - er ist wirklich verschoben (Quelle weg, Ziel da),
// projekt dagegen nicht. Fängt das Entfernen von `run.renamed[name] ||` in
// runCopy.
func TestMoveRenamedEntryStaysInCopiedNamesAfterCancel(t *testing.T) {
	src, dst, shared := renamePair()
	shared.addFile("/srv/b/projekt/alt.txt", "alt", 0o644, copyTestTime)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var once sync.Once
	shared.onWrite = func() { once.Do(cancel) }
	res, _ := runCopySync(t, ctx, renameRequest(src, dst, "notiz.txt", "projekt"), noConflicts(t))
	if !res.cancelled {
		t.Error("cancelled = false, erwartet true")
	}
	if !reflect.DeepEqual(res.copiedNames, []string{"notiz.txt"}) {
		t.Errorf("copiedNames = %v, erwartet genau [notiz.txt]", res.copiedNames)
	}
	if got := src.renameCalls(); len(got) != 1 || got[0][0] != "/home/a/notiz.txt" {
		t.Errorf("RenameNoReplace-Aufrufe = %v, erwartet nur notiz.txt", got)
	}
	if got, _ := shared.content("/srv/b/notiz.txt"); got != "hallo" || shared.node("/home/a/notiz.txt") != nil {
		t.Error("notiz.txt nicht wirklich verschoben")
	}
	if shared.node("/home/a/projekt/main.go") == nil && shared.node("/srv/b/projekt/main.go") == nil {
		t.Error("main.go verloren (weder Quelle noch Ziel)")
	}
	if res.renamed != 1 {
		t.Errorf("renamed = %d, erwartet 1", res.renamed)
	}
	assertNoProbe(t, shared)
	assertNoTempFiles(t, shared)
}
