// Paket main - Tests für die Menüs des Datei-Managers (Build 72)
//
// Prüft die Menüleiste wie im mc (F9): Links, Datei, Optionen, Rechts -
// "Befehl" entfällt, weil ssh-easy davon nichts anbietet. Außerdem:
//   - Navigation (←/→ wechseln das Menü, ↑/↓ die Einträge, Enter, Esc)
//   - jeder Eintrag löst dieselbe Funktion aus wie seine Taste
//   - Neu einlesen (auch Strg+R)
//   - Optionen: Aussehen (Farbmodus, gespeichert) und Endungen für F9
//     (gespeichert; riskante Endungen nur nach kräftigem Risikohinweis)
//   - F9 im Betrachter fragt bei riskanten Endungen "Wirklich öffnen?"
//
// @author Kurt Ingwer
// @date   2026-10-22 07:40
package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// openMenuManager öffnet den Datei-Manager mit zwei memFS.
func openMenuManager(t *testing.T) (AppModel, *memFS, *memFS) {
	t.Helper()
	src, dst := newCopySource(), newMemFS("/srv/b")
	return openCopyManager(t, src, dst), src, dst
}

// menuChoose öffnet mit F9 das Menü, wechselt zum Menü index und wählt
// den Eintrag mit der Beschriftung label (Enter, Folgekommandos laufen).
func menuChoose(t *testing.T, m AppModel, index int, label string) AppModel {
	t.Helper()
	m = pressRun(t, m, "f9")
	if !m.fm.menuOpen {
		t.Fatal("F9 öffnet kein Menü")
	}
	for i := 0; i < 8 && m.fm.menuIndex != index; i++ {
		m = pressRun(t, m, "right")
	}
	if m.fm.menuIndex != index {
		t.Fatalf("Menü %d nicht erreicht", index)
	}
	items := m.fmMenus()[index].items
	for i := 0; i < 20; i++ {
		if item := items[m.fm.menuItem]; m.tr(item.label) == label {
			return pressRun(t, m, "enter")
		}
		m = pressRun(t, m, "down")
	}
	t.Fatalf("Eintrag %q nicht gefunden", label)
	return m
}

// TestFileManagerMenuBarTitles - vier Menüs wie im mc, ohne "Befehl".
func TestFileManagerMenuBarTitles(t *testing.T) {
	m, _, _ := openMenuManager(t)
	bar := strings.Split(stripANSI(m.View()), "\n")[0]
	for _, title := range strings.Split(m.tr(txtFMMenuBar), "|") {
		if !strings.Contains(bar, title) {
			t.Errorf("Menü %q fehlt in %q", title, bar)
		}
	}
	if parts := strings.Split(m.tr(txtFMMenuBar), "|"); len(parts) != 4 || len(m.fmMenus()) != 4 {
		t.Errorf("Menüs = %v / %d, erwartet 4", parts, len(m.fmMenus()))
	}
	if strings.Contains(bar, "Befehl") {
		t.Errorf("Menü 'Befehl' noch vorhanden: %q", bar)
	}
}

// TestFileManagerF9OpensMenuOfActivePanel - F9 öffnet das Menü des aktiven
// Fensters (links: "Links", rechts: "Rechts").
func TestFileManagerF9OpensMenuOfActivePanel(t *testing.T) {
	m, _, _ := openMenuManager(t)
	m = pressRun(t, m, "f9")
	if !m.fm.menuOpen || m.fm.menuIndex != 0 || m.fm.menuItem != 0 {
		t.Fatalf("Menü offen %v, Index %d, Eintrag %d", m.fm.menuOpen, m.fm.menuIndex, m.fm.menuItem)
	}
	if !strings.Contains(stripANSI(m.View()), m.tr(txtFMMenuRescan)) {
		t.Error("Aufklappmenü nicht sichtbar")
	}
	assertScreenSize(t, m)
	m = pressRun(t, m, "esc")
	m = pressRun(t, m, "tab")
	m = pressRun(t, m, "f9")
	if m.fm.menuIndex != 3 {
		t.Errorf("rechts aktiv: Menü %d, erwartet 3 (Rechts)", m.fm.menuIndex)
	}
}

