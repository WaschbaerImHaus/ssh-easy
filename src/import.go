// Paket main - Import der Verbindungsliste (Build 52)
//
// Gegenstück zum Export (Taste "I" in der Liste, auch bei leerer Liste):
//  1. Dateischritt: Dateiname eingeben (Vorschlag: neueste
//     ssh-easy-export-*.json im Home-Ordner), Enter lädt
//  2. Auswahlschritt: Verbindungen der Datei mit Häkchen (wie beim Export).
//     Namen, die es schon gibt, sind als "(vorhanden)" markiert und
//     anfangs NICHT gewählt - wer sie wählt, ersetzt die vorhandene
//     Verbindung (lokale ID, Erstellzeit und Schlüsseleinstellung bleiben)
//
// Sicherheit - die Datei ist nicht vertrauenswürdig:
//   - höchstens maxImportBytes, nur das Format von connections.json
//   - jeder Eintrag muss Validate() und die Gruppen-Namensregel bestehen
//   - keine Steuer- oder unsichtbaren Formatzeichen in Name, Host, Benutzer
//     (sonst könnten Terminal-Escape-Sequenzen die Anzeige manipulieren);
//     ZWNJ/ZWJ bleiben erlaubt (z.B. Persisch)
//   - Schlüsselpfade werden NIE übernommen: eine präparierte Datei könnte
//     sonst beliebige Dateien als SSH-Schlüssel lesen lassen
//   - IDs werden neu vergeben, nur Verbindungen werden übernommen (keine
//     Programmeinstellungen wie Sprache oder Auto-Update)
//
// @author Kurt Ingwer
// @date   2026-10-30 16:21
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	// maxImportBytes - Obergrenze für Importdateien (1 MiB ≈ mehrere
	// tausend Verbindungen)
	maxImportBytes = 1 << 20
	// maxImportFieldLen - Obergrenze für Name, Host und Benutzer
	maxImportFieldLen = 255
)

var (
	// errImportFormat - Datei ist kein gültiger ssh-easy-Export
	errImportFormat = errors.New("import: invalid format")
	// errImportEmpty - Datei enthält keine gültige Verbindung
	errImportEmpty = errors.New("import: no valid connections")
	// errImportTooLarge - Datei überschreitet maxImportBytes
	errImportTooLarge = errors.New("import: file too large")
)

// importResult fasst das Ergebnis eines Imports zusammen.
type importResult struct {
	// Neu hinzugefügte und ersetzte Verbindungen
	added, replaced int
	// ID der ersten übernommenen Verbindung (wird danach ausgewählt)
	firstID string
}

// --- Fachlogik ---

// readImportFile liest und prüft eine Importdatei.
//
// @param path - Absoluter Pfad
// @return []Connection - Gültige Verbindungen (neue IDs, ohne Schlüssel)
// @return int - Zahl übersprungener Einträge (ungültig oder doppelt)
// @return error - os.ErrNotExist, errImportTooLarge, errImportFormat,
//
//	errImportEmpty oder Lesefehler
//
// @date   2026-10-17 23:40
func readImportFile(path string) ([]Connection, int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, errImportFormat
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxImportBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if len(data) > maxImportBytes {
		return nil, 0, errImportTooLarge
	}

	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, 0, errImportFormat
	}

	// Neue IDs: größer als alle bisherigen (Zeitstempel), innerhalb der
	// Datei fortlaufend - so gibt es keine Kollision, auch nicht bei der
	// groben Uhr unter Windows
	base := time.Now().UnixNano()
	now := time.Now().Format(time.RFC3339)
	seen := make(map[string]bool)
	var valid []Connection
	skipped := 0
	for _, conn := range cfg.Connections {
		clean, ok := sanitizeImportedConnection(conn)
		if !ok || seen[strings.ToLower(clean.Name)] {
			skipped++
			continue
		}
		seen[strings.ToLower(clean.Name)] = true
		clean.ID = strconv.FormatInt(base+int64(len(valid)), 10)
		clean.CreatedAt = now
		clean.UpdatedAt = now
		valid = append(valid, clean)
	}
	if len(valid) == 0 {
		return nil, skipped, errImportEmpty
	}
	return valid, skipped, nil
}

