// Paket main - Datei-Manager (MC-Modus): Erfassen vor dem Kopieren
//
// scan erfasst vor dem Kopieren bzw. Verschieben alle Einträge der Quelle
// (Verzeichnisse vor ihrem Inhalt, Geschwister nach Namen sortiert). Bis
// Build 89 lief das streng nacheinander: ein ReadDir pro Verzeichnis, also
// bei 30.000 leeren Verzeichnissen 30.000 Server-Rundreisen hintereinander
// (Nutzerbericht: "das Zählen hat unendlich lange gedauert").
//
// Seit Build 90 in zwei Schritten:
//  1. Vorlesen (prefetch): bis zu copyParallel Arbeiter lesen die
//     Verzeichnisse gleichzeitig (ReadDir, bei Einträgen ohne Rechte-Angabe
//     Lstat). Das Ergebnis ist ein Baum aus scanEntry. Die Grenzen gelten
//     schon hier: Verzeichnisse ab copyMaxDepth werden nicht gelesen, und
//     sind mehr als copyMaxEntries Einträge gefunden, hört das Lesen auf
//     (lieber früher abbrechen als den Speicher füllen).
//  2. Zusammensetzen (assemble): eine einzige Goroutine geht den Baum in
//     derselben Reihenfolge durch wie früher walk und prüft dabei genau wie
//     früher Grenzen, Namen und Fehler. Reihenfolge der Einträge und der
//     Fehlermeldungen bleibt also gleich, egal in welcher Reihenfolge die
//     Arbeiter fertig wurden.
//
// Abbruch (Esc) wirkt sofort: Die Arbeiter nehmen nach einem beendeten
// Kontext keine Verzeichnisse mehr an; laufende Anfragen kehren nach einer
// Rundreise zurück.
//
// @author Kurt Ingwer
// @date   2026-11-15 15:03
package main

import (
	"os"
	"path"
	"sort"
	"strings"
	"sync"
)

// scanEntry ist ein beim Vorlesen gefundener Eintrag der Quelle. Die
// Felder schreibt nur der Arbeiter, der den Eintrag bearbeitet; gelesen
// werden sie erst nach dem Ende aller Arbeiter (assemble).
type scanEntry struct {
	// Name und Pfad relativ zu srcDir
	name string
	rel  string
	// Name ungültig (safeCopyName/fsAcceptsName): nur melden, nie lesen
	unsafe bool
	// Angaben zum Eintrag (aus ReadDir des Elternverzeichnisses oder Lstat)
	info os.FileInfo
	// Fehler von Lstat
	statErr error
	// info bzw. statErr stehen fest
	resolved bool
	// Verzeichnis gelesen (children bzw. listErr stehen fest)
	listed   bool
	listErr  error
	children []*scanEntry
}

// prefetchQueue verteilt beim Vorlesen (Erfassen vor dem Kopieren,
// Löschen) die noch zu lesenden Einträge an die Arbeiter. Sie arbeitet als
// Stapel (zuletzt gefunden, zuerst gelesen): so bleibt die Warteschlange
// auch bei breiten Bäumen klein.
type prefetchQueue[T any] struct {
	mu    sync.Mutex
	cond  *sync.Cond
	items []T
	// Arbeiter, die gerade einen Eintrag bearbeiten
	busy int
	// Lesen beendet (Abbruch oder Grenze)
	stop bool
	// Bisher gefundene Einträge (Grenze copyMaxEntries)
	found int
}

