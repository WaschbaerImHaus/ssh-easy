// Paket main - TUI Formularansichten für ssh-easy
//
// Formulare zum Erstellen und Bearbeiten von Verbindungen sowie
// die Passwort-Eingabe für den Verbindungsaufbau.
// Nach erfolgreicher Passwort-Anmeldung wird automatisch ein SSH-Key
// generiert und auf dem Remote-Server deployed.
//
// @author Kurt Ingwer
// @date   2026-10-30 16:20
package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// handleFormKeys verarbeitet Tasten im Formular (Erstellen/Bearbeiten).
// Beim Bearbeiten bleiben AuthType, KeyPath und CreatedAt erhalten.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando
// @date   2026-10-30 16:20
func (m AppModel) handleFormKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Einfügen aus der Zwischenablage ins fokussierte Feld (Strg+V / Shift+Einf)
	if isPasteKey(msg) {
		return m.pasteIntoFocusedInput()
	}
	// NUL-Runen (Windows: Einfg ohne Shift, Strg+Plus) nie ins Feld schreiben
	if isNulKey(msg) {
		return m, nil
	}

	switch msg.String() {
	case "esc":
		m.state = ViewList
		m.errorMsg = ""
		return m, nil

	case "tab", "down", "enter":
		// Enter und Tab: zum nächsten Feld springen; beim letzten Feld speichern
		if msg.String() == "enter" && m.focusedInput == fieldCount-1 {
			// Letztes Feld + Enter → speichern (weiter unten)
			break
		}
		m.inputs[m.focusedInput].Blur()
		m.focusedInput = (m.focusedInput + 1) % fieldCount
		m.inputs[m.focusedInput].Focus()
		return m, textinput.Blink

	case "shift+tab", "up":
		m.inputs[m.focusedInput].Blur()
		m.focusedInput = (m.focusedInput - 1 + fieldCount) % fieldCount
		m.inputs[m.focusedInput].Focus()
		return m, textinput.Blink
	}

	// Enter im letzten Feld oder separater Speichern-Pfad
	if msg.String() == "enter" {
		conn, err := m.buildConnectionFromForm()
		if err != nil {
			m.errorMsg = err.Error()
			return m, nil
		}

		// Doppelpunkt = Gruppentrennzeichen: davor und danach muss Text stehen
		if !validGroupName(conn.Name) {
			m.errorMsg = m.tr(txtErrGroupName)
			return m, nil
		}

		// Doppelte Namen verbieten: Name muss in der Liste eindeutig sein.
		// Beim Bearbeiten wird die eigene Verbindung (m.activeID) ausgenommen.
		for _, existing := range m.connections {
			if strings.EqualFold(existing.Name, conn.Name) {
				if m.state == ViewCreate || existing.ID != m.activeID {
					m.errorMsg = m.lang.ErrDuplicateName
					return m, nil
				}
			}
		}

		if m.state == ViewCreate {
			// Neu (n) und Kopie (c): ohne Schlüssel, eigene Erstellzeit
			err = AddConnection(m.configPath, conn)
		} else {
			conn.ID = m.activeID
			// Das Formular kennt weder Authentifizierung noch Schlüssel.
			// Diese Felder und die Erstellzeit von der gespeicherten
			// Verbindung übernehmen, sonst gingen sie beim Bearbeiten verloren.
			m.keepStoredConnectionFields(&conn)
			err = UpdateConnection(m.configPath, conn)
		}

		if err != nil {
			m.errorMsg = err.Error()
			return m, nil
		}

		m.configCache.Invalidate()
		m.reloadConfig()
		m.state = ViewList
		m.successMsg = m.lang.SavedMsg
		m.errorMsg = ""
		// Neue bzw. bearbeitete Verbindung in der Liste markieren
		// (Gruppe wird dafür aufgeklappt, Scroll-Fenster folgt)
		m.selectConnectionByID(conn.ID)
		return m, nil
	}

	var cmd tea.Cmd
	m.inputs[m.focusedInput], cmd = m.inputs[m.focusedInput].Update(msg)
	return m, cmd
}

