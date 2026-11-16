// Paket main - Tests: Umbenennen statt Kopieren beim Verschieben (Build 90, Teil 2)
//
// Verschieben (F6) auf demselben Konto (gleicher Host ohne Groß-/Kleinschreibung
// und Randleerraum, gleicher Port und gleicher Benutzer - auch bei zwei
// verschiedenen Verbindungseinträgen, Nutzerentscheidung Build 90) muss nichts
// kopieren: ein Umbenennen auf dem Server genügt und dauert Millisekunden statt
// Stunden. Voraussetzung ist copyRequest.sameAccount (fmSameAccount) und ein
// Dateisystem mit RenameNoReplace (Schnittstelle noReplaceRenamer; sftpFS und
// localFS). Das Umbenennen ersetzt NIE ein vorhandenes Ziel. Umbenannte Einträge
// zählen in copyResult.renamed (nicht in files/bytes); die Abschlussmeldung
// nennt dann neutral Name bzw. Zahl der Einträge.
//
// Ablauf in runCopy (nur bei move && sameHost && sameAccount, ohne
// markDownloads und safeLinksOnly): einmal pro Lauf wird am Ziel eine
// Prüfdatei (.ssh-easy-probe-...) angelegt und über die Quellverbindung im
// Zielverzeichnis gesucht. Nur wenn sie dort sichtbar ist, sehen beide
// Verbindungen dasselbe Dateisystem. Dann wird je oberstem Namen, dessen Ziel
// nicht vorhanden ist, umbenannt. Jeder Fehler führt still zum bisherigen
// Kopierweg zurück.
//
// Testaufbau: Quelle und Ziel sind ZWEI Hüllen (renameFS) um DASSELBE memFS -
// so sehen beide dieselben Dateien, wie zwei SFTP-Sitzungen desselben Kontos.
//
// @author Kurt Ingwer
// @date   2026-11-15 15:14
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Schnittstellen müssen die echten Dateisysteme erfüllen (Übersetzungsfehler,
// wenn RenameNoReplace fehlt).
var (
	_ noReplaceRenamer = (*sftpFS)(nil)
	_ noReplaceRenamer = (*localFS)(nil)
	_ noReplaceRenamer = (*renameFS)(nil)
)

// renameFS ist eine Hülle um ein (gemeinsames) memFS mit RenameNoReplace.
// Sie merkt alle Aufrufe, die für den Test wichtig sind.
type renameFS struct {
	*memFS
	// Startverzeichnis (leer = das des memFS)
	home string
	// Erzwungener Fehler von RenameNoReplace (nil = normal arbeiten)
	failRename error
	// Namen, die ValidName ablehnt (wie ein Windows-Ziel)
	rejectNames map[string]bool
	// Anlegen einer Prüfdatei scheitert
	failProbe bool

	mu       sync.Mutex
	renames  [][2]string
	readDirs []string
	mkdirs   []string
	creates  []string
}

func (f *renameFS) Getwd() (string, error) {
	if f.home != "" {
		return f.home, nil
	}
	return f.memFS.Getwd()
}

// ValidName macht renameFS zum nameValidator (fsAcceptsName).
func (f *renameFS) ValidName(name string) bool { return !f.rejectNames[name] }

func (f *renameFS) ReadDir(p string) ([]os.FileInfo, error) {
	f.mu.Lock()
	f.readDirs = append(f.readDirs, p)
	f.mu.Unlock()
	return f.memFS.ReadDir(p)
}

func (f *renameFS) Mkdir(p string) error {
	f.mu.Lock()
	f.mkdirs = append(f.mkdirs, p)
	f.mu.Unlock()
	return f.memFS.Mkdir(p)
}

func (f *renameFS) CreateExclusive(p string) (io.WriteCloser, error) {
	f.mu.Lock()
	f.creates = append(f.creates, p)
	f.mu.Unlock()
	if f.failProbe && strings.HasPrefix(path.Base(p), copyProbePrefix) {
		return nil, os.ErrPermission
	}
	return f.memFS.CreateExclusive(p)
}

// RenameNoReplace verschiebt den Teilbaum unter oldPath nach newPath. Ist das
// Ziel vorhanden, scheitert es mit os.ErrExist (ersetzt nie).
func (f *renameFS) RenameNoReplace(oldPath, newPath string) error {
	f.mu.Lock()
	f.renames = append(f.renames, [2]string{oldPath, newPath})
	forced := f.failRename
	f.mu.Unlock()
	if forced != nil {
		return forced
	}
	f.memFS.mu.Lock()
	defer f.memFS.mu.Unlock()
	if _, ok := f.memFS.nodes[oldPath]; !ok {
		return os.ErrNotExist
	}
	if _, ok := f.memFS.nodes[newPath]; ok {
		return os.ErrExist
	}
	if parent, ok := f.memFS.nodes[path.Dir(newPath)]; !ok || !parent.mode.IsDir() {
		return os.ErrNotExist
	}
	moved := map[string]*memNode{}
	for p, n := range f.memFS.nodes {
		if p == oldPath || strings.HasPrefix(p, oldPath+"/") {
			moved[newPath+strings.TrimPrefix(p, oldPath)] = n
		}
	}
	for p := range f.memFS.nodes {
		if p == oldPath || strings.HasPrefix(p, oldPath+"/") {
			delete(f.memFS.nodes, p)
		}
	}
	for p, n := range moved {
		f.memFS.nodes[p] = n
	}
	return nil
}

// renameCalls liefert die bisherigen RenameNoReplace-Aufrufe (Kopie).
func (f *renameFS) renameCalls() [][2]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]string(nil), f.renames...)
}

// probeCreates zählt die angelegten Prüfdateien.
func (f *renameFS) probeCreates() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.creates {
		if strings.HasPrefix(path.Base(p), copyProbePrefix) {
			n++
		}
	}
	return n
}

// renamePair liefert zwei Hüllen um dasselbe memFS ("dieselbe Sitzung") samt
// Quelle /home/a (newCopySource) und leerem Ziel /srv/b.
func renamePair() (src, dst *renameFS, shared *memFS) {
	shared = newCopySource()
	shared.addDir("/srv/b", 0o755)
	return &renameFS{memFS: shared}, &renameFS{memFS: shared, home: "/srv/b"}, shared
}

// renameRequest liefert einen Verschiebe-Auftrag auf demselben Konto.
func renameRequest(src, dst *renameFS, names ...string) copyRequest {
	return copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: names, move: true, sameHost: true, sameAccount: true}
}

// assertNoProbe prüft, dass keine Prüfdatei liegen geblieben ist.
func assertNoProbe(t *testing.T, m *memFS) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for p := range m.nodes {
		if strings.HasPrefix(path.Base(p), copyProbePrefix) {
			t.Errorf("Prüfdatei liegen geblieben: %s", p)
		}
	}
}

// --- Fachlogik ---

// TestMoveRenamesDirectoryInsteadOfCopying - Fall 1: Verzeichnis mit Inhalt,
// Ziel fehlt. Genau ein Umbenennen, kein Lesen des Teilbaums in der Quelle,
// kein Mkdir und keine Datei als Kopie am Ziel (außer der einen Prüfdatei).
// Quelle weg, Ziel komplett (auch der Symlink), keine Fehler, nichts übrig.
func TestMoveRenamesDirectoryInsteadOfCopying(t *testing.T) {
	src, dst, shared := renamePair()
	want := treeSnapshot(shared, "/home/a/projekt")
	res, _ := runCopySync(t, context.Background(), renameRequest(src, dst, "projekt"), noConflicts(t))
	if len(res.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(res.failures))
	}
	if got, w := src.renameCalls(), [][2]string{{"/home/a/projekt", "/srv/b/projekt"}}; !reflect.DeepEqual(got, w) {
		t.Errorf("RenameNoReplace-Aufrufe = %v, erwartet %v", got, w)
	}
	for _, p := range append(append([]string(nil), src.readDirs...), dst.readDirs...) {
		if strings.HasPrefix(p, "/home/a/projekt") {
			t.Errorf("Teilbaum der Quelle gelesen: ReadDir(%s)", p)
		}
	}
	if len(dst.mkdirs) != 0 {
		t.Errorf("am Ziel Verzeichnisse angelegt: %v", dst.mkdirs)
	}
	for _, p := range dst.creates {
		if !strings.HasPrefix(path.Base(p), copyProbePrefix) {
			t.Errorf("am Ziel eine Datei angelegt (Kopie?): %s", p)
		}
	}
	if n := dst.probeCreates(); n != 1 {
		t.Errorf("%d Prüfdateien angelegt, erwartet genau 1", n)
	}
	if shared.node("/home/a/projekt") != nil {
		t.Error("Quelle noch da")
	}
	if got := treeSnapshot(shared, "/srv/b/projekt"); !reflect.DeepEqual(got, want) {
		t.Errorf("Ziel weicht ab: %d statt %d Einträge", len(got), len(want))
	}
	if !reflect.DeepEqual(res.copiedNames, []string{"projekt"}) {
		t.Errorf("copiedNames = %v", res.copiedNames)
	}
	// Umbenannt, nicht übertragen: nur renamed zählt, files/bytes bleiben 0
	if res.renamed != 1 || res.files != 0 || res.bytes != 0 {
		t.Errorf("renamed=%d files=%d bytes=%d, erwartet 1, 0 und 0 (ein Verzeichnis)", res.renamed, res.files, res.bytes)
	}
	assertNoProbe(t, shared)
	assertNoTempFiles(t, shared)
}

