// Paket main - Datei-Manager (MC-Modus): Kopieren (F5) und Dialoge
//
// F5 kopiert die markierten Einträge des aktiven Fensters (ohne Markierung
// den Eintrag unter dem Cursor) in das aktuelle Verzeichnis des anderen
// Fensters. Ablauf in Dialogen wie im mc:
//  1. Bestätigung (Enter = kopieren, Esc/q = abbrechen)
//  2. Fortschritt (Esc = abbrechen; F10 und alle anderen Tasten ruhen)
//  3. Rückfrage, wenn ein Ziel schon existiert (j/n/a/k/u/g, Esc)
//
// Das eigentliche Kopieren läuft als Goroutine (runCopy, filemanager_copy.go)
// und meldet sich über einen Kanal. Es wartet immer genau ein Kommando
// (waitCopyEvent) auf die nächste Meldung - bei einer Rückfrage erst nach
// der Antwort, weil die Kopie bis dahin ruht.
//
// @author Kurt Ingwer
// @date   2026-10-21 21:18
package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// fmDialog ist der gerade offene Dialog des Datei-Managers.
type fmDialog int

const (
	// fmDialogNone - kein Dialog
	fmDialogNone fmDialog = iota
	// fmDialogCopyConfirm - "N Einträge nach … kopieren?"
	fmDialogCopyConfirm
	// fmDialogCopying - Fortschritt
	fmDialogCopying
	// fmDialogOverwrite - "Ziel existiert bereits"
	fmDialogOverwrite
)

// Höchstbreite der Dialoge in Zeichen
const fmDialogMaxWidth = 64

// fmCopyEventMsg überbringt eine Meldung des Kopiervorgangs.
type fmCopyEventMsg struct {
	session int
	event   copyEvent
}

// fmCopyState ist der Zustand eines Kopiervorgangs in der Oberfläche.
type fmCopyState struct {
	// Zu kopierende Namen und Quellseite
	names  []string
	source int
	// Letzter gemeldeter Fortschritt
	copyProgress copyProgress
	// Offene Rückfrage
	conflictPath string
	conflictInfo copyConflictInfo
	// Abbruch, Ereignis- und Antwortkanal der laufenden Kopie
	cancel  context.CancelFunc
	events  chan copyEvent
	answers chan copyConflictAnswer
}

// fmSelection liefert die zu bearbeitenden Namen des aktiven Fensters:
// alle markierten, sonst den Eintrag unter dem Cursor (nie "..").
//
// @return []string - Namen (leer = nichts gewählt)
// @date   2026-10-21 21:03
func (m AppModel) fmSelection() []string {
	panel := m.fm.panels[m.fm.active]
	var names []string
	for _, e := range panel.entries {
		if e.marked {
			names = append(names, e.name)
		}
	}
	if len(names) > 0 {
		return names
	}
	if entry, ok := panel.current(); ok && !entry.parent {
		return []string{entry.name}
	}
	return nil
}

// fmOpenCopyConfirm öffnet nach F5 die Bestätigung.
//
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-21 21:03
func (m AppModel) fmOpenCopyConfirm() (tea.Model, tea.Cmd) {
	source := m.fm.active
	target := m.fm.panels[1-source]
	if m.fm.panels[source].loading || target.loading || target.fs == nil || m.fm.panels[source].fs == nil {
		return m, nil
	}
	names := m.fmSelection()
	if len(names) == 0 {
		m.fm.message = m.tr(txtFMNothingSelected)
		return m, nil
	}
	m.fm.copy = fmCopyState{names: names, source: source}
	m.fm.dialog = fmDialogCopyConfirm
	return m, nil
}

// fmStartCopy startet den Kopiervorgang im Hintergrund.
//
// @return tea.Model - Modell mit Fortschrittsdialog
// @return tea.Cmd - wartet auf die erste Meldung
// @date   2026-10-21 21:03
func (m AppModel) fmStartCopy() (tea.Model, tea.Cmd) {
	src := m.fm.panels[m.fm.copy.source]
	dst := m.fm.panels[1-m.fm.copy.source]
	ctx, cancel := context.WithCancel(context.Background())
	m.fm.copy.cancel = cancel
	m.fm.copy.events = make(chan copyEvent, 64)
	m.fm.copy.answers = make(chan copyConflictAnswer, 1)
	m.fm.copy.copyProgress = copyProgress{scanning: true}
	req := copyRequest{
		src: src.fs, dst: dst.fs,
		srcDir: src.path, dstDir: dst.path,
		names:    m.fm.copy.names,
		sameHost: strings.EqualFold(src.conn.Host, dst.conn.Host) && src.conn.Port == dst.conn.Port,
	}
	go runCopy(ctx, req, m.fm.copy.events, m.fm.copy.answers)
	m.fm.dialog = fmDialogCopying
	return m, m.waitCopyEvent()
}

