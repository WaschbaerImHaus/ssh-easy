// Paket main - Datei-Manager (MC-Modus): Betrachter (F3)
//
// F3 auf einer Datei öffnet einen bildschirmfüllenden Betrachter wie im mc.
// Die Datei wird per SFTP geladen (höchstens fmViewMaxBytes, sonst gekürzt
// mit Hinweis). Der Inhalt kommt vom Server und ist nicht vertrauenswürdig:
// Steuerzeichen und unsichtbare Formatzeichen werden zu "?", Tabs zu
// Leerzeichen - so kann eine Datei das Terminal nicht steuern.
//
// Tasten: ↑/↓, Bild↑/Bild↓, Pos1/Ende blättern; F2 Zeilenumbruch an/aus
// (ohne Umbruch ←/→ waagerecht); F4 Hex/Text; q, Esc, F3, F10 schließen.
// Binärdaten (NUL-Bytes) erscheinen automatisch als Hex-Ansicht.
//
// @author Kurt Ingwer
// @date   2026-10-21 21:24
package main

import (
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// fmViewMaxBytes - höchstens so viel wird geladen (Variable für Tests).
var fmViewMaxBytes int64 = 16 << 20

// fmViewTabWidth - Tabulatorabstand in der Textansicht
const fmViewTabWidth = 8

// fmViewScrollStep - so viele Zeichen springen ←/→ ohne Umbruch
const fmViewScrollStep = 8

// fmViewer ist der Zustand des Betrachters.
type fmViewer struct {
	// Anzeigename (user@host:/pfad) und Dateiname
	title, name string
	// Geladene Bytes und ob die Datei länger war
	data      []byte
	truncated bool
	// Dateigröße laut Server
	size int64
	// Aufbereitete Textzeilen und die breiteste Zeile (Zellen)
	lines    []string
	maxWidth int
	// Läuft das Laden noch?
	loading bool
	// Hex-Ansicht bzw. Zeilenumbruch
	hex, wrap bool
	// Textansicht: erste sichtbare Zeile und Umbruchstück darin
	top, topSub int
	// Textansicht ohne Umbruch: waagerechte Verschiebung in Zellen
	left int
	// Hex-Ansicht: erste sichtbare Zeile
	hexTop int
}

// fmViewLoadedMsg überbringt die geladene Datei.
type fmViewLoadedMsg struct {
	session, seq int
	data         []byte
	size         int64
	truncated    bool
	err          error
}

// fmOpenViewer öffnet den Betrachter für den Eintrag unter dem Cursor.
// Verzeichnisse und ".." verhalten sich wie Enter (wie im mc).
//
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Laden im Hintergrund
// @date   2026-10-21 21:24
func (m AppModel) fmOpenViewer() (tea.Model, tea.Cmd) {
	panel := m.fm.panels[m.fm.active]
	entry, ok := panel.current()
	if !ok || panel.fs == nil || panel.loading {
		return m, nil
	}
	if entry.dir {
		return m.fmEnter()
	}
	filePath := path.Join(panel.path, entry.name)
	m.fm.viewSeq++
	m.fm.viewing = true
	m.fm.view = fmViewer{
		title:   sanitizeFileName(panel.conn.User + "@" + panel.conn.Host + ":" + filePath),
		name:    sanitizeFileName(entry.name),
		loading: true,
		wrap:    true,
	}
	fsys, session, seq := panel.fs, m.fm.session, m.fm.viewSeq
	return m, func() tea.Msg {
		return loadViewData(fsys, filePath, session, seq)
	}
}

// loadViewData liest eine Datei bis zur Größengrenze.
//
// @param fsys - Dateisystem
// @param filePath - Pfad (Symlinks auf Dateien werden beim Öffnen verfolgt)
// @param session - Sitzungsnummer
// @param seq - Nummer des Betrachters
// @return fmViewLoadedMsg - Ergebnis
// @date   2026-10-21 21:24
func loadViewData(fsys remoteFS, filePath string, session, seq int) fmViewLoadedMsg {
	msg := fmViewLoadedMsg{session: session, seq: seq}
	reader, err := fsys.OpenRead(filePath)
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
		data = data[:fmViewMaxBytes]
		msg.truncated = true
	}
	msg.data = data
	msg.size = int64(len(data))
	if info, err := fsys.Stat(filePath); err == nil && info.Size() >= 0 {
		msg.size = info.Size()
	}
	return msg
}

