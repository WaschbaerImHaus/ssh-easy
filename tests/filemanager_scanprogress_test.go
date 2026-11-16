// Paket main - Tests: Anzeige beim Erfassen und beim Löschen der Quelle (Build 90, Teil 2)
//
// Beim Kopieren und Verschieben eines großen Baums (Nutzerbericht: 3.763
// Verzeichnisse auf einem Server im Internet) stand lange nur "Erfasse …" mit
// einer Dateizahl, die sich nicht bewegte, solange nur Verzeichnisse gelesen
// wurden. Und nach dem Kopieren beim Verschieben blieb die Anzeige stehen,
// während die leeren Quellverzeichnisse gelöscht wurden.
//
// Jetzt meldet copyProgress:
//   - beim Erfassen totalFiles (Dateien samt Symlinks) und totalDirs
//     (Verzeichnisse, das oberste mitgezählt), laufend während des Lesens
//   - beim Verschieben in der Lösch-Phase removingDirs, removedDirs und
//     removeTotal (Zahl der zu löschenden Quellverzeichnisse)
//
// Der Dialog zeigt dazu txtFMCopyScanned ("Erfasse … %d Dateien, %d
// Verzeichnisse") bzw. txtFMMoveCleanup ("Entferne leere Quellverzeichnisse
// … %d/%d"). Der alte Text txtFMCopyScanning entfällt.
//
// Dazu zwei Anzeige-Punkte (Build 90):
//   - Die beiden Zeilen werden bei zu schmaler Breite umgebrochen (wrapText),
//     nicht abgeschnitten.
//   - overlayAt ersetzt ein breites Zeichen (CJK), das an der RECHTEN
//     Kastenkante halb verdeckt wird, durch ein Leerzeichen; sonst wurde die
//     Bildschirmzeile eine Spalte zu breit (chinesisch/japanisch/koreanisch
//     bei 40 Spalten).
//
// Die Fortschrittsmeldungen sind auf eine pro 100 ms begrenzt; die Testbäume
// sind deshalb Ketten (jede Ebene erst nach dem Lesen der vorigen bekannt),
// die auch bei paralleler Arbeit länger als 100 ms dauern.
//
// @author Kurt Ingwer
// @date   2026-11-13 10:05
package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// collectCopyProgress führt runCopy aus und sammelt alle Fortschrittsmeldungen
// samt Ergebnis. Der Kanal ist groß genug, damit keine Meldung verworfen wird.
func collectCopyProgress(t *testing.T, req copyRequest) ([]copyProgress, copyResult) {
	t.Helper()
	events := make(chan copyEvent, 4096)
	go runCopy(context.Background(), req, events, make(chan copyConflictAnswer, 1))
	var progress []copyProgress
	timeout := time.After(20 * time.Second)
	for {
		select {
		case ev := <-events:
			switch {
			case ev.result != nil:
				return progress, *ev.result
			case ev.progress != nil:
				progress = append(progress, *ev.progress)
			case ev.conflict != "":
				t.Fatalf("unerwartete Rückfrage für %s", ev.conflict)
			}
		case <-timeout:
			t.Fatal("runCopy hängt")
		}
	}
}

// addChain legt unter root eine Kette aus n verschachtelten leeren
// Verzeichnissen an (root/k1/n/n/...) und liefert den Pfad des tiefsten.
func addChain(m *memFS, root, first string, n int) string {
	p := root + "/" + first
	for i := 1; i < n; i++ {
		p += "/n"
	}
	m.addDir(p, 0o755)
	return p
}

// --- Erfassen ---