// waitCopyEvent wartet auf die nächste Meldung des Kopiervorgangs.
//
// @return tea.Cmd - Kommando, das fmCopyEventMsg liefert
// @date   2026-10-21 21:03
func (m AppModel) waitCopyEvent() tea.Cmd {
	events := m.fm.copy.events
	session := m.fm.session
	return func() tea.Msg {
		return fmCopyEventMsg{session: session, event: <-events}
	}
}

// handleFMCopyEvent verarbeitet eine Meldung des Kopiervorgangs.
//
// @param msg - Meldung
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - nächstes Warten bzw. Neuladen des Zielfensters
// @date   2026-10-21 21:03
func (m AppModel) handleFMCopyEvent(msg fmCopyEventMsg) (tea.Model, tea.Cmd) {
	if msg.session != m.fm.session || m.state != ViewFileManager {
		return m, nil
	}
	event := msg.event
	switch {
	case event.progress != nil:
		m.fm.copy.copyProgress = *event.progress
		return m, m.waitCopyEvent()
	case event.conflict != "":
		m.fm.dialog = fmDialogOverwrite
		m.fm.copy.conflictPath = event.conflict
		m.fm.copy.conflictInfo = event.conflictInfo
		return m, nil
	case event.result != nil:
		return m.fmFinishCopy(*event.result)
	}
	return m, m.waitCopyEvent()
}

// fmFinishCopy schließt den Dialog, meldet das Ergebnis, hebt die
// Markierung vollständig kopierter Einträge auf und liest das Zielfenster
// neu ein.
//
// @param result - Ergebnis
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Neuladen des Zielfensters
// @date   2026-10-21 21:03
func (m AppModel) fmFinishCopy(result copyResult) (tea.Model, tea.Cmd) {
	if m.fm.copy.cancel != nil {
		m.fm.copy.cancel()
	}
	m.fm.dialog = fmDialogNone
	switch {
	case result.cancelled:
		m.fm.message = m.tr(txtFMCopyCancelled)
	case len(result.failures) > 0:
		m.fm.message = fmt.Sprintf(m.tr(txtFMCopyErrors), len(result.failures), m.fmDescribeFailure(result.failures[0]))
	default:
		m.fm.message = fmt.Sprintf(m.tr(txtFMCopyDone), result.files, formatSizeWithUnit(result.bytes))
	}

	// Markierung der vollständig kopierten Einträge aufheben
	source := &m.fm.panels[m.fm.copy.source]
	copied := map[string]bool{}
	for _, name := range result.copiedNames {
		copied[name] = true
	}
	for i := range source.entries {
		if copied[source.entries[i].name] {
			source.entries[i].marked = false
		}
	}

	// Zielfenster neu einlesen, Cursor auf dem bisherigen Eintrag lassen
	side := 1 - m.fm.copy.source
	target := &m.fm.panels[side]
	focus := ""
	if entry, ok := target.current(); ok && !entry.parent {
		focus = entry.name
	}
	target.loading = true
	fsys, dir, session := target.fs, target.path, m.fm.session
	return m, func() tea.Msg {
		entries, err := readPanelDir(fsys, dir)
		return fmDirLoadedMsg{session: session, side: side, path: dir, entries: entries, focus: focus, err: err}
	}
}

// fmDescribeFailure formuliert einen Kopierfehler für die Meldung.
//
// @param f - Fehler
// @return string - Text (bereinigt, ohne Steuerzeichen)
// @date   2026-10-21 21:03
func (m AppModel) fmDescribeFailure(f copyFailure) string {
	p := sanitizeFileName(f.path)
	switch {
	case errors.Is(f.err, errTargetTypeMismatch):
		return fmt.Sprintf(m.tr(txtFMErrTargetType), p)
	case errors.Is(f.err, errSameFile):
		return fmt.Sprintf(m.tr(txtFMErrSameFile), p)
	case errors.Is(f.err, errUnsafeName):
		return fmt.Sprintf(m.tr(txtFMErrUnsafeName), p)
	case errors.Is(f.err, errCannotProtect):
		return fmt.Sprintf(m.tr(txtFMErrCannotProtect), p)
	case errors.Is(f.err, errCopyLimit):
		return fmt.Sprintf(m.tr(txtFMErrCopyLimit), p, copyMaxEntries, copyMaxDepth)
	}
	return p + ": " + sanitizeFileName(f.err.Error())
}

