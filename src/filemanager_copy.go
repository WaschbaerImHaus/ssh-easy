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
	"sync"
	"time"
	"unicode"
)

// copyBufferSize - Schreibpuffer pro Datei für Ziele ohne WriteAt (dort
// schreibt pipeCopy der Reihe nach). Ziele mit WriteAt (SFTP, lokale
// Dateien) schreibt pipeCopyAt blockweise und gleichzeitig.
const copyBufferSize = 1 << 20

// copyProgressInterval - höchstens so oft wird Fortschritt gemeldet.
const copyProgressInterval = 100 * time.Millisecond

// errTargetTypeMismatch - Ziel existiert mit anderem Typ (Datei vs.
// Verzeichnis); es wird nicht ersetzt.
var errTargetTypeMismatch = errors.New("target exists with a different type")

// errSameFile - Quelle und Ziel sind dieselbe Datei (gleicher Server, gleicher
// Pfad); Kopieren würde die Quelle leeren.
var errSameFile = errors.New("source and target are the same file")

// errMoveCheckFailed - beim Verschieben ließ sich nicht sicher prüfen, ob
// Quelle und Ziel dasselbe Verzeichnis sind (Prüfdatei verschwunden).
var errMoveCheckFailed = errors.New("cannot tell whether source and target are the same directory")

// errUnsafeName - Name aus einem Verzeichnis enthält "/" oder "\" oder
// Steuerzeichen oder ist leer, "." bzw. "..". Ein bösartiger Server könnte
// so aus dem Zielverzeichnis des anderen Servers hinausschreiben (Path
// Traversal; "\" ist auf Windows-SFTP-Servern ein Pfadtrenner).
var errUnsafeName = errors.New("unsafe file name")

// errCannotProtect - der Zielserver lässt die Rechte einer privaten Datei
// (ohne Rechte für Gruppe/Andere) nicht einschränken; sie wäre dort für
// andere lesbar und wird deshalb nicht kopiert.
var errCannotProtect = errors.New("cannot restrict permissions on target")

// errUnsafeSymlink - Symlinks vom Server werden auf dem PC nicht angelegt
// (sein Ziel könnte auf vorhandene lokale Dateien zeigen).
var errUnsafeSymlink = errors.New("symlinks from a server are not created on the PC")

// errConnectionLost - die Verbindung zu Quelle oder Ziel ist abgerissen oder
// antwortete nicht mehr (watchConnection); der eigentliche Fehler steckt
// in connectionLostError.
var errConnectionLost = errors.New("connection lost")

// connectionLostError kennzeichnet einen Fehler nach Verbindungsverlust;
// errors.Is findet errConnectionLost und den ursprünglichen Fehler.
type connectionLostError struct {
	err error
}

// Error liefert "connection lost: <Ursache>", ohne den Text zu doppeln,
// wenn die Ursache das schon sagt (pkg/sftp meldet selbst "connection lost").
//
// @return string - Fehlertext
// @date   2026-10-24 20:29
func (e *connectionLostError) Error() string {
	msg := e.err.Error()
	if strings.Contains(msg, errConnectionLost.Error()) {
		return msg
	}
	return errConnectionLost.Error() + ": " + msg
}

// Unwrap liefert errConnectionLost und den ursprünglichen Fehler.
//
// @return []error - beide Fehler
// @date   2026-10-24 20:29
func (e *connectionLostError) Unwrap() []error {
	return []error{errConnectionLost, e.err}
}

// connectionWatcher meldet, ob die Verbindung eines Dateisystems verloren
// ging (sftpFS).
type connectionWatcher interface {
	connectionLost() bool
}

// fsConnectionLost meldet, ob die Verbindung eines Dateisystems verloren
// ging; Dateisysteme ohne Verbindung (lokaler PC) nie.
//
// @param fsys - Dateisystem
// @return bool - true wenn verloren
// @date   2026-10-24 20:29
func fsConnectionLost(fsys remoteFS) bool {
	w, ok := fsys.(connectionWatcher)
	return ok && w.connectionLost()
}

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

