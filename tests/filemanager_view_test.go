// Paket main - Tests für den Betrachter des Datei-Managers (F3, Build 59)
//
// Prüft: Öffnen/Schließen (q, Esc, F3, F10), bereinigte Anzeige
// (Steuerzeichen, unsichtbare Zeichen, Tabs, CRLF), Blättern, Zeilenumbruch
// (F2) und waagerechtes Scrollen, Hex-Ansicht (automatisch bei Binärdaten,
// F4), Größenbegrenzung, F3 auf Verzeichnissen/Symlinks, Fehler und exakte
// Bildschirmgröße in allen Farbmodi.
//
// @author Kurt Ingwer
// @date   2026-10-21 21:24
package main

import (
	"fmt"
	"strings"
	"testing"
)

// openViewManager öffnet den Datei-Manager mit einer Quelle, in der die
// angegebenen Dateien liegen (Inhalt je Name).
func openViewManager(t *testing.T, files map[string]string) (AppModel, *memFS) {
	t.Helper()
	src := newCopySource()
	for name, content := range files {
		src.addFile("/home/a/"+name, content, 0o644, copyTestTime)
	}
	m := openCopyManager(t, src, newMemFS("/srv/b"))
	return m, src
}

// viewText liefert den Bildschirm ohne Farbcodes.
func viewText(m AppModel) string {
	return stripANSI(m.View())
}

// TestViewerOpensAndCloses - F3 öffnet, q/Esc/F3/F10 schließen den
// Betrachter (nicht den Datei-Manager).
func TestViewerOpensAndCloses(t *testing.T) {
	for _, key := range []string{"q", "esc", "f3", "f10"} {
		m, _ := openViewManager(t, map[string]string{"hallo.txt": "erste Zeile\nzweite Zeile\n"})
		m = cursorTo(t, m, "hallo.txt")
		m = pressRun(t, m, "f3")
		if !m.fm.viewing {
			t.Fatalf("%s: Betrachter nicht geöffnet (Meldung %q)", key, m.fm.message)
		}
		text := viewText(m)
		if !strings.Contains(text, "erste Zeile") || !strings.Contains(text, "zweite Zeile") {
			t.Errorf("%s: Inhalt fehlt:\n%s", key, text)
		}
		if !strings.Contains(strings.Split(text, "\n")[0], "/home/a/hallo.txt") {
			t.Errorf("%s: Kopfzeile ohne Pfad: %q", key, strings.Split(text, "\n")[0])
		}
		assertScreenSize(t, m)
		m = pressRun(t, m, key)
		if m.fm.viewing || m.state != ViewFileManager {
			t.Errorf("%s: viewing=%v state=%v, erwartet zurück in den Fenstern", key, m.fm.viewing, m.state)
		}
	}
}

// TestViewerSanitizesContent - Steuersequenzen, unsichtbare Zeichen, Tabs
// und CRLF werden sicher dargestellt.
func TestViewerSanitizesContent(t *testing.T) {
	content := "a\tb\r\nboese\x1b[2J\x1b]0;titel\x07\nrechnung\u202efdp.exe\nende"
	m, _ := openViewManager(t, map[string]string{"x.txt": content})
	m = cursorTo(t, m, "x.txt")
	m = pressRun(t, m, "f3")
	raw := m.View()
	if strings.ContainsAny(raw, "\x1b\x07\r\u202e") {
		t.Errorf("unbereinigte Zeichen in der Ausgabe: %q", raw)
	}
	text := viewText(m)
	for _, want := range []string{"a       b", "boese?[2J?]0;titel?", "rechnung?fdp.exe", "ende"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q fehlt:\n%s", want, text)
		}
	}
}

