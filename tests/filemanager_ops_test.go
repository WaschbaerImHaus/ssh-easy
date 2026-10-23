// Paket main - Tests für Verschieben (F6), Verzeichnis anlegen (F7) und
// Löschen (F8) im Datei-Manager (Build 65)
//
// Prüft die Fachlogik (runCopy mit move, runDelete) und den Ablauf in der
// Oberfläche:
//   - Verschieben = kopieren (Temp-Datei + Umbenennen), danach die Quelle
//     löschen - aber nur, was vollständig angekommen ist
//   - Verschieben auf sich selbst (auch über einen zweiten Verbindungs-
//     eintrag zum selben Server) und in sich selbst wird abgelehnt
//   - Löschen rekursiv, Symlinks werden nie verfolgt, Fehler brechen nicht ab
//   - F8 verlangt eine bewusste Bestätigung (j/y), Enter löscht nicht
//   - F7 prüft den Namen, q ist dort ein normales Zeichen
//
// Grundlage ist memFS aus filemanager_copy_test.go.
//
// @author Kurt Ingwer
// @date   2026-10-22 05:17
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"
	"time"
)

// moveRequest liefert einen Verschiebe-Auftrag von /home/a nach /srv/b.
func moveRequest(src, dst *memFS, names ...string) copyRequest {
	return copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: names, move: true}
}

// failureErrors liefert die Fehler eines Ergebnisses als Text (für Meldungen).
func failureErrors(failures []copyFailure) string {
	var parts []string
	for _, f := range failures {
		parts = append(parts, f.path+": "+f.err.Error())
	}
	return strings.Join(parts, "; ")
}

// --- Verschieben: Fachlogik ---

// TestMoveRemovesSourceAfterCopy - alles kommt an, die Quelle ist danach
// leer (Verzeichnisse, Dateien und Symlink).
func TestMoveRemovesSourceAfterCopy(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	result, _ := runCopySync(t, context.Background(), moveRequest(src, dst, "projekt", "notiz.txt"), noConflicts(t))
	if len(result.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(result.failures))
	}
	for p, want := range map[string]string{
		"/srv/b/projekt/main.go":  "package main",
		"/srv/b/projekt/run.sh":   "#!/bin/sh",
		"/srv/b/projekt/lib/x.go": "x",
		"/srv/b/notiz.txt":        "hallo",
	} {
		if got, _ := dst.content(p); got != want {
			t.Errorf("%s = %q, erwartet %q", p, got, want)
		}
	}
	if n := dst.node("/srv/b/projekt/aktuell"); n == nil || n.target != "main.go" {
		t.Error("Symlink nicht verschoben")
	}
	for _, p := range []string{"/home/a/projekt", "/home/a/projekt/main.go", "/home/a/projekt/lib", "/home/a/projekt/lib/x.go", "/home/a/projekt/aktuell", "/home/a/notiz.txt"} {
		if src.node(p) != nil {
			t.Errorf("%s nach dem Verschieben noch in der Quelle", p)
		}
	}
	if result.files != 5 {
		t.Errorf("files = %d, erwartet 5", result.files)
	}
	if strings.Join(result.copiedNames, ",") != "projekt,notiz.txt" {
		t.Errorf("copiedNames = %v", result.copiedNames)
	}
	assertNoTempFiles(t, dst)
}

// TestMoveKeepsSourceOfFailedFile - eine Datei scheitert beim Schreiben:
// sie und ihr Verzeichnis bleiben in der Quelle, der Rest wird verschoben.
// Das übrig bleibende Verzeichnis ist KEIN zusätzlicher Fehler.
func TestMoveKeepsSourceOfFailedFile(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	// main.go scheitert sofort (am Inhalt erkannt, nicht an der Reihenfolge)
	dst.failContent = "package main"
	dst.failAfter = 0
	result, _ := runCopySync(t, context.Background(), moveRequest(src, dst, "projekt"), noConflicts(t))
	if len(result.failures) != 1 {
		t.Fatalf("Fehler = %s, erwartet genau einen", failureErrors(result.failures))
	}
	if got, _ := src.content("/home/a/projekt/main.go"); got != "package main" {
		t.Error("fehlgeschlagene Datei aus der Quelle gelöscht")
	}
	if src.node("/home/a/projekt") == nil {
		t.Error("Verzeichnis mit verbliebener Datei gelöscht")
	}
	for _, p := range []string{"/home/a/projekt/run.sh", "/home/a/projekt/lib/x.go", "/home/a/projekt/lib", "/home/a/projekt/aktuell"} {
		if src.node(p) != nil {
			t.Errorf("%s nicht verschoben", p)
		}
	}
	if len(result.copiedNames) != 0 {
		t.Errorf("copiedNames = %v, erwartet leer (unvollständig)", result.copiedNames)
	}
	assertNoTempFiles(t, dst)
}

// TestMoveConflictSkipKeepsSource - übersprungene Datei bleibt in der
// Quelle, das Ziel bleibt unverändert.
func TestMoveConflictSkipKeepsSource(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	dst.addFile("/srv/b/notiz.txt", "alt", 0o644, copyTestTime)
	result, conflicts := runCopySync(t, context.Background(), moveRequest(src, dst, "notiz.txt"),
		func(string) copyConflictAnswer { return copyAnswerSkip })
	if len(conflicts) != 1 {
		t.Fatalf("Rückfragen = %v", conflicts)
	}
	if got, _ := src.content("/home/a/notiz.txt"); got != "hallo" {
		t.Error("übersprungene Datei aus der Quelle gelöscht")
	}
	if got, _ := dst.content("/srv/b/notiz.txt"); got != "alt" {
		t.Error("Ziel trotz Überspringen verändert")
	}
	if len(result.copiedNames) != 0 || len(result.failures) != 0 {
		t.Errorf("copiedNames = %v, Fehler = %v", result.copiedNames, result.failures)
	}
	assertNoTempFiles(t, dst)
}

// TestMoveConflictOverwrite - überschreiben, dann Quelle löschen.
func TestMoveConflictOverwrite(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	dst.addFile("/srv/b/notiz.txt", "alt", 0o644, copyTestTime)
	runCopySync(t, context.Background(), moveRequest(src, dst, "notiz.txt"),
		func(string) copyConflictAnswer { return copyAnswerOverwrite })
	if got, _ := dst.content("/srv/b/notiz.txt"); got != "hallo" {
		t.Errorf("Ziel = %q", got)
	}
	if src.node("/home/a/notiz.txt") != nil {
		t.Error("Quelle nach dem Überschreiben nicht gelöscht")
	}
	assertNoTempFiles(t, dst)
}

