// Paket main - Verbindung als Vorlage kopieren (Taste c im Hauptmenü)
//
// Wie "n" (neu), aber das Formular übernimmt Name, Host, Port, Benutzer und
// Tunnel der gewählten Verbindung (Host seit Build 85). Schlüssel werden nicht
// übernommen: Ein Schlüssel gehört zu genau einer Verbindung. Der Name bekommt
// einen hochzählenden Zusatz " #2", " #3" ... - der erste freie Wert ab 2.
// Der Name hält immer connectionNameLimit Runen ein (Grundname wird gekürzt).
//
// @author Kurt Ingwer
// @date   2026-10-30 16:21
package main

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// copySuffixPattern erkennt einen vorhandenen Kopie-Zusatz " #N" (N ≥ 2)
// am Namensende, damit die Kopie einer Kopie "web #3" heißt statt "web #2 #2".
var copySuffixPattern = regexp.MustCompile(`^(.*\S) #([2-9]|[1-9][0-9]+)$`)

// nextCopyName liefert den Namen für die Kopie einer Verbindung: Grundname
// plus " #N" mit dem kleinsten freien N ≥ 2. Namen werden wie beim Speichern
// ohne Rücksicht auf Groß-/Kleinschreibung verglichen. Das Ergebnis hat
// höchstens connectionNameLimit Runen: Ein zu langer Grundname wird
// runenweise gekürzt (sonst schnitte das Eingabefeld " #N" ab).
//
// @param name - Name der Vorlage
// @param existing - vorhandene Verbindungen
// @return string - freier Name
// @date   2026-10-30 16:21
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
		suffix := " #" + strconv.Itoa(n)
		candidate := fitCopyBase(base, connectionNameLimit-utf8.RuneCountInString(suffix)) + suffix
		if !taken[strings.ToLower(candidate)] {
			return candidate
		}
	}
}

// fitCopyBase kürzt den Grundnamen runenweise auf höchstens maxRunes Runen
// und entfernt danach Leerzeichen am Ende, damit vor " #N" genau ein
// Leerzeichen steht. Kurze Namen bleiben unverändert.
//
// @param base - Grundname (ohne Leerzeichen am Rand)
// @param maxRunes - erlaubte Länge in Runen (≥ 0)
// @return string - gekürzter Grundname
// @date   2026-10-30 16:21
func fitCopyBase(base string, maxRunes int) string {
	if maxRunes < 0 {
		maxRunes = 0
	}
	runes := []rune(base)
	if len(runes) <= maxRunes {
		return base
	}
	return strings.TrimRightFunc(string(runes[:maxRunes]), unicode.IsSpace)
}

// startCopyConnection öffnet das Formular "Neue Verbindung", vorbelegt mit
// der gewählten Verbindung (ohne Schlüssel). Der Fokus steht wie bei "n" auf
// dem Namen, der Cursor am Ende des Namens. Auf Gruppen- und
// Datei-Manager-Zeilen passiert nichts.
//
// @return tea.Model - Modell im Formular oder unverändert
// @return tea.Cmd - Cursor-Blinken oder nil
// @date   2026-10-30 16:21
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
	// SetValue lässt einen Cursor ungleich 0 stehen - ans Ende setzen,
	// damit zwei Rücktasten genau " #N" (bei N < 10) löschen
	m.inputs[fieldName].CursorEnd()
	m.focusedInput = fieldName
	m.inputs[fieldName].Focus()
	m.errorMsg = ""
	m.successMsg = ""
	return m, textinput.Blink
}
