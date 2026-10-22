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
//  3. Jede Datei wird als exklusive Temp-Datei mit Rechten 0600
//     geschrieben und erst am Ende auf den Zielnamen umbenannt (siehe
//     copyData). Neue Verzeichnisse sind währenddessen 0700; ihre echten
//     Rechte setzt finishDirs am Ende. Rechte/Zeit-Fehler werden ignoriert
//     (manche Hoster verbieten chmod) - außer bei privaten Dateien.
//  4. Schutz vor bösartigen Servern: Namen mit "/", "\" oder
//     Steuerzeichen werden abgelehnt, Zielpfade müssen im Zielverzeichnis
//     liegen, Größen werden überlaufsicher addiert, Anzahl und Tiefe der
//     Einträge sind begrenzt.
//
// Verschieben (move) ist dasselbe Kopieren; danach wird jede vollständig
// angekommene Datei in der Quelle gelöscht, am Ende auch die leer
// gewordenen Verzeichnisse (siehe filemanager_move.go).
//
// Die Funktion kennt keine Oberfläche: Sie meldet Fortschritt, Rückfragen
// und das Ergebnis als copyEvent und wartet bei Rückfragen auf eine Antwort.
//
// @author Kurt Ingwer
// @date   2026-10-22 05:17
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"
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

// errUnsafeName - Name aus einem Verzeichnis enthält "/" oder "\" oder
// Steuerzeichen oder ist leer, "." bzw. "..". Ein bösartiger Server könnte
// so aus dem Zielverzeichnis des anderen Servers hinausschreiben (Path
// Traversal; "\" ist auf Windows-SFTP-Servern ein Pfadtrenner).
var errUnsafeName = errors.New("unsafe file name")

// errCannotProtect - der Zielserver lässt die Rechte einer privaten Datei
// (ohne Rechte für Gruppe/Andere) nicht einschränken; sie wäre dort für
// andere lesbar und wird deshalb nicht kopiert.
var errCannotProtect = errors.New("cannot restrict permissions on target")

// errCopyLimit - zu viele Einträge oder zu tief verschachtelt (Schutz vor
// bösartigen Servern mit endlosen Verzeichnisbäumen); nichts wird kopiert.
var errCopyLimit = errors.New("too many entries or nesting too deep")

// Grenzen beim Erfassen (Variablen, damit Tests sie verkleinern können)
var (
	copyMaxEntries = 1_000_000
	copyMaxDepth   = 128
)

// copyTempPrefix - Namensanfang der Temp-Dateien im Zielverzeichnis. Jede
// Datei wird erst unter "<Präfix><Zufall>.tmp" geschrieben und dann in
// einem Schritt auf den Zielnamen umbenannt.
const copyTempPrefix = ".ssh-easy-copy-"

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
	// Verschieben: Quelle nach erfolgreichem Kopieren löschen
	move bool
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
	// Grenze beim Erfassen erreicht (copyMaxEntries/copyMaxDepth)
	limitHit bool
	// Verschieben: Quellverzeichnisse, die am Ende gelöscht werden, wenn
	// sie leer sind (angelegt oder zusammengeführt, in Kopierreihenfolge)
	movedDirs []copyNode
	// Verschieben: Ergebnis der Prüfung "Quell- und Zielverzeichnis sind
	// dasselbe?" pro Verzeichnispaar
	sameDirs map[string]bool
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
		sameDirs:   map[string]bool{},
	}
	run.progress.scanning = true
	nodes := run.scan()
	run.progress.scanning = false
	if run.limitHit {
		// Grenze erreicht: gar nichts kopieren
		nodes = nil
		run.incomplete = map[string]bool{}
		for _, name := range req.names {
			run.incomplete[name] = true
		}
	}
	run.sendProgress(true)
	run.copyAll(nodes)
	run.finishDirs()
	if req.move && !run.result.cancelled {
		run.removeMovedDirs()
	}

	for _, name := range req.names {
		if !run.incomplete[name] && !run.result.cancelled {
			run.result.copiedNames = append(run.result.copiedNames, name)
		}
	}
	result := run.result
	events <- copyEvent{result: &result}
}

// safeEntryName prüft einen einzelnen Verzeichniseintrag für die Anzeige:
// nicht leer, nicht "." oder "..", kein "/" (nur so bleibt path.Join im
// Verzeichnis).
//
// @param name - Name vom Server
// @return bool - true wenn unbedenklich
// @date   2026-10-21 21:17
func safeEntryName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.Contains(name, "/")
}