// handleConnectKeys verarbeitet Tasten bei der Passwort-Eingabe.
// Nach erfolgreicher Verbindung wird automatisch ein SSH-Key deployed.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando
// @date   2026-03-08 00:00
func (m AppModel) handleConnectKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Passwort aus der Zwischenablage einfügen (Strg+V / Shift+Einf)
	if isPasteKey(msg) {
		return m.pasteIntoFocusedInput()
	}
	// NUL-Runen (Windows: Einfg ohne Shift, Strg+Plus) nie ins Feld schreiben
	if isNulKey(msg) {
		return m, nil
	}

	switch msg.String() {
	case "esc":
		m.state = ViewList
		return m, nil

	case "enter":
		password := m.passwordInput.Value()
		connID := m.activeID

		var conn *Connection
		for i := range m.connections {
			if m.connections[i].ID == connID {
				conn = &m.connections[i]
				break
			}
		}
		if conn == nil {
			m.errorMsg = m.lang.ConnNotFound
			m.state = ViewList
			return m, nil
		}

		// Passwort-Verbindung im Hintergrund aufbauen
		connCopy := *conn
		pwCopy := password
		manager := m.sshManager
		return m, func() tea.Msg {
			status, err := manager.ConnectWithPassword(connCopy, pwCopy)
			if err != nil {
				// Host-Key hat sich geändert: Dialog anzeigen
				if IsHostKeyChangedError(err) {
					hostname := parseHostKeyChangedHostname(err)
					return sshHostKeyChangedMsg{
						connID:      connID,
						hostname:    hostname,
						wasPassword: true,
						password:    pwCopy,
					}
				}
				// Anderer Fehler -> in ViewConnect bleiben
				return sshErrorMsg{id: connID, err: err, returnToConnect: true}
			}
			// Verbunden: Key wird automatisch in Update() deployed
			return sshConnectedMsg{
				id:          connID,
				status:      status,
				wasPassword: true,
				conn:        connCopy,
			}
		}
	}

	var cmd tea.Cmd
	m.passwordInput, cmd = m.passwordInput.Update(msg)
	return m, cmd
}

// renderForm rendert das Formular zum Erstellen/Bearbeiten von Verbindungen.
//
// @param s - String-Builder für die Ausgabe
// @param title - Titel des Formulars
// @date   2026-03-07 21:00
func (m AppModel) renderForm(s *strings.Builder, title string) {
	s.WriteString(titleStyle.Render("  " + title))
	s.WriteString("\n\n")

	labels := []string{
		m.lang.LabelName,
		m.lang.LabelHost,
		m.lang.LabelPort,
		m.lang.LabelUser,
		m.lang.LabelTunnels,
	}

	for i, label := range labels {
		cursor := "  "
		if i == m.focusedInput {
			cursor = "> "
		}
		s.WriteString(fmt.Sprintf("%s%s %s\n",
			cursor,
			labelStyle.Render(label),
			m.inputs[i].View()))
	}

	if m.errorMsg != "" {
		s.WriteString("\n" + errorStyle.Render(m.lang.ErrPrefix+m.errorMsg))
	}

	s.WriteString(helpStyle.Render("\n" + m.lang.FormHelp))
}

