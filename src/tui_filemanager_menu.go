// Paket main - Datei-Manager (MC-Modus): Menüs und Optionen
//
// Die Menüleiste funktioniert wie im Midnight Commander: F9 öffnet das
// Menü des aktiven Fensters, ←/→ wechseln das Menü, ↑/↓ den Eintrag,
// Enter führt aus, Esc/F9/F10/q schließen. Es gibt nur Einträge, die
// ssh-easy auch anbietet:
//   - Links / Rechts: Neu einlesen (auch Strg+R)
//   - Datei: Ansicht, Bearbeiten, Kopieren, Verschieben, Verzeichnis
//     anlegen, Löschen, Auswahl umkehren, Beenden (wirken wie ihre Tasten)
//   - Optionen: Aussehen (Farbmodus) und Endungen für F9
//
// Das mc-Menü "Befehl" entfällt, weil ssh-easy davon nichts anbietet.
//
// Einstellungen werden in der Konfiguration gespeichert. Endungen für F9:
// Alles außerhalb der sicheren Standardliste gilt als riskant; es wird
// nicht gesperrt (Nutzerentscheidung), aber nur nach einem kräftigen
// Risikohinweis übernommen, der mit j/y bestätigt werden muss (Enter
// genügt nicht). Beim Öffnen einer solchen Datei fragt F9 noch einmal.
//
// @author Kurt Ingwer
// @date   2026-10-22 07:50
package main

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// fmMenuAction ist die Funktion eines Menüeintrags.
type fmMenuAction int

const (
	fmActNone fmMenuAction = iota
	fmActRescan
	fmActView
	fmActEdit
	fmActCopy
	fmActMove
	fmActMkdir
	fmActDelete
	fmActInvert
	fmActExit
	fmActAppearance
	fmActOpenExt
)

// fmMenuItem ist ein Menüeintrag; label "" = Trennlinie.
type fmMenuItem struct {
	label  textKey
	key    string
	action fmMenuAction
}

// fmMenu ist ein Menü der Leiste.
type fmMenu struct {
	title string
	items []fmMenuItem
}

// fmExtMaxLength - Höchstlänge einer Endung (ohne Punkt)
const fmExtMaxLength = 16

// fmMenus liefert die Menüs in der Reihenfolge der Leiste. Mit nur einer
// Verbindung gibt es ein Fenster-Menü statt Links/Rechts und im
// Datei-Menü weder Kopieren noch Verschieben.
//
// @return []fmMenu - Links, Datei, Optionen, Rechts (bzw. Fenster, Datei, Optionen)
// @date   2026-10-26 21:04
func (m AppModel) fmMenus() []fmMenu {
	titles := strings.Split(m.tr(txtFMMenuBar), "|")
	for len(titles) < 4 {
		titles = append(titles, "")
	}
	panel := []fmMenuItem{{label: txtFMMenuRescan, key: m.tr(txtFMKeyCtrlR), action: fmActRescan}}
	file := []fmMenuItem{
		{label: txtFMMenuView, key: "F3", action: fmActView},
		{label: txtFMMenuEdit, key: "F4", action: fmActEdit},
	}
	if !m.fm.single {
		file = append(file,
			fmMenuItem{label: txtFMMenuCopy, key: "F5", action: fmActCopy},
			fmMenuItem{label: txtFMMenuMove, key: "F6", action: fmActMove})
	}
	file = append(file,
		fmMenuItem{label: txtFMMenuMkdir, key: "F7", action: fmActMkdir},
		fmMenuItem{label: txtFMMenuDelete, key: "F8", action: fmActDelete},
		fmMenuItem{},
		fmMenuItem{label: txtFMMenuInvert, key: "*", action: fmActInvert},
		fmMenuItem{},
		fmMenuItem{label: txtFMMenuExit, key: "F10", action: fmActExit})
	options := []fmMenuItem{
		{label: txtFMMenuAppearance, action: fmActAppearance},
		{label: txtFMMenuOpenExt, action: fmActOpenExt},
	}
	if m.fm.single {
		return []fmMenu{
			{title: m.tr(txtFMMenuPanel), items: panel},
			{title: titles[1], items: file},
			{title: titles[2], items: options},
		}
	}
	return []fmMenu{
		{title: titles[0], items: panel},
		{title: titles[1], items: file},
		{title: titles[2], items: options},
		{title: titles[3], items: panel},
	}
}

