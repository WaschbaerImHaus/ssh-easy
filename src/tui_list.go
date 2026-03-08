// Paket main - TUI Listenansicht fuer ssh-easy
//
// Rendert die Hauptansicht mit Verbindungsliste und verarbeitet
// deren Tasteneingaben. Startet beim Verbinden automatisch den
// Auto-Connect (Agent + alle verfuegbaren Keys) ohne Nutzerabfrage.
//
// @author Kurt Ingwer
// @date   2026-03-08 00:00
package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// handleListKeys verarbeitet Tasten in der Listenansicht.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando
// @date   2026-03-07 21:00
func (m AppModel) handleListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		if m.cursor < len(m.connections)-1 {
			m.cursor++
		}

	case "n":
		// Neue Verbindung - Formular zuruecksetzen
		m.state = ViewCreate
		m.inputs = createFormInputs()
		m.focusedInput = 0
		m.inputs[0].Focus()
		m.errorMsg = ""
		m.successMsg = ""
		return m, textinput.Blink

	case "e":
		// Verbindung bearbeiten
		if len(m.connections) > 0 {
			conn := m.connections[m.cursor]
			m.state = ViewEdit
			m.activeID = conn.ID
			m.inputs = createFormInputs()
			m.fillFormFromConnection(conn)
			m.focusedInput = 0
			m.inputs[0].Focus()
			m.errorMsg = ""
			m.successMsg = ""
			return m, textinput.Blink
		}

	case "d":
		// Verbindung loeschen (Bestaetigung)
		if len(m.connections) > 0 {
			m.state = ViewDelete
			m.activeID = m.connections[m.cursor].ID
			m.errorMsg = ""
			m.successMsg = ""
		}

	case "enter":
		// Verbindung herstellen (Auto-Connect) oder Statusansicht
		if len(m.connections) > 0 {
			conn := m.connections[m.cursor]
			if m.sshManager.IsConnected(conn.ID) {
				// Bereits verbunden: Statusansicht anzeigen
				m.state = ViewStatus
				m.activeID = conn.ID
			} else {
				// Auto-Connect: Agent + alle verfuegbaren Keys probieren
				m.state = ViewConnecting
				m.activeID = conn.ID
				m.errorMsg = ""
				m.successMsg = ""
				connCopy := conn
				manager := m.sshManager
				return m, func() tea.Msg {
					status, err := manager.ConnectAuto(connCopy)
					if err != nil {
						// Host-Key hat sich geaendert: Dialog anzeigen
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
		}

	case "x":
		// Verbindung trennen
		if len(m.connections) > 0 {
			conn := m.connections[m.cursor]
			if m.sshManager.IsConnected(conn.ID) {
				m.sshManager.Disconnect(conn.ID)
				m.successMsg = "Verbindung getrennt: " + conn.Name
			}
		}

	case "g":
		// SSH-Key generieren
		m.state = ViewKeygen
		m.keygenInputs = createKeygenInputs()
		m.keygenFocused = 0
		m.keygenInputs[0].Focus()
		m.errorMsg = ""
		m.successMsg = ""
		return m, textinput.Blink
	}

	return m, nil
}

// renderList rendert die Hauptansicht mit der Verbindungsliste.
//
// @param s - String-Builder fuer die Ausgabe
// @date   2026-03-07 21:00
func (m AppModel) renderList(s *strings.Builder) {
	s.WriteString(titleStyle.Render(fmt.Sprintf("  ssh-easy v%s", m.buildNumber)))
	s.WriteString("\n\n")

	if len(m.connections) == 0 {
		s.WriteString("  Keine Verbindungen gespeichert.\n")
		s.WriteString("  Druecke 'n' um eine neue Verbindung anzulegen.\n")
	} else {
		for i, conn := range m.connections {
			// Status-Indikator
			statusIcon := disconnectedStyle.Render("  ")
			if m.sshManager.IsConnected(conn.ID) {
				statusIcon = connectedStyle.Render("  ")
			}

			// Tunnel-Info
			tunnelInfo := ""
			if len(conn.Tunnels) > 0 {
				ports := make([]string, 0, len(conn.Tunnels))
				for _, t := range conn.Tunnels {
					if t.Enabled {
						ports = append(ports, strconv.Itoa(t.LocalPort))
					}
				}
				if len(ports) > 0 {
					tunnelInfo = fmt.Sprintf(" [Tunnel: %s]", strings.Join(ports, ","))
				}
			}

			// Auth-Info
			authInfo := ""
			if conn.AuthType == AuthAgent {
				authInfo = " [Agent]"
			}

			// Zeile formatieren
			line := fmt.Sprintf("%s %s (%s@%s:%d)%s%s",
				statusIcon, conn.Name, conn.User, conn.Host, conn.Port, tunnelInfo, authInfo)

			if i == m.cursor {
				s.WriteString(selectedStyle.Render(line))
			} else {
				s.WriteString(normalStyle.Render("  " + line))
			}
			s.WriteString("\n")
		}
	}

	// Meldungen anzeigen
	if m.errorMsg != "" {
		s.WriteString("\n" + errorStyle.Render("  Fehler: "+m.errorMsg))
	}
	if m.successMsg != "" {
		s.WriteString("\n" + successStyle.Render("  "+m.successMsg))
	}

	// Hilfe
	s.WriteString(helpStyle.Render("\n  n:Neu  e:Bearbeiten  d:Loeschen  Enter:Verbinden  x:Trennen  g:Key-Gen  q:Beenden"))
}

// renderConnecting rendert den Verbindungsaufbau-Bildschirm (Auto-Auth laeuft).
//
// @param s - String-Builder fuer die Ausgabe
// @date   2026-03-08 00:00
func (m AppModel) renderConnecting(s *strings.Builder) {
	name := m.activeID
	for _, c := range m.connections {
		if c.ID == m.activeID {
			name = c.Name
			break
		}
	}

	s.WriteString(titleStyle.Render(fmt.Sprintf("  Verbinde mit: %s", name)))
	s.WriteString("\n\n")
	s.WriteString("  Probiere SSH-Agent und verfuegbare Schluessel...\n\n")
	s.WriteString(helpStyle.Render("  Bitte warten"))
}

// renderDeleteConfirm rendert die Loeschbestaetigung.
//
// @param s - String-Builder fuer die Ausgabe
// @date   2026-03-07 21:00
func (m AppModel) renderDeleteConfirm(s *strings.Builder) {
	s.WriteString(titleStyle.Render("  Verbindung loeschen?"))
	s.WriteString("\n\n")

	name := m.activeID
	for _, c := range m.connections {
		if c.ID == m.activeID {
			name = c.Name
			break
		}
	}

	s.WriteString(fmt.Sprintf("  Soll die Verbindung '%s' wirklich geloescht werden?\n\n", name))
	s.WriteString("  [j/y] Ja   [n/Esc] Nein")
}

// handleDeleteKeys verarbeitet Tasten in der Loeschbestaetigung.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando
// @date   2026-03-07 21:00
func (m AppModel) handleDeleteKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "j":
		// Verbindung trennen falls aktiv
		m.sshManager.Disconnect(m.activeID)

		// Verbindung loeschen
		err := DeleteConnection(m.configPath, m.activeID)
		if err != nil {
			m.errorMsg = err.Error()
		} else {
			m.successMsg = "Verbindung geloescht!"
		}
		m.configCache.Invalidate()
		m.reloadConfig()
		if m.cursor >= len(m.connections) && m.cursor > 0 {
			m.cursor--
		}
		m.state = ViewList

	case "n", "esc":
		m.state = ViewList
	}

	return m, nil
}
