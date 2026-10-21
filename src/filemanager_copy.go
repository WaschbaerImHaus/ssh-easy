// Paket main - Datei-Manager (MC-Modus): Kopieren zwischen zwei Servern
//
// runCopy kopiert Einträge eines Verzeichnisses der Quelle in ein
// Verzeichnis des Ziels - Verzeichnisse rekursiv mit Inhalt, Symlinks als
// Symlink (ihr Ziel wird nicht verfolgt). Die Daten laufen über diesen
// Rechner: lesen von der Quelle, schreiben aufs Ziel; die Server müssen
// sich nicht gegenseitig erreichen.
//
// Ablauf:
//  1. Erfassen: Alle Einträge werden vorab gelistet (Gesamtgröße für den
//     Fortschritt; außerdem endet so auch ein Kopieren in ein eigenes
//     Unterverzeichnis desselben Servers garantiert).
//  2. Kopieren: Vorhandene Verzeichnisse werden zusammengeführt. Existiert
//     eine Datei/ein Symlink schon, wird über den Ereigniskanal nachgefragt
//     (überschreiben, überspringen, alle, keine, alle wenn neuer, alle wenn
//     größer, abbrechen). Fehler einzelner Einträge brechen nicht ab.
//  3. Rechte und Änderungszeit werden übernommen (Fehler dabei still
//     ignorieren - manche Hoster verbieten chmod). Verzeichnisrechte erst
//     am Ende setzen, sonst könnte ein schreibgeschütztes Verzeichnis das
//     Kopieren seines Inhalts verhindern.
//
// Die Funktion kennt keine Oberfläche: Sie meldet Fortschritt, Rückfragen
// und das Ergebnis als copyEvent und wartet bei Rückfragen auf eine Antwort.
//
// @author Kurt Ingwer
// @date   2026-10-21 21:09
package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

// copyBufferSize - Schreibpuffer pro Datei. SFTP überträgt einen vollen
// Puffer mit mehreren gleichzeitigen Paketen (UseConcurrentWrites).
const copyBufferSize = 1 << 20

// copyProgressInterval - höchstens so oft wird Fortschritt gemeldet.
const copyProgressInterval = 100 * time.Millisecond

// errTargetTypeMismatch - Ziel existiert mit anderem Typ (Datei vs.
// Verzeichnis); es wird nicht ersetzt.
var errTargetTypeMismatch = errors.New("target exists with a different type")

// errSameFile - Quelle und Ziel sind dieselbe Datei (gleicher Server, gleicher
// Pfad); Kopieren würde die Quelle leeren.
var errSameFile = errors.New("source and target are the same file")

// errUnsafeName - Name aus einem Verzeichnis enthält "/" oder ist leer, "."
// oder "..". Ein bösartiger Server könnte so aus dem Zielverzeichnis des
// anderen Servers hinausschreiben (Path Traversal).
var errUnsafeName = errors.New("unsafe file name")

// copyRequest beschreibt einen Kopierauftrag.
type copyRequest struct {
	// Quelle und Ziel
	src, dst remoteFS
	// Verzeichnis der Einträge in der Quelle, Zielverzeichnis
	srcDir, dstDir string
	// Zu kopierende Einträge (Namen in srcDir)
	names []string
	// Quelle und Ziel liegen auf demselben Server (Host + Port gleich)
	sameHost bool
}

// copyConflictAnswer ist die Antwort auf "Ziel existiert schon".
type copyConflictAnswer int

const (
	// copyAnswerOverwrite - diese Datei überschreiben
	copyAnswerOverwrite copyConflictAnswer = iota
	// copyAnswerSkip - diese Datei überspringen
	copyAnswerSkip
	// copyAnswerOverwriteAll - diese und alle weiteren überschreiben
	copyAnswerOverwriteAll
	// copyAnswerSkipAll - diese und alle weiteren überspringen
	copyAnswerSkipAll
	// copyAnswerOverwriteNewer - diese und alle weiteren nur überschreiben,
	// wenn die Quelle neuer ist
	copyAnswerOverwriteNewer
	// copyAnswerOverwriteLarger - diese und alle weiteren nur überschreiben,
	// wenn die Quelle größer ist
	copyAnswerOverwriteLarger
	// copyAnswerAbort - gesamten Vorgang abbrechen
	copyAnswerAbort
)

