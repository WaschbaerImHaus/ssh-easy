// Paket main - Datei-Manager (MC-Modus): Stichprobe für den find-Schnellweg
//
// Der Schnellweg (filemanager_fastscan.go) glaubt der find-Ausgabe, wenn die
// markierten Verzeichnisse selbst dieselben Angaben wie per SFTP haben. Das
// ist nur eine Heuristik (Audit Build 90): Eine andere Sicht (chroot,
// ForceCommand, Wrapper-Skript, anderer Rechner) könnte gleiche oberste
// Angaben, darunter aber anderes liefern - etwa eine Datei als Verzeichnis
// oder fehlende Einträge. Deshalb vergleicht sampleMatchesSFTP vor der
// Übernahme eine kleine Stichprobe per SFTP:
//   - bis zu fastScanSampleEntries Einträge per Lstat (Typ, Rechte samt
//     Sonderbits, Größe bei normalen Dateien), davon bis zu
//     fastScanSampleDeep der tiefsten, der Rest zufällig
//   - bis zu fastScanSampleDirs Verzeichnisse (auch die markierten) per
//     ReadDir: dieselben Namen, je Name dieselben Angaben wie oben. So fallen
//     auch Einträge auf, die find verschweigt. Nur Verzeichnisse mit
//     höchstens fastScanSampleMaxChildren Einträgen (laut find), damit eine
//     Anfrage reicht und der Zeitgewinn bleibt
//
// Alle Anfragen laufen gleichzeitig (höchstens copyParallel), insgesamt
// meist ein bis zwei Rundreisen. Weicht etwas ab, nimmt runCopy den
// SFTP-Weg. Eine Stichprobe beweist nichts über Einträge außerhalb von ihr
// (SECURITY_RISKS.md); bei kleinen Bäumen umfasst sie alles.
//
// @author Kurt Ingwer
// @date   2026-11-15 09:09
package main

import (
	"math/rand/v2"
	"os"
	"path"
	"sync"
)

// Grenzen der Stichprobe (Variablen, damit Tests sie verändern können)
var (
	// fastScanSampleEntries - so viele Einträge werden per Lstat verglichen
	fastScanSampleEntries = 16
	// fastScanSampleDeep - davon so viele der tiefsten Einträge
	fastScanSampleDeep = 4
	// fastScanSampleDirs - so viele Verzeichnisse werden per ReadDir verglichen
	fastScanSampleDirs = 8
	// fastScanSampleMaxChildren - nur Verzeichnisse mit höchstens so vielen
	// Einträgen kommen in die ReadDir-Stichprobe
	fastScanSampleMaxChildren = 256
)

// sampleSpecialBits sind die verglichenen Teile von os.FileMode.
const sampleSpecialBits = os.ModeType | os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky

// sampleDir ist ein Verzeichnis für die ReadDir-Stichprobe.
type sampleDir struct {
	// Pfad relativ zu srcDir
	rel string
	// Inhalt laut find (auch unsichere Namen, dann ohne Angaben)
	children []*scanEntry
	// Tiefe unter srcDir
	depth int
}

// sampleMatchesSFTP vergleicht eine Stichprobe der find-Bäume per SFTP
// (siehe Dateikopf).
//
// @param trees - Bäume je oberstem Eintrag (aus buildTrees)
// @return bool - true = alles gleich
// @return string - erster abweichender Pfad (für das Protokoll)
// @date   2026-11-15 09:09
func (r *copyRun) sampleMatchesSFTP(trees map[*scanEntry]*fastTree) (bool, string) {
	var entries []*scanEntry
	var dirs []sampleDir
	var walk func(list []*scanEntry)
	walk = func(list []*scanEntry) {
		for _, e := range list {
			if e.unsafe || e.info == nil {
				continue
			}
			entries = append(entries, e)
			if e.listed && e.info.IsDir() {
				if len(e.children) <= fastScanSampleMaxChildren {
					dirs = append(dirs, sampleDir{rel: e.rel, children: e.children, depth: scanDepth(e.rel)})
				}
				walk(e.children)
			}
		}
	}
	for top, tree := range trees {
		if len(tree.children) <= fastScanSampleMaxChildren {
			dirs = append(dirs, sampleDir{rel: top.rel, children: tree.children, depth: scanDepth(top.rel)})
		}
		walk(tree.children)
	}
	entryDepths := make([]int, len(entries))
	for i, e := range entries {
		entryDepths[i] = scanDepth(e.rel)
	}
	dirDepths := make([]int, len(dirs))
	for i, d := range dirs {
		dirDepths[i] = d.depth
	}
	var jobs []func() (bool, string)
	for _, i := range pickSample(entryDepths, fastScanSampleEntries, fastScanSampleDeep) {
		e := entries[i]
		jobs = append(jobs, func() (bool, string) { return r.sampleEntryMatches(e) })
	}
	for _, i := range pickSample(dirDepths, fastScanSampleDirs, fastScanSampleDeep) {
		d := dirs[i]
		jobs = append(jobs, func() (bool, string) { return r.sampleDirMatches(d) })
	}
	return r.runSampleJobs(jobs)
}

