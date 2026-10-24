// Paket main - schnelle Übertragung im Datei-Manager (F5/F6)
//
// Hintergrund (Nutzerbericht Build 77): 700 kleine Dateien zu kopieren
// dauerte lange, eine große Datei ging flott. Jede Datei braucht rund ein
// Dutzend Server-Rundreisen (anlegen, Rechte, schreiben, schließen, Zeit,
// umbenennen, öffnen, lesen ...), die jeweils auf Antwort warten. Bei
// vielen kleinen Dateien summiert sich diese Wartezeit, nicht die Datenmenge.
//
// Abhilfe in dieser Datei:
//   - copyParallel Dateien werden gleichzeitig übertragen (Arbeiter). Der
//     Ablauf selbst (Erfassen, Verzeichnisse, Symlinks, Rückfragen) bleibt
//     der Reihe nach; jede einzelne Datei läuft weiter über exklusive
//     Temp-Datei + Umbenennen (copyData).
//   - pipeCopy liest eine Datei, während sie geschrieben wird; der Vorlauf
//     ist über copyBudget für alle Übertragungen zusammen begrenzt.
//   - targetInfo fragt am Ziel einmal pro Verzeichnis die Liste ab statt
//     jede Datei einzeln; neu angelegte Verzeichnisse sind leer.
//
// Upload über das Internet (Nutzerbericht Build 80: 4-5 statt 40 Mbit):
//   - pipeCopyAt schreibt mehrere Blöcke einer Datei gleichzeitig (WriteAt
//     an der jeweiligen Stelle). Mit nur einem Write pro Block wartete jeder
//     Block auf alle Antworten, bevor der nächste startete - eine Pause von
//     einer Rundreise pro 2 MiB.
//   - copyParallel 32 statt 8: Eine kleine Datei braucht rund 5 Rundreisen;
//     bei 40 ms Ping sind das 200 ms, in denen kaum Daten fließen.
//
// @author Kurt Ingwer
// @date   2026-10-24 20:10

package main

import (
	"context"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	// copyChunkSize - Größe eines Lese-/Schreibblocks. 2 MiB: SFTP liest
	// einen Block mit vielen gleichzeitigen Anfragen - kleinere Blöcke
	// würden große Dateien bremsen (eine Rundreise pro Block)
	copyChunkSize = 2 << 20
	// copyReadAheadBytes - so viel darf insgesamt (alle Arbeiter zusammen)
	// gelesen, aber noch nicht geschrieben sein. Mehr bringt kein Tempo -
	// schneller als die langsamere Seite geht es nie -, kostet aber RAM.
	copyReadAheadBytes = 64 << 20
	// copyPipeChunks - höchstens so viele Blöcke belegt eine einzelne
	// Datei, damit eine große Datei kleine daneben nicht ausbremst
	copyPipeChunks = 8
	// copyWritesInFlight - so viele Blöcke einer Datei schreibt pipeCopyAt
	// gleichzeitig. Ab 2 überlappt das Warten auf die letzten Antworten
	// eines Blocks mit dem Senden des nächsten; 4 lässt Luft bei hoher
	// Latenz. Die Datenmenge unterwegs begrenzt zusätzlich das SSH-Fenster.
	copyWritesInFlight = 4
	// copyMaxPendingDirs - so viele Aufträge "Verzeichnis anlegen" dürfen
	// gleichzeitig offen sein (wartend oder laufend). Der Ablauf läuft bei
	// neuen Verzeichnissen ohne Rundreise voraus; ohne Grenze entstünde pro
	// Verzeichnis eine wartende Goroutine (bösartiger Server: 1 Million)
	copyMaxPendingDirs = 128
	// copyMaxTargetListing - größere Zielverzeichnisse werden nicht als
	// Liste vorgehalten (dann Lstat pro Datei)
	copyMaxTargetListing = 100_000
)

// copyParallel - so viele Dateien werden gleichzeitig übertragen
// (Variable, damit Tests sie ändern können). Messung 2026-10-24 bei 40 ms
// Ping: mit 8 Dateien kamen 4-KB-Dateien auf 0,9 Mbit/s - die Zeit geht
// fast nur aufs Warten auf Antworten, nicht auf Daten.
var copyParallel = 32

