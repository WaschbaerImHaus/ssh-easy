// Paket main - Datei-Manager (MC-Modus): Editor (F4) wie mcedit
//
// F4 auf einer Datei öffnet den eingebauten Editor. Unten steht die
// F-Tasten-Leiste wie im mcedit:
//
//	F1 Hilfe, F2 Speichern, F3 Block markieren (Anfang/Ende, dritter Druck
//	hebt auf), F4 Ersetzen (mit Rückfrage je Treffer), F5 Block kopieren,
//	F6 Block verschieben, F7 Suchen (erneut F7 + Enter: weitersuchen),
//	F8 Block löschen, F10/Esc Beenden (bei Änderungen mit Rückfrage);
//	dazu Strg+Y Zeile löschen.
//
// Textdateien bearbeitet der textBuffer (filemanager_editbuf.go) - Zeilen-
// enden und ein fehlender Umbruch am Ende bleiben erhalten. Binärdateien und
// Dateien mit ungültigem UTF-8 öffnen sich als Hex-Editor: Hex-Ziffern
// überschreiben Bytes (z. B. AD -> AF), Tab wechselt in die Zeichenspalte;
// die Größe bleibt gleich.
//
// Speichern: Wurde die Datei inzwischen auf dem Server geändert (Größe oder
// Zeit), wird nachgefragt. Geschrieben wird immer über eine Temp-Datei, die
// am Ende umbenannt wird (writeFileAtomically); bei einem Symlink wird die
// Zieldatei gespeichert, der Link bleibt.
//
// Angezeigter Inhalt ist bereinigt (Steuerzeichen als "?"), die Datei
// selbst bleibt Byte für Byte unverändert, solange man sie nicht ändert.
//
// @author Kurt Ingwer
// @date   2026-10-22 00:52
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// fmEditMaxSymlinks - so vielen Symlinks folgt das Speichern höchstens
const fmEditMaxSymlinks = 8

// errSymlinkLoop - zu viele verkettete Symlinks
var errSymlinkLoop = errors.New("too many levels of symbolic links")

// errNotRegularFile - Ziel ist keine normale Datei (z. B. Gerät, Pipe)
var errNotRegularFile = errors.New("not a regular file")

// edPrompt ist die gerade offene Eingabezeile des Editors.
type edPrompt int

const (
	// edPromptNone - keine Eingabe offen
	edPromptNone edPrompt = iota
	// edPromptSearch - Suchen (F7)
	edPromptSearch
	// edPromptReplaceFind - Ersetzen, Schritt 1: Suchbegriff (F4)
	edPromptReplaceFind
	// edPromptReplaceWith - Ersetzen, Schritt 2: Ersatz
	edPromptReplaceWith
)

// edDialog ist der gerade offene Dialog des Editors.
type edDialog int

const (
	// edDialogNone - kein Dialog
	edDialogNone edDialog = iota
	// edDialogSaveQuestion - "Änderungen speichern?" beim Beenden
	edDialogSaveQuestion
	// edDialogConflict - "Datei wurde auf dem Server geändert"
	edDialogConflict
	// edDialogReplace - "Diesen Treffer ersetzen?"
	edDialogReplace
)

// fmEditor ist der Zustand des Editors.
type fmEditor struct {
	// Anzeige (user@host:/pfad), bereinigter und originaler Dateiname
	title, name, rawName string
	// Geöffneter Pfad und tatsächlich gespeicherter Pfad (Symlink-Ziel)
	path, savePath string
	fs             remoteFS
	loading        bool
	// Hex-Editor (Binärdatei) bzw. Textpuffer
	hexMode bool
	text    *textBuffer
	hexb    *hexBuffer
	// Textmodus: Cursor, erste sichtbare Zeile, waagerechte Verschiebung
	cur       textPos
	top, left int
	// Hexmodus: erste sichtbare Zeile, Zeichenspalte aktiv?
	hexTop   int
	asciiCol bool
	// Geändert seit dem letzten Speichern?
	modified bool
	// Stand auf dem Server beim Laden/Speichern (für die Konfliktprüfung)
	perm     os.FileMode
	origSize int64
	origMod  time.Time
	// Block: Markieren läuft (Anker -> Cursor) bzw. fertiger Block
	markActive, blockSet         bool
	anchor, blockStart, blockEnd textPos
	// Eingabezeile; inputFresh = Vorbelegung noch unberührt
	prompt     edPrompt
	input      textinput.Model
	inputFresh bool
	// Letzter Suchbegriff und Ersatz
	query, replaceWith string
	// Dialog und Zähler für Ersetzen
	dialog       edDialog
	replaceCount int
	// Meldung (bis zum nächsten Tastendruck), Hilfe sichtbar?
	message  string
	showHelp bool
	// Speichern läuft; danach schließen?; wurde gespeichert (Fenster neu lesen)
	saving, quitAfterSave, savedOnce bool
}

