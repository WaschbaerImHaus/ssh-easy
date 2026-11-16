// Paket main - Datei-Manager (MC-Modus): Verschieben zwischen zwei Servern
//
// Verschieben (F6) ist Kopieren mit anschließendem Löschen der Quelle. Es
// nutzt runCopy (filemanager_copy.go) mit copyRequest.move; hier liegen
// die Teile, die nur beim Verschieben gebraucht werden:
//   - Eine Datei bzw. ein Symlink wird in der Quelle erst gelöscht, wenn
//     die Kopie am Ziel vollständig angekommen und umbenannt ist (Temp-Datei
//   - Umbenennen). Scheitert etwas, bleibt die Quelle unangetastet.
//   - Verzeichnisse werden am Ende von innen nach außen gelöscht - aber nur,
//     wenn sie leer geworden sind. Ist noch etwas darin (übersprungen oder
//     fehlgeschlagen), bleibt das Verzeichnis stehen, ohne eigene Meldung.
//   - Schutz vor Datenverlust: Zeigt das Ziel auf die Quelle selbst (zwei
//     Verbindungseinträge zum selben Server, andere Hostnamen), würde das
//     Löschen der Quelle die einzige Kopie vernichten. Deshalb wird bei
//     jedem schon vorhandenen Ziel geprüft, ob Quell- und Zielverzeichnis
//     dasselbe sind (sameEntry). Ein Verzeichnis lässt sich außerdem nicht
//     in sich selbst verschieben (movesIntoItself).
//   - Umbenennen statt Kopieren (Build 90, renameMoved): Nutzen beide
//     Fenster dieselbe Verbindung (gleicher Eintrag = derselbe Benutzer),
//     wird jeder oberste Eintrag, dessen Ziel noch nicht existiert, auf dem
//     Server in einem Schritt umbenannt - sofort fertig, Besitzer und
//     Rechte bleiben wie sie sind. Bei verschiedenen Benutzern auf demselben
//     Server wird weiter kopiert (sonst gehörten die Dateien dem falschen
//     Benutzer). Umbenannt wird nur ohne Ersetzen (RenameNoReplace); jeder
//     Fehler führt still zum bisherigen Kopieren.
//   - Leere Quellverzeichnisse werden ebenenweise gleichzeitig gelöscht,
//     tiefste Ebene zuerst (removeMovedDirs, Build 90), nur per RemoveDir
//     (rmdir): eine Datei fällt dabei nie, auch wenn eine Liste sie für ein
//     Verzeichnis hielt.
//
// @author Kurt Ingwer
// @date   2026-11-15 15:03
package main

import (
	"errors"
	"os"
	"path"
	"sync"
)

// noReplaceRenamer ist ein Dateisystem, das umbenennen kann, ohne ein
// vorhandenes Ziel zu ersetzen (sftpFS: SSH_FXP_RENAME; localFS:
// renameat2 RENAME_NOREPLACE bzw. MoveFileEx ohne Ersetzen).
type noReplaceRenamer interface {
	// RenameNoReplace benennt oldPath in newPath um; existiert newPath
	// schon, schlägt es fehl, statt es zu ersetzen
	RenameNoReplace(oldPath, newPath string) error
}

// errMoveIntoItself - ein Verzeichnis soll in sich selbst bzw. in eines
// seiner Unterverzeichnisse verschoben werden.
var errMoveIntoItself = errors.New("cannot move a directory into itself")

// movesIntoItself prüft (nur beim Verschieben auf demselben Server), ob
// das Zielverzeichnis der Eintrag selbst ist oder in ihm liegt.
//
// @param name - Oberster Eintrag (Name in srcDir)
// @return bool - true = ablehnen
// @date   2026-10-22 05:17
func (r *copyRun) movesIntoItself(name string) bool {
	if !r.req.move || !r.req.sameHost {
		return false
	}
	srcPath := path.Join(r.req.srcDir, name)
	dstDir := path.Clean(r.req.dstDir)
	return dstDir == srcPath || insideDir(srcPath, dstDir)
}

