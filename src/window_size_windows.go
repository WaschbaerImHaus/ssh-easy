// Fenstergröße der Windows-Konsole lesen und setzen.
//
// Die Größe wird in Zeichen (Spalten × Zeilen) des sichtbaren Bereichs
// gemessen (srWindow aus GetConsoleScreenBufferInfo). Maximierte, im
// Vollbild befindliche oder minimierte Fenster gelten nicht als "normal" und
// werden nicht gespeichert.
//
// Beim Setzen muss der Bildschirmpuffer immer mindestens so groß sein wie
// das Fenster: erst Puffer verbreitern, dann Fenster setzen, dann
// Pufferbreite auf Fensterbreite reduzieren (kein waagrechter Bildlauf).
// Die Pufferhöhe (Scrollback) bleibt erhalten.
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00

//go:build windows

package main

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	// procIsZoomed - ist das Fenster maximiert?
	procIsZoomed = user32Win.NewProc("IsZoomed")
	// procIsIconic - ist das Fenster minimiert?
	procIsIconic = user32Win.NewProc("IsIconic")
	// procGetConsoleDisplayMode - Vollbildmodus der Konsole
	procGetConsoleDisplayMode = kernel32Win.NewProc("GetConsoleDisplayMode")
	// procSetConsoleScreenBufferSize - Größe des Bildschirmpuffers setzen
	procSetConsoleScreenBufferSize = kernel32Win.NewProc("SetConsoleScreenBufferSize")
	// procGetClassNameW - Fensterklasse (erkennt das ConPTY-Pseudo-Fenster)
	procGetClassNameW = user32Win.NewProc("GetClassNameW")
	// procGetLargestConsoleWindowSize - größtmögliches Fenster (in Zeichen)
	procGetLargestConsoleWindowSize = kernel32Win.NewProc("GetLargestConsoleWindowSize")
)

// consoleFullscreenMode - Flag von GetConsoleDisplayMode für Vollbild
const consoleFullscreenMode = 0x1

// getConsoleWindowState liefert die sichtbare Größe der Konsole und ob das
// Fenster normal (nicht maximiert/Vollbild/minimiert) ist.
//
// @return int - Spalten
// @return int - Zeilen
// @return bool - true wenn die Größe gespeichert werden darf
// @date   2026-10-16 10:00
func getConsoleWindowState() (int, int, bool) {
	if isPseudoConsoleHost() {
		return 0, 0, false
	}
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return 0, 0, false
	}
	if zoomed, _, _ := procIsZoomed.Call(hwnd); zoomed != 0 {
		return 0, 0, false
	}
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		return 0, 0, false
	}
	var displayMode uint32
	if ret, _, _ := procGetConsoleDisplayMode.Call(uintptr(unsafe.Pointer(&displayMode))); ret != 0 && displayMode&consoleFullscreenMode != 0 {
		return 0, 0, false
	}

	handle := windows.Handle(getStdoutHandle())
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(handle, &info); err != nil {
		return 0, 0, false
	}
	cols := int(info.Window.Right-info.Window.Left) + 1
	rows := int(info.Window.Bottom-info.Window.Top) + 1
	return cols, rows, true
}

// setConsoleWindowSize setzt die sichtbare Größe der Konsole.
// Werte größer als das größtmögliche Fenster (Bildschirm) werden begrenzt.
//
// @param cols - Gewünschte Spalten
// @param rows - Gewünschte Zeilen
// @return bool - true wenn die Größe gesetzt wurde
// @date   2026-10-16 10:00
func setConsoleWindowSize(cols, rows int) bool {
	if isPseudoConsoleHost() {
		return false
	}
	handle := windows.Handle(getStdoutHandle())
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(handle, &info); err != nil {
		return false
	}

	// Auf die Bildschirmgröße begrenzen (COORD im Rückgabewert: X low, Y high)
	if largest, _, _ := procGetLargestConsoleWindowSize.Call(uintptr(handle)); largest != 0 {
		maxCols := int(int16(largest & 0xFFFF))
		maxRows := int(int16((largest >> 16) & 0xFFFF))
		if maxCols > 0 && cols > maxCols {
			cols = maxCols
		}
		if maxRows > 0 && rows > maxRows {
			rows = maxRows
		}
	}

	bufHeight := int(info.Size.Y)
	if bufHeight < rows {
		bufHeight = rows
	}
	// Schritt 1: Puffer so breit/hoch machen, dass altes und neues Fenster passen
	tmpWidth := int(info.Size.X)
	if tmpWidth < cols {
		tmpWidth = cols
	}
	if !setScreenBufferSize(handle, tmpWidth, bufHeight) {
		return false
	}

	// Schritt 2: Fenster setzen - obere Zeile möglichst beibehalten
	top := int(info.Window.Top)
	if top+rows > bufHeight {
		top = bufHeight - rows
	}
	rect := windows.SmallRect{
		Left:   0,
		Top:    int16(top),
		Right:  int16(cols - 1),
		Bottom: int16(top + rows - 1),
	}
	if err := setConsoleWindowInfo(handle, true, &rect); err != nil {
		return false
	}

	// Schritt 3: Pufferbreite = Fensterbreite (kein waagrechter Bildlauf)
	if tmpWidth != cols {
		setScreenBufferSize(handle, cols, bufHeight)
	}
	return true
}

// setScreenBufferSize setzt die Größe des Konsolen-Bildschirmpuffers.
//
// @param handle - Konsolen-Ausgabe-Handle
// @param width - Breite in Zeichen
// @param height - Höhe in Zeilen
// @return bool - true bei Erfolg
// @date   2026-10-16 10:00
func setScreenBufferSize(handle windows.Handle, width, height int) bool {
	// COORD wird als ein 32-Bit-Wert übergeben: X im unteren, Y im oberen Wort
	coord := uintptr(uint16(width)) | uintptr(uint16(height))<<16
	ret, _, _ := procSetConsoleScreenBufferSize.Call(uintptr(handle), coord)
	return ret != 0
}

// isPseudoConsoleHost erkennt Windows Terminal (ConPTY). Dort liefert
// GetConsoleWindow nur ein unsichtbares Pseudo-Fenster: IsZoomed ist immer 0
// und die Konsolen-API ändert nur den Puffer, nicht das sichtbare Fenster.
// Windows Terminal merkt sich seine Fenstergröße selbst.
//
// @return bool - true wenn ssh-easy in Windows Terminal läuft
// @date   2026-10-16 10:00
func isPseudoConsoleHost() bool {
	if os.Getenv("WT_SESSION") != "" {
		return true
	}
	// Ohne WT_SESSION (z.B. Windows 11 übergibt die per AllocConsole erzeugte
	// Konsole an Windows Terminal): Fensterklasse des Konsolenfensters prüfen
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return false
	}
	buf := make([]uint16, 64)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return n > 0 && windows.UTF16ToString(buf[:n]) == "PseudoConsoleWindow"
}