// fmEditLoadedMsg überbringt die zum Bearbeiten geladene Datei.
type fmEditLoadedMsg struct {
	session, seq int
	data         []byte
	savePath     string
	perm         os.FileMode
	size         int64
	mod          time.Time
	tooLarge     bool
	err          error
}

// fmEditSavedMsg meldet das Ergebnis des Speicherns.
type fmEditSavedMsg struct {
	session, seq int
	conflict     bool
	size         int64
	mod          time.Time
	err          error
}

// fmOpenEditor öffnet den Editor für den Eintrag unter dem Cursor
// (Verzeichnisse und ".": nichts).
//
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Laden im Hintergrund
// @date   2026-10-22 00:52
func (m AppModel) fmOpenEditor() (tea.Model, tea.Cmd) {
	panel := m.fm.panels[m.fm.active]
	entry, ok := panel.current()
	if !ok || panel.fs == nil || panel.loading || entry.dir {
		return m, nil
	}
	filePath := path.Join(panel.path, entry.name)
	m.fm.editSeq++
	m.fm.editing = true
	m.fm.edit = fmEditor{
		title:   panel.location(filePath),
		name:    sanitizeFileName(entry.name),
		rawName: entry.name,
		path:    filePath,
		fs:      panel.fs,
		loading: true,
	}
	fsys, session, seq := panel.fs, m.fm.session, m.fm.editSeq
	return m, func() tea.Msg {
		return loadEditData(fsys, filePath, session, seq)
	}
}

// resolveEditTarget folgt Symlinks bis zur eigentlichen Datei.
//
// @param fsys - Dateisystem
// @param p - Pfad
// @return string - Pfad der eigentlichen Datei
// @return os.FileInfo - deren Infos (Lstat)
// @return error - Fehler, Schleife oder keine normale Datei
// @date   2026-10-22 00:52
func resolveEditTarget(fsys remoteFS, p string) (string, os.FileInfo, error) {
	for i := 0; i <= fmEditMaxSymlinks; i++ {
		info, err := fsys.Lstat(p)
		if err != nil {
			return "", nil, err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			if !info.Mode().IsRegular() {
				return "", nil, errNotRegularFile
			}
			return p, info, nil
		}
		target, err := fsys.ReadLink(p)
		if err != nil {
			return "", nil, err
		}
		if !path.IsAbs(target) {
			target = path.Join(path.Dir(p), target)
		}
		p = path.Clean(target)
	}
	return "", nil, errSymlinkLoop
}

// loadEditData lädt eine Datei zum Bearbeiten (höchstens fmViewMaxBytes).
//
// @date   2026-10-22 00:52
func loadEditData(fsys remoteFS, filePath string, session, seq int) fmEditLoadedMsg {
	msg := fmEditLoadedMsg{session: session, seq: seq}
	target, info, err := resolveEditTarget(fsys, filePath)
	if err != nil {
		msg.err = err
		return msg
	}
	if info.Size() > fmViewMaxBytes {
		msg.tooLarge = true
		return msg
	}
	reader, err := fsys.OpenRead(target)
	if err != nil {
		msg.err = err
		return msg
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, fmViewMaxBytes+1))
	if err != nil {
		msg.err = err
		return msg
	}
	if int64(len(data)) > fmViewMaxBytes {
		msg.tooLarge = true
		return msg
	}
	msg.data, msg.savePath = data, target
	msg.perm, msg.size, msg.mod = info.Mode().Perm(), info.Size(), info.ModTime()
	return msg
}

// handleFMEditLoaded übernimmt die geladene Datei.
//
// @date   2026-10-22 00:52
func (m AppModel) handleFMEditLoaded(msg fmEditLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.session != m.fm.session || msg.seq != m.fm.editSeq || !m.fm.editing {
		return m, nil
	}
	e := &m.fm.edit
	if msg.tooLarge || msg.err != nil {
		name := e.name
		m.fm.editing = false
		m.fm.edit = fmEditor{}
		if msg.tooLarge {
			m.fm.message = fmt.Sprintf(m.tr(txtFMEditTooLarge), name, formatSizeWithUnit(fmViewMaxBytes))
		} else {
			m.fm.message = fmt.Sprintf(m.tr(txtFMErrEdit), name, sanitizeFileName(msg.err.Error()))
		}
		return m, nil
	}
	e.loading = false
	e.savePath, e.perm, e.origSize, e.origMod = msg.savePath, msg.perm, msg.size, msg.mod
	// Binärdaten und ungültiges UTF-8 byte-genau als Hex bearbeiten
	if looksBinary(msg.data) || !utf8.Valid(msg.data) {
		e.hexMode = true
		e.hexb = newHexBuffer(msg.data)
	} else {
		e.text = newTextBuffer(msg.data)
	}
	return m, nil
}