// TestMoveOntoItselfViaAlias - zwei Verbindungseinträge auf denselben
// Server (unterschiedliche Hostnamen, sameHost=false), beide Fenster im
// selben Verzeichnis: Ziel ist die Quelle selbst. Ohne Erkennung würde
// erst "kopiert" und dann das Original gelöscht - Datenverlust.
func TestMoveOntoItselfViaAlias(t *testing.T) {
	fsys := newCopySource()
	req := copyRequest{src: fsys, dst: fsys, srcDir: "/home/a", dstDir: "/home/a", names: []string{"notiz.txt", "projekt"}, move: true}
	result, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(result.failures) != 2 {
		t.Fatalf("Fehler = %s, erwartet 2", failureErrors(result.failures))
	}
	for _, f := range result.failures {
		if !errors.Is(f.err, errSameFile) {
			t.Errorf("%s: Fehler %v, erwartet errSameFile", f.path, f.err)
		}
	}
	for p, want := range map[string]string{
		"/home/a/notiz.txt":        "hallo",
		"/home/a/projekt/main.go":  "package main",
		"/home/a/projekt/lib/x.go": "x",
	} {
		if got, _ := fsys.content(p); got != want {
			t.Errorf("%s = %q, erwartet %q (Datenverlust!)", p, got, want)
		}
	}
	if fsys.node("/home/a/projekt/aktuell") == nil {
		t.Error("Symlink gelöscht")
	}
	assertNoTempFiles(t, fsys)
}

// TestMoveIntoItself - Verzeichnis in sich selbst bzw. in ein eigenes
// Unterverzeichnis verschieben wird abgelehnt, nichts ändert sich.
func TestMoveIntoItself(t *testing.T) {
	for _, dstDir := range []string{"/home/a/projekt", "/home/a/projekt/lib"} {
		fsys := newCopySource()
		req := copyRequest{src: fsys, dst: fsys, srcDir: "/home/a", dstDir: dstDir, names: []string{"projekt"}, move: true, sameHost: true}
		result, _ := runCopySync(t, context.Background(), req, noConflicts(t))
		if len(result.failures) != 1 || !errors.Is(result.failures[0].err, errMoveIntoItself) {
			t.Fatalf("%s: Fehler = %s, erwartet errMoveIntoItself", dstDir, failureErrors(result.failures))
		}
		if fsys.node(dstDir+"/projekt") != nil {
			t.Errorf("%s: trotzdem angelegt", dstDir)
		}
		if got, _ := fsys.content("/home/a/projekt/main.go"); got != "package main" {
			t.Errorf("%s: Quelle verändert", dstDir)
		}
	}
}

// TestMoveIntoSiblingOnSameHost - ein Nachbarverzeichnis mit ähnlichem
// Namen ("projekt2") ist NICHT "in sich selbst".
func TestMoveIntoSiblingOnSameHost(t *testing.T) {
	fsys := newCopySource()
	fsys.addDir("/home/a/projekt2", 0o755)
	req := copyRequest{src: fsys, dst: fsys, srcDir: "/home/a", dstDir: "/home/a/projekt2", names: []string{"projekt"}, move: true, sameHost: true}
	result, _ := runCopySync(t, context.Background(), req, noConflicts(t))
	if len(result.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(result.failures))
	}
	if got, _ := fsys.content("/home/a/projekt2/projekt/main.go"); got != "package main" {
		t.Error("nicht verschoben")
	}
	if fsys.node("/home/a/projekt") != nil {
		t.Error("Quelle nicht entfernt")
	}
}

// TestMoveSourceRemoveFails - Quelle lässt sich nicht löschen: Fehler mit
// Quellpfad, die Kopie bleibt am Ziel.
func TestMoveSourceRemoveFails(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	src.noRemove["/home/a/notiz.txt"] = true
	result, _ := runCopySync(t, context.Background(), moveRequest(src, dst, "notiz.txt"), noConflicts(t))
	if len(result.failures) != 1 || result.failures[0].path != "/home/a/notiz.txt" {
		t.Fatalf("Fehler = %s", failureErrors(result.failures))
	}
	if got, _ := dst.content("/srv/b/notiz.txt"); got != "hallo" {
		t.Error("Kopie am Ziel fehlt")
	}
	if len(result.copiedNames) != 0 {
		t.Errorf("copiedNames = %v", result.copiedNames)
	}
}

// TestMoveCancelledKeepsSource - Abbruch während des Schreibens: die
// gerade übertragene Datei bleibt in der Quelle, am Ziel liegt nichts halbes.
func TestMoveCancelledKeepsSource(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	ctx, cancel := context.WithCancel(context.Background())
	dst.onWrite = cancel
	result, _ := runCopySync(t, ctx, moveRequest(src, dst, "notiz.txt"), noConflicts(t))
	if !result.cancelled {
		t.Error("nicht als abgebrochen gemeldet")
	}
	if got, _ := src.content("/home/a/notiz.txt"); got != "hallo" {
		t.Error("Quelle trotz Abbruch gelöscht")
	}
	if _, ok := dst.content("/srv/b/notiz.txt"); ok {
		t.Error("halbe Datei am Ziel")
	}
	assertNoTempFiles(t, dst)
}

// TestMoveEmptyDirectory - leeres Verzeichnis wird verschoben.
func TestMoveEmptyDirectory(t *testing.T) {
	src, dst := newMemFS("/home/a"), newMemFS("/srv/b")
	src.addDir("/home/a/leer", 0o750)
	result, _ := runCopySync(t, context.Background(), moveRequest(src, dst, "leer"), noConflicts(t))
	if len(result.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(result.failures))
	}
	if n := dst.node("/srv/b/leer"); n == nil || !n.mode.IsDir() {
		t.Error("Verzeichnis am Ziel fehlt")
	}
	if src.node("/home/a/leer") != nil {
		t.Error("leeres Verzeichnis in der Quelle geblieben")
	}
}

// TestMoveMergesIntoExistingDirectory - vorhandenes Zielverzeichnis wird
// zusammengeführt (anderer Server), die Quelle danach entfernt; die
// Prüfdatei für "dasselbe Verzeichnis?" bleibt nicht liegen.
func TestMoveMergesIntoExistingDirectory(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	dst.addFile("/srv/b/projekt/vorhanden.txt", "bleibt", 0o644, copyTestTime)
	result, _ := runCopySync(t, context.Background(), moveRequest(src, dst, "projekt"), noConflicts(t))
	if len(result.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(result.failures))
	}
	if got, _ := dst.content("/srv/b/projekt/vorhanden.txt"); got != "bleibt" {
		t.Error("vorhandene Datei am Ziel verändert")
	}
	if got, _ := dst.content("/srv/b/projekt/main.go"); got != "package main" {
		t.Error("nicht zusammengeführt")
	}
	if src.node("/home/a/projekt") != nil {
		t.Error("Quelle nicht entfernt")
	}
	assertNoTempFiles(t, dst)
}

// TestCopyDoesNotRemoveSource - ohne move bleibt die Quelle unverändert
// (Gegenprobe).
func TestCopyDoesNotRemoveSource(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	req := moveRequest(src, dst, "projekt", "notiz.txt")
	req.move = false
	runCopySync(t, context.Background(), req, noConflicts(t))
	if got, _ := src.content("/home/a/notiz.txt"); got != "hallo" {
		t.Error("Kopieren hat die Quelle gelöscht")
	}
	if src.node("/home/a/projekt/lib/x.go") == nil {
		t.Error("Kopieren hat die Quelle gelöscht")
	}
}

// --- Löschen: Fachlogik ---

