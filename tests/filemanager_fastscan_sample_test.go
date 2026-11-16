// Paket main - Tests für die Stichprobe des find-Schnellwegs (Build 91)
//
// Prüft filemanager_fastscan_sample.go (sampleMatchesSFTP, pickSample,
// sampleInfoMatches) und die Anbindung in fastScan und removeMovedDir. Die
// oberste Ebene allein ist nur eine Heuristik (Audit Build 90): Eine andere
// Sicht (chroot, ForceCommand, Wrapper) kann gleiche oberste Angaben, darunter
// aber anderes liefern. Die Stichprobe vergleicht deshalb Einträge (Lstat) und
// Verzeichnisse (ReadDir) per SFTP; jede Abweichung führt zum SFTP-Weg.
//
//   - Abweichungen unter der obersten Ebene: SFTP-Weg, Ergebnis wie Referenz
//   - Verschieben: Hält find eine Datei für ein Verzeichnis, darf die Quelle
//     nie verloren gehen (removeMovedDir nutzt nur RemoveDir)
//   - Grenzen: höchstens fastScanSampleDirs ReadDir, nie große Verzeichnisse
//   - pickSample und sampleInfoMatches als reine Funktionen
//
// Wiederverwendet aus filemanager_fastscan_test.go: fastFS, fastSource,
// fastRunReq, fastCompare, fastCompareMove, fastSetRec, fastDropRec,
// fastWantPath, fastAssertSame, fastKeepVars.
//
// @author Kurt Ingwer
// @date   2026-11-15 14:50
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// fastAddRec hängt eine erfundene Zeile an die find-Ausgabe (Eintrag, den es
// per SFTP nicht gibt). Das Elternverzeichnis steht davor, die Reihenfolge
// bleibt also gültig.
func fastAddRec(rel string, typ, perm, size string) func(f *fastFS) {
	return func(f *fastFS) {
		f.mutate = func(recs []fastRecord) []fastRecord {
			return append(recs, fastRecord{
				typ: typ, perm: perm, size: size, mtime: "1700000000.0000000000",
				start: "/home/a/projekt", rel: rel,
			})
		}
	}
}

// fastSampleLog meldet, ob das Protokoll die Stichprobe als Grund nennt.
func fastSampleLog(o fastOutcome) bool {
	return strings.Contains(fastScanLog(o), "Stichprobe weicht ab")
}

// --- Verschieben: find hält eine Datei für ein Verzeichnis ---

// TestFastScanSampleCatchesFileAsDirOnMove - find meldet lib/x.go als
// Verzeichnis (Typ "d"), per SFTP ist es eine Datei. Mit Stichprobe fällt das
// auf (Lstat), der SFTP-Weg läuft, und das Ergebnis gleicht der Referenz: das
// Ziel hat die Datei lib/x.go mit Inhalt "x", die Quelle ist verschoben.
func TestFastScanSampleCatchesFileAsDirOnMove(t *testing.T) {
	defer fastKeepVars()()
	got, ref, src := fastCompareMove(t, fastSource, []string{"projekt"}, fastSetRec("lib/x.go", func(r *fastRecord) { r.typ = "d" }))
	fastWantPath(t, "Stichprobe", got, "SFTP")
	if !fastSampleLog(got) {
		t.Errorf("Protokoll nennt die Stichprobe nicht: %q", got.logs)
	}
	if !strings.Contains(fastScanLog(got), "/home/a/projekt/lib/x.go") {
		t.Errorf("Protokoll nennt den abweichenden Pfad nicht: %q", fastScanLog(got))
	}
	fastAssertSame(t, "Verschieben mit Stichprobe", got, ref)
	if want := `file -rw-r--r-- 1 ` + fmt.Sprint(copyTestTime.UnixNano()) + ` "x"`; got.snap["/srv/b/projekt/lib/x.go"] != want {
		t.Errorf("x.go am Ziel: %q, erwartet %q", got.snap["/srv/b/projekt/lib/x.go"], want)
	}
	if len(got.failures) != 0 {
		t.Errorf("Fehler: %q", got.failures)
	}
	if _, ok := src.content("/home/a/projekt/lib/x.go"); ok {
		t.Error("Quelle x.go nach erfolgreichem Verschieben noch da")
	}
	if src.node("/home/a/projekt") != nil {
		t.Error("Quellverzeichnis projekt nach erfolgreichem Verschieben noch da")
	}
}