// handleFMViewLoaded übernimmt die geladene Datei.
//
// @param msg - Ergebnis
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-21 21:24
func (m AppModel) handleFMViewLoaded(msg fmViewLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.session != m.fm.session || msg.seq != m.fm.viewSeq || !m.fm.viewing {
		return m, nil
	}
	if msg.err != nil {
		m.fm.viewing = false
		m.fm.message = fmt.Sprintf(m.tr(txtFMErrView), m.fm.view.name, sanitizeFileName(msg.err.Error()))
		m.fm.view = fmViewer{}
		return m, nil
	}
	v := &m.fm.view
	v.loading = false
	v.data = msg.data
	v.size = msg.size
	v.truncated = msg.truncated
	v.hex = looksBinary(msg.data)
	v.lines = splitViewLines(msg.data)
	for _, line := range v.lines {
		v.maxWidth = max(v.maxWidth, ansi.StringWidth(line))
	}
	return m, nil
}

// handleFMViewerKeys verarbeitet Tasten im Betrachter.
//
// @param msg - Tastendruck
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - kein Folgekommando
// @date   2026-10-21 21:24
func (m AppModel) handleFMViewerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "f3", "f10":
		m.fm.viewing = false
		m.fm.view = fmViewer{}
		return m, nil
	}
	v := &m.fm.view
	if v.loading {
		return m, nil
	}
	width, height := m.fmScreenSize()
	rows := max(height-2, 1)
	switch msg.String() {
	case "f2":
		v.wrap = !v.wrap
		v.topSub, v.left = 0, 0
	case "f4":
		v.hex = !v.hex
	case "up", "k":
		v.scroll(-1, width, rows)
	case "down", "j":
		v.scroll(1, width, rows)
	case "pgup":
		v.scroll(-rows, width, rows)
	case "pgdown":
		v.scroll(rows, width, rows)
	case "home":
		v.top, v.topSub, v.left, v.hexTop = 0, 0, 0, 0
	case "end":
		v.toEnd(width, rows)
	case "left":
		v.left = max(v.left-fmViewScrollStep, 0)
	case "right":
		if !v.wrap && !v.hex {
			v.left = min(v.left+fmViewScrollStep, max(v.maxWidth-width, 0))
		}
	}
	return m, nil
}

// scroll bewegt die Ansicht um delta Bildschirmzeilen; am Ende bleibt die
// letzte Bildschirmseite voll.
//
// @param delta - Zeilen (negativ = nach oben)
// @param width - Bildschirmbreite
// @param rows - sichtbare Zeilen
// @date   2026-10-21 21:24
func (v *fmViewer) scroll(delta, width, rows int) {
	if v.hex {
		total := hexRowCount(len(v.data), hexBytesPerRow(width))
		v.hexTop = min(max(v.hexTop+delta, 0), max(total-rows, 0))
		return
	}
	if !v.wrap {
		v.top = min(max(v.top+delta, 0), max(len(v.lines)-rows, 0))
		v.topSub = 0
		return
	}
	for ; delta < 0; delta++ {
		if v.topSub > 0 {
			v.topSub--
		} else if v.top > 0 {
			v.top--
			v.topSub = len(segmentLine(v.lines[v.top], width)) - 1
		}
	}
	for ; delta > 0 && !v.atEnd(width, rows); delta-- {
		if v.topSub+1 < len(segmentLine(v.lines[v.top], width)) {
			v.topSub++
		} else if v.top+1 < len(v.lines) {
			v.top++
			v.topSub = 0
		}
	}
}

