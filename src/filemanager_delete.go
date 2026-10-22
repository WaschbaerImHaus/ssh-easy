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
// Die Funktion kennt keine Oberfläche: Sie meldet Fortschritt und das
// Ergebnis als deleteEvent.
//
// @author Kurt Ingwer
// @date   2026-10-22 06:05
package main

import (
	"context"
	"errors"
	"path"
	"sort"
	"strings"
	"time"
)

// errDeleteRace - ein Verzeichnis wurde während des Löschens ersetzt (z. B.
// durch einen Symlink); darin wird nichts mehr gelöscht.
var errDeleteRace = errors.New("directory was replaced during deletion")

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
	ctx      context.Context
	req      deleteRequest
	events   chan<- deleteEvent
	progress deleteProgress
	lastSent time.Time
	result   deleteResult
	// Besuchte Einträge (Grenze copyMaxEntries)
	visited int
	// Grenze erreicht: nichts weiter löschen
	limitHit bool
	// Kanonischer Pfad von req.dir (leer = Server kann kein RealPath)
	base string
}

// runDelete führt einen Löschauftrag aus und meldet am Ende genau ein
// deleteEvent mit result. Abbruch über ctx.
//
// @param ctx - Abbruch-Kontext
// @param req - Auftrag
// @param events - Kanal für Fortschritt und Ergebnis
// @date   2026-10-22 05:17
func runDelete(ctx context.Context, req deleteRequest, events chan<- deleteEvent) {
	run := &deleteRun{ctx: ctx, req: req, events: events}
	if real, err := req.fs.RealPath(req.dir); err == nil && path.IsAbs(real) {
		run.base = path.Clean(real)
	}
	for _, name := range req.names {
		if run.stopped() {
			break
		}
		p := path.Join(req.dir, name)
		if !safeCopyName(name) {
			// Nicht path.Join: das würde ".." auflösen
			run.fail(req.dir+"/"+name, errUnsafeName)
			continue
		}
		if run.remove(p, p, 0) {
			run.result.deletedNames = append(run.result.deletedNames, name)
		}
	}
	run.result.cancelled = ctx.Err() != nil
	run.result.deleted = run.progress.done
	result := run.result
	events <- deleteEvent{result: &result}
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

// remove löscht einen Eintrag; Verzeichnisse erst ihren Inhalt.
//
// @param p - Pfad
// @param top - Pfad des gewählten obersten Eintrags
// @param depth - Verschachtelungstiefe unter dem obersten Eintrag
// @return bool - true = vollständig gelöscht
// @date   2026-10-22 05:55
func (d *deleteRun) remove(p, top string, depth int) bool {
	if d.stopped() {
		return false
	}
	d.visited++
	if d.visited > copyMaxEntries || depth >= copyMaxDepth {
		d.limitHit = true
		d.fail(p, errCopyLimit)
		return false
	}
	d.progress.current = p
	d.sendProgress(false)

	// Lstat: ein Symlink ist nie ein Verzeichnis - gelöscht wird der Link
	info, err := d.req.fs.Lstat(p)
	if err != nil {
		d.fail(p, err)
		return false
	}
	if info.IsDir() {
		children, err := d.req.fs.ReadDir(p)
		if err != nil {
			d.fail(p, err)
			return false
		}
		// Seit dem Lstat ausgetauscht? Ganze Kette bis hierher prüfen
		if !d.unchanged(top, p, true) {
			d.fail(p, errDeleteRace)
			return false
		}
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		complete := true
		for _, child := range children {
			name := child.Name()
			if name == "." || name == ".." {
				continue
			}
			if !safeCopyName(name) {
				d.fail(p+"/"+name, errUnsafeName)
				complete = false
				continue
			}
			if !d.remove(path.Join(p, name), top, depth+1) {
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
	if !d.unchanged(top, path.Dir(p), false) {
		d.fail(path.Dir(p), errDeleteRace)
		return false
	}
	if err := d.req.fs.Remove(p); err != nil {
		d.fail(p, err)
		return false
	}
	d.progress.done++
	d.sendProgress(false)
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
	// Home); dafür gilt nur der RealPath-Vergleich
	if path.Clean(dir) != path.Clean(d.req.dir) && !d.realDirectories(from, dir) {
		return false
	}
	if d.base == "" {
		return true
	}
	real, err := d.req.fs.RealPath(dir)
	if err != nil {
		return false
	}
	rel := strings.TrimPrefix(path.Clean(dir), path.Clean(d.req.dir))
	return path.Clean(real) == path.Join(d.base, rel)
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

// sendProgress meldet den Fortschritt höchstens alle copyProgressInterval
// (force: sofort). Ist der Kanal voll, wird die Meldung verworfen.
//
// @param force - ohne Zeitsperre melden
// @date   2026-10-22 05:17
func (d *deleteRun) sendProgress(force bool) {
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