// TestMoveRenamesFileCountsSize - eine reguläre Datei wird umbenannt: renamed 1,
// aber files und bytes bleiben 0 (nur Kopiertes zählt dort); der Inhalt
// (5 Bytes "hallo") steht am Ziel.
func TestMoveRenamesFileCountsSize(t *testing.T) {
	src, dst, shared := renamePair()
	res, _ := runCopySync(t, context.Background(), renameRequest(src, dst, "notiz.txt"), noConflicts(t))
	if len(res.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(res.failures))
	}
	if len(src.renameCalls()) != 1 {
		t.Errorf("RenameNoReplace-Aufrufe = %v", src.renameCalls())
	}
	if got, _ := shared.content("/srv/b/notiz.txt"); got != "hallo" {
		t.Errorf("Inhalt am Ziel = %q", got)
	}
	if shared.node("/home/a/notiz.txt") != nil {
		t.Error("Quelle noch da")
	}
	// Umbenannte Einträge zählen als renamed, nicht in files/bytes (es wurde
	// nichts übertragen; die Meldung nennt dann den Namen, siehe fmFinishCopy)
	if res.renamed != 1 || res.files != 0 || res.bytes != 0 {
		t.Errorf("renamed=%d files=%d bytes=%d, erwartet 1, 0 und 0", res.renamed, res.files, res.bytes)
	}
	if !reflect.DeepEqual(res.copiedNames, []string{"notiz.txt"}) {
		t.Errorf("copiedNames = %v", res.copiedNames)
	}
}

// TestMoveRenamesSymlinkItself - ein oberster Symlink wird selbst
// umbenannt; sein Ziel bleibt unberührt (auch wenn es woanders liegt).
func TestMoveRenamesSymlinkItself(t *testing.T) {
	src, dst, shared := renamePair()
	shared.addLink("/home/a/abkuerzung", "/etc/passwd")
	shared.addFile("/etc/passwd", "root", 0o644, copyTestTime)
	res, _ := runCopySync(t, context.Background(), renameRequest(src, dst, "abkuerzung"), noConflicts(t))
	if len(res.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(res.failures))
	}
	n := shared.node("/srv/b/abkuerzung")
	if n == nil || n.target != "/etc/passwd" || shared.node("/home/a/abkuerzung") != nil {
		t.Errorf("Symlink nicht verschoben: %+v", n)
	}
	if got, _ := shared.content("/etc/passwd"); got != "root" {
		t.Error("Ziel des Symlinks verändert")
	}
}

// TestMoveExistingTargetUsesMerge - Fall 2: Das Zielverzeichnis gibt es schon.
// Kein Umbenennen (das würde Inhalt verwerfen oder scheitern), sondern wie
// bisher zusammenführen: vorhandene Datei bleibt, alles aus der Quelle kommt
// dazu, die Quelle ist danach weg.
func TestMoveExistingTargetUsesMerge(t *testing.T) {
	src, dst, shared := renamePair()
	shared.addFile("/srv/b/projekt/alt.txt", "alt", 0o644, copyTestTime)
	res, _ := runCopySync(t, context.Background(), renameRequest(src, dst, "projekt"), noConflicts(t))
	if len(res.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(res.failures))
	}
	if calls := src.renameCalls(); len(calls) != 0 {
		t.Errorf("umbenannt trotz vorhandenem Ziel: %v", calls)
	}
	for p, want := range map[string]string{
		"/srv/b/projekt/alt.txt":  "alt",
		"/srv/b/projekt/main.go":  "package main",
		"/srv/b/projekt/lib/x.go": "x",
		"/srv/b/projekt/run.sh":   "#!/bin/sh",
	} {
		if got, _ := shared.content(p); got != want {
			t.Errorf("%s = %q, erwartet %q", p, got, want)
		}
	}
	if shared.node("/home/a/projekt") != nil {
		t.Error("Quelle nicht entfernt")
	}
	assertNoProbe(t, shared)
}

// TestMoveExistingFileStillAsksOverwrite - ein vorhandenes Ziel (Datei) führt
// weiter zur Rückfrage; erst nach "überschreiben" wird kopiert. Kein
// Umbenennen, das das Ziel ersetzen würde.
func TestMoveExistingFileStillAsksOverwrite(t *testing.T) {
	src, dst, shared := renamePair()
	shared.addFile("/srv/b/notiz.txt", "alt", 0o644, copyTestTime)
	res, conflicts := runCopySync(t, context.Background(), renameRequest(src, dst, "notiz.txt"), func(string) copyConflictAnswer { return copyAnswerOverwrite })
	if len(conflicts) != 1 || conflicts[0] != "/srv/b/notiz.txt" {
		t.Errorf("Rückfragen = %v", conflicts)
	}
	if len(src.renameCalls()) != 0 {
		t.Errorf("umbenannt trotz vorhandenem Ziel: %v", src.renameCalls())
	}
	if got, _ := shared.content("/srv/b/notiz.txt"); got != "hallo" || len(res.failures) != 0 {
		t.Errorf("Ziel = %q, Fehler: %s", got, failureErrors(res.failures))
	}
}

// TestMoveExistingFileSkippedKeepsSource - Antwort "überspringen" auf die
// Rückfrage: nichts wird umbenannt oder kopiert, die Quelle bleibt, der
// Eintrag gilt nicht als verschoben.
func TestMoveExistingFileSkippedKeepsSource(t *testing.T) {
	src, dst, shared := renamePair()
	shared.addFile("/srv/b/notiz.txt", "alt", 0o644, copyTestTime)
	res, _ := runCopySync(t, context.Background(), renameRequest(src, dst, "notiz.txt"), func(string) copyConflictAnswer { return copyAnswerSkip })
	if len(src.renameCalls()) != 0 {
		t.Errorf("umbenannt: %v", src.renameCalls())
	}
	if got, _ := shared.content("/home/a/notiz.txt"); got != "hallo" {
		t.Error("Quelle trotz Überspringen verändert")
	}
	if got, _ := shared.content("/srv/b/notiz.txt"); got != "alt" {
		t.Errorf("Ziel überschrieben: %q", got)
	}
	if len(res.copiedNames) != 0 {
		t.Errorf("copiedNames = %v, erwartet leer", res.copiedNames)
	}
}

// TestMoveNoRenameWithoutSameAccount - Fall 3: gleicher Host, aber anderes
// Konto (sameAccount false, z. B. zwei Einträge mit anderem Benutzer): kein
// Umbenennen, keine Prüfdatei; verschoben wird durch Kopieren und Löschen.
func TestMoveNoRenameWithoutSameAccount(t *testing.T) {
	src, dst, shared := renamePair()
	req := renameRequest(src, dst, "projekt")
	req.sameAccount = false
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(res.failures))
	}
	if len(src.renameCalls()) != 0 || dst.probeCreates() != 0 {
		t.Errorf("Umbenennen versucht (%v) oder Prüfdatei angelegt (%d)", src.renameCalls(), dst.probeCreates())
	}
	if got, _ := shared.content("/srv/b/projekt/lib/x.go"); got != "x" || shared.node("/home/a/projekt") != nil {
		t.Error("Kopierweg hat nicht verschoben")
	}
	if res.files != 4 {
		t.Errorf("files = %d, erwartet 4 (kopierte Dateien und Symlink)", res.files)
	}
}

