// Paket main - TUI Listenansicht für ssh-easy
//
// Rendert die Hauptansicht mit Verbindungsliste und verarbeitet
// deren Tasteneingaben. Startet beim Verbinden automatisch den
// Auto-Connect (Agent + alle verfügbaren Keys) ohne Nutzerabfrage.
//
// @author Kurt Ingwer
// @date   2026-10-31 02:06
package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// handleListKeys verarbeitet Tasten in der Listenansicht.
// Der Cursor zeigt auf eine Zeile von listRows() - das kann eine
// Verbindung, eine Gruppe oder ein Gruppenmitglied sein.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando
// @date   2026-10-31 02:06
func (m AppModel) handleListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.listRows()
	limit := visibleRowLimit(m.termHeight)
	var row *listRow
	if m.cursor >= 0 && m.cursor < len(rows) {
		row = &rows[m.cursor]
	}

	switch msg.String() {
	case "q":
		// Alle Verbindungen trennen vor dem Beenden
		m.sshManager.DisconnectAll()
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if m.cursor < len(rows)-1 {
			m.cursor++
		}

	case "pgup":
		m.cursor -= limit

	case "pgdown":
		m.cursor += limit

	case "home":
		m.cursor = 0

	case "end":
		m.cursor = len(rows) - 1

	case "right":
		// Gruppe aufklappen
		if row != nil && row.kind == rowGroup {
			m.expandedGroups[row.groupKey] = true
		}

	case "left":
		// Gruppe zuklappen; auf einem Mitglied: Cursor zurück auf die Gruppe
		if row != nil && (row.kind == rowGroup || row.kind == rowChild) {
			key := row.groupKey
			delete(m.expandedGroups, key)
			m.cursor = m.groupRowIndex(key)
		}

	case "n":
		// Neue Verbindung - Formular zurücksetzen
		m.state = ViewCreate
		m.inputs = createFormInputs()
		m.inputs[fieldName].Placeholder = m.lang.PlaceholderName
		// Keine Tunnel-Vorlage aus einem vorherigen e/c übernehmen
		m.formTunnelTemplate = nil
		m.focusedInput = 0
		m.inputs[0].Focus()
		m.errorMsg = ""
		m.successMsg = ""
		return m, textinput.Blink

	case "c":
		// Neue Verbindung als Kopie der gewählten (ohne Schlüssel)
		return m.startCopyConnection()

	case "r":
		// Fenster- und Schriftgröße auf den Stand beim Start zurücksetzen
		return m.resetConsoleSize()

	case "m":
		// Hauptmenü (Tastenhilfe unten) ein-/ausblenden - die Tasten wirken immer
		m.showMenu = !m.showMenu

	case "e":
		// Verbindung bearbeiten (nicht auf Gruppenzeilen)
		if conn, ok := m.selectedConnection(); ok {
			m.state = ViewEdit
			m.activeID = conn.ID
			m.inputs = createFormInputs()
			m.inputs[fieldName].Placeholder = m.lang.PlaceholderName
			m.fillFormFromConnection(conn)
			m.focusedInput = 0
			m.inputs[0].Focus()
			m.errorMsg = ""
			m.successMsg = ""
			return m, textinput.Blink
		}

	case "d":
		// Verbindung löschen (Bestätigung)
		if conn, ok := m.selectedConnection(); ok {
			m.state = ViewDelete
			m.activeID = conn.ID
			m.errorMsg = ""
			m.successMsg = ""
		}

	case "enter":
		// Datei-Manager-Eintrag ganz unten (gibt es erst ab einer Verbindung):
		// öffnet die Auswahl links; ein oder zwei Fenster entscheidet rechts
		if row != nil && row.kind == rowFileManager {
			return m.openFileManager()
		}
		// Gruppe: auf-/zuklappen
		if row != nil && row.kind == rowGroup {
			if m.expandedGroups[row.groupKey] {
				delete(m.expandedGroups, row.groupKey)
			} else {
				m.expandedGroups[row.groupKey] = true
			}
			break
		}
		// Verbindung herstellen (Auto-Connect) oder Statusansicht
		if conn, ok := m.selectedConnection(); ok {
			// Wahl merken (nur bei eingeschaltetem Schalter, auch wenn schon
			// verbunden). Ein Speicherfehler verhindert das Verbinden nicht.
			saveErr := m.rememberLastConnection(conn.ID)
			if m.sshManager.IsConnected(conn.ID) {
				// Bereits verbunden: Statusansicht anzeigen
				m.state = ViewStatus
				m.activeID = conn.ID
				if saveErr != nil {
					m.errorMsg = saveErr.Error()
				}
				return m, nil
			}
			updated, cmd := m.startAutoConnect(conn)
			if next, isApp := updated.(AppModel); isApp && saveErr != nil {
				next.errorMsg = saveErr.Error()
				return next, cmd
			}
			return updated, cmd
		}

	case "x":
		// Verbindung trennen
		if conn, ok := m.selectedConnection(); ok && m.sshManager.IsConnected(conn.ID) {
			m.sshManager.Disconnect(conn.ID)
			m.successMsg = m.lang.DisconnectedMsg + conn.Name
		}

	case "u":
		// Auto-Update ein-/ausschalten (Menüzeile ganz unten)
		return m.toggleAutoUpdate()

	case "v":
		// Schalter "Verbindung merken" umschalten (sofort gespeichert)
		return m.toggleRememberSelection()

	case "E":
		// Verbindungsliste exportieren (alle oder Auswahl, ohne Schlüssel)
		return m.openExport()

	case "I":
		// Verbindungen aus einer Exportdatei importieren
		return m.openImport()

	case "l":
		// Sprache wechseln
		m.state = ViewLanguage
		m.errorMsg = ""
		m.successMsg = ""
	}

	m.clampListCursor()
	return m, nil
}