// TestFastScanSampleOffMoveKeepsSourceFile - GEGENPROBE für removeMovedDir:
// Ist die Stichprobe aus (Einträge und Verzeichnisse 0), glaubt der
// Schnellweg find ("lib/x.go" ist ein Verzeichnis). Beim Verschieben darf die
// Quelldatei trotzdem NIE verloren gehen: removeMovedDir nutzt nur RemoveDir
// (rmdir), das bei einer Datei scheitert. Erwartet: Weg find, Quelle x.go
// weiter da mit Inhalt "x", ein Fehler gemeldet, "projekt" nicht in
// copiedNames (die Quelle wird nicht als erledigt gemeldet).
func TestFastScanSampleOffMoveKeepsSourceFile(t *testing.T) {
	defer fastKeepVars()()
	fastScanSampleEntries, fastScanSampleDirs = 0, 0
	got, _, src := fastCompareMove(t, fastSource, []string{"projekt"}, fastSetRec("lib/x.go", func(r *fastRecord) { r.typ = "d" }))
	fastWantPath(t, "Stichprobe aus", got, "find")
	if c, ok := src.content("/home/a/projekt/lib/x.go"); !ok || c != "x" {
		t.Fatalf("Quelldatei /home/a/projekt/lib/x.go verloren: %q, vorhanden %v", c, ok)
	}
	if len(got.failures) == 0 {
		t.Error("kein Fehler gemeldet, obwohl x.go nicht verschoben wurde")
	}
	found := false
	for _, f := range got.failures {
		if strings.Contains(f, "/home/a/projekt/lib/x.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("Fehlerliste nennt x.go nicht: %q", got.failures)
	}
	for _, name := range got.copied {
		if name == "projekt" {
			t.Errorf("projekt in copiedNames, obwohl nicht vollständig verschoben: %v", got.copied)
		}
	}
	// Die übrigen Quelldateien sind verschoben, projekt (nicht leer) bleibt
	if src.node("/home/a/projekt") == nil {
		t.Error("projekt gelöscht, obwohl x.go noch darin liegt")
	}
}

// --- Kopieren: find verschweigt einen Eintrag ---

// TestFastScanSampleCatchesDroppedFile - find lässt lib/deep/y.go weg, die
// oberste Ebene stimmt. Das ReadDir von lib/deep zeigt die Abweichung: SFTP-Weg
// (Protokoll mit "Stichprobe"), y.go liegt am Ziel, Ergebnis gleich der
// Referenz.
func TestFastScanSampleCatchesDroppedFile(t *testing.T) {
	defer fastKeepVars()()
	got, ref, _ := fastCompare(t, fastSource, []string{"projekt"}, false, fastDropRec("lib/deep/y.go"))
	fastWantPath(t, "y.go verschwiegen", got, "SFTP")
	if !fastSampleLog(got) {
		t.Errorf("Protokoll nennt die Stichprobe nicht: %q", got.logs)
	}
	fastAssertSame(t, "y.go verschwiegen", got, ref)
	if _, ok := got.snap["/srv/b/projekt/lib/deep/y.go"]; !ok {
		t.Errorf("y.go fehlt am Ziel: %v", got.snap)
	}
}

// --- Weitere Abweichungen unter der obersten Ebene ---

// TestFastScanSampleCatchesDeviations - Tabelle: jede Abweichung unterhalb
// der obersten Ebene führt zum SFTP-Weg mit dem Grund "Stichprobe", und das
// Ergebnis ist gleich der Referenz. Dazu die Gegenprobe: eine abweichende
// Größe eines VERZEICHNISSES ist keine Abweichung (Verzeichnisgrößen sind
// von Server zu Server verschieden), hier bleibt es beim Weg find.
func TestFastScanSampleCatchesDeviations(t *testing.T) {
	cases := []struct {
		name string
		tune func(f *fastFS)
		// find: Abweichung wird bewusst nicht als solche gewertet
		find bool
	}{
		{name: "falsche Größe einer Datei", tune: fastSetRec("main.go", func(r *fastRecord) { r.size = "999" })},
		{name: "falsche Größe einer tiefen Datei", tune: fastSetRec("lib/deep/y.go", func(r *fastRecord) { r.size = "0" })},
		{name: "falsche Rechte", tune: fastSetRec("run.sh", func(r *fastRecord) { r.perm = "644" })},
		{name: "setuid-Bit nur in find", tune: fastSetRec("main.go", func(r *fastRecord) { r.perm = "4644" })},
		{name: "sticky-Bit nur in find", tune: fastSetRec("lib", func(r *fastRecord) { r.perm = "1755" })},
		{name: "Symlink als Datei ausgegeben", tune: fastSetRec("aktuell", func(r *fastRecord) { r.typ = "f" })},
		{name: "Datei als Verzeichnis ausgegeben", tune: fastSetRec("main.go", func(r *fastRecord) { r.typ = "d" })},
		{name: "Verzeichnis als Datei ausgegeben", tune: fastSetRec("leer", func(r *fastRecord) { r.typ = "f" })},
		{name: "erfundener Eintrag nur in find (oben)", tune: fastAddRec("ghost.go", "f", "644", "3")},
		{name: "erfundener Eintrag nur in find (tief)", tune: fastAddRec("lib/ghost.go", "f", "644", "3")},
		{name: "verschwiegener Eintrag (oben)", tune: fastDropRec("main.go")},
		{name: "verschwiegener Eintrag (Unterverzeichnis)", tune: fastDropRec("lib/x.go")},
		{name: "Verzeichnisgröße weicht ab (kein Grund)", tune: fastSetRec("lib", func(r *fastRecord) { r.size = "4096" }), find: true},
		{name: "Dateizeit weicht ab (kein Grund)", tune: fastSetRec("main.go", func(r *fastRecord) { r.mtime = "1600000000.0000000000" }), find: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer fastKeepVars()()
			got, ref, _ := fastCompare(t, fastSource, []string{"projekt"}, false, c.tune)
			if c.find {
				fastWantPath(t, c.name, got, "find")
				return
			}
			fastWantPath(t, c.name, got, "SFTP")
			if !fastSampleLog(got) {
				t.Errorf("Protokoll nennt die Stichprobe nicht: %q", got.logs)
			}
			fastAssertSame(t, c.name, got, ref)
			if len(ref.snap) == 0 || len(got.failures) != 0 {
				t.Errorf("Referenz leer oder Fehler: %d Einträge, %q", len(ref.snap), got.failures)
			}
		})
	}
}