// copyProbePrefix - Namensanfang der Prüfdatei beim Verschieben (sameEntry).
// Bewusst anders als copyTempPrefix: Das Aufräumen alter Temp-Dateien
// (auch einer zweiten Instanz) darf sie nie erfassen (Audit Build 82).
const copyProbePrefix = ".ssh-easy-probe-"

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
	// Kopierte Dateien als heruntergeladen kennzeichnen (Server -> PC)
	markDownloads bool
	// Keine Symlinks anlegen, sondern melden (Server -> PC)
	safeLinksOnly bool
	// Protokoll (z. B. entfernte Temp-Dateien; nil = nichts protokollieren)
	logf func(format string, args ...interface{})
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
	// Neu angelegte Verzeichnisse (Rechte/Zeit am Ende setzen; nur die,
	// deren dirJob gelungen ist)
	createdDirs []copyNode
	// Aufträge "Verzeichnis anlegen" dieses Laufs (rel -> Auftrag); die
	// Map nur in dispatch, finishDirs und removeMovedDirs (nach dirWG.Wait)
	dirJobs map[string]*dirJob
	// Laufende Aufträge "Verzeichnis anlegen"
	dirWG sync.WaitGroup
	// Höchstens so viele Verzeichnisse werden gleichzeitig angelegt
	dirSlots chan struct{}
	// Höchstens so viele Aufträge "Verzeichnis anlegen" sind offen
	dirPending chan struct{}
	// Grenze beim Erfassen erreicht (copyMaxEntries/copyMaxDepth)
	limitHit bool
	// Vorgemerkte liegen gebliebene Temp-Dateien (nur dispatch, gelöscht
	// am Ende: removeStaleTemps)
	staleTemps []staleTemp
	// Verschieben: Quellverzeichnisse, die am Ende gelöscht werden, wenn
	// sie leer sind (angelegt oder zusammengeführt, in Kopierreihenfolge)
	movedDirs []copyNode
	// Verschieben: Ergebnis der Prüfung "Quell- und Zielverzeichnis sind
	// dasselbe?" pro Verzeichnispaar
	sameDirs map[string]bool
	// Vorgehaltene Listen der Zielverzeichnisse (targetInfo; nil = einzeln
	// fragen); nur der Ablauf greift darauf zu, nie die Arbeiter
	dstDirs map[string]*targetListing
	// Zielpfade dieses Laufs (klein geschrieben, looseTargetKey); nur dispatch
	assigned map[string]bool
	// Laufende Übertragungen der Arbeiter (targetInfo wartet bei Kollisionen)
	inflight sync.WaitGroup
	// Schützt progress, lastSent, result und incomplete: Arbeiter
	// übertragen gleichzeitig (filemanager_transfer.go)
	mu sync.Mutex
	// Hält alle Arbeiter an (Abbruch per Rückfrage oder Fehler des Kontexts)
	cancel context.CancelFunc
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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	run := &copyRun{
		ctx:        ctx,
		cancel:     cancel,
		dstDirs:    map[string]*targetListing{},
		assigned:   map[string]bool{},
		req:        req,
		events:     events,
		answers:    answers,
		incomplete: map[string]bool{},
		skipDirs:   map[string]bool{},
		sameDirs:   map[string]bool{},
		dirJobs:    map[string]*dirJob{},
		dirSlots:   make(chan struct{}, max(copyParallel, 1)),
		dirPending: make(chan struct{}, copyMaxPendingDirs),
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
	run.removeStaleTemps()
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
// Ist die Verbindung zu Quelle oder Ziel verloren, wird der Fehler als
// errConnectionLost gekennzeichnet (sonst sähe der Nutzer nur z. B. "EOF").
//
// @param node - Betroffener Eintrag
// @param p - Pfad für die Meldung
// @param err - Fehler
// @date   2026-10-24 20:29
func (r *copyRun) fail(node copyNode, p string, err error) {
	if !errors.Is(err, errConnectionLost) && (fsConnectionLost(r.req.src) || fsConnectionLost(r.req.dst)) {
		err = &connectionLostError{err: err}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.result.failures = append(r.result.failures, copyFailure{path: p, err: err})
	r.incomplete[node.top] = true
}

// markIncomplete merkt einen obersten Eintrag als unvollständig.
//
// @param top - Name des obersten Eintrags
// @date   2026-10-23 19:44
func (r *copyRun) markIncomplete(top string) {
	r.mu.Lock()
	r.incomplete[top] = true
	r.mu.Unlock()
}

// scan erfasst alle Einträge (Verzeichnisse vor ihrem Inhalt).
//
// @return []copyNode - Einträge in Kopierreihenfolge
// @date   2026-10-21 21:03
func (r *copyRun) scan() []copyNode {
	var nodes []copyNode
	// info: Angaben aus ReadDir des Elternverzeichnisses (nil = per Lstat
	// fragen) - spart eine Server-Rundreise pro Eintrag
	var walk func(rel, top string, info os.FileInfo)
	walk = func(rel, top string, info os.FileInfo) {
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
		if info == nil {
			var err error
			if info, err = r.req.src.Lstat(srcPath); err != nil {
				r.fail(copyNode{top: top}, srcPath, err)
				return
			}
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
				if !safeCopyName(child.Name()) || !fsAcceptsName(r.req.dst, child.Name()) {
					// Nicht path.Join: das würde ".." auflösen
					r.fail(node, srcPath+"/"+child.Name(), errUnsafeName)
					continue
				}
				// Ohne Rechte-Angaben in der Liste (Mode 0) einzeln fragen -
				// sonst gälte ein Verzeichnis als Datei
				var known os.FileInfo
				if child.Mode() != 0 {
					known = child
				}
				walk(path.Join(rel, child.Name()), top, known)
			}
			return
		}
		r.mu.Lock()
		r.progress.totalFiles++
		if info.Mode().IsRegular() {
			r.progress.totalBytes = satAdd(r.progress.totalBytes, max(info.Size(), 0))
		}
		r.mu.Unlock()
		r.sendProgress(false)
	}
	known := r.sourceInfos()
	for _, name := range r.req.names {
		if !safeCopyName(name) || !fsAcceptsName(r.req.dst, name) {
			r.fail(copyNode{top: name}, name, errUnsafeName)
			continue
		}
		if r.movesIntoItself(name) {
			r.fail(copyNode{top: name}, path.Join(r.req.srcDir, name), errMoveIntoItself)
			continue
		}
		walk(name, name, known[name])
	}
	return nodes
}

// copyAll kopiert die erfassten Einträge: dispatch geht sie der Reihe nach
// durch, copyParallel Arbeiter übertragen die Dateien gleichzeitig. Kehrt
// erst zurück, wenn alle Arbeiter fertig sind.
//
// @param nodes - Erfasste Einträge
// @date   2026-10-23 19:44
func (r *copyRun) copyAll(nodes []copyNode) {
	jobs, wait := r.startWorkers()
	if !r.dispatch(nodes, jobs) {
		// Abbruch: laufende Übertragungen anhalten (Temp-Dateien werden
		// dabei gelöscht)
		r.setCancelled()
	}
	close(jobs)
	wait()
	// Auch leere neue Verzeichnisse müssen fertig sein (finishDirs)
	r.dirWG.Wait()
}

// dispatch geht die erfassten Einträge der Reihe nach durch: Verzeichnisse,
// Symlinks und Rückfragen erledigt er selbst, Dateien übergibt er den
// Arbeitern.
//
// @param nodes - Erfasste Einträge
// @param jobs - Aufträge an die Arbeiter
// @return bool - false = Vorgang abgebrochen
// @date   2026-10-24 20:29
func (r *copyRun) dispatch(nodes []copyNode, jobs chan<- copyJob) bool {
	for _, node := range nodes {
		if r.ctx.Err() != nil {
			return false
		}
		if r.underSkippedDir(node.rel) {
			r.countSkipped(node)
			continue
		}
		srcPath := path.Join(r.req.srcDir, node.rel)
		dstPath := path.Join(r.req.dstDir, node.rel)
		r.mu.Lock()
		r.progress.current = srcPath
		r.mu.Unlock()
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
		existing, exists, skip := r.targetInfo(node.rel, dstPath)
		if skip {
			// Elternverzeichnis nicht angelegt: Inhalt übersprungen
			r.skipDir(node)
			r.countSkipped(node)
			continue
		}
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
				return false
			}
		default:
			if !r.copyRegular(node, srcPath, dstPath, existing, exists, jobs) {
				return false
			}
		}
	}
	return true
}

