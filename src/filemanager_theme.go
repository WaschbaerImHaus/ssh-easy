// Paket main - Datei-Manager (MC-Modus): Farbmodi
//
// Drei Farbmodi: klassisch wie der Midnight Commander (blau/türkis), im
// Stil von ssh-easy (dunkel/lila/pink, Menü türkis) und monochrom in fünf
// Graustufen (schwarz, dunkelgrau, grau, hellgrau, weiß). Umschalten wird
// später über das Menü "Optionen" möglich; bis dahin gilt der klassische
// Modus.
//
// Jeder Stil setzt Vorder- UND Hintergrund, damit jede Zelle des
// Bildschirms die Farbe des Modus trägt (sonst schimmert der
// Terminal-Hintergrund durch).
//
// @author Kurt Ingwer
// @date   2026-10-21 20:10
package main

import "github.com/charmbracelet/lipgloss"

// fmThemeColors beschreibt einen Farbmodus als Vorder-/Hintergrundpaare.
type fmThemeColors struct {
	// Interner Name (für die spätere Einstellung)
	name string
	// Menüleiste oben
	menuFg, menuBg string
	// Fensterfläche: Grundfarbe, Rahmen, Spaltenköpfe
	panelFg, panelBg, frameFg, headerFg string
	// Einträge: Verzeichnis, Datei, Symlink, defekter Symlink
	dirFg, fileFg, linkFg, brokenFg string
	// Cursorzeile
	cursorFg, cursorBg string
	// Markierte Einträge: Vorder-/Hintergrund, Hintergrund der Cursorzeile
	// auf einem markierten Eintrag. Im Monochrom-Modus unterscheidet ein
	// eigener grauer Hintergrund, da Graustufen allein zu ähnlich sind.
	markFg, markBg, cursorMarkBg string
	// Titel des aktiven Fensters
	activeTitleFg, activeTitleBg string
	// Meldungen (z. B. "kein Zugriff")
	messageFg, messageBg string
	// F-Tasten-Leiste: Nummer und Beschriftung
	keyNumFg, keyNumBg, keyLabelFg, keyLabelBg string
}

// fmThemes enthält alle Farbmodi; Index 0 ist der Standard.
var fmThemes = []fmThemeColors{
	{
		// Klassisch wie der Midnight Commander
		name:   "classic",
		menuFg: "0", menuBg: "6",
		panelFg: "7", panelBg: "4", frameFg: "7", headerFg: "11",
		dirFg: "15", fileFg: "7", linkFg: "14", brokenFg: "9",
		cursorFg: "0", cursorBg: "6",
		markFg: "11", markBg: "4", cursorMarkBg: "6",
		activeTitleFg: "0", activeTitleBg: "6",
		messageFg: "15", messageBg: "1",
		keyNumFg: "7", keyNumBg: "0", keyLabelFg: "0", keyLabelBg: "6",
	},
	{
		// Farben der ssh-easy-Verbindungsliste, Menü türkis
		name:   "ssh-easy",
		menuFg: "16", menuBg: "37",
		panelFg: "252", panelBg: "233", frameFg: "57", headerFg: "244",
		dirFg: "205", fileFg: "252", linkFg: "37", brokenFg: "196",
		cursorFg: "229", cursorBg: "57",
		markFg: "226", markBg: "233", cursorMarkBg: "57",
		activeTitleFg: "229", activeTitleBg: "57",
		messageFg: "229", messageBg: "160",
		keyNumFg: "205", keyNumBg: "233", keyLabelFg: "252", keyLabelBg: "236",
	},
	{
		// Monochrom: schwarz 16, dunkelgrau 238, grau 245, hellgrau 250, weiß 231
		// (markiert: weiß auf dunkelgrau, Cursor auf markiert: weiß auf grau)
		name:   "mono",
		menuFg: "16", menuBg: "245",
		panelFg: "250", panelBg: "16", frameFg: "245", headerFg: "245",
		dirFg: "231", fileFg: "250", linkFg: "245", brokenFg: "238",
		cursorFg: "16", cursorBg: "250",
		markFg: "231", markBg: "238", cursorMarkBg: "245",
		activeTitleFg: "16", activeTitleBg: "250",
		messageFg: "16", messageBg: "231",
		keyNumFg: "231", keyNumBg: "16", keyLabelFg: "16", keyLabelBg: "245",
	},
}

// fmStyles enthält die fertigen lipgloss-Stile eines Farbmodus.
type fmStyles struct {
	menu, panel, frame, header                   lipgloss.Style
	dir, file, link, broken, cursor, activeTitle lipgloss.Style
	mark, markFrame, cursorMark                  lipgloss.Style
	message, keyNum, keyLabel                    lipgloss.Style
}

// fmThemeStyles baut die Stile für einen Farbmodus. Ungültiger Index =
// Standardmodus.
//
// @param index - Index in fmThemes
// @return fmStyles - Stile
// @date   2026-10-21 20:10
func fmThemeStyles(index int) fmStyles {
	if index < 0 || index >= len(fmThemes) {
		index = 0
	}
	c := fmThemes[index]
	pair := func(fg, bg string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Background(lipgloss.Color(bg))
	}
	return fmStyles{
		menu:        pair(c.menuFg, c.menuBg),
		panel:       pair(c.panelFg, c.panelBg),
		frame:       pair(c.frameFg, c.panelBg),
		header:      pair(c.headerFg, c.panelBg).Bold(true),
		dir:         pair(c.dirFg, c.panelBg).Bold(true),
		file:        pair(c.fileFg, c.panelBg),
		link:        pair(c.linkFg, c.panelBg),
		broken:      pair(c.brokenFg, c.panelBg),
		cursor:      pair(c.cursorFg, c.cursorBg),
		mark:        pair(c.markFg, c.markBg).Bold(true),
		markFrame:   pair(c.frameFg, c.markBg),
		cursorMark:  pair(c.markFg, c.cursorMarkBg).Bold(true),
		activeTitle: pair(c.activeTitleFg, c.activeTitleBg),
		message:     pair(c.messageFg, c.messageBg).Bold(true),
		keyNum:      pair(c.keyNumFg, c.keyNumBg),
		keyLabel:    pair(c.keyLabelFg, c.keyLabelBg),
	}
}
