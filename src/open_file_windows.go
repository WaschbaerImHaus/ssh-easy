// Plattform-spezifisches Öffnen mit dem Standardprogramm (Windows).
//
// @author Kurt Ingwer
// @date   2026-10-22 00:35

//go:build windows

package main

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// platformOpenFile öffnet eine Datei mit dem für ihre Endung registrierten
// Programm (ShellExecute "open" - ohne Kommandozeile/Shell, also ohne
// Quoting- oder Injektionsprobleme).
//
// @param p - lokaler Pfad
// @return error - Fehler von ShellExecute
// @date   2026-10-22 00:35
func platformOpenFile(p string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return err
	}
	dir, err := windows.UTF16PtrFromString(filepath.Dir(p))
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, dir, windows.SW_SHOWNORMAL)
}

// markDownloaded kennzeichnet die Datei als "aus dem Internet" (Mark of the
// Web, Zone 3): Office öffnet sie in der geschützten Ansicht, SmartScreen
// und andere Programme warnen. Fehler (z. B. FAT-Datenträger) ignorieren.
//
// @param p - lokaler Pfad
// @date   2026-10-22 00:35
func markDownloaded(p string) {
	_ = os.WriteFile(p+":Zone.Identifier", []byte("[ZoneTransfer]\r\nZoneId=3\r\n"), 0o600)
}