// TestMoveRenameErrorFallsBackToCopy - Fall 4: Scheitert das Umbenennen aus
// irgendeinem Grund (Rechte, Ziel inzwischen da, nicht unterstützt, Rennen),
// wird still auf Kopieren zurückgegriffen: Erfolg, KEIN Fehler im Ergebnis,
// vollständig am Ziel, Quelle weg, Prüfdatei auch im Fehlerfall gelöscht.
func TestMoveRenameErrorFallsBackToCopy(t *testing.T) {
	for _, forced := range []error{errors.New("Server sagt nein"), os.ErrPermission, os.ErrExist, errors.ErrUnsupported, os.ErrNotExist} {
		t.Run(forced.Error(), func(t *testing.T) {
			src, dst, shared := renamePair()
			src.failRename = forced
			want := treeSnapshot(shared, "/home/a/projekt")
			res, _ := runCopySync(t, context.Background(), renameRequest(src, dst, "projekt", "notiz.txt"), noConflicts(t))
			if len(res.failures) != 0 {
				t.Fatalf("Fehler im Ergebnis: %s", failureErrors(res.failures))
			}
			if len(src.renameCalls()) == 0 {
				t.Error("Umbenennen gar nicht versucht")
			}
			if got := treeSnapshot(shared, "/srv/b/projekt"); !reflect.DeepEqual(got, want) {
				t.Errorf("Ziel unvollständig: %d statt %d Einträge", len(got), len(want))
			}
			if shared.node("/home/a/projekt") != nil || shared.node("/home/a/notiz.txt") != nil {
				t.Error("Quelle nicht entfernt")
			}
			if !reflect.DeepEqual(res.copiedNames, []string{"projekt", "notiz.txt"}) {
				t.Errorf("copiedNames = %v", res.copiedNames)
			}
			assertNoProbe(t, shared)
			assertNoTempFiles(t, shared)
		})
	}
}

// TestMoveIntoItselfNeverRenames - Fall 5: ein Verzeichnis in sich selbst
// verschieben bleibt abgelehnt (errMoveIntoItself); es wird nicht einmal
// umbenannt (rename(2) meldet das auch, aber der Schutz gehört davor).
func TestMoveIntoItselfNeverRenames(t *testing.T) {
	src, dst, shared := renamePair()
	req := renameRequest(src, dst, "projekt")
	req.dstDir = "/home/a/projekt/lib"
	before := parNodeCount(shared)
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 1 || !errors.Is(res.failures[0].err, errMoveIntoItself) {
		t.Fatalf("erwartet errMoveIntoItself, erhalten: %s", failureErrors(res.failures))
	}
	if calls := src.renameCalls(); len(calls) != 0 {
		t.Errorf("umbenannt: %v", calls)
	}
	if parNodeCount(shared) != before || shared.node("/home/a/projekt/main.go") == nil {
		t.Error("Baum verändert")
	}
	if len(res.copiedNames) != 0 {
		t.Errorf("copiedNames = %v", res.copiedNames)
	}
	assertNoProbe(t, shared)
}

// TestMoveBetweenDifferentFilesystemsNeverRenames - Fall 6: sameAccount ist
// gesetzt, aber Quelle und Ziel sind in Wahrheit verschiedene Dateisysteme
// (die Prüfdatei ist über die Quelle unsichtbar). Dann darf nichts umbenannt
// werden - ein Umbenennen im falschen Dateisystem wäre Datenverlust. Es wird
// kopiert; die Prüfdatei ist am Ziel wieder gelöscht.
func TestMoveBetweenDifferentFilesystemsNeverRenames(t *testing.T) {
	srcMem := newCopySource()
	dstMem := newMemFS("/srv/b")
	src, dst := &renameFS{memFS: srcMem}, &renameFS{memFS: dstMem, home: "/srv/b"}
	want := treeSnapshot(srcMem, "/home/a/projekt")
	res, _ := runCopySync(t, context.Background(), renameRequest(src, dst, "projekt", "notiz.txt"), noConflicts(t))
	if len(res.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(res.failures))
	}
	if calls := src.renameCalls(); len(calls) != 0 {
		t.Errorf("umbenannt trotz getrennter Dateisysteme: %v", calls)
	}
	if got := treeSnapshot(dstMem, "/srv/b/projekt"); !reflect.DeepEqual(got, want) {
		t.Errorf("Ziel unvollständig: %d statt %d Einträge", len(got), len(want))
	}
	if srcMem.node("/home/a/projekt") != nil || srcMem.node("/home/a/notiz.txt") != nil {
		t.Error("Quelle nicht entfernt")
	}
	assertNoProbe(t, dstMem)
	assertNoProbe(t, srcMem)
}

// TestMoveProbeCannotBeCreatedFallsBack - Die Prüfdatei lässt sich am Ziel
// nicht anlegen (kein Schreibrecht für Dateien, aber Kopieren geht ebenso
// nicht?): hier nur diese Namensklasse gesperrt. Ohne Prüfung darf nicht
// umbenannt werden; verschoben wird auf dem Kopierweg ohne Fehler.
func TestMoveProbeCannotBeCreatedFallsBack(t *testing.T) {
	src, dst, shared := renamePair()
	dst.failProbe = true
	res, _ := runCopySync(t, context.Background(), renameRequest(src, dst, "projekt"), noConflicts(t))
	if len(res.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(res.failures))
	}
	if calls := src.renameCalls(); len(calls) != 0 {
		t.Errorf("ohne Prüfung umbenannt: %v", calls)
	}
	if got, _ := shared.content("/srv/b/projekt/lib/x.go"); got != "x" || shared.node("/home/a/projekt") != nil {
		t.Error("Kopierweg hat nicht verschoben")
	}
}

// TestCopyNeverRenames - Fall 7: Kopieren (move false) benennt nie um und
// legt auch keine Prüfdatei an; die Quelle bleibt unverändert.
func TestCopyNeverRenames(t *testing.T) {
	src, dst, shared := renamePair()
	req := renameRequest(src, dst, "projekt", "notiz.txt")
	req.move = false
	want := treeSnapshot(shared, "/home/a/projekt")
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(res.failures))
	}
	if len(src.renameCalls()) != 0 || dst.probeCreates() != 0 {
		t.Errorf("Kopieren benutzte Umbenennen (%v) oder Prüfdatei (%d)", src.renameCalls(), dst.probeCreates())
	}
	if got := treeSnapshot(shared, "/home/a/projekt"); !reflect.DeepEqual(got, want) {
		t.Error("Quelle beim Kopieren verändert")
	}
	if got, _ := shared.content("/srv/b/notiz.txt"); got != "hallo" {
		t.Errorf("Kopie fehlt: %q", got)
	}
}

// TestMoveNoRenameWithMarkDownloadsOrSafeLinks - Fall 8: Wo Dateien beim
// Kopieren besonders behandelt werden müssen (als heruntergeladen markieren,
// nur sichere Symlinks), darf das Umbenennen diese Behandlung nicht umgehen.
func TestMoveNoRenameWithMarkDownloadsOrSafeLinks(t *testing.T) {
	for _, mode := range []string{"markDownloads", "safeLinksOnly"} {
		t.Run(mode, func(t *testing.T) {
			src, dst, shared := renamePair()
			req := renameRequest(src, dst, "notiz.txt")
			req.markDownloads = mode == "markDownloads"
			req.safeLinksOnly = mode == "safeLinksOnly"
			res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
			if len(res.failures) != 0 {
				t.Fatalf("Fehler: %s", failureErrors(res.failures))
			}
			if len(src.renameCalls()) != 0 || dst.probeCreates() != 0 {
				t.Errorf("Umbenennen (%v) bzw. Prüfdatei (%d) trotz %s", src.renameCalls(), dst.probeCreates(), mode)
			}
			if got, _ := shared.content("/srv/b/notiz.txt"); got != "hallo" || shared.node("/home/a/notiz.txt") != nil {
				t.Error("Kopierweg hat nicht verschoben")
			}
		})
	}
}