// fmOpenMenu öffnet mit F9 das Menü des aktiven Fensters.
//
// @return tea.Model - Modell mit offenem Menü
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-22 07:50
func (m AppModel) fmOpenMenu() (tea.Model, tea.Cmd) {
	m.fm.menuOpen = true
	m.fm.menuIndex = 0
	if m.fm.active == 1 {
		m.fm.menuIndex = 3
	}
	m.fm.menuItem = 0
	return m, nil
}

// handleFMMenuKeys verarbeitet Tasten bei offenem Menü.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando des ausgeführten Eintrags
// @date   2026-10-22 07:50
func (m AppModel) handleFMMenuKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	menus := m.fmMenus()
	items := menus[m.fm.menuIndex].items
	switch msg.String() {
	case "esc", "f9", "f10", "q":
		m.fm.menuOpen = false
	case "left":
		m.fm.menuIndex = (m.fm.menuIndex + len(menus) - 1) % len(menus)
		m.fm.menuItem = 0
	case "right":
		m.fm.menuIndex = (m.fm.menuIndex + 1) % len(menus)
		m.fm.menuItem = 0
	case "up":
		m.fm.menuItem = nextMenuItem(items, m.fm.menuItem, -1)
	case "down":
		m.fm.menuItem = nextMenuItem(items, m.fm.menuItem, 1)
	case "home":
		m.fm.menuItem = nextMenuItem(items, -1, 1)
	case "end":
		m.fm.menuItem = nextMenuItem(items, len(items), -1)
	case "enter":
		m.fm.menuOpen = false
		side := 0
		if m.fm.menuIndex == 3 {
			side = 1
		}
		return m.fmRunMenuAction(items[m.fm.menuItem].action, side)
	}
	return m, nil
}

// nextMenuItem liefert den nächsten Eintrag in Richtung dir, der keine
// Trennlinie ist (mit Umlauf).
//
// @param items - Einträge
// @param from - Ausgangsposition
// @param dir - +1 runter, -1 hoch
// @return int - neue Position
// @date   2026-10-22 07:50
func nextMenuItem(items []fmMenuItem, from, dir int) int {
	n := len(items)
	for step := 1; step <= n; step++ {
		i := ((from+dir*step)%n + n) % n
		if items[i].label != "" {
			return i
		}
	}
	return 0
}

// fmRunMenuAction führt einen Menüeintrag aus - wie die zugehörige Taste.
//
// @param action - Funktion
// @param side - Seite des Menüs Links/Rechts (für Neu einlesen)
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando
// @date   2026-10-22 07:50
func (m AppModel) fmRunMenuAction(action fmMenuAction, side int) (tea.Model, tea.Cmd) {
	switch action {
	case fmActRescan:
		return m, m.fmRescan(side)
	case fmActView:
		return m.fmOpenViewer()
	case fmActEdit:
		return m.fmOpenEditor()
	case fmActCopy:
		return m.fmOpenCopyConfirm(false)
	case fmActMove:
		return m.fmOpenCopyConfirm(true)
	case fmActMkdir:
		return m.fmOpenMkdir()
	case fmActDelete:
		return m.fmOpenDeleteConfirm()
	case fmActInvert:
		m.fm.panels[m.fm.active].invertMarks()
	case fmActExit:
		return m.closeFileManager(), nil
	case fmActAppearance:
		m.fm.themeCursor = m.fm.theme
		m.fm.dialog = fmDialogTheme
	case fmActOpenExt:
		return m.fmOpenExtDialog()
	}
	return m, nil
}