// renderConnect rendert die Passwort-Eingabe.
// Wird angezeigt wenn Auto-Auth (Agent + alle Keys) fehlgeschlagen ist.
// Zeigt Host und Benutzername, damit klar ist, welches Passwort gefragt ist.
// Nach erfolgreicher Anmeldung wird automatisch ein SSH-Key generiert
// und auf dem Server deployed, sodass die nächste Verbindung ohne Passwort klappt.
//
// @param s - String-Builder für die Ausgabe
// @date   2026-10-16 10:00
func (m AppModel) renderConnect(s *strings.Builder) {
	var conn Connection
	for _, c := range m.connections {
		if c.ID == m.activeID {
			conn = c
			break
		}
	}

	s.WriteString(titleStyle.Render(fmt.Sprintf(m.lang.ConnectingTitle, conn.Name)))
	s.WriteString("\n\n")
	// Host und - in der Zeile darunter - Benutzer anzeigen, damit klar ist,
	// für welches Konto das Passwort eingegeben werden muss
	if conn.Host != "" {
		s.WriteString(formatLabeledLine(m.lang.LabelHost, fmt.Sprintf("%s:%d", conn.Host, conn.Port), m.lang.LabelUser))
		s.WriteString(formatLabeledLine(m.lang.LabelUser, conn.User, m.lang.LabelHost))
		s.WriteString("\n")
	}
	s.WriteString(m.lang.NoKeyFound + "\n")
	s.WriteString("  " + m.passwordInput.View())
	s.WriteString("\n")

	if m.errorMsg != "" {
		s.WriteString("\n" + errorStyle.Render(m.lang.ErrPrefix+m.errorMsg))
		s.WriteString("\n")
	}

	s.WriteString(helpStyle.Render("\n" + m.lang.AfterPWHint))
	s.WriteString(helpStyle.Render("\n" + m.lang.ConnectHelp))
}

// fillFormFromConnection befüllt die Formularfelder mit Daten einer Verbindung.
// Die Tunnel merkt sich das Modell zusätzlich als Kopie (formTunnelTemplate),
// damit Remote-Port und Enabled-Flag beim Speichern erhalten bleiben.
// Danach steht in jedem Feld der Cursor am Ende: SetValue lässt einen Cursor
// ungleich 0 stehen (Port-Feld mit "22": bei "2222" sonst auf Position 2).
//
// @param conn - Die Verbindung deren Daten eingetragen werden
// @date   2026-10-30 16:20
func (m *AppModel) fillFormFromConnection(conn Connection) {
	m.inputs[fieldName].SetValue(conn.Name)
	m.inputs[fieldHost].SetValue(conn.Host)
	m.inputs[fieldPort].SetValue(strconv.Itoa(conn.Port))
	m.inputs[fieldUser].SetValue(conn.User)

	ports := make([]string, 0, len(conn.Tunnels))
	for _, t := range conn.Tunnels {
		ports = append(ports, strconv.Itoa(t.LocalPort))
	}
	m.inputs[fieldTunnels].SetValue(strings.Join(ports, ","))

	// Eigene Kopie: spätere Änderungen an conn wirken nicht zurück
	m.formTunnelTemplate = append([]TunnelConfig(nil), conn.Tunnels...)

	for i := range m.inputs {
		m.inputs[i].CursorEnd()
	}
}

// keepStoredConnectionFields übernimmt beim Bearbeiten (ViewEdit) die Felder,
// die das Formular nicht kennt, von der gespeicherten Verbindung m.activeID:
// AuthType, KeyPath, CreatedAt sowie AutoKeyOff und AutoKeyFailures. Ohne sie
// bliebe ein Schlüssel nach dem Speichern verwaist, die Verbindung fiele auf
// AuthPassword zurück, und eine ausgesetzte Schlüsselvergabe liefe wieder an.
// Neu (n) und Kopie (c) rufen diese Funktion nicht auf und starten ohne diese
// Felder. Gibt es die Verbindung nicht (mehr), bleibt conn unverändert; das
// Speichern meldet dann ohnehin "nicht gefunden".
//
// @param conn - aus dem Formular gebaute Verbindung, wird ergänzt
// @date   2026-10-30 16:20
func (m AppModel) keepStoredConnectionFields(conn *Connection) {
	for _, stored := range m.connections {
		if stored.ID == m.activeID {
			conn.AuthType = stored.AuthType
			conn.KeyPath = stored.KeyPath
			conn.CreatedAt = stored.CreatedAt
			// Merker der automatischen Schlüsselvergabe (ausgesetzt, Fehlversuche)
			conn.AutoKeyOff = stored.AutoKeyOff
			conn.AutoKeyFailures = stored.AutoKeyFailures
			return
		}
	}
}