// startAutoConnect startet den automatischen Verbindungsaufbau
// (Agent + alle verfügbaren Keys) im Hintergrund.
//
// @param conn - Zu verbindende Verbindung
// @return tea.Model - Modell in ViewConnecting
// @return tea.Cmd - Hintergrund-Kommando für den Verbindungsaufbau
// @date   2026-10-16 10:00
func (m AppModel) startAutoConnect(conn Connection) (tea.Model, tea.Cmd) {
	m.state = ViewConnecting
	m.activeID = conn.ID
	m.errorMsg = ""
	m.successMsg = ""
	connCopy := conn
	manager := m.sshManager
	return m, func() tea.Msg {
		status, err := manager.ConnectAuto(connCopy)
		if err != nil {
			// Host-Key hat sich geändert: Dialog anzeigen
			if IsHostKeyChangedError(err) {
				hostname := parseHostKeyChangedHostname(err)
				return sshHostKeyChangedMsg{
					connID:      connCopy.ID,
					hostname:    hostname,
					wasPassword: false,
				}
			}
			// Netzwerkfehler: direkt melden
			if IsNetworkError(err) {
				return sshErrorMsg{id: connCopy.ID, err: err}
			}
			// Auth-Fehler oder kein Key: Passwort abfragen
			return sshNeedPasswordMsg{id: connCopy.ID}
		}
		return sshConnectedMsg{
			id:          connCopy.ID,
			status:      status,
			wasPassword: false,
		}
	}
}

// listRows liefert die aktuell sichtbaren Zeilen der Verbindungsliste.
// Ganz unten steht genau ein Datei-Manager-Eintrag - aber nur, wenn es
// mindestens eine Verbindung gibt (Build 87). Ohne Verbindung ist die
// Liste leer.
//
// @return []listRow - Zeilen (Gruppen, Mitglieder, Einzelverbindungen,
// Datei-Manager)
// @date   2026-10-31 02:06
func (m AppModel) listRows() []listRow {
	rows := buildListRows(m.connections, m.expandedGroups)
	if len(m.connections) == 0 {
		return rows
	}
	return append(rows, listRow{kind: rowFileManager, connIndex: -1})
}