// fmRescan liest ein Fenster neu ein (Menü Neu einlesen, Strg+R); Cursor
// und Markierungen bleiben erhalten.
//
// @param side - Seite
// @return tea.Cmd - Hintergrundkommando (nil, wenn gerade geladen wird)
// @date   2026-10-22 07:50
func (m *AppModel) fmRescan(side int) tea.Cmd {
	panel := m.fm.panels[side]
	if panel.loading || panel.fs == nil {
		return nil
	}
	focus, cursor := m.fmPanelFocus(side)
	return m.fmReloadPanel(side, focus, cursor, m.fmRemainingMarks(side, nil))
}

// --- Einstellungen ---

// applyFMSettings übernimmt die Datei-Manager-Einstellungen aus der
// Konfiguration; ungültige Endungen führen zur Standardliste, eine leere
// Liste bleibt leer (der Nutzer hat alle Endungen gelöscht).
//
// @param cfg - Konfiguration
// @date   2026-10-23 20:04
func (m *AppModel) applyFMSettings(cfg *AppConfig) {
	m.fm.theme = fmThemeIndex(cfg.FileManagerTheme)
	m.fmOpenExtensions = nil
	if cfg.FileManagerOpenExtensions != nil {
		if list, bad := parseOpenExtensions(strings.Join(cfg.FileManagerOpenExtensions, " ")); bad == "" {
			if list == nil {
				// Bewusst leer: F9 öffnet nichts
				list = []string{}
			}
			m.fmOpenExtensions = list
		}
	}
}

// persistFMSettings speichert Farbmodus und Endungen in der Konfiguration.
//
// @return error - Fehler beim Laden/Speichern
// @date   2026-10-22 07:50
func (m *AppModel) persistFMSettings() error {
	cfg, err := LoadConfig(m.configPath)
	if err != nil {
		return err
	}
	cfg.FileManagerTheme = fmThemes[fmThemeIndexValid(m.fm.theme)].name
	cfg.FileManagerOpenExtensions = m.fmOpenExtensions
	if err := SaveConfig(m.configPath, cfg); err != nil {
		return err
	}
	m.configCache.Invalidate()
	return nil
}

// fmThemeIndex liefert den Index eines Farbmodus nach Namen (unbekannt = 0).
//
// @param name - Name ("classic", "ssh-easy", "mono")
// @return int - Index in fmThemes
// @date   2026-10-22 07:50
func fmThemeIndex(name string) int {
	for i, theme := range fmThemes {
		if theme.name == name {
			return i
		}
	}
	return 0
}

// fmThemeIndexValid begrenzt einen Index auf die vorhandenen Farbmodi.
//
// @param i - Index
// @return int - gültiger Index
// @date   2026-10-22 07:50
func fmThemeIndexValid(i int) int {
	if i < 0 || i >= len(fmThemes) {
		return 0
	}
	return i
}

// fmOpenAllowed liefert die erlaubten Endungen für F9.
//
// @return map[string]bool - Endungen mit Punkt, klein geschrieben
// @date   2026-10-22 07:50
func (m AppModel) fmOpenAllowed() map[string]bool {
	if m.fmOpenExtensions == nil {
		return allowedOpenExtensions
	}
	allowed := make(map[string]bool, len(m.fmOpenExtensions))
	for _, ext := range m.fmOpenExtensions {
		allowed[ext] = true
	}
	return allowed
}

// parseOpenExtensions liest die Eingabe der Endungen: getrennt durch
// Leerzeichen, Komma oder Semikolon; Punkt davor ist freiwillig; Groß-
// schreibung egal. Bei mehrteiligen Endungen (".tar.gz") zählt der letzte
// Teil, denn nur der entscheidet über das Programm.
//
// @param input - Eingabe
// @return []string - sortierte Endungen ohne Doppelte (nil = leer)
// @return string - erste ungültige Angabe ("" = alle gültig)
// @date   2026-10-22 07:50
func parseOpenExtensions(input string) ([]string, string) {
	fields := strings.FieldsFunc(input, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ';'
	})
	seen := map[string]bool{}
	var list []string
	for _, field := range fields {
		parts := strings.Split(strings.TrimPrefix(strings.ToLower(field), "."), ".")
		for _, part := range parts {
			if !validExtPart(part) {
				return nil, field
			}
		}
		ext := "." + parts[len(parts)-1]
		if !seen[ext] {
			seen[ext] = true
			list = append(list, ext)
		}
	}
	sort.Strings(list)
	return list, ""
}