// TestViewerScrolling - Pfeile, Bildtasten, Pos1/Ende; Kopfzeile zeigt die Zeile.
func TestViewerScrolling(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&b, "Zeile %03d\n", i)
	}
	m, _ := openViewManager(t, map[string]string{"lang.txt": b.String()})
	m.termWidth, m.termHeight = 80, 24
	m = cursorTo(t, m, "lang.txt")
	m = pressRun(t, m, "f3")
	rows := 24 - 2
	first := func() string {
		return strings.TrimSpace(strings.Split(viewText(m), "\n")[1])
	}
	if first() != "Zeile 001" {
		t.Fatalf("erste Zeile = %q", first())
	}
	m = pressRun(t, m, "down")
	if first() != "Zeile 002" {
		t.Errorf("nach ↓: %q", first())
	}
	m = pressRun(t, m, "pgdown")
	if want := fmt.Sprintf("Zeile %03d", 2+rows); first() != want {
		t.Errorf("nach Bild↓: %q, erwartet %q", first(), want)
	}
	m = pressRun(t, m, "end")
	text := viewText(m)
	if !strings.Contains(text, "Zeile 100") || strings.Contains(text, "Zeile 001") {
		t.Errorf("Ende zeigt nicht das Dateiende:\n%s", text)
	}
	m = pressRun(t, m, "down")
	if !strings.Contains(viewText(m), "Zeile 100") {
		t.Error("über das Ende hinaus gescrollt")
	}
	m = pressRun(t, m, "home")
	if first() != "Zeile 001" {
		t.Errorf("nach Pos1: %q", first())
	}
	m = pressRun(t, m, "up")
	if first() != "Zeile 001" {
		t.Errorf("über den Anfang hinaus gescrollt: %q", first())
	}
	// Abschließender Zeilenumbruch erzeugt keine leere 101. Zeile
	if header := strings.Split(viewText(m), "\n")[0]; !strings.Contains(header, fmt.Sprintf(m.tr(txtFMViewLine), 1, 100)) {
		t.Errorf("Kopfzeile ohne Zeilenangabe: %q", header)
	}
	assertScreenSize(t, m)
}

// TestViewerWrapAndHorizontalScroll - lange Zeilen werden umbrochen; F2
// schaltet den Umbruch ab, dann scrollen ←/→ waagerecht.
func TestViewerWrapAndHorizontalScroll(t *testing.T) {
	long := strings.Repeat("abcdefghij", 20) + "ENDE"
	m, _ := openViewManager(t, map[string]string{"breit.txt": long + "\nzweite"})
	m.termWidth, m.termHeight = 80, 24
	m = cursorTo(t, m, "breit.txt")
	m = pressRun(t, m, "f3")
	lines := strings.Split(viewText(m), "\n")
	if !strings.Contains(lines[3], "ENDE") || !strings.Contains(lines[4], "zweite") {
		t.Errorf("Umbruch falsch:\n%s", strings.Join(lines, "\n"))
	}
	// ↓ im Umbruch: eine Bildschirmzeile weiter
	m = pressRun(t, m, "down")
	if got := strings.Split(viewText(m), "\n")[1]; !strings.HasPrefix(got, long[80:90]) {
		t.Errorf("nach ↓ im Umbruch: %q", got)
	}
	m = pressRun(t, m, "home")
	m = pressRun(t, m, "f2")
	lines = strings.Split(viewText(m), "\n")
	if strings.Contains(lines[1], "ENDE") || !strings.Contains(lines[2], "zweite") {
		t.Errorf("ohne Umbruch falsch:\n%s", strings.Join(lines, "\n"))
	}
	for i := 0; i < 20; i++ {
		m = pressRun(t, m, "right")
	}
	if !strings.Contains(strings.Split(viewText(m), "\n")[1], "ENDE") {
		t.Errorf("→ scrollt nicht:\n%s", viewText(m))
	}
	m = pressRun(t, m, "home")
	if strings.Contains(strings.Split(viewText(m), "\n")[1], "ENDE") {
		t.Error("Pos1 setzt die waagerechte Position nicht zurück")
	}
	assertScreenSize(t, m)
}

