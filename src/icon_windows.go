// Windows-spezifische Konsolen- und Icon-Einrichtung für ssh-easy.
//
// WARUM GUI-SUBSYSTEM?
// ====================
// SSH-easy wird mit "-H windowsgui" (GUI-Subsystem) für Windows gebaut.
// Der entscheidende Unterschied:
//
//   Console-Subsystem: Windows erstellt automatisch ein Konsolenfenster,
//   das NICHT dem ssh-easy Prozess gehört, sondern dem Terminal-Host
//   (conhost.exe oder Windows Terminal). Das Taskleisten-Icon gehört dann
//   dem Terminal-Host → immer cmd-Icon, egal was man versucht.
//
//   GUI-Subsystem: Windows erstellt KEINE Konsole. Wir rufen AllocConsole()
//   selbst auf. Das Konsolenfenster gehört dann direkt dem ssh-easy Prozess
//   → das EXE-Icon erscheint korrekt in Titelleiste und Taskleiste.
//
// ABLAUF IN init():
//   1. AllocConsole()  → eigenes Konsolenfenster erstellen
//   2. Stdio umleiten  → os.Stdin/Stdout/Stderr auf neue Konsole zeigen
//   3. AUMID setzen    → eindeutige App-ID für Taskleisten-Gruppierung
//   4. Icon setzen     → WM_SETICON auf eigenes Konsolenfenster
//
// @author Kurt Ingwer
// @date   2026-04-07

//go:build windows

package main

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	// kernel32 – Windows-Kern-DLL
	kernel32Win = windows.NewLazySystemDLL("kernel32.dll")
	// user32 – Windows-UI-DLL
	user32Win = windows.NewLazySystemDLL("user32.dll")
	// shell32 – Windows-Shell-DLL
	shell32Win = windows.NewLazySystemDLL("shell32.dll")

	// allocConsole erstellt ein neues Konsolenfenster für den aktuellen Prozess.
	procAllocConsole = kernel32Win.NewProc("AllocConsole")
	// getConsoleWindow gibt das HWND des eigenen Konsolenfensters zurück.
	procGetConsoleWindow = kernel32Win.NewProc("GetConsoleWindow")
	// getModuleHandleW gibt die HINSTANCE der eigenen EXE zurück.
	procGetModuleHandleW = kernel32Win.NewProc("GetModuleHandleW")
	// setConsoleTitleW setzt den Fenstertitel des Konsolenfensters.
	procSetConsoleTitleW = kernel32Win.NewProc("SetConsoleTitleW")
	// loadImageW lädt ein Icon aus Ressourcen.
	procLoadImageW = user32Win.NewProc("LoadImageW")
	// sendMessageW sendet eine Nachricht an ein Fenster.
	procSendMessageW = user32Win.NewProc("SendMessageW")
	// setCurrentProcessExplicitAppUserModelID setzt die App-User-Model-ID.
	procSetCurrentProcessExplicitAppUserModelID = shell32Win.NewProc("SetCurrentProcessExplicitAppUserModelID")
)

const (
	// IMAGE_ICON – Typ-Konstante für LoadImage
	imageIcon = 1
	// LR_DEFAULTSIZE – Systemstandardgröße für Icons
	lrDefaultSize = 0x00000040
	// WM_SETICON – Fenster-Icon setzen
	wmSetIcon = 0x0080
	// ICON_SMALL – kleines Icon (16×16, Titelleiste + Taskleiste)
	iconSmall = 0
	// ICON_BIG – großes Icon (32×32, Alt+Tab)
	iconBig = 1
)