// TestFileManagerMenuNavigation - Wechsel mit Wrap, Trenner werden
// übersprungen, Esc/F9/F10/q schließen nur das Menü.
func TestFileManagerMenuNavigation(t *testing.T) {
	m, _, _ := openMenuManager(t)
	m = pressRun(t, m, "f9")
	m = pressRun(t, m, "left")
	if m.fm.menuIndex != 3 {
		t.Errorf("← von Links: Menü %d, erwartet 3", m.fm.menuIndex)
	}
	m = pressRun(t, m, "right")
	m = pressRun(t, m, "right")
	if m.fm.menuIndex != 1 || m.fm.menuItem != 0 {
		t.Fatalf("Menü %d Eintrag %d, erwartet Datei/0", m.fm.menuIndex, m.fm.menuItem)
	}
	items := m.fmMenus()[1].items
	for i := 0; i < 2*len(items); i++ {
		m = pressRun(t, m, "down")
		if items[m.fm.menuItem].label == "" {
			t.Fatal("Cursor auf einem Trenner")
		}
	}
	m = pressRun(t, m, "up")
	if items[m.fm.menuItem].label == "" {
		t.Fatal("Cursor auf einem Trenner")
	}
	assertScreenSize(t, m)
	m = pressRun(t, m, "esc")
	for _, key := range []string{"esc", "f9", "f10", "q"} {
		m = pressRun(t, m, "f9")
		if !m.fm.menuOpen {
			t.Fatalf("%s: Menü nicht offen", key)
		}
		m = pressRun(t, m, key)
		if m.fm.menuOpen || m.state != ViewFileManager {
			t.Errorf("%s: Menü offen %v, state %v", key, m.fm.menuOpen, m.state)
		}
	}
}

// TestFileManagerFileMenuItems - jeder Eintrag des Datei-Menüs wirkt wie
// seine Taste.
func TestFileManagerFileMenuItems(t *testing.T) {
	check := func(label textKey, prepare func(AppModel) AppModel, ok func(AppModel) bool) {
		t.Helper()
		m, _, _ := openMenuManager(t)
		if prepare != nil {
			m = prepare(m)
		}
		m = menuChoose(t, m, 1, m.tr(label))
		if m.fm.menuOpen || !ok(m) {
			t.Errorf("%s: nicht ausgeführt (Dialog %v, state %v, Meldung %q)", m.tr(label), m.fm.dialog, m.state, m.fm.message)
		}
	}
	onFile := func(m AppModel) AppModel { return cursorTo(t, m, "notiz.txt") }
	check(txtFMMenuView, onFile, func(m AppModel) bool { return m.fm.viewing })
	check(txtFMMenuEdit, onFile, func(m AppModel) bool { return m.fm.editing })
	check(txtFMMenuCopy, onFile, func(m AppModel) bool { return m.fm.dialog == fmDialogCopyConfirm && !m.fm.copy.move })
	check(txtFMMenuMove, onFile, func(m AppModel) bool { return m.fm.dialog == fmDialogCopyConfirm && m.fm.copy.move })
	check(txtFMMenuMkdir, nil, func(m AppModel) bool { return m.fm.dialog == fmDialogMkdir })
	check(txtFMMenuDelete, onFile, func(m AppModel) bool { return m.fm.dialog == fmDialogDeleteConfirm })
	check(txtFMMenuInvert, nil, func(m AppModel) bool { return len(markedNames(m.fm.panels[0])) == 2 })
	check(txtFMMenuExit, nil, func(m AppModel) bool { return m.state == ViewList })
}

