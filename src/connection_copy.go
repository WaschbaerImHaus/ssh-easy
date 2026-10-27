// Paket main - Verbindung als Vorlage kopieren (Taste c im Hauptmenü)
//
// Wie "n" (neu), aber das Formular übernimmt Name, Port, Benutzer und Tunnel
// der gewählten Verbindung. Host und Schlüssel werden nicht übernommen: Die
// Kopie ist für einen anderen Server gedacht, und ein Schlüssel gehört zu
// genau einer Verbindung. Der Name bekommt einen hochzählenden Zusatz " #2",
// " #3" ... - der erste freie Wert ab 2.
//
// @author Kurt Ingwer
// @date   2026-10-27 17:15
package main

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// copySuffixPattern erkennt einen vorhandenen Kopie-Zusatz " #N" (N ≥ 2)
// am Namensende, damit die Kopie einer Kopie "web #3" heißt statt "web #2 #2".
var copySuffixPattern = regexp.MustCompile(`^(.*\S) #([2-9]|[1-9][0-9]+)$`)

// nextCopyName liefert den Namen für die Kopie einer Verbindung: Grundname
// plus " #N" mit dem kleinsten freien N ≥ 2. Namen werden wie beim Speichern
// ohne Rücksicht auf Groß-/Kleinschreibung verglichen.
//
// @param name - Name der Vorlage
// @param existing - vorhandene Verbindungen
// @return string - freier Name
// @date   2026-10-27 17:15
func nextCopyName(name string, existing []Connection) string {
	base := strings.TrimSpace(name)
	if match := copySuffixPattern.FindStringSubmatch(base); match != nil {
		base = match[1]
	}
	taken := make(map[string]bool, len(existing))
	for _, conn := range existing {
		taken[strings.ToLower(strings.TrimSpace(conn.Name))] = true
	}
	for n := 2; ; n++ {
		candidate := base + " #" + strconv.Itoa(n)
		if !taken[strings.ToLower(candidate)] {
			return candidate
		}
	}
}

// startCopyConnection öffnet das Formular "Neue Verbindung", vorbelegt mit
// der gewählten Verbindung (ohne Host und Schlüssel). Der Fokus steht auf
// dem leeren Host-Feld. Auf Gruppen- und Datei-Manager-Zeilen passiert nichts.
//
// @return tea.Model - Modell im Formular oder unverändert
// @return tea.Cmd - Cursor-Blinken oder nil
// @date   2026-10-27 17:15
func (m AppModel) startCopyConnection() (tea.Model, tea.Cmd) {
	conn, ok := m.selectedConnection()
	if !ok {
		return m, nil
	}
	m.state = ViewCreate
	m.inputs = createFormInputs()
	m.inputs[fieldName].Placeholder = m.lang.PlaceholderName
	m.fillFormFromConnection(conn)
	m.inputs[fieldName].SetValue(nextCopyName(conn.Name, m.connections))
	m.inputs[fieldHost].SetValue("")
	m.focusedInput = fieldHost
	m.inputs[fieldHost].Focus()
	m.errorMsg = ""
	m.successMsg = ""
	return m, textinput.Blink
}
