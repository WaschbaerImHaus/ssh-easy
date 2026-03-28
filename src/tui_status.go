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
	cryptossh "golang.org/x/crypto/ssh"
)

// handleStatusKeys verarbeitet Tasten in der Statusansicht.
// ESC und x trennen die Verbindung und kehren zur Liste zurück.
// q kehrt ohne Trennen zurück.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando
// @date   2026-03-23 11:00
func (m AppModel) handleStatusKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		// Zurück zur Liste ohne zu trennen
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
		m.errorMsg = m.lang.NoActiveConn

	case "r":
		// SSH-Key vollständig entfernen: vom Server (authorized_keys),
		// lokal (Datei löschen), Config zurücksetzen, Verbindung trennen.
		var conn *Connection
		for i := range m.connections {
			if m.connections[i].ID == m.activeID {
				conn = &m.connections[i]
				break
			}
		}
		if conn == nil || conn.KeyPath == "" {
			// Keine Key-Auth konfiguriert – nichts zu entfernen
			m.errorMsg = m.lang.NoKeyMsg
			return m, nil
		}
		// SSH-Client aus aktiver Verbindung holen
		status, _ := m.sshManager.GetStatus(m.activeID)
		var sshClient *cryptossh.Client
		if status != nil {
			sshClient = status.SSHClient
		}
		// Async ausführen – SSH-Befehl kann kurz dauern
		connCopy := *conn
		client := sshClient
		configPath := m.configPath
		return m, func() tea.Msg {
			err := RemoveDeployedKey(connCopy, client, configPath)
			return sshKeyRemovedMsg{connID: connCopy.ID, err: err}
		}

	case "x":
		// Verbindung trennen und zur Liste zurück
		m.sshManager.Disconnect(m.activeID)
		m.successMsg = m.lang.DiscoMsg
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
		s.WriteString(m.lang.ConnNotFound)
		return
	}

	status, _ := m.sshManager.GetStatus(m.activeID)

	s.WriteString(titleStyle.Render(fmt.Sprintf(m.lang.StatusTitle, conn.Name)))
	s.WriteString("\n\n")

	var info strings.Builder
	info.WriteString(fmt.Sprintf("%s%s@%s:%d\n", m.lang.LabelServer, conn.User, conn.Host, conn.Port))
	info.WriteString(fmt.Sprintf("%s%s\n", m.lang.LabelAuth, conn.AuthType))

	if status != nil && status.Connected {
		info.WriteString(fmt.Sprintf("%s%s\n", m.lang.LabelStatus, connectedStyle.Render(m.lang.StatusConn)))
	} else {
		info.WriteString(fmt.Sprintf("%s%s\n", m.lang.LabelStatus, disconnectedStyle.Render(m.lang.StatusDisconn)))
	}

	// Ports die von anderen aktiven Verbindungen belegt sind vorher sammeln
	usedPorts := m.sshManager.GetUsedLocalPorts(m.activeID)

	info.WriteString(fmt.Sprintf("\n%s\n", m.lang.LabelTunnel))
	for _, t := range conn.Tunnels {
		if !t.Enabled {
			continue
		}
		var tunnelStatus string
		if status != nil {
			if errMsg, ok := status.TunnelErrors[t.LocalPort]; ok {
				// Tunnel-Startfehler aus diesem Verbindungsversuch
				tunnelStatus = errorStyle.Render(m.lang.TunnelErrPrefix + errMsg)
			} else if otherConn, conflict := usedPorts[t.LocalPort]; conflict {
				// Port wird von einer anderen aktiven Verbindung verwendet
				tunnelStatus = errorStyle.Render(fmt.Sprintf(m.lang.TunnelPortConflict, otherConn))
			} else {
				tunnelStatus = connectedStyle.Render(m.lang.TunnelActive)
			}
		} else {
			tunnelStatus = connectedStyle.Render(m.lang.TunnelActive)
		}
		info.WriteString(fmt.Sprintf("  localhost:%d -> remote:%d  %s\n",
			t.LocalPort, t.RemotePort, tunnelStatus))
	}

	s.WriteString(infoBoxStyle.Render(info.String()))

	s.WriteString(helpStyle.Render("\n\n" + m.lang.StatusHelp))
}