// copyProgress ist der Stand eines Kopiervorgangs.
type copyProgress struct {
	// Noch beim Erfassen der Einträge?
	scanning bool
	// Aktuell bearbeiteter Pfad (Quelle)
	current string
	// Erledigte / gesamte Dateien (inkl. Symlinks) und Bytes
	doneFiles, totalFiles int
	doneBytes, totalBytes int64
}

// copyConflictInfo beschreibt Quelle und vorhandenes Ziel einer Rückfrage.
type copyConflictInfo struct {
	srcSize, dstSize int64
	srcTime, dstTime time.Time
}

// copyFailure ist ein Fehler bei einem einzelnen Eintrag.
type copyFailure struct {
	// Betroffener Pfad (Quelle bei Lesefehlern, Ziel bei Schreibfehlern)
	path string
	err  error
}

// copyResult ist das Ergebnis eines Kopiervorgangs.
type copyResult struct {
	// Vollständig kopierte Einträge (ohne Fehler und ohne Übersprungenes)
	copiedNames []string
	// Kopierte Dateien (inkl. Symlinks) und Bytes
	files int
	bytes int64
	// Fehler einzelner Einträge
	failures []copyFailure
	// Vom Nutzer abgebrochen
	cancelled bool
}

// copyEvent ist eine Meldung des Kopiervorgangs an die Oberfläche. Genau
// eines der Felder progress, conflict (+ conflictInfo) oder result ist gesetzt.
type copyEvent struct {
	progress     *copyProgress
	conflict     string
	conflictInfo copyConflictInfo
	result       *copyResult
}

// copyNode ist ein erfasster Eintrag.
type copyNode struct {
	// Pfad relativ zu srcDir, z. B. "projekt/lib/x.go"
	rel string
	// Name des obersten Eintrags (für copiedNames)
	top  string
	info os.FileInfo
}

// copyOverwritePolicy ist eine für alle weiteren Rückfragen geltende Regel.
type copyOverwritePolicy int

const (
	policyAsk copyOverwritePolicy = iota
	policyAll
	policyNone
	policyNewer
	policyLarger
)

// copyRun hält den Zustand eines laufenden Kopiervorgangs.
type copyRun struct {
	ctx      context.Context
	req      copyRequest
	events   chan<- copyEvent
	answers  <-chan copyConflictAnswer
	progress copyProgress
	lastSent time.Time
	policy   copyOverwritePolicy
	result   copyResult
	// Oberste Einträge mit Fehler oder Übersprungenem
	incomplete map[string]bool
	// Verzeichnisse (rel), deren Inhalt übersprungen wird
	skipDirs map[string]bool
	// Neu angelegte Verzeichnisse (Rechte/Zeit am Ende setzen)
	createdDirs []copyNode
}

// runCopy führt einen Kopierauftrag aus und meldet am Ende genau ein
// copyEvent mit result. Abbruch über ctx oder die Antwort copyAnswerAbort.
//
// @param ctx - Abbruch-Kontext
// @param req - Auftrag
// @param events - Kanal für Fortschritt, Rückfragen und Ergebnis
// @param answers - Kanal für Antworten auf Rückfragen
// @date   2026-10-21 21:03
func runCopy(ctx context.Context, req copyRequest, events chan<- copyEvent, answers <-chan copyConflictAnswer) {
	run := &copyRun{
		ctx:        ctx,
		req:        req,
		events:     events,
		answers:    answers,
		incomplete: map[string]bool{},
		skipDirs:   map[string]bool{},
	}
	run.progress.scanning = true
	nodes := run.scan()
	run.progress.scanning = false
	run.sendProgress(true)
	run.copyAll(nodes)
	run.finishDirs()

	for _, name := range req.names {
		if !run.incomplete[name] && !run.result.cancelled {
			run.result.copiedNames = append(run.result.copiedNames, name)
		}
	}
	result := run.result
	events <- copyEvent{result: &result}
}

// safeEntryName prüft einen einzelnen Verzeichniseintrag: nicht leer, nicht
// "." oder "..", kein "/" (nur so bleibt path.Join im Verzeichnis).
//
// @param name - Name vom Server
// @return bool - true wenn unbedenklich
// @date   2026-10-21 21:09
func safeEntryName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.Contains(name, "/")
}

// fail merkt einen Fehler und markiert den obersten Eintrag als unvollständig.
//
// @param node - Betroffener Eintrag
// @param p - Pfad für die Meldung
// @param err - Fehler
// @date   2026-10-21 21:03
func (r *copyRun) fail(node copyNode, p string, err error) {
	r.result.failures = append(r.result.failures, copyFailure{path: p, err: err})
	r.incomplete[node.top] = true
}