// closeFMEditor schließt den Editor; wurde gespeichert, wird das Fenster
// neu eingelesen (Größe/Zeit aktuell).
//
// @date   2026-10-22 00:52
func (m AppModel) closeFMEditor() (tea.Model, tea.Cmd) {
	saved := m.fm.edit.savedOnce
	m.fm.editing = false
	m.fm.edit = fmEditor{}
	if !saved {
		return m, nil
	}
	side := m.fm.active
	panel := &m.fm.panels[side]
	focus := ""
	if entry, ok := panel.current(); ok && !entry.parent {
		focus = entry.name
	}
	panel.loading = true
	fsys, dir, session := panel.fs, panel.path, m.fm.session
	return m, func() tea.Msg {
		entries, err := readPanelDir(fsys, dir)
		return fmDirLoadedMsg{session: session, side: side, path: dir, entries: entries, focus: focus, err: err}
	}
}

// fmEditorRows liefert die Zahl der Inhaltszeilen.
//
// @date   2026-10-22 00:52
func (m AppModel) fmEditorRows() int {
	_, height := m.fmScreenSize()
	return max(height-2, 1)
}

// handleFMEditorKeys verarbeitet Tasten im Editor.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Folgekommando (Speichern, Cursor-Blinken)
// @date   2026-10-22 00:52
func (m AppModel) handleFMEditorKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := &m.fm.edit
	key := msg.String()
	if e.loading {
		if key == "esc" || key == "f10" {
			return m.closeFMEditor()
		}
		return m, nil
	}
	if e.showHelp {
		e.showHelp = false
		return m, nil
	}
	if e.dialog != edDialogNone {
		return m.handleFMEditorDialog(msg)
	}
	if e.prompt != edPromptNone {
		return m.handleFMEditorPrompt(msg)
	}
	if e.saving {
		return m, nil
	}
	e.message = ""
	switch key {
	case "f1":
		e.showHelp = true
		return m, nil
	case "f2":
		return m.fmEditSave(false)
	case "f10", "esc":
		if e.modified {
			e.dialog = edDialogSaveQuestion
			return m, nil
		}
		return m.closeFMEditor()
	case "f7":
		return m, e.openPrompt(edPromptSearch, e.query)
	case "shift+f7", "f17":
		e.searchNext(m.tr)
		return m, nil
	case "f3", "f4", "f5", "f6", "f8":
		if e.hexMode {
			e.message = m.tr(txtFMEditNotInHex)
			return m, nil
		}
	}
	width, _ := m.fmScreenSize()
	rows := m.fmEditorRows()
	if e.hexMode {
		e.handleHexKey(msg, width, rows)
		return m, nil
	}
	switch key {
	case "f3":
		e.toggleMark()
	case "f4":
		return m, e.openPrompt(edPromptReplaceFind, e.query)
	case "f5", "f6", "f8":
		if !e.blockSet {
			e.message = m.tr(txtFMEditNoBlock)
			return m, nil
		}
		switch key {
		case "f5":
			e.copyBlock()
		case "f6":
			e.moveBlock()
		case "f8":
			e.cur = e.text.deleteBetween(e.blockStart, e.blockEnd)
			e.blockSet = false
			e.modified = true
		}
	default:
		e.handleTextKey(msg, rows)
	}
	e.ensureVisible(width, rows)
	return m, nil
}

