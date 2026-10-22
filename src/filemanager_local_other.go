// Paket main - Datei-Manager: "Lokaler PC" unter Linux/macOS
//
// @author Kurt Ingwer
// @date   2026-10-22 06:40

//go:build !windows

package main

import "os"

// localIsWindows - unter Linux/macOS gelten POSIX-Pfade
const localIsWindows = false

// localDrives - ohne Laufwerksbuchstaben gibt es keine Laufwerksliste.
//
// @return []os.FileInfo - immer leer
// @date   2026-10-22 06:40
func localDrives() []os.FileInfo { return nil }