// TestCopyProgressDuringScanCountsDirectories - Zwei Ketten aus je 100 leeren
// Verzeichnissen und drei Dateien ganz unten (200 Verzeichnisse + das oberste
// = 201, 3 Dateien). Das Lesen dauert (3 ms je Ebene) deutlich länger als das
// Meldeintervall. Erwartet: mehrere Meldungen mit scanning=true, in denen
// totalDirs schon > 0 ist, bevor die erste Datei gefunden wird; die Zahlen
// wachsen nur; die erste Meldung nach dem Erfassen hat die Endsummen
// (totalFiles 3, totalDirs 201).
func TestCopyProgressDuringScanCountsDirectories(t *testing.T) {
	src := &parReadDirFS{memFS: newMemFS("/home/a"), readDelay: 3 * time.Millisecond}
	src.addDir("/home/a/wurzel", 0o755)
	deep := addChain(src.memFS, "/home/a/wurzel", "k1", 100)
	addChain(src.memFS, "/home/a/wurzel", "k2", 100)
	for i := 1; i <= 3; i++ {
		src.addFile(fmt.Sprintf("%s/datei%d.txt", deep, i), "x", 0o644, copyTestTime)
	}
	dst := newMemFS("/srv/b")
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"wurzel"}}
	progress, res := collectCopyProgress(t, req)
	if len(res.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(res.failures))
	}

	scanning, withDirs := 0, 0
	var last copyProgress
	var after *copyProgress
	for i := range progress {
		p := progress[i]
		if !p.scanning {
			after = &progress[i]
			break
		}
		scanning++
		if p.totalDirs > 0 {
			withDirs++
		}
		if p.totalDirs < last.totalDirs || p.totalFiles < last.totalFiles {
			t.Errorf("Zähler sinken: %+v nach %+v", p, last)
		}
		if p.totalDirs > 201 || p.totalFiles > 3 {
			t.Errorf("Zähler über den Endsummen: %+v", p)
		}
		last = p
	}
	if scanning < 2 || withDirs < 2 {
		t.Errorf("%d Meldungen beim Erfassen, davon %d mit Verzeichnissen; erwartet mindestens je 2", scanning, withDirs)
	}
	if after == nil {
		t.Fatal("keine Meldung nach dem Erfassen")
	}
	if after.totalFiles != 3 || after.totalDirs != 201 {
		t.Errorf("Endsummen: %d Dateien, %d Verzeichnisse; erwartet 3 und 201", after.totalFiles, after.totalDirs)
	}
	// Die Dateien liegen ganz unten: Erst die Verzeichnisse haben Meldungen ausgelöst
	var midScanFiles bool
	for _, p := range progress {
		if p.scanning && p.totalDirs > 0 && p.totalFiles == 0 {
			midScanFiles = true
		}
	}
	if !midScanFiles {
		t.Error("keine Meldung mit Verzeichnissen und noch 0 Dateien: Verzeichnisse lösen keine Meldung aus")
	}
}

// TestCopyProgressScanTotals - Endsummen nach dem Erfassen in Randfällen:
// ein einzelner Eintrag, der selbst eine Datei ist, hat 0 Verzeichnisse; ein
// Verzeichnis zählt sich selbst mit; Symlinks zählen als Dateien.
func TestCopyProgressScanTotals(t *testing.T) {
	cases := []struct {
		name        string
		names       []string
		files, dirs int
	}{
		{"nur Datei", []string{"notiz.txt"}, 1, 0},
		{"Verzeichnis mit Unterverzeichnis", []string{"projekt"}, 4, 2}, // projekt, projekt/lib; main.go, run.sh, x.go, Symlink
		{"Datei und Verzeichnis", []string{"notiz.txt", "projekt"}, 5, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := copyRequest{src: newCopySource(), dst: newMemFS("/srv/b"), srcDir: "/home/a", dstDir: "/srv/b", names: tc.names}
			progress, _ := collectCopyProgress(t, req)
			var after *copyProgress
			for i := range progress {
				if !progress[i].scanning {
					after = &progress[i]
					break
				}
			}
			if after == nil {
				t.Fatal("keine Meldung nach dem Erfassen")
			}
			if after.totalFiles != tc.files || after.totalDirs != tc.dirs {
				t.Errorf("Summen: %d Dateien, %d Verzeichnisse; erwartet %d und %d", after.totalFiles, after.totalDirs, tc.files, tc.dirs)
			}
		})
	}
}

// --- Löschen der Quellverzeichnisse beim Verschieben ---

