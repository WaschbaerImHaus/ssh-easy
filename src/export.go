// Paket main - Export der Verbindungsliste (Build 51)
//
// Über die Taste "E" in der Liste lassen sich alle oder ausgewählte
// Verbindungen in eine JSON-Datei exportieren. Die Datei hat das Format von
// connections.json und kann auf einem anderen Rechner als Konfiguration
// verwendet werden.
//
// SSH-Schlüssel sind nicht Teil des Exports - weder die Schlüsseldateien
// noch ihr Pfad. Verbindungen mit Schlüssel-Anmeldung werden als
// Passwort-Verbindung exportiert (wie nach "Key entfernen"); der Auto-
// Connect probiert Agent und vorhandene Schlüssel trotzdem zuerst.
//
// Bedienung der Exportansicht:
//   - ↑/↓, Bild↑/Bild↓, Pos1/Ende: Zeile wählen
//   - Leertaste: Eintrag an/aus (auf einer Gruppe: alle Mitglieder)
//   - a: alle an bzw. alle aus
//   - Tab: Dateinamenfeld (dort sind Leertaste und "a" normale Zeichen)
//   - Enter: exportieren, Esc: abbrechen
//
// @author Kurt Ingwer
// @date   2026-10-17 23:10
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// errExportExists - Zieldatei existiert bereits (wird nie überschrieben)
var errExportExists = errors.New("export file exists")

// errExportEmptyPath - kein Dateiname angegeben
var errExportEmptyPath = errors.New("export path empty")

// --- Fachlogik ---

// buildExportConfig erzeugt den Exportinhalt aus den gewählten Verbindungen.
// Reihenfolge wie in der Liste; Schlüsselbezüge werden entfernt, die
// Originale bleiben unverändert (Tunnel werden kopiert).
//
// @param conns - Alle Verbindungen (sortiert)
// @param selected - Gewählte Verbindungs-IDs
// @return AppConfig - Exportinhalt (nur Version und Verbindungen)
// @date   2026-10-17 23:10
func buildExportConfig(conns []Connection, selected map[string]bool) AppConfig {
	cfg := NewAppConfig()
	for _, conn := range conns {
		if !selected[conn.ID] {
			continue
		}
		exported := conn
		exported.Tunnels = append([]TunnelConfig{}, conn.Tunnels...)
		// Kein Schlüssel im Export: ohne Schlüssel wäre "key" ungültig
		exported.KeyPath = ""
		if exported.AuthType == AuthKey {
			exported.AuthType = AuthPassword
		}
		cfg.Connections = append(cfg.Connections, exported)
	}
	return cfg
}

// resolveExportPath macht aus der Eingabe einen absoluten Pfad: "~" und
// relative Pfade beziehen sich auf den Home-Ordner (das Arbeitsverzeichnis
// ist unter Windows oft der nicht beschreibbare Programmordner).
//
// @param input - Eingabe des Nutzers
// @param home - Home-Ordner
// @return string - Absoluter Pfad
// @return error - errExportEmptyPath bei leerer Eingabe
// @date   2026-10-17 23:10
func resolveExportPath(input, home string) (string, error) {
	path := strings.TrimSpace(input)
	if path == "" {
		return "", errExportEmptyPath
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		path = filepath.Join(home, path[1:])
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(home, path)
	}
	return filepath.Clean(path), nil
}

// writeExportFile schreibt den Export als JSON (Rechte 0600: Hosts und
// Benutzernamen sind vertraulich). Eine vorhandene Datei wird nie
// überschrieben, ein Symlink am Ziel nie verfolgt (O_EXCL); der Zielordner
// wird bewusst nicht angelegt, damit Tippfehler auffallen.
//
// @param input - Dateiname laut Eingabe
// @param cfg - Exportinhalt
// @param home - Home-Ordner für relative Pfade
// @return string - Tatsächlich geschriebener Pfad
// @return error - errExportExists, errExportEmptyPath oder Schreibfehler
// @date   2026-10-17 23:10
func writeExportFile(input string, cfg AppConfig, home string) (path string, err error) {
	path, err = resolveExportPath(input, home)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return path, errExportExists
		}
		return path, err
	}
	// Bei jedem Fehler keine halbe Datei zurücklassen
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(path)
		}
	}()
	if _, err = f.Write(append(data, '\n')); err != nil {
		return path, err
	}
	if err = f.Close(); err != nil {
		return path, err
	}
	return path, nil
}

