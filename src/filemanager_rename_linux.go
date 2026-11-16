// Paket main - Datei-Manager: Umbenennen ohne Ersetzen unter Linux
//
// @author Kurt Ingwer
// @date   2026-11-13 09:08

//go:build linux

package main

import "golang.org/x/sys/unix"

// localRenameNoReplace benennt um, ohne ein vorhandenes Ziel zu ersetzen
// (renameat2 mit RENAME_NOREPLACE, atomar im Kern). Dateisysteme ohne
// Unterstützung melden EINVAL - dann wird kopiert.
//
// @param from - bisheriger Pfad
// @param to - neuer Pfad
// @return error - Fehler (z. B. EEXIST, EXDEV, EINVAL)
// @date   2026-11-13 09:08
func localRenameNoReplace(from, to string) error {
	return unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE)
}