// toEnd springt ans Ende, sodass die letzte Bildschirmseite voll ist.
// Im Umbruch-Modus vom letzten Stück aus rückwärts - nicht Zeile für
// Zeile von vorn (bei großen Dateien zu langsam).
//
// @param width - Bildschirmbreite
// @param rows - sichtbare Zeilen
// @date   2026-10-21 21:24
func (v *fmViewer) toEnd(width, rows int) {
	if v.hex || !v.wrap {
		v.scroll(len(v.data)+len(v.lines)+1, width, rows)
		return
	}
	if len(v.lines) == 0 {
		return
	}
	v.top = len(v.lines) - 1
	v.topSub = len(segmentLine(v.lines[v.top], width)) - 1
	v.scroll(-(rows - 1), width, rows)
}

// atEnd meldet (Umbruch-Modus), ob ab der aktuellen Position höchstens noch
// eine Bildschirmseite folgt.
//
// @date   2026-10-21 21:24
func (v *fmViewer) atEnd(width, rows int) bool {
	remaining := 0
	for i := v.top; i < len(v.lines); i++ {
		segments := len(segmentLine(v.lines[i], width))
		if i == v.top {
			segments -= v.topSub
		}
		remaining += segments
		if remaining > rows {
			return false
		}
	}
	return true
}

// renderFMViewer rendert den Betrachter bildschirmfüllend.
//
// @param s - String-Builder für die Ausgabe
// @date   2026-10-21 21:24
func (m AppModel) renderFMViewer(s *strings.Builder) {
	width, height := m.fmScreenSize()
	st := fmThemeStyles(m.fm.theme)
	v := m.fm.view
	rows := max(height-2, 0)

	// Kopfzeile: Datei, Größe, Position, ggf. Hinweis auf Kürzung
	header := " " + v.title + "  " + formatSizeWithUnit(v.size)
	if !v.loading {
		if v.hex {
			header += fmt.Sprintf("  %08x", v.hexTop*hexBytesPerRow(width))
		} else {
			header += "  " + fmt.Sprintf(m.tr(txtFMViewLine), min(v.top+1, max(len(v.lines), 1)), len(v.lines))
		}
		if v.truncated {
			header += "  " + fmt.Sprintf(m.tr(txtFMViewTruncated), formatSizeWithUnit(int64(len(v.data))))
		}
	}
	lines := []string{st.menu.Render(fitWidth(header, width))}

	var content []string
	switch {
	case v.loading:
		content = []string{m.tr(txtFMViewLoading)}
	case v.hex:
		content = v.hexRows(width, rows)
	case v.wrap:
		content = v.wrappedRows(width, rows)
	default:
		for i := v.top; i < len(v.lines) && len(content) < rows; i++ {
			content = append(content, ansi.TruncateLeft(v.lines[i], v.left, ""))
		}
	}
	for i := 0; i < rows; i++ {
		text := ""
		if i < len(content) {
			text = content[i]
		}
		lines = append(lines, st.panel.Render(fitWidth(text, width)))
	}
	lines = append(lines, st.keyLabel.Render(fitWidth(" "+m.tr(txtFMViewHelp), width)))
	if len(lines) > height {
		lines = lines[:height]
	}
	s.WriteString(strings.Join(lines, "\n"))
}

// wrappedRows liefert die sichtbaren Bildschirmzeilen im Umbruch-Modus.
//
// @date   2026-10-21 21:24
func (v fmViewer) wrappedRows(width, rows int) []string {
	var out []string
	for i := v.top; i < len(v.lines) && len(out) < rows; i++ {
		segments := segmentLine(v.lines[i], width)
		start := 0
		if i == v.top {
			start = min(v.topSub, len(segments)-1)
		}
		for _, segment := range segments[start:] {
			if len(out) == rows {
				break
			}
			out = append(out, segment)
		}
	}
	return out
}

// hexRows liefert die sichtbaren Zeilen der Hex-Ansicht.
//
// @date   2026-10-21 21:24
func (v fmViewer) hexRows(width, rows int) []string {
	perRow := hexBytesPerRow(width)
	total := hexRowCount(len(v.data), perRow)
	top := min(v.hexTop, max(total-rows, 0))
	var out []string
	for row := top; row < total && len(out) < rows; row++ {
		start := row * perRow
		out = append(out, hexLine(v.data[start:min(start+perRow, len(v.data))], start, perRow))
	}
	return out
}

