// Paket main - TUI Host-Key-Dialog für ssh-easy
//
// Zeigt einen Dialog wenn sich der SSH-Host-Key eines Servers geändert hat.
// Der Nutzer kann den alten Key entfernen und die Verbindung neu aufbauen.
// Der Dialog warnt klar vor möglichen MITM-Angriffen.
//
// @author Kurt Ingwer
// @date   2026-03-08 00:00
package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// handleHostKeyChangedKeys verarbeitet Tasten im Host-Key-Geändert-Dialog.
// 'j' oder 'y' -> alten Key entfernen und Verbindung neu aufbauen
// 'n' oder Esc  -> Abbrechen
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando
// @date   2026-03-08 00:00
func (m AppModel) handleHostKeyChangedKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "y":
		// Alten Key entfernen und Verbindung neu versuchen
		hostname := m.hostKeyHostname
		wasPassword := m.hostKeyWasPassword
		password := m.hostKeyPassword
		connID := m.activeID

		// Verbindung aus der Liste holen
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

		connCopy := *conn
		manager := m.sshManager

		// Zum Verbinden-Zustand wechseln
		m.state = ViewConnecting
		m.errorMsg = ""
		m.successMsg = ""

		return m, func() tea.Msg {
			// Alten Host-Key aus known_hosts entfernen
			khPath, err := getKnownHostsPath()
			if err != nil {
				return sshErrorMsg{id: connID, err: fmt.Errorf("known_hosts Pfad: %w", err)}
			}
			if err := removeKnownHost(khPath, hostname); err != nil {
				return sshErrorMsg{id: connID, err: fmt.Errorf("Alter Key konnte nicht entfernt werden: %w", err)}
			}

			// Verbindung erneut versuchen
			if wasPassword {
				status, err := manager.ConnectWithPassword(connCopy, password)
				if err != nil {
					return sshErrorMsg{id: connID, err: err, returnToConnect: true}
				}
				return sshConnectedMsg{
					id:          connID,
					status:      status,
					wasPassword: true,
					conn:        connCopy,
				}
			}

			// Auto-Connect (Agent + Keys)
			status, err := manager.ConnectAuto(connCopy)
			if err != nil {
				if IsNetworkError(err) {
					return sshErrorMsg{id: connID, err: err}
				}
				return sshNeedPasswordMsg{id: connID}
			}
			return sshConnectedMsg{id: connID, status: status, wasPassword: false}
		}

	case "n", "esc":
		m.state = ViewList
	}

	return m, nil
}

// renderHostKeyChanged rendert den Host-Key-Änderungs-Warndialog.
// Zeigt klar an dass der Host-Key sich geändert hat und fragt den Nutzer
// ob er den alten Key entfernen und die Verbindung fortsetzen möchte.
//
// @param s - String-Builder fuer die Ausgabe
// @date   2026-03-08 00:00
func (m AppModel) renderHostKeyChanged(s *strings.Builder) {
	s.WriteString(errorStyle.Render("  SICHERHEITSWARNUNG: SSH HOST-KEY GEÄNDERT!"))
	s.WriteString("\n\n")

	hostname := m.hostKeyHostname
	if hostname == "" {
		hostname = "(unbekannt)"
	}

	// Warnungsbox
	var warn strings.Builder
	warn.WriteString(fmt.Sprintf("Der SSH-Schlüssel des Servers hat sich geändert!\n\n"))
	warn.WriteString(fmt.Sprintf("Host: %s\n\n", hostname))
	warn.WriteString("MÖGLICHE URSACHEN:\n")
	warn.WriteString("  - Server wurde neu installiert (legitim)\n")
	warn.WriteString("  - Server-Key wurde erneuert (legitim)\n")
	warn.WriteString("  - Man-in-the-Middle-Angriff (GEFÄHRLICH!)\n\n")
	warn.WriteString("Nur fortfahren wenn du weißt dass sich\n")
	warn.WriteString("der Server-Key geändert hat!")
	s.WriteString(infoBoxStyle.Render(warn.String()))
	s.WriteString("\n\n")

	s.WriteString("  Alten Host-Key entfernen und neu verbinden?\n\n")
	s.WriteString("  [j/y] Ja, ich weiss was ich tue   [n/Esc] Nein, abbrechen")
}
