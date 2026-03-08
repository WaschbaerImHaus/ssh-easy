// Paket main - TUI Key-Generierungs-Ansicht für ssh-easy
//
// Formular zur Generierung von SSH-Keys und Anzeige des Public Keys.
//
// @author Reisen macht Spaß... mit Pia und Dirk e.Kfm.
// @date   2026-03-07 21:00
package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// handleKeygenKeys verarbeitet Tasten im Keygen-Formular.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando
// @date   2026-03-07 21:00
func (m AppModel) handleKeygenKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = ViewList
		m.errorMsg = ""
		return m, nil

	case "tab", "down":
		m.keygenInputs[m.keygenFocused].Blur()
		m.keygenFocused = (m.keygenFocused + 1) % keygenFieldCount
		m.keygenInputs[m.keygenFocused].Focus()
		return m, textinput.Blink

	case "shift+tab", "up":
		m.keygenInputs[m.keygenFocused].Blur()
		m.keygenFocused = (m.keygenFocused - 1 + keygenFieldCount) % keygenFieldCount
		m.keygenInputs[m.keygenFocused].Focus()
		return m, textinput.Blink

	case "enter":
		keyPath := strings.TrimSpace(m.keygenInputs[keygenFieldPath].Value())
		passphrase := m.keygenInputs[keygenFieldPassphrase].Value()

		if keyPath == "" {
			m.errorMsg = "Dateipfad darf nicht leer sein"
			return m, nil
		}

		return m, func() tea.Msg {
			pubKey, err := GenerateSSHKey(keyPath, passphrase)
			return keygenResultMsg{pubKey: pubKey, err: err}
		}
	}

	var cmd tea.Cmd
	m.keygenInputs[m.keygenFocused], cmd = m.keygenInputs[m.keygenFocused].Update(msg)
	return m, cmd
}

// handleKeygenResultKeys verarbeitet Tasten in der Key-Ergebnisanzeige.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando
// @date   2026-03-07 21:00
func (m AppModel) handleKeygenResultKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter", "q":
		m.state = ViewList
	}
	return m, nil
}

// renderKeygen rendert das Formular zur SSH-Key-Generierung.
//
// @param s - String-Builder für die Ausgabe
// @date   2026-03-07 21:00
func (m AppModel) renderKeygen(s *strings.Builder) {
	s.WriteString(titleStyle.Render("  SSH-Key generieren (Ed25519)"))
	s.WriteString("\n\n")

	labels := []string{
		"Dateipfad:",
		"Passphrase (optional):",
	}

	for i, label := range labels {
		cursor := "  "
		if i == m.keygenFocused {
			cursor = "> "
		}
		s.WriteString(fmt.Sprintf("%s%s %s\n",
			cursor,
			labelStyle.Render(label),
			m.keygenInputs[i].View()))
	}

	if m.errorMsg != "" {
		s.WriteString("\n" + errorStyle.Render("  Fehler: "+m.errorMsg))
	}

	s.WriteString(helpStyle.Render("\n  Tab:Nächstes Feld  Enter:Generieren  Esc:Abbrechen"))
}

// renderKeygenResult rendert den generierten Public Key.
//
// @param s - String-Builder für die Ausgabe
// @date   2026-03-07 21:00
func (m AppModel) renderKeygenResult(s *strings.Builder) {
	s.WriteString(titleStyle.Render("  SSH-Key generiert!"))
	s.WriteString("\n\n")

	s.WriteString(successStyle.Render("  Schlüssel erfolgreich erstellt."))
	s.WriteString("\n\n")

	s.WriteString("  " + labelStyle.Render("Public Key:"))
	s.WriteString("\n\n")

	s.WriteString(infoBoxStyle.Render(strings.TrimSpace(m.generatedPubKey)))
	s.WriteString("\n\n")

	s.WriteString(helpStyle.Render("  Diesen Public Key auf dem Zielserver in ~/.ssh/authorized_keys eintragen."))
	s.WriteString(helpStyle.Render("\n  Enter/Esc:Zurück"))
}