// handleFMDialogKeys verarbeitet Tasten, solange ein Dialog offen ist.
// Alle übrigen Tasten (auch F10) ruhen.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando
// @date   2026-10-21 21:03
func (m AppModel) handleFMDialogKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch m.fm.dialog {
	case fmDialogCopyConfirm:
		switch key {
		case "enter":
			return m.fmStartCopy()
		case "esc", "q":
			m.fm.dialog = fmDialogNone
		}
	case fmDialogCopying:
		if key == "esc" && m.fm.copy.cancel != nil {
			// Die Kopie meldet sich mit "abgebrochen" zurück; das wartende
			// Kommando holt diese Meldung ab
			m.fm.copy.cancel()
		}
	case fmDialogOverwrite:
		answers := map[string]copyConflictAnswer{
			"j": copyAnswerOverwrite, "y": copyAnswerOverwrite,
			"n":   copyAnswerSkip,
			"a":   copyAnswerOverwriteAll,
			"k":   copyAnswerSkipAll,
			"u":   copyAnswerOverwriteNewer,
			"g":   copyAnswerOverwriteLarger,
			"esc": copyAnswerAbort,
		}
		answer, ok := answers[key]
		if !ok {
			return m, nil
		}
		m.fm.copy.answers <- answer
		if answer == copyAnswerAbort {
			m.fm.copy.cancel()
		}
		m.fm.dialog = fmDialogCopying
		return m, m.waitCopyEvent()
	}
	return m, nil
}

// --- Darstellung der Dialoge ---

// fmDialogLines liefert Titel, Textzeilen und Tastenhilfe des offenen Dialogs.
//
// @param inner - Innenbreite des Dialogs
// @return string - Titel
// @return []string - Inhalt (je Zeile höchstens inner breit)
// @return string - Tastenhilfe (wird umbrochen)
// @date   2026-10-21 21:03
func (m AppModel) fmDialogLines(inner int) (string, []string, string) {
	c := m.fm.copy
	switch m.fm.dialog {
	case fmDialogCopyConfirm:
		target := m.fm.panels[1-c.source].title()
		text := fmt.Sprintf(m.tr(txtFMCopyConfirmMany), len(c.names), target)
		if len(c.names) == 1 {
			text = fmt.Sprintf(m.tr(txtFMCopyConfirmOne), sanitizeFileName(c.names[0]), target)
		}
		return m.tr(txtFMCopyTitle), wrapText(text, inner), m.tr(txtFMConfirmHelp)
	case fmDialogCopying:
		p := c.copyProgress
		if p.scanning {
			return m.tr(txtFMCopyTitle), []string{fmt.Sprintf(m.tr(txtFMCopyScanning), p.totalFiles)}, m.tr(txtFMCancelHelp)
		}
		done, total := p.doneBytes, p.totalBytes
		if total == 0 {
			done, total = int64(p.doneFiles), int64(p.totalFiles)
		}
		percent := 100
		if total > 0 {
			// Kommazahl statt done*100: kein Überlauf bei absurden Größen
			percent = int(math.Min(math.Max(float64(done)/float64(total), 0), 1) * 100)
		}
		lines := []string{
			fitWidthLeft(sanitizeFileName(p.current), inner),
			fmt.Sprintf(m.tr(txtFMCopyProgress), p.doneFiles, p.totalFiles, formatSizeWithUnit(p.doneBytes), formatSizeWithUnit(p.totalBytes)),
			progressBar(done, total, max(inner-5, 0)) + fmt.Sprintf(" %3d%%", percent),
		}
		return m.tr(txtFMCopyTitle), lines, m.tr(txtFMCancelHelp)
	case fmDialogOverwrite:
		lines := wrapText(sanitizeFileName(c.conflictPath), inner)
		if len(lines) > 2 {
			// Lange Pfade: das Ende ist wichtiger als der Anfang
			lines = []string{fitWidthLeft(sanitizeFileName(c.conflictPath), inner)}
		}
		info := c.conflictInfo
		// Beschriftungen und Größen gleich breit, damit die Werte untereinanderstehen
		srcLabel, dstLabel := m.tr(txtFMSource), m.tr(txtFMTarget)
		labelWidth := max(ansi.StringWidth(srcLabel), ansi.StringWidth(dstLabel))
		srcSize, dstSize := formatSizeWithUnit(info.srcSize), formatSizeWithUnit(info.dstSize)
		sizeWidth := max(len(srcSize), len(dstSize))
		lines = append(lines,
			fitWidth(srcLabel, labelWidth)+" "+fitRight(srcSize, sizeWidth)+"  "+formatFMTime(info.srcTime),
			fitWidth(dstLabel, labelWidth)+" "+fitRight(dstSize, sizeWidth)+"  "+formatFMTime(info.dstTime),
		)
		return m.tr(txtFMOverwriteTitle), lines, m.tr(txtFMOverwriteHelp)
	}
	return "", nil, ""
}