// TestMoveProgressDuringSourceCleanup - Verschieben eines Baums aus dem
// obersten Verzeichnis und einer Kette von 60 leeren Unterverzeichnissen
// (Löschen dauert 3 ms je Ebene, tiefste zuerst). Erwartet: Meldungen mit
// removingDirs=true; removeTotal ist überall 61 (die Zahl der zu löschenden
// Quellverzeichnisse); removedDirs wächst nur, bleibt <= removeTotal und
// wird > 0; in diesen Meldungen wird nicht mehr "erfasst". Ankommende
// Meldungen werden einzeln geprüft (zu schnelle werden verworfen).
func TestMoveProgressDuringSourceCleanup(t *testing.T) {
	src := &parRemoveDirFS{memFS: newMemFS("/home/a"), delay: 3 * time.Millisecond}
	src.addDir("/home/a/wurzel", 0o755)
	deep := addChain(src.memFS, "/home/a/wurzel", "k1", 60)
	src.addFile(deep+"/datei.txt", "x", 0o644, copyTestTime)
	dst := newMemFS("/srv/b")
	req := copyRequest{src: src, dst: dst, srcDir: "/home/a", dstDir: "/srv/b", names: []string{"wurzel"}, move: true}
	progress, res := collectCopyProgress(t, req)
	if len(res.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(res.failures))
	}
	if src.node("/home/a/wurzel") != nil {
		t.Fatal("Quelle nicht gelöscht")
	}

	removing, maxRemoved, lastRemoved := 0, 0, 0
	seenRemoving := false
	for _, p := range progress {
		if !p.removingDirs {
			if seenRemoving {
				t.Errorf("Meldung ohne removingDirs nach Beginn der Lösch-Phase: %+v", p)
			}
			if p.removedDirs != 0 || p.removeTotal != 0 {
				t.Errorf("Lösch-Zähler außerhalb der Lösch-Phase: %+v", p)
			}
			continue
		}
		seenRemoving = true
		removing++
		if p.scanning {
			t.Errorf("Lösch-Phase meldet scanning: %+v", p)
		}
		if p.removeTotal != 61 {
			t.Errorf("removeTotal = %d, erwartet 61", p.removeTotal)
		}
		if p.removedDirs < 0 || p.removedDirs > p.removeTotal {
			t.Errorf("removedDirs = %d außerhalb 0..%d", p.removedDirs, p.removeTotal)
		}
		if p.removedDirs < lastRemoved {
			t.Errorf("removedDirs sinkt: %d nach %d", p.removedDirs, lastRemoved)
		}
		lastRemoved = p.removedDirs
		maxRemoved = max(maxRemoved, p.removedDirs)
	}
	if removing < 2 {
		t.Errorf("%d Meldungen der Lösch-Phase, erwartet mindestens 2", removing)
	}
	if maxRemoved == 0 {
		t.Error("removedDirs blieb in allen Meldungen 0")
	}
}

// TestCopyProgressHasNoCleanupFields - Beim reinen Kopieren (ohne move) gibt
// es keine Lösch-Phase: in keiner Meldung removingDirs, removedDirs oder
// removeTotal.
func TestCopyProgressHasNoCleanupFields(t *testing.T) {
	req := copyRequest{src: newCopySource(), dst: newMemFS("/srv/b"), srcDir: "/home/a", dstDir: "/srv/b", names: []string{"projekt"}}
	progress, _ := collectCopyProgress(t, req)
	if len(progress) == 0 {
		t.Fatal("keine Fortschrittsmeldung")
	}
	for _, p := range progress {
		if p.removingDirs || p.removedDirs != 0 || p.removeTotal != 0 {
			t.Errorf("Lösch-Felder beim Kopieren gesetzt: %+v", p)
		}
	}
}

// --- Dialog ---

// progressDialogModel öffnet den Datei-Manager und zeigt den Fortschrittsdialog
// mit dem gegebenen Stand (ohne einen Kopiervorgang zu starten).
func progressDialogModel(t *testing.T, move bool, p copyProgress) AppModel {
	t.Helper()
	m := openCopyManager(t, newCopySource(), newMemFS("/srv/b"))
	m.fm.copy = fmCopyState{names: []string{"projekt"}, source: 0, move: move, copyProgress: p}
	m.fm.dialog = fmDialogCopying
	return m
}