// hexBytesPerRow wählt die Bytes pro Hex-Zeile passend zur Breite:
// "OOOOOOOO  hh hh …  ascii" braucht 11 + 4 Zellen je Byte; höchstens 16.
//
// @param width - Bildschirmbreite
// @return int - Bytes pro Zeile (mindestens 1)
// @date   2026-10-21 21:24
func hexBytesPerRow(width int) int {
	return min(max((width-11)/4, 1), 16)
}

// hexRowCount liefert die Zahl der Hex-Zeilen.
//
// @date   2026-10-21 21:24
func hexRowCount(size, perRow int) int {
	return (size + perRow - 1) / perRow
}

// hexLine formatiert eine Hex-Zeile: Offset, Bytes, druckbare Zeichen.
//
// @param chunk - Bytes der Zeile
// @param offset - Position des ersten Bytes
// @param perRow - Bytes pro Zeile (für gleichmäßige Spalten)
// @return string - Zeile
// @date   2026-10-21 21:24
func hexLine(chunk []byte, offset, perRow int) string {
	var hexPart, asciiPart strings.Builder
	for i := 0; i < perRow; i++ {
		if i > 0 {
			hexPart.WriteByte(' ')
		}
		if i < len(chunk) {
			fmt.Fprintf(&hexPart, "%02x", chunk[i])
			if chunk[i] >= 0x20 && chunk[i] < 0x7f {
				asciiPart.WriteByte(chunk[i])
			} else {
				asciiPart.WriteByte('.')
			}
		} else {
			hexPart.WriteString("  ")
		}
	}
	return fmt.Sprintf("%08x  %s  %s", offset, hexPart.String(), asciiPart.String())
}

// splitViewLines zerlegt den Inhalt in aufbereitete Zeilen; ein
// abschließender Zeilenumbruch erzeugt keine leere letzte Zeile.
//
// @param data - Dateiinhalt
// @return []string - Zeilen
// @date   2026-10-21 21:24
func splitViewLines(data []byte) []string {
	raw := strings.Split(string(data), "\n")
	if len(raw) > 1 && raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
	}
	lines := make([]string, len(raw))
	for i, line := range raw {
		lines[i] = prepareViewLine(line)
	}
	return lines
}

// prepareViewLine macht eine Zeile anzeigbar: CR am Ende entfernen, Tabs zu
// Leerzeichen, Steuer- und Formatzeichen zu "?".
//
// @param line - Rohzeile
// @return string - sichere Zeile
// @date   2026-10-21 21:24
func prepareViewLine(line string) string {
	line = strings.TrimSuffix(line, "\r")
	if strings.Contains(line, "\t") {
		var out strings.Builder
		column := 0
		for _, r := range line {
			if r == '\t' {
				spaces := fmViewTabWidth - column%fmViewTabWidth
				out.WriteString(strings.Repeat(" ", spaces))
				column += spaces
				continue
			}
			out.WriteRune(r)
			column += max(runewidth.RuneWidth(r), 0)
		}
		line = out.String()
	}
	return sanitizeFileName(line)
}

// segmentLine bricht eine Zeile nach Zellbreite um (breite Zeichen zählen
// doppelt und werden nicht geteilt). Eine leere Zeile ergibt ein leeres Stück.
//
// @param line - aufbereitete Zeile
// @param width - Breite (≤ 0 = nicht umbrechen)
// @return []string - Stücke
// @date   2026-10-21 21:24
func segmentLine(line string, width int) []string {
	if width <= 0 || ansi.StringWidth(line) <= width {
		return []string{line}
	}
	var segments []string
	var current strings.Builder
	used := 0
	for _, r := range line {
		w := runewidth.RuneWidth(r)
		if used+w > width && used > 0 {
			segments = append(segments, current.String())
			current.Reset()
			used = 0
		}
		current.WriteRune(r)
		used += w
	}
	return append(segments, current.String())
}

// looksBinary erkennt Binärdaten an NUL-Bytes in den ersten 8 KiB.
//
// @param data - Inhalt
// @return bool - true bei Binärdaten
// @date   2026-10-21 21:24
func looksBinary(data []byte) bool {
	return bytes.IndexByte(data[:min(len(data), 8192)], 0) >= 0
}
