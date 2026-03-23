// Windows-spezifische Konsolenfenster-Icon-Einstellung für ssh-easy.
//
// Wenn ein Go-Programm als Konsolenanwendung läuft, zeigt Windows in der
// Taskleiste das Standard-Konsolen-Icon (cmd.exe / conhost.exe), nicht das
// in der EXE eingebettete Icon. Dieses Modul setzt das App-Icon beim Start
// per Windows-API direkt auf das Konsolenfenster-Handle.
//
// @author Kurt Ingwer
// @date   2026-03-23 10:00

//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

var (
	// kernel32 – Windows-Kern-DLL
	kernel32Win = windows.NewLazySystemDLL("kernel32.dll")
	// user32 – Windows-UI-DLL
	user32Win = windows.NewLazySystemDLL("user32.dll")

	// getConsoleWindow gibt das HWND des aktuellen Konsolenfensters zurück.
	procGetConsoleWindow = kernel32Win.NewProc("GetConsoleWindow")
	// getModuleHandleW gibt das HINSTANCE-Handle der eigenen EXE zurück.
	procGetModuleHandleW = kernel32Win.NewProc("GetModuleHandleW")
	// loadImageW lädt ein Icon/Bitmap aus Ressourcen oder Datei.
	procLoadImageW = user32Win.NewProc("LoadImageW")
	// sendMessageW sendet eine Nachricht an ein Fenster-Handle.
	procSendMessageW = user32Win.NewProc("SendMessageW")
)

const (
	// imageIcon – Typ-Konstante für LoadImage: Icon laden
	imageIcon = 1
	// lrDefaultSize – LoadImage-Flag: Systemgröße für Icons verwenden
	lrDefaultSize = 0x00000040
	// wmSetIcon – Windows-Nachricht zum Setzen des Fenster-Icons
	wmSetIcon = 0x0080
	// iconSmall – Index für kleines Icon (Taskleiste, Titelleiste)
	iconSmall = 0
	// iconBig – Index für großes Icon (Alt+Tab-Menü)
	iconBig = 1
)

// setConsoleWindowIcon setzt das in der EXE eingebettete Icon als
// Taskleisten- und Titelleisten-Icon des Konsolenfensters.
//
// goversioninfo legt das Icon immer unter Ressourcen-ID 1 ab.
// GetModuleHandle(nil) liefert die HINSTANCE der laufenden EXE, sodass
// LoadImage das Icon direkt aus dem eigenen Prozess lädt – kein separater
// Datei-Zugriff notwendig.
//
// @date 2026-03-23 10:00
func setConsoleWindowIcon() {
	// HWND des Konsolenfensters ermitteln (0 = kein Konsolenfenster)
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return
	}

	// HINSTANCE der eigenen EXE holen (Argument 0 = aktueller Prozess)
	hInst, _, _ := procGetModuleHandleW.Call(0)
	if hInst == 0 {
		return
	}

	// Kleines Icon laden (Taskleiste, Titelleiste) – Systemgröße via LR_DEFAULTSIZE
	// MAKEINTRESOURCE(1) entspricht dem uintptr-Wert 1 (goversioninfo Icon-ID)
	smallIcon, _, _ := procLoadImageW.Call(
		hInst,
		1,            // MAKEINTRESOURCE(1) – Icon-Ressourcen-ID
		imageIcon,    // IMAGE_ICON
		16, 16,       // Explizite Größe: 16×16 Pixel
		0,            // Keine zusätzlichen Flags bei expliziter Größe
	)

	// Großes Icon laden (Alt+Tab-Dialog) – 32×32 Pixel
	bigIcon, _, _ := procLoadImageW.Call(
		hInst,
		1,
		imageIcon,
		32, 32,
		0,
	)

	// Icons per WM_SETICON auf das Konsolenfenster anwenden
	if smallIcon != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, smallIcon)
	}
	if bigIcon != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconBig, bigIcon)
	}
}

// init wird automatisch beim Programmstart aufgerufen und setzt sofort
// das App-Icon, bevor die TUI startet.
//
// @date 2026-03-23 10:00
func init() {
	setConsoleWindowIcon()
}