// allocOwnConsole erstellt ein neues Konsolenfenster das diesem Prozess gehört.
//
// Mit GUI-Subsystem (-H windowsgui) erstellt Windows beim Start KEINE Konsole.
// AllocConsole() erstellt eine neue Konsole – der entscheidende Unterschied:
// Das Fenster gehört ssh-easy.exe, nicht dem Terminal-Host (conhost/wt.exe).
// Dadurch zeigt die Taskleiste das EXE-Icon statt des cmd-Icons.
//
// Nach AllocConsole müssen stdin/stdout/stderr auf die neue Konsole umgeleitet
// werden, damit Bubbletea und alle I/O-Operationen korrekt funktionieren.
//
// @return bool - true wenn eine neue Konsole erstellt wurde
// @date 2026-04-07
func allocOwnConsole() bool {
	// Prüfen ob bereits eine Konsole vorhanden (sollte bei GUI-Subsystem nicht sein)
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd != 0 {
		// Bereits eine Konsole vorhanden – nichts tun
		return false
	}

	// Neue Konsole erstellen; Rückgabe 0 = Fehler (bereits eine Konsole oder Fehler)
	ret, _, _ := procAllocConsole.Call()
	if ret == 0 {
		return false
	}

	// --- stdin umleiten ---
	// CONIN$ ist der logische Name für den Console-Input-Puffer
	conin, err := windows.CreateFile(
		windows.StringToUTF16Ptr("CONIN$"),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		0, 0,
	)
	if err != nil {
		return false
	}

	// --- stdout und stderr umleiten ---
	// CONOUT$ ist der logische Name für den Console-Output-Puffer
	conout, err := windows.CreateFile(
		windows.StringToUTF16Ptr("CONOUT$"),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0, 0,
	)
	if err != nil {
		return false
	}

	// Windows-Kernel-Standardhandles auf neue Konsole setzen
	// (werden von Windows-APIs wie GetStdHandle() verwendet)
	_ = windows.SetStdHandle(windows.STD_INPUT_HANDLE, conin)
	_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, conout)
	_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, conout)

	// Go's os.Std* aktualisieren – Bubbletea liest direkt von os.Stdin/os.Stdout
	// Diese müssen nach AllocConsole auf die neuen Handles zeigen
	os.Stdin = os.NewFile(uintptr(conin), "stdin")
	os.Stdout = os.NewFile(uintptr(conout), "stdout")
	os.Stderr = os.NewFile(uintptr(conout), "stderr")

	// Fenstertitel setzen (sonst zeigt Windows den EXE-Pfad)
	title, _ := windows.UTF16PtrFromString("ssh-easy")
	procSetConsoleTitleW.Call(uintptr(unsafe.Pointer(title)))

	return true
}

// setAppUserModelID weist dem Prozess eine eindeutige App-User-Model-ID zu.
//
// Stellt sicher dass Windows die Taskleisten-Gruppe korrekt der App zuordnet
// und nicht mehrere Instanzen unter verschiedenen Einträgen zeigt.
//
// @date 2026-04-07
func setAppUserModelID() {
	appID, err := windows.UTF16PtrFromString("KurtIngwer.ssh-easy")
	if err != nil {
		return
	}
	procSetCurrentProcessExplicitAppUserModelID.Call(uintptr(unsafe.Pointer(appID)))
}

// applyConsoleIcon setzt das in der EXE eingebettete Icon auf das Konsolenfenster.
//
// Da AllocConsole bereits aufgerufen wurde, gehört das Fenster unserem Prozess.
// WM_SETICON wirkt sich jetzt korrekt auf Titelleiste und Taskleiste aus.
//
// goversioninfo legt das Icon unter Ressourcen-ID 1 ab.
//
// @date 2026-04-07
func applyConsoleIcon() {
	// HWND des eigenen Konsolenfensters
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return
	}

	// HINSTANCE der eigenen EXE
	hInst, _, _ := procGetModuleHandleW.Call(0)
	if hInst == 0 {
		return
	}

	// Kleines Icon laden: 16×16 Pixel (Titelleiste, Taskleiste)
	// uintptr(1) = MAKEINTRESOURCE(1): goversioninfo Icon-Ressourcen-ID
	smallIcon, _, _ := procLoadImageW.Call(
		hInst, 1, imageIcon,
		16, 16, 0,
	)

	// Großes Icon laden: 32×32 Pixel (Alt+Tab-Dialog)
	bigIcon, _, _ := procLoadImageW.Call(
		hInst, 1, imageIcon,
		32, 32, 0,
	)

	// Icon per WM_SETICON auf das Konsolenfenster setzen
	// Funktioniert jetzt korrekt weil das Fenster unserem Prozess gehört
	if smallIcon != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, smallIcon)
	}
	if bigIcon != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconBig, bigIcon)
	}
}

// init läuft automatisch beim Programmstart vor main().
//
// Reihenfolge ist kritisch:
//  1. AllocConsole   – eigenes Konsolenfenster erstellen (GUI-Subsystem)
//  2. stdio umleiten – Bubbletea braucht gültige os.Stdin/Stdout nach AllocConsole
//  3. AUMID setzen   – App-Identität für Taskleiste
//  4. Icon setzen    – EXE-Icon auf eigenes Konsolenfenster
//
// @date 2026-04-07
func init() {
	allocOwnConsole()
	setAppUserModelID()
	applyConsoleIcon()
}