// TestMoveUnsafeNamesNeverRename - Fall 9: Namen mit Backslash, Schrägstrich,
// Steuerzeichen, "." und ".." werden abgelehnt (errUnsafeName), nie
// umbenannt - sonst könnte ein Name wie "../x" aus dem Verzeichnis führen.
// Auch ein Name, den das Ziel nicht annimmt (ValidName), wird nicht umbenannt.
func TestMoveUnsafeNamesNeverRename(t *testing.T) {
	for _, name := range []string{"a\\b", "a/b", "x\ty", "x\x1b[2J", "..", ".", "../x", "", "nope.txt"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			src, dst, shared := renamePair()
			dst.rejectNames = map[string]bool{"nope.txt": true}
			if name == "a\\b" || name == "x\ty" || name == "x\x1b[2J" || name == "nope.txt" {
				shared.addFile("/home/a/"+name, "geheim", 0o644, copyTestTime)
			}
			before := parNodeCount(shared)
			res, _ := runCopySync(t, context.Background(), renameRequest(src, dst, name), noConflicts(t))
			if len(res.failures) != 1 || !errors.Is(res.failures[0].err, errUnsafeName) {
				t.Fatalf("erwartet errUnsafeName, erhalten: %s", failureErrors(res.failures))
			}
			if calls := src.renameCalls(); len(calls) != 0 {
				t.Errorf("umbenannt: %v", calls)
			}
			if parNodeCount(shared) != before {
				t.Error("Dateisystem verändert")
			}
			if len(res.copiedNames) != 0 {
				t.Errorf("copiedNames = %v", res.copiedNames)
			}
			assertNoProbe(t, shared)
		})
	}
}

// TestMoveMixedTopEntries - Fall 10: mehrere oberste Einträge, bei einem
// (projekt) gibt es das Ziel schon: die anderen werden umbenannt, projekt
// wird zusammengeführt. copiedNames folgt der Eingabe, nicht der Art der
// Bearbeitung.
func TestMoveMixedTopEntries(t *testing.T) {
	src, dst, shared := renamePair()
	shared.addFile("/home/a/a.txt", "a", 0o644, copyTestTime)
	shared.addFile("/home/a/b.txt", "bb", 0o644, copyTestTime)
	shared.addFile("/srv/b/projekt/alt.txt", "alt", 0o644, copyTestTime)
	res, _ := runCopySync(t, context.Background(), renameRequest(src, dst, "a.txt", "projekt", "b.txt"), noConflicts(t))
	if len(res.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(res.failures))
	}
	renamed := map[string]bool{}
	for _, c := range src.renameCalls() {
		renamed[path.Base(c[0])] = true
	}
	if !reflect.DeepEqual(renamed, map[string]bool{"a.txt": true, "b.txt": true}) {
		t.Errorf("umbenannt wurden %v, erwartet a.txt und b.txt", renamed)
	}
	if !reflect.DeepEqual(res.copiedNames, []string{"a.txt", "projekt", "b.txt"}) {
		t.Errorf("copiedNames = %v, erwartet Eingabereihenfolge", res.copiedNames)
	}
	for p, want := range map[string]string{"/srv/b/a.txt": "a", "/srv/b/b.txt": "bb", "/srv/b/projekt/alt.txt": "alt", "/srv/b/projekt/main.go": "package main"} {
		if got, _ := shared.content(p); got != want {
			t.Errorf("%s = %q, erwartet %q", p, got, want)
		}
	}
	if shared.node("/home/a/projekt") != nil || shared.node("/home/a/a.txt") != nil || shared.node("/home/a/b.txt") != nil {
		t.Error("Quelle nicht entfernt")
	}
	// 2 umbenannte Dateien zählen nur als renamed; files/bytes enthalten nur die
	// 4 kopierten Dateien (inkl. Symlink) des zusammengeführten projekt
	if res.renamed != 2 || res.files != 4 || res.bytes != int64(len("package main#!/bin/shx")) {
		t.Errorf("renamed=%d files=%d bytes=%d, erwartet 2, 4 und %d", res.renamed, res.files, res.bytes, len("package main#!/bin/shx"))
	}
	assertNoProbe(t, shared)
}

// TestMoveRenamedEntryStaysDoneWhenLimitHit - Fall 11: Ein Eintrag wird
// umbenannt, ein anderer (Ziel vorhanden, also Kopierweg) ist zu groß. Die
// Grenze bricht nur den zu großen Baum ab (genau ein errCopyLimit); der
// umbenannte bleibt in copiedNames und ist wirklich verschoben.
func TestMoveRenamedEntryStaysDoneWhenLimitHit(t *testing.T) {
	defer func(entries int) { copyMaxEntries = entries }(copyMaxEntries)
	copyMaxEntries = 3
	src, dst, shared := renamePair()
	shared.addFile("/home/a/klein.txt", "k", 0o644, copyTestTime)
	for i := 0; i < 6; i++ {
		shared.addFile(fmt.Sprintf("/home/a/gross/f%d", i), "x", 0o644, copyTestTime)
	}
	shared.addDir("/srv/b/gross", 0o755)
	res, _ := runCopySync(t, context.Background(), renameRequest(src, dst, "klein.txt", "gross"), noConflicts(t))
	if len(res.failures) != 1 || !errors.Is(res.failures[0].err, errCopyLimit) {
		t.Fatalf("erwartet genau 1 errCopyLimit, erhalten: %s", failureErrors(res.failures))
	}
	if !reflect.DeepEqual(res.copiedNames, []string{"klein.txt"}) {
		t.Errorf("copiedNames = %v, erwartet [klein.txt]", res.copiedNames)
	}
	if got, _ := shared.content("/srv/b/klein.txt"); got != "k" || shared.node("/home/a/klein.txt") != nil {
		t.Error("klein.txt nicht verschoben")
	}
	if shared.node("/home/a/gross/f0") == nil || shared.node("/srv/b/gross/f0") != nil {
		t.Error("zu großer Baum angefasst")
	}
}

// TestMoveProbeCreatedOncePerRun - Drei Dateien werden umbenannt, aber die
// Prüfdatei wird nur EINMAL pro Lauf angelegt (nicht je Eintrag: das wären
// bei tausend markierten Dateien tausend Rundreisen mehr).
func TestMoveProbeCreatedOncePerRun(t *testing.T) {
	src, dst, shared := renamePair()
	shared.addFile("/home/a/a.txt", "a", 0o644, copyTestTime)
	shared.addFile("/home/a/b.txt", "b", 0o644, copyTestTime)
	res, _ := runCopySync(t, context.Background(), renameRequest(src, dst, "a.txt", "b.txt", "notiz.txt"), noConflicts(t))
	if len(res.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(res.failures))
	}
	if n := len(src.renameCalls()); n != 3 {
		t.Errorf("%d Umbenennungen, erwartet 3", n)
	}
	if n := dst.probeCreates(); n != 1 {
		t.Errorf("%d Prüfdateien, erwartet 1 pro Lauf", n)
	}
	assertNoProbe(t, shared)
}

// TestMoveWithoutRenamerCopiesAsBefore - Hat die Quelle kein RenameNoReplace
// (altes Dateisystem, anderer Dienst), bleibt alles wie bisher: kopieren und
// löschen, ohne Fehler und ohne Prüfdatei zum Umbenennen.
func TestMoveWithoutRenamerCopiesAsBefore(t *testing.T) {
	shared := newCopySource()
	shared.addDir("/srv/b", 0o755)
	req := copyRequest{src: shared, dst: shared, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt", "notiz.txt"}, move: true, sameHost: true, sameAccount: true}
	res, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(res.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(res.failures))
	}
	if got, _ := shared.content("/srv/b/projekt/lib/x.go"); got != "x" || shared.node("/home/a/projekt") != nil || shared.node("/home/a/notiz.txt") != nil {
		t.Error("nicht verschoben")
	}
	assertNoProbe(t, shared)
}