// TestCopyingDialogShowsScanCounts - Beim Erfassen steht im Dialog der Text
// "Erfasse … %d Dateien, %d Verzeichnisse" mit den Zahlen in der Reihenfolge
// Dateien, Verzeichnisse (Deutsch und Englisch), beim Kopieren und
// Verschieben gleich.
func TestCopyingDialogShowsScanCounts(t *testing.T) {
	for _, move := range []bool{false, true} {
		for _, lang := range []Language{LangDeutsch, LangEnglish} {
			m := progressDialogModel(t, move, copyProgress{scanning: true, totalFiles: 1234, totalDirs: 56})
			m.language, m.lang = lang, GetTranslations(lang)
			want := fmt.Sprintf(m.tr(txtFMCopyScanned), 1234, 56)
			text := stripANSI(m.View())
			if !strings.Contains(text, want) {
				t.Errorf("move=%v %s: Dialog ohne %q:\n%s", move, lang, want, text)
			}
			if !strings.Contains(want, "1234") || strings.Index(want, "1234") > strings.Index(want, "56") {
				t.Errorf("%s: Reihenfolge der Zahlen falsch in %q (erst Dateien, dann Verzeichnisse)", lang, want)
			}
			assertScreenSize(t, m)
		}
	}
	m := progressDialogModel(t, false, copyProgress{scanning: true})
	if want := fmt.Sprintf(m.tr(txtFMCopyScanned), 0, 0); !strings.Contains(stripANSI(m.View()), want) {
		t.Errorf("Dialog ohne Nullstand %q", want)
	}
}

// TestMovingDialogShowsCleanupCounts - In der Lösch-Phase steht
// "Entferne leere Quellverzeichnisse … %d/%d" im Dialog (Deutsch und Englisch).
func TestMovingDialogShowsCleanupCounts(t *testing.T) {
	for _, lang := range []Language{LangDeutsch, LangEnglish} {
		m := progressDialogModel(t, true, copyProgress{removingDirs: true, removedDirs: 7, removeTotal: 61, doneFiles: 9, totalFiles: 9})
		m.language, m.lang = lang, GetTranslations(lang)
		want := fmt.Sprintf(m.tr(txtFMMoveCleanup), 7, 61)
		text := stripANSI(m.View())
		if !strings.Contains(text, want) {
			t.Errorf("%s: Dialog ohne %q:\n%s", lang, want, text)
		}
		assertScreenSize(t, m)
	}
}

// TestProgressDialogsKeepScreenSize - Der Dialog füllt in allen Farbmodi und
// bei kleinen Terminals genau Höhe mal Breite, auch mit riesigen Zahlen
// (Überlauf der Breite) - beim Erfassen und in der Lösch-Phase.
func TestProgressDialogsKeepScreenSize(t *testing.T) {
	states := map[string]copyProgress{
		"erfassen": {scanning: true, totalFiles: 987654321, totalDirs: 123456789},
		"löschen":  {removingDirs: true, removedDirs: 987654321, removeTotal: 987654321},
		"kopieren": {doneFiles: 5, totalFiles: 10, doneBytes: 5, totalBytes: 10, current: "/home/a/x"},
		"null":     {},
	}
	for name, p := range states {
		for theme := range fmThemes {
			for _, size := range [][2]int{{100, 30}, {40, 12}, {20, 8}} {
				m := progressDialogModel(t, name == "löschen", p)
				m.fm.theme = theme
				m.termWidth, m.termHeight = size[0], size[1]
				assertScreenSize(t, m)
			}
		}
	}
}