// TestFileManagerMenuRescan - "Neu einlesen" im Menü Links/Rechts und
// Strg+R lesen das Fenster neu; Cursor und Markierungen bleiben.
func TestFileManagerMenuRescan(t *testing.T) {
	m, src, dst := openMenuManager(t)
	m = cursorTo(t, m, "notiz.txt")
	m = pressRun(t, m, "insert")
	m = cursorTo(t, m, "notiz.txt")
	src.addFile("/home/a/neu.txt", "n", 0o644, copyTestTime)
	dst.addFile("/srv/b/rechts.txt", "r", 0o644, copyTestTime)
	m = menuChoose(t, m, 0, m.tr(txtFMMenuRescan))
	if !strings.Contains(strings.Join(entryNames(m.fm.panels[0]), ","), "neu.txt") {
		t.Errorf("links nicht neu eingelesen: %v", entryNames(m.fm.panels[0]))
	}
	if entry, _ := m.fm.panels[0].current(); entry.name != "notiz.txt" {
		t.Errorf("Cursor auf %q, erwartet notiz.txt", entry.name)
	}
	if got := strings.Join(markedNames(m.fm.panels[0]), ","); got != "notiz.txt" {
		t.Errorf("Markierungen = %q", got)
	}
	if strings.Contains(strings.Join(entryNames(m.fm.panels[1]), ","), "rechts.txt") {
		t.Error("rechtes Fenster mit eingelesen")
	}
	m = menuChoose(t, m, 3, m.tr(txtFMMenuRescan))
	if !strings.Contains(strings.Join(entryNames(m.fm.panels[1]), ","), "rechts.txt") {
		t.Errorf("rechts nicht neu eingelesen: %v", entryNames(m.fm.panels[1]))
	}
	src.addFile("/home/a/strg.txt", "s", 0o644, copyTestTime)
	m = pressRun(t, m, "ctrl+r")
	if !strings.Contains(strings.Join(entryNames(m.fm.panels[0]), ","), "strg.txt") {
		t.Errorf("Strg+R liest nicht neu ein: %v", entryNames(m.fm.panels[0]))
	}
}

// TestFileManagerThemeDialog - Optionen -> Aussehen: Auswahl, Enter
// übernimmt und speichert, Esc verwirft; ein neuer Start übernimmt ihn.
func TestFileManagerThemeDialog(t *testing.T) {
	m, _, _ := openMenuManager(t)
	m = menuChoose(t, m, 2, m.tr(txtFMMenuAppearance))
	if m.fm.dialog != fmDialogTheme || m.fm.themeCursor != m.fm.theme {
		t.Fatalf("Dialog %v, Cursor %d", m.fm.dialog, m.fm.themeCursor)
	}
	text := stripANSI(m.View())
	for _, key := range []textKey{txtFMThemeClassic, txtFMThemeSSHEasy, txtFMThemeMono} {
		if !strings.Contains(text, m.tr(key)) {
			t.Errorf("%q fehlt:\n%s", m.tr(key), text)
		}
	}
	assertScreenSize(t, m)
	m = pressRun(t, m, "down")
	m = pressRun(t, m, "esc")
	if m.fm.theme != 0 || m.fm.dialog != fmDialogNone {
		t.Errorf("Esc: Farbmodus %d, Dialog %v", m.fm.theme, m.fm.dialog)
	}
	m = menuChoose(t, m, 2, m.tr(txtFMMenuAppearance))
	m = pressRun(t, m, "down")
	m = pressRun(t, m, "down")
	m = pressRun(t, m, "enter")
	if m.fm.theme != 2 || m.fm.dialog != fmDialogNone {
		t.Fatalf("Farbmodus %d, Dialog %v", m.fm.theme, m.fm.dialog)
	}
	cfg, err := LoadConfig(m.configPath)
	if err != nil || cfg.FileManagerTheme != "mono" {
		t.Errorf("gespeichert: %q / %v", cfg.FileManagerTheme, err)
	}
	// Neuer Programmstart liest den Farbmodus
	again := NewAppModel(m.configPath, "72", NewSSHManager(NewLogger()))
	if again.fm.theme != 2 {
		t.Errorf("nach Neustart Farbmodus %d, erwartet 2", again.fm.theme)
	}
	// Zurück zur Liste und wieder öffnen: bleibt
	m = pressRun(t, m, "f10")
	m = openPick(t, m)
	if m.fm.theme != 2 {
		t.Errorf("nach erneutem Öffnen Farbmodus %d", m.fm.theme)
	}
}