// safeCopyName ist die strengere Prüfung beim Kopieren: zusätzlich kein
// Backslash (Pfadtrenner auf Windows-SFTP-Servern) und keine Steuerzeichen.
// Ein Doppelpunkt bleibt erlaubt: Unter Linux ist er ein normales Zeichen,
// mitten im Pfad führt er auch unter Windows nicht aus dem Verzeichnis.
//
// @param name - Name vom Server
// @return bool - true wenn unbedenklich
// @date   2026-10-21 21:17
func safeCopyName(name string) bool {
	if !safeEntryName(name) || strings.Contains(name, "\\") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// insideDir prüft, ob p (nach Bereinigung) echt unterhalb von dir liegt -
// zweite Sicherung nach der Namensprüfung.
//
// @param dir - Verzeichnis
// @param p - Pfad
// @return bool - true wenn p in dir liegt (und nicht dir selbst ist)
// @date   2026-10-21 21:17
func insideDir(dir, p string) bool {
	d, c := path.Clean(dir), path.Clean(p)
	if d == "/" {
		return c != "/" && strings.HasPrefix(c, "/")
	}
	return strings.HasPrefix(c, d+"/")
}

// satAdd addiert ohne Überlauf und nie unter 0 (Größen meldet der Server,
// ein bösartiger kann absurde Werte schicken).
//
// @param a - Summe bisher
// @param b - Summand
// @return int64 - a+b, begrenzt auf [0, MaxInt64]
// @date   2026-10-21 21:17
func satAdd(a, b int64) int64 {
	if b > 0 && a > math.MaxInt64-b {
		return math.MaxInt64
	}
	if a+b < 0 {
		return 0
	}
	return a + b
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
		if r.ctx.Err() != nil || r.limitHit {
			return
		}
		// Schutz vor endlosen Bäumen eines bösartigen Servers
		if len(nodes) >= copyMaxEntries || strings.Count(rel, "/") >= copyMaxDepth {
			r.limitHit = true
			r.fail(copyNode{top: top}, path.Join(r.req.srcDir, rel), errCopyLimit)
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
				if !safeCopyName(child.Name()) {
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
			r.progress.totalBytes = satAdd(r.progress.totalBytes, max(info.Size(), 0))
		}
		r.sendProgress(false)
	}
	for _, name := range r.req.names {
		if !safeCopyName(name) {
			r.fail(copyNode{top: name}, name, errUnsafeName)
			continue
		}
		if r.movesIntoItself(name) {
			r.fail(copyNode{top: name}, path.Join(r.req.srcDir, name), errMoveIntoItself)
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

		// Zweite Sicherung: Ziel muss im Zielverzeichnis liegen
		if !insideDir(r.req.dstDir, dstPath) {
			r.fail(node, dstPath, errUnsafeName)
			r.skipDir(node)
			r.countSkipped(node)
			continue
		}

		if r.req.sameHost && path.Clean(srcPath) == path.Clean(dstPath) {
			r.fail(node, dstPath, errSameFile)
			r.skipDir(node)
			r.countSkipped(node)
			continue
		}
		existing, err := r.req.dst.Lstat(dstPath)
		exists := err == nil
		// Verschieben auf ein vorhandenes Ziel: ist es die Quelle selbst
		// (zweiter Verbindungseintrag zum selben Server)? Dann würde das
		// Löschen der Quelle die einzige Kopie vernichten
		if r.req.move && exists {
			if same, err := r.sameEntry(srcPath, dstPath); err != nil || same {
				if err == nil {
					err = errSameFile
				}
				r.fail(node, dstPath, err)
				r.skipDir(node)
				r.countSkipped(node)
				continue
			}
		}

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
			return
		}
		r.rememberMovedDir(node)
		return
	}
	if err := r.req.dst.Mkdir(dstPath); err != nil {
		r.fail(node, dstPath, err)
		r.skipDir(node)
		return
	}
	r.rememberMovedDir(node)
	// Während des Kopierens nur für den Besitzer zugänglich; die echten
	// Rechte setzt finishDirs am Ende
	_ = r.req.dst.Chmod(dstPath, 0o700)
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
	}
	// Erst unter Temp-Namen anlegen, dann in einem Schritt umbenennen
	// (ersetzt ein vorhandenes Ziel, ohne ihm zu folgen)
	tmp := tempCopyName(dstPath)
	if err := r.req.dst.Symlink(target, tmp); err != nil {
		r.fail(node, dstPath, err)
		return true
	}
	if err := r.req.dst.RenameOverwrite(tmp, dstPath); err != nil {
		_ = r.req.dst.Remove(tmp)
		r.fail(node, dstPath, err)
		return true
	}
	r.result.files++
	r.removeMovedSource(node, srcPath)
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
	}
	before := r.progress.doneBytes
	err := r.copyData(srcPath, dstPath, node.info)
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
	r.result.files++
	r.result.bytes = satAdd(r.result.bytes, max(node.info.Size(), 0))
	r.removeMovedSource(node, srcPath)
	r.progress.doneFiles++
	r.sendProgress(true)
	return true
}