// handleTextKey verarbeitet Bewegungs- und Eingabetasten im Textmodus.
//
// @date   2026-10-22 00:52
func (e *fmEditor) handleTextKey(msg tea.KeyMsg, rows int) {
	b := e.text
	switch msg.String() {
	case "up":
		e.cur = b.clamp(textPos{e.cur.row - 1, e.cur.col})
	case "down":
		e.cur = b.clamp(textPos{e.cur.row + 1, e.cur.col})
	case "left":
		if e.cur.col > 0 {
			e.cur.col--
		} else if e.cur.row > 0 {
			e.cur = textPos{e.cur.row - 1, len(b.lines[e.cur.row-1])}
		}
	case "right":
		if e.cur.col < len(b.lines[e.cur.row]) {
			e.cur.col++
		} else if e.cur.row+1 < len(b.lines) {
			e.cur = textPos{e.cur.row + 1, 0}
		}
	case "home":
		e.cur.col = 0
	case "end":
		e.cur.col = len(b.lines[e.cur.row])
	case "pgup":
		e.cur = b.clamp(textPos{e.cur.row - rows, e.cur.col})
	case "pgdown":
		e.cur = b.clamp(textPos{e.cur.row + rows, e.cur.col})
	case "ctrl+home":
		e.cur = textPos{}
	case "ctrl+end":
		e.cur = b.clamp(textPos{len(b.lines) - 1, 1 << 30})
	case "enter":
		e.edit(func() { e.cur = b.insert(e.cur, []rune{'\n'}) })
	case "backspace":
		e.edit(func() { e.cur = b.backspace(e.cur) })
	case "delete":
		e.edit(func() { b.deleteForward(e.cur) })
	case "tab":
		e.edit(func() { e.cur = b.insert(e.cur, []rune{'\t'}) })
	case "ctrl+y":
		e.edit(func() {
			b.deleteLine(e.cur.row)
			e.cur = b.clamp(textPos{e.cur.row, 0})
		})
	case " ":
		e.edit(func() { e.cur = b.insert(e.cur, []rune{' '}) })
	default:
		if msg.Type != tea.KeyRunes || isNulKey(msg) {
			return
		}
		text := msg.Runes
		if msg.Paste {
			text = []rune(strings.ReplaceAll(strings.ReplaceAll(string(text), "\r\n", "\n"), "\r", "\n"))
		}
		e.edit(func() { e.cur = b.insert(e.cur, text) })
	}
}

// edit führt eine Änderung aus: geändert merken, Block aufheben (seine
// Positionen stimmen danach nicht mehr).
//
// @date   2026-10-22 00:52
func (e *fmEditor) edit(change func()) {
	change()
	e.modified = true
	e.markActive, e.blockSet = false, false
}

// toggleMark: erster F3 setzt den Anfang, zweiter das Ende, dritter hebt auf.
//
// @date   2026-10-22 00:52
func (e *fmEditor) toggleMark() {
	switch {
	case e.markActive:
		e.markActive = false
		e.blockStart, e.blockEnd = orderPos(e.anchor, e.cur)
		e.blockSet = e.blockStart != e.blockEnd
	case e.blockSet:
		e.blockSet = false
	default:
		e.anchor = e.cur
		e.markActive = true
	}
}

// copyBlock fügt den Block an der Cursorposition ein (F5); der ursprüngliche
// Block bleibt markiert.
//
// @date   2026-10-22 00:52
func (e *fmEditor) copyBlock() {
	text := e.text.textBetween(e.blockStart, e.blockEnd)
	insertAt := e.cur
	end := e.text.insert(insertAt, text)
	// Liegt die Einfügestelle vor dem Block, verschiebt sich der Block
	if insertAt.before(e.blockStart) || insertAt == e.blockStart {
		e.blockStart = shiftPos(e.blockStart, insertAt, end)
		e.blockEnd = shiftPos(e.blockEnd, insertAt, end)
	}
	e.modified = true
}

// moveBlock verschiebt den Block an die Cursorposition (F6) und markiert ihn
// dort. Liegt der Cursor im Block, passiert nichts.
//
// @date   2026-10-22 00:52
func (e *fmEditor) moveBlock() {
	start, end := e.blockStart, e.blockEnd
	target := e.cur
	if start.before(target) && target.before(end) {
		return
	}
	text := e.text.textBetween(start, end)
	if !target.before(start) {
		// Ziel hinter dem Block: erst löschen, dann Ziel umrechnen
		e.text.deleteBetween(start, end)
		if target.row == end.row {
			target = textPos{start.row, start.col + target.col - end.col}
		} else {
			target.row -= end.row - start.row
		}
	} else {
		e.text.deleteBetween(start, end)
	}
	newEnd := e.text.insert(target, text)
	e.blockStart, e.blockEnd, e.blockSet = target, newEnd, true
	e.cur = target
	e.modified = true
}

// shiftPos verschiebt eine Position, wenn an from Text bis to eingefügt wurde.
//
// @date   2026-10-22 00:52
func shiftPos(p, from, to textPos) textPos {
	if p.row == from.row {
		return textPos{to.row, to.col + p.col - from.col}
	}
	return textPos{p.row + to.row - from.row, p.col}
}

// ensureVisible hält den Cursor im sichtbaren Bereich.
//
// @date   2026-10-22 00:52
func (e *fmEditor) ensureVisible(width, rows int) {
	if e.hexMode {
		perRow := hexBytesPerRow(width)
		row := e.hexb.cursor / perRow
		if row < e.hexTop {
			e.hexTop = row
		}
		if row >= e.hexTop+rows {
			e.hexTop = row - rows + 1
		}
		return
	}
	if e.cur.row < e.top {
		e.top = e.cur.row
	}
	if e.cur.row >= e.top+rows {
		e.top = e.cur.row - rows + 1
	}
	column := displayColumn(e.text.lines[e.cur.row], e.cur.col)
	if column < e.left {
		e.left = column
	}
	if column >= e.left+width {
		e.left = column - width + 1
	}
}