// TestParseOpenExtensions - Eingabe der Endungen.
func TestParseOpenExtensions(t *testing.T) {
	got, bad := parseOpenExtensions(" PDF, .png;jpg  .tar.gz .png")
	if bad != "" || strings.Join(got, " ") != ".gz .jpg .pdf .png" && strings.Join(got, " ") != ".jpg .pdf .png .tar.gz" {
		t.Errorf("Ergebnis %v / %q", got, bad)
	}
	for _, in := range []string{"pd/f", "..x", "a*", "x\\y", ".", "a" + strings.Repeat("b", 30), "ex e\x1b"} {
		if _, bad := parseOpenExtensions(in); bad == "" {
			t.Errorf("%q angenommen", in)
		}
	}
	if got, bad := parseOpenExtensions("   "); got != nil || bad != "" {
		t.Errorf("leer: %v / %q", got, bad)
	}
}

// TestRiskyOpenExtensions - riskant ist alles außerhalb der sicheren
// Standardliste.
func TestRiskyOpenExtensions(t *testing.T) {
	got := riskyOpenExtensions([]string{".pdf", ".exe", ".png", ".docx"})
	if strings.Join(got, " ") != ".docx .exe" {
		t.Errorf("riskant = %v", got)
	}
}

// openExtDialog öffnet Optionen -> Endungen für F9.
func openExtDialog(t *testing.T) AppModel {
	t.Helper()
	m, _, _ := openMenuManager(t)
	m = menuChoose(t, m, 2, m.tr(txtFMMenuOpenExt))
	if m.fm.dialog != fmDialogExtensions {
		t.Fatalf("Dialog %v", m.fm.dialog)
	}
	return m
}

// typeExt tippt Zeichen einzeln in das Eingabefeld (ohne Blink-Kommandos).
func typeExt(t *testing.T, m AppModel, text string) AppModel {
	t.Helper()
	for _, r := range text {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(AppModel)
	}
	return m
}

// extCursorTo setzt den Cursor der Endungen-Liste per Pfeiltasten auf ext.
func extCursorTo(t *testing.T, m AppModel, ext string) AppModel {
	t.Helper()
	m = pressRun(t, m, "home")
	for i := 0; i < len(m.fm.extList); i++ {
		if m.fm.extList[m.fm.extCursor] == ext {
			return m
		}
		m = pressRun(t, m, "down")
	}
	t.Fatalf("%s nicht in der Liste %v", ext, m.fm.extList)
	return m
}

// screenText liefert den Bildschirm ohne Farben, Leerraum zusammengefasst.
func screenText(m AppModel) string {
	return strings.Join(strings.Fields(stripANSI(m.View())), " ")
}

// TestExtListSortedWithCursor - Nutzerwunsch 2026-10-23: statt eines langen,
// nicht bearbeitbaren Eingabefelds eine senkrechte Liste, alphanumerisch
// sortiert, mit sichtbarem Cursor.
func TestExtListSortedWithCursor(t *testing.T) {
	m := openExtDialog(t)
	if len(m.fm.extList) != len(allowedOpenExtensions) {
		t.Fatalf("Liste hat %d Einträge, erwartet die %d der Standardliste", len(m.fm.extList), len(allowedOpenExtensions))
	}
	if !sort.StringsAreSorted(m.fm.extList) {
		t.Errorf("Liste nicht sortiert: %v", m.fm.extList)
	}
	first := m.fm.extList[0]
	if text := screenText(m); !strings.Contains(text, "▸ "+first) {
		t.Errorf("Cursor auf %s nicht sichtbar:\n%s", first, text)
	}
	m = pressRun(t, m, "down")
	if text := screenText(m); m.fm.extCursor != 1 || !strings.Contains(text, "▸ "+m.fm.extList[1]) {
		t.Errorf("nach ↓: Cursor %d, Bildschirm:\n%s", m.fm.extCursor, text)
	}
	assertScreenSize(t, m)
}

