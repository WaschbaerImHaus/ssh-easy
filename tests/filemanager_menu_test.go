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
	"strings"
	"testing"
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

// TestFileManagerExtDialogSafe - sichere Endungen werden ohne Rückfrage
// gespeichert und gelten sofort.
func TestFileManagerExtDialogSafe(t *testing.T) {
	m := openExtDialog(t)
	if v := m.fm.extInput.Value(); !strings.Contains(v, ".pdf") || !strings.Contains(v, ".png") {
		t.Errorf("Vorbelegung = %q", v)
	}
	assertScreenSize(t, m)
	m.fm.extInput.SetValue(".pdf png")
	m = pressRun(t, m, "enter")
	if m.fm.dialog != fmDialogNone {
		t.Fatalf("Dialog %v", m.fm.dialog)
	}
	if want := fmt.Sprintf(m.tr(txtFMExtSaved), 2); m.fm.message != want {
		t.Errorf("Meldung = %q, erwartet %q", m.fm.message, want)
	}
	allowed := m.fmOpenAllowed()
	if len(allowed) != 2 || !allowed[".pdf"] || !allowed[".png"] {
		t.Errorf("erlaubt = %v", allowed)
	}
	cfg, _ := LoadConfig(m.configPath)
	if strings.Join(cfg.FileManagerOpenExtensions, " ") != ".pdf .png" {
		t.Errorf("gespeichert = %v", cfg.FileManagerOpenExtensions)
	}
	again := NewAppModel(m.configPath, "72", NewSSHManager(NewLogger()))
	if got := again.fmOpenAllowed(); len(got) != 2 {
		t.Errorf("nach Neustart = %v", got)
	}
}

// TestFileManagerExtDialogRisky - riskante Endungen: kräftiger Hinweis,
// Enter übernimmt NICHT, n kehrt zur Eingabe zurück, j speichert.
func TestFileManagerExtDialogRisky(t *testing.T) {
	m := openExtDialog(t)
	m.fm.extInput.SetValue("exe .PDF")
	m = pressRun(t, m, "enter")
	if m.fm.dialog != fmDialogExtRisk {
		t.Fatalf("Dialog %v, erwartet Risikohinweis", m.fm.dialog)
	}
	text := strings.Join(strings.Fields(stripANSI(m.View())), " ")
	if !strings.Contains(text, m.tr(txtFMRiskTitle)) || !strings.Contains(text, ".exe") {
		t.Errorf("Risikohinweis unvollständig:\n%s", text)
	}
	assertScreenSize(t, m)
	m = pressRun(t, m, "enter")
	if m.fm.dialog != fmDialogExtRisk {
		t.Fatal("Enter hat den Risikohinweis bestätigt")
	}
	m = pressRun(t, m, "n")
	if m.fm.dialog != fmDialogExtensions || m.fm.extInput.Value() != "exe .PDF" {
		t.Fatalf("n: Dialog %v, Eingabe %q", m.fm.dialog, m.fm.extInput.Value())
	}
	if m.fmOpenAllowed()[".exe"] {
		t.Error(".exe ohne Bestätigung erlaubt")
	}
	m = pressRun(t, m, "enter")
	m = pressRun(t, m, "j")
	if m.fm.dialog != fmDialogNone || !m.fmOpenAllowed()[".exe"] || !m.fmOpenAllowed()[".pdf"] {
		t.Errorf("j: Dialog %v, erlaubt %v", m.fm.dialog, m.fmOpenAllowed())
	}
}

// TestFileManagerExtDialogInvalidAndReset - ungültige Endung: Meldung,
// Dialog bleibt; leere Eingabe stellt den Standard wieder her.
func TestFileManagerExtDialogInvalidAndReset(t *testing.T) {
	m := openExtDialog(t)
	m.fm.extInput.SetValue(".pdf pd/f")
	m = pressRun(t, m, "enter")
	if m.fm.dialog != fmDialogExtensions || m.fm.message != fmt.Sprintf(m.tr(txtFMExtBad), "pd/f") {
		t.Errorf("Dialog %v, Meldung %q", m.fm.dialog, m.fm.message)
	}
	m.fm.extInput.SetValue(".pdf")
	m = pressRun(t, m, "enter")
	m = menuChoose(t, m, 2, m.tr(txtFMMenuOpenExt))
	m.fm.extInput.SetValue("  ")
	m = pressRun(t, m, "enter")
	if m.fm.message != m.tr(txtFMExtReset) {
		t.Errorf("Meldung = %q", m.fm.message)
	}
	if len(m.fmOpenAllowed()) != len(allowedOpenExtensions) {
		t.Errorf("Standard nicht wiederhergestellt: %d", len(m.fmOpenAllowed()))
	}
	cfg, _ := LoadConfig(m.configPath)
	if cfg.FileManagerOpenExtensions != nil {
		t.Errorf("gespeichert = %v, erwartet leer (Standard)", cfg.FileManagerOpenExtensions)
	}
	// Esc verwirft
	m = menuChoose(t, m, 2, m.tr(txtFMMenuOpenExt))
	m.fm.extInput.SetValue(".exe")
	m = pressRun(t, m, "esc")
	if m.fm.dialog != fmDialogNone || m.fmOpenAllowed()[".exe"] {
		t.Error("Esc hat übernommen")
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
	m.fm.extInput.SetValue(".exe .bat .ps1")
	m = pressRun(t, m, "enter")
	check("Risiko")
}