// TestMoveCancelledBeforeStartNeverRenames - Fall 14: Ist der Kontext schon
// vor dem Start abgebrochen, geschieht nichts: kein Umbenennen, keine
// Prüfdatei, das Dateisystem bleibt unverändert. Das Ergebnis meldet den
// Abbruch und keinen einzigen Namen als erledigt (Build 90).
func TestMoveCancelledBeforeStartNeverRenames(t *testing.T) {
	src, dst, shared := renamePair()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := treeSnapshot(shared, "")
	res, _ := runCopySync(t, ctx, renameRequest(src, dst, "projekt", "notiz.txt"), noConflicts(t))
	if calls := src.renameCalls(); len(calls) != 0 {
		t.Errorf("nach Abbruch umbenannt: %v", calls)
	}
	if got := treeSnapshot(shared, ""); !reflect.DeepEqual(got, before) {
		t.Error("Dateisystem trotz Abbruch verändert")
	}
	assertNoProbe(t, shared)
	if !res.cancelled || len(res.copiedNames) != 0 {
		t.Errorf("cancelled = %v, copiedNames = %v; erwartet true und leer", res.cancelled, res.copiedNames)
	}
}

// TestCopyCancelledBeforeStartReportsCancelled - Abbruch vor dem Start ohne
// Umbenennen-Weg, Kopieren und Verschieben (Build 90, Frage 5): Das Ergebnis
// meldet cancelled == true und leere copiedNames (sonst würde die Oberfläche
// nach "Abbruch" Einträge als kopiert/verschoben markieren bzw. in der Quelle
// abwählen). Die Quelle ist unverändert (auch beim Verschieben), am Ziel wird
// nichts angelegt - kein Verzeichnis, keine Temp-Datei. Zusätzlich mit einem
// Namen, den es nicht gibt, und mit einem leeren Zielordner.
func TestCopyCancelledBeforeStartReportsCancelled(t *testing.T) {
	for _, move := range []bool{false, true} {
		for _, names := range [][]string{{"projekt", "notiz.txt"}, {"gibt-es-nicht"}, {"projekt"}} {
			t.Run(fmt.Sprintf("move=%v/%v", move, names), func(t *testing.T) {
				src, dst := newCopySource(), newMemFS("/srv/b")
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				srcBefore, dstBefore := treeSnapshot(src, ""), treeSnapshot(dst, "")
				req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: names, move: move}
				res, _ := runCopySync(t, ctx, req, noConflicts(t))
				if !res.cancelled {
					t.Error("Abbruch vor dem Start nicht gemeldet")
				}
				if len(res.copiedNames) != 0 {
					t.Errorf("copiedNames = %v, erwartet leer", res.copiedNames)
				}
				if got := treeSnapshot(src, ""); !reflect.DeepEqual(got, srcBefore) {
					t.Error("Quelle verändert")
				}
				if got := treeSnapshot(dst, ""); !reflect.DeepEqual(got, dstBefore) {
					t.Errorf("am Ziel etwas angelegt: %d statt %d Einträge", len(got), len(dstBefore))
				}
				assertNoTempFiles(t, dst)
			})
		}
	}
}

// TestMoveCancelledDuringRenamesStops - Wird mitten in einer langen Liste
// abgebrochen (im 3. Umbenennen), werden die übrigen nicht mehr umbenannt und
// bleiben in der Quelle. Umbenannt wird gleichzeitig (bis copyParallel), daher
// dürfen nach dem Abbruch noch die schon laufenden fertig werden. Ob das
// Ergebnis zusätzlich cancelled meldet, ist nicht festgelegt und wird hier
// nicht geprüft.
func TestMoveCancelledDuringRenamesStops(t *testing.T) {
	src, dst, shared := renamePair()
	const total = 300
	var names []string
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("n%03d.txt", i)
		shared.addFile("/home/a/"+name, "x", 0o644, copyTestTime)
		names = append(names, name)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	wrapped := &slowRenameFS{renameFS: src, onRename: func() {
		if calls.Add(1) == 3 {
			cancel()
		}
	}}
	req := renameRequest(src, dst, names...)
	req.src = wrapped
	runCopySync(t, ctx, req, noConflicts(t))
	if n := int(calls.Load()); n > 3+2*copyParallel {
		t.Errorf("%d Umbenennungen, nach dem Abbruch (bei 3) höchstens %d weitere erlaubt", n, 2*copyParallel)
	}
	// Die übrigen Einträge bleiben in der Quelle (nichts ungefragt verschoben)
	left := 0
	for _, name := range names {
		if shared.node("/home/a/"+name) != nil {
			left++
		}
	}
	if left < total-(3+2*copyParallel) {
		t.Errorf("nur %d von %d Dateien blieben in der Quelle", left, total)
	}
	assertNoProbe(t, shared)
}

// slowRenameFS verzögert RenameNoReplace um 3 ms und ruft danach einen Haken
// auf (Abbruch mitten in einer langen Liste).
type slowRenameFS struct {
	*renameFS
	onRename func()
}

func (c *slowRenameFS) RenameNoReplace(oldPath, newPath string) error {
	time.Sleep(3 * time.Millisecond)
	err := c.renameFS.RenameNoReplace(oldPath, newPath)
	c.onRename()
	return err
}

// --- localFS.RenameNoReplace (Fall 12) ---

// writeLocal legt eine Datei mit Inhalt an.
func writeLocal(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readLocal liefert den Inhalt einer Datei ("" wenn nicht lesbar).
func readLocal(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

// TestLocalRenameNoReplaceMoves - Datei und Verzeichnis (mit Inhalt) lassen
// sich verschieben; die Quelle ist danach weg, der Inhalt am Ziel.
func TestLocalRenameNoReplaceMoves(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Pfadform unter Windows anders (/C:/...)")
	}
	fsys, dir := newLocalTestFS(t)
	writeLocal(t, filepath.Join(dir, "a.txt"), "hallo")
	writeLocal(t, filepath.Join(dir, "d", "tief", "x.txt"), "x")
	home := filepath.ToSlash(dir)
	if err := fsys.RenameNoReplace(home+"/a.txt", home+"/b.txt"); err != nil {
		t.Fatalf("Datei: %v", err)
	}
	if readLocal(filepath.Join(dir, "b.txt")) != "hallo" {
		t.Error("Inhalt der Datei am Ziel falsch")
	}
	if _, err := os.Lstat(filepath.Join(dir, "a.txt")); !os.IsNotExist(err) {
		t.Error("Quelldatei noch da")
	}
	if err := fsys.RenameNoReplace(home+"/d", home+"/e"); err != nil {
		t.Fatalf("Verzeichnis: %v", err)
	}
	if readLocal(filepath.Join(dir, "e", "tief", "x.txt")) != "x" {
		t.Error("Inhalt des Verzeichnisses am Ziel falsch")
	}
	if _, err := os.Lstat(filepath.Join(dir, "d")); !os.IsNotExist(err) {
		t.Error("Quellverzeichnis noch da")
	}
}

// TestLocalRenameNoReplaceNeverReplaces - Ein vorhandenes Ziel (Datei,
// nicht leeres UND leeres Verzeichnis, Symlink - auch ein ins Leere zeigender)
// führt zum Fehler; Ziel und Quelle bleiben unverändert. Ein reines rename(2)
// würde Datei, leeres Verzeichnis und Symlink stillschweigend ersetzen.
func TestLocalRenameNoReplaceNeverReplaces(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Pfadform unter Windows anders (/C:/...)")
	}
	fsys, dir := newLocalTestFS(t)
	type target struct {
		name  string
		setup func(p string)
		check func(p string) bool
	}
	targets := []target{
		{"Datei", func(p string) { writeLocal(t, p, "ziel") }, func(p string) bool { return readLocal(p) == "ziel" }},
		{"leeres Verzeichnis", func(p string) { os.MkdirAll(p, 0o755) }, func(p string) bool {
			info, err := os.Lstat(p)
			entries, _ := os.ReadDir(p)
			return err == nil && info.IsDir() && len(entries) == 0
		}},
		{"volles Verzeichnis", func(p string) { writeLocal(t, filepath.Join(p, "drin.txt"), "drin") }, func(p string) bool { return readLocal(filepath.Join(p, "drin.txt")) == "drin" }},
		{"Symlink ins Leere", func(p string) { os.Symlink(filepath.Join(dir, "gibtsnicht"), p) }, func(p string) bool {
			info, err := os.Lstat(p)
			return err == nil && info.Mode()&os.ModeSymlink != 0
		}},
	}
	sources := []struct {
		name  string
		setup func(p string)
	}{
		{"Datei", func(p string) { writeLocal(t, p, "quelle") }},
		{"Verzeichnis", func(p string) { writeLocal(t, filepath.Join(p, "q.txt"), "quelle") }},
	}
	for _, tg := range targets {
		for _, sc := range sources {
			t.Run(sc.name+" auf "+tg.name, func(t *testing.T) {
				srcP, dstP := filepath.Join(dir, "q-"+sc.name+tg.name), filepath.Join(dir, "z-"+sc.name+tg.name)
				sc.setup(srcP)
				tg.setup(dstP)
				if err := fsys.RenameNoReplace(filepath.ToSlash(srcP), filepath.ToSlash(dstP)); err == nil {
					t.Fatal("kein Fehler, das Ziel wurde ersetzt")
				}
				if !tg.check(dstP) {
					t.Error("Ziel verändert")
				}
				if _, err := os.Lstat(srcP); err != nil {
					t.Errorf("Quelle verschwunden: %v", err)
				}
			})
		}
	}
}

