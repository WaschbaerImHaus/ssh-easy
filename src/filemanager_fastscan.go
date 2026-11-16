// Paket main - Datei-Manager (MC-Modus): Schnellweg beim Erfassen per find
//
// Auch gleichzeitig per SFTP braucht das Erfassen eines Baums mit 30.000
// Verzeichnissen pro Verzeichnis eine Rundreise (Nutzerbericht Build 90:
// "die Dateiauflistung sollte viel schneller möglich sein"). Ist die Quelle
// ein Server, liest ssh-easy deshalb zuerst den ganzen Baum mit EINEM
// find-Befehl über einen eigenen Exec-Kanal derselben SSH-Verbindung:
//
//	sh -c 'LC_ALL=C exec xargs -0 -I"{}" find -P "{}" -mindepth 0 -maxdepth N -printf "%y\0%m\0%s\0%T@\0%H\0%P\0"'
//
// Sicherheit:
//   - Der Befehl ist ein fester Text ohne Nutzerdaten (nur N ist eine von
//     ssh-easy formatierte Zahl). Die Startpfade kommen NUL-getrennt über
//     stdin, nicht über die Befehlszeile - so gibt es kein Quoting, das unter
//     sh/bash/BusyBox/fish/csh als Login-Shell unterschiedlich wirkt (fish
//     halbiert Backslash-Paare in '…', csh kennt keine Zeilenumbrüche in '…').
//     Der Text enthält weder "!", "\\", "\'" noch Zeilenumbrüche.
//   - -P: Symlinks werden nie verfolgt. NUL-getrennte Felder: Namen mit
//     Zeilenumbrüchen sind sicher.
//   - Die Ausgabe ist nicht vertrauenswürdig: Feldlängen, Gesamtgröße,
//     Eintragszahl, Tiefe und Laufzeit sind begrenzt, ctx (Esc) bricht ab.
//     Jedes Feld wird geprüft (Typzeichen, Oktalrechte, Zahlen, Startpfad,
//     relative Pfade ohne "", "." und ".."); ein ungültiges Feld verwirft den
//     ganzen Schnellweg. Jeder Name läuft durch safeCopyName/fsAcceptsName
//     wie beim SFTP-Weg (unsichere Namen werden gemeldet, ihr Inhalt nie
//     erfasst).
//   - Gleiches Dateisystem wie SFTP? Startpfade sind per SFTP-RealPath
//     kanonisiert; jedes markierte Verzeichnis muss in find dieselben
//     Angaben haben wie per SFTP (Typ, Rechte, Größe, Änderungszeit,
//     sameAsSFTP). Sonst (chroot, ForceCommand, anderer Rechner) SFTP-Weg.
//   - Rückfall auf das gleichzeitige SFTP-Erfassen bei: kein Exec erlaubt,
//     kein find mit -printf (BusyBox), Exit-Code ungleich 0, Ausgabe auf
//     stderr (z. B. "Permission denied"), Zeitlimit, Größenlimit.
//   - Danach vergleicht eine kleine Stichprobe (Lstat und ReadDir per SFTP,
//     filemanager_fastscan_sample.go) Einträge darunter; jede Abweichung
//     führt zum SFTP-Weg.
//   - Esc und Zeitgrenzen wirken sofort, auch wenn der Server den Kanal
//     nicht schließt (runTreeList liest in einer eigenen Goroutine).
//   - Die find-Daten ersetzen nur das Erfassen. Die Kopierphase prüft weiter
//     selbst (Lstat am Ziel, Temp-Datei, Umbenennen, sameEntry …).
//
// Der Lokale PC nutzt den Schnellweg nie (localFS kann kein ListTree).
//
// @author Kurt Ingwer
// @date   2026-11-16 12:13
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// treeLister ist ein Dateisystem, das einen ganzen Baum mit einem Befehl
// auf dem Server lesen kann (sftpFS über einen Exec-Kanal).
type treeLister interface {
	// ListTree startet find für die Startpfade (absolut, kanonisch) bis zur
	// Tiefe maxDepth. Liefert die Standardausgabe (Close bricht ab) und
	// wait: wartet auf das Ende, Fehler bei Exit-Code ungleich 0 oder
	// Ausgabe auf stderr.
	ListTree(ctx context.Context, starts []string, maxDepth int) (io.ReadCloser, func() error, error)
}

// Grenzen des Schnellwegs (Variablen, damit Tests sie verkleinern können)
var (
	// fastScanEnabled - Schnellweg überhaupt versuchen
	fastScanEnabled = true
	// fastScanMaxBytes - höchstens so viel Ausgabe wird gelesen
	fastScanMaxBytes int64 = 256 << 20
	// fastScanTimeout - längste Laufzeit des find-Befehls
	fastScanTimeout = 10 * time.Minute
	// fastScanIdleTimeout - so lange darf find ohne jede Ausgabe bleiben
	// (z. B. ForceCommand internal-sftp: dann antwortet der sftp-server
	// statt find)
	fastScanIdleTimeout = 30 * time.Second
	// fastScanMaxStarts - höchstens so viele Startpfade (sonst SFTP-Weg)
	fastScanMaxStarts = 4096
)