// TestFastScanSampleCleanTreeStaysOnFind - Gegenprobe: stimmt alles, bleibt
// es beim Weg find (die Stichprobe ist kein Dauer-Rückfall), und kein
// Protokolltext nennt die Stichprobe.
func TestFastScanSampleCleanTreeStaysOnFind(t *testing.T) {
	defer fastKeepVars()()
	got, ref, _ := fastCompare(t, fastSource, []string{"projekt"}, false, nil)
	fastWantPath(t, "sauberer Baum", got, "find")
	if fastSampleLog(got) {
		t.Errorf("Stichprobe meldet Abweichung bei sauberem Baum: %q", got.logs)
	}
	fastAssertSame(t, "sauberer Baum", got, ref)
}

// --- Grenzen der Stichprobe ---

// fastBigSource baut einen großen Baum: 60 Verzeichnisse mit je 20 Dateien in
// drei verschiedenen Tiefen (1, 2, 3 unter projekt).
func fastBigSource() *fastFS {
	src := newFastFS()
	src.addDir("/home/a/projekt", 0o755)
	src.nodes["/home/a/projekt"].modTime = copyTestTime
	for i := 0; i < 60; i++ {
		dir := fmt.Sprintf("/home/a/projekt/d%02d", i)
		switch i % 3 {
		case 1:
			dir += "/sub"
		case 2:
			dir += "/sub/tief"
		}
		for j := 0; j < 20; j++ {
			src.addFile(fmt.Sprintf("%s/f%02d.txt", dir, j), fmt.Sprintf("%d-%d", i, j), 0o644, copyTestTime)
		}
	}
	return src
}