// validExtPart prüft einen Teil einer Endung: 1-16 Zeichen aus a-z, 0-9,
// "_", "+", "-" (beginnend mit Buchstabe oder Ziffer).
//
// @param part - Teil ohne Punkt
// @return bool - true wenn gültig
// @date   2026-10-22 07:50
func validExtPart(part string) bool {
	if part == "" || len(part) > fmExtMaxLength {
		return false
	}
	for i, r := range part {
		alnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !alnum && (i == 0 || !strings.ContainsRune("_+-", r)) {
			return false
		}
	}
	return true
}

// riskyOpenExtensions liefert die Endungen, die nicht zur sicheren
// Standardliste gehören (sortiert).
//
// @param list - Endungen
// @return []string - riskante Endungen
// @date   2026-10-22 07:50
func riskyOpenExtensions(list []string) []string {
	var risky []string
	for _, ext := range list {
		if !allowedOpenExtensions[ext] {
			risky = append(risky, ext)
		}
	}
	sort.Strings(risky)
	return risky
}

// --- Dialoge der Optionen ---

// fmExtVisibleRows - so viele Endungen zeigt die Liste höchstens auf einmal
const fmExtVisibleRows = 12

// fmOpenExtDialog öffnet die Liste der erlaubten Endungen (Nutzerwunsch
// 2026-10-23: senkrechte, scrollbare Liste mit Cursor statt eines langen
// Eingabefelds). Entf löscht (mit Rückfrage), Tab/Einfg springen ins
// Hinzufügen-Feld.
//
// @return tea.Model - Modell mit Dialog
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-23 20:04
func (m AppModel) fmOpenExtDialog() (tea.Model, tea.Cmd) {
	list := make([]string, 0, len(m.fmOpenAllowed()))
	for ext := range m.fmOpenAllowed() {
		list = append(list, ext)
	}
	sort.Strings(list)
	m.fm.extList = list
	m.fm.extCursor, m.fm.extOffset = 0, 0
	m.fm.extAdding = false
	m.fm.extInput = newExtInput()
	m.fm.extPending = nil
	m.fm.dialog = fmDialogExtensions
	return m, nil
}

// newExtInput legt das Hinzufügen-Feld an (ohne Punkt, höchstens
// fmExtMaxLength Zeichen).
//
// @return textinput.Model - leeres Feld ohne Fokus
// @date   2026-10-23 20:04
func newExtInput() textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = fmExtMaxLength
	return input
}

// fmExtRows liefert die Zahl der Listenzeilen für die Bildschirmhöhe
// (Rest des Kastens: Rahmen, ▲/▼, Feld, Hinweis, Leerzeile, Hilfe).
//
// @return int - sichtbare Zeilen (mindestens 1)
// @date   2026-10-23 20:04
func (m AppModel) fmExtRows() int {
	_, height := m.fmScreenSize()
	return min(max(height-12, 1), fmExtVisibleRows)
}