// TestProgressTextsInAllLanguages - Die beiden neuen Texte stimmen in allen
// 41 Sprachen mit den Zahlen überein: kein Formatfehler (%!d), beide Zahlen
// stehen im Text, und der Dialog zeigt die Zeile vollständig (nicht
// abgeschnitten) bei 100 Spalten.
func TestProgressTextsInAllLanguages(t *testing.T) {
	for _, opt := range AvailableLanguages {
		scan := progressDialogModel(t, false, copyProgress{scanning: true, totalFiles: 1234, totalDirs: 56})
		scan.language, scan.lang = opt.Code, GetTranslations(opt.Code)
		line := fmt.Sprintf(scan.tr(txtFMCopyScanned), 1234, 56)
		if strings.Contains(line, "%!") || !strings.Contains(line, "1234") || !strings.Contains(line, "56") {
			t.Errorf("%s: txtFMCopyScanned ergibt %q", opt.Code, line)
		}
		if !strings.Contains(stripANSI(scan.View()), line) {
			t.Errorf("%s: Zeile %q fehlt oder ist abgeschnitten", opt.Code, line)
		}
		clean := progressDialogModel(t, true, copyProgress{removingDirs: true, removedDirs: 7, removeTotal: 61})
		clean.language, clean.lang = opt.Code, GetTranslations(opt.Code)
		line = fmt.Sprintf(clean.tr(txtFMMoveCleanup), 7, 61)
		if strings.Contains(line, "%!") || !strings.Contains(line, "7") || !strings.Contains(line, "61") {
			t.Errorf("%s: txtFMMoveCleanup ergibt %q", opt.Code, line)
		}
		if !strings.Contains(stripANSI(clean.View()), line) {
			t.Errorf("%s: Zeile %q fehlt oder ist abgeschnitten", opt.Code, line)
		}
	}
}

// --- Umbruch statt Abschneiden ---

// squash entfernt alle Leerraumzeichen (Umbruchstellen ändern sie).
func squash(text string) string { return strings.Join(strings.Fields(text), "") }

// TestProgressLinesWrapInsteadOfTruncate - Die Zeilen "Erfasse …" und
// "Entferne leere Quellverzeichnisse …" werden bei schmaler Innenbreite
// umgebrochen: keine Zeile ist breiter als die Innenbreite, und aus den
// Teilzeilen setzt sich der ganze Text wieder zusammen (nichts abgeschnitten,
// auch nicht bei langen Wörtern ohne Leerzeichen wie im Chinesischen oder bei
// riesigen Zahlen). Alle 41 Sprachen, Innenbreiten 6 bis 60.
func TestProgressLinesWrapInsteadOfTruncate(t *testing.T) {
	for _, opt := range AvailableLanguages {
		for _, inner := range []int{6, 10, 14, 20, 32, 60} {
			scan := progressDialogModel(t, false, copyProgress{scanning: true, totalFiles: 1234567, totalDirs: 89})
			scan.language, scan.lang = opt.Code, GetTranslations(opt.Code)
			_, lines, _ := scan.fmDialogLines(inner)
			text := fmt.Sprintf(scan.tr(txtFMCopyScanned), 1234567, 89)
			assertWrapped(t, fmt.Sprintf("%s/erfassen/%d", opt.Code, inner), lines, text, inner)

			clean := progressDialogModel(t, true, copyProgress{removingDirs: true, removedDirs: 7, removeTotal: 3763})
			clean.language, clean.lang = opt.Code, GetTranslations(opt.Code)
			_, lines, _ = clean.fmDialogLines(inner)
			text = fmt.Sprintf(clean.tr(txtFMMoveCleanup), 7, 3763)
			// Letzte Zeile ist der Fortschrittsbalken
			assertWrapped(t, fmt.Sprintf("%s/entfernen/%d", opt.Code, inner), lines[:len(lines)-1], text, inner)
		}
	}
}

// assertWrapped prüft: jede Zeile höchstens inner breit, zusammengesetzt (ohne
// Leerraum) ergibt sich der Text.
func assertWrapped(t *testing.T, name string, lines []string, text string, inner int) {
	t.Helper()
	for _, line := range lines {
		if w := ansi.StringWidth(line); w > inner {
			t.Errorf("%s: Zeile %q ist %d breit, Innenbreite %d", name, line, w, inner)
		}
	}
	if got, want := squash(strings.Join(lines, " ")), squash(text); got != want {
		t.Errorf("%s: Text abgeschnitten oder verändert:\n erhalten: %q\n erwartet: %q", name, got, want)
	}
}