// sameEntry prüft, ob ein vorhandenes Ziel in Wahrheit die Quelle selbst
// ist. Weil Quelle und Ziel denselben Namen tragen, genügt die Frage, ob
// beide Elternverzeichnisse dasselbe Verzeichnis sind: Im Zielverzeichnis
// wird kurz eine leere Prüfdatei (exklusiver Temp-Name) angelegt und
// nachgesehen, ob sie über die Quellverbindung im Quellverzeichnis
// auftaucht. Das Ergebnis gilt für das Verzeichnispaar und wird gemerkt.
//
// Im Zweifel sperren (Sicherheitsaudit Build 82): Taucht die Prüfdatei in
// der Quelle nicht auf, muss sie am Ziel danach noch da sein - sonst hat
// jemand sie zwischendurch gelöscht (fremder Aufräumer), und "nicht
// gefunden" hieße nichts. Dann wird nicht verschoben, statt womöglich die
// einzige Kopie zu löschen. Die Prüfdatei heißt anders als Temp-Dateien
// (tempProbeName), damit das Aufräumen alter Temp-Dateien sie nie erfasst.
//
// @param srcPath - Pfad in der Quelle
// @param dstPath - Pfad am Ziel (existiert)
// @return bool - true = dasselbe Verzeichnis, also dieselbe Datei
// @return error - Prüfdatei nicht anlegbar oder verschwunden (dann nicht
// verschieben)
// @date   2026-10-26 21:15
func (r *copyRun) sameEntry(srcPath, dstPath string) (bool, error) {
	srcDir, dstDir := path.Dir(srcPath), path.Dir(dstPath)
	key := srcDir + "\x00" + dstDir
	if same, ok := r.sameDirs[key]; ok {
		return same, nil
	}
	probe := tempProbeName(dstPath)
	writer, err := r.req.dst.CreateExclusive(probe)
	if err != nil {
		return false, err
	}
	writer.Close()
	_, statErr := r.req.src.Lstat(path.Join(srcDir, path.Base(probe)))
	if statErr != nil {
		// Existiert sie am Ziel noch, gab es sie auch während der Abfrage
		// über die Quelle (zufälliger Name) - die Verzeichnisse sind verschieden
		if _, err := r.req.dst.Lstat(probe); err != nil {
			return false, errMoveCheckFailed
		}
	}
	_ = r.req.dst.Remove(probe)
	same := statErr == nil
	r.sameDirs[key] = same
	return same, nil
}

// removeMovedSource löscht beim Verschieben eine vollständig kopierte
// Datei bzw. einen Symlink in der Quelle (der Link selbst, nie sein Ziel).
//
// @param node - Eintrag
// @param srcPath - Pfad in der Quelle
// @date   2026-10-22 05:17
func (r *copyRun) removeMovedSource(node copyNode, srcPath string) {
	if !r.req.move {
		return
	}
	if err := r.req.src.Remove(srcPath); err != nil {
		r.fail(node, srcPath, err)
	}
}

// rememberMovedDir merkt beim Verschieben ein Verzeichnis, das am Ziel
// angelegt oder mit einem vorhandenen zusammengeführt wurde.
//
// @param node - Verzeichnis
// @date   2026-10-22 05:17
func (r *copyRun) rememberMovedDir(node copyNode) {
	if r.req.move {
		r.movedDirs = append(r.movedDirs, node)
	}
}

// removeMovedDirs löscht die Quellverzeichnisse von innen nach außen:
// ebenenweise, tiefste Ebene zuerst, alle Verzeichnisse einer Ebene
// gleichzeitig (forEachParallel) - bei zehntausenden leeren
// Verzeichnissen sonst eine Rundreise nach der anderen. Ein Verzeichnis,
// in dem noch etwas liegt (übersprungen, fehlgeschlagen), bleibt ohne
// eigene Meldung stehen; scheitert das Löschen eines leeren Verzeichnisses,
// ist das ein Fehler. Der Fortschritt zeigt diese Phase eigens an
// (removingDirs). Abbruch: übrige Verzeichnisse bleiben stehen, der
// Vorgang gilt als abgebrochen.
//
// @date   2026-11-13 09:08
func (r *copyRun) removeMovedDirs() {
	levels := map[int][]copyNode{}
	deepest, total := 0, 0
	for _, node := range r.movedDirs {
		if !r.dirJobs[node.rel].succeeded() {
			// Am Ziel nicht angelegt: Quelle bleibt (auch wenn sie leer ist)
			continue
		}
		depth := scanDepth(node.rel)
		levels[depth] = append(levels[depth], node)
		deepest = max(deepest, depth)
		total++
	}
	if total == 0 {
		return
	}
	r.mu.Lock()
	r.progress.removingDirs, r.progress.removedDirs, r.progress.removeTotal = true, 0, total
	r.sendProgressLocked(true)
	r.mu.Unlock()
	for depth := deepest; depth >= 0; depth-- {
		forEachParallel(levels[depth], r.removeMovedDir)
	}
	r.mu.Lock()
	if r.ctx.Err() != nil {
		r.result.cancelled = true
	}
	r.sendProgressLocked(true)
	r.mu.Unlock()
}

// removeMovedDir löscht ein leer gewordenes Quellverzeichnis (läuft
// gleichzeitig mit den anderen Verzeichnissen seiner Ebene). Gelöscht wird
// nur per RemoveDir (rmdir): nie eine Datei oder ein Symlink (Audit
// Build 90).
//
// @param node - Verzeichnis
// @date   2026-11-15 09:09
func (r *copyRun) removeMovedDir(node copyNode) {
	defer func() {
		r.mu.Lock()
		r.progress.removedDirs++
		r.sendProgressLocked(false)
		r.mu.Unlock()
	}()
	if r.ctx.Err() != nil {
		// Abbruch: stehen lassen
		r.markIncomplete(node.top)
		return
	}
	srcPath := path.Join(r.req.srcDir, node.rel)
	// Nur rmdir, nie Remove: hielt das Erfassen (find-Schnellweg) eine
	// Datei fälschlich für ein Verzeichnis, bleibt sie so erhalten - Remove
	// hätte sie als Datei gelöscht, ohne dass sie kopiert war
	if err := r.req.src.RemoveDir(srcPath); err != nil {
		if children, readErr := r.req.src.ReadDir(srcPath); readErr == nil && len(children) > 0 {
			// Absichtlich nicht leer: oberster Eintrag ist unvollständig
			r.markIncomplete(node.top)
			return
		}
		r.fail(node, srcPath, err)
	}
}