// copyReadAhead - gemeinsames Budget für den Lesevorlauf aller Übertragungen.
var copyReadAhead = newCopyBudget(copyReadAheadBytes, copyChunkSize)

// copyBudget begrenzt den Speicher für gelesene, noch nicht geschriebene
// Blöcke. Jeder Block braucht eine Marke; ist keine frei, wartet der Leser,
// bis ein Schreiber einen Block abgearbeitet hat.
type copyBudget struct {
	chunk  int
	tokens chan struct{}
	pool   sync.Pool
}

// newCopyBudget legt ein Budget an.
//
// @param total - höchstens so viele Bytes im Vorlauf
// @param chunk - Blockgröße in Bytes
// @return *copyBudget - Budget mit total/chunk Marken (mindestens 1)
// @date   2026-10-23 09:40
func newCopyBudget(total, chunk int) *copyBudget {
	n := max(total/chunk, 1)
	b := &copyBudget{chunk: chunk, tokens: make(chan struct{}, n)}
	for i := 0; i < n; i++ {
		b.tokens <- struct{}{}
	}
	b.pool.New = func() any {
		buf := make([]byte, chunk)
		return &buf
	}
	return b
}

// acquire wartet auf eine freie Marke.
//
// @param ctx - Abbruch-Kontext
// @param done - wird geschlossen, wenn der Schreiber aufgegeben hat
// @return bool - false bei Abbruch
// @date   2026-10-23 09:40
func (b *copyBudget) acquire(ctx context.Context, done <-chan struct{}) bool {
	select {
	case <-b.tokens:
		return true
	case <-ctx.Done():
		return false
	case <-done:
		return false
	}
}

// release gibt eine Marke zurück.
//
// @date   2026-10-23 09:40
func (b *copyBudget) release() {
	b.tokens <- struct{}{}
}

// free liefert die Zahl freier Marken (für Tests).
//
// @return int - freie Marken
// @date   2026-10-23 09:40
func (b *copyBudget) free() int {
	return len(b.tokens)
}

// copyChunk ist ein gelesener Block.
type copyChunk struct {
	buf *[]byte
	n   int
	err error
}

// pipeCopy kopiert src nach dst und liest dabei vor: Ein Leser holt Blöcke,
// solange das Budget Marken hat (pro Datei höchstens copyPipeChunks),
// während der Aufrufer sie schreibt. So
// laufen Lesen (Quelle) und Schreiben (Ziel) gleichzeitig. Bei einem Fehler
// oder Abbruch wird der Leser beendet und jede Marke zurückgegeben.
//
// @param ctx - Abbruch-Kontext
// @param dst - Ziel
// @param src - Quelle
// @param budget - Begrenzung des Vorlaufs
// @return int64 - geschriebene Bytes
// @return error - erster Lese-/Schreibfehler oder ctx.Err()
// @date   2026-10-24 20:10
func pipeCopy(ctx context.Context, dst io.Writer, src io.Reader, budget *copyBudget) (int64, error) {
	// Pro Datei höchstens copyPipeChunks Blöcke: in der Warteschlange,
	// beim Leser und beim Schreiber je einer
	done := make(chan struct{})
	chunks := readChunks(ctx, src, budget, done, max(copyPipeChunks-2, 1))

	var written int64
	var err error
	sawEnd := false
	stopped := false
	for c := range chunks {
		if err == nil && c.n > 0 {
			var n int
			n, err = dst.Write((*c.buf)[:c.n])
			written += int64(n)
			if err == nil && n < c.n {
				err = io.ErrShortWrite
			}
		}
		if c.err != nil {
			sawEnd = true
			if err == nil && c.err != io.EOF {
				err = c.err
			}
		}
		budget.pool.Put(c.buf)
		budget.release()
		if err == nil {
			err = ctx.Err()
		}
		if err != nil && !stopped {
			// Leser anhalten; die restlichen Blöcke werden nur noch freigegeben
			stopped = true
			close(done)
		}
	}
	if err == nil && !sawEnd {
		// Leser hat ohne Dateiende aufgehört: nur bei Abbruch möglich
		err = ctx.Err()
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
	}
	return written, err
}