// TestViewerHexMode - Binärdaten erscheinen als Hex; F4 schaltet um.
func TestViewerHexMode(t *testing.T) {
	m, _ := openViewManager(t, map[string]string{"bild.bin": "AB\x00\x01\xffZ"})
	m.termWidth, m.termHeight = 100, 30
	m = cursorTo(t, m, "bild.bin")
	m = pressRun(t, m, "f3")
	if !m.fm.view.hex {
		t.Fatal("Binärdatei nicht automatisch als Hex")
	}
	text := viewText(m)
	if !strings.Contains(text, "00000000") || !strings.Contains(text, "41 42 00 01 ff 5a") || !strings.Contains(text, "AB...Z") {
		t.Errorf("Hex-Ansicht falsch:\n%s", text)
	}
	assertScreenSize(t, m)
	m = pressRun(t, m, "f4")
	if m.fm.view.hex {
		t.Error("F4 schaltet nicht auf Text")
	}
	m = pressRun(t, m, "f4")
	if !m.fm.view.hex {
		t.Error("F4 schaltet nicht zurück auf Hex")
	}
	// Schmale Fenster: weniger Bytes pro Zeile, Breite bleibt exakt
	for _, w := range []int{40, 60, 81} {
		m.termWidth = w
		assertScreenSize(t, m)
	}
}

// TestViewerHexScrolling - Hex-Ansicht blättert zeilenweise bis zum Ende.
func TestViewerHexScrolling(t *testing.T) {
	m, _ := openViewManager(t, map[string]string{"gross.bin": strings.Repeat("\x00", 16*100)})
	m.termWidth, m.termHeight = 100, 24
	m = cursorTo(t, m, "gross.bin")
	m = pressRun(t, m, "f3")
	m = pressRun(t, m, "end")
	text := viewText(m)
	if !strings.Contains(text, "00000630") || strings.Contains(text, "00000000 ") {
		t.Errorf("Ende der Hex-Ansicht falsch:\n%s", text)
	}
	m = pressRun(t, m, "home")
	if !strings.Contains(viewText(m), "00000000") {
		t.Error("Pos1 in Hex")
	}
}

// TestViewerSizeLimit - große Dateien werden nur bis zur Grenze geladen.
func TestViewerSizeLimit(t *testing.T) {
	defer func(old int64) { fmViewMaxBytes = old }(fmViewMaxBytes)
	fmViewMaxBytes = 100
	m, _ := openViewManager(t, map[string]string{"riesig.txt": strings.Repeat("x", 1000)})
	m = cursorTo(t, m, "riesig.txt")
	m = pressRun(t, m, "f3")
	if len(m.fm.view.data) != 100 || !m.fm.view.truncated {
		t.Errorf("geladen %d Bytes, gekürzt %v", len(m.fm.view.data), m.fm.view.truncated)
	}
	if !strings.Contains(strings.Split(viewText(m), "\n")[0], fmt.Sprintf(m.tr(txtFMViewTruncated), formatSizeWithUnit(100))) {
		t.Errorf("Hinweis auf Kürzung fehlt: %q", strings.Split(viewText(m), "\n")[0])
	}
}

// TestViewerEmptyFile - leere Datei, kein Absturz.
func TestViewerEmptyFile(t *testing.T) {
	m, _ := openViewManager(t, map[string]string{"leer.txt": ""})
	m = cursorTo(t, m, "leer.txt")
	m = pressRun(t, m, "f3")
	for _, key := range []string{"down", "pgdown", "end", "f2", "right", "f4", "home"} {
		m = pressRun(t, m, key)
		assertScreenSize(t, m)
	}
}

