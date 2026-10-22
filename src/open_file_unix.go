// Plattform-spezifisches Öffnen mit dem Standardprogramm (Linux/macOS).
//
// @author Kurt Ingwer
// @date   2026-10-22 00:35

//go:build !windows

package main

import (
	"os/exec"
	"runtime"
)

// platformOpenFile öffnet eine Datei mit dem Standardprogramm: xdg-open
// (Linux) bzw. open (macOS). Direkter Programmaufruf ohne Shell; Ausgaben
// gehen ins Leere, damit sie die Oberfläche nicht stören. Nicht warten -
// das geöffnete Programm läuft weiter.
//
// @param p - lokaler Pfad
// @return error - Fehler beim Start (z. B. xdg-open nicht installiert)
// @date   2026-10-22 00:35
func platformOpenFile(p string) error {
	tool := "xdg-open"
	if runtime.GOOS == "darwin" {
		tool = "open"
	}
	cmd := exec.Command(tool, p)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// markDownloaded ist unter Linux/macOS ohne Wirkung (keine Zonen-Kennzeichnung;
// die Datei ist ohnehin nicht ausführbar, Rechte 0600).
//
// @param p - lokaler Pfad
// @date   2026-10-22 00:35
func markDownloaded(p string) {}
