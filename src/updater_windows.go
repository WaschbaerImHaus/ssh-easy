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
// Beim Beenden startet ssh-easy nicht selbst das Setup, sondern einen
// abgekoppelten Helfer (startDetachedSetupInstaller): Die UAC-Bestätigung darf
// länger dauern, als Windows dem schließenden Hauptprozess Zeit lässt.
//
// @author Kurt Ingwer
// @date   2026-10-31 02:30

//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// defaultSetupStarter ist unter Windows der abgekoppelte Helfer.
//
// @param path - Pfad des Setups
// @param sha - Erwartete SHA-256 (hex)
// @return error - Fehler beim Starten des Helfers
// @date   2026-10-31 02:30
func defaultSetupStarter(path, sha string) error {
	return startDetachedSetupInstaller(path, sha)
}

// startDetachedSetupInstaller prüft das Setup vor und startet ssh-easy.exe als
// abgekoppelten Helfer (--install-update). Keine Shell: Der Pfad ist ein
// einzelnes Argument. Der Helfer überlebt das Ende dieses Prozesses (eigene
// Prozessgruppe, ohne Konsole, wenn möglich außerhalb des Jobs), wartet auf
// dessen Ende und startet das Setup mit erneuter, gesperrter Prüfung.
//
// @param path - Pfad des heruntergeladenen Setups
// @param sha - Erwartete SHA-256 (hex)
// @return error - Prüf- oder Startfehler
// @date   2026-10-31 02:30
func startDetachedSetupInstaller(path, sha string) error {
	// Vorprüfung: derselbe Aufruf, den der Helfer später selbst prüft
	configDir, err := GetConfigDir()
	if err != nil {
		return err
	}
	pid := os.Getpid()
	if _, _, _, err := validateInstallHelperArgs(installHelperArgs(path, sha, pid)[1:], filepath.Join(configDir, "update"), "windows", runtime.GOARCH); err != nil {
		return fmt.Errorf("Setup-Aufruf ungültig: %w", err)
	}
	if err := checkSetupDirectoryClean(path); err != nil {
		return err
	}
	if err := verifyFileSHA256(path, sha); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("eigene EXE unbekannt: %w", err)
	}

	// Erst mit Breakaway aus dem Job (überlebt ein Schließen des Jobs), bei
	// Ablehnung (Job ohne Breakaway-Recht) ohne
	const baseFlags = windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS
	var startErr error
	for _, flags := range []uint32{baseFlags | windows.CREATE_BREAKAWAY_FROM_JOB, baseFlags} {
		cmd := exec.Command(exe, installHelperArgs(path, sha, pid)...)
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
		if startErr = cmd.Start(); startErr == nil {
			// Nicht auf den Helfer warten: Handle freigeben
			return cmd.Process.Release()
		}
	}
	return fmt.Errorf("Update-Helfer nicht startbar: %w", startErr)
}

// waitForProcessExit wartet, bis der Prozess mit der PID beendet ist (höchstens
// timeout). Ist er nicht öffenbar (bereits beendet), kehrt die Funktion sofort
// zurück: Das Setup prüft/ersetzt die EXE ohnehin selbst.
//
// @param pid - PID des Hauptprozesses
// @param timeout - Höchstdauer
// @date   2026-10-31 02:30
func waitForProcessExit(pid int, timeout time.Duration) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, uint32(timeout.Milliseconds()))
}

// runVerifiedSetupInstaller prüft das Setup über einen gesperrten Handle
// und startet es still mit Administratorrechten. Wartet nicht auf das Ende -
// aufgerufen vom abgekoppelten Helfer, nachdem sich ssh-easy beendet hat,
// damit das Setup die EXE ersetzen kann. Unverändert streng: gesperrter
// Handle, SHA-256, Ordnerprüfung.
//
// @param path - Pfad des heruntergeladenen Setups
// @param sha256 - Erwartete Prüfsumme (hex)
// @return error - Prüf- oder Startfehler (z.B. UAC abgelehnt)
// @date   2026-10-31 02:30
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