// runDeleteSync führt runDelete aus und liefert das Ergebnis.
func runDeleteSync(t *testing.T, ctx context.Context, fsys remoteFS, names ...string) deleteResult {
	t.Helper()
	events := make(chan deleteEvent, 16)
	go runDelete(ctx, deleteRequest{fs: fsys, dir: "/home/a", names: names}, events)
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.result != nil {
				return *ev.result
			}
		case <-timeout:
			t.Fatal("runDelete hängt")
		}
	}
}

// TestDeleteRecursive - Verzeichnisse samt Inhalt, Dateien, Symlinks.
func TestDeleteRecursive(t *testing.T) {
	fsys := newCopySource()
	result := runDeleteSync(t, context.Background(), fsys, "projekt", "notiz.txt")
	if len(result.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(result.failures))
	}
	fsys.mu.Lock()
	for p := range fsys.nodes {
		if strings.HasPrefix(p, "/home/a/") {
			t.Errorf("%s nicht gelöscht", p)
		}
	}
	fsys.mu.Unlock()
	if result.deleted != 7 {
		t.Errorf("deleted = %d, erwartet 7", result.deleted)
	}
	if strings.Join(result.deletedNames, ",") != "projekt,notiz.txt" {
		t.Errorf("deletedNames = %v", result.deletedNames)
	}
	if fsys.node("/home/a") == nil {
		t.Error("Elternverzeichnis gelöscht")
	}
}

// TestDeleteSymlinkToDirKeepsTarget - ein Symlink auf ein Verzeichnis wird
// selbst gelöscht, sein Ziel samt Inhalt bleibt.
func TestDeleteSymlinkToDirKeepsTarget(t *testing.T) {
	fsys := newMemFS("/home/a")
	fsys.addFile("/home/a/ziel/wichtig.txt", "bleibt", 0o644, copyTestTime)
	fsys.addLink("/home/a/link", "ziel")
	fsys.addLink("/home/a/abs", "/home/a/ziel")
	result := runDeleteSync(t, context.Background(), fsys, "link", "abs")
	if len(result.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(result.failures))
	}
	if fsys.node("/home/a/link") != nil || fsys.node("/home/a/abs") != nil {
		t.Error("Symlink nicht gelöscht")
	}
	if got, _ := fsys.content("/home/a/ziel/wichtig.txt"); got != "bleibt" {
		t.Error("Ziel des Symlinks gelöscht")
	}
}

// TestDeleteContinuesAfterError - nicht löschbare Datei: sie und ihr
// Verzeichnis bleiben, alles andere wird gelöscht.
func TestDeleteContinuesAfterError(t *testing.T) {
	fsys := newCopySource()
	fsys.noRemove["/home/a/projekt/main.go"] = true
	result := runDeleteSync(t, context.Background(), fsys, "projekt", "notiz.txt")
	if len(result.failures) != 1 || result.failures[0].path != "/home/a/projekt/main.go" {
		t.Fatalf("Fehler = %s, erwartet nur main.go", failureErrors(result.failures))
	}
	if fsys.node("/home/a/projekt/main.go") == nil || fsys.node("/home/a/projekt") == nil {
		t.Error("nicht löschbare Datei bzw. ihr Verzeichnis verschwunden")
	}
	for _, p := range []string{"/home/a/projekt/run.sh", "/home/a/projekt/lib", "/home/a/notiz.txt"} {
		if fsys.node(p) != nil {
			t.Errorf("%s nicht gelöscht", p)
		}
	}
	if strings.Join(result.deletedNames, ",") != "notiz.txt" {
		t.Errorf("deletedNames = %v", result.deletedNames)
	}
}

// TestDeleteUnreadableDirectory - nicht lesbares Verzeichnis bleibt stehen.
func TestDeleteUnreadableDirectory(t *testing.T) {
	fsys := newCopySource()
	fsys.denied["/home/a/projekt/lib"] = true
	result := runDeleteSync(t, context.Background(), fsys, "projekt")
	if len(result.failures) != 1 || result.failures[0].path != "/home/a/projekt/lib" {
		t.Fatalf("Fehler = %s", failureErrors(result.failures))
	}
	if fsys.node("/home/a/projekt/lib/x.go") == nil {
		t.Error("Inhalt eines nicht lesbaren Verzeichnisses gelöscht")
	}
	if fsys.node("/home/a/projekt/main.go") != nil {
		t.Error("lesbare Nachbardatei nicht gelöscht")
	}
}

// TestDeleteRefusesUnsafeNames - ein bösartiger Server liefert Namen wie
// "../x": sie werden nicht gelöscht (nichts außerhalb des Verzeichnisses),
// das Verzeichnis bleibt deshalb stehen.
func TestDeleteRefusesUnsafeNames(t *testing.T) {
	base := newCopySource()
	base.addFile("/home/x", "fremd", 0o644, copyTestTime)
	evil := &evilFS{memFS: base, names: []string{"../../x", "a\\b"}}
	result := runDeleteSync(t, context.Background(), evil, "projekt")
	if got, _ := base.content("/home/x"); got != "fremd" {
		t.Fatal("Datei außerhalb des Verzeichnisses gelöscht")
	}
	unsafe := 0
	for _, f := range result.failures {
		if errors.Is(f.err, errUnsafeName) {
			unsafe++
		}
	}
	if unsafe == 0 {
		t.Errorf("keine errUnsafeName-Meldung: %s", failureErrors(result.failures))
	}
	if base.node("/home/a/projekt") == nil {
		t.Error("Verzeichnis mit unzulässigen Einträgen gelöscht")
	}
	// Auch ein unzulässiger Name in der Auswahl selbst wird abgelehnt
	result = runDeleteSync(t, context.Background(), base, "../x")
	if len(result.failures) != 1 || !errors.Is(result.failures[0].err, errUnsafeName) {
		t.Errorf("Auswahl '../x': %s", failureErrors(result.failures))
	}
	if got, _ := base.content("/home/x"); got != "fremd" {
		t.Error("Datei außerhalb gelöscht")
	}
}

// TestDeleteCancelled - abgebrochener Kontext: nichts wird gelöscht.
func TestDeleteCancelled(t *testing.T) {
	fsys := newCopySource()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := runDeleteSync(t, ctx, fsys, "projekt", "notiz.txt")
	if !result.cancelled {
		t.Error("nicht als abgebrochen gemeldet")
	}
	if fsys.node("/home/a/notiz.txt") == nil || fsys.node("/home/a/projekt/main.go") == nil {
		t.Error("trotz Abbruch gelöscht")
	}
}

// TestDeleteDepthLimit - zu tief verschachtelt: Fehler, oberstes
// Verzeichnis bleibt.
func TestDeleteDepthLimit(t *testing.T) {
	old := copyMaxDepth
	copyMaxDepth = 3
	defer func() { copyMaxDepth = old }()
	fsys := newMemFS("/home/a")
	fsys.addFile("/home/a/t/1/2/3/4/5/f", "x", 0o644, copyTestTime)
	result := runDeleteSync(t, context.Background(), fsys, "t")
	found := false
	for _, f := range result.failures {
		found = found || errors.Is(f.err, errCopyLimit)
	}
	if !found {
		t.Errorf("keine errCopyLimit-Meldung: %s", failureErrors(result.failures))
	}
	if fsys.node("/home/a/t") == nil {
		t.Error("oberstes Verzeichnis trotz Grenze gelöscht")
	}
}