// TestFastScanSampleIsBounded - großer Baum (60 Verzeichnisse mit je 20
// Dateien, drei Tiefen): Der Schnellweg bleibt (find), die Stichprobe liest
// höchstens fastScanSampleDirs Verzeichnisse per ReadDir (nicht den Baum),
// das Ergebnis gleicht der Referenz. Mit kleineren Grenzen sinkt die Zahl mit.
func TestFastScanSampleIsBounded(t *testing.T) {
	for _, dirs := range []int{8, 3, 1} {
		t.Run(fmt.Sprintf("Verzeichnisse=%d", dirs), func(t *testing.T) {
			defer fastKeepVars()()
			fastScanSampleDirs = dirs
			got, ref, src := fastCompare(t, fastBigSource, []string{"projekt"}, false, nil)
			fastWantPath(t, "großer Baum", got, "find")
			fastAssertSame(t, "großer Baum", got, ref)
			total := 0
			for _, p := range src.readDirPaths() {
				total += src.readDirCount(p)
			}
			if total == 0 || total > dirs {
				t.Errorf("ReadDir-Aufrufe der Stichprobe: %d, erwartet 1..%d (%q)", total, dirs, src.readDirPaths())
			}
			if len(ref.snap) < 1200 {
				t.Errorf("Referenz zu klein: %d Einträge", len(ref.snap))
			}
		})
	}
}

// TestFastScanSampleSkipsLargeDirectories - Verzeichnisse mit mehr als
// fastScanSampleMaxChildren Einträgen (laut find) kommen nie in die
// ReadDir-Stichprobe: Eine Anfrage dafür würde den Zeitgewinn auffressen.
// Hier mit MaxChildren = 3: projekt (5 Kinder) wird nicht gelesen, die
// kleinen Verzeichnisse (leer, lib, lib/deep) werden gelesen (die Stichprobe
// ist größer als der Baum, es kommen also alle in Frage).
func TestFastScanSampleSkipsLargeDirectories(t *testing.T) {
	defer fastKeepVars()()
	fastScanSampleMaxChildren = 3
	fastScanSampleDirs = 50
	got, ref, src := fastCompare(t, fastSource, []string{"projekt"}, false, nil)
	fastWantPath(t, "MaxChildren", got, "find")
	fastAssertSame(t, "MaxChildren", got, ref)
	if n := src.readDirCount("/home/a/projekt"); n != 0 {
		t.Errorf("projekt (5 Kinder > 3) wurde %d-mal per ReadDir gelesen", n)
	}
	for _, p := range []string{"/home/a/projekt/leer", "/home/a/projekt/lib", "/home/a/projekt/lib/deep"} {
		if n := src.readDirCount(p); n != 1 {
			t.Errorf("%s: %d ReadDir-Aufrufe, erwartet 1", p, n)
		}
	}

	// Ein Verzeichnis mit 300 Dateien (über der Standardgrenze 256) wird nie gelesen
	big := func() *fastFS {
		s := fastSource()
		for i := 0; i < 300; i++ {
			s.addFile(fmt.Sprintf("/home/a/projekt/gross/f%03d", i), "z", 0o644, copyTestTime)
		}
		return s
	}
	fastScanSampleMaxChildren = 256
	got, ref, src = fastCompare(t, big, []string{"projekt"}, false, nil)
	fastWantPath(t, "300 Dateien", got, "find")
	fastAssertSame(t, "300 Dateien", got, ref)
	if n := src.readDirCount("/home/a/projekt/gross"); n != 0 {
		t.Errorf("Verzeichnis mit 300 Einträgen wurde %d-mal per ReadDir gelesen", n)
	}
	if n := src.readDirCount("/home/a/projekt/lib"); n != 1 {
		t.Errorf("lib: %d ReadDir-Aufrufe, erwartet 1", n)
	}
}

