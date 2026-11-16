// Paket main - Datei-Manager: Umbenennen ohne Ersetzen auf übrigen Systemen
//
// @author Kurt Ingwer
// @date   2026-11-13 09:08

//go:build !linux && !windows

package main

import "errors"

// localRenameNoReplace - ohne sicheres Umbenennen ohne Ersetzen (macOS,
// BSD): nicht unterstützt, Verschieben kopiert dann wie bisher.
//
// @param from - bisheriger Pfad
// @param to - neuer Pfad
// @return error - immer errors.ErrUnsupported
// @date   2026-11-13 09:08
func localRenameNoReplace(from, to string) error {
	return errors.ErrUnsupported
}