// toggleRememberSelection schaltet "Verbindung merken" um und speichert den
// Schalter sofort. Beim Ausschalten werden die gemerkte Verbindung und die
// gemerkte Wahl links/rechts im Datei-Manager gelöscht, damit beim
// Wiedereinschalten kein alter Stand zurückkommt. Der Cursor bleibt stehen.
// updateConfig lädt die Datei frisch und ändert nur diese Felder; parallel
// gespeicherte Schlüsselpfade bleiben erhalten.
//
// @return tea.Model - Modell mit neuem Schalterstand
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-31 02:06
func (m AppModel) toggleRememberSelection() (tea.Model, tea.Cmd) {
	on := !m.rememberSelection
	err := updateConfig(m.configPath, func(cfg *AppConfig) bool {
		cfg.RememberSelection = on
		if !on {
			cfg.LastConnectionID = ""
			cfg.FileManagerLastLeft = ""
			cfg.FileManagerLastRight = ""
		}
		return true
	})
	if err != nil {
		m.errorMsg = err.Error()
		return m, nil
	}
	m.configCache.Invalidate()
	m.rememberSelection = on
	m.errorMsg = ""
	return m, nil
}

// rememberLastConnection speichert die per Enter gewählte Verbindung als
// Vorauswahl für den nächsten Start - nur bei eingeschaltetem Schalter und
// nur, wenn sich der gespeicherte Wert ändert (sonst wird die Datei nicht
// neu geschrieben). Läuft synchron im Update; Cursorbewegungen rufen diese
// Funktion nie auf. Verglichen wird mit der Datei, nicht mit einem Stand im
// Modell: So greift auch eine Wahl nach dem Zurücksetzen mit r.
//
// @param id - ID der gewählten Verbindung
// @return error - Fehler beim Lesen oder Schreiben der Konfiguration
// @date   2026-10-31 02:06
func (m *AppModel) rememberLastConnection(id string) error {
	if !m.rememberSelection {
		return nil
	}
	changed := false
	err := updateConfig(m.configPath, func(cfg *AppConfig) bool {
		if cfg.LastConnectionID == id {
			return false
		}
		cfg.LastConnectionID = id
		changed = true
		return true
	})
	if err != nil {
		return err
	}
	if changed {
		m.configCache.Invalidate()
	}
	return nil
}

// selectedConnection liefert die Verbindung unter dem Cursor.
// Auf einer Gruppenzeile und dem Datei-Manager-Eintrag gibt es keine
// Verbindung (ok = false).
//
// @return Connection - Ausgewählte Verbindung
// @return bool - false bei leerer Liste, Gruppe oder Datei-Manager
// @date   2026-10-21 19:45
func (m AppModel) selectedConnection() (Connection, bool) {
	rows := m.listRows()
	if m.cursor < 0 || m.cursor >= len(rows) || rows[m.cursor].connIndex < 0 {
		return Connection{}, false
	}
	return m.connections[rows[m.cursor].connIndex], true
}

// groupRowIndex liefert die Zeilennummer eines Gruppenkopfs.
//
// @param key - Gruppenschlüssel (Kleinbuchstaben)
// @return int - Zeilennummer oder der aktuelle Cursor, falls nicht gefunden
// @date   2026-10-16 10:00
func (m AppModel) groupRowIndex(key string) int {
	for i, row := range m.listRows() {
		if row.kind == rowGroup && row.groupKey == key {
			return i
		}
	}
	return m.cursor
}

