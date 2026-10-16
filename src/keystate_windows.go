// Plattform-spezifische Abfrage des Tastenzustands für Windows.
//
// Wird für Shift+Einfg in Formularfeldern gebraucht: Bubbletea liefert die
// Einfg-Taste unter Windows ohne Modifier als Rune 0x00.
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00

//go:build windows

package main

// procGetAsyncKeyState liefert den aktuellen Zustand einer Taste.
var procGetAsyncKeyState = user32Win.NewProc("GetAsyncKeyState")

// isKeyPressed prüft ob eine Taste gerade gedrückt ist.
// Bit 0x8000 = "jetzt gedrückt"; Bit 0x0001 = "seit der letzten Abfrage
// gedrückt worden" (fängt sehr kurze Tastendrücke ab, die beim Verarbeiten
// des Ereignisses schon wieder losgelassen sind).
//
// @param vk - Virtueller Tastencode
// @return bool - true wenn gedrückt
// @date   2026-10-16 10:00
func isKeyPressed(vk int) bool {
	state, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return state&0x8001 != 0
}
