// Paket main - Auswahlliste mit Häkchen für Export und Import (Build 52)
//
// connChecklist zeigt Verbindungen wie die Hauptliste (Gruppen per
// Doppelpunkt, hier immer aufgeklappt) mit Auswahlkästchen:
//
//	[x] gewählt, [ ] nicht gewählt, [~] Gruppe teilweise gewählt
//
// Tasten: ↑/↓ (j/k), Bild↑/Bild↓, Pos1/Ende, Leertaste (Eintrag bzw. ganze
// Gruppe umschalten), a (alle/keine).
//
// @author Kurt Ingwer
// @date   2026-10-17 23:40
package main

import (
	"fmt"
	"strings"
)

// connChecklist ist eine scrollbare Verbindungsliste mit Auswahl.
type connChecklist struct {
	// Angezeigte Verbindungen (alphabetisch sortiert, IDs eindeutig)
	conns []Connection
	// Gewählte Verbindungs-IDs
	selected map[string]bool
	// Cursorzeile und erste sichtbare Zeile
	cursor, offset int
}

// newConnChecklist erstellt eine Auswahlliste.
//
// @param conns - Verbindungen (werden nach Namen sortiert)
// @param preselect - Liefert true für anfangs gewählte Verbindungen
// @return connChecklist - Neue Liste, Cursor auf der ersten Zeile
// @date   2026-10-17 23:40
func newConnChecklist(conns []Connection, preselect func(Connection) bool) connChecklist {
	list := connChecklist{conns: sortConnectionsByName(conns), selected: make(map[string]bool, len(conns))}
	for _, conn := range list.conns {
		if preselect(conn) {
			list.selected[conn.ID] = true
		}
	}
	return list
}

// rows liefert die Zeilen der Liste (alle Gruppen aufgeklappt).
//
// @return []listRow - Zeilen
// @date   2026-10-17 23:40
func (l connChecklist) rows() []listRow {
	expanded := make(map[string]bool)
	for _, row := range buildListRows(l.conns, nil) {
		if row.kind == rowGroup {
			expanded[row.groupKey] = true
		}
	}
	return buildListRows(l.conns, expanded)
}

// rowIDs liefert die Verbindungs-IDs einer Zeile (Gruppe: alle Mitglieder).
//
// @param row - Zeile
// @return []string - IDs
// @date   2026-10-17 23:40
func (l connChecklist) rowIDs(row listRow) []string {
	if row.kind == rowGroup {
		ids := make([]string, 0, len(row.children))
		for _, idx := range row.children {
			ids = append(ids, l.conns[idx].ID)
		}
		return ids
	}
	return []string{l.conns[row.connIndex].ID}
}

// count zählt die gewählten Verbindungen.
//
// @return int - Anzahl
// @date   2026-10-17 23:40
func (l connChecklist) count() int {
	n := 0
	for _, conn := range l.conns {
		if l.selected[conn.ID] {
			n++
		}
	}
	return n
}

// handleKey verarbeitet eine Taste der Liste.
//
// @param key - Tastenname (msg.String())
// @param limit - Anzahl sichtbarer Zeilen (für Bild↑/Bild↓)
// @return bool - true wenn die Taste zur Liste gehört
// @date   2026-10-17 23:40
func (l *connChecklist) handleKey(key string, limit int) bool {
	rows := l.rows()
	switch key {
	case "up", "k":
		l.cursor--
	case "down", "j":
		l.cursor++
	case "pgup":
		l.cursor -= limit
	case "pgdown":
		l.cursor += limit
	case "home":
		l.cursor = 0
	case "end":
		l.cursor = len(rows) - 1
	case " ", "space":
		if l.cursor >= 0 && l.cursor < len(rows) {
			ids := l.rowIDs(rows[l.cursor])
			// Ganz gewählt -> alle aus; sonst (keiner/teilweise) -> alle an
			allOn := true
			for _, id := range ids {
				allOn = allOn && l.selected[id]
			}
			for _, id := range ids {
				l.selected[id] = !allOn
			}
		}
	case "a":
		allOn := l.count() == len(l.conns)
		for _, conn := range l.conns {
			l.selected[conn.ID] = !allOn
		}
	default:
		return false
	}
	if l.cursor >= len(rows) {
		l.cursor = len(rows) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	l.offset = scrollOffset(len(rows), l.cursor, l.offset, limit)
	return true
}

// render schreibt den sichtbaren Ausschnitt der Liste.
//
// @param s - Ausgabe
// @param limit - Anzahl sichtbarer Zeilen
// @param active - false = Cursor nicht hervorheben (Fokus woanders)
// @param note - Optionaler Zusatz hinter einer Verbindung (nil = keiner)
// @date   2026-10-17 23:40
func (l connChecklist) render(s *strings.Builder, limit int, active bool, note func(conn Connection, selected bool) string) {
	rows := l.rows()
	offset := scrollOffset(len(rows), l.cursor, l.offset, limit)
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	if offset > 0 {
		s.WriteString(helpStyle.UnsetMarginTop().Render(fmt.Sprintf("  ↑ +%d", offset)) + "\n")
	}
	for i := offset; i < end; i++ {
		s.WriteString(l.renderRow(rows[i], active && i == l.cursor, note))
		s.WriteString("\n")
	}
	if end < len(rows) {
		s.WriteString(helpStyle.UnsetMarginTop().Render(fmt.Sprintf("  ↓ +%d", len(rows)-end)) + "\n")
	}
}

// renderRow rendert eine Zeile mit Auswahlkästchen.
//
// @param row - Zeile
// @param cursor - Cursor steht auf der Zeile
// @param note - Optionaler Zusatz hinter einer Verbindung
// @return string - Gerenderte Zeile
// @date   2026-10-17 23:40
func (l connChecklist) renderRow(row listRow, cursor bool, note func(Connection, bool) string) string {
	base := normalStyle
	if cursor {
		base = selectedStyle
	}
	on := 0
	ids := l.rowIDs(row)
	for _, id := range ids {
		if l.selected[id] {
			on++
		}
	}
	box := "[ ]"
	switch {
	case on == len(ids):
		box = "[x]"
	case on > 0:
		box = "[~]"
	}

	suffix := ""
	if row.kind != rowGroup && note != nil {
		conn := l.conns[row.connIndex]
		if text := note(conn, l.selected[conn.ID]); text != "" {
			suffix = "  " + text
		}
	}
	switch row.kind {
	case rowGroup:
		return base.Render(fmt.Sprintf("  %s %s (%d)", box, row.label, len(row.children)))
	case rowChild:
		branch := "├"
		if row.lastChild {
			branch = "└"
		}
		return base.Render("    " + branch + " " + box + " " + row.label + suffix)
	}
	return base.Render("  " + box + " " + row.label + suffix)
}