// TestProgressLinesWrapOnSmallScreen - Auf dem Bildschirm (40x12) steht die
// lange Zeile in Deutsch und Englisch vollständig im Dialog, auf mehrere
// Zeilen verteilt: kein Wort fehlt. Das Terminal behält genau 40x12.
func TestProgressLinesWrapOnSmallScreen(t *testing.T) {
	for _, lang := range []Language{LangDeutsch, LangEnglish} {
		for _, move := range []bool{false, true} {
			p := copyProgress{scanning: true, totalFiles: 1234, totalDirs: 56}
			if move {
				p = copyProgress{removingDirs: true, removedDirs: 7, removeTotal: 3763}
			}
			m := progressDialogModel(t, move, p)
			m.language, m.lang = lang, GetTranslations(lang)
			text := fmt.Sprintf(m.tr(txtFMCopyScanned), 1234, 56)
			if move {
				text = fmt.Sprintf(m.tr(txtFMMoveCleanup), 7, 3763)
			}
			m.termWidth, m.termHeight = 40, 12
			assertScreenSize(t, m)
			screen := stripANSI(m.View())
			for _, word := range strings.Fields(text) {
				if !strings.Contains(screen, word) {
					t.Errorf("%s move=%v: Wort %q fehlt im Dialog:\n%s", lang, move, word, screen)
				}
			}
		}
	}
}

// --- overlayAt und breite Zeichen ---

// wideBackground liefert eine Bildschirmzeile genau width breit: "cjk" nur aus
// doppelt breiten Zeichen (bei ungerader Breite mit einem Leerzeichen am
// Ende), "mixed" abwechselnd ASCII und doppelt breit.
func wideBackground(kind string, width int) string {
	var b strings.Builder
	used := 0
	for used < width {
		switch {
		case kind == "mixed" && (used/3)%2 == 0 && width-used >= 1:
			b.WriteString("a")
			used++
		case width-used >= 2:
			b.WriteString("漢")
			used += 2
		default:
			b.WriteString(" ")
			used++
		}
	}
	return b.String()
}

// TestOverlayAtKeepsLineWidthWithWideCharacters - Hintergrundzeilen voller
// CJK-Zeichen (auch mit Farbcodes, auch gemischt mit ASCII), ein Kasten mit
// gerader und ungerader linker Position und Breite, auch an der rechten
// Bildschirmkante: jede Zeile ist danach genau width breit
// (ansi.StringWidth), der Kasteninhalt steckt unverändert in der Zeile, die
// Zeile über dem Kasten bleibt unberührt. Fehler vor dem Fix: ein breites
// Zeichen an der rechten Kastenkante blieb halb stehen, die Zeile wurde eine
// Spalte zu breit.
func TestOverlayAtKeepsLineWidthWithWideCharacters(t *testing.T) {
	for _, width := range []int{20, 21, 40, 41} {
		for _, kind := range []string{"cjk", "mixed"} {
			for _, styled := range []bool{false, true} {
				for boxWidth := 4; boxWidth <= 9; boxWidth++ {
					for _, left := range []int{0, 1, 2, 3, 4, 5, width - boxWidth - 1, width - boxWidth} {
						row := wideBackground(kind, width)
						if styled {
							row = "\x1b[31m" + row + "\x1b[0m"
						}
						lines := []string{row, row, row, row}
						// Zweite Kastenzeile enthält selbst breite Zeichen
						box := []string{
							"┌" + strings.Repeat("─", boxWidth-2) + "┐",
							"│" + strings.Repeat("你", (boxWidth-2)/2) + strings.Repeat(" ", (boxWidth-2)%2) + "│",
						}
						overlayAt(lines, box, boxWidth, 1, left, width)
						name := fmt.Sprintf("Breite %d %s styled=%v Kasten %d bei %d", width, kind, styled, boxWidth, left)
						for i, line := range lines {
							if w := ansi.StringWidth(line); w != width {
								t.Errorf("%s: Zeile %d ist %d breit, erwartet %d: %q", name, i, w, width, stripANSI(line))
							}
						}
						for i, boxLine := range box {
							if !strings.Contains(lines[1+i], boxLine) {
								t.Errorf("%s: Kastenzeile %d fehlt in %q", name, i, stripANSI(lines[1+i]))
							}
						}
						if lines[0] != row || lines[3] != row {
							t.Errorf("%s: Zeilen außerhalb des Kastens verändert", name)
						}
					}
				}
			}
		}
	}
}