// defaultExportFileName schlägt einen Dateinamen mit Zeitstempel vor
// (ohne Doppelpunkte - unter Windows in Dateinamen verboten).
//
// @return string - z.B. "ssh-easy-export-20261017-2310.json"
// @date   2026-10-17 23:10
func defaultExportFileName() string {
	return "ssh-easy-export-" + time.Now().Format("20060102-1504") + ".json"
}

// --- TUI ---

// exportRows liefert die Zeilen der Exportansicht: wie die Liste, aber
// mit allen Gruppen aufgeklappt.
//
// @return []listRow - Zeilen
// @date   2026-10-17 23:10
func (m AppModel) exportRows() []listRow {
	expanded := make(map[string]bool)
	for _, row := range buildListRows(m.connections, nil) {
		if row.kind == rowGroup {
			expanded[row.groupKey] = true
		}
	}
	return buildListRows(m.connections, expanded)
}

// openExport öffnet die Exportansicht mit allen Verbindungen ausgewählt.
//
// @return tea.Model - Modell in ViewExport (bzw. Liste mit Fehler)
// @return tea.Cmd - Cursor-Blinken des Dateinamenfelds
// @date   2026-10-17 23:10
func (m AppModel) openExport() (tea.Model, tea.Cmd) {
	m.successMsg = ""
	if len(m.connections) == 0 {
		m.errorMsg = m.tr(txtExportNothing)
		return m, nil
	}
	m.errorMsg = ""
	m.state = ViewExport
	m.exportSelected = make(map[string]bool, len(m.connections))
	for _, conn := range m.connections {
		m.exportSelected[conn.ID] = true
	}
	m.exportCursor = 0
	m.exportOffset = 0
	m.exportFocusPath = false
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 1024
	input.Width = 60
	input.SetValue(defaultExportFileName())
	m.exportPathInput = input
	return m, nil
}

// exportRowIDs liefert die Verbindungs-IDs einer Zeile (Gruppe: alle
// Mitglieder).
//
// @param row - Zeile
// @return []string - IDs
// @date   2026-10-17 23:10
func (m AppModel) exportRowIDs(row listRow) []string {
	if row.kind == rowGroup {
		ids := make([]string, 0, len(row.children))
		for _, idx := range row.children {
			ids = append(ids, m.connections[idx].ID)
		}
		return ids
	}
	return []string{m.connections[row.connIndex].ID}
}

// exportSelectedCount zählt die gewählten Verbindungen.
//
// @return int - Anzahl
// @date   2026-10-17 23:10
func (m AppModel) exportSelectedCount() int {
	count := 0
	for _, conn := range m.connections {
		if m.exportSelected[conn.ID] {
			count++
		}
	}
	return count
}

// handleExportKeys verarbeitet Tasten in der Exportansicht.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando
// @date   2026-10-17 23:10
func (m AppModel) handleExportKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = ViewList
		m.errorMsg = ""
		return m, nil
	case "enter":
		return m.runExport()
	case "tab", "shift+tab":
		m.exportFocusPath = !m.exportFocusPath
		if m.exportFocusPath {
			m.exportPathInput.CursorEnd()
			return m, m.exportPathInput.Focus()
		}
		m.exportPathInput.Blur()
		return m, nil
	}

	// Dateinamenfeld: alle übrigen Tasten gehen ins Feld
	if m.exportFocusPath {
		if isPasteKey(msg) {
			return m.pasteIntoFocusedInput()
		}
		if isNulKey(msg) {
			return m, nil
		}
		var cmd tea.Cmd
		m.exportPathInput, cmd = m.exportPathInput.Update(msg)
		return m, cmd
	}

	rows := m.exportRows()
	limit := visibleRowLimit(m.termHeight)
	switch msg.String() {
	case "up", "k":
		m.exportCursor--
	case "down", "j":
		m.exportCursor++
	case "pgup":
		m.exportCursor -= limit
	case "pgdown":
		m.exportCursor += limit
	case "home":
		m.exportCursor = 0
	case "end":
		m.exportCursor = len(rows) - 1
	case " ", "space":
		if m.exportCursor >= 0 && m.exportCursor < len(rows) {
			ids := m.exportRowIDs(rows[m.exportCursor])
			// Ganz gewählt -> alle aus; sonst (keiner/teilweise) -> alle an
			allOn := true
			for _, id := range ids {
				allOn = allOn && m.exportSelected[id]
			}
			for _, id := range ids {
				m.exportSelected[id] = !allOn
			}
		}
	case "a":
		allOn := m.exportSelectedCount() == len(m.connections)
		for _, conn := range m.connections {
			m.exportSelected[conn.ID] = !allOn
		}
	}

	if m.exportCursor >= len(rows) {
		m.exportCursor = len(rows) - 1
	}
	if m.exportCursor < 0 {
		m.exportCursor = 0
	}
	m.exportOffset = scrollOffset(len(rows), m.exportCursor, m.exportOffset, limit)
	return m, nil
}

