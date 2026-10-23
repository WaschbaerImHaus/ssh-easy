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
// @author Kurt Ingwer
// @date   2026-10-23 09:40

package main

import (
	"context"
	"io"
	"os"
	"path"
	"strings"
	"sync"
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
	// copyMaxTargetListing - größere Zielverzeichnisse werden nicht als
	// Liste vorgehalten (dann Lstat pro Datei)
	copyMaxTargetListing = 100_000
)

// copyParallel - so viele Dateien werden gleichzeitig übertragen
// (Variable, damit Tests sie ändern können).
var copyParallel = 8

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
// @date   2026-10-23 09:40
func pipeCopy(ctx context.Context, dst io.Writer, src io.Reader, budget *copyBudget) (int64, error) {
	// Pro Datei höchstens copyPipeChunks Blöcke: in der Warteschlange,
	// beim Leser und beim Schreiber je einer
	chunks := make(chan copyChunk, max(copyPipeChunks-2, 1))
	done := make(chan struct{})
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

// copyJob ist eine Datei, die ein Arbeiter überträgt.
type copyJob struct {
	node    copyNode
	srcPath string
	dstPath string
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
// @date   2026-10-23 09:40
func (r *copyRun) transfer(job copyJob) {
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
//   - Kollidiert der Name ohne Rücksicht auf Groß-/Kleinschreibung mit einem
//     in diesem Lauf schon vergebenen (README und readme auf einem Windows-
//     Ziel), wird gewartet, bis alle laufenden Übertragungen fertig sind,
//     und dann einzeln gefragt - sonst würde die zweite Datei die erste
//     still ersetzen (beim Verschieben: beide Quellen weg).
//   - Ungewöhnliche Namen, die ein Server selbst umdeuten kann (Punkt oder
//     Leerzeichen am Ende, "~" wie in 8.3-Kurznamen, Nicht-ASCII wegen
//     Unicode-Normalisierung), werden genauso einzeln gefragt.
//   - Findet die Liste den Namen nur ohne Groß-/Kleinschreibung, wird
//     einzeln gefragt.
//   - Der lokale PC wird immer einzeln gefragt (keine Netzwerk-Rundreise).
//
// @param rel - Pfad relativ zum Zielverzeichnis des Auftrags
// @param dstPath - Zielpfad
// @return os.FileInfo - vorhandener Eintrag
// @return bool - existiert er?
// @date   2026-10-23 19:55
func (r *copyRun) targetInfo(rel, dstPath string) (os.FileInfo, bool) {
	key, simple := looseTargetKey(rel)
	exact := !simple || r.assigned[key]
	r.assigned[key] = true
	if exact {
		// Laufende Übertragungen könnten genau diesen Eintrag gerade anlegen
		r.inflight.Wait()
		info, err := r.req.dst.Lstat(dstPath)
		return info, err == nil
	}
	dir, name := path.Dir(dstPath), path.Base(dstPath)
	listing, known := r.dstDirs[dir]
	if !known && !isLocalFS(r.req.dst) {
		listing = r.listTarget(dir)
		r.dstDirs[dir] = listing
	}
	if listing != nil {
		if info, ok := listing.exact[name]; ok {
			return info, true
		}
		if !listing.folded[strings.ToLower(name)] {
			return nil, false
		}
	}
	info, err := r.req.dst.Lstat(dstPath)
	return info, err == nil
}

// looseTargetKey liefert einen Vergleichsschlüssel für einen Zielpfad, der
// Groß-/Kleinschreibung ignoriert, und ob alle Namensteile "einfach" sind:
// nur druckbares ASCII, kein Punkt/Leerzeichen am Ende, kein "~". Nur für
// einfache Namen ist ein Vergleich per Liste verlässlich.
//
// @param rel - Pfad relativ zum Zielverzeichnis des Auftrags
// @return string - Schlüssel (klein geschrieben)
// @return bool - true wenn alle Namensteile einfach sind
// @date   2026-10-23 19:55
func looseTargetKey(rel string) (string, bool) {
	simple := true
	for _, part := range strings.Split(rel, "/") {
		if !simpleTargetName(part) {
			simple = false
		}
	}
	return strings.ToLower(rel), simple
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
