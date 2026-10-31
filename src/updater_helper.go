// Auto-Update: abgekoppelter Installations-Helfer (plattformneutraler Teil).
//
// Problem: Wer das Fenster schließt (Kreuz, Alt+F4), gibt dem Programm unter
// Windows nur ca. 5 Sekunden, bevor es hart beendet wird. Die UAC-Bestätigung
// für das Setup dauert oft länger. Darum startet ssh-easy beim Beenden sich
// selbst als Helfer ("ssh-easy.exe --install-update <Setup> <SHA-256> <PID>"),
// der unabhängig weiterläuft, auf das Ende des Hauptprozesses wartet und
// dann das Setup startet (runVerifiedSetupInstaller, unverändert: gesperrter
// Handle, SHA-256-Prüfung, Ordnerprüfung, runas, ohne Zeitlimit).
//
// Sicherheit: Es gibt keine Shell, jeder Pfad ist EIN Argument. Der Helfer
// nimmt nur genau das eigene, frisch geladene Setup an
// (<Update-Ordner>/setup-*/<Setup-Dateiname>) und verlässt sich für den Inhalt
// auf die erneute Prüfung mit gesperrtem Handle.
//
// @author Kurt Ingwer
// @date   2026-10-31 02:30
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// installHelperFlag - Schalter, mit dem sich ssh-easy als Helfer startet
	installHelperFlag = "--install-update"
	// installHelperParentWait - längste Wartezeit auf das Ende des Hauptprozesses
	installHelperParentWait = 15 * time.Second
)

// installHelperArgs liefert die Argumente für den Start des Helfers
// (exec.Command(exePath, args...)): Schalter, Setup-Pfad, SHA-256, PID des
// Hauptprozesses. Jeder Wert ist ein eigenes Argument.
//
// @param path - Pfad des Setups
// @param sha - Erwartete SHA-256 (hex)
// @param pid - PID des Hauptprozesses
// @return []string - genau 4 Argumente
// @date   2026-10-31 02:30
func installHelperArgs(path, sha string, pid int) []string {
	return []string{installHelperFlag, path, sha, strconv.Itoa(pid)}
}

// isInstallHelperInvocation erkennt den Helfer-Aufruf: nur wenn GENAU das
// zweite Element (os.Args[1]) der Schalter ist.
//
// @param args - Kommandozeile inkl. Programmname (os.Args)
// @return bool - true im Helfer-Modus
// @date   2026-10-31 02:30
func isInstallHelperInvocation(args []string) bool {
	return len(args) >= 2 && args[1] == installHelperFlag
}