// TestExtListScrolls - die Liste ist länger als der Kasten: sie scrollt mit
// dem Cursor, Pfeile ▲/▼ zeigen weitere Einträge an.
func TestExtListScrolls(t *testing.T) {
	m := openExtDialog(t)
	first, last := m.fm.extList[0], m.fm.extList[len(m.fm.extList)-1]
	text := screenText(m)
	if !strings.Contains(text, "▼") || strings.Contains(text, "▲") {
		t.Errorf("oben: ▼ erwartet, kein ▲:\n%s", text)
	}
	m = pressRun(t, m, "end")
	text = screenText(m)
	if !strings.Contains(text, "▸ "+last) || strings.Contains(text, " "+first+" ") {
		t.Errorf("nach Ende: %s sichtbar mit Cursor, %s weggescrollt erwartet:\n%s", last, first, text)
	}
	if !strings.Contains(text, "▲") {
		t.Errorf("unten: ▲ erwartet:\n%s", text)
	}
	m = pressRun(t, m, "pgup")
	m = pressRun(t, m, "home")
	if m.fm.extCursor != 0 || m.fm.extOffset != 0 {
		t.Errorf("Pos1: Cursor %d, Versatz %d", m.fm.extCursor, m.fm.extOffset)
	}
	for _, size := range [][2]int{{100, 30}, {80, 24}, {40, 12}, {20, 8}} {
		m.termWidth, m.termHeight = size[0], size[1]
		m = pressRun(t, m, "end")
		assertScreenSize(t, m)
		if !strings.Contains(screenText(m), last) {
			t.Errorf("%dx%d: Cursor-Eintrag %s nicht sichtbar", size[0], size[1], last)
		}
	}
}

// TestExtListDeleteAsks - Entf fragt "wirklich entfernen?"; Enter bestätigt
// nicht, n lässt den Eintrag stehen, j entfernt und speichert sofort.
func TestExtListDeleteAsks(t *testing.T) {
	m := openExtDialog(t)
	m = extCursorTo(t, m, ".pdf")
	pos := m.fm.extCursor
	m = pressRun(t, m, "delete")
	if m.fm.dialog != fmDialogExtDelete {
		t.Fatalf("Dialog %v, erwartet Rückfrage", m.fm.dialog)
	}
	if text := screenText(m); !strings.Contains(text, fmt.Sprintf(m.tr(txtFMExtDeleteText), ".pdf")) {
		t.Errorf("Rückfrage fehlt:\n%s", text)
	}
	assertScreenSize(t, m)
	m = pressRun(t, m, "enter")
	if m.fm.dialog != fmDialogExtDelete {
		t.Fatal("Enter hat das Löschen bestätigt")
	}
	m = pressRun(t, m, "n")
	if m.fm.dialog != fmDialogExtensions || !m.fmOpenAllowed()[".pdf"] {
		t.Fatalf("n: Dialog %v, .pdf erlaubt %v", m.fm.dialog, m.fmOpenAllowed()[".pdf"])
	}
	m = pressRun(t, m, "delete")
	m = pressRun(t, m, "j")
	if m.fm.dialog != fmDialogExtensions || m.fmOpenAllowed()[".pdf"] {
		t.Fatalf("j: Dialog %v, .pdf noch erlaubt", m.fm.dialog)
	}
	if m.fm.extCursor != pos || m.fm.extList[pos] == ".pdf" {
		t.Errorf("Cursor %d (erwartet %d, nächster Eintrag)", m.fm.extCursor, pos)
	}
	cfg, _ := LoadConfig(m.configPath)
	if len(cfg.FileManagerOpenExtensions) != len(allowedOpenExtensions)-1 {
		t.Errorf("gespeichert %d Endungen, erwartet %d", len(cfg.FileManagerOpenExtensions), len(allowedOpenExtensions)-1)
	}
}

