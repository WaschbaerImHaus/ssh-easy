// Auto-Update: Setup-Installation für Windows.
//
// Liegt ssh-easy im Programme-Ordner, fehlen dem normalen Benutzer die
// Schreibrechte zum Austauschen der EXE. Dann wird beim Beenden das
// heruntergeladene Setup still (/S) mit Administratorrechten gestartet
// ("runas" -> Windows fragt per Benutzerkontensteuerung nach). Das Setup
// entfernt die alte Version selbst und übernimmt die Desktop-Verknüpfung.
//
// Schutz vor Austausch (TOCTOU): Die Datei wird nur mit Lesefreigabe
// geöffnet (kein FILE_SHARE_WRITE/DELETE). Solange dieser Handle offen ist,
// kann kein anderer Prozess die Datei ändern, löschen oder umbenennen. Die
// Prüfsumme wird über genau diesen Handle berechnet und der Handle erst nach
// dem Start des Setups geschlossen.
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00

//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// runVerifiedSetupInstaller prüft das Setup über einen gesperrten Handle
// und startet es still mit Administratorrechten. Wartet nicht auf das Ende -
// ssh-easy beendet sich direkt danach, damit das Setup die EXE ersetzen kann.
//
// @param path - Pfad des heruntergeladenen Setups
// @param sha256 - Erwartete Prüfsumme (hex)
// @return error - Prüf- oder Startfehler (z.B. UAC abgelehnt)
// @date   2026-10-16 10:00
func runVerifiedSetupInstaller(path, sha256 string) error {
	path16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	// FILE_FLAG_OPEN_REPARSE_POINT: Ein untergeschobener Symlink wird selbst
	// geöffnet statt verfolgt -> Prüfsumme stimmt nicht -> Abbruch
	handle, err := windows.CreateFile(path16, windows.GENERIC_READ, windows.FILE_SHARE_READ,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return fmt.Errorf("Setup nicht exklusiv lesbar: %w", err)
	}
	locked := os.NewFile(uintptr(handle), path)
	// Handle bleibt bis nach ShellExecute offen (Sperre gegen Austausch)
	defer locked.Close()
	if err := verifyReaderSHA256(locked, sha256); err != nil {
		return err
	}
	// Im Ordner darf nur das Setup liegen (keine untergeschobenen DLLs)
	if err := checkSetupDirectoryClean(path); err != nil {
		return err
	}

	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	args, err := windows.UTF16PtrFromString("/S")
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, path16, args, nil, windows.SW_SHOWNORMAL)
}
