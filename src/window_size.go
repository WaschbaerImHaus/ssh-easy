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

// fixHorizontalScroll entfernt einen waagrechten Bildlauf der Konsole
// (Puffer breiter als das Fenster). Funktionszeiger für Tests.
var fixHorizontalScroll = removeHorizontalScroll

// hscrollAction ist die Korrektur eines waagrechten Bildlaufbalkens.
type hscrollAction int

const (
	// hscrollNone - nichts zu tun (oder Bremse aktiv)
	hscrollNone hscrollAction = iota
	// hscrollShrinkBuffer - Puffer ist breiter als das Fenster: verkleinern
	hscrollShrinkBuffer
	// hscrollNudge - Scheinbalken: Fensterbreite kurz neu setzen
	hscrollNudge
)

const (
	// hscrollNudgeGap - Mindestabstand zwischen zwei Versuchen "Anstupsen"
	hscrollNudgeGap = 2 * time.Second
	// hscrollMaxNudges - so viele erfolglose Versuche in Folge, dann Ruhe
	hscrollMaxNudges = 3
)

// hscrollGuard merkt sich die Versuche gegen einen Scheinbalken. Ein
// Scheinbalken ist sichtbar, obwohl Puffer und Fenster gleich breit sind -
// er lässt sich nicht scrollen, verdeckt aber die letzte Zeile (vermutlich
// ein Pixel-Überstand des Fensterrahmens nach Schrift-/Monitorwechsel).
type hscrollGuard struct {
	// Erfolglose Versuche in Folge
	attempts int
	// Zeitpunkt des letzten Versuchs
	last time.Time
}

// hscrollLog schreibt Korrekturen ins Log (setzt main). Standard: nichts.
var hscrollLog = func(format string, args ...interface{}) {}

// decide entscheidet, was gegen einen waagrechten Bildlaufbalken zu tun ist.
// Ein zu breiter Puffer wird immer verkleinert (das beendet sich selbst);
// gegen einen Scheinbalken wird höchstens alle hscrollNudgeGap und nur
// hscrollMaxNudges-mal in Folge angestupst, damit nichts flackert.
//
// @param now - aktuelle Zeit
// @param bufWidth - Pufferbreite in Zeichen
// @param winLeft - linke sichtbare Spalte
// @param winRight - rechte sichtbare Spalte
// @param barVisible - zeigt das Fenster einen waagrechten Balken?
// @return int - Fensterbreite in Zeichen
// @return hscrollAction - Korrektur
// @date   2026-10-23 19:34
func (g *hscrollGuard) decide(now time.Time, bufWidth, winLeft, winRight int, barVisible bool) (int, hscrollAction) {
	if width, needed := horizontalScrollFix(bufWidth, winLeft, winRight); needed {
		return width, hscrollShrinkBuffer
	}
	width := winRight - winLeft + 1
	if !barVisible || width < 2 {
		g.attempts = 0
		return width, hscrollNone
	}
	if g.attempts >= hscrollMaxNudges || (!g.last.IsZero() && now.Sub(g.last) < hscrollNudgeGap) {
		return width, hscrollNone
	}
	g.attempts++
	g.last = now
	return width, hscrollNudge
}

// horizontalScrollFix berechnet, ob die Konsole waagrecht scrollen kann und
// wie das behoben wird. Windows zeigt unten einen Bildlaufbalken, sobald der
// Bildschirmpuffer breiter ist als das sichtbare Fenster - der Balken
// verdeckt dann die letzte Zeile. Das passiert spontan, etwa wenn das
// Fenster nach einer Schriftänderung (Strg+Mausrad), einem Monitorwechsel
// oder dem Andocken in Pixeln knapp eine Spalte zu schmal ist. Abhilfe:
// Fenster an Spalte 0 schieben und den Puffer auf Fensterbreite verkleinern.
//
// @param bufWidth - Breite des Bildschirmpuffers in Zeichen
// @param winLeft - linke sichtbare Spalte
// @param winRight - rechte sichtbare Spalte
// @return int - neue Pufferbreite (= Fensterbreite)
// @return bool - true wenn korrigiert werden muss
// @date   2026-10-23 19:32
func horizontalScrollFix(bufWidth, winLeft, winRight int) (int, bool) {
	width := winRight - winLeft + 1
	if width < 1 || bufWidth < 1 {
		return 0, false
	}
	if bufWidth <= width && winLeft == 0 {
		return width, false
	}
	return width, true
}

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
	saved := false
	err := updateConfig(configPath, func(cfg *AppConfig) bool {
		if cfg.WindowCols == cols && cfg.WindowRows == rows {
			return false
		}
		cfg.WindowCols = cols
		cfg.WindowRows = rows
		saved = true
		return true
	})
	return saved && err == nil
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

// handleWindowPoll entfernt einen waagrechten Bildlauf, speichert die
// aktuelle Fenstergröße (nur wenn normal und geändert) und die Schriftgröße
// (auch nach Strg+Mausrad, das an der Anwendung vorbeiläuft) und plant die
// nächste Abfrage. Unter Unix liefern beide Abfragen "unbekannt" - dann
// passiert nichts außer dem nächsten Tick.
//
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Nächste Abfrage
// @date   2026-10-27 17:20
func (m AppModel) handleWindowPoll() (tea.Model, tea.Cmd) {
	// Waagrechten Bildlauf (Balken verdeckt die letzte Zeile) sofort beheben
	fixHorizontalScroll()
	if persistConsoleState(m.configPath, m.consoleDefaults) {
		m.configCache.Invalidate()
	}
	return m, windowPollCmd()
}