// TestFastScanSampleDisabled - Einträge und Verzeichnisse 0: die Stichprobe
// liest nichts (kein ReadDir), der Schnellweg bleibt; negative Werte wirken
// wie 0 (kein Absturz).
func TestFastScanSampleDisabled(t *testing.T) {
	for _, n := range []int{0, -1} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			defer fastKeepVars()()
			fastScanSampleEntries, fastScanSampleDirs = n, n
			got, ref, src := fastCompare(t, fastSource, []string{"projekt"}, false, nil)
			fastWantPath(t, "Stichprobe aus", got, "find")
			fastAssertSame(t, "Stichprobe aus", got, ref)
			if paths := src.readDirPaths(); len(paths) != 0 {
				t.Errorf("ReadDir trotz abgeschalteter Stichprobe: %q", paths)
			}
		})
	}
}

// --- pickSample (rein) ---

// sortedInts liefert eine sortierte Kopie.
func sortedInts(in []int) []int {
	out := append([]int(nil), in...)
	sort.Ints(out)
	return out
}

// assertDistinctInRange prüft: Länge want, alle Indizes in [0,n), keine Dublette.
func assertDistinctInRange(t *testing.T, label string, got []int, want, n int) {
	t.Helper()
	if len(got) != want {
		t.Fatalf("%s: Länge %d, erwartet %d (%v)", label, len(got), want, got)
	}
	seen := map[int]bool{}
	for _, i := range got {
		if i < 0 || i >= n {
			t.Fatalf("%s: Index %d außerhalb von 0..%d", label, i, n-1)
		}
		if seen[i] {
			t.Fatalf("%s: Index %d doppelt in %v", label, i, got)
		}
		seen[i] = true
	}
}

// TestPickSampleSmallInputTakesAll - ist n >= len(depths), kommen alle
// Indizes (Randfälle: gleich groß, leere Eingabe, deep größer als n).
func TestPickSampleSmallInputTakesAll(t *testing.T) {
	for _, c := range []struct{ length, n, deep int }{{5, 5, 2}, {5, 9, 2}, {1, 1, 1}, {3, 100, 100}, {0, 4, 2}, {7, 7, 0}} {
		depths := make([]int, c.length)
		for i := range depths {
			depths[i] = i % 3
		}
		got := sortedInts(pickSample(depths, c.n, c.deep))
		want := make([]int, c.length)
		for i := range want {
			want[i] = i
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("len %d, n %d, deep %d: %v, erwartet %v", c.length, c.n, c.deep, got, want)
		}
	}
}

// TestPickSampleZeroOrNegative - n = 0 (oder negativ) wählt nichts.
func TestPickSampleZeroOrNegative(t *testing.T) {
	for _, n := range []int{0, -1, -100} {
		if got := pickSample([]int{1, 2, 3, 4}, n, 2); len(got) != 0 {
			t.Errorf("n=%d: %v, erwartet leer", n, got)
		}
		if got := pickSample(nil, n, 2); len(got) != 0 {
			t.Errorf("n=%d, leere Eingabe: %v", n, got)
		}
	}
}