// readChunks startet den Leser für pipeCopy/pipeCopyAt: Er holt Blöcke,
// solange das Budget Marken hat, und schließt den Kanal am Dateiende, bei
// einem Lesefehler (beide als letzter Block mit err) oder bei Abbruch.
// Hat der Schreiber aufgegeben (done geschlossen), gibt der Leser seinen
// Block selbst frei. Jeder empfangene Block muss vom Empfänger
// freigegeben werden (pool.Put + release).
//
// @param ctx - Abbruch-Kontext
// @param src - Quelle
// @param budget - Begrenzung des Vorlaufs
// @param done - wird geschlossen, wenn der Schreiber aufgegeben hat
// @param queue - Plätze in der Warteschlange
// @return <-chan copyChunk - gelesene Blöcke
// @date   2026-10-24 20:10
func readChunks(ctx context.Context, src io.Reader, budget *copyBudget, done <-chan struct{}, queue int) <-chan copyChunk {
	chunks := make(chan copyChunk, queue)
	go func() {
		defer close(chunks)
		for {
			if !budget.acquire(ctx, done) {
				return
			}
			bufp := budget.pool.Get().(*[]byte)
			n, err := io.ReadFull(src, *bufp)
			if err == io.ErrUnexpectedEOF {
				err = io.EOF
			}
			select {
			case chunks <- copyChunk{buf: bufp, n: n, err: err}:
			case <-done:
				// Schreiber hat aufgegeben: Block selbst freigeben
				budget.pool.Put(bufp)
				budget.release()
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return chunks
}

// pipeCopyAt kopiert src nach dst wie pipeCopy, schreibt aber bis zu
// copyWritesInFlight Blöcke gleichzeitig, jeden an seine Stelle (WriteAt).
// So entsteht keine Pause zwischen den Blöcken: Während ein Block auf
// seine letzten Antworten wartet, ist der nächste schon unterwegs.
// pipeCopyAt kehrt erst zurück, wenn kein Schreibvorgang mehr läuft.
//
// @param ctx - Abbruch-Kontext
// @param dst - Ziel (z. B. *sftp.File, *os.File)
// @param src - Quelle
// @param budget - Begrenzung des Vorlaufs
// @param written - wird nach jedem Block mit dessen geschriebenen Bytes
//
//	aufgerufen, auch gleichzeitig (nil = keine Meldung)
//
// @return int64 - geschriebene Bytes
// @return error - erster Lese-/Schreibfehler oder ctx.Err()
// @date   2026-10-24 20:10
func pipeCopyAt(ctx context.Context, dst io.WriterAt, src io.Reader, budget *copyBudget, written func(int)) (int64, error) {
	// Pro Datei höchstens copyPipeChunks Blöcke: Warteschlange, Leser,
	// ein wartender Block und die laufenden Schreibvorgänge
	done := make(chan struct{})
	chunks := readChunks(ctx, src, budget, done, max(copyPipeChunks-2-copyWritesInFlight, 1))
	slots := make(chan struct{}, copyWritesInFlight)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		total    int64
		firstErr error
	)
	setErr := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}
	getErr := func() error {
		mu.Lock()
		defer mu.Unlock()
		return firstErr
	}
	release := func(c copyChunk) {
		budget.pool.Put(c.buf)
		budget.release()
	}

	var offset int64
	sawEnd := false
	stopped := false
	for c := range chunks {
		if c.err != nil {
			sawEnd = true
			if c.err != io.EOF {
				setErr(c.err)
			}
		}
		if err := ctx.Err(); err != nil {
			setErr(err)
		}
		if c.n > 0 && getErr() == nil {
			slots <- struct{}{}
			// Während des Wartens auf einen Platz kann ein Schreibvorgang
			// gescheitert sein: dann nichts mehr senden
			if getErr() != nil {
				<-slots
				release(c)
			} else {
				wg.Add(1)
				go func(c copyChunk, off int64) {
					defer wg.Done()
					defer func() { <-slots }()
					defer release(c)
					n, err := dst.WriteAt((*c.buf)[:c.n], off)
					if err == nil && n < c.n {
						err = io.ErrShortWrite
					}
					if n > 0 {
						mu.Lock()
						total += int64(n)
						mu.Unlock()
						if written != nil {
							written(n)
						}
					}
					if err != nil {
						setErr(err)
					}
				}(c, offset)
				offset += int64(c.n)
			}
		} else {
			release(c)
		}
		if getErr() != nil && !stopped {
			// Leser anhalten; die restlichen Blöcke werden nur noch freigegeben
			stopped = true
			close(done)
		}
	}
	wg.Wait()

	err := getErr()
	if err == nil && !sawEnd {
		// Leser hat ohne Dateiende aufgehört: nur bei Abbruch möglich
		err = ctx.Err()
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
	}
	return total, err
}

// forEachParallel ruft fn für jeden Eintrag auf, mit bis zu copyParallel
// Aufrufen gleichzeitig, und kehrt zurück, wenn alle fertig sind.
//
// @param nodes - Einträge
// @param fn - Arbeit pro Eintrag (muss gleichzeitig laufen dürfen)
// @date   2026-10-24 20:10
func forEachParallel(nodes []copyNode, fn func(copyNode)) {
	work := make(chan copyNode)
	var wg sync.WaitGroup
	for i := 0; i < min(max(copyParallel, 1), len(nodes)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for node := range work {
				fn(node)
			}
		}()
	}
	for _, node := range nodes {
		work <- node
	}
	close(work)
	wg.Wait()
}