// newPrefetchQueue legt eine leere Warteschlange an.
//
// @return *prefetchQueue[T] - Warteschlange
// @date   2026-11-13 09:28
func newPrefetchQueue[T any]() *prefetchQueue[T] {
	q := &prefetchQueue[T]{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// push legt Einträge zum Lesen ab (nach dem Ende des Lesens nicht mehr).
// Der zuletzt abgelegte wird zuerst gelesen.
//
// @param items - Einträge
// @date   2026-11-13 09:28
func (q *prefetchQueue[T]) push(items ...T) {
	if len(items) == 0 {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stop {
		return
	}
	q.items = append(q.items, items...)
	q.cond.Broadcast()
}

// pop wartet auf den nächsten Eintrag. Liefert false, wenn nichts mehr zu
// tun ist: keine Einträge und kein Arbeiter mehr beschäftigt (dann kommen
// auch keine neuen), das Lesen beendet oder der Kontext abgebrochen.
//
// @param cancelled - meldet einen abgebrochenen Kontext
// @return T - Eintrag
// @return bool - false = Arbeiter beenden
// @date   2026-11-13 09:28
func (q *prefetchQueue[T]) pop(cancelled func() bool) (T, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && q.busy > 0 && !q.stop {
		q.cond.Wait()
	}
	if q.stop || len(q.items) == 0 || cancelled() {
		q.stop = true
		// Alle Wartenden wecken, damit sie ebenfalls aufhören
		q.cond.Broadcast()
		var none T
		return none, false
	}
	e := q.items[len(q.items)-1]
	q.items = q.items[:len(q.items)-1]
	q.busy++
	return e, true
}

// done meldet, dass ein Arbeiter seinen Eintrag fertig bearbeitet hat.
//
// @date   2026-11-13 09:28
func (q *prefetchQueue[T]) done() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.busy--
	if q.busy == 0 && len(q.items) == 0 {
		q.cond.Broadcast()
	}
}

// count zählt neu gefundene Einträge; über copyMaxEntries endet das Lesen.
//
// @param n - Anzahl
// @return bool - false = Grenze überschritten, nichts mehr ablegen
// @date   2026-11-13 09:28
func (q *prefetchQueue[T]) count(n int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.found += n
	if q.found > copyMaxEntries {
		q.stop = true
		q.cond.Broadcast()
		return false
	}
	return true
}

// runPrefetch startet workers Arbeiter, die fetch für jeden Eintrag der
// Warteschlange aufrufen (fetch darf weitere ablegen), und kehrt zurück,
// wenn alle fertig sind.
//
// @param q - Warteschlange (schon gefüllt)
// @param workers - Anzahl der Arbeiter
// @param cancelled - meldet einen abgebrochenen Kontext
// @param fetch - Arbeit pro Eintrag
// @date   2026-11-13 09:28
func runPrefetch[T any](q *prefetchQueue[T], workers int, cancelled func() bool, fetch func(T)) {
	var wg sync.WaitGroup
	for w := 0; w < max(workers, 1); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				e, ok := q.pop(cancelled)
				if !ok {
					return
				}
				fetch(e)
				q.done()
			}
		}()
	}
	wg.Wait()
}

// scanDepth liefert die Verschachtelungstiefe eines relativen Pfads
// ("a" = 0, "a/b" = 1) - wie die Tiefengrenze beim Erfassen sie zählt.
//
// @param rel - Pfad relativ zu srcDir
// @return int - Tiefe
// @date   2026-11-13 09:28
func scanDepth(rel string) int {
	return strings.Count(rel, "/")
}

// scan erfasst alle Einträge (Verzeichnisse vor ihrem Inhalt). Die
// obersten Einträge werden der Reihe nach geprüft wie bisher (unsicherer
// Name, Verschieben in sich selbst, schon umbenannt); die übrigen liest
// auf Servern zuerst fastScan mit einem find-Befehl, sonst prefetch
// gleichzeitig per SFTP. assemble setzt sie der Reihe nach zusammen.
//
// @return []copyNode - Einträge in Kopierreihenfolge
// @date   2026-11-13 09:28
func (r *copyRun) scan() []copyNode {
	var known map[string]os.FileInfo
	if len(r.renamed) < len(r.req.names) {
		// Nur lesen, wenn noch etwas zu erfassen ist (nicht alles umbenannt)
		known = r.sourceInfos()
	}
	tops := make([]*scanEntry, len(r.req.names))
	var pending []*scanEntry
	for i, name := range r.req.names {
		if r.renamed[name] || !safeCopyName(name) || !fsAcceptsName(r.req.dst, name) || r.movesIntoItself(name) {
			// Wird unten in assemble gemeldet bzw. ist schon erledigt
			continue
		}
		e := &scanEntry{name: name, rel: name}
		if info, ok := known[name]; ok && info != nil {
			e.info, e.resolved = info, true
			r.countScanned(info)
		}
		tops[i] = e
		pending = append(pending, e)
	}
	// Server: erst der Schnellweg per find (filemanager_fastscan.go), sonst
	// bzw. bei jedem Zweifel gleichzeitig per SFTP
	if !r.fastScan(pending) {
		r.prefetch(pending)
	}
	return r.assemble(tops)
}