// sanitizeImportedConnection prüft einen Eintrag der Importdatei und
// entfernt Schlüsselbezüge sowie den Zustand der automatischen
// Schlüsselvergabe (AutoKeyOff, AutoKeyFailures).
//
// @param conn - Eintrag aus der Datei
// @return Connection - Bereinigte Verbindung
// @return bool - false wenn der Eintrag ungültig ist
// @date   2026-10-28 13:27
func sanitizeImportedConnection(conn Connection) (Connection, bool) {
	for _, field := range []string{conn.Name, conn.Host, conn.User} {
		if !safeImportText(field) {
			return Connection{}, false
		}
	}
	// Schlüssel nie übernehmen; ohne Schlüssel wäre "key" ungültig
	conn.KeyPath = ""
	if conn.AuthType == AuthKey {
		conn.AuthType = AuthPassword
	}
	// Zustand der Schlüsselvergabe nie aus der Datei übernehmen (auch nicht
	// Werte wie 7 oder -4): er gehört zum lokalen Rechner
	conn.AutoKeyOff = false
	conn.AutoKeyFailures = 0
	conn.Tunnels = append([]TunnelConfig{}, conn.Tunnels...)
	if conn.Validate() != nil || !validGroupName(conn.Name) {
		return Connection{}, false
	}
	return conn, true
}

// safeImportText prüft ein Textfeld: höchstens maxImportFieldLen Bytes,
// gültiges UTF-8, keine Steuerzeichen (C0/C1, z.B. ESC) und keine
// unsichtbaren Formatzeichen (z.B. Bidi-Steuerung) außer ZWNJ/ZWJ.
//
// @param text - Feldinhalt
// @return bool - true wenn unbedenklich
// @date   2026-10-17 23:40
func safeImportText(text string) bool {
	if len(text) > maxImportFieldLen {
		return false
	}
	for _, r := range text {
		if r == unicode.ReplacementChar || unicode.IsControl(r) {
			return false
		}
		if unicode.Is(unicode.Cf, r) && r != 0x200C && r != 0x200D {
			return false
		}
	}
	return true
}

// existingNames liefert die Namen vorhandener Verbindungen in Kleinbuchstaben.
//
// @param conns - Vorhandene Verbindungen
// @return map[string]bool - Namen
// @date   2026-10-17 23:40
func existingNames(conns []Connection) map[string]bool {
	names := make(map[string]bool, len(conns))
	for _, conn := range conns {
		names[strings.ToLower(conn.Name)] = true
	}
	return names
}

// mergeImport übernimmt die gewählten Verbindungen in die vorhandene Liste.
// Gleicher Name (Groß-/Kleinschreibung egal) = ersetzen: Host, Port,
// Benutzer, Tunnel und Name kommen aus der Datei; ID, Erstellzeit und eine
// lokale Schlüssel-Anmeldung sowie der lokale Zustand der automatischen
// Schlüsselvergabe (AutoKeyOff, AutoKeyFailures) bleiben erhalten.
//
// @param existing - Vorhandene Verbindungen
// @param imported - Verbindungen aus der Datei
// @param selected - Gewählte IDs (der importierten Verbindungen)
// @return []Connection - Neue Gesamtliste
// @return importResult - Zähler und erste übernommene ID
// @date   2026-10-28 13:27
func mergeImport(existing, imported []Connection, selected map[string]bool) ([]Connection, importResult) {
	merged := append([]Connection{}, existing...)
	index := make(map[string]int, len(merged))
	for i, conn := range merged {
		index[strings.ToLower(conn.Name)] = i
	}
	now := time.Now().Format(time.RFC3339)
	var res importResult
	for _, conn := range imported {
		if !selected[conn.ID] {
			continue
		}
		if i, ok := index[strings.ToLower(conn.Name)]; ok {
			local := merged[i]
			conn.ID = local.ID
			conn.CreatedAt = local.CreatedAt
			if local.AuthType == AuthKey && local.KeyPath != "" {
				conn.AuthType = AuthKey
				conn.KeyPath = local.KeyPath
			}
			// Ausgesetzte Schlüsselvergabe bleibt lokal erhalten, sonst würde
			// ein Import sie still wieder einschalten
			conn.AutoKeyOff = local.AutoKeyOff
			conn.AutoKeyFailures = local.AutoKeyFailures
			conn.UpdatedAt = now
			merged[i] = conn
			res.replaced++
		} else {
			index[strings.ToLower(conn.Name)] = len(merged)
			merged = append(merged, conn)
			res.added++
		}
		if res.firstID == "" {
			res.firstID = conn.ID
		}
	}
	return merged, res
}

