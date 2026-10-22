// Paket main - Datei-Manager (MC-Modus): Verschieben (F6), Verzeichnis
// anlegen (F7) und Löschen (F8)
//
//   - F6 nutzt die Dialoge des Kopierens (tui_filemanager_copy.go) mit
//     fmCopyState.move: Bestätigung (Enter), Fortschritt, Rückfrage bei
//     vorhandenem Ziel. Danach werden beide Fenster neu gelesen.
//   - F7 fragt einen Namen ab (Enter = anlegen, Esc = abbrechen; "q" ist
//     hier ein normales Zeichen). Erlaubt ist genau ein Name ohne "/",
//     "\" und Steuerzeichen. Danach steht der Cursor auf dem neuen
//     Verzeichnis.
//   - F8 verlangt eine bewusste Bestätigung mit j/y - Enter löscht nicht,
//     damit ein versehentliches F8-Enter nichts zerstört. Verzeichnisse
//     werden samt Inhalt gelöscht (der Dialog weist darauf hin), Symlinks
//     nie verfolgt. Das Löschen läuft als Goroutine (runDelete,
//     filemanager_delete.go); Esc bricht ab.
//
// Nach F6/F8 bleiben Einträge, die nicht vollständig bearbeitet wurden,
// markiert; der Cursor bleibt an seiner Stelle.
//
// @author Kurt Ingwer
// @date   2026-10-22 05:17
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// fmMkdirMaxLength - Höchstlänge eines Verzeichnisnamens (Zeichen)
const fmMkdirMaxLength = 255

// fmDeleteState ist der Zustand eines Löschvorgangs in der Oberfläche.
type fmDeleteState struct {
	// Zu löschende Namen und Seite
	names []string
	side  int
	// Ist ein echtes Verzeichnis dabei? (Hinweis "samt Inhalt")
	hasDir bool
	// Letzter gemeldeter Fortschritt
	progress deleteProgress
	// Abbruch und Ereigniskanal des laufenden Löschens
	cancel context.CancelFunc
	events chan deleteEvent
}

// fmDeleteEventMsg überbringt eine Meldung des Löschvorgangs.
type fmDeleteEventMsg struct {
	session int
	event   deleteEvent
}

// fmMkdirDoneMsg meldet das Ergebnis von F7.
type fmMkdirDoneMsg struct {
	session int
	side    int
	// Name und vollständiger Pfad des neuen Verzeichnisses
	name, path string
	err        error
}

// --- gemeinsame Hilfen ---

// fmReloadPanel liest ein Fenster neu ein. Der Cursor geht auf focus;
// fehlt dieser Eintrag inzwischen, auf die Position fallback (begrenzt).
// Namen in marks werden danach wieder markiert.
//
// @param side - Seite (0 links, 1 rechts)
// @param focus - Name für den Cursor ("" = Anfang)
// @param fallback - Cursorposition, falls focus fehlt
// @param marks - wieder zu markierende Namen (nil = keine)
// @return tea.Cmd - Hintergrundkommando
// @date   2026-10-22 05:17
func (m *AppModel) fmReloadPanel(side int, focus string, fallback int, marks map[string]bool) tea.Cmd {
	panel := &m.fm.panels[side]
	panel.loading = true
	fsys, dir, session := panel.fs, panel.path, m.fm.session
	return func() tea.Msg {
		entries, err := readPanelDir(fsys, dir)
		return fmDirLoadedMsg{session: session, side: side, path: dir, entries: entries, focus: focus, fallback: fallback, marks: marks, err: err}
	}
}

// fmPanelFocus liefert Name und Position des Cursors eines Fensters (für
// fmReloadPanel); auf ".." ist der Name leer.
//
// @param side - Seite
// @return string - Name unter dem Cursor
// @return int - Cursorposition
// @date   2026-10-22 05:17
func (m AppModel) fmPanelFocus(side int) (string, int) {
	panel := m.fm.panels[side]
	if entry, ok := panel.current(); ok && !entry.parent {
		return entry.name, panel.cursor
	}
	return "", panel.cursor
}

