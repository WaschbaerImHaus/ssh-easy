// Paket main - TUI Statusansicht fuer ssh-easy
//
// Zeigt den Verbindungsstatus und Tunnel-Details an.
//
// @author Reisen macht Spass... mit Pia und Dirk e.Kfm.
// @date   2026-03-07 21:00
package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// handleStatusKeys verarbeitet Tasten in der Statusansicht.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando
// @date   2026-03-07 21:00
func (m AppModel) handleStatusKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.state = ViewList

	case "x":
		m.sshManager.Disconnect(m.activeID)
		m.successMsg = "Verbindung getrennt"
		m.state = ViewList
	}

	return m, nil
}

// renderStatus rendert die Statusanzeige einer aktiven Verbindung.
//
// @param s - String-Builder fuer die Ausgabe
// @date   2026-03-07 21:00
func (m AppModel) renderStatus(s *strings.Builder) {
	var conn *Connection
	for i := range m.connections {
		if m.connections[i].ID == m.activeID {
			conn = &m.connections[i]
			break
		}
	}
	if conn == nil {
		s.WriteString("Verbindung nicht gefunden")
		return
	}

	status, _ := m.sshManager.GetStatus(m.activeID)

	s.WriteString(titleStyle.Render(fmt.Sprintf("  Status: %s", conn.Name)))
	s.WriteString("\n\n")

	var info strings.Builder
	info.WriteString(fmt.Sprintf("Server:  %s@%s:%d\n", conn.User, conn.Host, conn.Port))
	info.WriteString(fmt.Sprintf("Auth:    %s\n", conn.AuthType))

	if status != nil && status.Connected {
		info.WriteString(fmt.Sprintf("Status:  %s\n", connectedStyle.Render("Verbunden")))
	} else {
		info.WriteString(fmt.Sprintf("Status:  %s\n", disconnectedStyle.Render("Getrennt")))
	}

	info.WriteString("\nTunnel:\n")
	for _, t := range conn.Tunnels {
		if !t.Enabled {
			continue
		}
		tunnelStatus := connectedStyle.Render("aktiv")
		if status != nil {
			if errMsg, ok := status.TunnelErrors[t.LocalPort]; ok {
				tunnelStatus = errorStyle.Render("Fehler: " + errMsg)
			}
		}
		info.WriteString(fmt.Sprintf("  localhost:%d -> remote:%d  %s\n",
			t.LocalPort, t.RemotePort, tunnelStatus))
	}

	s.WriteString(infoBoxStyle.Render(info.String()))

	s.WriteString(helpStyle.Render("\n\n  x:Trennen  Esc:Zurueck"))
}