// handleHexKey verarbeitet Tasten im Hex-Editor.
//
// @date   2026-10-22 00:52
func (e *fmEditor) handleHexKey(msg tea.KeyMsg, width, rows int) {
	h := e.hexb
	perRow := hexBytesPerRow(width)
	switch msg.String() {
	case "tab":
		e.asciiCol = !e.asciiCol
		h.low = false
	case "left":
		h.move(-1, perRow)
	case "right":
		h.move(1, perRow)
	case "up":
		h.move(-perRow, perRow)
	case "down":
		h.move(perRow, perRow)
	case "pgup":
		h.move(-perRow*rows, perRow)
	case "pgdown":
		h.move(perRow*rows, perRow)
	case "home":
		h.move(-(h.cursor % perRow), perRow)
	case "end":
		h.move(perRow-1-h.cursor%perRow, perRow)
	case "ctrl+home":
		h.move(-len(h.data), perRow)
	case "ctrl+end":
		h.move(len(h.data), perRow)
	case " ":
		if e.asciiCol && h.typeASCII(' ') {
			e.modified = h.modified()
		}
	default:
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && !isNulKey(msg) {
			accepted := false
			if e.asciiCol {
				accepted = h.typeASCII(msg.Runes[0])
			} else {
				accepted = h.typeHex(msg.Runes[0])
			}
			if accepted {
				e.modified = h.modified()
			}
		}
	}
	e.ensureVisible(width, rows)
}

// openPrompt öffnet eine Eingabezeile, vorbelegt mit value (das erste
// getippte Zeichen ersetzt die Vorbelegung, wie im mc).
//
// @date   2026-10-22 00:52
func (e *fmEditor) openPrompt(kind edPrompt, value string) tea.Cmd {
	e.prompt = kind
	e.input = textinput.New()
	e.input.Prompt = ""
	e.input.CharLimit = 256
	e.input.SetValue(value)
	e.inputFresh = value != ""
	e.input.Focus()
	return textinput.Blink
}

// handleFMEditorPrompt verarbeitet Tasten in der Eingabezeile.
//
// @date   2026-10-22 00:52
func (m AppModel) handleFMEditorPrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := &m.fm.edit
	switch msg.String() {
	case "esc":
		e.prompt = edPromptNone
		return m, nil
	case "enter":
		kind, value := e.prompt, e.input.Value()
		e.prompt = edPromptNone
		width, _ := m.fmScreenSize()
		switch kind {
		case edPromptSearch:
			if value == "" {
				return m, nil
			}
			e.query = value
			e.searchNext(m.tr)
		case edPromptReplaceFind:
			if value == "" {
				return m, nil
			}
			e.query = value
			return m, e.openPrompt(edPromptReplaceWith, e.replaceWith)
		case edPromptReplaceWith:
			e.replaceWith = value
			e.replaceCount = 0
			if pos, ok := e.text.find(e.query, e.cur); ok {
				e.cur = pos
				e.dialog = edDialogReplace
			} else {
				e.message = fmt.Sprintf(m.tr(txtFMNotFound), sanitizeFileName(e.query))
			}
		}
		e.ensureVisible(width, m.fmEditorRows())
		return m, nil
	}
	if e.inputFresh && (msg.Type == tea.KeyRunes || msg.Type == tea.KeyBackspace) {
		e.input.SetValue("")
		if msg.Type == tea.KeyBackspace {
			e.inputFresh = false
			return m, nil
		}
	}
	e.inputFresh = false
	var cmd tea.Cmd
	e.input, cmd = e.input.Update(msg)
	return m, cmd
}

// searchNext sucht den letzten Begriff hinter dem Cursor (Text: ohne
// Groß-/Kleinschreibung; Hex: "ad ff" als Bytes, sonst Text).
//
// @date   2026-10-22 00:52
func (e *fmEditor) searchNext(tr func(textKey) string) {
	if e.query == "" {
		return
	}
	notFound := fmt.Sprintf(tr(txtFMNotFound), sanitizeFileName(e.query))
	if e.hexMode {
		h := e.hexb
		start := min(h.cursor+1, len(h.data))
		index := bytes.Index(h.data[start:], parseHexQuery(e.query))
		if index < 0 {
			e.message = notFound
			return
		}
		h.cursor, h.low = start+index, false
		return
	}
	pos, ok := e.text.find(e.query, textPos{e.cur.row, e.cur.col + 1})
	if !ok {
		e.message = notFound
		return
	}
	e.cur = pos
}