// TestViewerF3OnDirectoriesAndLinks - F3 auf Verzeichnis = Enter; auf
// Verzeichnis-Symlink Meldung; Symlink auf Datei zeigt deren Inhalt.
func TestViewerF3OnDirectoriesAndLinks(t *testing.T) {
	m, src := openViewManager(t, nil)
	src.addLink("/home/a/linkdir", "projekt")
	m = pressRun(t, m, "f10")
	m = openCopyManager(t, src, newMemFS("/srv/b"))
	m = cursorTo(t, m, "projekt")
	m = pressRun(t, m, "f3")
	if m.fm.viewing || m.fm.panels[0].path != "/home/a/projekt" {
		t.Errorf("F3 auf Verzeichnis: viewing=%v Pfad=%q", m.fm.viewing, m.fm.panels[0].path)
	}
	m = cursorTo(t, m, "aktuell") // Symlink auf main.go
	m = pressRun(t, m, "f3")
	if !m.fm.viewing || !strings.Contains(viewText(m), "package main") {
		t.Errorf("Symlink auf Datei nicht angezeigt (Meldung %q)", m.fm.message)
	}
	m = pressRun(t, m, "q")
	m = cursorTo(t, m, "..")
	m = pressRun(t, m, "f3")
	if m.fm.panels[0].path != "/home/a" {
		t.Errorf("F3 auf ..: Pfad %q", m.fm.panels[0].path)
	}
	m = cursorTo(t, m, "linkdir")
	m = pressRun(t, m, "f3")
	if m.fm.viewing || m.fm.message != fmt.Sprintf(m.tr(txtFMErrSymlink), "linkdir") {
		t.Errorf("F3 auf Verzeichnis-Symlink: viewing=%v Meldung %q", m.fm.viewing, m.fm.message)
	}
}

// TestViewerReadError - nicht lesbare Datei: Meldung, kein Betrachter.
func TestViewerReadError(t *testing.T) {
	m, src := openViewManager(t, map[string]string{"geheim.txt": "x"})
	src.denied["/home/a/geheim.txt"] = true
	m = cursorTo(t, m, "geheim.txt")
	m = pressRun(t, m, "f3")
	if m.fm.viewing {
		t.Error("Betrachter trotz Lesefehler offen")
	}
	if !strings.Contains(m.fm.message, "geheim.txt") {
		t.Errorf("Meldung = %q", m.fm.message)
	}
}

// TestViewerIgnoresPanelKeys - im Betrachter wirken Fenster-Tasten nicht.
func TestViewerIgnoresPanelKeys(t *testing.T) {
	m, _ := openViewManager(t, map[string]string{"x.txt": "x"})
	m = cursorTo(t, m, "x.txt")
	m = pressRun(t, m, "f3")
	for _, key := range []string{"insert", "*", "tab", "f5", "enter"} {
		m = pressRun(t, m, key)
	}
	if !m.fm.viewing || m.fm.active != 0 || len(markedNames(m.fm.panels[0])) != 0 || m.fm.dialog != fmDialogNone {
		t.Errorf("Fenster-Tasten wirken im Betrachter: viewing=%v aktiv=%d markiert=%v Dialog=%v",
			m.fm.viewing, m.fm.active, markedNames(m.fm.panels[0]), m.fm.dialog)
	}
}

// TestViewerStaleLoadIgnored - Ergebnis eines geschlossenen Betrachters
// wird verworfen.
func TestViewerStaleLoadIgnored(t *testing.T) {
	m, _ := openViewManager(t, map[string]string{"a.txt": "AAA", "b.txt": "BBB"})
	m = cursorTo(t, m, "a.txt")
	updated, cmd := m.Update(fmKey("f3"))
	m = updated.(AppModel)
	m = pressRun(t, m, "q")
	m = cursorTo(t, m, "b.txt")
	m = pressRun(t, m, "f3")
	updated, _ = m.Update(cmd())
	m = updated.(AppModel)
	if !strings.Contains(viewText(m), "BBB") || strings.Contains(viewText(m), "AAA") {
		t.Errorf("veraltetes Ergebnis angezeigt:\n%s", viewText(m))
	}
}