// prefetch liest die Einträge mit bis zu copyParallel Arbeitern
// gleichzeitig vor und kehrt zurück, wenn alle fertig sind.
//
// @param pending - zu bearbeitende oberste Einträge
// @date   2026-11-13 09:28
func (r *copyRun) prefetch(pending []*scanEntry) {
	if len(pending) == 0 {
		return
	}
	q := newPrefetchQueue[*scanEntry]()
	if !q.count(len(pending)) {
		return
	}
	// Umgekehrt ablegen: der Stapel gibt dann den ersten zuerst heraus
	for i := len(pending) - 1; i >= 0; i-- {
		q.push(pending[i])
	}
	cancelled := func() bool { return r.ctx.Err() != nil }
	runPrefetch(q, copyParallel, cancelled, func(e *scanEntry) { r.fetchEntry(e, q) })
}

// fetchEntry bearbeitet einen Eintrag: unbekannte Angaben per Lstat
// holen, Verzeichnisse (unterhalb der Tiefengrenze) lesen, sortieren und
// ihre Einträge zum Weiterlesen ablegen.
//
// @param e - Eintrag (gehört während des Aufrufs nur diesem Arbeiter)
// @param q - Warteschlange
// @date   2026-11-15 15:03
func (r *copyRun) fetchEntry(e *scanEntry, q *prefetchQueue[*scanEntry]) {
	if r.ctx.Err() != nil {
		return
	}
	srcPath := path.Join(r.req.srcDir, e.rel)
	if !e.resolved {
		info, err := r.req.src.Lstat(srcPath)
		e.info, e.statErr, e.resolved = info, err, true
		if err != nil {
			return
		}
		r.countScanned(info)
	}
	if !e.info.IsDir() || scanDepth(e.rel) >= copyMaxDepth || r.ctx.Err() != nil {
		// Zu tief: assemble bricht vorher mit errCopyLimit ab
		return
	}
	infos, err := r.req.src.ReadDir(srcPath)
	if err != nil {
		e.listErr, e.listed = err, true
		return
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name() < infos[j].Name() })
	children := make([]*scanEntry, 0, len(infos))
	var next []*scanEntry
	// Nur sichere Namen zählen zur Eintragsgrenze (wie bis Build 89)
	safe := 0
	for _, info := range infos {
		name := info.Name()
		if name == "." || name == ".." {
			continue
		}
		child := &scanEntry{name: name, rel: path.Join(e.rel, name)}
		if !safeCopyName(name) || !fsAcceptsName(r.req.dst, name) {
			// Nicht path.Join beim Melden: das würde ".." auflösen
			child.unsafe = true
			children = append(children, child)
			continue
		}
		safe++
		// Ohne Rechte-Angaben in der Liste (Mode 0) einzeln fragen -
		// sonst gälte ein Verzeichnis als Datei
		if info.Mode() != 0 {
			child.info, child.resolved = info, true
			r.countScanned(info)
		}
		children = append(children, child)
		if scanDepth(child.rel) < copyMaxDepth && (!child.resolved || child.info.IsDir()) {
			next = append(next, child)
		}
	}
	e.children, e.listed = children, true
	if !q.count(safe) {
		return
	}
	// Umgekehrt ablegen, damit der Stapel in Namensreihenfolge abarbeitet
	for i, j := 0, len(next)-1; i < j; i, j = i+1, j-1 {
		next[i], next[j] = next[j], next[i]
	}
	q.push(next...)
}