// copyDir legt ein Verzeichnis an bzw. führt es mit einem vorhandenen zusammen.
//
// @date   2026-10-24 20:29
func (r *copyRun) copyDir(node copyNode, dstPath string, existing os.FileInfo, exists bool) {
	if exists {
		if !existing.IsDir() {
			r.fail(node, dstPath, errTargetTypeMismatch)
			r.skipDir(node)
			return
		}
		// Zusammengeführt: Einträge darin hängen weiter am Auftrag des neu
		// angelegten Elternverzeichnisses, falls es einen gibt
		// (Sicherheitsaudit Build 81 - sonst gälte ein Verzeichnis hinter
		// einem gescheiterten Mkdir als "gab es schon")
		if parent := r.parentDirJob(node); parent != nil {
			r.dirJobs[node.rel] = parent
		}
		r.rememberMovedDir(node)
		return
	}
	// Anlegen läuft im Hintergrund (startDirJob); Einträge darin warten
	// auf diesen Auftrag. Scheitert er, werden sie übersprungen
	job := r.startDirJob(node, dstPath)
	r.dirJobs[node.rel] = job
	r.rememberMovedDir(node)
	// Neu angelegt = leer: Einträge darin muss niemand mehr abfragen
	r.dstDirs[dstPath] = newTargetListing(nil)
	r.createdDirs = append(r.createdDirs, node)
}