// setAttributes setzt Rechte und Änderungszeit gleichzeitig (eine
// Rundreise statt zwei). Fehler werden ignoriert (eingeschränkte Hoster).
// Die Datei muss geschlossen sein, sonst könnte ein späteres Schreiben die
// Zeit wieder ändern.
//
// @param fsys - Dateisystem
// @param p - Pfad
// @param perm - Rechte
// @param mtime - Zugriffs- und Änderungszeit
// @date   2026-10-24 20:10
func setAttributes(fsys remoteFS, p string, perm os.FileMode, mtime time.Time) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = fsys.Chmod(p, perm)
	}()
	_ = fsys.Chtimes(p, mtime, mtime)
	wg.Wait()
}

// copyJob ist eine Datei, die ein Arbeiter überträgt.
type copyJob struct {
	node    copyNode
	srcPath string
	dstPath string
	// Auftrag für das neu angelegte Elternverzeichnis (nil = es gab es schon)
	parent *dirJob
}

// dirJob ist das Anlegen eines neuen Verzeichnisses (Mkdir + chmod 0700)
// im Hintergrund. Einträge darin warten, bis es fertig ist - so wird nie
// etwas in einem Verzeichnis angelegt, das noch für andere offen ist.
type dirJob struct {
	// Wird geschlossen, wenn der Auftrag fertig ist
	done chan struct{}
	// Gelungen? (geschrieben vor close(done))
	ok bool
}

// wait wartet, bis das Verzeichnis angelegt ist.
//
// @return bool - true wenn angelegt (nil-Auftrag: Verzeichnis gab es schon)
// @date   2026-10-24 20:29
func (j *dirJob) wait() bool {
	if j == nil {
		return true
	}
	<-j.done
	return j.ok
}

// succeeded meldet nach dem Ende aller Aufträge, ob das Verzeichnis
// angelegt wurde (nil-Auftrag: vorhandenes Verzeichnis, zählt als gelungen).
//
// @return bool - true wenn vorhanden bzw. angelegt
// @date   2026-10-24 20:29
func (j *dirJob) succeeded() bool {
	return j.wait()
}