// TestPickSampleExactLengthDistinct - bei großer Eingabe genau n eindeutige
// gültige Indizes, über viele Wiederholungen ohne Dublette (die Zufallsauswahl
// zieht Wiederholungen ab). Mit deep = 0, deep > n und negativem deep.
func TestPickSampleExactLengthDistinct(t *testing.T) {
	depths := make([]int, 100)
	for i := range depths {
		depths[i] = i % 7
	}
	for _, deep := range []int{0, 4, 16, 50, -3} {
		for rep := 0; rep < 2000; rep++ {
			got := pickSample(depths, 16, deep)
			assertDistinctInRange(t, fmt.Sprintf("deep=%d rep=%d", deep, rep), got, 16, len(depths))
		}
	}
	// n nur einen unter der Länge: fast alles, nie eine Dublette
	for rep := 0; rep < 500; rep++ {
		assertDistinctInRange(t, "n=len-1", pickSample(depths, 99, 4), 99, len(depths))
	}
}

// TestPickSampleAlwaysContainsDeepest - die deep tiefsten Einträge (eindeutig
// konstruiert, sonst gleiche Tiefe 1) sind immer enthalten; Ist deep größer
// als n, zählt n.
func TestPickSampleAlwaysContainsDeepest(t *testing.T) {
	depths := make([]int, 80)
	for i := range depths {
		depths[i] = 1
	}
	deepIdx := []int{3, 41, 42, 77}
	for k, i := range deepIdx {
		depths[i] = 100 + k
	}
	for rep := 0; rep < 1000; rep++ {
		got := pickSample(depths, 16, 4)
		assertDistinctInRange(t, "deep=4", got, 16, len(depths))
		has := map[int]bool{}
		for _, i := range got {
			has[i] = true
		}
		for _, i := range deepIdx {
			if !has[i] {
				t.Fatalf("rep %d: tiefster Eintrag %d fehlt in %v", rep, i, got)
			}
		}
	}
	// deep (4) > n (3): nur die drei tiefsten (Index 77, 42, 41 mit 103, 102, 101 ...)
	for rep := 0; rep < 200; rep++ {
		got := sortedInts(pickSample(depths, 3, 4))
		if fmt.Sprint(got) != "[41 42 77]" {
			t.Fatalf("deep > n: %v, erwartet [41 42 77]", got)
		}
	}
	// Gleiche Tiefe überall: trotzdem n eindeutige
	flat := make([]int, 50)
	assertDistinctInRange(t, "alle gleich tief", pickSample(flat, 10, 4), 10, 50)
}

// TestPickSampleIsRandomBeyondDeepest - Der Rest ist zufällig: über viele
// Wiederholungen kommt jeder Index vor, und die Auswahl ist nicht immer
// dieselbe (sonst prüfte die Stichprobe immer dieselben Einträge).
func TestPickSampleIsRandomBeyondDeepest(t *testing.T) {
	depths := make([]int, 60)
	depths[0], depths[1] = 100, 101
	seen := map[int]bool{}
	variants := map[string]bool{}
	for rep := 0; rep < 600; rep++ {
		got := pickSample(depths, 10, 2)
		variants[fmt.Sprint(sortedInts(got))] = true
		for _, i := range got {
			seen[i] = true
		}
	}
	if len(seen) != len(depths) {
		t.Errorf("nur %d von %d Indizes kamen je vor", len(seen), len(depths))
	}
	if len(variants) < 100 {
		t.Errorf("nur %d verschiedene Auswahlen in 600 Wiederholungen", len(variants))
	}
}

// --- sampleInfoMatches (rein) ---

// sampleInfo baut Angaben (Name egal) für die Tabelle.
func sampleInfo(mode os.FileMode, size int64) os.FileInfo {
	return &treeFileInfo{name: "x", size: size, mode: mode, modTime: time.Unix(1700000000, 0)}
}

