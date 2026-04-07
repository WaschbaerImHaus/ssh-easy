// Windows-spezifische Konsolenfenster-Icon-Einstellung für ssh-easy.
//
// Wenn ein Go-Programm als Konsolenanwendung läuft, zeigt Windows in der
// Taskleiste standardmäßig das Icon des Console-Hosts (conhost.exe / Windows
// Terminal) – nicht das in der EXE eingebettete Icon. Dieses Modul löst das
// Problem auf zwei Wegen:
//
//  1. SetCurrentProcessExplicitAppUserModelID – weist dem Prozess eine
//     eindeutige App-User-Model-ID zu, damit Windows den Taskleisteneintrag
//     als eigenständige Anwendung behandelt und das EXE-Icon verwendet.
//  2. WM_SETICON – setzt zusätzlich das Icon direkt auf das Konsolenfenster
//     (Titelleiste, Alt+Tab-Ansicht).
//
// Beide Maßnahmen zusammen sorgen dafür, dass das richtige Icon sowohl in der
// Taskleiste als auch im Titelbalken erscheint – unabhängig davon, ob die App
// als Administrator oder als normaler Nutzer gestartet wird.
//
// @author Kurt Ingwer
// @date   2026-04-07

//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	// kernel32 – Windows-Kern-DLL
	kernel32Win = windows.NewLazySystemDLL("kernel32.dll")
	// user32 – Windows-UI-DLL
	user32Win = windows.NewLazySystemDLL("user32.dll")
	// shell32 – Windows-Shell-DLL (enthält SetCurrentProcessExplicitAppUserModelID)
	shell32Win = windows.NewLazySystemDLL("shell32.dll")

	// getConsoleWindow gibt das HWND des aktuellen Konsolenfensters zurück.
	procGetConsoleWindow = kernel32Win.NewProc("GetConsoleWindow")
	// getModuleHandleW gibt das HINSTANCE-Handle der eigenen EXE zurück.
	procGetModuleHandleW = kernel32Win.NewProc("GetModuleHandleW")
	// loadImageW lädt ein Icon/Bitmap aus Ressourcen oder Datei.
	procLoadImageW = user32Win.NewProc("LoadImageW")
	// sendMessageW sendet eine Nachricht an ein Fenster-Handle.
	procSendMessageW = user32Win.NewProc("SendMessageW")
	// setCurrentProcessExplicitAppUserModelID setzt die App-User-Model-ID des
	// aktuellen Prozesses – notwendig damit Windows den Taskleisteneintrag der
	// App (statt dem Console-Host) zuordnet.
	procSetCurrentProcessExplicitAppUserModelID = shell32Win.NewProc("SetCurrentProcessExplicitAppUserModelID")
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

// setAppUserModelID weist dem aktuellen Prozess eine eindeutige
// App-User-Model-ID (AUMID) zu. Windows nutzt diese ID, um den
// Taskleisteneintrag der Anwendung zuzuordnen und das korrekte EXE-Icon
// anzuzeigen – auch wenn die App als normaler (nicht-Administrator) Nutzer
// gestartet wurde.
//
// Ohne AUMID gruppiert Windows Terminal / conhost.exe alle Konsolenprozesse
// unter ihrem eigenen Icon, weshalb das in der EXE eingebettete Icon in der
// Taskleiste unsichtbar bleibt.
//
// @date 2026-04-07
func setAppUserModelID() {
	// AUMID als UTF-16-String kodieren (Windows-API erwartet LPCWSTR)
	appID, err := windows.UTF16PtrFromString("KurtIngwer.ssh-easy")
	if err != nil {
		return
	}
	// Shell-API aufrufen; Rückgabewert (HRESULT) wird ignoriert – falls die
	// Funktion fehlschlägt (ältere Windows-Versionen), ist das nicht kritisch.
	procSetCurrentProcessExplicitAppUserModelID.Call(uintptr(unsafe.Pointer(appID)))
}

// setConsoleWindowIcon setzt das in der EXE eingebettete Icon als
// Taskleisten- und Titelleisten-Icon des Konsolenfensters.
//
// goversioninfo legt das Icon immer unter Ressourcen-ID 1 ab.
// GetModuleHandle(nil) liefert die HINSTANCE der laufenden EXE, sodass
// LoadImage das Icon direkt aus dem eigenen Prozess lädt – kein separater
// Datei-Zugriff notwendig.
//
// @date 2026-04-07
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

	// Kleines Icon laden (Taskleiste, Titelleiste) – 16×16 Pixel
	// MAKEINTRESOURCE(1) entspricht dem uintptr-Wert 1 (goversioninfo Icon-ID)
	smallIcon, _, _ := procLoadImageW.Call(
		hInst,
		1,         // MAKEINTRESOURCE(1) – Icon-Ressourcen-ID
		imageIcon, // IMAGE_ICON
		16, 16,    // Explizite Größe: 16×16 Pixel
		0,         // Keine zusätzlichen Flags bei expliziter Größe
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
// die App-User-Model-ID sowie das Fenster-Icon, bevor die TUI startet.
//
// Reihenfolge ist wichtig:
//  1. AUMID setzen – damit Windows den Taskleisteneintrag korrekt zuordnet
//  2. WM_SETICON senden – damit Titelleiste und Alt+Tab das richtige Icon zeigen
//
// @date 2026-04-07
func init() {
	setAppUserModelID()
	setConsoleWindowIcon()
}