// renameMoved verschiebt beim Verschieben auf demselben Konto oberste
// Einträge per Umbenennen auf dem Server (gleichzeitig, bis copyParallel).
// Voraussetzungen, sonst passiert hier nichts und es wird kopiert:
//   - Verschieben, gleicher Server UND gleiche Verbindung (sameAccount);
//     nie vom Server auf den PC (markDownloads/safeLinksOnly)
//   - die Quelle kann ohne Ersetzen umbenennen (noReplaceRenamer)
//   - die Quellverbindung sieht das Zielverzeichnis wirklich
//     (renameReachesTarget, Prüfdatei)
//
// Pro Eintrag zusätzlich: sicherer Name, nicht in sich selbst, Ziel im
// Zielverzeichnis und per Lstat NICHT vorhanden. Ein vorhandenes Ziel geht
// den bisherigen Weg (Rückfragen, Zusammenführen, sameEntry). Scheitert das
// Umbenennen (anderes Dateisystem, Rechte, Server kann es nicht), wird
// still kopiert - die Quelle ist dann unverändert.
//
// @date   2026-11-15 15:03
func (r *copyRun) renameMoved() {
	req := r.req
	if !req.move || !req.sameHost || !req.sameAccount || req.markDownloads || req.safeLinksOnly {
		return
	}
	renamer, ok := req.src.(noReplaceRenamer)
	if !ok || r.ctx.Err() != nil || !r.renameReachesTarget() {
		return
	}
	known := r.sourceInfos()
	work := make(chan string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for w := 0; w < min(max(copyParallel, 1), len(req.names)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range work {
				_, done := r.renameOne(renamer, name, known[name])
				if !done {
					continue
				}
				mu.Lock()
				r.renamed[name] = true
				mu.Unlock()
				// Nicht in files/bytes: übertragen wurde nichts. Die Meldung
				// nennt dann die obersten Einträge (fmFinishCopy)
				r.mu.Lock()
				r.result.renamed++
				r.mu.Unlock()
			}
		}()
	}
	for _, name := range req.names {
		work <- name
	}
	close(work)
	wg.Wait()
}

// renameOne benennt einen obersten Eintrag um, wenn alle Prüfungen passen.
//
// @param renamer - Quelle (kann ohne Ersetzen umbenennen)
// @param name - oberster Eintrag
// @param info - Angaben aus der Liste des Quellverzeichnisses (nil = Lstat)
// @return os.FileInfo - Angaben zum Eintrag (für Größe; evtl. nil)
// @return bool - true = umbenannt, sonst unverändert (dann kopieren)
// @date   2026-11-13 09:08
func (r *copyRun) renameOne(renamer noReplaceRenamer, name string, info os.FileInfo) (os.FileInfo, bool) {
	req := r.req
	if r.ctx.Err() != nil || !safeCopyName(name) || !fsAcceptsName(req.dst, name) || r.movesIntoItself(name) {
		return nil, false
	}
	srcPath, dstPath := path.Join(req.srcDir, name), path.Join(req.dstDir, name)
	if !insideDir(req.dstDir, dstPath) || path.Clean(srcPath) == path.Clean(dstPath) {
		return nil, false
	}
	if info == nil {
		var err error
		if info, err = req.src.Lstat(srcPath); err != nil {
			// Fehler meldet der Kopierweg
			return nil, false
		}
	}
	if _, err := req.dst.Lstat(dstPath); !errors.Is(err, os.ErrNotExist) {
		// Ziel vorhanden (oder unklar): bisheriger Weg mit Rückfragen
		return nil, false
	}
	if err := renamer.RenameNoReplace(srcPath, dstPath); err != nil {
		return nil, false
	}
	return info, true
}

// renameReachesTarget prüft einmal pro Lauf, ob die Quellverbindung das
// Zielverzeichnis des anderen Fensters unter demselben Pfad sieht: Über das
// Ziel wird eine leere Prüfdatei angelegt und über die Quelle gesucht.
// Gleiche Verbindung heißt nicht zwingend gleicher Rechner (z. B. ein
// Name, der auf mehrere Server verteilt). Im Zweifel nein (dann kopieren).
//
// @return bool - true = Umbenennen erreicht das Zielverzeichnis
// @date   2026-11-13 09:08
func (r *copyRun) renameReachesTarget() bool {
	probe := tempProbeName(path.Join(r.req.dstDir, "x"))
	writer, err := r.req.dst.CreateExclusive(probe)
	if err != nil {
		return false
	}
	writer.Close()
	defer r.req.dst.Remove(probe)
	_, err = r.req.src.Lstat(probe)
	return err == nil
}