// TestLocalRenameNoReplaceErrors - fehlende Quelle und fehlendes
// Elternverzeichnis des Ziels melden einen Fehler (kein Anlegen von
// Zwischenverzeichnissen).
func TestLocalRenameNoReplaceErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Pfadform unter Windows anders (/C:/...)")
	}
	fsys, dir := newLocalTestFS(t)
	home := filepath.ToSlash(dir)
	if err := fsys.RenameNoReplace(home+"/gibtsnicht", home+"/x"); err == nil {
		t.Error("fehlende Quelle ohne Fehler")
	}
	writeLocal(t, filepath.Join(dir, "a.txt"), "a")
	if err := fsys.RenameNoReplace(home+"/a.txt", home+"/fehlt/b.txt"); err == nil {
		t.Error("fehlendes Elternverzeichnis ohne Fehler")
	}
	if _, err := os.Lstat(filepath.Join(dir, "fehlt")); err == nil {
		t.Error("Zwischenverzeichnis angelegt")
	}
	if readLocal(filepath.Join(dir, "a.txt")) != "a" {
		t.Error("Quelle verändert")
	}
}

// TestLocalRenameNoReplaceMovesSymlinkItself - Ein Symlink wird selbst
// verschoben, nicht sein Ziel.
func TestLocalRenameNoReplaceMovesSymlinkItself(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Symlinks brauchen unter Windows Rechte")
	}
	fsys, dir := newLocalTestFS(t)
	home := filepath.ToSlash(dir)
	writeLocal(t, filepath.Join(dir, "echt.txt"), "echt")
	if err := os.Symlink("echt.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := fsys.RenameNoReplace(home+"/link", home+"/link2"); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(dir, "link2")); err != nil || target != "echt.txt" {
		t.Errorf("Link2 -> %q (%v)", target, err)
	}
	if readLocal(filepath.Join(dir, "echt.txt")) != "echt" {
		t.Error("Ziel des Links verändert")
	}
}

// TestLocalRenameNoReplaceIsAtomic - Viele gleichzeitige Umbenennungen auf
// DASSELBE Ziel (Datei bzw. leeres Verzeichnis): genau eine gelingt, die
// anderen scheitern. Eine Lösung aus "erst prüfen, dann umbenennen" ersetzte
// das Ziel im Rennen (Zeitfenster zwischen Prüfung und Umbenennen).
func TestLocalRenameNoReplaceIsAtomic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Pfadform unter Windows anders (/C:/...)")
	}
	for _, kind := range []string{"Datei", "leeres Verzeichnis"} {
		t.Run(kind, func(t *testing.T) {
			fsys, dir := newLocalTestFS(t)
			home := filepath.ToSlash(dir)
			const workers = 16
			for i := 0; i < workers; i++ {
				p := filepath.Join(dir, fmt.Sprintf("q%02d", i))
				if kind == "Datei" {
					writeLocal(t, p, fmt.Sprintf("inhalt %d", i))
				} else if err := os.MkdirAll(p, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			var wg sync.WaitGroup
			var ok atomic.Int32
			start := make(chan struct{})
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					if fsys.RenameNoReplace(fmt.Sprintf("%s/q%02d", home, i), home+"/ziel") == nil {
						ok.Add(1)
					}
				}(i)
			}
			close(start)
			wg.Wait()
			if n := ok.Load(); n != 1 {
				t.Errorf("%d Umbenennungen gelungen, erwartet genau 1", n)
			}
			left := 0
			for i := 0; i < workers; i++ {
				if _, err := os.Lstat(filepath.Join(dir, fmt.Sprintf("q%02d", i))); err == nil {
					left++
				}
			}
			if left != workers-1 {
				t.Errorf("%d Quellen übrig, erwartet %d", left, workers-1)
			}
		})
	}
}

// --- Oberfläche (Fall 13) ---

// openRenameManager öffnet den Datei-Manager über die ECHTE Auswahl
// (openPick/pickSide) mit zwei Verbindungseinträgen "Links" (id-0) und
// "Rechts" (id-1). Beide stehen zunächst auf demselben Host, Port und
// Benutzer (newListTestModel: 10.0.0.1:22, root); tune ändert die Einträge
// vor der Auswahl (nil = unverändert). Links Quelle /home/a, rechts Ziel
// /srv/b, beide über das jeweils übergebene Dateisystem.
func openRenameManager(t *testing.T, left, right remoteFS, tune func(left, right *Connection)) AppModel {
	t.Helper()
	m := newListTestModel(t, "Links", "Rechts")
	if tune != nil {
		tune(&m.connections[0], &m.connections[1])
	}
	m.fmConnect = func(conn Connection, password string) (remoteFS, error) {
		if conn.Name == "Links" {
			return left, nil
		}
		return right, nil
	}
	m.termWidth, m.termHeight = 100, 30
	m = openPick(t, m)
	m = pickSide(t, m, "Links")
	m = pickSide(t, m, "Rechts")
	if m.state != ViewFileManager {
		t.Fatalf("state = %v (%q)", m.state, m.fm.message)
	}
	if m.fm.panels[0].conn.ID == m.fm.panels[1].conn.ID {
		t.Fatalf("beide Fenster zeigen denselben Eintrag %q, erwartet zwei verschiedene", m.fm.panels[0].conn.ID)
	}
	if m.fm.panels[1].path != "/srv/b" {
		t.Fatalf("rechtes Fenster zeigt %q", m.fm.panels[1].path)
	}
	return m
}

// moveViaF6 verschiebt den Eintrag unter dem Cursor (oder die Markierung) mit
// F6 und Enter und liefert das Modell nach dem Neuladen beider Fenster.
func moveViaF6(t *testing.T, m AppModel) AppModel {
	t.Helper()
	m = pressRun(t, m, "f6")
	if m.fm.dialog != fmDialogCopyConfirm {
		t.Fatalf("Dialog = %v, erwartet die Bestätigung", m.fm.dialog)
	}
	m = pressRun(t, m, "enter")
	if m.fm.dialog != fmDialogNone {
		t.Fatalf("Dialog nach dem Verschieben noch offen: %v (%q)", m.fm.dialog, m.fm.message)
	}
	return m
}