// runExport schreibt die gewählten Verbindungen in die Datei.
//
// @return tea.Model - Liste mit Erfolgsmeldung bzw. Exportansicht mit Fehler
// @return tea.Cmd - immer nil
// @date   2026-10-17 23:10
func (m AppModel) runExport() (tea.Model, tea.Cmd) {
	count := m.exportSelectedCount()
	if count == 0 {
		m.errorMsg = m.tr(txtExportNoneSelected)
		return m, nil
	}
	cfg := buildExportConfig(m.connections, m.exportSelected)
	path, err := writeExportFile(m.exportPathInput.Value(), cfg, m.exportHomeDir)
	switch {
	case errors.Is(err, errExportExists):
		m.errorMsg = fmt.Sprintf(m.tr(txtExportExists), path)
		return m, nil
	case errors.Is(err, errExportEmptyPath):
		m.errorMsg = m.tr(txtExportNoPath)
		return m, nil
	case err != nil:
		m.errorMsg = err.Error()
		return m, nil
	}
	if m.sshManager != nil {
		m.sshManager.logger.Info("%d Verbindungen exportiert nach %s", count, path)
	}
	m.state = ViewList
	m.errorMsg = ""
	m.successMsg = fmt.Sprintf(m.tr(txtExportDone), count, path)
	return m, nil
}

// renderExport rendert die Exportansicht.
//
// @param s - String-Builder für die Ausgabe
// @date   2026-10-17 23:10
func (m AppModel) renderExport(s *strings.Builder) {
	s.WriteString(titleStyle.Render("  " + m.tr(txtExportTitle)))
	s.WriteString("\n\n")

	rows := m.exportRows()
	limit := visibleRowLimit(m.termHeight)
	offset := scrollOffset(len(rows), m.exportCursor, m.exportOffset, limit)
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	if offset > 0 {
		s.WriteString(helpStyle.UnsetMarginTop().Render(fmt.Sprintf("  ↑ +%d", offset)) + "\n")
	}
	for i := offset; i < end; i++ {
		s.WriteString(m.renderExportRow(rows[i], i == m.exportCursor && !m.exportFocusPath))
		s.WriteString("\n")
	}
	if end < len(rows) {
		s.WriteString(helpStyle.UnsetMarginTop().Render(fmt.Sprintf("  ↓ +%d", len(rows)-end)) + "\n")
	}

	// labelStyle ohne feste Breite: hier stehen Zähler und Feld direkt hinter
	// der Beschriftung (nicht in Formularspalten)
	label := labelStyle.UnsetWidth()
	s.WriteString("\n" + label.Render(fmt.Sprintf("  "+m.tr(txtExportCount), m.exportSelectedCount(), len(m.connections))))
	s.WriteString("\n" + label.Render("  "+m.tr(txtExportFile)) + " " + m.exportPathInput.View())
	s.WriteString("\n" + helpStyle.UnsetMarginTop().Render("  "+m.tr(txtExportNoKeys)))

	if m.errorMsg != "" {
		s.WriteString("\n" + errorStyle.Render(m.lang.ErrPrefix+m.errorMsg))
	}
	s.WriteString(helpStyle.Render("\n" + m.tr(txtExportHelp)))
}

// renderExportRow rendert eine Zeile mit Auswahlkästchen:
// [x] gewählt, [ ] nicht gewählt, [~] Gruppe teilweise gewählt.
//
// @param row - Zeile
// @param selected - Cursor steht auf der Zeile
// @return string - Gerenderte Zeile
// @date   2026-10-17 23:10
func (m AppModel) renderExportRow(row listRow, selected bool) string {
	base := normalStyle
	if selected {
		base = selectedStyle
	}
	on := 0
	ids := m.exportRowIDs(row)
	for _, id := range ids {
		if m.exportSelected[id] {
			on++
		}
	}
	box := "[ ]"
	switch {
	case on == len(ids):
		box = "[x]"
	case on > 0:
		box = "[~]"
	}

	label := row.label
	switch row.kind {
	case rowGroup:
		label = fmt.Sprintf("%s (%d)", row.label, len(row.children))
	case rowChild:
		branch := "├"
		if row.lastChild {
			branch = "└"
		}
		return base.Render("    " + branch + " " + box + " " + label)
	}
	return base.Render("  " + box + " " + label)
}
