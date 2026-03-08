// Paket main - TUI Formularansichten fuer ssh-easy
//
// Formulare zum Erstellen und Bearbeiten von Verbindungen sowie
// die Passwort-Eingabe fuer den Verbindungsaufbau.
// Nach erfolgreicher Passwort-Anmeldung wird automatisch ein SSH-Key
// generiert und auf dem Remote-Server deployed.
//
// @author Kurt Ingwer
// @date   2026-03-08 00:00
package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// handleFormKeys verarbeitet Tasten im Formular (Erstellen/Bearbeiten).
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando
// @date   2026-03-07 21:00
func (m AppModel) handleFormKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = ViewList
		m.errorMsg = ""
		return m, nil

	case "tab", "down":
		m.inputs[m.focusedInput].Blur()
		m.focusedInput = (m.focusedInput + 1) % fieldCount
		m.inputs[m.focusedInput].Focus()
		return m, textinput.Blink

	case "shift+tab", "up":
		m.inputs[m.focusedInput].Blur()
		m.focusedInput = (m.focusedInput - 1 + fieldCount) % fieldCount
		m.inputs[m.focusedInput].Focus()
		return m, textinput.Blink

	case "enter":
		conn, err := m.buildConnectionFromForm()
		if err != nil {
			m.errorMsg = err.Error()
			return m, nil
		}

		if m.state == ViewCreate {
			err = AddConnection(m.configPath, conn)
		} else {
			conn.ID = m.activeID
			err = UpdateConnection(m.configPath, conn)
		}

		if err != nil {
			m.errorMsg = err.Error()
			return m, nil
		}

		m.configCache.Invalidate()
		m.reloadConfig()
		m.state = ViewList
		m.successMsg = "Verbindung gespeichert!"
		m.errorMsg = ""
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
			m.errorMsg = "Verbindung nicht gefunden"
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
				// Host-Key hat sich geaendert: Dialog anzeigen
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
// @param s - String-Builder fuer die Ausgabe
// @param title - Titel des Formulars
// @date   2026-03-07 21:00
func (m AppModel) renderForm(s *strings.Builder, title string) {
	s.WriteString(titleStyle.Render("  " + title))
	s.WriteString("\n\n")

	labels := []string{
		"Name:",
		"Host:",
		"Port:",
		"Benutzer:",
		"Auth (password/key/agent):",
		"Key-Pfad:",
		"Tunnel-Ports (,):",
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
		s.WriteString("\n" + errorStyle.Render("  Fehler: "+m.errorMsg))
	}

	s.WriteString(helpStyle.Render("\n  Tab:Naechstes Feld  Enter:Speichern  Esc:Abbrechen"))
}

// renderConnect rendert die Passwort-Eingabe.
// Wird angezeigt wenn Auto-Auth (Agent + alle Keys) fehlschlug.
// Nach erfolgreicher Anmeldung wird automatisch ein SSH-Key generiert
// und auf dem Server deployed, sodass die naechste Verbindung ohne Passwort klappt.
//
// @param s - String-Builder fuer die Ausgabe
// @date   2026-03-08 00:00
func (m AppModel) renderConnect(s *strings.Builder) {
	name := ""
	for _, c := range m.connections {
		if c.ID == m.activeID {
			name = c.Name
			break
		}
	}

	s.WriteString(titleStyle.Render(fmt.Sprintf("  Verbinde mit: %s", name)))
	s.WriteString("\n\n")
	s.WriteString("  Kein passender SSH-Key gefunden. Bitte Passwort eingeben:\n")
	s.WriteString("  " + m.passwordInput.View())
	s.WriteString("\n")

	if m.errorMsg != "" {
		s.WriteString("\n" + errorStyle.Render("  Fehler: "+m.errorMsg))
		s.WriteString("\n")
	}

	s.WriteString(helpStyle.Render("\n  Nach erfolgreicher Anmeldung wird automatisch ein SSH-Key erstellt."))
	s.WriteString(helpStyle.Render("\n  Enter:Verbinden  Esc:Abbrechen"))
}

// fillFormFromConnection befuellt die Formularfelder mit Daten einer Verbindung.
//
// @param conn - Die Verbindung deren Daten eingetragen werden
// @date   2026-03-07 21:00
func (m *AppModel) fillFormFromConnection(conn Connection) {
	m.inputs[fieldName].SetValue(conn.Name)
	m.inputs[fieldHost].SetValue(conn.Host)
	m.inputs[fieldPort].SetValue(strconv.Itoa(conn.Port))
	m.inputs[fieldUser].SetValue(conn.User)
	m.inputs[fieldAuthType].SetValue(string(conn.AuthType))
	m.inputs[fieldKeyPath].SetValue(conn.KeyPath)

	ports := make([]string, 0, len(conn.Tunnels))
	for _, t := range conn.Tunnels {
		ports = append(ports, strconv.Itoa(t.LocalPort))
	}
	m.inputs[fieldTunnels].SetValue(strings.Join(ports, ","))
}

// buildConnectionFromForm erstellt eine Connection aus den Formularfeldern.
//
// @return Connection - Erstellte Verbindung
// @return error - Validierungsfehler
// @date   2026-03-07 21:00
func (m AppModel) buildConnectionFromForm() (Connection, error) {
	name := strings.TrimSpace(m.inputs[fieldName].Value())
	host := strings.TrimSpace(m.inputs[fieldHost].Value())
	portStr := strings.TrimSpace(m.inputs[fieldPort].Value())
	user := strings.TrimSpace(m.inputs[fieldUser].Value())
	authStr := strings.TrimSpace(m.inputs[fieldAuthType].Value())
	keyPath := strings.TrimSpace(m.inputs[fieldKeyPath].Value())
	tunnelStr := strings.TrimSpace(m.inputs[fieldTunnels].Value())

	port := 22
	if portStr != "" {
		var err error
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return Connection{}, fmt.Errorf("Port muss eine Zahl sein")
		}
	}

	// Auth-Typ bestimmen
	authType := AuthPassword
	switch authStr {
	case "key":
		authType = AuthKey
	case "agent":
		authType = AuthAgent
	}

	conn := NewConnection(name, host, port, user, authType)
	conn.KeyPath = keyPath

	// Tunnel-Ports parsen
	if tunnelStr != "" {
		parts := strings.Split(tunnelStr, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			tunnelPort, err := strconv.Atoi(p)
			if err != nil {
				return Connection{}, fmt.Errorf("Tunnel-Port '%s' ist keine gueltige Zahl", p)
			}
			conn.Tunnels = append(conn.Tunnels, TunnelConfig{
				LocalPort:  tunnelPort,
				RemotePort: tunnelPort,
				Enabled:    true,
			})
		}
	}

	if err := conn.Validate(); err != nil {
		return Connection{}, err
	}

	conn.UpdatedAt = time.Now().Format(time.RFC3339)
	return conn, nil
}