// copyData überträgt den Inhalt einer Datei sicher:
//  1. Temp-Datei im Zielverzeichnis exklusiv anlegen (O_EXCL: kein
//     untergeschobener Symlink, keine fremde Datei wird benutzt)
//  2. sofort auf 0600 setzen - der Inhalt ist nie für andere lesbar; geht
//     das nicht und ist die Quelle privat, wird abgebrochen (errCannotProtect)
//  3. Inhalt schreiben, dann echte Rechte und Änderungszeit setzen
//  4. in einem Schritt auf den Zielnamen umbenennen (ersetzt ein vorhandenes
//     Ziel bzw. einen Symlink dort, ohne ihm zu folgen)
//
// Die Quelle wird dabei nie geleert, auch wenn Quelle und Ziel unerkannt
// dieselbe Datei sind (zwei Verbindungseinträge auf denselben Server). Bei
// Fehler oder Abbruch wird die Temp-Datei gelöscht; ein vorhandenes Ziel
// bleibt dann unverändert.
//
// @param srcPath - Quelle
// @param dstPath - Ziel
// @param info - Infos der Quelle (Rechte, Zeit)
// @return error - Fehler beim Lesen/Schreiben oder ctx.Err()
// @date   2026-10-21 21:17
func (r *copyRun) copyData(srcPath, dstPath string, info os.FileInfo) (err error) {
	reader, err := r.req.src.OpenRead(srcPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	tmp := tempCopyName(dstPath)
	writer, err := r.req.dst.CreateExclusive(tmp)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = r.req.dst.Remove(tmp)
		}
	}()
	perm := info.Mode().Perm()
	if chmodErr := r.req.dst.Chmod(tmp, 0o600); chmodErr != nil && perm&0o077 == 0 {
		writer.Close()
		return errCannotProtect
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
		return err
	}
	// Rechte und Zeit übernehmen; Fehler ignorieren (eingeschränkte Hoster)
	_ = r.req.dst.Chmod(tmp, perm)
	_ = r.req.dst.Chtimes(tmp, info.ModTime(), info.ModTime())
	return r.req.dst.RenameOverwrite(tmp, dstPath)
}

// tempCopyName liefert einen zufälligen Temp-Namen im Verzeichnis des Ziels.
//
// @param dstPath - Zielpfad
// @return string - z. B. "/srv/b/.ssh-easy-copy-3f9a….tmp"
// @date   2026-10-21 21:17
func tempCopyName(dstPath string) string {
	random := make([]byte, 8)
	_, _ = rand.Read(random)
	return path.Join(path.Dir(dstPath), copyTempPrefix+hex.EncodeToString(random)+".tmp")
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
	c.run.progress.doneBytes = satAdd(c.run.progress.doneBytes, int64(n))
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
		r.progress.doneBytes = satAdd(r.progress.doneBytes, max(size, 0))
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

// writeFileAtomically schreibt Daten sicher in eine entfernte Datei (für
// den Editor): exklusive Temp-Datei im selben Verzeichnis, sofort 0600
// (geht das bei einer privaten Datei nicht -> errCannotProtect), Inhalt
// schreiben, Rechte perm setzen, in einem Schritt umbenennen. Bei einem
// Fehler wird die Temp-Datei gelöscht; die Originaldatei bleibt dann
// unverändert.
//
// @param fsys - entferntes Dateisystem
// @param p - Zielpfad (bei Symlinks bereits das aufgelöste Ziel)
// @param data - neuer Inhalt
// @param perm - Zugriffsrechte der Datei
// @return error - Fehler beim Schreiben/Umbenennen
// @date   2026-10-22 00:50
func writeFileAtomically(fsys remoteFS, p string, data []byte, perm os.FileMode) (err error) {
	tmp := tempCopyName(p)
	writer, err := fsys.CreateExclusive(tmp)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = fsys.Remove(tmp)
		}
	}()
	if chmodErr := fsys.Chmod(tmp, 0o600); chmodErr != nil && perm&0o077 == 0 {
		writer.Close()
		return errCannotProtect
	}
	if _, err = writer.Write(data); err != nil {
		writer.Close()
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	_ = fsys.Chmod(tmp, perm)
	return fsys.RenameOverwrite(tmp, p)
}