// TestDeleteProgressEvents - Fortschritt nennt die Zahl gelöschter Einträge.
func TestDeleteProgressEvents(t *testing.T) {
	fsys := newCopySource()
	events := make(chan deleteEvent, 64)
	go runDelete(context.Background(), deleteRequest{fs: fsys, dir: "/home/a", names: []string{"projekt"}}, events)
	last := -1
	for ev := range events {
		if ev.progress != nil {
			if ev.progress.done < last {
				t.Errorf("Fortschritt rückwärts: %d nach %d", ev.progress.done, last)
			}
			last = ev.progress.done
		}
		if ev.result != nil {
			if ev.result.deleted != 6 {
				t.Errorf("deleted = %d, erwartet 6", ev.result.deleted)
			}
			break
		}
	}
}

// --- Oberfläche: F6 Verschieben ---

// TestFileManagerF6MovesMarked - Bestätigung, Verschieben, beide Fenster
// neu gelesen, Meldung.
func TestFileManagerF6MovesMarked(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	m := openCopyManager(t, src, dst)
	m = cursorTo(t, m, "projekt")
	m = pressRun(t, m, "insert")
	m = cursorTo(t, m, "notiz.txt")
	m = pressRun(t, m, "insert")
	m = pressRun(t, m, "f6")
	if m.fm.dialog != fmDialogCopyConfirm || !m.fm.copy.move {
		t.Fatalf("Dialog %v / move %v, erwartet Verschiebe-Bestätigung", m.fm.dialog, m.fm.copy.move)
	}
	text := stripANSI(m.View())
	if !strings.Contains(text, m.tr(txtFMMoveTitle)) || !strings.Contains(text, fmt.Sprintf(m.tr(txtFMMoveConfirmMany), 2, "root@10.0.0.2:/srv/b")) {
		t.Errorf("Bestätigung ohne Verschiebe-Text:\n%s", text)
	}
	assertScreenSize(t, m)
	m = pressRun(t, m, "enter")
	if m.fm.dialog != fmDialogNone {
		t.Fatalf("Dialog nach dem Verschieben noch offen: %v", m.fm.dialog)
	}
	if got, _ := dst.content("/srv/b/projekt/lib/x.go"); got != "x" {
		t.Error("nicht verschoben")
	}
	left := strings.Join(entryNames(m.fm.panels[0]), ",")
	if strings.Contains(left, "projekt") || strings.Contains(left, "notiz.txt") {
		t.Errorf("Quellfenster nicht neu gelesen: %s", left)
	}
	right := strings.Join(entryNames(m.fm.panels[1]), ",")
	if !strings.Contains(right, "projekt") || !strings.Contains(right, "notiz.txt") {
		t.Errorf("Zielfenster nicht neu gelesen: %s", right)
	}
	if want := fmt.Sprintf(m.tr(txtFMMoveDone), 5, formatSizeWithUnit(27)); m.fm.message != want {
		t.Errorf("Meldung = %q, erwartet %q", m.fm.message, want)
	}
}

// TestFileManagerF6ConfirmOneAndCancel - ein Eintrag, Esc und q brechen ab.
func TestFileManagerF6ConfirmOneAndCancel(t *testing.T) {
	for _, key := range []string{"esc", "q"} {
		src, dst := newCopySource(), newMemFS("/srv/b")
		m := openCopyManager(t, src, dst)
		m = cursorTo(t, m, "notiz.txt")
		m = pressRun(t, m, "f6")
		if !strings.Contains(stripANSI(m.View()), fmt.Sprintf(m.tr(txtFMMoveConfirmOne), "notiz.txt", "root@10.0.0.2:/srv/b")) {
			t.Errorf("%s: Bestätigung für einen Eintrag fehlt", key)
		}
		m = pressRun(t, m, key)
		if m.fm.dialog != fmDialogNone || m.state != ViewFileManager {
			t.Errorf("%s: Dialog %v / state %v", key, m.fm.dialog, m.state)
		}
		if src.node("/home/a/notiz.txt") == nil {
			t.Errorf("%s: trotz Abbruch verschoben", key)
		}
	}
}

// TestFileManagerF6ErrorKeepsMark - fehlgeschlagener Eintrag bleibt in der
// Quelle markiert, der Rest ist weg.
func TestFileManagerF6ErrorKeepsMark(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	src.noRemove["/home/a/notiz.txt"] = true
	m := openCopyManager(t, src, dst)
	m = cursorTo(t, m, "projekt")
	m = pressRun(t, m, "insert")
	m = cursorTo(t, m, "notiz.txt")
	m = pressRun(t, m, "insert")
	m = pressRun(t, m, "f6")
	m = pressRun(t, m, "enter")
	if got := strings.Join(markedNames(m.fm.panels[0]), ","); got != "notiz.txt" {
		t.Errorf("Markierungen = %q, erwartet notiz.txt", got)
	}
	prefix := strings.SplitN(m.tr(txtFMMoveErrors), "%", 2)[0]
	if !strings.HasPrefix(m.fm.message, prefix) {
		t.Errorf("Meldung = %q, erwartet Fehlertext", m.fm.message)
	}
}

// TestFileManagerF6IntoItselfMessage - Meldung mit Übersetzung.
func TestFileManagerF6IntoItselfMessage(t *testing.T) {
	fsys := newCopySource()
	m := newListTestModel(t, "Links", "Rechts")
	m.fmConnect = func(conn Connection, password string) (remoteFS, error) { return fsys, nil }
	m.termWidth, m.termHeight = 100, 30
	m = openPick(t, m)
	m = pickSide(t, m, "Links")
	m = pickSide(t, m, "Rechts")
	// Rechts in projekt/lib wechseln
	m = pressRun(t, m, "tab")
	m = cursorTo(t, m, "projekt")
	m = pressRun(t, m, "enter")
	m = cursorTo(t, m, "lib")
	m = pressRun(t, m, "enter")
	m = pressRun(t, m, "tab")
	m = cursorTo(t, m, "projekt")
	m = pressRun(t, m, "f6")
	m = pressRun(t, m, "enter")
	if want := fmt.Sprintf(m.tr(txtFMErrIntoItself), "/home/a/projekt"); !strings.Contains(m.fm.message, want) {
		t.Errorf("Meldung = %q, erwartet %q", m.fm.message, want)
	}
	if got, _ := fsys.content("/home/a/projekt/main.go"); got != "package main" {
		t.Error("Quelle verändert")
	}
}

// --- Oberfläche: F7 Verzeichnis anlegen ---

// openMkdir öffnet den Datei-Manager und den F7-Dialog.
func openMkdir(t *testing.T) (AppModel, *memFS) {
	t.Helper()
	src := newCopySource()
	m := openCopyManager(t, src, newMemFS("/srv/b"))
	m = pressRun(t, m, "f7")
	if m.fm.dialog != fmDialogMkdir {
		t.Fatalf("Dialog = %v, erwartet MkDir", m.fm.dialog)
	}
	return m, src
}