// TestViewerAllThemesAndSizes - exakte Bildschirmgröße überall.
func TestViewerAllThemesAndSizes(t *testing.T) {
	withColorProfile(t)
	m, _ := openViewManager(t, map[string]string{"x.txt": strings.Repeat("Text mit 日本語 und Tab\there\n", 50)})
	m = cursorTo(t, m, "x.txt")
	m = pressRun(t, m, "f3")
	for theme := range fmThemes {
		m.fm.theme = theme
		for _, size := range [][2]int{{100, 30}, {80, 24}, {40, 12}, {200, 60}, {20, 5}} {
			m.termWidth, m.termHeight = size[0], size[1]
			assertScreenSize(t, m)
			m = pressRun(t, m, "f2")
			assertScreenSize(t, m)
			m = pressRun(t, m, "f4")
			assertScreenSize(t, m)
			m = pressRun(t, m, "f4")
		}
	}
}

// TestSegmentLine prüft das Umbrechen nach Zellbreite (breite Zeichen).
func TestSegmentLine(t *testing.T) {
	cases := []struct {
		in    string
		width int
		want  []string
	}{
		{"", 5, []string{""}},
		{"abc", 5, []string{"abc"}},
		{"abcdef", 3, []string{"abc", "def"}},
		{"abcdefg", 3, []string{"abc", "def", "g"}},
		{"日本語", 4, []string{"日本", "語"}},
		{"a日本", 2, []string{"a", "日", "本"}},
		{"abc", 0, []string{"abc"}},
	}
	for _, c := range cases {
		got := segmentLine(c.in, c.width)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("segmentLine(%q, %d) = %q, erwartet %q", c.in, c.width, got, c.want)
		}
	}
}

// TestPrepareViewText prüft Tab-Erweiterung und Bereinigung.
func TestPrepareViewText(t *testing.T) {
	cases := map[string]string{
		"a\tb":        "a       b",
		"abcdefgh\tx": "abcdefgh        x",
		"\t":          "        ",
		"x\r":         "x",
		"\x1b[31m":    "?[31m",
	}
	for in, want := range cases {
		if got := prepareViewLine(in); got != want {
			t.Errorf("prepareViewLine(%q) = %q, erwartet %q", in, got, want)
		}
	}
}

// TestLooksBinary prüft die Binär-Erkennung.
func TestLooksBinary(t *testing.T) {
	if looksBinary([]byte("Hallo\nWelt\tmit Tab äöü")) {
		t.Error("Text als binär erkannt")
	}
	if !looksBinary([]byte("PNG\x00\x01\x02")) {
		t.Error("NUL-Bytes nicht als binär erkannt")
	}
	if looksBinary(nil) {
		t.Error("leere Datei als binär erkannt")
	}
}

// --- mc-Tasten im Betrachter (Build 60) ---

// typeText tippt einen Text Zeichen für Zeichen (ohne die Blink-Kommandos
// der Eingabezeile auszuführen - die warten jeweils eine halbe Sekunde).
func typeText(t *testing.T, m AppModel, text string) AppModel {
	t.Helper()
	for _, r := range text {
		m = press(t, m, string(r))
	}
	return m
}

// openLongView öffnet eine Datei mit 100 Zeilen "Zeile NNN".
func openLongView(t *testing.T) AppModel {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&b, "Zeile %03d\n", i)
	}
	m, _ := openViewManager(t, map[string]string{"lang.txt": b.String()})
	m.termWidth, m.termHeight = 80, 24
	m = cursorTo(t, m, "lang.txt")
	return pressRun(t, m, "f3")
}

// firstRow liefert die erste Inhaltszeile.
func firstRow(m AppModel) string {
	return strings.TrimSpace(strings.Split(viewText(m), "\n")[1])
}