// Feldlängen einer find-Zeile (Pfade: PATH_MAX-Reserve)
const (
	fastScanMaxShortField = 32
	fastScanMaxPathField  = 64 << 10
)

// errFastScanInvalid - Ausgabe von find ungültig oder passt nicht zu SFTP
var errFastScanInvalid = errors.New("tree listing invalid")

// errFastScanTooLarge - Ausgabe zu groß (Byte-Grenze)
var errFastScanTooLarge = errors.New("tree listing too large")

// errFastScanTooMany - mehr als copyMaxEntries Einträge
var errFastScanTooMany = errors.New("tree listing has too many entries")

// treeListCommand liefert den festen find-Befehl (siehe Dateikopf).
//
// @param maxDepth - größte Tiefe (Zahl, von ssh-easy formatiert)
// @return string - Befehl für den Exec-Kanal
// @date   2026-11-13 09:28
func treeListCommand(maxDepth int) string {
	return `sh -c 'LC_ALL=C exec xargs -0 -I"{}" find -P "{}" -mindepth 0 -maxdepth ` +
		strconv.Itoa(max(maxDepth, 0)) + ` -printf "%y\0%m\0%s\0%T@\0%H\0%P\0"'`
}

// treeRecord ist eine geprüfte Zeile der find-Ausgabe.
type treeRecord struct {
	// Startpfad (%H) und Pfad relativ dazu (%P, "" = der Startpfad selbst)
	start string
	rel   string
	// Angaben zum Eintrag
	info *treeFileInfo
}

// treeFileInfo sind die Angaben eines Eintrags aus find (wie Lstat).
type treeFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func (i *treeFileInfo) Name() string       { return i.name }
func (i *treeFileInfo) Size() int64        { return i.size }
func (i *treeFileInfo) Mode() os.FileMode  { return i.mode }
func (i *treeFileInfo) ModTime() time.Time { return i.modTime }
func (i *treeFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i *treeFileInfo) Sys() any           { return nil }

// treeTypeModes bildet die Typzeichen von find (%y) auf os.FileMode ab
// (wie pkg/sftp die Typen eines SFTP-Lstat abbildet).
var treeTypeModes = map[byte]os.FileMode{
	'f': 0,
	'd': os.ModeDir,
	'l': os.ModeSymlink,
	'p': os.ModeNamedPipe,
	's': os.ModeSocket,
	'c': os.ModeDevice | os.ModeCharDevice,
	'b': os.ModeDevice,
}

// readTreeField liest ein NUL-beendetes Feld mit Höchstlänge.
//
// @param r - Ausgabe
// @param limit - größte Feldlänge in Bytes
// @param budget - restliche Byte-Grenze (wird verringert)
// @return string - Feld ohne NUL
// @return error - io.EOF (sauberes Ende vor dem Feld), errFastScanInvalid, errFastScanTooLarge
// @date   2026-11-13 09:28
func readTreeField(r *bufio.Reader, limit int, budget *int64) (string, error) {
	var field []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			if err == io.EOF && len(field) > 0 {
				// Feld ohne abschließendes NUL: abgeschnittene Ausgabe
				return "", errFastScanInvalid
			}
			return "", err
		}
		*budget--
		if *budget < 0 {
			return "", errFastScanTooLarge
		}
		if b == 0 {
			return string(field), nil
		}
		if len(field) >= limit {
			return "", errFastScanInvalid
		}
		field = append(field, b)
	}
}

// parseTreeOutput liest die find-Ausgabe Zeile für Zeile (6 Felder) und
// prüft jedes Feld. Liefert die Einträge in der Reihenfolge von find.
//
// @param r - Ausgabe (nicht vertrauenswürdig)
// @param starts - erlaubte Startpfade
// @param maxEntries - größte Zahl von Einträgen
// @param maxBytes - größte Ausgabe in Bytes
// @return []treeRecord - Einträge
// @return error - errFastScanInvalid/-TooLarge/-TooMany oder Lesefehler
// @date   2026-11-13 09:28
func parseTreeOutput(r io.Reader, starts map[string]bool, maxEntries int, maxBytes int64) ([]treeRecord, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	budget := maxBytes
	var records []treeRecord
	for {
		typ, err := readTreeField(br, fastScanMaxShortField, &budget)
		if err == io.EOF {
			return records, nil
		}
		if err != nil {
			return nil, err
		}
		var fields [5]string
		limits := [5]int{fastScanMaxShortField, fastScanMaxShortField, fastScanMaxShortField, fastScanMaxPathField, fastScanMaxPathField}
		for i := range fields {
			if fields[i], err = readTreeField(br, limits[i], &budget); err != nil {
				if err == io.EOF {
					err = errFastScanInvalid
				}
				return nil, err
			}
		}
		if len(records) >= maxEntries {
			return nil, errFastScanTooMany
		}
		rec, ok := parseTreeRecord(typ, fields, starts)
		if !ok {
			return nil, errFastScanInvalid
		}
		records = append(records, rec)
	}
}