// startDirJob legt ein neues Verzeichnis im Hintergrund an: erst wenn das
// Elternverzeichnis fertig ist, dann Mkdir und sofort chmod 0700 (die
// echten Rechte setzt finishDirs am Ende). Geschwister laufen gleichzeitig
// (höchstens copyParallel), statt dass der Ablauf bei jedem Verzeichnis
// zwei Rundreisen wartet. Scheitert das Elternverzeichnis, wird dieses
// übersprungen (kein eigener Fehler); scheitert Mkdir, wird der Fehler
// gemeldet. Nur aus dispatch aufrufen.
//
// @param node - Verzeichnis
// @param dstPath - Zielpfad
// @return *dirJob - Auftrag
// @date   2026-10-24 20:29
func (r *copyRun) startDirJob(node copyNode, dstPath string) *dirJob {
	job := &dirJob{done: make(chan struct{})}
	parent := r.parentDirJob(node)
	// Höchstens copyMaxPendingDirs offene Aufträge; ein Auftrag wartet nur
	// auf frühere, daher wird immer einer fertig
	select {
	case r.dirPending <- struct{}{}:
	case <-r.ctx.Done():
		close(job.done)
		return job
	}
	// inflight: targetInfo wartet bei Namenskollisionen auch auf neue
	// Verzeichnisse (README/readme werden zusammengeführt statt zu scheitern)
	r.inflight.Add(1)
	r.dirWG.Add(1)
	go func() {
		defer r.dirWG.Done()
		defer r.inflight.Done()
		defer func() { <-r.dirPending }()
		defer close(job.done)
		// Erst auf das Elternverzeichnis warten, dann einen Platz belegen -
		// sonst könnten wartende Kinder alle Plätze blockieren
		if !parent.wait() {
			// Elternverzeichnis gescheitert (Fehler schon gemeldet): still
			// übersprungen
			return
		}
		if r.ctx.Err() != nil {
			// Abbruch, auch wenn der Ablauf schon fertig ist: sonst hieße
			// es "fertig", obwohl Verzeichnisse fehlen
			r.setCancelled()
			return
		}
		r.dirSlots <- struct{}{}
		defer func() { <-r.dirSlots }()
		if err := r.req.dst.Mkdir(dstPath); err != nil {
			r.fail(node, dstPath, err)
			return
		}
		// Während des Kopierens nur für den Besitzer zugänglich
		_ = r.req.dst.Chmod(dstPath, 0o700)
		job.ok = true
	}()
	return job
}

// parentDirJob liefert den Auftrag für das Elternverzeichnis eines
// Eintrags, wenn es in diesem Lauf neu angelegt wird. Nur aus dispatch.
//
// @param node - Eintrag
// @return *dirJob - Auftrag oder nil (Elternverzeichnis gab es schon)
// @date   2026-10-24 20:29
func (r *copyRun) parentDirJob(node copyNode) *dirJob {
	return r.dirJobs[path.Dir(node.rel)]
}

// startWorkers startet copyParallel Arbeiter für Dateiübertragungen.
//
// @return chan<- copyJob - Aufträge (schließen, wenn alle vergeben sind)
// @return func() - wartet, bis alle Arbeiter fertig sind
// @date   2026-10-23 09:40
func (r *copyRun) startWorkers() (chan<- copyJob, func()) {
	jobs := make(chan copyJob)
	var wg sync.WaitGroup
	for i := 0; i < max(copyParallel, 1); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				r.transfer(job)
				r.inflight.Done()
			}
		}()
	}
	return jobs, wg.Wait
}

// transfer überträgt eine Datei (Arbeiter) und verbucht das Ergebnis.
// Beim Verschieben wird die Quelle erst nach erfolgreichem Umbenennen am
// Ziel gelöscht.
//
// @param job - Auftrag
// @date   2026-10-24 20:29
func (r *copyRun) transfer(job copyJob) {
	if !job.parent.wait() {
		// Elternverzeichnis nicht angelegt (Fehler schon gemeldet oder
		// Abbruch): übersprungen, Fortschritt trotzdem weiter
		if r.ctx.Err() != nil {
			r.setCancelled()
			return
		}
		r.countSkipped(job.node)
		return
	}
	if r.ctx.Err() != nil {
		r.setCancelled()
		return
	}
	if err := r.copyData(job.srcPath, job.dstPath, job.node.info); err != nil {
		if r.ctx.Err() != nil {
			r.setCancelled()
			return
		}
		r.fail(job.node, job.dstPath, err)
		r.countDone(job.node, job.node.info.Size())
		return
	}
	r.mu.Lock()
	r.result.files++
	r.result.bytes = satAdd(r.result.bytes, max(job.node.info.Size(), 0))
	r.progress.doneFiles++
	r.sendProgressLocked(true)
	r.mu.Unlock()
	r.removeMovedSource(job.node, job.srcPath)
}

