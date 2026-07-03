// Paket main - Konsolen-Schriftgröße (plattformneutrale Logik)
//
// Enthält die plattformunabhängigen Teile der Schriftgrößen-Steuerung:
// Grenzwerte und Clamping. Die eigentliche Font-API ist plattformspezifisch:
//   console_font_windows.go - SetCurrentConsoleFontEx (wirksam)
//   console_font_unix.go    - No-Op (Terminal-Emulator bestimmt die Schrift)
//
// @author Kurt Ingwer
// @date   2026-07-03 19:40
package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// Grenzwerte für die Konsolen-Schriftgröße (in Pixeln, Fonthöhe)
const (
	// MinFontSize - kleinste erlaubte Schriftgröße
	MinFontSize = 8
	// MaxFontSize - größte erlaubte Schriftgröße
	MaxFontSize = 72
	// FontSizeStep - Schrittweite für Strg+Plus/Strg+Minus
	FontSizeStep = 2
)

// clampFontSize begrenzt eine Schriftgröße auf den erlaubten Bereich.
//
// @param size - Gewünschte Schriftgröße
// @return int - Auf [MinFontSize, MaxFontSize] begrenzte Größe
// @date   2026-07-03 19:40
func clampFontSize(size int) int {
	if size < MinFontSize {
		return MinFontSize
	}
	if size > MaxFontSize {
		return MaxFontSize
	}
	return size
}

// fontSizeChangedText liefert die Erfolgsmeldung für Schriftgrößen-Änderung.
// Fallback auf Englisch: die beiden FontSize-Strings sind nur in DE/EN/FR
// gepflegt - bei den übrigen Sprachen wäre das Feld leer.
//
// @return string - Format-String mit %d für die neue Größe
// @date   2026-07-03 19:45
func (t Translations) fontSizeChangedText() string {
	if t.FontSizeChanged != "" {
		return t.FontSizeChanged
	}
	return "Font size: %d"
}

// fontSizeUnsupportedText liefert den Hinweis für nicht unterstützte
// Plattformen (Linux/macOS). Fallback auf Englisch, siehe oben.
//
// @return string - Hinweistext
// @date   2026-07-03 19:45
func (t Translations) fontSizeUnsupportedText() string {
	if t.FontSizeUnsupported != "" {
		return t.FontSizeUnsupported
	}
	return "Font size can only be changed on Windows (elsewhere: use terminal settings)"
}

// applyStartupFontSize wendet eine persistierte Schriftgröße beim
// Programmstart an. 0 oder negative Werte bedeuten "Systemstandard" -
// dann bleibt die Konsole unverändert. Nur unter Windows wirksam.
//
// @param size - Gespeicherte Schriftgröße aus AppConfig.FontSize
// @date   2026-07-03 19:45
func applyStartupFontSize(size int) {
	if size <= 0 {
		return
	}
	setConsoleFontSize(clampFontSize(size))
}

// persistFontSize speichert eine Schriftgröße in der Config, falls sie sich
// vom gespeicherten Wert unterscheidet. Wird beim Programmende aufgerufen,
// damit auch per Strg+Mausrad (nativer conhost-Zoom) geänderte Größen den
// Neustart überleben - dieser Zoom läuft an der Anwendung vorbei und wäre
// sonst beim nächsten Start wieder weg.
//
// @param configPath - Pfad zur connections.json
// @param size - Aktuelle Fonthöhe (0 = nicht ermittelbar, wird ignoriert)
// @return bool - true wenn gespeichert wurde
// @date   2026-07-03 20:40
func persistFontSize(configPath string, size int) bool {
	if size <= 0 {
		return false
	}
	cfg, err := LoadConfig(configPath)
	if err != nil || cfg.FontSize == size {
		return false
	}
	cfg.FontSize = size
	return SaveConfig(configPath, cfg) == nil
}

// changeFontSize ändert die Konsolen-Schriftgröße um delta Pixel und
// persistiert den neuen Wert in der Config (font_size).
// Auf Plattformen ohne Font-Steuerung (Linux/macOS) erscheint nur ein Hinweis.
//
// @param delta - Änderung in Pixeln (+FontSizeStep / -FontSizeStep)
// @return tea.Model - Aktualisiertes Modell
// @return tea.Cmd - Immer nil
// @date   2026-07-03 19:45
func (m AppModel) changeFontSize(delta int) (tea.Model, tea.Cmd) {
	current := getConsoleFontSize()
	if current <= 0 {
		// Plattform kann die Schriftgröße nicht steuern (z.B. Linux)
		m.errorMsg = m.lang.fontSizeUnsupportedText()
		m.successMsg = ""
		return m, nil
	}

	newSize := clampFontSize(current + delta)
	if newSize == current {
		// Bereits am Limit - nichts zu tun
		return m, nil
	}
	if !setConsoleFontSize(newSize) {
		return m, nil
	}

	m.successMsg = fmt.Sprintf(m.lang.fontSizeChangedText(), newSize)
	m.errorMsg = ""

	// Neuen Wert persistieren, damit er beim nächsten Start angewendet wird
	if cfg, err := LoadConfig(m.configPath); err == nil {
		cfg.FontSize = newSize
		if err := SaveConfig(m.configPath, cfg); err == nil {
			m.configCache.Invalidate()
		}
	}

	return m, nil
}
