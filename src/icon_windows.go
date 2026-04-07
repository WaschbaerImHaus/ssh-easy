// Windows-spezifische Icon-Einstellung für ssh-easy.
//
// Warum drei verschiedene Methoden?
//
//  1. SetCurrentProcessExplicitAppUserModelID
//     Weist dem Prozess eine eindeutige App-ID zu, damit Windows die
//     Taskleisten-Gruppe korrekt der App (nicht dem Console-Host) zuordnet.
//     Ohne diese ID gruppiert Windows Terminal / conhost den Prozess unter
//     dem Host-Icon.
//
//  2. SetConsoleIcon  (undokumentiert, stabil seit Windows Vista)
//     Setzt das Icon für die gesamte Konsole auf Kernel-Ebene – ändert
//     sowohl Titelleiste als auch den Taskleisteneintrag des Console-Hosts.
//     Das ist die einzige API, die das Taskleisten-Icon für normale Nutzer
//     (ohne Admin-Rechte) zuverlässig ändert.
//
//  3. WM_SETICON
//     Setzt das Icon direkt auf das Konsolenfenster-Handle – Fallback und
//     Ergänzung zu SetConsoleIcon für ältere Windows-Versionen.
//
// Alle drei Methoden werden beim Programmstart aufgerufen (init).
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
	// shell32 – Windows-Shell-DLL
	shell32Win = windows.NewLazySystemDLL("shell32.dll")

	// getConsoleWindow gibt das HWND des aktuellen Konsolenfensters zurück.
	procGetConsoleWindow = kernel32Win.NewProc("GetConsoleWindow")
	// getModuleHandleW gibt das HINSTANCE-Handle der eigenen EXE zurück.
	procGetModuleHandleW = kernel32Win.NewProc("GetModuleHandleW")
	// loadImageW lädt ein Icon aus Ressourcen oder Datei.
	procLoadImageW = user32Win.NewProc("LoadImageW")
	// sendMessageW sendet eine Nachricht an ein Fenster-Handle.
	procSendMessageW = user32Win.NewProc("SendMessageW")
	// setConsoleIcon – undokumentierte kernel32-API, setzt das Icon für
	// die gesamte Konsole auf OS-Ebene (Titelleiste + Taskleiste).
	procSetConsoleIcon = kernel32Win.NewProc("SetConsoleIcon")
	// setCurrentProcessExplicitAppUserModelID setzt die App-User-Model-ID
	// damit Windows den Taskleisteneintrag korrekt zuordnet.
	procSetCurrentProcessExplicitAppUserModelID = shell32Win.NewProc("SetCurrentProcessExplicitAppUserModelID")
)

const (
	// imageIcon – Typ-Konstante für LoadImage: Icon laden
	imageIcon = 1
	// lrDefaultSize – LoadImage-Flag: Systemstandardgröße verwenden
	lrDefaultSize = 0x00000040
	// wmSetIcon – Windows-Nachricht zum Setzen des Fenster-Icons
	wmSetIcon = 0x0080
	// iconSmall – Index für kleines Icon (16×16, Titelleiste + Taskleiste)
	iconSmall = 0
	// iconBig – Index für großes Icon (32×32, Alt+Tab)
	iconBig = 1
)

// setAppUserModelID weist dem Prozess eine eindeutige App-User-Model-ID zu.
//
// Ohne diese ID ordnet Windows den Taskleisteneintrag dem Console-Host
// (conhost.exe / Windows Terminal) zu statt der eigenen App. Das führt dazu,
// dass das EXE-Icon in der Taskleiste nie angezeigt wird.
//
// @date 2026-04-07
func setAppUserModelID() {
	appID, err := windows.UTF16PtrFromString("KurtIngwer.ssh-easy")
	if err != nil {
		return
	}
	procSetCurrentProcessExplicitAppUserModelID.Call(uintptr(unsafe.Pointer(appID)))
}

// applyConsoleIcon setzt das in der EXE eingebettete Icon auf Kernel-Ebene
// (SetConsoleIcon) sowie als WM_SETICON-Fensternachricht.
//
// SetConsoleIcon ist die einzige API, die das Taskleisten-Icon für normale
// Windows-Nutzer (ohne Admin-Rechte) zuverlässig ändert. WM_SETICON ist
// ein Fallback und wirkt auf Titelleiste und Alt+Tab-Dialog.
//
// goversioninfo legt das Icon unter Ressourcen-ID 1 ab.
//
// @date 2026-04-07
func applyConsoleIcon() {
	// HINSTANCE der eigenen EXE holen (NULL = aktueller Prozess)
	hInst, _, _ := procGetModuleHandleW.Call(0)
	if hInst == 0 {
		return
	}

	// --- SetConsoleIcon (Methode 1: OS-Ebene, Titelleiste + Taskleiste) ---
	// Icon mit Standardgröße laden – Windows wählt die passende Auflösung
	// MAKEINTRESOURCE(1) == uintptr(1): goversioninfo Icon-Ressourcen-ID
	hIconDefault, _, _ := procLoadImageW.Call(
		hInst,
		1,             // MAKEINTRESOURCE(1) – goversioninfo Icon-ID
		imageIcon,     // IMAGE_ICON
		0, 0,          // Breite/Höhe = 0 → Systemstandard (SM_CXICON)
		lrDefaultSize, // LR_DEFAULTSIZE: Systemgröße verwenden
	)
	if hIconDefault != 0 {
		// SetConsoleIcon setzt das Icon für die gesamte Konsole auf OS-Ebene.
		// Rückgabewert wird ignoriert – bei Fehler greift WM_SETICON als Fallback.
		procSetConsoleIcon.Call(hIconDefault)
	}

	// --- WM_SETICON (Methode 2: Fensternachricht, Titelleiste + Alt+Tab) ---
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return
	}

	// Kleines Icon: 16×16 Pixel (Titelleiste, Taskleiste)
	smallIcon, _, _ := procLoadImageW.Call(
		hInst,
		1,
		imageIcon,
		16, 16,
		0,
	)

	// Großes Icon: 32×32 Pixel (Alt+Tab-Dialog)
	bigIcon, _, _ := procLoadImageW.Call(
		hInst,
		1,
		imageIcon,
		32, 32,
		0,
	)

	if smallIcon != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, smallIcon)
	}
	if bigIcon != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconBig, bigIcon)
	}
}

// init wird automatisch beim Programmstart aufgerufen – vor main() und der TUI.
//
// Reihenfolge:
//  1. AUMID setzen → Taskleistenzuordnung zur App (nicht Console-Host)
//  2. Icon setzen → SetConsoleIcon + WM_SETICON
//
// @date 2026-04-07
func init() {
	setAppUserModelID()
	applyConsoleIcon()
}