// TestSampleInfoMatchesTable - Vergleich der Angaben aus find und SFTP: Typ,
// Rechte samt Sonderbits, Größe nur bei normalen Dateien; SFTP-Angaben ohne
// Rechte (ReadDir mancher Server) zählen nur über den Typ; nil ist nie gleich.
func TestSampleInfoMatchesTable(t *testing.T) {
	const reg, dir, link = os.FileMode(0), os.ModeDir, os.ModeSymlink
	cases := []struct {
		name       string
		find, sftp os.FileInfo
		want       bool
	}{
		{"gleich (Datei)", sampleInfo(reg|0o644, 10), sampleInfo(reg|0o644, 10), true},
		{"gleich (Verzeichnis)", sampleInfo(dir|0o755, 4096), sampleInfo(dir|0o755, 4096), true},
		{"gleich (Symlink)", sampleInfo(link|0o777, 4), sampleInfo(link|0o777, 4), true},
		{"Typ verschieden: Datei/Verzeichnis", sampleInfo(reg|0o755, 0), sampleInfo(dir|0o755, 0), false},
		{"Typ verschieden: Verzeichnis/Datei", sampleInfo(dir|0o755, 0), sampleInfo(reg|0o755, 0), false},
		{"Typ verschieden: Datei/Symlink", sampleInfo(reg|0o777, 3), sampleInfo(link|0o777, 3), false},
		{"Typ verschieden: Symlink/Verzeichnis", sampleInfo(link|0o777, 3), sampleInfo(dir|0o777, 3), false},
		{"Rechte verschieden", sampleInfo(reg|0o644, 10), sampleInfo(reg|0o600, 10), false},
		{"Rechte verschieden (Verzeichnis)", sampleInfo(dir|0o755, 0), sampleInfo(dir|0o700, 0), false},
		{"setuid verschieden", sampleInfo(reg|0o755|os.ModeSetuid, 10), sampleInfo(reg|0o755, 10), false},
		{"setuid nur per SFTP", sampleInfo(reg|0o755, 10), sampleInfo(reg|0o755|os.ModeSetuid, 10), false},
		{"setgid verschieden", sampleInfo(dir|0o755|os.ModeSetgid, 0), sampleInfo(dir|0o755, 0), false},
		{"sticky verschieden", sampleInfo(dir|0o777|os.ModeSticky, 0), sampleInfo(dir|0o777, 0), false},
		{"Größe verschieden (Datei)", sampleInfo(reg|0o644, 10), sampleInfo(reg|0o644, 11), false},
		{"Größe 0 gegen 1 (Datei)", sampleInfo(reg|0o644, 0), sampleInfo(reg|0o644, 1), false},
		{"Größe verschieden (Verzeichnis) egal", sampleInfo(dir|0o755, 4096), sampleInfo(dir|0o755, 60), true},
		{"Größe verschieden (Symlink) egal", sampleInfo(link|0o777, 4), sampleInfo(link|0o777, 9), true},
		{"SFTP ohne Rechte: nur Typ zählt (Datei)", sampleInfo(reg|0o644, 10), sampleInfo(reg, 10), true},
		{"SFTP ohne Rechte: Datei, Größe verschieden", sampleInfo(reg|0o644, 10), sampleInfo(reg, 99), false},
		{"SFTP ohne Rechte: Verzeichnis", sampleInfo(dir|0o755, 0), sampleInfo(dir, 0), true},
		{"SFTP ohne Rechte: Symlink", sampleInfo(link|0o777, 3), sampleInfo(link, 3), true},
		{"SFTP ohne Rechte: falscher Typ", sampleInfo(dir|0o755, 0), sampleInfo(reg, 0), false},
		{"find nil", nil, sampleInfo(reg|0o644, 1), false},
		{"SFTP nil", sampleInfo(reg|0o644, 1), nil, false},
		{"beide nil", nil, nil, false},
	}
	for _, c := range cases {
		if got := sampleInfoMatches(c.find, c.sftp); got != c.want {
			t.Errorf("%s: %v, erwartet %v", c.name, got, c.want)
		}
	}
}
