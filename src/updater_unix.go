// Auto-Update: Setup-Installation für Unix/Linux/macOS.
//
// Unter Unix gibt es kein Setup - die Binary wird direkt ersetzt
// (siehe replaceExecutable in updater.go).
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00

//go:build !windows

package main

// runVerifiedSetupInstaller ist unter Unix nicht verfügbar.
//
// @param path - Pfad des Setups (ignoriert)
// @param sha256 - Erwartete Prüfsumme (ignoriert)
// @return error - immer errUpdateUnsupported
// @date   2026-10-16 10:00
func runVerifiedSetupInstaller(path, sha256 string) error {
	return errUpdateUnsupported
}