// parseTreeRecord prüft die Felder einer Zeile.
//
// @param typ - %y (ein Zeichen)
// @param f - %m, %s, %T@, %H, %P
// @param starts - erlaubte Startpfade
// @return treeRecord - geprüfter Eintrag
// @return bool - false = ungültig
// @date   2026-11-13 09:28
func parseTreeRecord(typ string, f [5]string, starts map[string]bool) (treeRecord, bool) {
	if len(typ) != 1 {
		return treeRecord{}, false
	}
	kind, ok := treeTypeModes[typ[0]]
	if !ok {
		return treeRecord{}, false
	}
	perm, ok := parseTreePerm(f[0])
	if !ok {
		return treeRecord{}, false
	}
	size, err := strconv.ParseInt(f[1], 10, 64)
	if err != nil || size < 0 || f[1] == "" || f[1][0] == '+' {
		return treeRecord{}, false
	}
	mtime, ok := parseTreeTime(f[2])
	if !ok {
		return treeRecord{}, false
	}
	start, rel := f[3], f[4]
	if !starts[start] || !validTreeRel(rel) {
		return treeRecord{}, false
	}
	name := path.Base(start)
	if rel != "" {
		name = path.Base(rel)
	}
	return treeRecord{start: start, rel: rel, info: &treeFileInfo{name: name, size: size, mode: kind | perm, modTime: mtime}}, true
}