// TestFileManagerF7CreatesDirectory - Name tippen, Enter: Verzeichnis
// angelegt, Cursor steht darauf.
func TestFileManagerF7CreatesDirectory(t *testing.T) {
	m, src := openMkdir(t)
	if !strings.Contains(stripANSI(m.View()), m.tr(txtFMMkdirPrompt)) {
		t.Error("Eingabeaufforderung fehlt")
	}
	assertScreenSize(t, m)
	m = typeText(t, m, "neu qx")
	m = pressRun(t, m, "enter")
	if m.state != ViewFileManager || m.fm.dialog != fmDialogNone {
		t.Fatalf("state %v / Dialog %v", m.state, m.fm.dialog)
	}
	if n := src.node("/home/a/neu qx"); n == nil || !n.mode.IsDir() {
		t.Fatal("Verzeichnis nicht angelegt")
	}
	if entry, ok := m.fm.panels[0].current(); !ok || entry.name != "neu qx" {
		t.Errorf("Cursor auf %q, erwartet das neue Verzeichnis", entry.name)
	}
	if want := fmt.Sprintf(m.tr(txtFMMkdirDone), "neu qx"); m.fm.message != want {
		t.Errorf("Meldung = %q, erwartet %q", m.fm.message, want)
	}
}

// TestFileManagerF7EscCancels - Esc schließt, nichts wird angelegt.
func TestFileManagerF7EscCancels(t *testing.T) {
	m, src := openMkdir(t)
	m = typeText(t, m, "abc")
	m = pressRun(t, m, "esc")
	if m.fm.dialog != fmDialogNone || m.state != ViewFileManager {
		t.Errorf("Dialog %v / state %v", m.fm.dialog, m.state)
	}
	if src.node("/home/a/abc") != nil {
		t.Error("trotz Esc angelegt")
	}
}

// TestFileManagerF7EmptyName - Enter ohne Namen bewirkt nichts.
func TestFileManagerF7EmptyName(t *testing.T) {
	m, _ := openMkdir(t)
	m = pressRun(t, m, "enter")
	if m.fm.dialog != fmDialogMkdir {
		t.Errorf("Dialog = %v, erwartet weiter MkDir", m.fm.dialog)
	}
}

// TestFileManagerF7InvalidNames - Pfadtrenner, "." und ".." werden
// abgelehnt; der Dialog bleibt offen.
func TestFileManagerF7InvalidNames(t *testing.T) {
	for _, name := range []string{"a/b", "..", ".", "x\\y", "../../etc"} {
		m, src := openMkdir(t)
		m = typeText(t, m, name)
		m = pressRun(t, m, "enter")
		if m.fm.dialog != fmDialogMkdir {
			t.Errorf("%q: Dialog geschlossen", name)
		}
		if m.fm.message != m.tr(txtFMMkdirBadName) {
			t.Errorf("%q: Meldung = %q", name, m.fm.message)
		}
		src.mu.Lock()
		count := len(src.nodes)
		src.mu.Unlock()
		if count != len(newCopySource().nodes) {
			t.Errorf("%q: Dateisystem verändert", name)
		}
	}
}

// TestFileManagerF7ControlCharacter - Steuerzeichen (z. B. eingefügt)
// landen nie in einem Verzeichnisnamen - egal ob die Eingabezeile sie
// schon entfernt oder die Namensprüfung sie ablehnt.
func TestFileManagerF7ControlCharacter(t *testing.T) {
	m, src := openMkdir(t)
	m.fm.mkdirInput.SetValue("a\x1bb")
	m = pressRun(t, m, "enter")
	src.mu.Lock()
	defer src.mu.Unlock()
	for p := range src.nodes {
		if strings.ContainsRune(p, 0x1b) {
			t.Errorf("Verzeichnis mit Steuerzeichen angelegt: %q", p)
		}
	}
}

// TestFileManagerF7Exists - vorhandener Name: Meldung, nichts verändert.
func TestFileManagerF7Exists(t *testing.T) {
	m, src := openMkdir(t)
	m = typeText(t, m, "notiz.txt")
	m = pressRun(t, m, "enter")
	if want := fmt.Sprintf(m.tr(txtFMMkdirExists), "notiz.txt"); m.fm.message != want {
		t.Errorf("Meldung = %q, erwartet %q", m.fm.message, want)
	}
	if got, _ := src.content("/home/a/notiz.txt"); got != "hallo" {
		t.Error("vorhandene Datei verändert")
	}
}

// TestFileManagerF7Error - Server verweigert das Anlegen.
func TestFileManagerF7Error(t *testing.T) {
	m, src := openMkdir(t)
	src.denied["/home/a"] = true
	m = typeText(t, m, "neu")
	m = pressRun(t, m, "enter")
	prefix := strings.SplitN(m.tr(txtFMErrMkdir), "%", 2)[0]
	if !strings.HasPrefix(m.fm.message, prefix) || !strings.Contains(m.fm.message, "/home/a/neu") {
		t.Errorf("Meldung = %q, erwartet Fehler beim Anlegen", m.fm.message)
	}
}

// TestFileManagerF7InRightPanel - legt im aktiven (rechten) Fenster an.
func TestFileManagerF7InRightPanel(t *testing.T) {
	src, dst := newCopySource(), newMemFS("/srv/b")
	m := openCopyManager(t, src, dst)
	m = pressRun(t, m, "tab")
	m = pressRun(t, m, "f7")
	m = typeText(t, m, "rechts")
	m = pressRun(t, m, "enter")
	if dst.node("/srv/b/rechts") == nil || src.node("/home/a/rechts") != nil {
		t.Error("nicht im aktiven Fenster angelegt")
	}
}

// --- Oberfläche: F8 Löschen ---

// TestFileManagerF8RequiresDeliberateConfirm - Enter löscht NICHT (kein
// versehentliches F8-Enter), n/Esc/q brechen ab.
func TestFileManagerF8RequiresDeliberateConfirm(t *testing.T) {
	for _, key := range []string{"n", "esc", "q"} {
		src := newCopySource()
		m := openCopyManager(t, src, newMemFS("/srv/b"))
		m = cursorTo(t, m, "notiz.txt")
		m = pressRun(t, m, "f8")
		if m.fm.dialog != fmDialogDeleteConfirm {
			t.Fatalf("Dialog = %v, erwartet Lösch-Bestätigung", m.fm.dialog)
		}
		if !strings.Contains(stripANSI(m.View()), fmt.Sprintf(m.tr(txtFMDeleteConfirmOne), "notiz.txt")) {
			t.Error("Bestätigung nennt die Datei nicht")
		}
		assertScreenSize(t, m)
		m = pressRun(t, m, "enter")
		if m.fm.dialog != fmDialogDeleteConfirm || src.node("/home/a/notiz.txt") == nil {
			t.Fatal("Enter hat gelöscht bzw. den Dialog geschlossen")
		}
		m = pressRun(t, m, key)
		if m.fm.dialog != fmDialogNone || m.state != ViewFileManager {
			t.Errorf("%s: Dialog %v / state %v", key, m.fm.dialog, m.state)
		}
		if src.node("/home/a/notiz.txt") == nil {
			t.Errorf("%s: trotz Abbruch gelöscht", key)
		}
	}
}