// fmRemainingMarks liefert die markierten Namen eines Fensters ohne die
// erledigten (die gibt es nach dem Neueinlesen nicht mehr bzw. sollen
// nicht markiert bleiben).
//
// @param side - Seite
// @param done - vollständig bearbeitete Namen
// @return map[string]bool - weiter markierte Namen
// @date   2026-10-22 05:17
func (m AppModel) fmRemainingMarks(side int, done []string) map[string]bool {
	finished := map[string]bool{}
	for _, name := range done {
		finished[name] = true
	}
	marks := map[string]bool{}
	for _, e := range m.fm.panels[side].entries {
		if e.marked && !finished[e.name] {
			marks[e.name] = true
		}
	}
	return marks
}

// --- F7: Verzeichnis anlegen ---

// fmInDriveList meldet, ob ein Fenster die Windows-Laufwerksliste zeigt
// (lokaler PC, "/"): dort sind Dateifunktionen gesperrt.
//
// @param side - Seite
// @return bool - true in der Laufwerksliste
// @date   2026-10-22 06:40
func (m AppModel) fmInDriveList(side int) bool {
	panel := m.fm.panels[side]
	return panel.fs != nil && panelIsDriveList(panel.fs, panel.path)
}

// fmOpenMkdir öffnet nach F7 die Namensabfrage.
//
// @return tea.Model - Modell mit Dialog
// @return tea.Cmd - Cursor-Blinken
// @date   2026-10-22 06:40
func (m AppModel) fmOpenMkdir() (tea.Model, tea.Cmd) {
	panel := m.fm.panels[m.fm.active]
	if panel.loading || panel.fs == nil {
		return m, nil
	}
	if m.fmInDriveList(m.fm.active) {
		m.fm.message = m.tr(txtFMDriveList)
		return m, nil
	}
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = fmMkdirMaxLength
	input.Focus()
	m.fm.mkdirInput = input
	m.fm.dialog = fmDialogMkdir
	return m, textinput.Blink
}

// handleFMMkdirKeys verarbeitet Tasten in der Namensabfrage.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando
// @date   2026-10-22 05:17
func (m AppModel) handleFMMkdirKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.fm.dialog = fmDialogNone
		m.fm.message = ""
		return m, nil
	case tea.KeyEnter:
		name := m.fm.mkdirInput.Value()
		if name == "" {
			return m, nil
		}
		if !safeCopyName(name) || !fsAcceptsName(m.fm.panels[m.fm.active].fs, name) {
			// Dialog bleibt offen, damit der Name korrigiert werden kann
			m.fm.message = m.tr(txtFMMkdirBadName)
			return m, nil
		}
		return m.fmStartMkdir(name)
	}
	m.fm.message = ""
	var cmd tea.Cmd
	m.fm.mkdirInput, cmd = m.fm.mkdirInput.Update(msg)
	return m, cmd
}

// fmStartMkdir legt das Verzeichnis im Hintergrund an. Ein vorhandener
// Eintrag gleichen Namens wird vorher erkannt (SFTP-Server melden das
// sonst nur als allgemeinen Fehler).
//
// @param name - geprüfter Name
// @return tea.Model - Modell ohne Dialog
// @return tea.Cmd - Hintergrundkommando
// @date   2026-10-22 05:17
func (m AppModel) fmStartMkdir(name string) (tea.Model, tea.Cmd) {
	m.fm.dialog = fmDialogNone
	m.fm.message = ""
	side := m.fm.active
	panel := &m.fm.panels[side]
	panel.loading = true
	fsys, session := panel.fs, m.fm.session
	p := path.Join(panel.path, name)
	return m, func() tea.Msg {
		result := fmMkdirDoneMsg{session: session, side: side, name: name, path: p}
		if _, err := fsys.Lstat(p); err == nil {
			result.err = os.ErrExist
			return result
		}
		result.err = fsys.Mkdir(p)
		return result
	}
}