// TestFileManagerF6SameAccountViaRealPick - Fall 13 (Nutzerentscheidung Build
// 90): fmStartCopy setzt sameAccount, wenn beide Fenster denselben Host (ohne
// Leerraum am Rand, ohne Groß-/Kleinschreibung), Port und Benutzer haben - auch
// bei ZWEI verschiedenen Verbindungseinträgen (id-0 und id-1, über die echte
// Auswahl gewählt). Dann benennt F6 genau einmal um. Gegenfälle: anderer
// Benutzer (auch nur andere Groß-/Kleinschreibung, der Benutzer wird exakt
// verglichen), anderer Port, anderer Host: kein Umbenennen, sondern Kopieren
// und Löschen (sonst gehörten die Dateien dem falschen Benutzer). In jedem Fall
// kommt alles am Ziel an und die Quelle ist weg. Beobachtet über das Verhalten
// (RenameNoReplace-Aufrufe der Quellhülle).
func TestFileManagerF6SameAccountViaRealPick(t *testing.T) {
	for _, tc := range []struct {
		name       string
		tune       func(a, b *Connection)
		wantRename bool
	}{
		{"zwei Einträge, alles gleich", func(a, b *Connection) {}, true},
		{"Host mit anderer Schreibweise und Leerraum", func(a, b *Connection) {
			a.Host, b.Host = "server.example", " Server.Example "
		}, true},
		{"Benutzer mit Leerraum am Rand", func(a, b *Connection) {
			a.User, b.User = "root", " root "
		}, true},
		{"anderer Benutzer", func(a, b *Connection) { b.User = "admin" }, false},
		{"Benutzer nur anders geschrieben", func(a, b *Connection) { a.User, b.User = "root", "Root" }, false},
		{"anderer Port", func(a, b *Connection) { b.Port = 2222 }, false},
		{"anderer Host", func(a, b *Connection) { b.Host = "10.0.0.2" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, dst, shared := renamePair()
			m := openRenameManager(t, src, dst, tc.tune)
			m = cursorTo(t, m, "projekt")
			m = moveViaF6(t, m)
			calls := src.renameCalls()
			if tc.wantRename {
				if want := [][2]string{{"/home/a/projekt", "/srv/b/projekt"}}; !reflect.DeepEqual(calls, want) {
					t.Errorf("RenameNoReplace-Aufrufe = %v, erwartet genau %v", calls, want)
				}
			} else if len(calls) != 0 {
				t.Errorf("umbenannt trotz anderem Konto: %v", calls)
			}
			// In allen Fällen kommt alles an und die Quelle ist weg
			if got, _ := shared.content("/srv/b/projekt/lib/x.go"); got != "x" || shared.node("/home/a/projekt") != nil {
				t.Errorf("projekt nicht verschoben (Meldung: %q)", m.fm.message)
			}
			if got, _ := shared.content("/srv/b/projekt/main.go"); got != "package main" {
				t.Errorf("main.go am Ziel = %q", got)
			}
			if names := entryNames(m.fm.panels[1]); !strings.Contains(strings.Join(names, ","), "projekt") {
				t.Errorf("Zielfenster nicht neu gelesen: %v", names)
			}
			if names := entryNames(m.fm.panels[0]); strings.Contains(strings.Join(names, ","), "projekt") {
				t.Errorf("Quellfenster zeigt projekt noch: %v", names)
			}
			assertNoProbe(t, shared)
		})
	}
}

// TestFMSameAccountAndSameHostTable - fmSameAccount/fmSameHost als reine
// Funktionen: gleicher Host (ohne Groß-/Kleinschreibung und Randleerraum),
// Port und Benutzer (exakt, nur Randleerraum egal) ist dasselbe Konto, auch bei
// verschiedener ID. Leerer Host oder Benutzer ist nie dasselbe Konto (im
// Zweifel kopieren). Der Lokale PC zählt nur mit sich selbst - nie mit einem
// Server, auch wenn dessen Angaben leer oder gleich wären.
func TestFMSameAccountAndSameHostTable(t *testing.T) {
	conn := func(id, host string, port int, user string) Connection {
		return Connection{ID: id, Host: host, Port: port, User: user}
	}
	local := Connection{ID: fmLocalConnID, Name: "Lokaler PC"}
	// Ein Server, der zufällig wie der Lokale PC aussieht (gleiche Felder)
	localLike := conn(fmLocalConnID, "", 0, "")
	for _, tc := range []struct {
		name        string
		a, b        Connection
		sameAccount bool
		sameHost    bool
	}{
		{"identisch", conn("a", "h.example", 22, "root"), conn("a", "h.example", 22, "root"), true, true},
		{"andere ID, sonst gleich", conn("a", "h.example", 22, "root"), conn("b", "h.example", 22, "root"), true, true},
		{"Host Groß-/Kleinschreibung", conn("a", "H.Example", 22, "root"), conn("b", "h.EXAMPLE", 22, "root"), true, true},
		{"Host mit Leerraum", conn("a", " h.example\t", 22, "root"), conn("b", "h.example", 22, "root"), true, true},
		{"Benutzer mit Leerraum", conn("a", "h.example", 22, " root"), conn("b", "h.example", 22, "root "), true, true},
		{"anderer Host", conn("a", "h.example", 22, "root"), conn("b", "i.example", 22, "root"), false, false},
		{"Host nur Präfix", conn("a", "h.example", 22, "root"), conn("b", "h.example.org", 22, "root"), false, false},
		{"anderer Port", conn("a", "h.example", 22, "root"), conn("b", "h.example", 2222, "root"), false, false},
		{"anderer Benutzer", conn("a", "h.example", 22, "root"), conn("b", "h.example", 22, "admin"), false, true},
		{"Benutzer Groß-/Kleinschreibung", conn("a", "h.example", 22, "root"), conn("b", "h.example", 22, "Root"), false, true},
		{"Benutzer leer (beide)", conn("a", "h.example", 22, ""), conn("b", "h.example", 22, ""), false, true},
		{"Benutzer nur Leerraum (beide)", conn("a", "h.example", 22, "  "), conn("b", "h.example", 22, " "), false, true},
		{"Benutzer leer (einer)", conn("a", "h.example", 22, ""), conn("b", "h.example", 22, "root"), false, true},
		{"Host leer (beide)", conn("a", "", 22, "root"), conn("b", "", 22, "root"), false, true},
		{"Host nur Leerraum (beide)", conn("a", " ", 22, "root"), conn("b", "", 22, "root"), false, true},
		{"Lokaler PC mit sich selbst", local, local, true, true},
		{"Lokaler PC mit Server", local, conn("a", "h.example", 22, "root"), false, false},
		{"Server mit Lokalem PC", conn("a", "h.example", 22, "root"), local, false, false},
		{"Lokaler PC mit Server gleicher Felder", localLike, conn("a", "", 0, ""), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := fmSameAccount(tc.a, tc.b); got != tc.sameAccount {
				t.Errorf("fmSameAccount = %v, erwartet %v", got, tc.sameAccount)
			}
			if got := fmSameAccount(tc.b, tc.a); got != tc.sameAccount {
				t.Errorf("fmSameAccount (vertauscht) = %v, erwartet %v", got, tc.sameAccount)
			}
			// Ob dasselbe Gerät vorliegt, ist nur für echte Server festgelegt
			if tc.a.ID != fmLocalConnID && tc.b.ID != fmLocalConnID && strings.TrimSpace(tc.a.Host) != "" {
				if got := fmSameHost(tc.a, tc.b); got != tc.sameHost {
					t.Errorf("fmSameHost = %v, erwartet %v", got, tc.sameHost)
				}
			}
		})
	}
}

// --- Abschlusstext beim Verschieben mit Umbenennen ---

// renameDoneText liefert die erwartete Meldung in der Sprache des Modells.
func renameDoneText(m AppModel, one bool, arg any) string {
	if one {
		return fmt.Sprintf(m.tr(txtFMMoveDoneOne), arg)
	}
	return fmt.Sprintf(m.tr(txtFMMoveDoneEntries), arg)
}

// markEntries markiert Einträge des aktiven Fensters (Einfg).
func markEntries(t *testing.T, m AppModel, names ...string) AppModel {
	t.Helper()
	for _, name := range names {
		m = cursorTo(t, m, name)
		m = pressRun(t, m, "insert")
	}
	if got := markedNames(m.fm.panels[m.fm.active]); len(got) != len(names) {
		t.Fatalf("markiert: %v, erwartet %v", got, names)
	}
	return m
}