// handleFMEditorDialog verarbeitet Tasten in Rückfragen.
//
// @date   2026-10-22 00:52
func (m AppModel) handleFMEditorDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := &m.fm.edit
	key := msg.String()
	switch e.dialog {
	case edDialogSaveQuestion:
		switch key {
		case "j", "y":
			e.dialog = edDialogNone
			e.quitAfterSave = true
			return m.fmEditSave(false)
		case "n":
			return m.closeFMEditor()
		case "esc":
			e.dialog = edDialogNone
		}
	case edDialogConflict:
		switch key {
		case "j", "y":
			e.dialog = edDialogNone
			return m.fmEditSave(true)
		case "n", "esc":
			e.dialog = edDialogNone
			e.quitAfterSave = false
		}
	case edDialogReplace:
		matchLen := len([]rune(e.query))
		replacement := []rune(e.replaceWith)
		switch key {
		case "j", "y":
			e.cur = e.text.replaceAt(e.cur, matchLen, replacement)
			e.replaceCount++
			e.modified = true
			e.nextReplaceMatch(e.cur, m.tr)
		case "n":
			e.nextReplaceMatch(textPos{e.cur.row, e.cur.col + 1}, m.tr)
		case "a":
			for e.dialog == edDialogReplace {
				e.cur = e.text.replaceAt(e.cur, matchLen, replacement)
				e.replaceCount++
				e.modified = true
				e.nextReplaceMatch(e.cur, m.tr)
			}
		case "esc":
			e.finishReplace(m.tr)
		}
		width, _ := m.fmScreenSize()
		e.ensureVisible(width, m.fmEditorRows())
	}
	return m, nil
}

// nextReplaceMatch sucht den nächsten Treffer ab from; ohne Treffer endet
// das Ersetzen mit der Anzahl.
//
// @date   2026-10-22 00:52
func (e *fmEditor) nextReplaceMatch(from textPos, tr func(textKey) string) {
	if pos, ok := e.text.find(e.query, from); ok {
		e.cur = pos
		return
	}
	e.finishReplace(tr)
}

// finishReplace beendet das Ersetzen und meldet die Anzahl.
//
// @date   2026-10-22 00:52
func (e *fmEditor) finishReplace(tr func(textKey) string) {
	e.dialog = edDialogNone
	e.message = fmt.Sprintf(tr(txtFMEditReplaced), e.replaceCount)
	if e.replaceCount > 0 {
		e.markActive, e.blockSet = false, false
	}
}

// fmEditSave speichert im Hintergrund. Ohne force wird vorher geprüft, ob
// die Datei auf dem Server inzwischen geändert wurde (Größe oder Zeit).
//
// @param force - ohne Konfliktprüfung speichern
// @date   2026-10-22 00:52
func (m AppModel) fmEditSave(force bool) (tea.Model, tea.Cmd) {
	e := &m.fm.edit
	var data []byte
	if e.hexMode {
		data = append([]byte{}, e.hexb.data...)
	} else {
		data = e.text.bytes()
	}
	e.saving = true
	fsys, target, perm := e.fs, e.savePath, e.perm
	origSize, origMod := e.origSize, e.origMod
	session, seq := m.fm.session, m.fm.editSeq
	return m, func() tea.Msg {
		msg := fmEditSavedMsg{session: session, seq: seq}
		if !force {
			if info, err := fsys.Lstat(target); err == nil && (info.Size() != origSize || !info.ModTime().Equal(origMod)) {
				msg.conflict = true
				return msg
			}
		}
		if err := writeFileAtomically(fsys, target, data, perm); err != nil {
			msg.err = err
			return msg
		}
		if info, err := fsys.Lstat(target); err == nil {
			msg.size, msg.mod = info.Size(), info.ModTime()
		}
		return msg
	}
}