// formTunnels baut aus den lokalen Ports des Formulars die Tunnel-Liste.
// Gibt es im Template einen Eintrag mit gleichem LocalPort, wird er komplett
// übernommen (Remote-Port, Enabled); sonst gilt Port -> Port und aktiv.
// Die Reihenfolge der Eingabe bleibt erhalten, doppelte Ports bleiben doppelt
// (die Validierung meldet sie). Das Template wird nicht verändert, und das
// Ergebnis teilt keinen Speicher mit ihm.
//
// @param ports - lokale Ports in der Reihenfolge der Eingabe
// @param template - Tunnel der Vorlage (nil = keine)
// @return []TunnelConfig - neue Tunnel-Liste (nil bei leeren Ports)
// @date   2026-10-30 16:20
func formTunnels(ports []int, template []TunnelConfig) []TunnelConfig {
	var tunnels []TunnelConfig
	for _, port := range ports {
		tunnel := TunnelConfig{LocalPort: port, RemotePort: port, Enabled: true}
		for _, candidate := range template {
			if candidate.LocalPort == port {
				// Wertkopie der Struktur, kein gemeinsamer Speicher
				tunnel = candidate
				break
			}
		}
		tunnels = append(tunnels, tunnel)
	}
	return tunnels
}

// buildConnectionFromForm erstellt eine Connection aus den Formularfeldern.
// Tunnel entstehen über formTunnels aus den lokalen Ports und dem Template
// der Vorlage (e und c), damit Remote-Port und Enabled-Flag erhalten bleiben.
//
// @return Connection - Erstellte Verbindung
// @return error - Validierungsfehler
// @date   2026-10-30 16:20
func (m AppModel) buildConnectionFromForm() (Connection, error) {
	name := strings.TrimSpace(m.inputs[fieldName].Value())
	host := strings.TrimSpace(m.inputs[fieldHost].Value())
	portStr := strings.TrimSpace(m.inputs[fieldPort].Value())
	user := strings.TrimSpace(m.inputs[fieldUser].Value())
	tunnelStr := strings.TrimSpace(m.inputs[fieldTunnels].Value())

	port := 22
	if portStr != "" {
		var err error
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return Connection{}, fmt.Errorf("%s", m.lang.ErrPortMustBeNum)
		}
	}

	// Authentifizierung erfolgt automatisch (Agent → Keys → Passwort)
	conn := NewConnection(name, host, port, user, AuthPassword)

	// Tunnel-Ports parsen (nur lokale Ports, Details aus dem Template)
	var tunnelPorts []int
	if tunnelStr != "" {
		parts := strings.Split(tunnelStr, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			tunnelPort, err := strconv.Atoi(p)
			if err != nil {
				return Connection{}, fmt.Errorf(m.lang.ErrTunnelPort, p)
			}
			tunnelPorts = append(tunnelPorts, tunnelPort)
		}
	}
	// Ohne Ports bleibt die leere Liste von NewConnection stehen
	if tunnels := formTunnels(tunnelPorts, m.formTunnelTemplate); len(tunnels) > 0 {
		conn.Tunnels = tunnels
	}

	if err := conn.Validate(); err != nil {
		return Connection{}, err
	}

	conn.UpdatedAt = time.Now().Format(time.RFC3339)
	return conn, nil
}

// formatLabeledLine formatiert eine Zeile "  <Beschriftung> <Wert>" und
// richtet den Wert an der längeren der beiden Beschriftungen aus, damit
// Host und Benutzer untereinander stehen.
//
// @param label - Beschriftung dieser Zeile (z.B. "Host:")
// @param value - Anzuzeigender Wert
// @param otherLabel - Beschriftung der Nachbarzeile (für die Ausrichtung)
// @return string - Formatierte Zeile mit Zeilenumbruch
// @date   2026-10-16 10:00
func formatLabeledLine(label, value, otherLabel string) string {
	width := lipgloss.Width(label)
	if w := lipgloss.Width(otherLabel); w > width {
		width = w
	}
	padding := strings.Repeat(" ", width-lipgloss.Width(label))
	return "  " + label + padding + " " + value + "\n"
}
