// Paket main - Zeilenmodell der Verbindungsliste (Gruppen + Scrollen)
//
// Ein Doppelpunkt im Verbindungsnamen ist ein Gruppentrennzeichen:
// "Abc:123" und "Abc:234" erscheinen als Gruppe "Abc" mit den Einträgen
// "123" und "234". Getrennt wird nur am ERSTEN Doppelpunkt
// ("Büro:Server:1" -> Gruppe "Büro", Eintrag "Server:1").
//
// Die Liste zeigt höchstens maxVisibleListRows Zeilen gleichzeitig; bei mehr
// Zeilen wird gescrollt. Alle Funktionen hier sind rein (ohne Seiteneffekte)
// und dadurch einfach testbar.
//
// @author Kurt Ingwer
// @date   2026-10-21 19:45
package main

import "strings"

// groupSeparator trennt Gruppenname und Eintragsname.
const groupSeparator = ":"

// maxVisibleListRows - maximale Anzahl gleichzeitig sichtbarer Listenzeilen.
// Ab der 16. Zeile wird die Liste scrollbar.
const maxVisibleListRows = 15

// minVisibleListRows - Untergrenze für sehr kleine Terminalfenster.
const minVisibleListRows = 3

// listChromeLines - Zeilen, die außer der Liste selbst auf dem Bildschirm
// stehen (Titel, Bildlauf-Hinweise, Meldungen, Hilfe, Export-Hinweis,
// Auto-Update-Zeile, Leerzeile vor dem Datei-Manager-Eintrag).
const listChromeLines = 13

// listRowKind unterscheidet die Zeilentypen der Liste.
type listRowKind int

const (
	// rowSingle - Verbindung ohne Gruppe
	rowSingle listRowKind = iota
	// rowGroup - Gruppenkopf (auf-/zuklappbar)
	rowGroup
	// rowChild - Verbindung innerhalb einer aufgeklappten Gruppe
	rowChild
	// rowFileManager - Eintrag "Datei-Manager" ganz unten (keine Verbindung)
	rowFileManager
)

// listRow ist eine sichtbare Zeile der Verbindungsliste.
type listRow struct {
	// Zeilentyp
	kind listRowKind
	// Anzeigetext: Name, Gruppenname oder Eintragsname nach dem Doppelpunkt
	label string
	// Gruppenschlüssel in Kleinbuchstaben (nur rowGroup/rowChild)
	groupKey string
	// Index in der Verbindungsliste (-1 bei rowGroup und rowFileManager)
	connIndex int
	// Indizes aller Verbindungen der Gruppe (nur rowGroup)
	children []int
	// Letzter Eintrag seiner Gruppe? (nur rowChild, für └ statt ├)
	lastChild bool
}

// splitGroupName zerlegt einen Namen am ersten Doppelpunkt.
// ok ist nur true, wenn vor UND nach dem Doppelpunkt Text steht.
//
// @param name - Verbindungsname, z.B. "Abc:123"
// @return group - Gruppenname ("Abc")
// @return child - Eintragsname ("123")
// @return ok - true wenn der Name eine gültige Gruppierung enthält
// @date   2026-10-16 10:00
func splitGroupName(name string) (group, child string, ok bool) {
	before, after, found := strings.Cut(name, groupSeparator)
	if !found {
		return "", "", false
	}
	group = strings.TrimSpace(before)
	child = strings.TrimSpace(after)
	if group == "" || child == "" {
		return "", "", false
	}
	return group, child, true
}

// validGroupName prüft einen Namen für das Formular: Ohne Doppelpunkt ist
// jeder Name gültig, mit Doppelpunkt muss davor und danach Text stehen.
//
// @param name - Eingegebener Name
// @return bool - true wenn gültig
// @date   2026-10-16 10:00
func validGroupName(name string) bool {
	if !strings.Contains(name, groupSeparator) {
		return true
	}
	_, _, ok := splitGroupName(name)
	return ok
}

