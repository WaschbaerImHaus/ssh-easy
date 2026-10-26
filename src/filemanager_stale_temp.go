// Paket main - liegen gebliebene Temp-Dateien aufräumen (F5/F6, Build 82)
//
// Jede Datei wird erst als ".ssh-easy-copy-<16 hex>.tmp" geschrieben und
// dann umbenannt (copyData). Reißt die Verbindung mitten in einer Datei
// ab, lässt sich diese Temp-Datei nicht mehr löschen und bleibt am Ziel
// liegen (Nutzerfall 2026-10-24: 623 MB). Kopiert man später wieder in
// dieses Verzeichnis, merkt listTarget eigene Temp-Dateien vor, die seit
// copyStaleTempAge nicht mehr geändert wurden; removeStaleTemps löscht sie
// am Ende des Laufs.
//
// Warum das sicher ist (Code-Review und Sicherheitsaudit Build 82):
//   - Nur genau das eigene Namensmuster und nur reguläre Dateien - kein
//     Verzeichnis, kein Symlink (Remove entfernt ohnehin nur den Link).
//     Die Prüfdatei beim Verschieben heißt anders (tempProbeName).
//   - Eine laufende Kopie einer zweiten Instanz ist geschützt:
//     - 24 Stunden Mindestalter: auch eine falsch gehende Uhr (Dual-Boot,
//       Server-Uhr) reicht nicht, um eine laufende Datei alt aussehen zu
//       lassen.
//     - Auf einem Server nur Rechte genau 0600: So bleibt eine Datei nach
//       einem Abbruch liegen. Kurz vor dem Umbenennen hat eine fertige
//       Datei schon die echten Rechte und die alte Zeit der Quelle.
//     - Gelöscht wird erst am Ende des Laufs und nur, wenn Größe und Zeit
//       sich seit dem Vormerken nicht geändert haben (eine umbenannte
//       Datei ist dann weg, eine wachsende hat sich verändert).
//   - Höchstens copyMaxStaleTemps Versuche pro Lauf, ein Verzeichnis ohne
//     Löschrecht wird nach dem ersten Versuch aufgegeben, Esc hält an.
//   - Nur in Verzeichnissen, die der Lauf ohnehin liest: nie in einem neu
//     angelegten (leer) und nie unter einem gescheiterten
//     Elternverzeichnis (dort kann ein fremder Ordner liegen).
//   - Jede gelöschte Datei steht im Log.
//
// @author Kurt Ingwer
// @date   2026-10-26 21:15

package main

import (
	"errors"
	"os"
	"path"
	"strings"
	"time"
)

const (
	// copyStaleTempAge - so lange unverändert gilt eine eigene Temp-Datei
	// als liegen geblieben
	copyStaleTempAge = 24 * time.Hour
	// copyMaxStaleTemps - höchstens so viele werden pro Lauf vorgemerkt und
	// zu löschen versucht (jede kostet Rundreisen; ein bösartiger Server
	// könnte eine Million solcher Namen melden)
	copyMaxStaleTemps = 100
)

// staleTemp ist eine vorgemerkte liegen gebliebene Temp-Datei.
type staleTemp struct {
	path  string
	size  int64
	mtime time.Time
}

// isStaleCopyTemp meldet, ob ein Eintrag eine liegen gebliebene eigene
// Temp-Datei ist: Name genau "<copyTempPrefix><16 Zeichen 0-9a-f>.tmp"
// (wie tempCopyName), reguläre Datei, seit copyStaleTempAge unverändert.
// Eine Zeit in der Zukunft gilt nicht als alt.
//
// @param info - Eintrag (Lstat-Angaben, Symlinks nicht aufgelöst)
// @param now - aktuelle Zeit
// @param privateOnly - nur Rechte genau 0600 (Server; der lokale PC unter
// Windows kennt keine Unix-Rechte)
// @return bool - true wenn entfernt werden darf
// @date   2026-10-26 21:15
func isStaleCopyTemp(info os.FileInfo, now time.Time, privateOnly bool) bool {
	if !info.Mode().IsRegular() || (privateOnly && info.Mode().Perm() != 0o600) {
		return false
	}
	random, ok := strings.CutPrefix(info.Name(), copyTempPrefix)
	if !ok {
		return false
	}
	random, ok = strings.CutSuffix(random, ".tmp")
	if !ok || len(random) != 16 {
		return false
	}
	for i := 0; i < len(random); i++ {
		if c := random[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return now.Sub(info.ModTime()) >= copyStaleTempAge
}

// noteStaleTemps merkt liegen gebliebene Temp-Dateien aus einem gerade
// gelesenen Zielverzeichnis vor (höchstens copyMaxStaleTemps pro Lauf).
// Nur aus dispatch (listTarget).
//
// @param dir - Zielverzeichnis
// @param infos - seine Einträge (ReadDir)
// @date   2026-10-26 21:15
func (r *copyRun) noteStaleTemps(dir string, infos []os.FileInfo) {
	now := time.Now()
	private := !isLocalFS(r.req.dst)
	for _, info := range infos {
		if len(r.staleTemps) >= copyMaxStaleTemps {
			return
		}
		if isStaleCopyTemp(info, now, private) {
			r.staleTemps = append(r.staleTemps, staleTemp{
				path:  path.Join(dir, info.Name()),
				size:  info.Size(),
				mtime: info.ModTime(),
			})
		}
	}
}

// removeStaleTemps löscht am Ende des Laufs die vorgemerkten Temp-Dateien,
// sofern sie noch genauso aussehen wie beim Vormerken. Fehler werden
// ignoriert (dann bleibt die Datei eben liegen); ohne Löschrecht wird das
// Verzeichnis aufgegeben. Erst aufrufen, wenn alle Arbeiter fertig sind.
//
// @date   2026-10-26 21:15
func (r *copyRun) removeStaleTemps() {
	now := time.Now()
	private := !isLocalFS(r.req.dst)
	denied := map[string]bool{}
	for _, c := range r.staleTemps {
		if r.ctx.Err() != nil {
			return
		}
		dir := path.Dir(c.path)
		if denied[dir] {
			continue
		}
		info, err := r.req.dst.Lstat(c.path)
		if err != nil || !isStaleCopyTemp(info, now, private) || info.Size() != c.size || !info.ModTime().Equal(c.mtime) {
			continue
		}
		if err := r.req.dst.Remove(c.path); err != nil {
			if errors.Is(err, os.ErrPermission) {
				denied[dir] = true
			}
			continue
		}
		if r.req.logf != nil {
			r.req.logf("Datei-Manager: liegen gebliebene Temp-Datei entfernt (%d Bytes): %s", c.size, sanitizeFileName(c.path))
		}
	}
}