// renderFMDialogBox rendert den offenen Dialog als Kasten.
//
// @param st - Stile des Farbmodus
// @param width - Bildschirmbreite
// @param height - Bildschirmhöhe
// @return []string - Zeilen des Kastens (alle gleich breit)
// @return int - Breite des Kastens
// @date   2026-10-21 21:03
func (m AppModel) renderFMDialogBox(st fmStyles, width, height int) ([]string, int) {
	boxWidth := min(width-4, fmDialogMaxWidth)
	if boxWidth < 12 {
		boxWidth = min(width, 12)
	}
	inner := max(boxWidth-4, 1)
	title, content, help := m.fmDialogLines(inner)

	row := func(text string) string {
		return st.dialog.Render("│ " + fitWidth(text, inner) + " │")
	}
	titleText := fitWidthLeft(" "+title+" ", max(boxWidth-2, 0))
	fill := max(boxWidth-2-ansi.StringWidth(titleText), 0)
	lines := []string{st.dialog.Render("┌"+strings.Repeat("─", fill/2)) + st.dialogTitle.Render(titleText) +
		st.dialog.Render(strings.Repeat("─", fill-fill/2)+"┐")}
	for _, line := range content {
		lines = append(lines, row(line))
	}
	lines = append(lines, row(""))
	for _, line := range wrapHelp(help, inner) {
		lines = append(lines, row(line))
	}
	lines = append(lines, st.dialog.Render("└"+strings.Repeat("─", max(boxWidth-2, 0))+"┘"))

	// Zu hoch für das Terminal: Mitte kürzen, Rahmen behalten
	if len(lines) > height && height >= 2 {
		lines = append(lines[:height-1], lines[len(lines)-1])
	}
	return lines, boxWidth
}

// overlayDialog legt den Dialog mittig über die Bildschirmzeilen.
//
// @param lines - Bildschirmzeilen (werden verändert)
// @param box - Zeilen des Kastens
// @param boxWidth - Breite des Kastens
// @param width - Bildschirmbreite
// @date   2026-10-21 21:03
func overlayDialog(lines, box []string, boxWidth, width int) {
	top := max((len(lines)-len(box))/2, 0)
	left := max((width-boxWidth)/2, 0)
	for i, boxLine := range box {
		y := top + i
		if y >= len(lines) {
			break
		}
		before := ansi.Truncate(lines[y], left, "")
		before += strings.Repeat(" ", max(left-ansi.StringWidth(before), 0))
		after := ansi.TruncateLeft(lines[y], left+boxWidth, "")
		line := before + boxLine + after
		// Breite Zeichen an den Schnittkanten: auf volle Breite auffüllen
		line += strings.Repeat(" ", max(width-ansi.StringWidth(line), 0))
		lines[y] = line
	}
}

// wrapText bricht einen Text an Leerzeichen auf die Breite um; zu lange
// Wörter werden hart getrennt.
//
// @param text - Text
// @param width - Höchstbreite
// @return []string - Zeilen
// @date   2026-10-21 21:03
func wrapText(text string, width int) []string {
	if width <= 0 {
		return nil
	}
	var lines []string
	current := ""
	for _, word := range strings.Fields(text) {
		for ansi.StringWidth(word) > width {
			if current != "" {
				lines = append(lines, current)
				current = ""
			}
			part := ansi.Truncate(word, width, "")
			if part == "" {
				// Einzelnes Zeichen breiter als die Zeile: trotzdem weiterkommen
				_, size := utf8.DecodeRuneInString(word)
				part = word[:size]
			}
			lines = append(lines, part)
			word = word[len(part):]
		}
		switch {
		case current == "":
			current = word
		case ansi.StringWidth(current)+1+ansi.StringWidth(word) <= width:
			current += " " + word
		default:
			lines = append(lines, current)
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// wrapHelp verteilt eine Tastenhilfe ("a: x  b: y  …", Einträge durch zwei
// Leerzeichen getrennt) auf Zeilen, ohne Einträge zu zerreißen.
//
// @param help - Tastenhilfe
// @param width - Höchstbreite
// @return []string - Zeilen
// @date   2026-10-21 21:03
func wrapHelp(help string, width int) []string {
	var lines []string
	current := ""
	for _, item := range strings.Split(strings.TrimSpace(help), "  ") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		switch {
		case current == "":
			current = item
		case ansi.StringWidth(current)+2+ansi.StringWidth(item) <= width:
			current += "  " + item
		default:
			lines = append(lines, current)
			current = item
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// progressBar zeichnet einen Fortschrittsbalken aus █ und ░.
//
// @param done - Erledigt
// @param total - Gesamt (0 = fertig)
// @param width - Breite in Zeichen
// @return string - Balken
// @date   2026-10-21 21:03
func progressBar(done, total int64, width int) string {
	if width <= 0 {
		return ""
	}
	filled := width
	if total > 0 {
		// Kommazahl statt done*width: kein Überlauf, Ergebnis immer in [0, width]
		ratio := math.Min(math.Max(float64(done)/float64(total), 0), 1)
		filled = min(max(int(ratio*float64(width)), 0), width)
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}