// applyImport lädt die aktuelle Konfiguration, führt den Import zusammen
// und speichert (atomar). Andere Einstellungen bleiben unverändert.
//
// @param configPath - Pfad der Konfiguration
// @param imported - Verbindungen aus der Datei
// @param selected - Gewählte IDs
// @return importResult - Ergebnis
// @return error - Lade- oder Speicherfehler
// @date   2026-10-17 23:40
func applyImport(configPath string, imported []Connection, selected map[string]bool) (importResult, error) {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return importResult{}, err
	}
	var res importResult
	cfg.Connections, res = mergeImport(cfg.Connections, imported, selected)
	if err := SaveConfig(configPath, cfg); err != nil {
		return importResult{}, err
	}
	return res, nil
}

// latestExportFile sucht die neueste Exportdatei im Ordner.
//
// @param dir - Ordner (Home)
// @return string - Dateiname oder "" wenn keine vorhanden
// @date   2026-10-17 23:40
func latestExportFile(dir string) string {
	matches, _ := filepath.Glob(filepath.Join(dir, "ssh-easy-export-*.json"))
	best, bestTime := "", time.Time{}
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if best == "" || info.ModTime().After(bestTime) {
			best, bestTime = filepath.Base(path), info.ModTime()
		}
	}
	return best
}

// --- TUI ---

// openImport öffnet den Dateischritt des Imports.
//
// @return tea.Model - Modell in ViewImportFile
// @return tea.Cmd - Cursor-Blinken des Dateinamenfelds
// @date   2026-10-17 23:40
func (m AppModel) openImport() (tea.Model, tea.Cmd) {
	m.errorMsg = ""
	m.successMsg = ""
	m.state = ViewImportFile
	m.importPathInput = newPathInput(latestExportFile(m.exportHomeDir))
	m.importPathInput.CursorEnd()
	return m, m.importPathInput.Focus()
}

// handleImportFileKeys verarbeitet Tasten im Dateischritt.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando
// @date   2026-10-17 23:40
func (m AppModel) handleImportFileKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = ViewList
		m.errorMsg = ""
		return m, nil
	case "enter":
		return m.loadImport()
	}
	if isPasteKey(msg) {
		return m.pasteIntoFocusedInput()
	}
	if isNulKey(msg) {
		return m, nil
	}
	var cmd tea.Cmd
	m.importPathInput, cmd = m.importPathInput.Update(msg)
	return m, cmd
}

// loadImport liest die gewählte Datei und wechselt zur Auswahl.
//
// @return tea.Model - Auswahlschritt bzw. Dateischritt mit Fehlermeldung
// @return tea.Cmd - immer nil
// @date   2026-10-17 23:40
func (m AppModel) loadImport() (tea.Model, tea.Cmd) {
	path, err := resolveExportPath(m.importPathInput.Value(), m.exportHomeDir)
	if err != nil {
		m.errorMsg = m.tr(txtExportNoPath)
		return m, nil
	}
	conns, skipped, err := readImportFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		m.errorMsg = fmt.Sprintf(m.tr(txtImportNotFound), path)
		return m, nil
	case errors.Is(err, errImportTooLarge):
		m.errorMsg = m.tr(txtImportTooLarge)
		return m, nil
	case errors.Is(err, errImportFormat):
		m.errorMsg = m.tr(txtImportInvalid)
		return m, nil
	case errors.Is(err, errImportEmpty):
		m.errorMsg = m.tr(txtImportEmpty)
		return m, nil
	case err != nil:
		m.errorMsg = err.Error()
		return m, nil
	}

	m.importExisting = existingNames(m.connections)
	existing := m.importExisting
	m.importList = newConnChecklist(conns, func(c Connection) bool {
		return !existing[strings.ToLower(c.Name)]
	})
	m.importSkipped = skipped
	m.importFile = path
	m.importPathInput.Blur()
	m.state = ViewImportSelect
	m.errorMsg = ""
	return m, nil
}