// handleFMMkdirDone meldet das Ergebnis und liest das Fenster neu ein
// (Cursor auf dem neuen Verzeichnis).
//
// @param msg - Ergebnis
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Neuladen des Fensters
// @date   2026-10-22 05:17
func (m AppModel) handleFMMkdirDone(msg fmMkdirDoneMsg) (tea.Model, tea.Cmd) {
	if msg.session != m.fm.session || m.state != ViewFileManager {
		return m, nil
	}
	m.fm.panels[msg.side].loading = false
	switch {
	case errors.Is(msg.err, os.ErrExist):
		m.fm.message = fmt.Sprintf(m.tr(txtFMMkdirExists), sanitizeFileName(msg.name))
		return m, nil
	case msg.err != nil:
		m.fm.message = fmt.Sprintf(m.tr(txtFMErrMkdir), sanitizeFileName(msg.path), sanitizeFileName(msg.err.Error()))
		return m, nil
	}
	m.fm.message = fmt.Sprintf(m.tr(txtFMMkdirDone), sanitizeFileName(msg.name))
	marks := m.fmRemainingMarks(msg.side, nil)
	return m, m.fmReloadPanel(msg.side, msg.name, m.fm.panels[msg.side].cursor, marks)
}

// --- F8: Löschen ---

// fmOpenDeleteConfirm öffnet nach F8 die Bestätigung.
//
// @return tea.Model - Modell mit Dialog
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-22 05:17
func (m AppModel) fmOpenDeleteConfirm() (tea.Model, tea.Cmd) {
	side := m.fm.active
	panel := m.fm.panels[side]
	if panel.loading || panel.fs == nil {
		return m, nil
	}
	if m.fmInDriveList(side) {
		// F8 auf "C:" würde ein ganzes Laufwerk leeren
		m.fm.message = m.tr(txtFMDriveList)
		return m, nil
	}
	names := m.fmSelection()
	if len(names) == 0 {
		m.fm.message = m.tr(txtFMNothingSelected)
		return m, nil
	}
	m.fm.del = fmDeleteState{names: names, side: side, hasDir: deleteHasDirectory(panel.entries, names)}
	m.fm.dialog = fmDialogDeleteConfirm
	return m, nil
}

// fmStartDelete startet das Löschen im Hintergrund.
//
// @return tea.Model - Modell mit Fortschrittsdialog
// @return tea.Cmd - wartet auf die erste Meldung
// @date   2026-10-22 05:17
func (m AppModel) fmStartDelete() (tea.Model, tea.Cmd) {
	panel := m.fm.panels[m.fm.del.side]
	ctx, cancel := context.WithCancel(context.Background())
	m.fm.del.cancel = cancel
	m.fm.del.events = make(chan deleteEvent, 64)
	m.fm.del.progress = deleteProgress{}
	go runDelete(ctx, deleteRequest{fs: panel.fs, dir: panel.path, names: m.fm.del.names}, m.fm.del.events)
	m.fm.dialog = fmDialogDeleting
	return m, m.waitDeleteEvent()
}

// waitDeleteEvent wartet auf die nächste Meldung des Löschvorgangs.
//
// @return tea.Cmd - Kommando, das fmDeleteEventMsg liefert
// @date   2026-10-22 05:17
func (m AppModel) waitDeleteEvent() tea.Cmd {
	events, session := m.fm.del.events, m.fm.session
	return func() tea.Msg {
		return fmDeleteEventMsg{session: session, event: <-events}
	}
}

// handleFMDeleteEvent verarbeitet eine Meldung des Löschvorgangs.
//
// @param msg - Meldung
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - nächstes Warten bzw. Neuladen des Fensters
// @date   2026-10-22 05:17
func (m AppModel) handleFMDeleteEvent(msg fmDeleteEventMsg) (tea.Model, tea.Cmd) {
	if msg.session != m.fm.session || m.state != ViewFileManager {
		return m, nil
	}
	if msg.event.result != nil {
		return m.fmFinishDelete(*msg.event.result)
	}
	if msg.event.progress != nil {
		m.fm.del.progress = *msg.event.progress
	}
	return m, m.waitDeleteEvent()
}