// parseTreePerm liest die Rechte aus %m (oktal, höchstens 4 Stellen) und
// bildet setuid/setgid/sticky wie pkg/sftp ab.
//
// @param s - z. B. "755", "4755", "1777"
// @return os.FileMode - Rechte samt Sonderbits
// @return bool - false = ungültig
// @date   2026-11-13 09:28
func parseTreePerm(s string) (os.FileMode, bool) {
	if s == "" || len(s) > 4 {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil || v > 0o7777 {
		return 0, false
	}
	mode := os.FileMode(v & 0o777)
	if v&0o4000 != 0 {
		mode |= os.ModeSetuid
	}
	if v&0o2000 != 0 {
		mode |= os.ModeSetgid
	}
	if v&0o1000 != 0 {
		mode |= os.ModeSticky
	}
	return mode, true
}

// parseTreeTime liest %T@ ("Sekunden.Bruchteil", Bruchteil höchstens 10
// Stellen, ggf. mit Minus).
//
// @param s - z. B. "1699999999.1234567890"
// @return time.Time - Zeitpunkt
// @return bool - false = ungültig
// @date   2026-11-13 09:28
func parseTreeTime(s string) (time.Time, bool) {
	secText, fracText, _ := strings.Cut(s, ".")
	neg := strings.HasPrefix(secText, "-")
	digits := strings.TrimPrefix(secText, "-")
	if digits == "" || len(fracText) > 10 || strings.Trim(digits, "0123456789") != "" || strings.Trim(fracText, "0123456789") != "" {
		return time.Time{}, false
	}
	sec, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || sec > 1<<40 {
		return time.Time{}, false
	}
	// Bruchteil auf Nanosekunden (9 Stellen) bringen
	frac := (fracText + "000000000")[:9]
	nsec, _ := strconv.ParseInt(frac, 10, 64)
	if neg {
		sec, nsec = -sec, -nsec
	}
	return time.Unix(sec, nsec), true
}

// validTreeRel prüft %P: leer (Startpfad) oder ein relativer Pfad aus
// nicht leeren Teilen ohne "." und "..".
//
// @param rel - relativer Pfad
// @return bool - true wenn gültig
// @date   2026-11-13 09:28
func validTreeRel(rel string) bool {
	if rel == "" {
		return true
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// fastScan versucht das Erfassen der markierten Verzeichnisse per find
// (siehe Dateikopf). Gelingt es, sind deren scanEntry-Bäume vollständig
// gefüllt und es gibt nichts mehr per SFTP vorzulesen; sonst bleiben sie
// unverändert und prefetch übernimmt.
//
// @param pending - oberste Einträge (ggf. ohne Angaben)
// @return bool - true = erfasst (oder Grenze erreicht), false = SFTP-Weg
// @date   2026-11-16 11:37
func (r *copyRun) fastScan(pending []*scanEntry) bool {
	lister, ok := r.req.src.(treeLister)
	if !ok || r.ctx.Err() != nil {
		return false
	}
	if !fastScanEnabled {
		r.logScan("SFTP (Schnellweg abgeschaltet)")
		return false
	}
	if d, ok := lister.(fastScanSwitch); ok && d.fastScanDisabled() {
		r.logScan("SFTP (Schnellweg für diese Verbindung abgeschaltet)")
		return false
	}
	// Angaben der obersten Einträge (aus der Liste des Quellverzeichnisses
	// oder per Lstat) - nur echte Verzeichnisse lohnen den Schnellweg
	var dirs []*scanEntry
	for _, e := range pending {
		if !e.resolved {
			info, err := r.req.src.Lstat(path.Join(r.req.srcDir, e.rel))
			e.info, e.statErr, e.resolved = info, err, true
			if err == nil {
				r.countScanned(info)
			}
		}
		if e.statErr == nil && e.info.IsDir() {
			dirs = append(dirs, e)
		}
	}
	if len(dirs) == 0 || len(dirs) > fastScanMaxStarts || r.ctx.Err() != nil {
		return len(dirs) == 0
	}
	base, err := r.req.src.RealPath(r.req.srcDir)
	if err != nil || !path.IsAbs(base) || strings.ContainsRune(base, 0) {
		r.logScan("SFTP (realpath: %v)", err)
		return false
	}
	base = path.Clean(base)
	byStart := map[string]*scanEntry{}
	starts := make([]string, 0, len(dirs))
	for _, e := range dirs {
		start := path.Join(base, e.name)
		byStart[start] = e
		starts = append(starts, start)
	}
	began := time.Now()
	records, err := r.runTreeList(lister, starts)
	if errors.Is(err, errFastScanTooMany) {
		// Zu viele ZEILEN (Speichergrenze, unsichere Namen und ihr Inhalt
		// eingerechnet): nicht als Eintragsgrenze werten - die zählt nur
		// sichere Einträge (wie bis Build 89); der SFTP-Weg zählt genau
		err = fmt.Errorf("%w (Zeilen)", errFastScanTooLarge)
	}
	var trees map[*scanEntry]*fastTree
	if err == nil {
		trees, err = r.buildTrees(records, byStart)
	}
	if errors.Is(err, errConnectionLost) {
		// find hing und die Verbindung ist getrennt: kein SFTP-Rückfall über
		// die tote Verbindung, sondern wie eine Grenze nichts kopieren und
		// den Abbruch melden
		r.limitHit = true
		r.fail(copyNode{top: dirs[0].name}, path.Join(r.req.srcDir, dirs[0].name), err)
		return true
	}
	if errors.Is(err, errFastScanTooMany) {
		// Mehr Einträge als erlaubt: wie beim SFTP-Weg die Grenze melden
		r.limitHit = true
		r.fail(copyNode{top: dirs[0].name}, path.Join(r.req.srcDir, dirs[0].name), errCopyLimit)
		r.logScan("find: Eintragsgrenze erreicht")
		return true
	}
	if err != nil {
		r.logScan("SFTP (find: %v)", err)
		return false
	}
	if !r.sameAsSFTP(trees) {
		r.logScan("SFTP (find sieht anderes als SFTP)")
		return false
	}
	// Stichprobe darunter (filemanager_fastscan_sample.go): die obersten
	// Angaben allein sind nur eine Heuristik (Audit Build 90)
	if same, where := r.sampleMatchesSFTP(trees); !same {
		r.logScan("SFTP (Stichprobe weicht ab: %s)", where)
		return false
	}
	// Erst jetzt übernehmen: bis hierher bleiben die Einträge unverändert
	files, folders := 0, 0
	for e, tree := range trees {
		e.listed, e.children = true, tree.children
		f, d := countTree(tree.children)
		files, folders = files+f, folders+d
	}
	r.mu.Lock()
	r.progress.totalFiles += files
	r.progress.totalDirs += folders
	r.sendProgressLocked(true)
	r.mu.Unlock()
	r.logScan("find (%d Einträge in %v)", len(records), time.Since(began).Round(time.Millisecond))
	return true
}

// fastScanSwitch ist ein treeLister, dessen Schnellweg sich für die
// Dauer der Verbindung abschalten lässt (sftpFS): nach einem hängenden
// find-Kanal, einem Leerlauf- oder Zeitlimit kostete sonst jedes weitere
// F5/F6 wieder bis zu fastScanIdleTimeout + fastScanAbortGrace.
type fastScanSwitch interface {
	// disableFastScanForSession schaltet den Schnellweg ab
	disableFastScanForSession()
	// fastScanDisabled meldet, ob er abgeschaltet ist
	fastScanDisabled() bool
}

// disableFastScan schaltet den Schnellweg der Quelle ab (falls möglich)
// und schreibt das ins Protokoll.
//
// @param lister - Quelle
// @param reason - Grund (Zeitlimit, Leerlauf, Trennung)
// @date   2026-11-15 19:55
func (r *copyRun) disableFastScan(lister treeLister, reason error) {
	d, ok := lister.(fastScanSwitch)
	if !ok || d.fastScanDisabled() {
		return
	}
	d.disableFastScanForSession()
	r.logScan("SFTP ab jetzt für diese Verbindung (find: %v)", reason)
}

// inheritFastScanOff überträgt einen abgeschalteten Schnellweg von der alten
// auf die neue Verbindung desselben Fensters (Neu-Verbinden,
// tui_filemanager_reconnect.go). Nach einem hängenden find-Kanal trennt
// AbortTreeList die Verbindung; ohne Übernahme hinge das nächste F5/F6 über
// die neue Verbindung wieder fastScanIdleTimeout + fastScanAbortGrace lang
// und trennte erneut. Gilt nur innerhalb dieser Datei-Manager-Sitzung.
//
// @param oldFS - bisherige Verbindung (darf nil sein)
// @param newFS - neue Verbindung
// @date   2026-11-16 11:37
func inheritFastScanOff(oldFS, newFS remoteFS) {
	from, ok := oldFS.(fastScanSwitch)
	if !ok || !from.fastScanDisabled() {
		return
	}
	if to, ok := newFS.(fastScanSwitch); ok {
		to.disableFastScanForSession()
	}
}

// treeListAborter ist ein treeLister, der einen hängenden find-Befehl
// notfalls hart beenden kann (sftpFS: SSH-Verbindung schließen). Ein
// Server, der das Schließen des Exec-Kanals nicht bestätigt, hält sonst
// Lesen und wait ewig auf.
type treeListAborter interface {
	// AbortTreeList beendet alles, was am find-Befehl hängt
	AbortTreeList()
}

// errFastScanIdle - find lieferte fastScanIdleTimeout lang nichts
// (Leerlauf-Wächter; auch wenn schon das Öffnen des Kanals hängt)
var errFastScanIdle = errors.New("tree listing idle timeout")

// errFastScanTimeout - find brauchte länger als fastScanTimeout
var errFastScanTimeout = errors.New("tree listing timeout")

// errTreeListHung - der find-Befehl endete nach dem Abbruch nicht; die
// Verbindung wurde getrennt (wird als errConnectionLost gemeldet)
var errTreeListHung = errors.New("tree listing did not end after abort")

// fastScanAbortGrace - so lange darf der find-Befehl nach einem Abbruch
// (Esc, Zeitlimit, Leerlauf) zum Beenden brauchen, bevor AbortTreeList die
// Verbindung trennt (Variable, damit Tests sie verkleinern können).
var fastScanAbortGrace = 5 * time.Second

// treeListResult ist das Ergebnis des Lesens im Hintergrund.
type treeListResult struct {
	records []treeRecord
	err     error
}

// runTreeList führt den find-Befehl aus und liest seine Ausgabe (mit
// Zeitlimit; ctx bricht ab). Start, Lesen und Warten laufen in einer
// eigenen Goroutine (readTreeList), damit der Server Esc und Zeitgrenzen
// nicht aufhalten kann (Audit Build 90) - auch wenn schon das Öffnen des
// Exec-Kanals hängt. Endet ctx, schließt readTreeList den Kanal; dann:
//   - Esc (r.ctx beendet): sofort zurück. Esc allein schaltet den
//     Schnellweg nie ab. Endet der Befehl nicht binnen fastScanAbortGrace,
//     gilt er als hängend: Schnellweg für diese Verbindung aus, und gab es
//     schon einen Kanal, trennt AbortTreeList im Hintergrund die
//     Verbindung (der Vorgang ist ohnehin beendet).
//   - Zeitlimit oder Leerlauf: Schnellweg für diese Verbindung aus
//     (disableFastScan), sonst kostete jedes weitere F5/F6 dieselbe
//     Wartezeit. Hängt schon das Öffnen des Kanals (ListTree kehrt nicht
//     zurück), sofort ctx.Err() - Rückfall auf SFTP, die SFTP-Sitzung ist
//     davon nicht betroffen. Sonst bis fastScanAbortGrace auf das Ende des
//     Kanals warten, damit der SFTP-Rückfall eine intakte Verbindung hat
//     (ein normaler Server bestätigt das Schließen sofort). Hängt er
//     danach noch, trennt AbortTreeList die Verbindung und runTreeList
//     meldet errConnectionLost - kein stiller Rückfall über eine Leitung,
//     die gleich darauf getrennt wäre.
//
// Hängt schon ListTree, schließt eine Quelle wie sftpFS einen bereits
// geöffneten Kanal über ctx selbst (auch wenn "exec" unbeantwortet bleibt).
// Liefert ListTree erst nach dem Abbruch einen Kanal, schließt readTreeList
// ihn selbst (ctx ist beendet); hängt auch dieser, bleibt die Goroutine
// höchstens bis zum Ende der Verbindung - und weil der Schnellweg dann
// abgeschaltet ist, höchstens einmal je Verbindung.
//
// @param lister - Quelle
// @param starts - Startpfade
// @return []treeRecord - geprüfte Zeilen
// @return error - Befehl, Ausgabe oder Exit-Status ungültig, context.Cause(ctx)
// (errFastScanIdle, errFastScanTimeout, nach Esc context.Canceled), errConnectionLost
// @date   2026-11-16 12:13
func (r *copyRun) runTreeList(lister treeLister, starts []string) ([]treeRecord, error) {
	// Ursachen für das Protokoll: Leerlauf bzw. Zeitlimit statt nur
	// "context canceled"/"deadline exceeded" (Esc bleibt context.Canceled)
	idleCtx, idleCancel := context.WithCancelCause(r.ctx)
	defer idleCancel(nil)
	ctx, cancel := context.WithTimeoutCause(idleCtx, fastScanTimeout, errFastScanTimeout)
	defer cancel()
	stopIdle := func() { idleCancel(errFastScanIdle) }
	allowed := make(map[string]bool, len(starts))
	for _, s := range starts {
		allowed[s] = true
	}
	done := make(chan treeListResult, 1)
	var started atomic.Bool
	go func() {
		done <- readTreeList(ctx, stopIdle, lister, starts, allowed, &started)
	}()
	select {
	case res := <-done:
		if ctx.Err() != nil {
			if r.ctx.Err() == nil {
				// Zeitlimit/Leerlauf: Schnellweg für diese Verbindung lassen
				r.disableFastScan(lister, context.Cause(ctx))
			}
			return nil, context.Cause(ctx)
		}
		return res.records, res.err
	case <-ctx.Done():
	}
	if r.ctx.Err() == nil {
		// Zeitlimit oder Leerlauf: diese Verbindung liefert find nicht
		// zuverlässig - bis zum Ende der Sitzung nur noch SFTP
		r.disableFastScan(lister, context.Cause(ctx))
		if !started.Load() {
			// ListTree ist noch nicht zurück (Öffnen des Kanals oder "exec"
			// unbeantwortet): nichts zu trennen, die SFTP-Sitzung ist davon
			// nicht betroffen. Einen schon geöffneten Kanal schließt ListTree
			// selbst über ctx (sftpFS: AfterFunc vor dem Start), einen
			// später doch gelieferten readTreeList (ctx ist beendet)
			return nil, context.Cause(ctx)
		}
	}
	aborter, canAbort := lister.(treeListAborter)
	// Frist einmal lesen; beide Wartewege (auch die Goroutine nach Esc)
	// laufen gegen denselben Zeitpunkt. Kein "defer timer.Stop()": das
	// stoppte den Timer der Hintergrund-Goroutine beim Rückkehren, und nach
	// Esc würde nie getrennt (Fund im Test)
	timer := time.NewTimer(fastScanAbortGrace)
	if r.ctx.Err() == nil {
		// Zeitlimit/Leerlauf: auf das Ende des Kanals warten (Esc bricht ab)
		select {
		case <-done:
			timer.Stop()
			return nil, context.Cause(ctx)
		case <-r.ctx.Done():
		case <-timer.C:
			if !canAbort {
				// Nicht trennbar: Goroutine bleibt bis zum Ende der Verbindung
				return nil, context.Cause(ctx)
			}
			r.disableFastScan(lister, errTreeListHung)
			aborter.AbortTreeList()
			r.logScan("find endete nach dem Abbruch nicht, Verbindung getrennt")
			return nil, &connectionLostError{err: errTreeListHung}
		}
	}
	// Esc: sofort zurück, notfalls im Hintergrund trennen (die Goroutine
	// besitzt den Timer jetzt allein)
	if !canAbort {
		timer.Stop()
	} else {
		go func() {
			defer timer.Stop()
			select {
			case <-done:
			case <-timer.C:
				// Nicht Esc schaltet ab, sondern der hängende Befehl
				r.disableFastScan(lister, errTreeListHung)
				if !started.Load() {
					// Schon das Öffnen des Kanals hängt: nichts zu trennen,
					// die SFTP-Sitzung lebt (wie beim Zeitlimit oben)
					return
				}
				aborter.AbortTreeList()
				r.logScan("find endete nach dem Abbruch nicht, Verbindung getrennt")
			}
		}()
	}
	return nil, r.ctx.Err()
}

// readTreeList startet den find-Befehl, liest und prüft die Ausgabe und
// wartet auf das Ende (läuft in einer eigenen Goroutine, runTreeList).
// Endet ctx, wird die Ausgabe geschlossen.
//
// @param ctx - Abbruch und Zeitlimit
// @param cancel - bricht ctx ab (Leerlauf-Wächter)
// @param lister - Quelle
// @param starts - Startpfade
// @param allowed - Startpfade als Menge
// @param started - wird gesetzt, sobald ListTree einen Kanal geliefert hat
// @return treeListResult - Zeilen oder Fehler
// @date   2026-11-16 11:37
func readTreeList(ctx context.Context, cancel context.CancelFunc, lister treeLister, starts []string, allowed map[string]bool, started *atomic.Bool) treeListResult {
	// Leerlauf-Wächter schon VOR dem Start: hängt bereits das Öffnen des
	// Exec-Kanals (Server antwortet nicht), greift sonst nur das Zeitlimit
	// (Fable-Review Build 90). Kommt fastScanIdleTimeout lang nichts, abbrechen
	idle := &idleReader{}
	idle.touch()
	go idle.watch(ctx, cancel, fastScanIdleTimeout)
	out, wait, err := lister.ListTree(ctx, starts, copyMaxDepth)
	if err != nil {
		return treeListResult{err: err}
	}
	// Ab hier gibt es einen Kanal, der beim Abbruch geschlossen werden muss
	started.Store(true)
	// Kein Datenrennen: der Wächter liest nur idle.last (atomar); idle.r
	// setzt und liest allein diese Goroutine (Read in parseTreeOutput)
	idle.r = out
	idle.touch()
	// Abbruch schließt die Ausgabe (gibt ein hängendes Read frei, soweit
	// der Kanal das kann)
	stopClose := context.AfterFunc(ctx, func() { out.Close() })
	defer stopClose()
	// Eintragsgrenze plus die Startpfade selbst
	// Speichergrenze für Zeilen: doppelte Eintragsgrenze, weil unsichere
	// Namen und ihr Inhalt nicht zur Eintragsgrenze zählen (buildTrees zählt
	// nur sichere; darüber hinaus SFTP-Weg)
	records, parseErr := parseTreeOutput(idle, allowed, 2*copyMaxEntries+len(starts), fastScanMaxBytes)
	if parseErr != nil {
		// Abbrechen statt den Rest zu lesen
		out.Close()
		_ = wait()
		return treeListResult{err: parseErr}
	}
	waitErr := wait()
	out.Close()
	return treeListResult{records: records, err: waitErr}
}

// idleReader merkt sich, wann zuletzt Daten kamen (Leerlauf-Wächter des
// Schnellwegs).
type idleReader struct {
	r    io.Reader
	last atomic.Int64
}

// Read liest weiter und merkt den Zeitpunkt.
//
// @date   2026-11-13 09:28
func (i *idleReader) Read(b []byte) (int, error) {
	n, err := i.r.Read(b)
	if n > 0 {
		i.touch()
	}
	return n, err
}

// touch merkt den aktuellen Zeitpunkt als letzte Aktivität.
//
// @date   2026-11-13 09:28
func (i *idleReader) touch() { i.last.Store(time.Now().UnixNano()) }

// watch bricht ab (cancel), wenn länger als limit nichts gelesen wurde;
// endet mit ctx.
//
// @param ctx - Ende der Überwachung
// @param cancel - Abbruch des Befehls
// @param limit - längster Leerlauf
// @date   2026-11-13 09:28
func (i *idleReader) watch(ctx context.Context, cancel context.CancelFunc, limit time.Duration) {
	tick := time.NewTicker(max(min(limit/4, time.Second), time.Millisecond))
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if time.Since(time.Unix(0, i.last.Load())) > limit {
				cancel()
				return
			}
		}
	}
}

// fastTree ist der aus find gebaute Inhalt eines obersten Verzeichnisses.
type fastTree struct {
	// Inhalt erster Ebene (nach Namen sortiert, darunter rekursiv)
	children []*scanEntry
	// Angaben des Verzeichnisses selbst aus find
	self *treeFileInfo
}

// buildTrees baut aus den find-Zeilen die scanEntry-Bäume der obersten
// Verzeichnisse. find liefert jedes Verzeichnis vor seinem Inhalt; ein
// Eintrag ohne bekanntes Elternverzeichnis, doppelte Pfade oder ein
// fehlender Startpfad machen die Ausgabe ungültig. Unter einem unsicheren
// Namen (safeCopyName/fsAcceptsName) wird nichts übernommen - wie beim
// SFTP-Weg, der dort nie liest.
//
// @param records - Zeilen in find-Reihenfolge
// @param byStart - Startpfad -> oberster Eintrag
// @return map[*scanEntry]*fastTree - Bäume je oberstem Eintrag
// @return error - errFastScanInvalid oder errFastScanTooMany
// @date   2026-11-13 09:28
func (r *copyRun) buildTrees(records []treeRecord, byStart map[string]*scanEntry) (map[*scanEntry]*fastTree, error) {
	trees := map[*scanEntry]*fastTree{}
	// Verzeichnisse je Startpfad + rel; ignored: unter unsicheren Namen
	dirs := map[string]*scanEntry{}
	ignored := map[string]bool{}
	seen := map[string]bool{}
	// oberster Eintrag -> Sammler seines Inhalts
	holders := map[*scanEntry]*scanEntry{}
	safe := 0
	for _, rec := range records {
		top := byStart[rec.start]
		key := rec.start + "\x00" + rec.rel
		if seen[key] {
			return nil, errFastScanInvalid
		}
		seen[key] = true
		if rec.rel == "" {
			if !rec.info.IsDir() || trees[top] != nil {
				return nil, errFastScanInvalid
			}
			holder := &scanEntry{name: top.name, rel: top.rel, info: rec.info, resolved: true, listed: true}
			trees[top] = &fastTree{self: rec.info}
			dirs[key] = holder
			holders[top] = holder
			continue
		}
		parentRel := path.Dir(rec.rel)
		if parentRel == "." {
			parentRel = ""
		}
		parentKey := rec.start + "\x00" + parentRel
		if ignored[parentKey] {
			ignored[key] = true
			continue
		}
		parent := dirs[parentKey]
		if parent == nil {
			// Elternverzeichnis unbekannt (Reihenfolge verletzt)
			return nil, errFastScanInvalid
		}
		child := &scanEntry{name: rec.info.name, rel: path.Join(top.rel, rec.rel)}
		parent.children = append(parent.children, child)
		if !safeCopyName(child.name) || !fsAcceptsName(r.req.dst, child.name) {
			child.unsafe = true
			ignored[key] = true
			continue
		}
		safe++
		if safe > copyMaxEntries {
			return nil, errFastScanTooMany
		}
		child.info, child.resolved = rec.info, true
		if rec.info.IsDir() && scanDepth(child.rel) < copyMaxDepth {
			// Tiefer liest find nicht (-maxdepth); assemble meldet dort die Grenze
			child.listed = true
			dirs[key] = child
		}
	}
	for top, tree := range trees {
		tree.children = holders[top].children
	}
	if len(trees) != len(byStart) {
		return nil, errFastScanInvalid
	}
	// Wie beim SFTP-Weg: Geschwister nach Namen sortiert
	var sortAll func(list []*scanEntry)
	sortAll = func(list []*scanEntry) {
		sort.Slice(list, func(i, j int) bool { return list[i].name < list[j].name })
		for _, e := range list {
			sortAll(e.children)
		}
	}
	for _, tree := range trees {
		sortAll(tree.children)
	}
	return trees, nil
}

// sameAsSFTP prüft, ob find dasselbe Dateisystem gesehen hat wie SFTP: Je
// markiertem Verzeichnis müssen Typ, Rechte (samt Sonderbits), Größe und
// Änderungszeit (ganze Sekunden, so genau wie SFTP v3) aus find mit den
// SFTP-Angaben übereinstimmen (Liste des Quellverzeichnisses bzw. Lstat).
// Das kostet keine weitere Rundreise; ein gleichnamiges Verzeichnis in
// einer anderen Sicht (chroot, ForceCommand) hat kaum dieselben Angaben.
// Eine Abweichung - auch durch eine gleichzeitige Änderung - führt zum
// SFTP-Weg. (Ein Vergleich der ganzen ersten Ebene per SFTP-ReadDir kostete
// bei 2000 Einträgen und 30 ms schon 0,75 s - E2E Build 90.)
//
// @param trees - Bäume je oberstem Eintrag
// @return bool - true = passt
// @date   2026-11-13 09:28
func (r *copyRun) sameAsSFTP(trees map[*scanEntry]*fastTree) bool {
	const special = os.ModeType | os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky
	for top, tree := range trees {
		viaFind, viaSFTP := tree.self, top.info
		if viaFind.Mode()&special != viaSFTP.Mode()&special ||
			viaFind.Size() != viaSFTP.Size() ||
			viaFind.ModTime().Unix() != viaSFTP.ModTime().Unix() {
			return false
		}
	}
	return r.ctx.Err() == nil
}

// countTree zählt Dateien und Verzeichnisse eines Baums (ohne unsichere
// Namen) für die Anzeige beim Erfassen.
//
// @param list - Einträge
// @return int - Dateien (inkl. Symlinks)
// @return int - Verzeichnisse
// @date   2026-11-13 09:28
func countTree(list []*scanEntry) (int, int) {
	files, dirs := 0, 0
	for _, e := range list {
		if e.unsafe {
			continue
		}
		if e.info.IsDir() {
			dirs++
			f, d := countTree(e.children)
			files, dirs = files+f, dirs+d
		} else {
			files++
		}
	}
	return files, dirs
}

// logScan schreibt ins Protokoll, welcher Weg beim Erfassen genutzt wurde
// (Fehlertexte vom Server bereinigt).
//
// @param format - Text mit Platzhaltern
// @param args - Werte
// @date   2026-11-13 09:28
func (r *copyRun) logScan(format string, args ...interface{}) {
	if r.req.logf == nil {
		return
	}
	r.req.logf("%s", "Datei-Manager: Erfassen über "+sanitizeFileName(fmt.Sprintf(format, args...)))
}

// treeListInput liefert die Startpfade NUL-getrennt für stdin von xargs -0.
//
// @param starts - Startpfade
// @return []byte - Eingabe
// @date   2026-11-13 09:28
func treeListInput(starts []string) []byte {
	var buf bytes.Buffer
	for _, s := range starts {
		buf.WriteString(s)
		buf.WriteByte(0)
	}
	return buf.Bytes()
}
