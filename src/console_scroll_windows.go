// Paket main - Konsolen-Scroll-Snap (Windows-Variante)
//
// Problem: Scrollt der Nutzer im SSH-Terminal mit dem Mausrad nach oben
// (Konsolen-Scrollback) und tippt dann, bleibt der Windows-Konsolen-Viewport
// an der gescrollten Position stehen - man tippt "blind". PuTTY springt in
// diesem Fall automatisch zur Eingabezeile zurück ("reset scrollback on
// keypress").
//
// Lösung: Bei jeder Tastatureingabe im stdin-Forwarder wird der Viewport
// per SetConsoleWindowInfo so verschoben, dass die Cursor-Zeile (dort
// erscheint die Eingabe) wieder sichtbar ist.
//
// @author Kurt Ingwer
// @date   2026-07-03 19:35

//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// procSetConsoleWindowInfo verschiebt das sichtbare Fenster im Konsolen-Puffer.
// Nicht in golang.org/x/sys/windows als Wrapper vorhanden, daher direkter Proc.
var procSetConsoleWindowInfo = kernel32Win.NewProc("SetConsoleWindowInfo")

// snapConsoleToBottom scrollt den Konsolen-Viewport zur Cursor-Zeile zurück.
//
// Ablauf:
//  1. GetConsoleScreenBufferInfo liefert Cursor-Position und aktuelles
//     Sichtfenster (srWindow) im Puffer
//  2. Liegt der Cursor unterhalb des Sichtfensters (Nutzer hat hochgescrollt),
//     wird das Fenster per SetConsoleWindowInfo so verschoben, dass die
//     Cursor-Zeile die unterste sichtbare Zeile ist
//
// Fehler werden still ignoriert - Scroll-Snap ist Komfort, kein Muss.
//
// @date   2026-07-03 19:35
func snapConsoleToBottom() {
	handle := windows.Handle(getStdoutHandle())
	if handle == windows.InvalidHandle || handle == 0 {
		return
	}

	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(handle, &info); err != nil {
		return
	}

	// Cursor bereits im sichtbaren Bereich? Dann ist nichts zu tun.
	if info.CursorPosition.Y >= info.Window.Top && info.CursorPosition.Y <= info.Window.Bottom {
		return
	}

	// Fensterhöhe beibehalten, Fenster so verschieben dass die Cursor-Zeile
	// die unterste sichtbare Zeile wird (wie "ans Ende springen").
	height := info.Window.Bottom - info.Window.Top
	newBottom := info.CursorPosition.Y
	newTop := newBottom - height
	if newTop < 0 {
		newTop = 0
		newBottom = height
	}

	rect := windows.SmallRect{
		Top:    newTop,
		Bottom: newBottom,
		Left:   info.Window.Left,
		Right:  info.Window.Right,
	}
	// true = absolute Koordinaten (nicht relativ zum aktuellen Fenster)
	_ = setConsoleWindowInfo(handle, true, &rect)
}

// setConsoleWindowInfo ruft die Win32-API SetConsoleWindowInfo auf.
// golang.org/x/sys/windows bietet dafür keinen fertigen Wrapper.
//
// @param handle - Console-Output-Handle
// @param absolute - true = rect enthält absolute Pufferkoordinaten
// @param rect - Neues Sichtfenster
// @return error - Fehler des API-Aufrufs
// @date   2026-07-03 19:35
func setConsoleWindowInfo(handle windows.Handle, absolute bool, rect *windows.SmallRect) error {
	abs := uintptr(0)
	if absolute {
		abs = 1
	}
	ret, _, err := procSetConsoleWindowInfo.Call(uintptr(handle), abs, uintptr(unsafe.Pointer(rect)))
	if ret == 0 {
		return err
	}
	return nil
}

// getStdoutHandle liefert das aktuelle STD_OUTPUT_HANDLE des Prozesses.
// Nach AllocConsole zeigt dieses auf CONOUT$ der eigenen Konsole.
//
// @return uintptr - Handle oder 0 bei Fehler
// @date   2026-07-03 19:35
func getStdoutHandle() uintptr {
	h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return 0
	}
	return uintptr(h)
}