// handleFMOptionKeys verarbeitet Tasten in den Dialogen der Optionen.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando
// @date   2026-10-23 20:04
func (m AppModel) handleFMOptionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch m.fm.dialog {
	case fmDialogTheme:
		switch key {
		case "up":
			m.fm.themeCursor = (m.fm.themeCursor + len(fmThemes) - 1) % len(fmThemes)
		case "down":
			m.fm.themeCursor = (m.fm.themeCursor + 1) % len(fmThemes)
		case "enter":
			m.fm.theme = m.fm.themeCursor
			m.fm.dialog = fmDialogNone
			m.fm.message = fmt.Sprintf(m.tr(txtFMThemeSaved), m.fmThemeLabel(m.fm.theme))
			if err := m.persistFMSettings(); err != nil {
				m.fm.message = fmt.Sprintf(m.tr(txtFMErrSaveSettings), sanitizeFileName(err.Error()))
			}
		case "esc", "q":
			m.fm.dialog = fmDialogNone
		}
		return m, nil
	case fmDialogExtensions:
		if m.fm.extAdding {
			return m.handleExtAddKeys(msg)
		}
		return m.handleExtListKeys(msg)
	case fmDialogExtRisk:
		switch key {
		case "j", "y":
			// Bewusst bestätigt (Nutzerentscheidung: Warnung statt Sperre)
			ext := m.fm.extPending[0]
			m.fm.extPending = nil
			m.fm.dialog = fmDialogExtensions
			return m.fmExtAdd(ext)
		case "n", "esc", "q":
			// Zurück ins Feld, nichts übernommen
			m.fm.extPending = nil
			m.fm.dialog = fmDialogExtensions
		}
	case fmDialogExtDelete:
		switch key {
		case "j", "y":
			m.fm.dialog = fmDialogExtensions
			return m.fmExtRemove()
		case "n", "esc", "q":
			m.fm.dialog = fmDialogExtensions
		}
	}
	return m, nil
}

// handleExtListKeys bedient die Endungen-Liste: Pfeile/Bild/Pos1/Ende
// bewegen den Cursor, Entf fragt nach dem Löschen, Tab/Einfg springen ins
// Hinzufügen-Feld, Esc/q schließt.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Cursor-Blinken beim Wechsel ins Feld
// @date   2026-10-23 20:04
func (m AppModel) handleExtListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.fm.message = ""
	if isInsertKey(msg) || msg.Type == tea.KeyTab {
		m.fm.extAdding = true
		m.fm.extInput.Focus()
		return m, textinput.Blink
	}
	rows := m.fmExtRows()
	last := len(m.fm.extList) - 1
	switch msg.String() {
	case "up":
		m.fm.extCursor--
	case "down":
		m.fm.extCursor++
	case "pgup":
		m.fm.extCursor -= rows
	case "pgdown":
		m.fm.extCursor += rows
	case "home":
		m.fm.extCursor = 0
	case "end":
		m.fm.extCursor = last
	case "delete":
		if len(m.fm.extList) > 0 {
			m.fm.dialog = fmDialogExtDelete
		}
	case "esc", "q", "f10":
		m.fm.dialog = fmDialogNone
		return m, nil
	}
	m.fm.extCursor = max(min(m.fm.extCursor, last), 0)
	m.fm.extOffset = scrollOffset(len(m.fm.extList), m.fm.extCursor, m.fm.extOffset, rows)
	return m, nil
}

// handleExtAddKeys bedient das Hinzufügen-Feld. Erlaubt sind nur a-z und
// 0-9: Großbuchstaben werden klein, alles andere (auch der Punkt) wird
// verworfen. Enter fügt hinzu, Tab springt zur Liste, Esc verwirft die
// Eingabe und springt zur Liste.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando des Felds
// @date   2026-10-23 20:04
func (m AppModel) handleExtAddKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.fm.message = ""
	switch msg.Type {
	case tea.KeyEsc:
		m.fm.extAdding = false
		m.fm.extInput.Reset()
		m.fm.extInput.Blur()
		return m, nil
	case tea.KeyTab:
		m.fm.extAdding = false
		m.fm.extInput.Blur()
		return m, nil
	case tea.KeyEnter:
		return m.fmExtSubmit()
	case tea.KeySpace:
		return m, nil
	case tea.KeyRunes:
		runes := filterExtRunes(msg.Runes)
		if len(runes) == 0 {
			return m, nil
		}
		msg.Runes = runes
	}
	var cmd tea.Cmd
	m.fm.extInput, cmd = m.fm.extInput.Update(msg)
	return m, cmd
}

