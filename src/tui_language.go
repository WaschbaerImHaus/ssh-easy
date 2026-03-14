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

// renderLanguage rendert die Sprachauswahl-Ansicht.
// Zeigt alle verfügbaren Sprachen in ihren Muttersprachen-Namen (UTF-8).
//
// @param s - String-Builder für die Ausgabe
// @date   2026-03-14
func (m AppModel) renderLanguage(s *strings.Builder) {
	s.WriteString(titleStyle.Render(m.lang.LangSelectTitle))
	s.WriteString("\n\n")
	s.WriteString(m.lang.LangSelectPrompt)
	s.WriteString("\n\n")

	// Sprachenliste mit Cursor
	for i, opt := range AvailableLanguages {
		line := fmt.Sprintf("  %s", opt.Name)
		if i == m.langCursor {
			// Aktuell ausgewählte Sprache hervorheben
			s.WriteString(selectedStyle.Render("> " + line))
		} else {
			s.WriteString(normalStyle.Render("  " + line))
		}
		s.WriteString("\n")
	}

	s.WriteString(helpStyle.Render("\n" + m.lang.LangSelectHelp))
}
