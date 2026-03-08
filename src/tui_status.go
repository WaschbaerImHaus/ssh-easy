// Paket main - TUI Statusansicht für ssh-easy
//
// Zeigt den Verbindungsstatus und Tunnel-Details an.
// Ermöglicht über 't' das Öffnen einer interaktiven Remote-Shell.
//
// @author Kurt Ingwer
// @date   2026-03-08 00:00
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
// @return tea.Cmd - Folgekommando
// @date   2026-03-07 21:00
func (m AppModel) handleStatusKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.state = ViewList

	case "t":
		// Interaktive Remote-Shell öffnen
		status, _ := m.sshManager.GetStatus(m.activeID)
		if status != nil && status.Connected && status.SSHClient != nil {
			// TUI pausieren, SSH-PTY-Session starten, danach TUI wiederherstellen
			cmd := newSSHTerminalCmd(status.SSHClient, m.termWidth, m.termHeight)
			return m, tea.Exec(cmd, func(err error) tea.Msg {
				return terminalDoneMsg{err: err}
			})
		}
		m.errorMsg = "Keine aktive Verbindung"

	case "x":
		m.sshManager.Disconnect(m.activeID)
		m.successMsg = "Verbindung getrennt"
		m.state = ViewList
	}

	return m, nil
}

// renderStatus rendert die Statusanzeige einer aktiven Verbindung.
//
// @param s - String-Builder für die Ausgabe
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

	s.WriteString(helpStyle.Render("\n\n  t:Terminal  x:Trennen  Esc:Zurück"))
}