// copySymlink legt den Symlink am Ziel neu an (gleiches Linkziel); vom
// Server auf den PC (safeLinksOnly) wird er nur gemeldet.
//
// @return bool - false = Vorgang abgebrochen
// @date   2026-10-22 08:20
func (r *copyRun) copySymlink(node copyNode, srcPath, dstPath string, existing os.FileInfo, exists bool) bool {
	defer r.countDone(node, 0)
	if r.req.safeLinksOnly {
		// Server -> PC: Symlinks gar nicht anlegen. Ein Linkziel lässt sich
		// als Text nicht sicher prüfen - es kann auf schon vorhandene
		// Einträge im Zielordner zeigen (".ssh" in ~) oder über dort
		// liegende Symlinks hinausführen
		r.fail(node, srcPath, errUnsafeSymlink)
		return true
	}
	if !r.parentDirJob(node).wait() {
		// Elternverzeichnis nicht angelegt: übersprungen
		return true
	}
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
	r.mu.Lock()
	r.result.files++
	r.mu.Unlock()
	r.removeMovedSource(node, srcPath)
	return true
}

// copyRegular prüft eine Datei (Typ, vorhandenes Ziel, Rückfrage) und
// übergibt sie einem Arbeiter, der sie samt Rechten und Änderungszeit
// überträgt.
//
// @return bool - false = Vorgang abgebrochen
// @date   2026-10-23 19:44
func (r *copyRun) copyRegular(node copyNode, srcPath, dstPath string, existing os.FileInfo, exists bool, jobs chan<- copyJob) bool {
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
	// Übertragen (und beim Verschieben danach die Quelle löschen) macht
	// ein Arbeiter - siehe transfer
	r.inflight.Add(1)
	select {
	case jobs <- copyJob{node: node, srcPath: srcPath, dstPath: dstPath, parent: r.parentDirJob(node)}:
		return true
	case <-r.ctx.Done():
		r.inflight.Done()
		return false
	}
}

// copyData überträgt den Inhalt einer Datei sicher:
//  1. Temp-Datei im Zielverzeichnis exklusiv anlegen (O_EXCL: kein
//     untergeschobener Symlink, keine fremde Datei wird benutzt)
//  2. sofort auf 0600 setzen - der Inhalt ist nie für andere lesbar; geht
//     das nicht und ist die Quelle privat, wird abgebrochen (errCannotProtect)
//  3. Inhalt schreiben (mit WriteAt mehrere Blöcke gleichzeitig), dann
//     echte Rechte und Änderungszeit gleichzeitig setzen
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
// @date   2026-10-24 20:10
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

	counter := &copyCountingWriter{run: r}
	defer func() {
		if err != nil {
			// Fortschritt dieser Datei zurücknehmen; der Aufrufer zählt sie
			// danach als erledigt (Balken erreicht trotzdem 100 %)
			r.mu.Lock()
			r.progress.doneBytes -= counter.n
			r.mu.Unlock()
		}
	}()
	// Lesen und Schreiben gleichzeitig (Lesevorlauf im gemeinsamen Budget)
	if at, ok := writer.(io.WriterAt); ok {
		// Mehrere Blöcke gleichzeitig unterwegs: keine Pause nach jedem Block
		_, err = pipeCopyAt(r.ctx, at, reader, copyReadAhead, counter.add)
	} else {
		buffered := bufio.NewWriterSize(writer, copyBufferSize)
		counter.w = buffered
		_, err = pipeCopy(r.ctx, counter, reader, copyReadAhead)
		if err == nil {
			err = buffered.Flush()
		}
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
	// Herkunft "Internet" kennzeichnen (Windows warnt vor dem Ausführen),
	// solange die Temp-Datei noch 0600 ist - ein Schreibschutz aus den
	// Originalrechten würde das verhindern. Scheitert es, wird die Datei
	// nicht abgelegt (sonst läge z. B. eine unmarkierte .exe auf dem PC)
	if marker, ok := r.req.dst.(downloadMarker); ok && r.req.markDownloads {
		if err := marker.MarkDownloaded(tmp); err != nil {
			return err
		}
	}
	// Rechte und Zeit übernehmen (gleichzeitig, die Datei ist geschlossen);
	// Fehler ignorieren (eingeschränkte Hoster)
	setAttributes(r.req.dst, tmp, perm, info.ModTime())
	return r.req.dst.RenameOverwrite(tmp, dstPath)
}