// runSampleJobs führt die Vergleiche gleichzeitig aus (höchstens
// copyParallel). Nach der ersten Abweichung oder einem Abbruch startet kein
// weiterer Vergleich.
//
// @param jobs - Vergleiche
// @return bool - true = alle gleich (und kein Abbruch)
// @return string - erster abweichender Pfad
// @date   2026-11-15 09:09
func (r *copyRun) runSampleJobs(jobs []func() (bool, string)) (bool, string) {
	var mu sync.Mutex
	ok, where := true, ""
	work := make(chan func() (bool, string))
	var wg sync.WaitGroup
	for i := 0; i < min(max(copyParallel, 1), len(jobs)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range work {
				mu.Lock()
				skip := !ok
				mu.Unlock()
				if skip || r.ctx.Err() != nil {
					continue
				}
				if same, p := job(); !same {
					mu.Lock()
					if ok {
						ok, where = false, p
					}
					mu.Unlock()
				}
			}
		}()
	}
	for _, job := range jobs {
		work <- job
	}
	close(work)
	wg.Wait()
	if r.ctx.Err() != nil {
		return false, "abgebrochen"
	}
	return ok, where
}

// sampleEntryMatches vergleicht einen Eintrag per SFTP-Lstat.
//
// @param e - Eintrag aus find
// @return bool - true = gleich
// @return string - Pfad (bei Abweichung)
// @date   2026-11-15 09:09
func (r *copyRun) sampleEntryMatches(e *scanEntry) (bool, string) {
	p := path.Join(r.req.srcDir, e.rel)
	info, err := r.req.src.Lstat(p)
	if err != nil || !sampleInfoMatches(e.info, info) {
		return false, p
	}
	return true, ""
}

// sampleDirMatches vergleicht den Inhalt eines Verzeichnisses per
// SFTP-ReadDir: dieselben Namen (auch unsichere), je sicherem Namen
// dieselben Angaben.
//
// @param d - Verzeichnis aus find
// @return bool - true = gleich
// @return string - Pfad (bei Abweichung)
// @date   2026-11-15 09:09
func (r *copyRun) sampleDirMatches(d sampleDir) (bool, string) {
	p := path.Join(r.req.srcDir, d.rel)
	list, err := r.req.src.ReadDir(p)
	if err != nil {
		return false, p
	}
	viaSFTP := make(map[string]os.FileInfo, len(list))
	for _, info := range list {
		name := info.Name()
		if name == "." || name == ".." {
			continue
		}
		viaSFTP[name] = info
	}
	if len(viaSFTP) != len(d.children) {
		return false, p
	}
	for _, child := range d.children {
		info, found := viaSFTP[child.name]
		if !found {
			return false, path.Join(p, child.name)
		}
		if child.info != nil && !sampleInfoMatches(child.info, info) {
			return false, path.Join(p, child.name)
		}
	}
	return true, ""
}

// sampleInfoMatches vergleicht die Angaben aus find mit denen per SFTP: Typ
// und Rechte samt Sonderbits, bei normalen Dateien auch die Größe.
// Angaben ohne Rechte (Mode 0 ohne Typ, manche Server bei ReadDir) gelten
// nur über den Typ.
//
// @param viaFind - Angaben aus find
// @param viaSFTP - Angaben per SFTP
// @return bool - true = gleich
// @date   2026-11-15 09:09
func sampleInfoMatches(viaFind, viaSFTP os.FileInfo) bool {
	if viaFind == nil || viaSFTP == nil {
		return false
	}
	findMode, sftpMode := viaFind.Mode()&sampleSpecialBits, viaSFTP.Mode()&sampleSpecialBits
	if findMode.Type() != sftpMode.Type() {
		return false
	}
	if sftpMode != sftpMode.Type() && findMode != sftpMode {
		return false
	}
	if findMode.IsRegular() && viaFind.Size() != viaSFTP.Size() {
		return false
	}
	return true
}

// pickSample wählt bis zu n verschiedene Indizes aus: zuerst bis zu deep
// der tiefsten (bei gleicher Tiefe zufällig), dann zufällige. Sind es
// höchstens n, kommen alle in die Stichprobe.
//
// @param depths - Tiefe je Index
// @param n - größte Stichprobe
// @param deep - davon höchstens so viele der tiefsten
// @return []int - gewählte Indizes
// @date   2026-11-15 09:09
func pickSample(depths []int, n, deep int) []int {
	if n <= 0 {
		return nil
	}
	if len(depths) <= n {
		all := make([]int, len(depths))
		for i := range all {
			all[i] = i
		}
		return all
	}
	// Die tiefsten in einem Durchgang: kleine Bestenliste, gleiche Tiefe
	// per Zufallsschlüssel entschieden
	type candidate struct {
		idx, depth int
		key        uint64
	}
	less := func(a, b candidate) bool {
		return a.depth < b.depth || (a.depth == b.depth && a.key < b.key)
	}
	deep = min(max(deep, 0), n)
	best := make([]candidate, 0, deep)
	for i, d := range depths {
		if deep == 0 {
			break
		}
		c := candidate{idx: i, depth: d, key: rand.Uint64()}
		if len(best) < deep {
			best = append(best, c)
			continue
		}
		weakest := 0
		for j := range best {
			if less(best[j], best[weakest]) {
				weakest = j
			}
		}
		if less(best[weakest], c) {
			best[weakest] = c
		}
	}
	chosen := make(map[int]bool, n)
	out := make([]int, 0, n)
	for _, c := range best {
		chosen[c.idx] = true
		out = append(out, c.idx)
	}
	// Rest zufällig (n ist klein gegen len(depths): wenige Wiederholungen)
	for len(out) < n {
		i := rand.IntN(len(depths))
		if !chosen[i] {
			chosen[i] = true
			out = append(out, i)
		}
	}
	return out
}
