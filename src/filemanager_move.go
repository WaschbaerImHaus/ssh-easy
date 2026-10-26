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
//
// @author Kurt Ingwer
// @date   2026-10-22 05:17
package main

import (
	"errors"
	"path"
)

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

// removeMovedDirs löscht die Quellverzeichnisse von innen nach außen. Ein
// Verzeichnis, in dem noch etwas liegt (übersprungen, fehlgeschlagen),
// bleibt ohne eigene Meldung stehen; scheitert das Löschen eines leeren
// Verzeichnisses, ist das ein Fehler.
//
// @date   2026-10-22 05:17
func (r *copyRun) removeMovedDirs() {
	for i := len(r.movedDirs) - 1; i >= 0; i-- {
		node := r.movedDirs[i]
		if !r.dirJobs[node.rel].succeeded() {
			// Am Ziel nicht angelegt: Quelle bleibt (auch wenn sie leer ist)
			continue
		}
		srcPath := path.Join(r.req.srcDir, node.rel)
		if err := r.req.src.Remove(srcPath); err != nil {
			if children, readErr := r.req.src.ReadDir(srcPath); readErr == nil && len(children) > 0 {
				// Absichtlich nicht leer: oberster Eintrag ist unvollständig
				r.markIncomplete(node.top)
				continue
			}
			r.fail(node, srcPath, err)
		}
	}
}