// TestFileManagerF8DeletesMarked - j löscht die markierten Einträge
// rekursiv, das Fenster wird neu gelesen.
func TestFileManagerF8DeletesMarked(t *testing.T) {
	for _, key := range []string{"j", "y"} {
		src := newCopySource()
		m := openCopyManager(t, src, newMemFS("/srv/b"))
		m = cursorTo(t, m, "projekt")
		m = pressRun(t, m, "insert")
		m = cursorTo(t, m, "notiz.txt")
		m = pressRun(t, m, "insert")
		m = pressRun(t, m, "f8")
		text := stripANSI(m.View())
		if !strings.Contains(text, fmt.Sprintf(m.tr(txtFMDeleteConfirmMany), 2)) {
			t.Errorf("%s: Anzahl fehlt:\n%s", key, text)
		}
		if !strings.Contains(strings.Join(strings.Fields(text), " "), firstWords(m.tr(txtFMDeleteDirsNote), 2)) {
			t.Errorf("%s: Hinweis auf rekursives Löschen fehlt", key)
		}
		m = pressRun(t, m, key)
		if m.fm.dialog != fmDialogNone {
			t.Fatalf("%s: Dialog %v nach dem Löschen", key, m.fm.dialog)
		}
		if src.node("/home/a/projekt") != nil || src.node("/home/a/notiz.txt") != nil {
			t.Errorf("%s: nicht gelöscht", key)
		}
		if names := strings.Join(entryNames(m.fm.panels[0]), ","); strings.Contains(names, "projekt") || strings.Contains(names, "notiz") {
			t.Errorf("%s: Fenster nicht neu gelesen: %s", key, names)
		}
		if want := fmt.Sprintf(m.tr(txtFMDeleteDone), 7); m.fm.message != want {
			t.Errorf("%s: Meldung = %q, erwartet %q", key, m.fm.message, want)
		}
	}
}

// firstWords liefert die ersten n Wörter eines Textes (Dialoge brechen um).
func firstWords(s string, n int) string {
	words := strings.Fields(s)
	if len(words) > n {
		words = words[:n]
	}
	return strings.Join(words, " ")
}

// TestFileManagerF8FileHasNoDirsNote - bei reinen Dateien kein Hinweis
// auf Verzeichnisse.
func TestFileManagerF8FileHasNoDirsNote(t *testing.T) {
	m := openCopyManager(t, newCopySource(), newMemFS("/srv/b"))
	m = cursorTo(t, m, "notiz.txt")
	m = pressRun(t, m, "f8")
	_, lines, _ := m.fmDialogLines(60)
	if strings.Contains(strings.Join(lines, " "), firstWords(m.tr(txtFMDeleteDirsNote), 2)) {
		t.Error("Hinweis auf Verzeichnisse bei einer Datei")
	}
}

// TestFileManagerF8NothingSelected - Cursor auf "..": Meldung, kein Dialog.
func TestFileManagerF8NothingSelected(t *testing.T) {
	m := openCopyManager(t, newCopySource(), newMemFS("/srv/b"))
	m = cursorTo(t, m, "..")
	m = pressRun(t, m, "f8")
	if m.fm.dialog != fmDialogNone || m.fm.message != m.tr(txtFMNothingSelected) {
		t.Errorf("Dialog %v / Meldung %q", m.fm.dialog, m.fm.message)
	}
}

// TestFileManagerF8ErrorKeepsMark - nicht löschbarer Eintrag bleibt
// markiert, Fehlermeldung.
func TestFileManagerF8ErrorKeepsMark(t *testing.T) {
	src := newCopySource()
	src.noRemove["/home/a/notiz.txt"] = true
	m := openCopyManager(t, src, newMemFS("/srv/b"))
	m = cursorTo(t, m, "projekt")
	m = pressRun(t, m, "insert")
	m = cursorTo(t, m, "notiz.txt")
	m = pressRun(t, m, "insert")
	m = pressRun(t, m, "f8")
	m = pressRun(t, m, "j")
	if got := strings.Join(markedNames(m.fm.panels[0]), ","); got != "notiz.txt" {
		t.Errorf("Markierungen = %q, erwartet notiz.txt", got)
	}
	prefix := strings.SplitN(m.tr(txtFMDeleteErrors), "%", 2)[0]
	if !strings.HasPrefix(m.fm.message, prefix) || !strings.Contains(m.fm.message, "/home/a/notiz.txt") {
		t.Errorf("Meldung = %q", m.fm.message)
	}
}

// TestFileManagerF8CursorStaysNearby - nach dem Löschen steht der Cursor
// an der bisherigen Stelle (bzw. am Ende), nicht oben.
func TestFileManagerF8CursorStaysNearby(t *testing.T) {
	src := newCopySource()
	src.addFile("/home/a/zz.txt", "z", 0o644, copyTestTime)
	m := openCopyManager(t, src, newMemFS("/srv/b"))
	m = cursorTo(t, m, "notiz.txt")
	before := m.fm.panels[0].cursor
	m = pressRun(t, m, "f8")
	m = pressRun(t, m, "j")
	panel := m.fm.panels[0]
	if panel.cursor != before {
		t.Errorf("Cursor = %d, erwartet %d", panel.cursor, before)
	}
	if entry, _ := panel.current(); entry.name != "zz.txt" {
		t.Errorf("Cursor auf %q, erwartet den Nachfolger zz.txt", entry.name)
	}
	// Letzten Eintrag löschen: Cursor auf den neuen letzten
	m = cursorTo(t, m, "zz.txt")
	m = pressRun(t, m, "f8")
	m = pressRun(t, m, "j")
	panel = m.fm.panels[0]
	if panel.cursor != len(panel.entries)-1 {
		t.Errorf("Cursor = %d bei %d Einträgen", panel.cursor, len(panel.entries))
	}
}

// TestFileManagerDeleteProgressDialog - Fortschrittsdialog: F10 ruht,
// Bildschirmgröße stimmt.
func TestFileManagerDeleteProgressDialog(t *testing.T) {
	src := newCopySource()
	m := openCopyManager(t, src, newMemFS("/srv/b"))
	m = cursorTo(t, m, "projekt")
	m = pressRun(t, m, "f8")
	updated, cmd := m.Update(keyMsgFor("j"))
	m = updated.(AppModel)
	if m.fm.dialog != fmDialogDeleting || cmd == nil {
		t.Fatalf("Dialog %v, Kommando %v - erwartet Fortschritt", m.fm.dialog, cmd != nil)
	}
	assertScreenSize(t, m)
	updated, _ = m.Update(fmKey("f10"))
	m = updated.(AppModel)
	if m.state != ViewFileManager || m.fm.dialog != fmDialogDeleting {
		t.Error("F10 beendet den Datei-Manager während des Löschens")
	}
	m = runCmd(t, m, cmd)
	if m.fm.dialog != fmDialogNone {
		t.Errorf("Dialog %v nach Ende", m.fm.dialog)
	}
}

