// Paket main - Datei-Manager (MC-Modus): Löschen (F8)
//
// runDelete löscht Einträge eines Verzeichnisses - Verzeichnisse rekursiv
// samt Inhalt (erst der Inhalt, dann das Verzeichnis). Symlinks werden nie
// verfolgt: gelöscht wird der Link selbst, sein Ziel bleibt unangetastet
// (Lstat statt Stat).
//
// Fehler einzelner Einträge brechen nicht ab; ein Verzeichnis, in dem
// etwas nicht gelöscht werden konnte, bleibt stehen. Schutz vor
// bösartigen Servern wie beim Kopieren: Namen mit "/", "\" oder
// Steuerzeichen werden abgelehnt (sonst könnte "../x" außerhalb löschen),
// Anzahl und Tiefe der Einträge sind begrenzt (copyMaxEntries/copyMaxDepth).
//
// Schutz vor ausgetauschten Verzeichnissen: SFTP arbeitet nur mit Pfaden.
// Ersetzt jemand ein Verzeichnis während des Löschens durch einen Symlink,
// würden die folgenden Pfade in das Ziel des Links führen. Deshalb wird
// immer per Lstat geprüft: nach dem Einlesen eines Verzeichnisses die ganze
// Kette vom gewählten Eintrag bis zu ihm, vor jedem Löschen das direkte
// Elternverzeichnis (ohne RealPath: auch hier die ganze Kette).
// Zusätzlich - nie statt dessen, denn nicht jeder Server löst bei
// realpath wirklich Symlinks auf - muss der kanonische Pfad (RealPath)
// des betroffenen Verzeichnisses dem erwarteten entsprechen; so fällt bei
// üblichen Servern (OpenSSH) ein Austausch an jeder Stelle der Kette auf.
// Bei einer Abweichung wird darin nichts mehr gelöscht (errDeleteRace).
// Ein Restfenster zwischen Prüfung und Löschen bleibt (SECURITY_RISKS.md).
//
// Tempo (Build 90): Alle Lesezugriffe laufen vorab gleichzeitig
// (prefetchDelete, bis deleteParallel Arbeiter): Lstat jedes Eintrags,
// ReadDir der Verzeichnisse und direkt danach die Kettenprüfung wie
// bisher. Geprüft-und-gelöscht wird danach weiterhin von EINER Goroutine
// in derselben Reihenfolge wie bisher: beim Betreten jedes Verzeichnisses
// erneut die ganze Kette (Audit Build 90: das Vorlesen kann lange
// zurückliegen), unmittelbar vor jedem Remove die frische Prüfung des
// Elternverzeichnisses, dann das Remove. So bleibt die
// Zusage, dass nach einem Austausch höchstens der eine gerade laufende
// Eintrag fällt (gleichzeitige Removes würden das Fenster auf bis zu
// deleteParallel Einträge vergrößern). Ein Verzeichnis wird weiter erst
// gelöscht, wenn sein ganzer Inhalt fertig ist.
//
// Die Funktion kennt keine Oberfläche: Sie meldet Fortschritt und das
// Ergebnis als deleteEvent.
//
// @author Kurt Ingwer
// @date   2026-11-15 15:03
package main