// scan erfasst alle Einträge (Verzeichnisse vor ihrem Inhalt).
//
// @return []copyNode - Einträge in Kopierreihenfolge
// @date   2026-10-21 21:03
func (r *copyRun) scan() []copyNode {
	var nodes []copyNode
	var walk func(rel, top string)
	walk = func(rel, top string) {
		if r.ctx.Err() != nil {
			return
		}
		srcPath := path.Join(r.req.srcDir, rel)
		info, err := r.req.src.Lstat(srcPath)
		if err != nil {
			r.fail(copyNode{top: top}, srcPath, err)
			return
		}
		node := copyNode{rel: rel, top: top, info: info}
		nodes = append(nodes, node)
		if info.IsDir() {
			children, err := r.req.src.ReadDir(srcPath)
			if err != nil {
				// Verzeichnis selbst wird angelegt, sein Inhalt fehlt
				r.fail(node, srcPath, err)
				return
			}
			sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
			for _, child := range children {
				if child.Name() == "." || child.Name() == ".." {
					continue
				}
				if !safeEntryName(child.Name()) {
					// Nicht path.Join: das würde ".." auflösen
					r.fail(node, srcPath+"/"+child.Name(), errUnsafeName)
					continue
				}
				walk(path.Join(rel, child.Name()), top)
			}
			return
		}
		r.progress.totalFiles++
		if info.Mode().IsRegular() {
			r.progress.totalBytes += info.Size()
		}
		r.sendProgress(false)
	}
	for _, name := range r.req.names {
		if !safeEntryName(name) {
			r.fail(copyNode{top: name}, name, errUnsafeName)
			continue
		}
		walk(name, name)
	}
	return nodes
}

// copyAll kopiert die erfassten Einträge der Reihe nach.
//
// @param nodes - Erfasste Einträge
// @date   2026-10-21 21:03
func (r *copyRun) copyAll(nodes []copyNode) {
	for _, node := range nodes {
		if r.ctx.Err() != nil {
			r.result.cancelled = true
			return
		}
		if r.underSkippedDir(node.rel) {
			r.countSkipped(node)
			continue
		}
		srcPath := path.Join(r.req.srcDir, node.rel)
		dstPath := path.Join(r.req.dstDir, node.rel)
		r.progress.current = srcPath
		r.sendProgress(false)

		if r.req.sameHost && path.Clean(srcPath) == path.Clean(dstPath) {
			r.fail(node, dstPath, errSameFile)
			r.skipDir(node)
			r.countSkipped(node)
			continue
		}
		existing, err := r.req.dst.Lstat(dstPath)
		exists := err == nil

		switch {
		case node.info.IsDir():
			r.copyDir(node, dstPath, existing, exists)
		case node.info.Mode()&os.ModeSymlink != 0:
			if !r.copySymlink(node, srcPath, dstPath, existing, exists) {
				return
			}
		default:
			if !r.copyRegular(node, srcPath, dstPath, existing, exists) {
				return
			}
		}
	}
}

// copyDir legt ein Verzeichnis an bzw. führt es mit einem vorhandenen zusammen.
//
// @date   2026-10-21 21:03
func (r *copyRun) copyDir(node copyNode, dstPath string, existing os.FileInfo, exists bool) {
	if exists {
		if !existing.IsDir() {
			r.fail(node, dstPath, errTargetTypeMismatch)
			r.skipDir(node)
		}
		return
	}
	if err := r.req.dst.Mkdir(dstPath); err != nil {
		r.fail(node, dstPath, err)
		r.skipDir(node)
		return
	}
	r.createdDirs = append(r.createdDirs, node)
}

// copySymlink legt den Symlink am Ziel neu an (gleiches Linkziel).
//
// @return bool - false = Vorgang abgebrochen
// @date   2026-10-21 21:03
func (r *copyRun) copySymlink(node copyNode, srcPath, dstPath string, existing os.FileInfo, exists bool) bool {
	defer r.countDone(node, 0)
	target, err := r.req.src.ReadLink(srcPath)
	if err != nil {
		r.fail(node, srcPath, err)
		return true
	}
	if exists {
		overwrite, ok := r.resolveConflict(node, dstPath, existing)
		if !ok {
			return false
		}
		if !overwrite {
			return true
		}
		if err := r.req.dst.Remove(dstPath); err != nil {
			r.fail(node, dstPath, err)
			return true
		}
	}
	if err := r.req.dst.Symlink(target, dstPath); err != nil {
		r.fail(node, dstPath, err)
		return true
	}
	r.result.files++
	return true
}