// TestFileManagerDeleteEscCancels - Esc im Fortschritt bricht ab und
// meldet das.
func TestFileManagerDeleteEscCancels(t *testing.T) {
	src := newCopySource()
	m := openCopyManager(t, src, newMemFS("/srv/b"))
	m = cursorTo(t, m, "projekt")
	m = pressRun(t, m, "f8")
	// Das Löschen hält beim ersten Remove an, bis Esc gedrückt ist
	started, release := make(chan struct{}), make(chan struct{})
	first := true
	src.onRemove = func() {
		if first {
			first = false
			close(started)
			<-release
		}
	}
	updated, cmd := m.Update(keyMsgFor("j"))
	m = updated.(AppModel)
	<-started
	updated, _ = m.Update(fmKey("esc"))
	m = updated.(AppModel)
	close(release)
	m = runCmd(t, m, cmd)
	if m.fm.message != m.tr(txtFMDeleteCancelled) {
		t.Errorf("Meldung = %q", m.fm.message)
	}
	if src.node("/home/a/projekt/main.go") == nil {
		t.Error("trotz Abbruch gelöscht")
	}
}

// TestFileManagerOpsDialogsAllThemesAndSizes - neue Dialoge passen in
// jede Größe und jeden Farbmodus.
func TestFileManagerOpsDialogsAllThemesAndSizes(t *testing.T) {
	withColorProfile(t)
	m := openCopyManager(t, newCopySource(), newMemFS("/srv/b"))
	m = cursorTo(t, m, "projekt")
	check := func(label string) {
		t.Helper()
		for theme := range fmThemes {
			m.fm.theme = theme
			for _, size := range [][2]int{{100, 30}, {80, 24}, {40, 12}, {200, 60}} {
				m.termWidth, m.termHeight = size[0], size[1]
				lines := strings.Split(m.View(), "\n")
				if len(lines) != m.termHeight {
					t.Errorf("%s %v: %d Zeilen", label, size, len(lines))
				}
				assertScreenSize(t, m)
			}
		}
		m.termWidth, m.termHeight = 100, 30
	}
	m = pressRun(t, m, "f6")
	check("F6")
	m = pressRun(t, m, "esc")
	m = pressRun(t, m, "f7")
	m.fm.mkdirInput.SetValue(strings.Repeat("lang", 50))
	check("F7")
	m = pressRun(t, m, "esc")
	m = pressRun(t, m, "f8")
	check("F8")
	m.fm.dialog = fmDialogDeleting
	m.fm.del.progress = deleteProgress{current: "/home/a/" + strings.Repeat("tief/", 40), done: 12}
	check("Löschen")
}

// TestFileManagerOpsSanitizeNames - Steuerzeichen in Namen erscheinen in
// keinem der neuen Dialoge.
func TestFileManagerOpsSanitizeNames(t *testing.T) {
	src := newMemFS("/home/a")
	src.addFile("/home/a/böse\x1b[2J", "x", 0o644, copyTestTime)
	m := openCopyManager(t, src, newMemFS("/srv/b"))
	m = pressRun(t, m, "end")
	for _, key := range []string{"f6", "f8"} {
		m = pressRun(t, m, key)
		if strings.Contains(m.View(), "\x1b[2J") {
			t.Errorf("%s: Steuersequenz im Dialog", key)
		}
		m = pressRun(t, m, "esc")
	}
	// Löschen lehnt den unzulässigen Namen ab, die Meldung ist bereinigt
	m = pressRun(t, m, "f8")
	m = pressRun(t, m, "j")
	if strings.Contains(m.fm.message, "\x1b") {
		t.Error("Steuersequenz in der Meldung")
	}
	if src.node("/home/a/böse\x1b[2J") == nil {
		t.Error("Eintrag mit Steuerzeichen gelöscht")
	}
}

// swapFS ersetzt ein Verzeichnis durch einen Symlink, nachdem es gelesen
// wurde (swapAfterRead) bzw. beim ersten Remove (swapOnRemove) - so wie ein
// Angreifer mit Schreibrecht es während des Löschens austauschen könnte.
type swapFS struct {
	*memFS
	swapAfterRead string
	swapOnRemove  string
	swapped       bool
}

func (s *swapFS) swap(p string) {
	s.mu.Lock()
	s.nodes[p] = &memNode{mode: os.ModeSymlink | 0o777, target: "/home/fremd"}
	s.mu.Unlock()
	s.swapped = true
}

func (s *swapFS) ReadDir(p string) ([]os.FileInfo, error) {
	infos, err := s.memFS.ReadDir(p)
	if p == s.swapAfterRead && !s.swapped {
		s.swap(p)
	}
	return infos, err
}

func (s *swapFS) Remove(p string) error {
	if s.swapOnRemove != "" && !s.swapped {
		s.swap(s.swapOnRemove)
	}
	return s.memFS.Remove(p)
}

// TestDeleteStopsWhenDirectoryReplacedAfterRead - wird ein Verzeichnis nach
// dem Einlesen durch einen Symlink ersetzt, wird darin nichts mehr gelöscht
// (über SFTP würde der Pfad sonst ins Ziel des Links führen).
func TestDeleteStopsWhenDirectoryReplacedAfterRead(t *testing.T) {
	base := newCopySource()
	fsys := &swapFS{memFS: base, swapAfterRead: "/home/a/projekt"}
	result := runDeleteSync(t, context.Background(), fsys, "projekt")
	found := false
	for _, f := range result.failures {
		found = found || errors.Is(f.err, errDeleteRace)
	}
	if !found {
		t.Fatalf("keine errDeleteRace-Meldung: %s", failureErrors(result.failures))
	}
	for _, p := range []string{"/home/a/projekt/main.go", "/home/a/projekt/run.sh", "/home/a/projekt/lib/x.go"} {
		if base.node(p) == nil {
			t.Errorf("%s trotz ausgetauschtem Verzeichnis gelöscht", p)
		}
	}
}

// TestDeleteStopsWhenParentReplacedDuringDelete - wird ein übergeordnetes
// Verzeichnis mitten im Löschen ersetzt, bricht das Löschen darin ab.
func TestDeleteStopsWhenParentReplacedDuringDelete(t *testing.T) {
	base := newCopySource()
	fsys := &swapFS{memFS: base, swapOnRemove: "/home/a/projekt"}
	result := runDeleteSync(t, context.Background(), fsys, "projekt")
	found := false
	for _, f := range result.failures {
		found = found || errors.Is(f.err, errDeleteRace)
	}
	if !found {
		t.Fatalf("keine errDeleteRace-Meldung: %s", failureErrors(result.failures))
	}
	// Erster Eintrag (aktuell) war schon unterwegs, danach darf nichts mehr fallen
	for _, p := range []string{"/home/a/projekt/main.go", "/home/a/projekt/run.sh", "/home/a/projekt/lib/x.go"} {
		if base.node(p) == nil {
			t.Errorf("%s nach dem Austausch gelöscht", p)
		}
	}
}

// TestDeleteRaceMessageTranslated - Meldung in der Oberfläche übersetzt.
func TestDeleteRaceMessageTranslated(t *testing.T) {
	m := openCopyManager(t, newCopySource(), newMemFS("/srv/b"))
	got := m.fmDescribeFailure(copyFailure{path: "/home/a/x", err: errDeleteRace})
	if want := fmt.Sprintf(m.tr(txtFMErrDeleteRace), "/home/a/x"); got != want {
		t.Errorf("Meldung = %q, erwartet %q", got, want)
	}
}