// TestViewerKeyBarLikeMC - unten die F-Tasten-Leiste wie im mc.
func TestViewerKeyBarLikeMC(t *testing.T) {
	m := openLongView(t)
	m.termWidth = 100 // bei 80 Spalten werden lange Beschriftungen gekürzt
	lines := strings.Split(viewText(m), "\n")
	bar := lines[len(lines)-1]
	labels := strings.Split(m.tr(txtFMViewFKeys), "|")
	if len(labels) != 10 {
		t.Fatalf("%d Beschriftungen, erwartet 10", len(labels))
	}
	for i, label := range labels {
		if want := fmt.Sprintf("%d%s", i+1, label); !strings.Contains(bar, want) {
			t.Errorf("Leiste ohne %q: %q", want, bar)
		}
	}
	assertScreenSize(t, m)
}

// TestViewerF1Help - F1 zeigt die Hilfe, eine beliebige Taste schließt sie.
func TestViewerF1Help(t *testing.T) {
	m := openLongView(t)
	m = pressRun(t, m, "f1")
	text := viewText(m)
	if !strings.Contains(text, m.tr(txtFMHelpTitle)) || !strings.Contains(text, "F7") {
		t.Errorf("Hilfe fehlt:\n%s", text)
	}
	assertScreenSize(t, m)
	m = pressRun(t, m, "q") // schließt nur die Hilfe
	if !m.fm.viewing || m.fm.view.showHelp {
		t.Errorf("viewing=%v Hilfe=%v", m.fm.viewing, m.fm.view.showHelp)
	}
}

// TestViewerF5Goto - F5 springt zu einer Zeile; ungültige Eingabe meldet sich.
func TestViewerF5Goto(t *testing.T) {
	m := openLongView(t)
	m = pressRun(t, m, "f5")
	if !strings.Contains(viewText(m), m.tr(txtFMGotoLine)) {
		t.Error("Eingabezeile fehlt")
	}
	assertScreenSize(t, m)
	m = typeText(t, m, "50")
	m = pressRun(t, m, "enter")
	if firstRow(m) != "Zeile 050" {
		t.Errorf("nach Gehe zu 50: %q", firstRow(m))
	}
	m = pressRun(t, m, "f5")
	m = typeText(t, m, "abc")
	m = pressRun(t, m, "enter")
	if !strings.Contains(viewText(m), fmt.Sprintf(m.tr(txtFMBadNumber), "abc")) || firstRow(m) != "Zeile 050" {
		t.Errorf("ungültige Eingabe:\n%s", viewText(m))
	}
	// Esc bricht die Eingabe ab, ohne den Betrachter zu schließen; q wird getippt
	m = pressRun(t, m, "f5")
	m = pressRun(t, m, "q")
	if !m.fm.viewing || m.fm.view.prompt == fmPromptNone {
		t.Fatal("q in der Eingabe schließt")
	}
	m = pressRun(t, m, "esc")
	if !m.fm.viewing || m.fm.view.prompt != fmPromptNone {
		t.Errorf("Esc: viewing=%v prompt=%v", m.fm.viewing, m.fm.view.prompt)
	}
}

// TestViewerF7SearchAndNext - F7 sucht (ohne Groß-/Kleinschreibung), n
// sucht weiter; nicht gefunden meldet sich.
func TestViewerF7SearchAndNext(t *testing.T) {
	m := openLongView(t)
	m = pressRun(t, m, "f7")
	if !strings.Contains(viewText(m), m.tr(txtFMSearchPrompt)) {
		t.Error("Suchzeile fehlt")
	}
	m = typeText(t, m, "ZEILE 07")
	m = pressRun(t, m, "enter")
	if firstRow(m) != "Zeile 070" {
		t.Errorf("Treffer: %q", firstRow(m))
	}
	m = pressRun(t, m, "n")
	if firstRow(m) != "Zeile 071" {
		t.Errorf("weitersuchen: %q", firstRow(m))
	}
	// Vorbelegt mit dem letzten Begriff; das erste Zeichen ersetzt ihn (wie im mc)
	m = pressRun(t, m, "f7")
	if m.fm.view.input.Value() != "ZEILE 07" {
		t.Errorf("Suchfeld nicht vorbelegt: %q", m.fm.view.input.Value())
	}
	m = typeText(t, m, "gibtsnicht")
	m = pressRun(t, m, "enter")
	if !strings.Contains(viewText(m), fmt.Sprintf(m.tr(txtFMNotFound), "gibtsnicht")) {
		t.Errorf("Meldung fehlt:\n%s", viewText(m))
	}
	if firstRow(m) != "Zeile 071" {
		t.Errorf("Position nach erfolgloser Suche verändert: %q", firstRow(m))
	}
	assertScreenSize(t, m)
}