// handleFMEditSaved übernimmt das Ergebnis des Speicherns.
//
// @date   2026-10-22 00:52
func (m AppModel) handleFMEditSaved(msg fmEditSavedMsg) (tea.Model, tea.Cmd) {
	if msg.session != m.fm.session || msg.seq != m.fm.editSeq || !m.fm.editing {
		return m, nil
	}
	e := &m.fm.edit
	e.saving = false
	switch {
	case msg.conflict:
		e.dialog = edDialogConflict
		return m, nil
	case msg.err != nil:
		e.quitAfterSave = false
		text := sanitizeFileName(msg.err.Error())
		if errors.Is(msg.err, errCannotProtect) {
			text = fmt.Sprintf(m.tr(txtFMErrCannotProtect), e.name)
		}
		e.message = fmt.Sprintf(m.tr(txtFMErrSave), e.name, text)
		return m, nil
	}
	e.modified, e.savedOnce = false, true
	e.origSize, e.origMod = msg.size, msg.mod
	if e.hexMode {
		cursor := e.hexb.cursor
		e.hexb = newHexBuffer(e.hexb.data)
		e.hexb.cursor = cursor
	}
	e.message = fmt.Sprintf(m.tr(txtFMEditSaved), e.name)
	if e.quitAfterSave {
		return m.closeFMEditor()
	}
	return m, nil
}

// --- Darstellung ---