// TestDeleteStopsWhenAncestorReplacedDuringDelete - wird ein weiter oben
// liegendes Verzeichnis (nicht das direkte Elternverzeichnis) mitten im
// Löschen ersetzt, darf darunter nichts mehr gelöscht werden.
func TestDeleteStopsWhenAncestorReplacedDuringDelete(t *testing.T) {
	base := newMemFS("/home/a")
	base.addFile("/home/a/t/1/2/f1", "1", 0o644, copyTestTime)
	base.addFile("/home/a/t/1/2/f2", "2", 0o644, copyTestTime)
	fsys := &swapFS{memFS: base, swapOnRemove: "/home/a/t"}
	result := runDeleteSync(t, context.Background(), fsys, "t")
	found := false
	for _, f := range result.failures {
		found = found || errors.Is(f.err, errDeleteRace)
	}
	if !found {
		t.Fatalf("keine errDeleteRace-Meldung: %s", failureErrors(result.failures))
	}
	if base.node("/home/a/t/1/2/f2") == nil {
		t.Error("f2 nach dem Austausch eines übergeordneten Verzeichnisses gelöscht")
	}
}

// TestDeleteWorksBelowSymlinkedPanelPath - liegt das Fensterverzeichnis
// selbst hinter einem Symlink (z. B. /home -> /data/home), ist das kein
// Austausch: gelöscht wird normal.
func TestDeleteWorksBelowSymlinkedPanelPath(t *testing.T) {
	fsys := newMemFS("/data/home/a")
	fsys.addFile("/data/home/a/projekt/x", "x", 0o644, copyTestTime)
	fsys.addLink("/home", "/data/home")
	events := make(chan deleteEvent, 16)
	go runDelete(context.Background(), deleteRequest{fs: &aliasFS{memFS: fsys, from: "/home", to: "/data/home"}, dir: "/home/a", names: []string{"projekt"}}, events)
	var result deleteResult
	for ev := range events {
		if ev.result != nil {
			result = *ev.result
			break
		}
	}
	if len(result.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(result.failures))
	}
	if fsys.node("/data/home/a/projekt") != nil {
		t.Error("nicht gelöscht")
	}
}

// aliasFS bildet einen Pfad-Präfix (Symlink im Fensterpfad) auf das echte
// Verzeichnis ab, wie es ein Server beim Auflösen tut.
type aliasFS struct {
	*memFS
	from, to string
}

func (a *aliasFS) real(p string) string {
	if p == a.from || strings.HasPrefix(p, a.from+"/") {
		return a.to + strings.TrimPrefix(p, a.from)
	}
	return p
}
func (a *aliasFS) Lstat(p string) (os.FileInfo, error)     { return a.memFS.Lstat(a.real(p)) }
func (a *aliasFS) ReadDir(p string) ([]os.FileInfo, error) { return a.memFS.ReadDir(a.real(p)) }
func (a *aliasFS) Remove(p string) error                   { return a.memFS.Remove(a.real(p)) }
func (a *aliasFS) RealPath(p string) (string, error)       { return a.memFS.RealPath(a.real(p)) }

// literalRealPathFS ist ein Server, dessen realpath Symlinks NICHT auflöst
// (nur den Pfad bereinigt) - die RealPath-Prüfung allein wäre dann wirkungslos.
type literalRealPathFS struct{ *swapFS }

func (l *literalRealPathFS) RealPath(p string) (string, error) { return path.Clean(p), nil }

// TestDeleteDetectsSwapWithoutResolvingRealPath - auch bei einem solchen
// Server fällt ein ausgetauschtes Elternverzeichnis auf (Lstat-Prüfung
// läuft immer, RealPath nur zusätzlich).
func TestDeleteDetectsSwapWithoutResolvingRealPath(t *testing.T) {
	base := newCopySource()
	fsys := &literalRealPathFS{&swapFS{memFS: base, swapOnRemove: "/home/a/projekt"}}
	result := runDeleteSync(t, context.Background(), fsys, "projekt")
	found := false
	for _, f := range result.failures {
		found = found || errors.Is(f.err, errDeleteRace)
	}
	if !found {
		t.Fatalf("keine errDeleteRace-Meldung: %s", failureErrors(result.failures))
	}
	for _, p := range []string{"/home/a/projekt/main.go", "/home/a/projekt/run.sh"} {
		if base.node(p) == nil {
			t.Errorf("%s nach dem Austausch gelöscht", p)
		}
	}
}

// TestDeleteInSymlinkedPanelDirWithLiteralRealPath - das Fensterverzeichnis
// selbst ist ein Symlink (z. B. verlinktes Home) und der Server löst
// realpath nicht auf: Löschen darin funktioniert trotzdem.
func TestDeleteInSymlinkedPanelDirWithLiteralRealPath(t *testing.T) {
	base := newMemFS("/data/a")
	base.addFile("/data/a/notiz.txt", "x", 0o644, copyTestTime)
	base.addDir("/data/a/leer", 0o755)
	base.addLink("/home/a", "/data/a")
	fsys := &literalRealPathFS{&swapFS{memFS: base}}
	alias := &aliasFS{memFS: base, from: "/home/a", to: "/data/a"}
	events := make(chan deleteEvent, 16)
	go runDelete(context.Background(), deleteRequest{fs: &literalAlias{alias, fsys}, dir: "/home/a", names: []string{"notiz.txt", "leer"}}, events)
	for ev := range events {
		if ev.result != nil {
			if len(ev.result.failures) != 0 {
				t.Fatalf("Fehler: %s", failureErrors(ev.result.failures))
			}
			break
		}
	}
	if base.node("/data/a/notiz.txt") != nil || base.node("/data/a/leer") != nil {
		t.Error("nicht gelöscht")
	}
}

// literalAlias: Pfade wie aliasFS, Lstat des Fensterverzeichnisses zeigt
// aber den Symlink selbst, realpath löst nicht auf.
type literalAlias struct {
	*aliasFS
	literal *literalRealPathFS
}

func (l *literalAlias) Lstat(p string) (os.FileInfo, error) {
	if p == l.from {
		return l.memFS.Lstat(p)
	}
	return l.aliasFS.Lstat(p)
}
func (l *literalAlias) RealPath(p string) (string, error) { return l.literal.RealPath(p) }

// noRealPathFS ist ein Server ohne realpath.
type noRealPathFS struct{ *swapFS }

func (n *noRealPathFS) RealPath(p string) (string, error) { return "", errors.New("unsupported") }

// TestDeleteDetectsPanelDirSwapWithoutRealPath - kann der Server kein
// realpath und wird das Fensterverzeichnis selbst ausgetauscht, wird nichts
// mehr gelöscht.
func TestDeleteDetectsPanelDirSwapWithoutRealPath(t *testing.T) {
	base := newCopySource()
	fsys := &noRealPathFS{&swapFS{memFS: base, swapOnRemove: "/home/a"}}
	result := runDeleteSync(t, context.Background(), fsys, "notiz.txt", "projekt")
	found := false
	for _, f := range result.failures {
		found = found || errors.Is(f.err, errDeleteRace)
	}
	if !found {
		t.Fatalf("keine errDeleteRace-Meldung: %s", failureErrors(result.failures))
	}
	if base.node("/home/a/projekt/main.go") == nil {
		t.Error("nach dem Austausch des Fensterverzeichnisses gelöscht")
	}
}