// hasControlChars prüft auf Steuerzeichen (inkl. NUL, Zeilenumbruch, DEL).
func hasControlChars(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// isHexString prüft, ob s genau n Hex-Zeichen (0-9, a-f, A-F) enthält.
func isHexString(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// validateInstallHelperArgs prüft die Argumente des Helfers (ohne Schalter,
// also os.Args[2:]: {Pfad, SHA-256, PID}). Reine Funktion ohne Dateizugriff.
//
// Gültig nur: genau 3 Argumente; downloadDir absolut und nicht leer; Pfad
// absolut, unverändert (filepath.Clean(path) == path), ohne Steuerzeichen und
// von der Form <downloadDir>/setup-*/<Setup-Dateiname der Plattform>;
// SHA-256 aus 64 Hex-Zeichen; PID eine positive Dezimalzahl.
//
// @param args - Argumente nach dem Schalter
// @param downloadDir - Update-Ordner (~/.ssh-easy/update)
// @param goos - Betriebssystem (bestimmt den Setup-Dateinamen)
// @param goarch - Architektur
// @return path - geprüfter Setup-Pfad
// @return sha - geprüfte SHA-256 (unverändert)
// @return pid - PID des Hauptprozesses
// @return err - Grund der Ablehnung
// @date   2026-10-31 02:30
func validateInstallHelperArgs(args []string, downloadDir, goos, goarch string) (path, sha string, pid int, err error) {
	if len(args) != 3 {
		return "", "", 0, fmt.Errorf("erwartet 3 Argumente, erhalten %d", len(args))
	}
	if downloadDir == "" || !filepath.IsAbs(downloadDir) {
		return "", "", 0, fmt.Errorf("Update-Ordner unbekannt")
	}
	assetName, ok := updateAssetName(goos, goarch, true)
	if !ok {
		return "", "", 0, fmt.Errorf("für %s/%s gibt es kein Setup", goos, goarch)
	}

	path, sha = args[0], args[1]
	if path == "" || hasControlChars(path) {
		return "", "", 0, fmt.Errorf("ungültiger Setup-Pfad")
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", "", 0, fmt.Errorf("Setup-Pfad ist nicht absolut oder nicht bereinigt")
	}
	if filepath.Base(path) != assetName {
		return "", "", 0, fmt.Errorf("unerwarteter Setup-Dateiname")
	}
	setupDir := filepath.Dir(path)
	sameDir := func(a, b string) bool {
		if goos == "windows" {
			return strings.EqualFold(a, b)
		}
		return a == b
	}
	if !sameDir(filepath.Dir(setupDir), filepath.Clean(downloadDir)) {
		return "", "", 0, fmt.Errorf("Setup liegt nicht im Update-Ordner")
	}
	if name := filepath.Base(setupDir); !strings.HasPrefix(name, "setup-") || len(name) == len("setup-") {
		return "", "", 0, fmt.Errorf("Setup-Ordner heißt nicht setup-*")
	}

	if !isHexString(sha, 64) {
		return "", "", 0, fmt.Errorf("SHA-256 ist nicht 64 Hex-Zeichen lang")
	}
	// PID: nur Ziffern (Atoi ließe auch "+5" zu)
	pidText := args[2]
	if pidText == "" || len(pidText) > 10 || strings.Trim(pidText, "0123456789") != "" {
		return "", "", 0, fmt.Errorf("PID ist keine Dezimalzahl")
	}
	pid, perr := strconv.Atoi(pidText)
	if perr != nil || pid <= 0 {
		return "", "", 0, fmt.Errorf("PID ist nicht positiv")
	}
	return path, sha, pid, nil
}

// removeSetupDir entfernt den setup-*-Ordner eines gescheiterten Updates.
// Symlinks und Nicht-Ordner werden nie angefasst.
//
// @param dir - setup-*-Ordner
// @date   2026-10-31 02:30
func removeSetupDir(dir string) {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return
	}
	_ = os.RemoveAll(dir)
}

// runInstallUpdateHelper ist der Einstieg des Helfer-Prozesses: Argumente
// prüfen, auf das Ende des Hauptprozesses warten (höchstens
// installHelperParentWait), dann das Setup über die unveränderte, gesperrte
// Prüfung starten (runVerifiedSetupInstaller). Bei Fehlern wird protokolliert
// und der setup-*-Ordner entfernt.
//
// @param args - Argumente nach dem Schalter (os.Args[2:])
// @param logger - Protokoll (darf nil sein)
// @return int - Exit-Code des Prozesses (0 = Setup gestartet)
// @date   2026-10-31 02:30
func runInstallUpdateHelper(args []string, logger *Logger) int {
	if runtime.GOOS != "windows" {
		logger.Error("Update-Helfer: %v", errUpdateUnsupported)
		return 2
	}
	configDir, err := GetConfigDir()
	if err != nil {
		logger.Error("Update-Helfer: Konfigurationsordner unbekannt: %v", err)
		return 2
	}
	path, sha, pid, err := validateInstallHelperArgs(args, filepath.Join(configDir, "update"), runtime.GOOS, runtime.GOARCH)
	if err != nil {
		// Ungeprüfte Argumente nie löschen oder starten
		logger.Error("Update-Helfer: Aufruf abgelehnt: %v", err)
		return 2
	}
	waitForProcessExit(pid, installHelperParentWait)
	if err := runVerifiedSetupInstaller(path, sha); err != nil {
		logger.Error("Update-Helfer: Setup nicht gestartet: %v", err)
		removeSetupDir(filepath.Dir(path))
		return 1
	}
	logger.Info("Update-Helfer: Setup gestartet")
	return 0
}