// TestMoveDoneMessageAfterRename - Nach F6 mit Umbenennen steht in der Meldung
// weder "0 Dateien" noch eine Größe (es wurde nichts übertragen), sondern
// neutral: genau ein oberster Eintrag mit seinem Namen (txtFMMoveDoneOne),
// mehrere mit ihrer Zahl (txtFMMoveDoneEntries), auch gemischt (ein Eintrag
// umbenannt, ein zusammengeführter kopiert). Deutsch und Englisch, mit dem
// genauen Wortlaut, damit der Test nicht nur den Schlüssel spiegelt. Der Name
// kommt vom Server: ein Format-Steuerzeichen (U+202E, Rechts-nach-links-
// Überschreibung; kein Steuerzeichen für safeCopyName, also benennbar) darf
// nie unbereinigt im Text stehen.
func TestMoveDoneMessageAfterRename(t *testing.T) {
	rlo := "\u202e"
	for _, lang := range []struct {
		code Language
		one  string // Wortlaut bei einem Eintrag für den Namen "projekt"
		many string // Wortlaut bei zwei Einträgen
	}{
		{LangDeutsch, "\u201eprojekt\u201c verschoben", "2 Einträge verschoben"},
		{LangEnglish, "\u201cprojekt\u201d moved", "2 entries moved"},
	} {
		setLang := func(m AppModel) AppModel {
			m.language, m.lang = lang.code, GetTranslations(lang.code)
			return m
		}
		t.Run(string(lang.code)+"/ein Verzeichnis", func(t *testing.T) {
			src, dst, _ := renamePair()
			m := setLang(openRenameManager(t, src, dst, nil))
			m = cursorTo(t, m, "projekt")
			m = moveViaF6(t, m)
			if m.fm.message != lang.one {
				t.Errorf("Meldung = %q, erwartet %q", m.fm.message, lang.one)
			}
		})
		t.Run(string(lang.code)+"/mehrere", func(t *testing.T) {
			src, dst, shared := renamePair()
			m := setLang(openRenameManager(t, src, dst, nil))
			m = markEntries(t, m, "projekt", "notiz.txt")
			m = moveViaF6(t, m)
			if len(src.renameCalls()) != 2 {
				t.Fatalf("RenameNoReplace-Aufrufe = %v, erwartet 2", src.renameCalls())
			}
			if m.fm.message != lang.many {
				t.Errorf("Meldung = %q, erwartet %q", m.fm.message, lang.many)
			}
			if shared.node("/home/a/projekt") != nil || shared.node("/srv/b/notiz.txt") == nil {
				t.Error("nicht verschoben")
			}
		})
		t.Run(string(lang.code)+"/gemischt", func(t *testing.T) {
			// projekt gibt es am Ziel schon (zusammenführen = kopieren), notiz.txt
			// nicht (umbenennen): ein Teil umbenannt, ein Teil kopiert
			src, dst, shared := renamePair()
			shared.addFile("/srv/b/projekt/alt.txt", "alt", 0o644, copyTestTime)
			m := setLang(openRenameManager(t, src, dst, nil))
			m = markEntries(t, m, "projekt", "notiz.txt")
			m = moveViaF6(t, m)
			if got := src.renameCalls(); len(got) != 1 || got[0][0] != "/home/a/notiz.txt" {
				t.Fatalf("RenameNoReplace-Aufrufe = %v, erwartet nur notiz.txt", got)
			}
			if m.fm.message != lang.many {
				t.Errorf("Meldung = %q, erwartet %q (alle obersten Einträge gezählt)", m.fm.message, lang.many)
			}
			if got, _ := shared.content("/srv/b/projekt/main.go"); got != "package main" || shared.node("/home/a/projekt") != nil {
				t.Error("zusammengeführtes projekt nicht verschoben")
			}
		})
		t.Run(string(lang.code)+"/Name mit Formatzeichen", func(t *testing.T) {
			src, dst, shared := renamePair()
			evil := "evil" + rlo + "txt.exe"
			shared.addFile("/home/a/"+evil, "x", 0o644, copyTestTime)
			m := setLang(openRenameManager(t, src, dst, nil))
			m = cursorTo(t, m, evil)
			m = moveViaF6(t, m)
			if len(src.renameCalls()) != 1 {
				t.Fatalf("nicht umbenannt (%v), Meldung %q", src.renameCalls(), m.fm.message)
			}
			if strings.Contains(m.fm.message, rlo) {
				t.Errorf("Formatzeichen U+202E in der Meldung: %q", m.fm.message)
			}
			if want := renameDoneText(m, true, "evil?txt.exe"); m.fm.message != want {
				t.Errorf("Meldung = %q, erwartet %q", m.fm.message, want)
			}
		})
	}
}

// TestFinishCopyMessages - fmFinishCopy mit fertigen Ergebnissen: Mit
// Umbenennen gilt der neutrale Text nur beim Verschieben ohne Fehler und ohne
// Abbruch; Fehler und Abbruch behalten ihre Texte, Kopieren und Verschieben
// OHNE Umbenennen den alten Text mit Dateizahl und Größe. Ein Name mit
// Steuerzeichen (ESC) wird bereinigt, auch wenn die Eingabe unsauber ankommt.
func TestFinishCopyMessages(t *testing.T) {
	m, _ := openFileManagerFor(t)
	esc := string(rune(0x1b))
	finish := func(move bool, res copyResult) string {
		mm := m
		mm.fm.copy = fmCopyState{move: move}
		out, _ := mm.fmFinishCopy(res)
		return out.(AppModel).fm.message
	}
	oldMove := fmt.Sprintf(m.tr(txtFMMoveDone), 3, formatSizeWithUnit(10))
	oldCopy := fmt.Sprintf(m.tr(txtFMCopyDone), 3, formatSizeWithUnit(10))

	t.Run("Verschieben ohne Umbenennen: alter Text", func(t *testing.T) {
		got := finish(true, copyResult{files: 3, bytes: 10, copiedNames: []string{"a", "b"}})
		if got != oldMove {
			t.Errorf("Meldung = %q, erwartet %q", got, oldMove)
		}
	})
	t.Run("Kopieren: alter Text", func(t *testing.T) {
		got := finish(false, copyResult{files: 3, bytes: 10, copiedNames: []string{"a"}})
		if got != oldCopy {
			t.Errorf("Meldung = %q, erwartet %q", got, oldCopy)
		}
	})
	t.Run("Kopieren ignoriert renamed", func(t *testing.T) {
		// renamed gibt es nur beim Verschieben; beim Kopieren bleibt der alte Text
		got := finish(false, copyResult{files: 3, bytes: 10, renamed: 1, copiedNames: []string{"a"}})
		if got != oldCopy {
			t.Errorf("Meldung = %q, erwartet %q", got, oldCopy)
		}
	})
	t.Run("ein umbenannter Eintrag", func(t *testing.T) {
		got := finish(true, copyResult{renamed: 1, copiedNames: []string{"projekt"}})
		if want := renameDoneText(m, true, "projekt"); got != want {
			t.Errorf("Meldung = %q, erwartet %q", got, want)
		}
	})
	t.Run("mehrere, gemischt mit Kopiertem", func(t *testing.T) {
		got := finish(true, copyResult{renamed: 1, files: 4, bytes: 22, copiedNames: []string{"a", "b", "c"}})
		if want := renameDoneText(m, false, 3); got != want {
			t.Errorf("Meldung = %q, erwartet %q", got, want)
		}
	})
	t.Run("Steuerzeichen im Namen", func(t *testing.T) {
		got := finish(true, copyResult{renamed: 1, copiedNames: []string{"x" + esc + "[2Jy"}})
		if strings.ContainsRune(got, 0x1b) {
			t.Errorf("ESC in der Meldung: %q", got)
		}
		if want := renameDoneText(m, true, "x?[2Jy"); got != want {
			t.Errorf("Meldung = %q, erwartet %q", got, want)
		}
	})
	t.Run("Fehler bleibt Fehler", func(t *testing.T) {
		res := copyResult{renamed: 1, copiedNames: []string{"a"}, failures: []copyFailure{{path: "/p/q", err: errUnsafeName}}}
		got := finish(true, res)
		if strings.Contains(got, fmt.Sprintf(m.tr(txtFMMoveDoneOne), "a")) || !strings.Contains(got, "/p/q") {
			t.Errorf("Meldung = %q, erwartet den Fehlertext mit Pfad", got)
		}
	})
	t.Run("Abbruch bleibt Abbruch", func(t *testing.T) {
		got := finish(true, copyResult{renamed: 1, copiedNames: []string{"a"}, cancelled: true})
		if want := m.tr(txtFMMoveCancelled); got != want {
			t.Errorf("Meldung = %q, erwartet %q", got, want)
		}
	})
}
