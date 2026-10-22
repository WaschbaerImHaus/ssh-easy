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
// damit Gatekeeper die Datei wie einen Download behandelt. Fehler ignorieren.
//
// @param p - lokaler Pfad
// @date   2026-10-22 00:43
func markDownloaded(p string) {
	value := fmt.Sprintf("0081;%x;ssh-easy;", time.Now().Unix())
	_ = unix.Setxattr(p, "com.apple.quarantine", []byte(value), 0)
}