// filterExtRunes lässt nur Zeichen durch, die in einer Endung erlaubt sind
// (a-z, 0-9); A-Z werden klein geschrieben.
//
// @param runes - getippte Zeichen
// @return []rune - erlaubte Zeichen
// @date   2026-10-23 20:04
func filterExtRunes(runes []rune) []rune {
	var out []rune
	for _, r := range runes {
		switch {
		case r >= 'A' && r <= 'Z':
			out = append(out, r-'A'+'a')
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			out = append(out, r)
		}
	}
	return out
}

// fmExtSubmit übernimmt die Eingabe des Hinzufügen-Felds: leer = nichts,
// schon vorhanden = Hinweis, außerhalb der sicheren Liste = kräftiger
// Risikohinweis (j/y), sonst sofort hinzufügen.
//
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-23 20:04
func (m AppModel) fmExtSubmit() (tea.Model, tea.Cmd) {
	value := m.fm.extInput.Value()
	if value == "" {
		return m, nil
	}
	ext := "." + value
	if i := sort.SearchStrings(m.fm.extList, ext); i < len(m.fm.extList) && m.fm.extList[i] == ext {
		m.fm.message = fmt.Sprintf(m.tr(txtFMExtExists), ext)
		m.fm.extCursor = i
		m.fm.extOffset = scrollOffset(len(m.fm.extList), i, m.fm.extOffset, m.fmExtRows())
		return m, nil
	}
	if !allowedOpenExtensions[ext] {
		m.fm.extPending = []string{ext}
		m.fm.dialog = fmDialogExtRisk
		return m, nil
	}
	return m.fmExtAdd(ext)
}

// fmExtAdd nimmt eine Endung sortiert in die Liste auf, setzt den Cursor
// darauf, leert das Feld und speichert sofort.
//
// @param ext - Endung mit Punkt
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-23 20:04
func (m AppModel) fmExtAdd(ext string) (tea.Model, tea.Cmd) {
	list := append(append([]string{}, m.fm.extList...), ext)
	sort.Strings(list)
	m.fm.extList = list
	m.fm.extCursor = sort.SearchStrings(list, ext)
	m.fm.extOffset = scrollOffset(len(list), m.fm.extCursor, m.fm.extOffset, m.fmExtRows())
	m.fm.extInput.Reset()
	return m.fmExtSave(fmt.Sprintf(m.tr(txtFMExtAdded), ext))
}

// fmExtRemove entfernt die Endung unter dem Cursor und speichert sofort;
// der Cursor bleibt an derselben Stelle (nächster Eintrag).
//
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-23 20:04
func (m AppModel) fmExtRemove() (tea.Model, tea.Cmd) {
	if m.fm.extCursor < 0 || m.fm.extCursor >= len(m.fm.extList) {
		return m, nil
	}
	ext := m.fm.extList[m.fm.extCursor]
	list := append(append([]string{}, m.fm.extList[:m.fm.extCursor]...), m.fm.extList[m.fm.extCursor+1:]...)
	m.fm.extList = list
	m.fm.extCursor = max(min(m.fm.extCursor, len(list)-1), 0)
	m.fm.extOffset = scrollOffset(len(list), m.fm.extCursor, m.fm.extOffset, m.fmExtRows())
	return m.fmExtSave(fmt.Sprintf(m.tr(txtFMExtRemoved), ext))
}

// fmExtSave übernimmt die Liste als erlaubte Endungen (auch leer = F9
// öffnet nichts) und speichert sie in der Konfiguration.
//
// @param done - Meldung bei Erfolg
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-23 20:04
func (m AppModel) fmExtSave(done string) (tea.Model, tea.Cmd) {
	m.fmOpenExtensions = append([]string{}, m.fm.extList...)
	m.fm.message = done
	if err := m.persistFMSettings(); err != nil {
		m.fm.message = fmt.Sprintf(m.tr(txtFMErrSaveSettings), sanitizeFileName(err.Error()))
	}
	return m, nil
}