// TestExtListAddWithTabAndInsert - Tab oder Einfg (auch Windows-Einfg als
// NUL-Rune) wechseln ins Hinzufügen-Feld. Erlaubt sind nur a-z und 0-9
// (Großbuchstaben werden klein, alles andere wird verworfen). Eine sichere
// Endung kommt ohne Rückfrage einsortiert in die Liste.
func TestExtListAddWithTabAndInsert(t *testing.T) {
	stubShift(t, false)
	for _, key := range []string{"tab", "insert", "nul"} {
		m := openExtDialog(t)
		m = extCursorTo(t, m, ".pdf")
		m = pressRun(t, m, "delete")
		m = pressRun(t, m, "j")
		m = pressRun(t, m, key)
		if !m.fm.extAdding {
			t.Fatalf("%s: nicht im Hinzufügen-Feld", key)
		}
		m = typeExt(t, m, "P.d/Fü ")
		if got := m.fm.extInput.Value(); got != "pdf" {
			t.Errorf("%s: Eingabe %q, erwartet pdf (nur a-z, 0-9)", key, got)
		}
		m = pressRun(t, m, "enter")
		if m.fm.dialog != fmDialogExtensions || !m.fmOpenAllowed()[".pdf"] {
			t.Fatalf("%s: Dialog %v, .pdf nicht erlaubt", key, m.fm.dialog)
		}
		if !sort.StringsAreSorted(m.fm.extList) || m.fm.extList[m.fm.extCursor] != ".pdf" {
			t.Errorf("%s: nicht einsortiert oder Cursor nicht darauf", key)
		}
		if m.fm.extInput.Value() != "" {
			t.Errorf("%s: Eingabe nach dem Hinzufügen nicht leer", key)
		}
		assertScreenSize(t, m)
	}
}

// TestExtListAddDigitsAndLimits - Ziffern sind erlaubt; doppelte Einträge
// werden gemeldet statt verdoppelt; leeres Enter tut nichts.
func TestExtListAddDigitsAndLimits(t *testing.T) {
	m := openExtDialog(t)
	n := len(m.fm.extList)
	m = pressRun(t, m, "tab")
	m = pressRun(t, m, "enter")
	if len(m.fm.extList) != n || m.fm.dialog != fmDialogExtensions {
		t.Error("leeres Enter hat etwas verändert")
	}
	m = typeExt(t, m, "PNG")
	m = pressRun(t, m, "enter")
	if len(m.fm.extList) != n || m.fm.message != fmt.Sprintf(m.tr(txtFMExtExists), ".png") {
		t.Errorf("doppelt: %d Einträge, Meldung %q", len(m.fm.extList), m.fm.message)
	}
	m = typeExt(t, m, strings.Repeat("a", 40))
	if got := len(m.fm.extInput.Value()); got > fmExtMaxLength {
		t.Errorf("Eingabe %d Zeichen lang, Grenze %d", got, fmExtMaxLength)
	}
}

// TestExtListAddRiskyWarns - eine Endung außerhalb der sicheren Liste: der
// kräftige Risikohinweis erscheint; Enter bestätigt nicht, n verwirft, j
// nimmt sie auf (Nutzerentscheidung: Warnung statt Sperre).
func TestExtListAddRiskyWarns(t *testing.T) {
	m := openExtDialog(t)
	m = pressRun(t, m, "tab")
	m = typeExt(t, m, "exe")
	m = pressRun(t, m, "enter")
	if m.fm.dialog != fmDialogExtRisk {
		t.Fatalf("Dialog %v, erwartet Risikohinweis", m.fm.dialog)
	}
	if text := screenText(m); !strings.Contains(text, m.tr(txtFMRiskTitle)) || !strings.Contains(text, ".exe") {
		t.Errorf("Risikohinweis unvollständig:\n%s", text)
	}
	assertScreenSize(t, m)
	m = pressRun(t, m, "enter")
	if m.fm.dialog != fmDialogExtRisk {
		t.Fatal("Enter hat den Risikohinweis bestätigt")
	}
	m = pressRun(t, m, "n")
	if m.fm.dialog != fmDialogExtensions || m.fmOpenAllowed()[".exe"] {
		t.Fatalf("n: Dialog %v, .exe erlaubt %v", m.fm.dialog, m.fmOpenAllowed()[".exe"])
	}
	m = pressRun(t, m, "enter")
	m = pressRun(t, m, "j")
	if !m.fmOpenAllowed()[".exe"] || !m.fmOpenAllowed()[".pdf"] {
		t.Errorf("j: erlaubt %v", m.fmOpenAllowed())
	}
	again := NewAppModel(m.configPath, "79", NewSSHManager(NewLogger()))
	if !again.fmOpenAllowed()[".exe"] {
		t.Error(".exe nach Neustart nicht mehr erlaubt")
	}
}