// fsAcceptsName meldet, ob ein Dateisystem einen Namen unverändert anlegen
// kann (lokales Windows: keine Gerätenamen, kein ":" usw.).
//
// @param fsys - Dateisystem
// @param name - Name
// @return bool - true wenn anlegbar
// @date   2026-10-22 06:40
func fsAcceptsName(fsys remoteFS, name string) bool {
	v, ok := fsys.(nameValidator)
	return !ok || v.ValidName(name)
}

// tempProbeName liefert einen zufälligen Namen für die Prüfdatei beim
// Verschieben (sameEntry) im Verzeichnis des Ziels.
//
// @param dstPath - Zielpfad
// @return string - z. B. "/srv/b/.ssh-easy-probe-3f9a….tmp"
// @date   2026-10-26 21:15
func tempProbeName(dstPath string) string {
	random := make([]byte, 8)
	_, _ = rand.Read(random)
	return path.Join(path.Dir(dstPath), copyProbePrefix+hex.EncodeToString(random)+".tmp")
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
	// Ziel für Write (nur ohne WriteAt; pipeCopyAt meldet über add)
	w io.Writer
	// In diese Datei geschriebene Bytes (unter run.mu)
	n int64
}

// Write leitet weiter, zählt und prüft auf Abbruch.
//
// @date   2026-10-24 20:10
func (c *copyCountingWriter) Write(b []byte) (int, error) {
	if err := c.run.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.w.Write(b)
	c.add(n)
	return n, err
}

// add verbucht geschriebene Bytes im Fortschritt; darf gleichzeitig
// aufgerufen werden (pipeCopyAt).
//
// @param n - geschriebene Bytes
// @date   2026-10-24 20:10
func (c *copyCountingWriter) add(n int) {
	c.run.mu.Lock()
	defer c.run.mu.Unlock()
	c.n += int64(n)
	c.run.progress.doneBytes = satAdd(c.run.progress.doneBytes, int64(n))
	c.run.sendProgressLocked(false)
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
			r.setCancelled()
			return false, false
		}
		switch answer {
		case copyAnswerOverwrite:
			return true, true
		case copyAnswerSkip:
			r.markIncomplete(node.top)
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
		r.markIncomplete(node.top)
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
// wieder überschrieben wird und Rechte ohne x-Bit am Elternverzeichnis den
// Weg zu den Unterverzeichnissen nicht versperren. Alle Verzeichnisse einer
// Ebene laufen gleichzeitig (sonst bei vielen Verzeichnissen und hoher
// Latenz minutenlanges Warten).
//
// @date   2026-10-24 20:10
func (r *copyRun) finishDirs() {
	levels := map[int][]copyNode{}
	deepest := 0
	for _, node := range r.createdDirs {
		if !r.dirJobs[node.rel].succeeded() {
			continue
		}
		depth := strings.Count(node.rel, "/")
		levels[depth] = append(levels[depth], node)
		deepest = max(deepest, depth)
	}
	for depth := deepest; depth >= 0; depth-- {
		forEachParallel(levels[depth], func(node copyNode) {
			dstPath := path.Join(r.req.dstDir, node.rel)
			setAttributes(r.req.dst, dstPath, node.info.Mode().Perm(), node.info.ModTime())
		})
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
	r.mu.Lock()
	defer r.mu.Unlock()
	r.progress.doneFiles++
	if node.info.Mode().IsRegular() {
		r.progress.doneBytes = satAdd(r.progress.doneBytes, max(size, 0))
	}
	r.sendProgressLocked(true)
}

// sendProgress meldet den Fortschritt höchstens alle copyProgressInterval
// (force: sofort). Ist der Kanal voll, wird die Meldung verworfen - die
// nächste bringt ohnehin den neueren Stand.
//
// @param force - ohne Zeitsperre melden
// @date   2026-10-21 21:03
func (r *copyRun) sendProgress(force bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sendProgressLocked(force)
}

// sendProgressLocked wie sendProgress, r.mu ist bereits gesperrt. Sendet
// nie blockierend (voller Kanal = Meldung verwerfen), hält die Sperre also
// nicht lange.
//
// @param force - ohne Zeitsperre melden
// @date   2026-10-23 19:44
func (r *copyRun) sendProgressLocked(force bool) {
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
