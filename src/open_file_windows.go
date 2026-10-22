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
// und andere Programme warnen. Muss vor dem Setzen eines Schreibschutzes
// passieren (sonst lässt Windows den Datenstrom nicht anlegen).
// Datenträger ohne benannte Datenströme (FAT, exFAT) können keine
// Kennzeichnung tragen - dort ist das kein Fehler.
//
// @param p - lokaler Pfad
// @return error - Kennzeichnung möglich, aber fehlgeschlagen
// @date   2026-10-22 07:10
func markDownloaded(p string) error {
	err := os.WriteFile(p+":Zone.Identifier", []byte("[ZoneTransfer]\r\nZoneId=3\r\n"), 0o600)
	if err == nil || !volumeSupportsStreams(p) {
		return nil
	}
	return err
}

// volumeSupportsStreams meldet, ob das Laufwerk einer Datei benannte
// Datenströme kennt (NTFS ja, FAT/exFAT nein). Im Zweifel: ja.
//
// @param p - lokaler Pfad
// @return bool - true wenn Datenströme möglich sind
// @date   2026-10-22 07:10
func volumeSupportsStreams(p string) bool {
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return true
	}
	root := make([]uint16, windows.MAX_PATH+1)
	if err := windows.GetVolumePathName(name, &root[0], uint32(len(root))); err != nil {
		return true
	}
	var flags uint32
	if err := windows.GetVolumeInformation(&root[0], nil, 0, nil, nil, &flags, nil, 0); err != nil {
		return true
	}
	// FILE_NAMED_STREAMS
	return flags&0x00040000 != 0
}