import (
	"context"
	"errors"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// errDeleteRace - ein Verzeichnis wurde während des Löschens ersetzt (z. B.
// durch einen Symlink); darin wird nichts mehr gelöscht.
var errDeleteRace = errors.New("directory was replaced during deletion")

// deleteParallel - so viele Lesezugriffe laufen beim Vorlesen höchstens
// gleichzeitig (Variable, damit Tests sie verkleinern können).
var deleteParallel = 32

// deleteRequest beschreibt einen Löschauftrag.
type deleteRequest struct {
	// Dateisystem
	fs remoteFS
	// Verzeichnis der Einträge
	dir string
	// Zu löschende Einträge (Namen in dir)
	names []string
}

// deleteProgress ist der Stand eines Löschvorgangs.
type deleteProgress struct {
	// Gerade bearbeiteter Pfad
	current string
	// Bisher gelöschte Einträge (Dateien, Symlinks, Verzeichnisse)
	done int
}

// deleteResult ist das Ergebnis eines Löschvorgangs.
type deleteResult struct {
	// Gelöschte Einträge insgesamt (mit Inhalt der Verzeichnisse)
	deleted int
	// Vollständig gelöschte oberste Einträge
	deletedNames []string
	// Fehler einzelner Einträge
	failures []copyFailure
	// Vom Nutzer abgebrochen
	cancelled bool
}

// deleteEvent ist eine Meldung des Löschvorgangs an die Oberfläche. Genau
// eines der Felder ist gesetzt.
type deleteEvent struct {
	progress *deleteProgress
	result   *deleteResult
}

// deleteRun hält den Zustand eines laufenden Löschvorgangs.
type deleteRun struct {
	ctx    context.Context
	req    deleteRequest
	events chan<- deleteEvent
	// Schützt progress und lastSent (die Arbeiter des Vorlesens melden
	// den gerade gelesenen Pfad)
	mu       sync.Mutex
	progress deleteProgress
	lastSent time.Time
	// Ergebnis, Zähler und Grenze: nur die löschende Goroutine
	result deleteResult
	// Besuchte Einträge (Grenze copyMaxEntries)
	visited int
	// Grenze erreicht: nichts weiter löschen
	limitHit bool
	// Kanonischer Pfad von req.dir (leer = Server kann kein RealPath)
	base string
	// Zustand von req.dir beim Start (Art, ggf. Linkziel) - Vergleich,
	// wenn der Server kein RealPath kann
	dirKnown bool
	dirType  os.FileMode
	dirLink  string
}

// deleteEntry ist ein vorab gelesener Eintrag (prefetchDelete). Die Felder
// schreibt nur der Arbeiter, der ihn bearbeitet; gelesen werden sie erst
// nach dem Ende aller Arbeiter.
type deleteEntry struct {
	// Name, Pfad, Pfad des gewählten obersten Eintrags, Tiefe darunter
	name  string
	path  string
	top   string
	depth int
	// Name ungültig (safeCopyName): nur melden, nie lesen oder löschen
	unsafe bool
	// Lstat (folgt Symlinks nicht)
	info     os.FileInfo
	statErr  error
	resolved bool
	// Verzeichnis: ReadDir + Kettenprüfung (listErr = errDeleteRace bei
	// Austausch), Einträge nach Namen sortiert
	listed   bool
	listErr  error
	children []*deleteEntry
}

// runDelete führt einen Löschauftrag aus und meldet am Ende genau ein
// deleteEvent mit result. Abbruch über ctx. Erst liest prefetchDelete den
// ganzen Baum gleichzeitig vor, dann löscht remove der Reihe nach.
//
// @param ctx - Abbruch-Kontext
// @param req - Auftrag
// @param events - Kanal für Fortschritt und Ergebnis
// @date   2026-11-13 09:08
func runDelete(ctx context.Context, req deleteRequest, events chan<- deleteEvent) {
	run := &deleteRun{ctx: ctx, req: req, events: events}
	if real, err := req.fs.RealPath(req.dir); err == nil && path.IsAbs(real) {
		run.base = path.Clean(real)
	}
	run.dirType, run.dirLink, run.dirKnown = run.panelDirState()
	tops := make([]*deleteEntry, len(req.names))
	var pending []*deleteEntry
	for i, name := range req.names {
		if safeCopyName(name) {
			p := path.Join(req.dir, name)
			tops[i] = &deleteEntry{name: name, path: p, top: p}
			pending = append(pending, tops[i])
		}
	}
	run.prefetchDelete(pending)
	for i, name := range req.names {
		if run.stopped() {
			break
		}
		if tops[i] == nil {
			// Nicht path.Join: das würde ".." auflösen
			run.fail(req.dir+"/"+name, errUnsafeName)
			continue
		}
		if run.remove(tops[i]) {
			run.result.deletedNames = append(run.result.deletedNames, name)
		}
	}
	run.result.cancelled = ctx.Err() != nil
	run.result.deleted = run.progress.done
	result := run.result
	events <- deleteEvent{result: &result}
}

// prefetchDelete liest die Einträge mit bis zu deleteParallel Arbeitern
// gleichzeitig vor: Lstat, bei Verzeichnissen (unter der Tiefengrenze)
// ReadDir und unmittelbar danach - wie bisher nach dem Einlesen - die
// Kettenprüfung unchanged(top, p, true). Gelöscht wird hier nichts. Mehr
// als copyMaxEntries gefundene Einträge beenden das Lesen (remove meldet
// dann die Grenze).
//
// @param pending - oberste Einträge (sichere Namen)
// @date   2026-11-13 09:08
func (d *deleteRun) prefetchDelete(pending []*deleteEntry) {
	if len(pending) == 0 {
		return
	}
	q := newPrefetchQueue[*deleteEntry]()
	if !q.count(len(pending)) {
		return
	}
	for i := len(pending) - 1; i >= 0; i-- {
		q.push(pending[i])
	}
	cancelled := func() bool { return d.ctx.Err() != nil }
	runPrefetch(q, deleteParallel, cancelled, func(e *deleteEntry) { d.fetchDeleteEntry(e, q) })
}

// fetchDeleteEntry liest einen Eintrag vor (siehe prefetchDelete) bzw.
// - mit q == nil - bei Bedarf aus remove, wenn das Vorlesen an der
// Eintragsgrenze aufgehört hat.
//
// @param e - Eintrag (gehört während des Aufrufs nur diesem Arbeiter)
// @param q - Warteschlange (nil = nichts ablegen und nicht zählen)
// @date   2026-11-15 15:03
func (d *deleteRun) fetchDeleteEntry(e *deleteEntry, q *prefetchQueue[*deleteEntry]) {
	if d.ctx.Err() != nil || e.depth >= copyMaxDepth {
		// Zu tief: remove meldet vorher die Grenze
		return
	}
	d.mu.Lock()
	d.progress.current = e.path
	d.sendProgressLocked(false)
	d.mu.Unlock()
	// Lstat: ein Symlink ist nie ein Verzeichnis - gelöscht wird der Link
	info, err := d.req.fs.Lstat(e.path)
	e.info, e.statErr, e.resolved = info, err, true
	if err != nil || !info.IsDir() || d.ctx.Err() != nil {
		return
	}
	children, err := d.req.fs.ReadDir(e.path)
	if err != nil {
		e.listErr, e.listed = err, true
		return
	}
	// Seit dem Lstat ausgetauscht? Ganze Kette bis hierher prüfen
	if !d.unchanged(e.top, e.path, true) {
		e.listErr, e.listed = errDeleteRace, true
		return
	}
	sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
	list := make([]*deleteEntry, 0, len(children))
	var next []*deleteEntry
	safe := 0
	for _, child := range children {
		name := child.Name()
		if name == "." || name == ".." {
			continue
		}
		c := &deleteEntry{name: name, path: path.Join(e.path, name), top: e.top, depth: e.depth + 1}
		if !safeCopyName(name) {
			// Unsichere Namen zählen nicht zur Grenze (wie bis Build 89)
			c.unsafe = true
		} else {
			safe++
			if c.depth < copyMaxDepth {
				next = append(next, c)
			}
		}
		list = append(list, c)
	}
	e.children, e.listed = list, true
	// Ohne Warteschlange (Lesen bei Bedarf in remove): nichts ablegen
	if q == nil || !q.count(safe) {
		return
	}
	// Umgekehrt ablegen, damit der Stapel in Namensreihenfolge abarbeitet
	for i, j := 0, len(next)-1; i < j; i, j = i+1, j-1 {
		next[i], next[j] = next[j], next[i]
	}
	q.push(next...)
}

// stopped meldet, ob abgebrochen wurde oder eine Grenze erreicht ist.
//
// @return bool - true = nichts weiter tun
// @date   2026-10-22 05:17
func (d *deleteRun) stopped() bool {
	return d.ctx.Err() != nil || d.limitHit
}

// fail merkt einen Fehler.
//
// @param p - Betroffener Pfad
// @param err - Fehler
// @date   2026-10-22 05:17
func (d *deleteRun) fail(p string, err error) {
	d.result.failures = append(d.result.failures, copyFailure{path: p, err: err})
}

// remove löscht einen vorgelesenen Eintrag; Verzeichnisse erst ihren
// Inhalt (der Reihe nach, wie bisher). Fehlt ein Eintrag, weil das
// Vorlesen an der Eintragsgrenze aufgehört hat, gilt die Grenze als
// erreicht; nach einem Abbruch endet es still.
//
// @param e - Eintrag
// @return bool - true = vollständig gelöscht
// @date   2026-11-15 15:03
func (d *deleteRun) remove(e *deleteEntry) bool {
	if d.stopped() {
		return false
	}
	p := e.path
	d.visited++
	if d.visited > copyMaxEntries || e.depth >= copyMaxDepth {
		d.limitHit = true
		d.fail(p, errCopyLimit)
		return false
	}
	if !e.resolved || (e.statErr == nil && e.info.IsDir() && !e.listed) {
		// Das Vorlesen hat an der Eintragsgrenze aufgehört (riesiges
		// Verzeichnis): wie bis Build 89 erst jetzt lesen - so wird bis zur
		// Grenze gelöscht, dann folgt die Meldung (visited oben)
		if d.ctx.Err() != nil {
			return false
		}
		d.fetchDeleteEntry(e, nil)
		if !e.resolved {
			return false
		}
	}
	d.mu.Lock()
	d.progress.current = p
	d.sendProgressLocked(false)
	d.mu.Unlock()

	if e.statErr != nil {
		d.fail(p, e.statErr)
		return false
	}
	if e.info.IsDir() {
		if !e.listed {
			// Nur nach einem Abbruch möglich (fetchDeleteEntry liest sonst)
			return false
		}
		if e.listErr != nil {
			d.fail(p, e.listErr)
			return false
		}
		// Beim Betreten, vor dem ersten Kind: ganze Kette erneut per Lstat
		// (und RealPath) prüfen. Die Prüfung beim Vorlesen liegt womöglich
		// lange zurück; bei Servern mit wörtlichem realpath (pkg/sftp-Server)
		// fiele ein seitdem ausgetauschtes Großelternverzeichnis sonst erst
		// auf, nachdem schon Kinder durch den Symlink gelöscht wurden
		// (Audit Build 90, wie bis Build 89 nach dem Einlesen).
		if !d.unchanged(e.top, p, true) {
			d.fail(p, errDeleteRace)
			return false
		}
		complete := true
		for _, child := range e.children {
			if child.unsafe {
				d.fail(p+"/"+child.name, errUnsafeName)
				complete = false
				continue
			}
			if !d.remove(child) {
				complete = false
			}
			if d.stopped() {
				return false
			}
		}
		if !complete {
			// Etwas ist geblieben: Verzeichnis stehen lassen
			return false
		}
	}
	// Kette bis zum Elternverzeichnis unmittelbar vor dem Löschen unverändert?
	if !d.unchanged(e.top, path.Dir(p), false) {
		d.fail(path.Dir(p), errDeleteRace)
		return false
	}
	if err := d.req.fs.Remove(p); err != nil {
		d.fail(p, err)
		return false
	}
	d.mu.Lock()
	d.progress.done++
	d.sendProgressLocked(false)
	d.mu.Unlock()
	return true
}

// unchanged prüft, ob ein Verzeichnis noch an der erwarteten Stelle liegt:
//  1. immer Lstat: dir (wholeChain bzw. ohne RealPath: die ganze Kette von
//     top bis dir) muss aus echten Verzeichnissen bestehen - außer dem
//     Fensterverzeichnis selbst, das ein Symlink sein darf
//  2. zusätzlich, wenn der Server RealPath kann: der kanonische Pfad muss
//     dem kanonischen Fensterverzeichnis plus relativem Pfad entsprechen
//
// @param top - Pfad des gewählten obersten Eintrags
// @param dir - zu prüfendes Verzeichnis (req.dir, top oder darunter)
// @param wholeChain - ganze Kette per Lstat prüfen (nach dem Einlesen)
// @return bool - true = unverändert
// @date   2026-10-22 06:05
func (d *deleteRun) unchanged(top, dir string, wholeChain bool) bool {
	from := dir
	if wholeChain || d.base == "" {
		from = top
	}
	// Das Fensterverzeichnis selbst darf ein Symlink sein (z. B. verlinktes
	// Home); es wird mit dem Zustand beim Start verglichen (ohne RealPath)
	// bzw. über den RealPath-Vergleich geprüft
	if path.Clean(dir) != path.Clean(d.req.dir) && !d.realDirectories(from, dir) {
		return false
	}
	if d.base == "" {
		return d.panelDirUnchanged()
	}
	real, err := d.req.fs.RealPath(dir)
	if err != nil {
		return false
	}
	rel := strings.TrimPrefix(path.Clean(dir), path.Clean(d.req.dir))
	return path.Clean(real) == path.Join(d.base, rel)
}

// panelDirState liest Art und ggf. Linkziel des Fensterverzeichnisses.
//
// @return os.FileMode - Art (Verzeichnis, Symlink …)
// @return string - Linkziel (nur bei Symlinks)
// @return bool - false wenn nicht lesbar
// @date   2026-10-22 06:20
func (d *deleteRun) panelDirState() (os.FileMode, string, bool) {
	info, err := d.req.fs.Lstat(d.req.dir)
	if err != nil {
		return 0, "", false
	}
	kind := info.Mode().Type()
	if kind&os.ModeSymlink == 0 {
		return kind, "", true
	}
	target, err := d.req.fs.ReadLink(d.req.dir)
	if err != nil {
		return 0, "", false
	}
	return kind, target, true
}

// panelDirUnchanged prüft (Server ohne RealPath), ob das Fensterverzeichnis
// noch genauso aussieht wie beim Start: gleiche Art, bei Symlinks gleiches
// Ziel.
//
// @return bool - true = unverändert
// @date   2026-10-22 06:20
func (d *deleteRun) panelDirUnchanged() bool {
	if !d.dirKnown {
		return false
	}
	kind, target, ok := d.panelDirState()
	return ok && kind == d.dirType && target == d.dirLink
}

// realDirectories prüft, ob p und alle Verzeichnisse von top bis p echte
// Verzeichnisse sind (Lstat: kein Symlink, nicht verschwunden).
//
// @param top - oberstes zu prüfendes Verzeichnis
// @param p - unterstes zu prüfendes Verzeichnis (top oder darunter)
// @return bool - true wenn alle echte Verzeichnisse sind
// @date   2026-10-22 05:55
func (d *deleteRun) realDirectories(top, p string) bool {
	for {
		info, err := d.req.fs.Lstat(p)
		if err != nil || !info.IsDir() {
			return false
		}
		if p == top || !insideDir(top, p) {
			return true
		}
		p = path.Dir(p)
	}
}

// sendProgressLocked meldet den Fortschritt höchstens alle
// copyProgressInterval (force: sofort); d.mu ist gesperrt. Ist der Kanal
// voll, wird die Meldung verworfen (nie blockierend, die Sperre bleibt kurz).
//
// @param force - ohne Zeitsperre melden
// @date   2026-11-13 09:08
func (d *deleteRun) sendProgressLocked(force bool) {
	if !force && time.Since(d.lastSent) < copyProgressInterval {
		return
	}
	d.lastSent = time.Now()
	snapshot := d.progress
	select {
	case d.events <- deleteEvent{progress: &snapshot}:
	default:
	}
}

// deleteHasDirectory prüft, ob unter den Namen ein echtes Verzeichnis ist
// (für den Hinweis "samt Inhalt" in der Bestätigung).
//
// @param entries - Einträge des Fensters
// @param names - Gewählte Namen
// @return bool - true wenn mindestens ein Verzeichnis (kein Symlink) dabei ist
// @date   2026-10-22 05:17
func deleteHasDirectory(entries []fmEntry, names []string) bool {
	chosen := map[string]bool{}
	for _, name := range names {
		chosen[name] = true
	}
	for _, e := range entries {
		if chosen[e.name] && e.dir && !e.symlink && !e.parent {
			return true
		}
	}
	return false
}