// TestExtListEmptyMeansNone - wer alle Einträge löscht, will, dass F9 nichts
// öffnet: die leere Liste wird so gespeichert und nach einem Neustart nicht
// stillschweigend durch die Standardliste ersetzt.
func TestExtListEmptyMeansNone(t *testing.T) {
	m := openExtDialog(t)
	for len(m.fm.extList) > 0 {
		m = pressRun(t, m, "delete")
		m = pressRun(t, m, "j")
	}
	if text := screenText(m); !strings.Contains(text, m.tr(txtFMExtEmpty)) {
		t.Errorf("Hinweis für leere Liste fehlt:\n%s", text)
	}
	m = pressRun(t, m, "delete") // leere Liste: nichts zu löschen
	if m.fm.dialog != fmDialogExtensions {
		t.Errorf("Entf auf leerer Liste: Dialog %v", m.fm.dialog)
	}
	assertScreenSize(t, m)
	if len(m.fmOpenAllowed()) != 0 {
		t.Errorf("erlaubt %d, erwartet keine", len(m.fmOpenAllowed()))
	}
	again := NewAppModel(m.configPath, "79", NewSSHManager(NewLogger()))
	if got := len(again.fmOpenAllowed()); got != 0 {
		t.Errorf("nach Neustart %d erlaubt, erwartet 0 (nicht die Standardliste)", got)
	}
}

// TestExtListAddLineNotCut - die Zeile des Hinzufügen-Felds passt in den
// Kasten (vorher war das Feld eine Zelle zu breit und wurde mit "~"
// abgeschnitten), in jeder Größe und mit Eingabe.
func TestExtListAddLineNotCut(t *testing.T) {
	m := openExtDialog(t)
	m = pressRun(t, m, "tab")
	m = typeExt(t, m, "abc")
	for _, size := range [][2]int{{100, 30}, {80, 24}, {60, 20}} {
		m.termWidth, m.termHeight = size[0], size[1]
		for _, line := range strings.Split(stripANSI(m.View()), "\n") {
			if strings.Contains(line, m.tr(txtFMExtAddLabel)) && strings.Contains(line, "~") {
				t.Errorf("%dx%d: Feld abgeschnitten: %q", size[0], size[1], line)
			}
		}
	}
}

// TestExtListEscBehaviour - Esc im Hinzufügen-Feld führt zurück zur Liste
// (Eingabe verworfen), Esc in der Liste schließt; q tippt im Feld ein q.
func TestExtListEscBehaviour(t *testing.T) {
	m := openExtDialog(t)
	m = pressRun(t, m, "tab")
	m = typeExt(t, m, "q")
	if m.fm.extInput.Value() != "q" || m.fm.dialog != fmDialogExtensions {
		t.Fatalf("q im Feld: Eingabe %q, Dialog %v", m.fm.extInput.Value(), m.fm.dialog)
	}
	m = pressRun(t, m, "esc")
	if m.fm.extAdding || m.fm.extInput.Value() != "" || m.fm.dialog != fmDialogExtensions {
		t.Errorf("Esc im Feld: Hinzufügen %v, Eingabe %q, Dialog %v", m.fm.extAdding, m.fm.extInput.Value(), m.fm.dialog)
	}
	m = pressRun(t, m, "esc")
	if m.fm.dialog != fmDialogNone {
		t.Errorf("Esc in der Liste: Dialog %v", m.fm.dialog)
	}
}

