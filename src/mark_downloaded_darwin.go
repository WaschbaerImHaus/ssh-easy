// Kennzeichnung heruntergeladener Dateien (macOS).
//
// @author Kurt Ingwer
// @date   2026-10-22 00:43

//go:build darwin

package main

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// markDownloaded setzt die Quarantäne-Kennzeichnung (com.apple.quarantine),
// damit Gatekeeper die Datei wie einen Download behandelt. Dateisysteme
// ohne erweiterte Attribute können keine Kennzeichnung tragen - dort ist
// das kein Fehler.
//
// @param p - lokaler Pfad
// @return error - Kennzeichnung möglich, aber fehlgeschlagen
// @date   2026-10-22 07:10
func markDownloaded(p string) error {
	value := fmt.Sprintf("0081;%x;ssh-easy;", time.Now().Unix())
	err := unix.Setxattr(p, "com.apple.quarantine", []byte(value), 0)
	if err == unix.ENOTSUP {
		return nil
	}
	return err
}