// setCancelled markiert den Vorgang als abgebrochen und hält alle Arbeiter an.
//
// @date   2026-10-23 09:40
func (r *copyRun) setCancelled() {
	r.mu.Lock()
	r.result.cancelled = true
	r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
}

// targetInfo liefert, ob und wie ein Eintrag am Ziel schon existiert. Statt
// jede Datei einzeln abzufragen (eine Rundreise pro Datei), wird das
// Zielverzeichnis einmal gelesen; neu angelegte Verzeichnisse sind leer.
//
// Der Liste wird nur geglaubt, wenn das sicher ist (Review Build 78):
//   - Kollidiert der Name gefaltet (looseTargetKey) mit einem in diesem
//     Lauf schon vergebenen (README und readme, a.txt. und a.txt auf einem
//     Windows-Ziel), wird gewartet, bis alle laufenden Übertragungen fertig sind,
//     und dann einzeln gefragt - sonst würde die zweite Datei die erste
//     still ersetzen (beim Verschieben: beide Quellen weg).
//   - Ungewöhnliche Namen, die ein Server selbst umdeuten kann (Punkt oder
//     Leerzeichen am Ende, "~" wie in 8.3-Kurznamen, Nicht-ASCII wegen
//     Unicode-Normalisierung), werden genauso einzeln gefragt.
//   - Findet die Liste den Namen nur ohne Groß-/Kleinschreibung, wird
//     einzeln gefragt.
//   - Der lokale PC wird immer einzeln gefragt (keine Netzwerk-Rundreise).
//
// Wird einzeln gefragt (Lstat), ist das Elternverzeichnis aber in diesem
// Lauf gescheitert, wird nicht gefragt, sondern übersprungen
// (Sicherheitsaudit Build 81): Dort kann inzwischen ein fremder Ordner
// liegen (untergeschobener Symlink/Junction), und Lstat würde durch ihn
// hindurch einen Eintrag finden, der dann zusammengeführt würde.
//
// @param rel - Pfad relativ zum Zielverzeichnis des Auftrags
// @param dstPath - Zielpfad
// @return os.FileInfo - vorhandener Eintrag
// @return bool - existiert er?
// @return bool - überspringen (Elternverzeichnis gescheitert)
// @date   2026-10-24 20:29
func (r *copyRun) targetInfo(rel, dstPath string) (os.FileInfo, bool, bool) {
	key, simple := looseTargetKey(rel)
	exact := !simple || r.assigned[key]
	r.assigned[key] = true
	if exact {
		// Laufende Übertragungen könnten genau diesen Eintrag gerade anlegen
		r.inflight.Wait()
		if !r.dirJobs[path.Dir(rel)].wait() {
			return nil, false, true
		}
		info, err := r.req.dst.Lstat(dstPath)
		return info, err == nil, false
	}
	dir, name := path.Dir(dstPath), path.Base(dstPath)
	listing, known := r.dstDirs[dir]
	if !known && !isLocalFS(r.req.dst) {
		listing = r.listTarget(dir)
		r.dstDirs[dir] = listing
	}
	if listing != nil {
		if info, ok := listing.exact[name]; ok {
			return info, true, false
		}
		if !listing.folded[strings.ToLower(name)] {
			return nil, false, false
		}
	}
	info, err := r.req.dst.Lstat(dstPath)
	return info, err == nil, false
}