// fmThemeLabel liefert den übersetzten Namen eines Farbmodus.
//
// @param i - Index
// @return string - Name
// @date   2026-10-22 07:50
func (m AppModel) fmThemeLabel(i int) string {
	switch fmThemes[fmThemeIndexValid(i)].name {
	case "ssh-easy":
		return m.tr(txtFMThemeSSHEasy)
	case "mono":
		return m.tr(txtFMThemeMono)
	}
	return m.tr(txtFMThemeClassic)
}

// fmOptionDialogLines liefert Titel, Inhalt und Hilfe der Options-Dialoge.
//
// @param inner - Innenbreite
// @return string - Titel
// @return []string - Inhalt
// @return string - Tastenhilfe
// @date   2026-10-22 07:50
func (m AppModel) fmOptionDialogLines(inner int) (string, []string, string) {
	switch m.fm.dialog {
	case fmDialogTheme:
		var lines []string
		for i := range fmThemes {
			prefix, radio := "  ", "( )"
			if i == m.fm.themeCursor {
				prefix = "▸ "
			}
			if i == m.fm.theme {
				radio = "(*)"
			}
			lines = append(lines, prefix+radio+" "+m.fmThemeLabel(i))
		}
		return m.tr(txtFMThemeTitle), lines, m.tr(txtFMThemeHelp)
	case fmDialogExtensions:
		return m.tr(txtFMExtTitle), m.fmExtListLines(inner), m.fmExtHelp()
	case fmDialogExtDelete:
		ext := ""
		if m.fm.extCursor < len(m.fm.extList) {
			ext = m.fm.extList[m.fm.extCursor]
		}
		return m.tr(txtFMExtDeleteTitle), wrapText(fmt.Sprintf(m.tr(txtFMExtDeleteText), ext), inner), m.tr(txtFMExtDeleteHelp)
	case fmDialogExtRisk:
		text := fmt.Sprintf(m.tr(txtFMExtRiskText), strings.Join(riskyOpenExtensions(m.fm.extPending), " "))
		return m.tr(txtFMRiskTitle), wrapText(text, inner), m.tr(txtFMExtRiskHelp)
	}
	return "", nil, ""
}

// fmExtListLines liefert den Inhalt der Endungen-Liste: ▲ (weitere oben),
// die sichtbaren Einträge mit Cursor ▸, ▼ (weitere unten), Leerzeile,
// Hinzufügen-Feld und Hinweis auf die erlaubten Zeichen. Die Zeilenzahl ist
// fest, damit der Kasten beim Scrollen nicht springt.
//
// @param inner - Innenbreite
// @return []string - Zeilen
// @date   2026-10-23 20:04
func (m AppModel) fmExtListLines(inner int) []string {
	rows := m.fmExtRows()
	list := m.fm.extList
	offset := scrollOffset(len(list), m.fm.extCursor, m.fm.extOffset, rows)
	up, down := "", ""
	if offset > 0 {
		up = "  ▲"
	}
	if offset+rows < len(list) {
		down = "  ▼"
	}
	lines := []string{up}
	for i := 0; i < rows; i++ {
		idx := offset + i
		switch {
		case len(list) == 0 && i == 0:
			lines = append(lines, "  "+m.tr(txtFMExtEmpty))
		case idx < len(list):
			mark := "  "
			if idx == m.fm.extCursor && !m.fm.extAdding {
				mark = "▸ "
			}
			lines = append(lines, mark+list[idx])
		default:
			lines = append(lines, "")
		}
	}
	label := m.tr(txtFMExtAddLabel) + " "
	input := m.fm.extInput
	// Platz: Markierung (2), Beschriftung, Punkt (1), Cursorzelle (1)
	input.Width = max(inner-ansi.StringWidth(label)-4, 1)
	mark := "  "
	if m.fm.extAdding {
		mark = "▸ "
	}
	return append(lines, down, "", mark+label+"."+input.View(), "  "+m.tr(txtFMExtChars))
}

// fmExtHelp liefert die Tastenhilfe passend zum Fokus (Liste oder Feld).
//
// @return string - Tastenhilfe
// @date   2026-10-23 20:04
func (m AppModel) fmExtHelp() string {
	if m.fm.extAdding {
		return m.tr(txtFMExtAddHelp)
	}
	return m.tr(txtFMExtListHelp)
}