// clampListCursor hält Cursor und Scroll-Position im gültigen Bereich.
// Muss nach jeder Änderung von Cursor, Liste oder Fenstergröße laufen.
//
// @date   2026-10-16 10:00
func (m *AppModel) clampListCursor() {
	total := len(m.listRows())
	if m.cursor >= total {
		m.cursor = total - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.listOffset = scrollOffset(total, m.cursor, m.listOffset, visibleRowLimit(m.termHeight))
}

// selectConnectionByID setzt den Cursor auf eine Verbindung. Liegt sie in
// einer Gruppe, wird die Gruppe aufgeklappt. Das Scroll-Fenster folgt.
//
// @param id - ID der auszuwählenden Verbindung
// @date   2026-10-16 10:00
func (m *AppModel) selectConnectionByID(id string) {
	for _, conn := range m.connections {
		if conn.ID != id {
			continue
		}
		if group, _, ok := splitGroupName(conn.Name); ok {
			m.expandedGroups[strings.ToLower(group)] = true
		}
		break
	}
	for i, row := range m.listRows() {
		if row.connIndex >= 0 && m.connections[row.connIndex].ID == id {
			m.cursor = i
			break
		}
	}
	m.clampListCursor()
}

// renderList rendert die Hauptansicht mit der Verbindungsliste.
// Ab mehr als visibleRowLimit() Zeilen wird nur ein Ausschnitt gezeigt;
// Hinweise "↑ +N" / "↓ +N" nennen die Zahl der verborgenen Zeilen.
//
// @param s - String-Builder für die Ausgabe
// @date   2026-10-31 02:06
func (m AppModel) renderList(s *strings.Builder) {
	// Titel mit Version und Build, z.B. "ssh-easy v0.46 (Build 46)"
	s.WriteString(titleStyle.Render("  " + formatAppTitle(m.buildNumber)))
	s.WriteString("\n\n")

	rows := m.listRows()
	hasGroups := false
	if len(m.connections) == 0 {
		s.WriteString(m.lang.NoConnections + "\n")
		s.WriteString(m.lang.PressNToAdd + "\n")
	}
	limit := visibleRowLimit(m.termHeight)
	offset := scrollOffset(len(rows), m.cursor, m.listOffset, limit)
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	if offset > 0 {
		s.WriteString(helpStyle.UnsetMarginTop().Render(fmt.Sprintf("  ↑ +%d", offset)) + "\n")
	}
	for i := offset; i < end; i++ {
		// Datei-Manager-Eintrag mit Leerzeile von den Verbindungen absetzen
		if rows[i].kind == rowFileManager {
			s.WriteString("\n")
		}
		s.WriteString(m.renderListRow(rows[i], i == m.cursor))
		s.WriteString("\n")
	}
	if end < len(rows) {
		s.WriteString(helpStyle.UnsetMarginTop().Render(fmt.Sprintf("  ↓ +%d", len(rows)-end)) + "\n")
	}
	for _, row := range rows {
		if row.kind == rowGroup {
			hasGroups = true
			break
		}
	}

	// Meldungen anzeigen
	if m.errorMsg != "" {
		s.WriteString("\n" + errorStyle.Render(m.lang.ErrPrefix+m.errorMsg))
	}
	if m.successMsg != "" {
		s.WriteString("\n" + successStyle.Render("  "+m.successMsg))
	}

	m.renderListMenu(s, hasGroups)
}

// renderListMenu rendert das Hauptmenü unten: eingeblendet (Taste m) die
// Tastenhilfe mit dem Auto-Update-Schalter als letzter Zeile, sonst nur den
// Hinweis auf m. Eine Meldung der Update-Suche bleibt auch ohne Menü
// sichtbar - ausgeblendet wird nur die Tastenhilfe.
//
// @param s - String-Builder für die Ausgabe
// @param hasGroups - true wenn die Liste Gruppen enthält (Gruppenhilfe)
// @date   2026-10-31 02:06
func (m AppModel) renderListMenu(s *strings.Builder, hasGroups bool) {
	if !m.showMenu {
		if m.autoUpdate && m.updateStatus != "" {
			s.WriteString("\n" + m.renderAutoUpdateLine())
		}
		s.WriteString(helpStyle.Render("\n" + m.tr(txtMenuHint)))
		return
	}
	s.WriteString(helpStyle.Render("\n" + m.lang.HelpList))
	s.WriteString("\n" + helpStyle.UnsetMarginTop().Render(m.tr(txtHelpMenuExtra)))
	if hasGroups {
		s.WriteString("\n" + helpStyle.UnsetMarginTop().Render(m.tr(txtGroupHelp)))
	}
	s.WriteString("\n" + helpStyle.UnsetMarginTop().Render(m.tr(txtHelpExport)+m.tr(txtHelpImport)))

	// Schalter "Verbindung merken" direkt vor dem Auto-Update-Schalter
	remember := m.tr(txtRememberOff)
	if m.rememberSelection {
		remember = m.tr(txtRememberOn)
	}
	s.WriteString("\n" + helpStyle.UnsetMarginTop().Render(remember))

	// Auto-Update-Schalter ganz unten im Menü
	s.WriteString("\n" + m.renderAutoUpdateLine())
}

// renderListRow rendert eine Zeile der Verbindungsliste.
//
// Jedes Teilstück (Einrückung, Statuspunkt, Name, Tunnel-Ports) wird mit
// demselben Grundstil gerendert. Früher wurde der grüne Punkt separat
// gerendert und in die markierte Zeile eingebettet - sein Reset-Code
// (ESC[0m) beendete dabei auch den Auswahl-Hintergrund, sodass der Name
// verbundener Einträge schwarz hinterlegt blieb.
//
// @param row - Darzustellende Zeile
// @param selected - true wenn der Cursor auf der Zeile steht
// @return string - Gerenderte Zeile
// @date   2026-10-31 02:06
func (m AppModel) renderListRow(row listRow, selected bool) string {
	base := normalStyle
	if selected {
		base = selectedStyle
	}
	dotStyle := base.Foreground(lipgloss.Color("46")).Bold(true)

	// Datei-Manager-Eintrag: keine Verbindung, kein Statuspunkt, nie
	// ausgegraut (es gibt ihn nur ab einer Verbindung)
	if row.kind == rowFileManager {
		return base.Render("    ▸ " + m.tr(txtFMEntry))
	}

	// Statuspunkt: Verbindung verbunden bzw. mindestens ein Gruppenmitglied
	connected := false
	if row.kind == rowGroup {
		for _, idx := range row.children {
			if m.sshManager.IsConnected(m.connections[idx].ID) {
				connected = true
				break
			}
		}
	} else {
		connected = m.sshManager.IsConnected(m.connections[row.connIndex].ID)
	}

	var line strings.Builder
	line.WriteString(base.Render("  "))
	if connected {
		line.WriteString(dotStyle.Render("● "))
	} else {
		line.WriteString(base.Render("  "))
	}

	switch row.kind {
	case rowGroup:
		arrow := "►"
		if m.expandedGroups[row.groupKey] {
			arrow = "▼"
		}
		line.WriteString(base.Render(fmt.Sprintf("%s %s (%d)", arrow, row.label, len(row.children))))
	case rowChild:
		branch := "├"
		if row.lastChild {
			branch = "└"
		}
		line.WriteString(base.Render("  " + branch + " " + row.label))
		line.WriteString(m.renderTunnelSummary(m.connections[row.connIndex], base))
	default:
		line.WriteString(base.Render(row.label))
		line.WriteString(m.renderTunnelSummary(m.connections[row.connIndex], base))
	}
	return line.String()
}

// maxListedTunnelPorts - so viele Tunnel-Ports werden in der Liste
// ausgeschrieben; ab dem dritten erscheint nur noch "+N weitere".
const maxListedTunnelPorts = 2

// renderTunnelSummary rendert die Tunnel-Kurzanzeige einer Verbindung,
// z.B. " [Tunnel: 3306,8080 +3 weitere]". Bei bestehender Verbindung
// erscheinen Ports, deren Tunnel nicht aufgebaut werden konnte, rot.
//
// @param conn - Verbindung
// @param base - Grundstil der Zeile (normal oder ausgewählt)
// @return string - Gerenderte Anzeige (leer ohne aktive Tunnel)
// @date   2026-10-16 10:00
func (m AppModel) renderTunnelSummary(conn Connection, base lipgloss.Style) string {
	ports := make([]int, 0, len(conn.Tunnels))
	for _, t := range conn.Tunnels {
		if t.Enabled {
			ports = append(ports, t.LocalPort)
		}
	}
	if len(ports) == 0 {
		return ""
	}

	// Fehlgeschlagene Tunnel nur bei bestehender Verbindung markieren
	var status *ConnectionStatus
	var usedPorts map[int]string
	if s, ok := m.sshManager.GetStatus(conn.ID); ok && s != nil && s.Connected {
		status = s
		usedPorts = m.sshManager.GetUsedLocalPorts(conn.ID)
	}
	failStyle := base.Foreground(lipgloss.Color("196"))

	// Format-String (z.B. " [Tunnel: %s]") in Präfix und Suffix zerlegen,
	// damit jeder Port einzeln eingefärbt werden kann
	prefix, suffix, _ := strings.Cut(m.lang.TunnelInfo, "%s")

	var out strings.Builder
	out.WriteString(base.Render(prefix))
	shown := ports
	if len(shown) > maxListedTunnelPorts {
		shown = shown[:maxListedTunnelPorts]
	}
	for i, port := range shown {
		if i > 0 {
			out.WriteString(base.Render(","))
		}
		style := base
		if tunnelFailed(status, port, usedPorts) {
			style = failStyle
		}
		out.WriteString(style.Render(strconv.Itoa(port)))
	}
	if more := len(ports) - len(shown); more > 0 {
		format := m.tr(txtMoreTunnelsMany)
		if more == 1 {
			format = m.tr(txtMoreTunnelsOne)
		}
		// Rot, wenn mindestens ein verborgener Tunnel nicht läuft
		style := base
		for _, port := range ports[len(shown):] {
			if tunnelFailed(status, port, usedPorts) {
				style = failStyle
				break
			}
		}
		out.WriteString(base.Render(" "))
		out.WriteString(style.Render(fmt.Sprintf(format, more)))
	}
	out.WriteString(base.Render(suffix))
	return out.String()
}

// renderConnecting rendert den Verbindungsaufbau-Bildschirm (Auto-Auth läuft).
//
// @param s - String-Builder für die Ausgabe
// @date   2026-03-08 00:00
func (m AppModel) renderConnecting(s *strings.Builder) {
	name := m.activeID
	for _, c := range m.connections {
		if c.ID == m.activeID {
			name = c.Name
			break
		}
	}

	s.WriteString(titleStyle.Render(fmt.Sprintf(m.lang.ConnectingTitle, name)))
	s.WriteString("\n\n")
	s.WriteString(m.lang.TryingAutoAuth + "\n\n")
	s.WriteString(helpStyle.Render(m.lang.PleaseWait))
}

// renderDeleteConfirm rendert die Löschbestätigung.
//
// @param s - String-Builder für die Ausgabe
// @date   2026-03-07 21:00
func (m AppModel) renderDeleteConfirm(s *strings.Builder) {
	s.WriteString(titleStyle.Render(m.lang.DeleteTitle))
	s.WriteString("\n\n")

	name := m.activeID
	for _, c := range m.connections {
		if c.ID == m.activeID {
			name = c.Name
			break
		}
	}

	s.WriteString(fmt.Sprintf(m.lang.DeleteConfirm, name))
	s.WriteString(m.lang.DeleteYesNo)
}

// handleDeleteKeys verarbeitet Tasten in der Löschbestätigung.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando
// @date   2026-10-31 02:06
func (m AppModel) handleDeleteKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "j":
		// Verbindung trennen falls aktiv und Auth-Cache-Eintrag löschen
		m.sshManager.Disconnect(m.activeID)
		m.sshManager.ClearAuthCache(m.activeID)

		// Verbindung löschen
		err := DeleteConnection(m.configPath, m.activeID)
		if err != nil {
			m.errorMsg = err.Error()
		} else {
			m.successMsg = m.lang.DeletedMsg
		}
		m.configCache.Invalidate()
		m.reloadConfig()
		m.clampListCursor()
		// Wurde die letzte Verbindung der Liste gelöscht, nicht auf dem
		// Datei-Manager-Eintrag stehen bleiben, sondern auf der Verbindung davor
		rows := m.listRows()
		for m.cursor > 0 && m.cursor < len(rows) && rows[m.cursor].kind == rowFileManager {
			m.cursor--
		}
		m.clampListCursor()
		m.state = ViewList

	case "n", "esc":
		m.state = ViewList
	}

	return m, nil
}