// TestViewerF9RiskyAsksFirst - eine erlaubte, aber riskante Endung fragt
// vor dem Öffnen nach; Enter öffnet nicht, n bricht ab, j öffnet.
func TestViewerF9RiskyAsksFirst(t *testing.T) {
	opened := stubOpener(t, nil)
	m := openViewerOn(t, map[string]string{"tool.exe": "MZ"}, "tool.exe")
	m.fmOpenExtensions = []string{".exe", ".pdf"}
	m = pressRun(t, m, "f9")
	if !m.fm.view.confirmOpen || len(*opened) != 0 {
		t.Fatalf("keine Rückfrage (geöffnet %v, Meldung %q)", *opened, m.fm.view.message)
	}
	text := strings.Join(strings.Fields(stripANSI(m.View())), " ")
	if !strings.Contains(text, m.tr(txtFMRiskTitle)) || !strings.Contains(text, "tool.exe") {
		t.Errorf("Rückfrage unvollständig:\n%s", text)
	}
	assertScreenSize(t, m)
	m = pressRun(t, m, "enter")
	if len(*opened) != 0 || !m.fm.view.confirmOpen {
		t.Fatal("Enter hat geöffnet")
	}
	m = pressRun(t, m, "n")
	if m.fm.view.confirmOpen || len(*opened) != 0 || !m.fm.viewing {
		t.Fatal("n hat geöffnet oder den Betrachter geschlossen")
	}
	m = pressRun(t, m, "f9")
	m = pressRun(t, m, "j")
	if len(*opened) != 1 {
		t.Fatalf("j hat nicht geöffnet (Meldung %q)", m.fm.view.message)
	}
}

// TestViewerF9SafeOpensDirectly - sichere Endung aus der eigenen Liste
// öffnet ohne Rückfrage, eine nicht eingetragene wird abgelehnt.
func TestViewerF9SafeOpensDirectly(t *testing.T) {
	opened := stubOpener(t, nil)
	m := openViewerOn(t, map[string]string{"a.pdf": "%PDF", "b.png": "PNG"}, "a.pdf")
	m.fmOpenExtensions = []string{".pdf"}
	m = pressRun(t, m, "f9")
	if len(*opened) != 1 || m.fm.view.confirmOpen {
		t.Errorf("geöffnet %v, Rückfrage %v", *opened, m.fm.view.confirmOpen)
	}
	m = openViewerOn(t, map[string]string{"b.png": "PNG"}, "b.png")
	m.fmOpenExtensions = []string{".pdf"}
	m = pressRun(t, m, "f9")
	if len(*opened) != 1 || m.fm.view.message != fmt.Sprintf(m.tr(txtFMOpenBlocked), ".png") {
		t.Errorf("nicht eingetragene Endung: geöffnet %v, Meldung %q", *opened, m.fm.view.message)
	}
}

// TestFileManagerMenusAllThemesAndSizes - Menüs und neue Dialoge passen
// in jede Größe und jeden Farbmodus.
func TestFileManagerMenusAllThemesAndSizes(t *testing.T) {
	withColorProfile(t)
	m, _, _ := openMenuManager(t)
	check := func(label string) {
		t.Helper()
		for theme := range fmThemes {
			m.fm.theme = theme
			for _, size := range [][2]int{{100, 30}, {80, 24}, {40, 12}, {20, 8}, {200, 60}} {
				m.termWidth, m.termHeight = size[0], size[1]
				assertScreenSize(t, m)
			}
		}
		m.fm.theme = 0
		m.termWidth, m.termHeight = 100, 30
	}
	m = pressRun(t, m, "f9")
	for i := 0; i < 4; i++ {
		check(fmt.Sprintf("Menü %d", i))
		m = pressRun(t, m, "right")
	}
	m = pressRun(t, m, "esc")
	m = menuChoose(t, m, 2, m.tr(txtFMMenuAppearance))
	check("Aussehen")
	m = pressRun(t, m, "esc")
	m = menuChoose(t, m, 2, m.tr(txtFMMenuOpenExt))
	check("Endungen")
	m = pressRun(t, m, "tab")
	check("Endungen hinzufügen")
	m = typeExt(t, m, "exe")
	m = pressRun(t, m, "enter")
	check("Risiko")
	m = pressRun(t, m, "n")
	m = pressRun(t, m, "esc")
	m = pressRun(t, m, "delete")
	check("Endung löschen")
}
