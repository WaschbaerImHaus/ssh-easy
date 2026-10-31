// Auto-Update: Setup-Installation für Unix/Linux/macOS.
//
// Unter Unix gibt es kein Setup - die Binary wird direkt ersetzt
// (siehe replaceExecutable in updater.go).
//
// @author Kurt Ingwer
// @date   2026-10-31 02:30

//go:build !windows

package main

import "time"

// runVerifiedSetupInstaller ist unter Unix nicht verfügbar.
//
// @param path - Pfad des Setups (ignoriert)
// @param sha256 - Erwartete Prüfsumme (ignoriert)
// @return error - immer errUpdateUnsupported
// @date   2026-10-16 10:00
func runVerifiedSetupInstaller(path, sha256 string) error {
	return errUpdateUnsupported
}

// defaultSetupStarter ist unter Unix wie bisher runVerifiedSetupInstaller
// (nicht verfügbar): Linux nutzt den Ersetzen-Weg, nie ein Setup.
//
// @param path - Pfad des Setups (ignoriert)
// @param sha - Erwartete Prüfsumme (ignoriert)
// @return error - immer errUpdateUnsupported
// @date   2026-10-31 02:30
func defaultSetupStarter(path, sha string) error {
	return runVerifiedSetupInstaller(path, sha)
}

// waitForProcessExit ist unter Unix ohne Wirkung: Der Helfer läuft nur unter
// Windows.
//
// @param pid - PID (ignoriert)
// @param timeout - Höchstdauer (ignoriert)
// @date   2026-10-31 02:30
func waitForProcessExit(pid int, timeout time.Duration) {}