// TestOverlayAtReplacesHalfCoveredWideCharacter - Genaue Ergebnisse an den
// Schnittkanten (Bildschirm 10 Spalten, Zeile aus fünf breiten Zeichen):
//   - Kasten (3 breit) bei Spalte 2 (deckt 2-4): das Zeichen auf Spalte 4/5
//     liegt halb unter dem Kasten und wird zum Leerzeichen (der Fall des Fehlers)
//   - Kasten bei ungerader Spalte 3: das links halb verdeckte Zeichen wird zum
//     Leerzeichen (bisheriges Verhalten), rechts passt die Kante genau
//   - Kasten (2 breit) bei Spalte 3: beide Kanten schneiden ein Zeichen
//   - Kasten am linken und am rechten Rand (Kante genau auf Zeichengrenze) und
//     bei Spalte 5 (links halb verdecktes Zeichen)
func TestOverlayAtReplacesHalfCoveredWideCharacter(t *testing.T) {
	cases := []struct {
		boxWidth, left int
		want           string
	}{
		{3, 2, "漢[a] 漢漢"},
		{3, 3, "漢 [a]漢漢"},
		{2, 3, "漢 [] 漢漢"},
		{4, 0, "[ab]漢漢漢"},
		{4, 6, "漢漢漢[ab]"},
		{5, 5, "漢漢 [abc]"},
	}
	for _, c := range cases {
		row := wideBackground("cjk", 10)
		lines := []string{row}
		box := []string{"[" + "abcdefgh"[:c.boxWidth-2] + "]"}
		overlayAt(lines, box, c.boxWidth, 0, c.left, 10)
		if ansi.StringWidth(lines[0]) != 10 {
			t.Errorf("Kasten %d bei %d: Zeile %q ist %d breit", c.boxWidth, c.left, lines[0], ansi.StringWidth(lines[0]))
		}
		if lines[0] != c.want {
			t.Errorf("Kasten %d bei %d:\n erhalten: %q\n erwartet: %q", c.boxWidth, c.left, lines[0], c.want)
		}
	}
}

// TestCopyDialogsChineseKeepScreenSize - Der Kopieren-Dialog (Bestätigung
// nach F5, Fortschritt beim Erfassen, Kopieren und Löschen) in Chinesisch,
// Japanisch und Koreanisch füllt auch bei 40x12 (und 41, 39, 20x8) genau
// Höhe mal Breite - der Fall, der früher durch overlayAt eine Spalte zu
// breit wurde.
func TestCopyDialogsChineseKeepScreenSize(t *testing.T) {
	sizes := [][2]int{{100, 30}, {41, 12}, {40, 12}, {39, 12}, {20, 8}}
	for _, lang := range []Language{LangChinese, LangJapanese, LangKorean} {
		var models []AppModel
		for _, move := range []bool{false, true} {
			for _, p := range []copyProgress{
				{scanning: true, totalFiles: 1234, totalDirs: 56},
				{removingDirs: true, removedDirs: 7, removeTotal: 61},
				{doneFiles: 5, totalFiles: 10, doneBytes: 5, totalBytes: 10, current: "/home/a/文件夹/測試"},
			} {
				models = append(models, progressDialogModel(t, move, p))
			}
			m := openCopyManager(t, newCopySource(), newMemFS("/srv/b"))
			m = cursorTo(t, m, "notiz.txt")
			m = pressRun(t, m, map[bool]string{false: "f5", true: "f6"}[move])
			if m.fm.dialog != fmDialogCopyConfirm {
				t.Fatalf("%s: Dialog = %v, erwartet Bestätigung", lang, m.fm.dialog)
			}
			models = append(models, m)
		}
		for i, m := range models {
			m.language, m.lang = lang, GetTranslations(lang)
			for _, size := range sizes {
				m.termWidth, m.termHeight = size[0], size[1]
				t.Run(fmt.Sprintf("%s/%d/%dx%d", lang, i, size[0], size[1]), func(t *testing.T) { assertScreenSize(t, m) })
			}
		}
	}
}