// countScanned zählt einen gefundenen Eintrag für die Anzeige beim
// Erfassen (Dateien inkl. Symlinks bzw. Verzeichnisse). Die genauen
// Summen setzt assemble am Ende.
//
// @param info - Angaben zum Eintrag
// @date   2026-11-13 09:28
func (r *copyRun) countScanned(info os.FileInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if info.IsDir() {
		r.progress.totalDirs++
	} else {
		r.progress.totalFiles++
	}
	r.sendProgressLocked(false)
}

// assemble setzt die vorgelesenen Einträge in Kopierreihenfolge zusammen
// (Tiefensuche, Verzeichnis vor Inhalt) und prüft dabei wie früher walk:
// Grenzen, unsichere Namen, Lese- und Lstat-Fehler. Fehlt ein Eintrag,
// weil das Vorlesen an der Grenze aufgehört hat, gilt die Grenze als
// erreicht; nach einem Abbruch endet es still.
//
// @param tops - oberste Einträge in der Reihenfolge von req.names (nil =
// nicht vorgelesen: unsicher, in sich selbst oder schon umbenannt)
// @return []copyNode - Einträge in Kopierreihenfolge
// @date   2026-11-13 09:28
func (r *copyRun) assemble(tops []*scanEntry) []copyNode {
	var nodes []copyNode
	files, dirs, bytes := 0, 0, int64(0)
	limit := func(top, p string) {
		r.limitHit = true
		r.fail(copyNode{top: top}, p, errCopyLimit)
	}
	var walk func(e *scanEntry, top string)
	walk = func(e *scanEntry, top string) {
		if r.ctx.Err() != nil || r.limitHit {
			return
		}
		srcPath := path.Join(r.req.srcDir, e.rel)
		// Schutz vor endlosen Bäumen eines bösartigen Servers
		if len(nodes) >= copyMaxEntries || scanDepth(e.rel) >= copyMaxDepth {
			limit(top, srcPath)
			return
		}
		if !e.resolved {
			// Nicht vorgelesen: Abbruch oder Eintragsgrenze beim Vorlesen
			if r.ctx.Err() == nil {
				limit(top, srcPath)
			}
			return
		}
		if e.statErr != nil {
			r.fail(copyNode{top: top}, srcPath, e.statErr)
			return
		}
		node := copyNode{rel: e.rel, top: top, info: e.info}
		nodes = append(nodes, node)
		if !e.info.IsDir() {
			files++
			if e.info.Mode().IsRegular() {
				bytes = satAdd(bytes, max(e.info.Size(), 0))
			}
			return
		}
		dirs++
		if !e.listed {
			if r.ctx.Err() == nil {
				limit(top, srcPath)
			}
			return
		}
		if e.listErr != nil {
			// Verzeichnis selbst wird angelegt, sein Inhalt fehlt
			r.fail(node, srcPath, e.listErr)
			return
		}
		for _, child := range e.children {
			if child.unsafe {
				// Nicht path.Join: das würde ".." auflösen
				r.fail(node, srcPath+"/"+child.name, errUnsafeName)
				continue
			}
			walk(child, top)
		}
	}
	for i, name := range r.req.names {
		switch {
		case r.renamed[name]:
			// Schon per Umbenennen verschoben (renameMoved)
			continue
		case !safeCopyName(name) || !fsAcceptsName(r.req.dst, name):
			r.fail(copyNode{top: name}, name, errUnsafeName)
			continue
		case r.movesIntoItself(name):
			r.fail(copyNode{top: name}, path.Join(r.req.srcDir, name), errMoveIntoItself)
			continue
		}
		if tops[i] != nil {
			walk(tops[i], name)
		}
	}
	// Genaue Summen statt der Zählung beim Vorlesen
	r.mu.Lock()
	r.progress.totalFiles, r.progress.totalDirs, r.progress.totalBytes = files, dirs, bytes
	r.mu.Unlock()
	return nodes
}
