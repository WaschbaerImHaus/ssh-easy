// Paket main - Datei-Manager (MC-Modus): Einfügen im Editor (F4)
//
// Im Textmodus des Editors fügen Strg+V, Shift+Einfg und Strg+Einfg den
// Inhalt der Zwischenablage (readClipboard) an der Cursorposition ein.
// Strg+Einfg ist ausdrücklich ein Wunsch des Nutzers (sonst oft "Kopieren").
//
// Wie die Tasten ankommen:
//   - Strg+V: überall als KeyCtrlV.
//   - Windows: Bubbletea v1 kennt VK_INSERT nicht; Einfg, Shift+Einfg und
//     Strg+Einfg kommen als einzelne NUL-Rune. Welche Kombination gemeint
//     ist, verrät erst der echte Tastenzustand (shiftInsertPressed,
//     ctrlInsertPressed).
//   - Unix: ESC[2;2~ (Shift+Einfg) und ESC[2;5~ (Strg+Einfg) kommen als
//     unbekannte CSI-Sequenzen; tui.go leitet sie bei offenem Editor hierher
//     (fmEditorAcceptsPaste/fmEditorPaste).
//
// Der Text wird vor dem Einfügen bereinigt (cleanEditPaste): CRLF und CR
// werden zu LF, Steuerzeichen außer Tab und LF fallen weg - auch bei einem
// vom Terminal gemeldeten Einfügen (Bracketed Paste). Die Datei darf dabei
// nicht größer als fmViewMaxBytes werden, sonst ließe sie sich danach nicht
// mehr öffnen.
//
// @author Kurt Ingwer
// @date   2026-11-07 12:10
package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// isEditorPasteKey prüft, ob ein Tastendruck im Editor "Einfügen" bedeutet:
// Strg+V, Shift+Einfg (falls ein Terminal es als Taste meldet) oder unter
// Windows die NUL-Rune bei gedrücktem Shift+Einfg bzw. Strg+Einfg. Die
// NUL-Rune mit Alt-Kennzeichen (Alt-Taste allein, etwa bei Alt+Tab) zählt
// nie. Strg+Einfg wird hier VOR jeder Einfg-Auswertung (isInsertKey)
// erkannt; in den Datei-Fenstern markiert dieselbe Eingabe weiter.
//
// @param msg - Tastendruck
// @return bool - true, wenn eingefügt werden soll
// @date   2026-11-07 11:51
func isEditorPasteKey(msg tea.KeyMsg) bool {
	if msg.Paste {
		// Vom Terminal gemeldetes Einfügen bringt den Text selbst mit
		return false
	}
	switch msg.String() {
	case "ctrl+v", "shift+insert":
		return true
	}
	if msg.Alt || !isNulKey(msg) || len(msg.Runes) != 1 {
		return false
	}
	return shiftInsertPressed() || ctrlInsertPressed()
}

// fmEditorAcceptsPaste prüft, ob gerade in den Text des Editors eingefügt
// werden darf: Datei-Manager-Ansicht, Editor offen und fertig geladen,
// Textmodus (nicht Hex), keine Hilfe, kein Dialog, keine Eingabezeile, kein
// laufendes Speichern. tui.go fragt das für die Unix-CSI-Sequenzen ab.
//
// @return bool - true, wenn Einfügen in den Text wirkt
// @date   2026-11-07 11:51
func (m AppModel) fmEditorAcceptsPaste() bool {
	if m.state != ViewFileManager || !m.fm.editing {
		return false
	}
	e := &m.fm.edit
	return !e.loading && !e.saving && !e.showHelp && !e.hexMode && e.text != nil &&
		e.dialog == edDialogNone && e.prompt == edPromptNone
}

// fmEditorPaste liest die Zwischenablage und fügt ihren Inhalt an der
// Cursorposition ein. Ist der Editor nicht bereit (fmEditorAcceptsPaste),
// die Zwischenablage leer oder nicht lesbar, ändert sich nichts.
//
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - kein Folgekommando
// @date   2026-11-07 11:51
func (m AppModel) fmEditorPaste() (tea.Model, tea.Cmd) {
	if !m.fmEditorAcceptsPaste() {
		return m, nil
	}
	text, err := readClipboard()
	if err != nil || text == "" {
		return m, nil
	}
	e := &m.fm.edit
	e.message = ""
	e.insertPasted(text, m.tr)
	width, _ := m.fmScreenSize()
	e.ensureVisible(width, m.fmEditorRows())
	return m, nil
}

// insertPasted fügt bereinigten Text an der Cursorposition ein - wie jede
// Änderung über e.edit (geändert merken, Block aufheben, Cursor dahinter).
// Ist der Rohtext größer als 4 * fmViewMaxBytes Bytes, wird schon VOR dem
// Bereinigen abgelehnt (eine riesige Zwischenablage soll nicht erst ganz
// durchlaufen werden). Bleibt nach dem Bereinigen nichts übrig, passiert
// nichts. Würde die Datei größer als fmViewMaxBytes, wird ebenfalls
// abgelehnt; beide Ablehnungen zeigen dieselbe Meldung.
//
// @param text - Text aus Zwischenablage oder Terminal (nicht vertrauenswürdig)
// @param tr - Übersetzungsfunktion für die Meldung
// @date   2026-11-07 12:10
func (e *fmEditor) insertPasted(text string, tr func(textKey) string) {
	tooLarge := fmt.Sprintf(tr(txtFMEditPasteTooLarge), formatSizeWithUnit(fmViewMaxBytes))
	// Grobe Vorprüfung auf die Rohgröße: genau 4 * Grenze ist noch erlaubt
	if int64(len(text)) > 4*fmViewMaxBytes {
		e.message = tooLarge
		return
	}
	clean := cleanEditPaste(text)
	if clean == "" {
		return
	}
	b := e.text
	// Größe der gespeicherten Datei danach: jeder eingefügte Zeilenumbruch
	// wird bei einer CRLF-Datei als \r\n geschrieben
	added := int64(len(clean))
	if b.crlf {
		added += int64(strings.Count(clean, "\n"))
	}
	if int64(len(b.bytes()))+added > fmViewMaxBytes {
		e.message = tooLarge
		return
	}
	runes := []rune(clean)
	e.edit(func() { e.cur = b.insert(e.cur, runes) })
}

// cleanEditPaste bereinigt eingefügten Text für den Editor: Zuerst werden
// CRLF und einzelnes CR zu LF (sonst klebten Zeilen zusammen), dann fallen
// alle Steuerzeichen außer Tab und LF weg (C0, DEL, C1). Von einer Escape-
// Sequenz bleibt so nur der druckbare Rest stehen - sie kann weder das
// Terminal beim Anzeigen noch die Datei steuern. Ungültiges UTF-8 wird beim
// Durchlaufen zu U+FFFD, damit der Text-Puffer gültig bleibt.
//
// @param text - Rohtext
// @return string - bereinigter Text (leer, wenn nichts übrig bleibt)
// @date   2026-11-07 11:51
func cleanEditPaste(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var out strings.Builder
	out.Grow(len(text))
	for _, r := range text {
		switch {
		case r == '\t' || r == '\n':
			out.WriteRune(r)
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			// Steuerzeichen: weglassen
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}