// looseTargetKey liefert einen Vergleichsschlüssel für einen Zielpfad, so
// wie das großzügigste Ziel Namen gleichsetzt (Sicherheitsreview Build 78):
// Groß-/Kleinschreibung nach Unicode gefaltet (auch das Kelvin-Zeichen K
// = k, ſ = s), Punkte und Leerzeichen am Ende jedes Namensteils entfernt
// (Windows legt "a.txt." als "a.txt" ab). Dazu, ob alle Namensteile
// "einfach" sind: nur druckbares ASCII, kein Punkt/Leerzeichen am Ende,
// kein "~". Nur für einfache Namen ist ein Vergleich per Liste verlässlich.
//
// @param rel - Pfad relativ zum Zielverzeichnis des Auftrags
// @return string - Schlüssel
// @return bool - true wenn alle Namensteile einfach sind
// @date   2026-10-23 20:01
func looseTargetKey(rel string) (string, bool) {
	simple := true
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		if !simpleTargetName(part) {
			simple = false
		}
		folded := strings.Map(foldRune, part)
		if trimmed := strings.TrimRight(folded, ". "); trimmed != "" {
			folded = trimmed
		}
		parts[i] = folded
	}
	return strings.Join(parts, "/"), simple
}

// foldRune faltet ein Zeichen auf einen festen Vertreter aller Zeichen, die
// sich nur in der Groß-/Kleinschreibung unterscheiden (kleinstes Zeichen
// der Faltungskette, klein geschrieben): K, k und das Kelvin-Zeichen
// ergeben alle "k".
//
// @param r - Zeichen
// @return rune - gefaltetes Zeichen
// @date   2026-10-23 20:01
func foldRune(r rune) rune {
	smallest := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f < smallest {
			smallest = f
		}
	}
	return unicode.ToLower(smallest)
}

// simpleTargetName prüft einen einzelnen Namen (siehe looseTargetKey).
//
// @param name - Name
// @return bool - true wenn einfach
// @date   2026-10-23 19:55
func simpleTargetName(name string) bool {
	if name == "" || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; c < 0x20 || c > 0x7e || c == '~' {
			return false
		}
	}
	return true
}

// targetListing ist die vorgehaltene Liste eines Zielverzeichnisses.
type targetListing struct {
	exact  map[string]os.FileInfo
	folded map[string]bool
}

// newTargetListing baut eine Liste aus Verzeichniseinträgen (leer = neu
// angelegtes Verzeichnis).
//
// @param infos - Einträge
// @return *targetListing - Liste
// @date   2026-10-23 09:40
func newTargetListing(infos []os.FileInfo) *targetListing {
	l := &targetListing{exact: map[string]os.FileInfo{}, folded: map[string]bool{}}
	for _, info := range infos {
		l.exact[info.Name()] = info
		l.folded[strings.ToLower(info.Name())] = true
	}
	return l
}

// listTarget liest ein Zielverzeichnis für targetInfo. Fehler oder sehr
// große Verzeichnisse ergeben nil (dann wird einzeln gefragt).
//
// @param dir - Zielverzeichnis
// @return *targetListing - Liste oder nil
// @date   2026-10-23 09:40
func (r *copyRun) listTarget(dir string) *targetListing {
	infos, err := r.req.dst.ReadDir(dir)
	if err != nil || len(infos) > copyMaxTargetListing {
		return nil
	}
	return newTargetListing(infos)
}

// sourceInfos liest das Quellverzeichnis einmal, wenn viele Einträge
// markiert sind - sonst kostet jeder markierte Eintrag eine Rundreise.
//
// @return map[string]os.FileInfo - Einträge nach Name (nil = einzeln fragen)
// @date   2026-10-23 09:40
func (r *copyRun) sourceInfos() map[string]os.FileInfo {
	if len(r.req.names) < 2 {
		return nil
	}
	infos, err := r.req.src.ReadDir(r.req.srcDir)
	if err != nil {
		return nil
	}
	byName := make(map[string]os.FileInfo, len(infos))
	for _, info := range infos {
		// Ohne Rechte-Angaben (Mode 0) lieber einzeln per Lstat fragen
		if info.Mode() != 0 {
			byName[info.Name()] = info
		}
	}
	return byName
}
