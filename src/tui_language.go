// Paket main – TUI Sprachauswahl-Ansicht für ssh-easy
//
// Wird beim ersten Start angezeigt und ist jederzeit über 'l' erreichbar.
// Speichert die gewählte Sprache dauerhaft in der Konfigurationsdatei.
//
// @author Kurt Ingwer
// @date   2026-03-14
package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// handleLanguageKeys verarbeitet Tasten in der Sprachauswahl-Ansicht.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folge-Kommando
// @date   2026-03-14
func (m AppModel) handleLanguageKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.langCursor > 0 {
			m.langCursor--
		}

	case "down", "j":
		if m.langCursor < len(AvailableLanguages)-1 {
			m.langCursor++
		}

	case "enter":
		// Gewählte Sprache anwenden und speichern
		selected := AvailableLanguages[m.langCursor].Code
		m.language = selected
		m.lang = GetTranslations(selected)

		// Platzhalter der Eingabefelder auf neue Sprache aktualisieren
		if len(m.inputs) > fieldName {
			m.inputs[fieldName].Placeholder = m.lang.PlaceholderName
		}
		m.passwordInput.Placeholder = m.lang.PlaceholderPW
		if len(m.keygenInputs) > keygenFieldPassphrase {
			m.keygenInputs[keygenFieldPassphrase].Placeholder = m.lang.PlaceholderPass
		}

		// Sprache in Konfiguration speichern
		m.persistLanguage(selected)

		m.state = ViewList
		m.errorMsg = ""
		m.successMsg = ""

	case "esc":
		// ESC nur wenn bereits eine Sprache gewählt ist (also nicht beim ersten Start)
		if m.language != "" {
			m.state = ViewList
		}
	}

	return m, nil
}

// persistLanguage speichert die gewählte Sprache dauerhaft in der JSON-Konfiguration.
// Bei einem Fehler wird dieser still ignoriert (nicht kritisch für den Betrieb).
//
// @param lang - Zu speichernder Sprachcode
// @date   2026-03-14
func (m *AppModel) persistLanguage(lang Language) {
	cfg, err := LoadConfig(m.configPath)
	if err != nil {
		return
	}
	cfg.Language = lang
	// Fehler beim Speichern ignorieren – nächster Start zeigt wieder Auswahl
	_ = SaveConfig(m.configPath, cfg)
	m.configCache.Invalidate()
}

// langPageSize legt fest, wie viele Sprachen gleichzeitig sichtbar sind.
const langPageSize = 8

// renderLanguage rendert die Sprachauswahl-Ansicht.
// Zeigt genau langPageSize Einträge als zweispaltige Tabelle mit Scroll-Pfeilen:
//   ↑  – es gibt weitere Einträge oberhalb des sichtbaren Bereichs
//   ↓  – es gibt weitere Einträge unterhalb des sichtbaren Bereichs
//
// Das sichtbare Fenster scrollt automatisch mit dem Cursor mit.
//
// @param s - String-Builder für die Ausgabe
// @date   2026-03-14
func (m AppModel) renderLanguage(s *strings.Builder) {
	total := len(AvailableLanguages)

	// Startindex des sichtbaren Fensters berechnen:
	// Der Cursor soll stets mittig im Fenster bleiben, soweit möglich.
	winStart := m.langCursor - langPageSize/2
	if winStart < 0 {
		winStart = 0
	}
	if winStart+langPageSize > total {
		winStart = total - langPageSize
	}
	winEnd := winStart + langPageSize

	s.WriteString(titleStyle.Render(m.lang.LangSelectTitle))
	s.WriteString("\n\n")
	s.WriteString(m.lang.LangSelectPrompt)
	s.WriteString("\n\n")

	// Pfeil oben: zeigt an dass es weitere Einträge oberhalb gibt
	if winStart > 0 {
		s.WriteString(helpStyle.Render("  ↑ more"))
	} else {
		s.WriteString("        ") // gleich viel Platz damit Layout stabil bleibt
	}
	s.WriteString("\n")

	// Sichtbaren Ausschnitt der Sprachenliste rendern
	for i := winStart; i < winEnd; i++ {
		opt := AvailableLanguages[i]
		// Englischer Name auf 10 Zeichen aufgefüllt, dann nativer Name (UTF-8)
		line := fmt.Sprintf("%-10s  %s", opt.English, opt.Name)
		if i == m.langCursor {
			s.WriteString(selectedStyle.Render("> " + line))
		} else {
			s.WriteString(normalStyle.Render("  " + line))
		}
		s.WriteString("\n")
	}

	// Pfeil unten: zeigt an dass es weitere Einträge unterhalb gibt
	if winEnd < total {
		s.WriteString(helpStyle.Render("  ↓ more"))
	} else {
		s.WriteString("        ")
	}
	s.WriteString("\n")

	s.WriteString(helpStyle.Render("\n" + m.lang.LangSelectHelp))
}