// TestViewerF6RegexSearch - F6 sucht mit regulärem Ausdruck.
func TestViewerF6RegexSearch(t *testing.T) {
	m := openLongView(t)
	m = pressRun(t, m, "f6")
	if !strings.Contains(viewText(m), m.tr(txtFMRegexPrompt)) {
		t.Error("Eingabezeile fehlt")
	}
	m = typeText(t, m, "zeile 0[89]9")
	m = pressRun(t, m, "enter")
	if firstRow(m) != "Zeile 089" {
		t.Errorf("Treffer: %q", firstRow(m))
	}
	m = pressRun(t, m, "n")
	if firstRow(m) != "Zeile 099" {
		t.Errorf("weiter: %q", firstRow(m))
	}
	m = pressRun(t, m, "f6")
	m = typeText(t, m, "(")
	m = pressRun(t, m, "enter")
	prefix := strings.SplitN(m.tr(txtFMBadRegex), "%", 2)[0]
	if !strings.Contains(viewText(m), strings.TrimSpace(prefix)) {
		t.Errorf("ungültiger Ausdruck ohne Meldung:\n%s", viewText(m))
	}
}

// TestViewerSearchHighlightsMatch - die Trefferzeile ist hervorgehoben.
func TestViewerSearchHighlightsMatch(t *testing.T) {
	withColorProfile(t)
	m := openLongView(t)
	m = pressRun(t, m, "f7")
	m = typeText(t, m, "zeile 042")
	m = pressRun(t, m, "enter")
	st := fmThemeStyles(m.fm.theme)
	cursor := st.cursor.Render("X")
	cursor = cursor[:strings.Index(cursor, "X")]
	row := strings.Split(m.View(), "\n")[1]
	if !strings.HasPrefix(row, cursor) {
		t.Errorf("Trefferzeile nicht hervorgehoben: %q", row)
	}
}

// TestViewerHexGotoAndSearch - in der Hex-Ansicht: Offset und Byte-Suche.
func TestViewerHexGotoAndSearch(t *testing.T) {
	data := strings.Repeat("\x00", 16*50) + "MARKE" + strings.Repeat("\x00", 16*50)
	m, _ := openViewManager(t, map[string]string{"bin.dat": data})
	m.termWidth, m.termHeight = 100, 24
	m = cursorTo(t, m, "bin.dat")
	m = pressRun(t, m, "f3")
	m = pressRun(t, m, "f5")
	if !strings.Contains(viewText(m), m.tr(txtFMGotoOffset)) {
		t.Error("Offset-Eingabe fehlt")
	}
	m = typeText(t, m, "0x100")
	m = pressRun(t, m, "enter")
	if !strings.HasPrefix(firstRow(m), "00000100") {
		t.Errorf("Gehe zu 0x100: %q", firstRow(m))
	}
	m = pressRun(t, m, "home")
	m = pressRun(t, m, "f7")
	m = typeText(t, m, "marke")
	m = pressRun(t, m, "enter")
	if !strings.HasPrefix(firstRow(m), "00000320") {
		t.Errorf("Byte-Suche: %q", firstRow(m))
	}
}

// TestViewerI18nKeyBar - zehn Teile in jeder Sprache.
func TestViewerI18nKeyBar(t *testing.T) {
	for _, opt := range AvailableLanguages {
		if n := len(strings.Split(extraText(opt.Code, txtFMViewFKeys), "|")); n != 10 {
			t.Errorf("%s: %d Teile, erwartet 10", opt.Code, n)
		}
	}
}
