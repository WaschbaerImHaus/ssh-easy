// Paket main - Datei-Manager: "Lokaler PC" unter Windows (Laufwerksliste)
//
// @author Kurt Ingwer
// @date   2026-11-13 09:08

//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// localIsWindows - Windows-Pfadregeln für den lokalen PC
const localIsWindows = true

// localDrives liefert die vorhandenen Laufwerke (C:, D:, …) als
// Verzeichnisse der Laufwerksliste "/". Es wird nur die Bitmaske des
// Systems gelesen, kein Laufwerk angesprochen (ein leeres DVD-Laufwerk
// würde sonst warten lassen).
//
// @return []os.FileInfo - Laufwerke
// @date   2026-10-22 06:40
func localDrives() []os.FileInfo {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	var drives []os.FileInfo
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) != 0 {
			drives = append(drives, localDirInfo{name: string(rune('A'+i)) + ":"})
		}
	}
	return drives
}

// localRenameNoReplace benennt um, ohne ein vorhandenes Ziel zu ersetzen:
// MoveFileEx ohne MOVEFILE_REPLACE_EXISTING (und ohne COPY_ALLOWED, also
// nur auf demselben Laufwerk).
//
// @param from - bisheriger Pfad
// @param to - neuer Pfad
// @return error - Fehler (z. B. Ziel vorhanden, anderes Laufwerk)
// @date   2026-11-13 09:08
func localRenameNoReplace(from, to string) error {
	fromW, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	toW, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(fromW, toW, 0)
}