// fmFinishDelete schließt den Dialog, meldet das Ergebnis und liest das
// Fenster neu ein; nicht gelöschte Einträge bleiben markiert.
//
// @param result - Ergebnis
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Neuladen des Fensters
// @date   2026-10-22 05:17
func (m AppModel) fmFinishDelete(result deleteResult) (tea.Model, tea.Cmd) {
	if m.fm.del.cancel != nil {
		m.fm.del.cancel()
	}
	m.fm.dialog = fmDialogNone
	switch {
	case result.cancelled:
		m.fm.message = m.tr(txtFMDeleteCancelled)
	case len(result.failures) > 0:
		m.fm.message = fmt.Sprintf(m.tr(txtFMDeleteErrors), len(result.failures), m.fmDescribeFailure(result.failures[0]))
	default:
		m.fm.message = fmt.Sprintf(m.tr(txtFMDeleteDone), result.deleted)
	}
	side := m.fm.del.side
	focus, cursor := m.fmPanelFocus(side)
	marks := m.fmRemainingMarks(side, result.deletedNames)
	return m, m.fmReloadPanel(side, focus, cursor, marks)
}

// handleFMDeleteKeys verarbeitet Tasten in Bestätigung und Fortschritt
// des Löschens. Bestätigt wird nur mit j/y (nicht mit Enter).
//
// @param key - Taste
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando
// @date   2026-10-22 05:17
func (m AppModel) handleFMDeleteKeys(key string) (tea.Model, tea.Cmd) {
	switch m.fm.dialog {
	case fmDialogDeleteConfirm:
		switch key {
		case "j", "y":
			return m.fmStartDelete()
		case "n", "esc", "q":
			m.fm.dialog = fmDialogNone
		}
	case fmDialogDeleting:
		if key == "esc" && m.fm.del.cancel != nil {
			// runDelete meldet sich mit "abgebrochen" zurück
			m.fm.del.cancel()
		}
	}
	return m, nil
}

// --- Darstellung ---

// fmOpsDialogLines liefert Titel, Inhalt und Tastenhilfe der Dialoge von
// F7 und F8.
//
// @param inner - Innenbreite des Dialogs
// @return string - Titel
// @return []string - Inhalt
// @return string - Tastenhilfe
// @date   2026-10-22 05:17
func (m AppModel) fmOpsDialogLines(inner int) (string, []string, string) {
	switch m.fm.dialog {
	case fmDialogMkdir:
		input := m.fm.mkdirInput
		input.Width = max(inner-1, 1)
		lines := append(wrapText(m.tr(txtFMMkdirPrompt), inner), input.View())
		return m.tr(txtFMMkdirTitle), lines, m.tr(txtFMMkdirHelp)
	case fmDialogDeleteConfirm:
		d := m.fm.del
		text := fmt.Sprintf(m.tr(txtFMDeleteConfirmMany), len(d.names))
		if len(d.names) == 1 {
			text = fmt.Sprintf(m.tr(txtFMDeleteConfirmOne), sanitizeFileName(d.names[0]))
		}
		lines := wrapText(text, inner)
		if d.hasDir {
			lines = append(lines, wrapText(m.tr(txtFMDeleteDirsNote), inner)...)
		}
		return m.tr(txtFMDeleteTitle), lines, m.tr(txtFMDeleteConfirmHelp)
	case fmDialogDeleting:
		p := m.fm.del.progress
		lines := []string{
			fitWidthLeft(sanitizeFileName(p.current), inner),
			fmt.Sprintf(m.tr(txtFMDeleteProgress), p.done),
		}
		return m.tr(txtFMDeleteTitle), lines, m.tr(txtFMCancelHelp)
	}
	return "", nil, ""
}
