// Paket main - Letzte Fenstergröße merken (plattformneutrale Logik)
//
// Die Fenstergröße (Spalten × Zeilen) wird gespeichert, solange das Fenster
// nicht maximiert, im Vollbild oder minimiert ist, und beim nächsten Start
// wieder hergestellt. Gespeichert wird verzögert nach der letzten
// Größenänderung (beim Ziehen am Fensterrand kommen viele Ereignisse) und
// zusätzlich beim Beenden.
//
// Die Konsolen-API ist plattformspezifisch:
//
//	window_size_windows.go - GetConsoleScreenBufferInfo/SetConsoleWindowInfo
//	window_size_unix.go    - No-Op (Terminal-Emulator verwaltet sein Fenster)
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00
package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Plausibilitätsgrenzen für gespeicherte Fenstergrößen
const (
	minWindowCols = 20
	maxWindowCols = 1000
	minWindowRows = 5
	maxWindowRows = 500
)

// windowSaveDelay - Wartezeit nach der letzten Größenänderung bis zum Speichern
const windowSaveDelay = time.Second

// windowPollInterval - Abstand der periodischen Fenstergrößen-Abfrage.
// Nötig, weil Bubbletea unter Windows nur Änderungen der Puffergröße meldet:
// Bei großem Scrollback-Puffer kommt eine reine Höhenänderung nie an.
const windowPollInterval = 2 * time.Second

// windowPollMsg löst die periodische Fenstergrößen-Abfrage aus.
type windowPollMsg struct{}

// windowSaveMsg wird windowSaveDelay nach einer Größenänderung zugestellt.
// Gespeichert wird nur, wenn seq noch der letzten Änderung entspricht.
type windowSaveMsg struct {
	seq int
}

// queryWindowState liefert Spalten, Zeilen und ob das Fenster "normal" ist
// (nicht maximiert/Vollbild/minimiert). Funktionszeiger für Tests.
var queryWindowState = getConsoleWindowState

// setWindowSize setzt die Fenstergröße in Zeichen. Funktionszeiger für Tests.
var setWindowSize = setConsoleWindowSize

// validWindowSize prüft eine Fenstergröße auf Plausibilität.
//
// @param cols - Spalten
// @param rows - Zeilen
// @return bool - true wenn speicher-/anwendbar
// @date   2026-10-16 10:00
func validWindowSize(cols, rows int) bool {
	return cols >= minWindowCols && cols <= maxWindowCols &&
		rows >= minWindowRows && rows <= maxWindowRows
}

// persistWindowSize speichert die Fenstergröße in der Config, wenn das
// Fenster normal (nicht maximiert/Vollbild) ist und sich die Größe geändert hat.
//
// @param configPath - Pfad zur connections.json
// @param cols - Spalten
// @param rows - Zeilen
// @param normal - false bei maximiertem/Vollbild-/minimiertem Fenster
// @return bool - true wenn gespeichert wurde
// @date   2026-10-16 10:00
func persistWindowSize(configPath string, cols, rows int, normal bool) bool {
	if !normal || !validWindowSize(cols, rows) {
		return false
	}
	cfg, err := LoadConfig(configPath)
	if err != nil || (cfg.WindowCols == cols && cfg.WindowRows == rows) {
		return false
	}
	cfg.WindowCols = cols
	cfg.WindowRows = rows
	return SaveConfig(configPath, cfg) == nil
}

// applyStartupWindowSize stellt die gespeicherte Fenstergröße beim Start
// wieder her. Ungültige oder fehlende Werte (0) lassen das Fenster unverändert.
//
// @param cols - Gespeicherte Spalten
// @param rows - Gespeicherte Zeilen
// @date   2026-10-16 10:00
func applyStartupWindowSize(cols, rows int) {
	if !validWindowSize(cols, rows) {
		return
	}
	setWindowSize(cols, rows)
}

// scheduleWindowSave plant das verzögerte Speichern der Fenstergröße.
// Jede Größenänderung erhöht die laufende Nummer; nur das Ereignis der
// letzten Änderung speichert tatsächlich (Entprellen).
//
// @return tea.Model - Modell mit neuer laufender Nummer
// @return tea.Cmd - Verzögertes windowSaveMsg
// @date   2026-10-16 10:00
func (m AppModel) scheduleWindowSave() (tea.Model, tea.Cmd) {
	m.windowSaveSeq++
	seq := m.windowSaveSeq
	return m, tea.Tick(windowSaveDelay, func(time.Time) tea.Msg {
		return windowSaveMsg{seq: seq}
	})
}

// handleWindowSave speichert die Fenstergröße, sofern seit dem geplanten
// Speichern keine weitere Größenänderung kam.
//
// @param msg - Verzögertes Speicher-Ereignis
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Immer nil
// @date   2026-10-16 10:00
func (m AppModel) handleWindowSave(msg windowSaveMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.windowSaveSeq {
		return m, nil
	}
	cols, rows, normal := queryWindowState()
	if persistWindowSize(m.configPath, cols, rows, normal) {
		m.configCache.Invalidate()
	}
	return m, nil
}

// windowPollCmd plant die nächste periodische Fenstergrößen-Abfrage.
//
// @return tea.Cmd - Verzögertes windowPollMsg
// @date   2026-10-16 10:00
func windowPollCmd() tea.Cmd {
	return tea.Tick(windowPollInterval, func(time.Time) tea.Msg {
		return windowPollMsg{}
	})
}

// handleWindowPoll speichert die aktuelle Fenstergröße (nur wenn normal und
// geändert) und plant die nächste Abfrage. Unter Unix liefert
// queryWindowState immer "nicht normal" - dann passiert nichts außer dem
// nächsten Tick.
//
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Nächste Abfrage
// @date   2026-10-16 10:00
func (m AppModel) handleWindowPoll() (tea.Model, tea.Cmd) {
	cols, rows, normal := queryWindowState()
	if persistWindowSize(m.configPath, cols, rows, normal) {
		m.configCache.Invalidate()
	}
	return m, windowPollCmd()
}