// listEntry ist ein Eintrag der obersten Ebene: Einzelverbindung oder Gruppe.
type listEntry struct {
	// Einzelverbindung: Index in conns; Gruppe: -1
	single int
	// Gruppenname in der Schreibweise des ersten Eintrags
	groupLabel string
	// Gruppenschlüssel (Kleinbuchstaben)
	groupKey string
	// Indizes der Gruppenmitglieder
	members []int
}

// buildListRows erzeugt die sichtbaren Zeilen aus der (sortierten)
// Verbindungsliste. Gruppen erscheinen an der Position ihres ersten
// Mitglieds; aufgeklappte Gruppen zeigen ihre Mitglieder darunter.
//
// @param conns - Alphabetisch sortierte Verbindungen
// @param expanded - Aufgeklappte Gruppen (Schlüssel = Kleinbuchstaben)
// @return []listRow - Zeilen in Anzeigereihenfolge
// @date   2026-10-16 10:00
func buildListRows(conns []Connection, expanded map[string]bool) []listRow {
	// Schritt 1: Einträge der obersten Ebene sammeln
	var entries []*listEntry
	groups := make(map[string]*listEntry)
	for i, conn := range conns {
		group, _, ok := splitGroupName(conn.Name)
		if !ok {
			entries = append(entries, &listEntry{single: i})
			continue
		}
		key := strings.ToLower(group)
		entry, exists := groups[key]
		if !exists {
			entry = &listEntry{single: -1, groupLabel: group, groupKey: key}
			groups[key] = entry
			entries = append(entries, entry)
		}
		entry.members = append(entry.members, i)
	}

	// Schritt 2: Zeilen erzeugen
	rows := make([]listRow, 0, len(conns))
	for _, entry := range entries {
		if entry.single >= 0 {
			rows = append(rows, listRow{kind: rowSingle, label: conns[entry.single].Name, connIndex: entry.single})
			continue
		}
		rows = append(rows, listRow{
			kind:      rowGroup,
			label:     entry.groupLabel,
			groupKey:  entry.groupKey,
			connIndex: -1,
			children:  entry.members,
		})
		if !expanded[entry.groupKey] {
			continue
		}
		for n, idx := range entry.members {
			_, child, _ := splitGroupName(conns[idx].Name)
			rows = append(rows, listRow{
				kind:      rowChild,
				label:     child,
				groupKey:  entry.groupKey,
				connIndex: idx,
				lastChild: n == len(entry.members)-1,
			})
		}
	}
	return rows
}

// scrollOffset berechnet die erste sichtbare Zeile so, dass der Cursor im
// Fenster bleibt. Das Fenster bewegt sich erst, wenn der Cursor den Rand
// verlässt (kein Springen beim Hoch-/Runterlaufen).
//
// @param total - Anzahl aller Zeilen
// @param cursor - Cursorposition
// @param offset - Bisherige erste sichtbare Zeile
// @param limit - Anzahl sichtbarer Zeilen
// @return int - Neue erste sichtbare Zeile
// @date   2026-10-16 10:00
func scrollOffset(total, cursor, offset, limit int) int {
	if limit <= 0 || total <= limit {
		return 0
	}
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+limit {
		offset = cursor - limit + 1
	}
	// Nicht über das Listenende hinaus scrollen
	if offset > total-limit {
		offset = total - limit
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// visibleRowLimit liefert die Anzahl gleichzeitig sichtbarer Listenzeilen:
// höchstens 15, bei kleinen Fenstern weniger (damit Titel und Hilfe
// sichtbar bleiben), mindestens 3.
//
// @param termHeight - Terminalhöhe in Zeilen (0 = unbekannt)
// @return int - Sichtbare Zeilen
// @date   2026-10-16 10:00
func visibleRowLimit(termHeight int) int {
	if termHeight <= 0 {
		return maxVisibleListRows
	}
	limit := termHeight - listChromeLines
	if limit > maxVisibleListRows {
		return maxVisibleListRows
	}
	if limit < minVisibleListRows {
		return minVisibleListRows
	}
	return limit
}