// renderFMEditor rendert den Editor bildschirmfüllend.
//
// @param s - String-Builder für die Ausgabe
// @date   2026-10-22 00:52
func (m AppModel) renderFMEditor(s *strings.Builder) {
	width, height := m.fmScreenSize()
	st := fmThemeStyles(m.fm.theme)
	e := m.fm.edit
	rows := max(height-2, 0)

	// Kopfzeile: Datei, geändert-Stern, Position, Zeilenende, Modus
	header := " " + e.title
	if e.modified {
		header += " *"
	}
	switch {
	case e.loading:
	case e.hexMode:
		header += fmt.Sprintf("  %08x/%08x  HEX", e.hexb.cursor, len(e.hexb.data))
	default:
		header += "  " + fmt.Sprintf(m.tr(txtFMEditPos), e.cur.row+1, e.cur.col+1)
		if e.text.crlf {
			header += "  CRLF"
		}
		if e.markActive {
			header += "  " + m.tr(txtFMEditMarking)
		}
	}
	lines := []string{st.menu.Render(fitWidth(header, width))}

	for i := 0; i < rows; i++ {
		switch {
		case e.loading:
			text := ""
			if i == 0 {
				text = m.tr(txtFMViewLoading)
			}
			lines = append(lines, st.panel.Render(fitWidth(text, width)))
		case e.hexMode:
			lines = append(lines, e.renderHexRow(st, e.hexTop+i, width))
		default:
			lines = append(lines, e.renderTextRow(st, e.top+i, width))
		}
	}

	// Untere Zeile: Eingabe, Meldung oder F-Tasten-Leiste wie im mcedit
	switch {
	case e.prompt != edPromptNone:
		label := m.tr(txtFMSearchPrompt)
		if e.prompt == edPromptReplaceWith {
			label = m.tr(txtFMEditReplacePrompt)
		}
		lines = append(lines, st.keyLabel.Render(fitWidth(" "+label+e.input.View(), width)))
	case e.message != "":
		lines = append(lines, st.message.Render(fitWidth(" "+e.message, width)))
	default:
		lines = append(lines, renderKeyBar(st, width, strings.Split(m.tr(txtFMEditFKeys), "|")))
	}

	// Hilfe bzw. Rückfrage als Kasten
	if len(lines) > 2 {
		var title, help string
		var content []string
		boxWidth := dialogBoxWidth(width)
		inner := max(boxWidth-4, 1)
		switch {
		case e.showHelp:
			title, help = m.tr(txtFMHelpTitle), m.tr(txtFMHelpClose)
			key := txtFMEditHelpText
			if e.hexMode {
				key = txtFMEditHexHelpText
			}
			content = strings.Split(m.tr(key), "\n")
		case e.dialog == edDialogSaveQuestion:
			title, help = m.tr(txtFMEditSaveTitle), m.tr(txtFMEditSaveHelp)
			content = wrapText(fmt.Sprintf(m.tr(txtFMEditSaveQuestion), e.name), inner)
		case e.dialog == edDialogConflict:
			title, help = m.tr(txtFMEditConflictTitle), m.tr(txtFMEditConflictHelp)
			content = wrapText(fmt.Sprintf(m.tr(txtFMEditConflict), e.name), inner)
		case e.dialog == edDialogReplace:
			title, help = m.tr(txtFMEditReplaceTitle), m.tr(txtFMEditReplaceHelp)
			content = wrapText(m.tr(txtFMEditReplaceQuestion), inner)
		}
		if title != "" {
			box := renderDialogBox(st, boxWidth, height, title, content, help)
			overlayDialog(lines, box, boxWidth, width)
		}
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	s.WriteString(strings.Join(lines, "\n"))
}

// editCell ist ein Stück einer Bildschirmzeile mit einheitlichem Stil.
type editCell struct {
	text  string
	style lipgloss.Style
}

// renderCells setzt Zellen zu einer Zeile genau width breit zusammen
// (gleiche Stile werden zusammengefasst).
//
// @date   2026-10-22 00:52
func renderCells(cells []editCell, fill lipgloss.Style, width int) string {
	// Stil-Kennung je Zelle einmal berechnen (Vergleich über die Darstellung)
	keys := make([]string, len(cells))
	for i, c := range cells {
		keys[i] = c.style.Render("x")
	}
	var out strings.Builder
	used := 0
	for i := 0; i < len(cells); {
		j := i
		var run strings.Builder
		for j < len(cells) && keys[j] == keys[i] {
			run.WriteString(cells[j].text)
			j++
		}
		text := run.String()
		out.WriteString(cells[i].style.Render(text))
		used += ansi.StringWidth(text)
		i = j
	}
	if used < width {
		out.WriteString(fill.Render(strings.Repeat(" ", width-used)))
	}
	return out.String()
}

// renderTextRow rendert eine Zeile des Textmodus mit Cursor und Block.
//
// @date   2026-10-22 00:52
func (e fmEditor) renderTextRow(st fmStyles, row, width int) string {
	if row >= len(e.text.lines) {
		return st.panel.Render(strings.Repeat(" ", max(width, 0)))
	}
	line := e.text.lines[row]
	var blockStart, blockEnd textPos
	inBlock := false
	switch {
	case e.blockSet:
		blockStart, blockEnd, inBlock = e.blockStart, e.blockEnd, true
	case e.markActive:
		blockStart, blockEnd = orderPos(e.anchor, e.cur)
		inBlock = true
	}
	var cells []editCell
	column := 0
	for col := 0; col <= len(line); col++ {
		text, w := " ", 1
		if col < len(line) {
			text, w = editRuneDisplay(line[col], column)
		} else if !(row == e.cur.row && col == e.cur.col) {
			break
		}
		style := st.panel
		pos := textPos{row, col}
		if inBlock && !pos.before(blockStart) && pos.before(blockEnd) {
			style = st.mark
		}
		if row == e.cur.row && col == e.cur.col {
			style = st.cursor
		}
		// Nur der sichtbare Ausschnitt [left, left+width)
		if column+w > e.left && column < e.left+width {
			if column < e.left || column+w > e.left+width {
				// Breites Zeichen am Rand: durch Leerzeichen ersetzen
				visible := min(column+w, e.left+width) - max(column, e.left)
				text = strings.Repeat(" ", visible)
			}
			cells = append(cells, editCell{text, style})
		}
		column += w
		if column >= e.left+width {
			break
		}
	}
	return renderCells(cells, st.panel, width)
}

// renderHexRow rendert eine Zeile des Hex-Editors: Offset, Bytes,
// Zeichen; Cursor in der aktiven Spalte, geänderte Bytes hervorgehoben.
//
// @date   2026-10-22 00:52
func (e fmEditor) renderHexRow(st fmStyles, row, width int) string {
	h := e.hexb
	perRow := hexBytesPerRow(width)
	start := row * perRow
	if start >= len(h.data) && !(start == 0 && len(h.data) == 0) {
		return st.panel.Render(strings.Repeat(" ", max(width, 0)))
	}
	cells := []editCell{{fmt.Sprintf("%08x  ", start), st.panel}}
	styleFor := func(i int, ascii bool) lipgloss.Style {
		switch {
		case i == h.cursor && ascii == e.asciiCol:
			return st.cursor
		case i == h.cursor:
			return st.activeTitle
		case i < len(h.changed) && h.changed[i]:
			return st.mark
		}
		return st.panel
	}
	for i := 0; i < perRow; i++ {
		if i > 0 {
			cells = append(cells, editCell{" ", st.panel})
		}
		index := start + i
		if index < len(h.data) {
			cells = append(cells, editCell{fmt.Sprintf("%02x", h.data[index]), styleFor(index, false)})
		} else {
			cells = append(cells, editCell{"  ", st.panel})
		}
	}
	cells = append(cells, editCell{"  ", st.panel})
	for i := 0; i < perRow && start+i < len(h.data); i++ {
		b := h.data[start+i]
		char := "."
		if b >= 0x20 && b < 0x7f {
			char = string(rune(b))
		}
		cells = append(cells, editCell{char, styleFor(start+i, true)})
	}
	// Auf die Breite kürzen
	var trimmed []editCell
	used := 0
	for _, c := range cells {
		w := ansi.StringWidth(c.text)
		if used+w > width {
			if rest := width - used; rest > 0 {
				trimmed = append(trimmed, editCell{c.text[:rest], c.style})
				used = width
			}
			break
		}
		trimmed = append(trimmed, c)
		used += w
	}
	return renderCells(trimmed, st.panel, width)
}
