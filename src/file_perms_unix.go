// Paket main - Datei-Berechtigungen (Unix-Variante)
//
// Setzt 0600-Berechtigungen via os.Chmod. Pendant zu file_perms_windows.go
// das unter Windows DACLs auf Owner-only beschränkt.
//
// @author Kurt Ingwer
// @date   2026-04-19 12:00

//go:build !windows

package main

import "os"

// restrictFilePermissions setzt die Datei auf "nur Besitzer darf lesen/schreiben".
// Unter Unix reicht chmod 0600 – die Group- und Other-Bits werden entfernt.
//
// @param path - Pfad zur Datei
// @return error - Fehler beim Setzen der Berechtigungen
// @date   2026-04-19 12:00
func restrictFilePermissions(path string) error {
	return os.Chmod(path, 0600)
}
