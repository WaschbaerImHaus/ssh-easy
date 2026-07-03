// Paket main - Konsolen-Schriftgröße (Windows-Variante)
//
// Liest und setzt die Schriftgröße der eigenen Konsole (AllocConsole) über
// GetCurrentConsoleFontEx / SetCurrentConsoleFontEx. Die Änderung wirkt
// sofort - Windows löst dabei ein Fenster-Resize aus, das Bubbletea als
// WindowSizeMsg empfängt und die TUI neu rendert.
//
// @author Kurt Ingwer
// @date   2026-07-03 19:40

//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	// procGetCurrentConsoleFontEx liest die aktuelle Konsolen-Font-Info.
	procGetCurrentConsoleFontEx = kernel32Win.NewProc("GetCurrentConsoleFontEx")
	// procSetCurrentConsoleFontEx setzt die Konsolen-Font-Info.
	procSetCurrentConsoleFontEx = kernel32Win.NewProc("SetCurrentConsoleFontEx")
)

// consoleFontInfoEx entspricht der Win32-Struktur CONSOLE_FONT_INFOEX.
// https://learn.microsoft.com/en-us/windows/console/console-font-infoex
type consoleFontInfoEx struct {
	// Größe dieser Struktur in Bytes (muss vor jedem API-Aufruf gesetzt sein)
	cbSize uint32
	// Index in der Font-Tabelle der Konsole
	nFont uint32
	// Zeichenzellen-Größe: X = Breite, Y = Höhe (die "Schriftgröße")
	dwFontSizeX int16
	dwFontSizeY int16
	// Font-Familie (Bitmaske, z.B. TMPF_TRUETYPE)
	fontFamily uint32
	// Strichstärke (400 = normal, 700 = fett)
	fontWeight uint32
	// Font-Name (UTF-16, max. 32 Zeichen = LF_FACESIZE)
	faceName [32]uint16
}

// getConsoleFontSize liefert die aktuelle Fonthöhe der Konsole in Pixeln.
//
// @return int - Fonthöhe oder 0 bei Fehler
// @date   2026-07-03 19:40
func getConsoleFontSize() int {
	handle := windows.Handle(getStdoutHandle())
	if handle == windows.InvalidHandle || handle == 0 {
		return 0
	}

	var info consoleFontInfoEx
	info.cbSize = uint32(unsafe.Sizeof(info))
	// FALSE = Font des normalen Fensters (nicht Vollbild-Modus)
	ret, _, _ := procGetCurrentConsoleFontEx.Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return 0
	}
	return int(info.dwFontSizeY)
}

// setConsoleFontSize setzt die Fonthöhe der Konsole in Pixeln.
// Breite 0 lässt Windows das korrekte Seitenverhältnis selbst wählen.
// Font-Name und übrige Attribute bleiben unverändert (erst lesen, dann
// nur die Höhe ändern).
//
// @param size - Neue Fonthöhe in Pixeln (Aufrufer clampt vorher)
// @return bool - true wenn die Änderung übernommen wurde
// @date   2026-07-03 19:40
func setConsoleFontSize(size int) bool {
	handle := windows.Handle(getStdoutHandle())
	if handle == windows.InvalidHandle || handle == 0 {
		return false
	}

	// Aktuelle Font-Einstellungen lesen damit Name/Gewicht erhalten bleiben
	var info consoleFontInfoEx
	info.cbSize = uint32(unsafe.Sizeof(info))
	ret, _, _ := procGetCurrentConsoleFontEx.Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return false
	}

	// Nur die Höhe ändern; Breite 0 = Windows wählt passend zum Font
	info.dwFontSizeX = 0
	info.dwFontSizeY = int16(size)

	ret, _, _ = procSetCurrentConsoleFontEx.Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&info)))
	return ret != 0
}