// copyRegular kopiert eine Datei samt Rechten und Änderungszeit.
//
// @return bool - false = Vorgang abgebrochen
// @date   2026-10-21 21:03
func (r *copyRun) copyRegular(node copyNode, srcPath, dstPath string, existing os.FileInfo, exists bool) bool {
	if !node.info.Mode().IsRegular() {
		// Gerätedateien, Pipes, Sockets: nicht kopierbar
		r.fail(node, srcPath, errTargetTypeMismatch)
		r.countDone(node, 0)
		return true
	}
	if exists {
		overwrite, ok := r.resolveConflict(node, dstPath, existing)
		if !ok {
			return false
		}
		if !overwrite {
			r.countDone(node, node.info.Size())
			return true
		}
		// Am Ziel steht ein Symlink: entfernen statt hindurch zu schreiben
		if existing.Mode()&os.ModeSymlink != 0 {
			if err := r.req.dst.Remove(dstPath); err != nil {
				r.fail(node, dstPath, err)
				r.countDone(node, node.info.Size())
				return true
			}
		}
	}
	before := r.progress.doneBytes
	err := r.copyData(srcPath, dstPath)
	if err != nil {
		if r.ctx.Err() != nil {
			r.result.cancelled = true
			return false
		}
		r.fail(node, dstPath, err)
		r.progress.doneBytes = before
		r.countDone(node, node.info.Size())
		return true
	}
	// Rechte und Zeit übernehmen; Fehler ignorieren (eingeschränkte Hoster)
	_ = r.req.dst.Chmod(dstPath, node.info.Mode().Perm())
	_ = r.req.dst.Chtimes(dstPath, node.info.ModTime(), node.info.ModTime())
	r.result.files++
	r.result.bytes += node.info.Size()
	r.progress.doneFiles++
	r.sendProgress(true)
	return true
}

// copyData überträgt den Inhalt einer Datei. Bei Fehler oder Abbruch wird
// die halb geschriebene Zieldatei gelöscht.
//
// @param srcPath - Quelle
// @param dstPath - Ziel
// @return error - Fehler beim Lesen/Schreiben oder ctx.Err()
// @date   2026-10-21 21:03
func (r *copyRun) copyData(srcPath, dstPath string) error {
	reader, err := r.req.src.OpenRead(srcPath)
	if err != nil {
		return err
	}
	defer reader.Close()
	writer, err := r.req.dst.CreateFile(dstPath)
	if err != nil {
		return err
	}
	buffered := bufio.NewWriterSize(writer, copyBufferSize)
	counter := &copyCountingWriter{run: r, w: buffered}
	_, err = io.Copy(counter, reader)
	if err == nil {
		err = buffered.Flush()
	}
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = r.ctx.Err()
	}
	if err != nil {
		_ = r.req.dst.Remove(dstPath)
	}
	return err
}

// copyCountingWriter zählt geschriebene Bytes für den Fortschritt und
// bricht ab, sobald der Kontext beendet ist.
type copyCountingWriter struct {
	run *copyRun
	w   io.Writer
}

// Write leitet weiter, zählt und prüft auf Abbruch.
//
// @date   2026-10-21 21:03
func (c *copyCountingWriter) Write(b []byte) (int, error) {
	if err := c.run.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.w.Write(b)
	c.run.progress.doneBytes += int64(n)
	c.run.sendProgress(false)
	return n, err
}