// --- Darstellung der Menüs ---

// fmMenuTitleOffsets liefert die Spalte jedes Menütitels in der Leiste.
//
// @param menus - Menüs
// @return []int - Startspalten
// @date   2026-10-22 07:50
func fmMenuTitleOffsets(menus []fmMenu) []int {
	offsets := make([]int, len(menus))
	x := 2
	for i, menu := range menus {
		offsets[i] = x
		x += ansi.StringWidth(menu.title) + 4
	}
	return offsets
}

// renderFMMenuBarLine rendert die Menüleiste; bei offenem Menü ist dessen
// Titel hervorgehoben.
//
// @param st - Stile
// @param width - Bildschirmbreite
// @return string - Zeile
// @date   2026-10-22 07:50
func (m AppModel) renderFMMenuBarLine(st fmStyles, width int) string {
	menus := m.fmMenus()
	var bar strings.Builder
	bar.WriteString(st.menu.Render("  "))
	used := 2
	for i, menu := range menus {
		if used >= width {
			break
		}
		title := fitWidth(menu.title, min(ansi.StringWidth(menu.title), width-used))
		style := st.menu
		if m.fm.menuOpen && i == m.fm.menuIndex {
			style = st.menuSel
		}
		bar.WriteString(style.Render(title))
		used += ansi.StringWidth(title)
		gap := min(4, max(width-used, 0))
		bar.WriteString(st.menu.Render(strings.Repeat(" ", gap)))
		used += gap
	}
	if used < width {
		bar.WriteString(st.menu.Render(strings.Repeat(" ", width-used)))
	}
	return bar.String()
}

// renderFMMenuBox rendert das aufgeklappte Menü.
//
// @param st - Stile
// @param width - Bildschirmbreite
// @param height - Bildschirmhöhe
// @return []string - Zeilen des Kastens
// @return int - Breite des Kastens
// @return int - linke Spalte
// @date   2026-10-22 07:50
func (m AppModel) renderFMMenuBox(st fmStyles, width, height int) ([]string, int, int) {
	menus := m.fmMenus()
	menu := menus[m.fm.menuIndex]
	labelWidth, keyWidth := 0, 0
	for _, item := range menu.items {
		if item.label != "" {
			labelWidth = max(labelWidth, ansi.StringWidth(m.tr(item.label)))
			keyWidth = max(keyWidth, ansi.StringWidth(item.key))
		}
	}
	inner := labelWidth + 2 + keyWidth
	if keyWidth == 0 {
		inner = labelWidth
	}
	boxWidth := min(inner+4, width)
	inner = max(boxWidth-4, 0)
	lines := []string{st.menu.Render("┌" + strings.Repeat("─", max(boxWidth-2, 0)) + "┐")}
	for i, item := range menu.items {
		if item.label == "" {
			lines = append(lines, st.menu.Render("├"+strings.Repeat("─", max(boxWidth-2, 0))+"┤"))
			continue
		}
		key := fitRight(item.key, min(keyWidth, inner))
		label := fitWidth(m.tr(item.label), max(inner-ansi.StringWidth(key), 0))
		text := label + key
		style := st.menu
		if i == m.fm.menuItem {
			style = st.menuSel
		}
		lines = append(lines, st.menu.Render("│ ")+style.Render(fitWidth(text, inner))+st.menu.Render(" │"))
	}
	lines = append(lines, st.menu.Render("└"+strings.Repeat("─", max(boxWidth-2, 0))+"┘"))
	// Zu hoch: unten abschneiden (Leiste oben bleibt frei)
	if limit := height - 1; len(lines) > limit && limit > 0 {
		lines = lines[:limit]
	}
	left := 0
	if offsets := fmMenuTitleOffsets(menus); m.fm.menuIndex < len(offsets) {
		left = max(offsets[m.fm.menuIndex]-1, 0)
	}
	left = max(min(left, width-boxWidth), 0)
	return lines, boxWidth, left
}