// handleImportSelectKeys verarbeitet Tasten im Auswahlschritt.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando
// @date   2026-10-17 23:40
func (m AppModel) handleImportSelectKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Zurück zum Dateischritt (Dateiname bleibt stehen)
		m.state = ViewImportFile
		m.errorMsg = ""
		return m, m.importPathInput.Focus()
	case "enter":
		return m.runImport()
	}
	m.importList.handleKey(msg.String(), visibleRowLimit(m.termHeight))
	return m, nil
}

// runImport übernimmt die gewählten Verbindungen und speichert.
//
// @return tea.Model - Liste mit Erfolgsmeldung bzw. Auswahl mit Fehler
// @return tea.Cmd - immer nil
// @date   2026-10-17 23:40
func (m AppModel) runImport() (tea.Model, tea.Cmd) {
	if m.importList.count() == 0 {
		m.errorMsg = m.tr(txtExportNoneSelected)
		return m, nil
	}
	res, err := applyImport(m.configPath, m.importList.conns, m.importList.selected)
	if err != nil {
		m.errorMsg = err.Error()
		return m, nil
	}
	if m.sshManager != nil {
		m.sshManager.logger.Info("Import aus %s: %d neu, %d ersetzt", m.importFile, res.added, res.replaced)
	}
	m.configCache.Invalidate()
	m.reloadConfig()
	m.state = ViewList
	m.errorMsg = ""
	m.successMsg = fmt.Sprintf(m.tr(txtImportDone), res.added+res.replaced, res.replaced)
	m.selectConnectionByID(res.firstID)
	return m, nil
}

// renderImportFile rendert den Dateischritt.
//
// @param s - Ausgabe
// @date   2026-10-17 23:40
func (m AppModel) renderImportFile(s *strings.Builder) {
	s.WriteString(titleStyle.Render("  " + m.tr(txtImportTitle)))
	s.WriteString("\n\n")
	label := labelStyle.UnsetWidth()
	s.WriteString(label.Render("  "+m.tr(txtExportFile)) + " " + m.importPathInput.View())
	s.WriteString("\n" + helpStyle.UnsetMarginTop().Render("  "+m.tr(txtImportNoKeys)))
	if m.errorMsg != "" {
		s.WriteString("\n" + errorStyle.Render(m.lang.ErrPrefix+m.errorMsg))
	}
	s.WriteString(helpStyle.Render("\n" + m.tr(txtImportHelpFile)))
}

// renderImportSelect rendert den Auswahlschritt.
//
// @param s - Ausgabe
// @date   2026-10-17 23:40
func (m AppModel) renderImportSelect(s *strings.Builder) {
	s.WriteString(titleStyle.Render("  " + m.tr(txtImportTitle)))
	s.WriteString("\n")
	s.WriteString(helpStyle.UnsetMarginTop().Render("  " + m.importFile))
	s.WriteString("\n\n")

	existing := m.importExisting
	note := func(conn Connection, selected bool) string {
		if !existing[strings.ToLower(conn.Name)] {
			return ""
		}
		if selected {
			return m.tr(txtImportReplace)
		}
		return m.tr(txtImportExisting)
	}
	m.importList.render(s, visibleRowLimit(m.termHeight), true, note)

	label := labelStyle.UnsetWidth()
	s.WriteString("\n" + label.Render(fmt.Sprintf("  "+m.tr(txtExportCount), m.importList.count(), len(m.importList.conns))))
	if m.importSkipped > 0 {
		s.WriteString("\n" + helpStyle.UnsetMarginTop().Render(fmt.Sprintf("  "+m.tr(txtImportSkipped), m.importSkipped)))
	}
	s.WriteString("\n" + helpStyle.UnsetMarginTop().Render("  "+m.tr(txtImportNoKeys)))
	if m.errorMsg != "" {
		s.WriteString("\n" + errorStyle.Render(m.lang.ErrPrefix+m.errorMsg))
	}
	s.WriteString(helpStyle.Render("\n" + m.tr(txtImportHelpSelect)))
}