// resolveConflict entscheidet, ob ein vorhandenes Ziel überschrieben wird -
// nach geltender Regel oder per Rückfrage.
//
// @return bool - überschreiben?
// @return bool - false = Vorgang abgebrochen
// @date   2026-10-21 21:03
func (r *copyRun) resolveConflict(node copyNode, dstPath string, existing os.FileInfo) (bool, bool) {
	if existing.IsDir() {
		r.fail(node, dstPath, errTargetTypeMismatch)
		return false, true
	}
	if r.policy == policyAsk {
		answer, ok := r.ask(dstPath, node.info, existing)
		if !ok {
			r.result.cancelled = true
			return false, false
		}
		switch answer {
		case copyAnswerOverwrite:
			return true, true
		case copyAnswerSkip:
			r.incomplete[node.top] = true
			return false, true
		case copyAnswerOverwriteAll:
			r.policy = policyAll
		case copyAnswerSkipAll:
			r.policy = policyNone
		case copyAnswerOverwriteNewer:
			r.policy = policyNewer
		case copyAnswerOverwriteLarger:
			r.policy = policyLarger
		}
	}
	overwrite := false
	switch r.policy {
	case policyAll:
		overwrite = true
	case policyNewer:
		// Sekundengenau vergleichen (SFTP überträgt nur ganze Sekunden)
		overwrite = node.info.ModTime().Truncate(time.Second).After(existing.ModTime().Truncate(time.Second))
	case policyLarger:
		overwrite = node.info.Size() > existing.Size()
	}
	if !overwrite {
		r.incomplete[node.top] = true
	}
	return overwrite, true
}

// ask stellt eine Rückfrage und wartet auf die Antwort.
//
// @return copyConflictAnswer - Antwort
// @return bool - false bei Abbruch (Antwort oder Kontext)
// @date   2026-10-21 21:03
func (r *copyRun) ask(dstPath string, src, dst os.FileInfo) (copyConflictAnswer, bool) {
	event := copyEvent{conflict: dstPath, conflictInfo: copyConflictInfo{
		srcSize: src.Size(), dstSize: dst.Size(), srcTime: src.ModTime(), dstTime: dst.ModTime(),
	}}
	select {
	case r.events <- event:
	case <-r.ctx.Done():
		return copyAnswerAbort, false
	}
	select {
	case answer := <-r.answers:
		return answer, answer != copyAnswerAbort
	case <-r.ctx.Done():
		return copyAnswerAbort, false
	}
}

// finishDirs setzt Rechte und Zeit neu angelegter Verzeichnisse - von innen
// nach außen, damit das Setzen der Zeit nicht durch Änderungen am Inhalt
// wieder überschrieben wird.
//
// @date   2026-10-21 21:03
func (r *copyRun) finishDirs() {
	for i := len(r.createdDirs) - 1; i >= 0; i-- {
		node := r.createdDirs[i]
		dstPath := path.Join(r.req.dstDir, node.rel)
		_ = r.req.dst.Chmod(dstPath, node.info.Mode().Perm())
		_ = r.req.dst.Chtimes(dstPath, node.info.ModTime(), node.info.ModTime())
	}
}

// skipDir merkt ein Verzeichnis, dessen Inhalt übersprungen wird.
//
// @date   2026-10-21 21:03
func (r *copyRun) skipDir(node copyNode) {
	if node.info != nil && node.info.IsDir() {
		r.skipDirs[node.rel] = true
	}
}

// underSkippedDir prüft, ob ein Eintrag in einem übersprungenen Verzeichnis liegt.
//
// @date   2026-10-21 21:03
func (r *copyRun) underSkippedDir(rel string) bool {
	for dir := path.Dir(rel); dir != "." && dir != "/"; dir = path.Dir(dir) {
		if r.skipDirs[dir] {
			return true
		}
	}
	return false
}

// countSkipped zählt einen übersprungenen Eintrag im Fortschritt als erledigt.
//
// @date   2026-10-21 21:03
func (r *copyRun) countSkipped(node copyNode) {
	if node.info != nil && !node.info.IsDir() {
		r.countDone(node, node.info.Size())
	}
}

// countDone zählt eine (nicht oder fehlerhaft kopierte) Datei als erledigt,
// damit der Fortschrittsbalken trotzdem 100 % erreicht.
//
// @date   2026-10-21 21:03
func (r *copyRun) countDone(node copyNode, size int64) {
	r.progress.doneFiles++
	if node.info.Mode().IsRegular() {
		r.progress.doneBytes += size
	}
	r.sendProgress(true)
}

// sendProgress meldet den Fortschritt höchstens alle copyProgressInterval
// (force: sofort). Ist der Kanal voll, wird die Meldung verworfen - die
// nächste bringt ohnehin den neueren Stand.
//
// @param force - ohne Zeitsperre melden
// @date   2026-10-21 21:03
func (r *copyRun) sendProgress(force bool) {
	if !force && time.Since(r.lastSent) < copyProgressInterval {
		return
	}
	r.lastSent = time.Now()
	snapshot := r.progress
	select {
	case r.events <- copyEvent{progress: &snapshot}:
	default:
	}
}
