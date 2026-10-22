// Kennzeichnung heruntergeladener Dateien (Linux und andere Unix-Systeme).
//
// @author Kurt Ingwer
// @date   2026-10-22 00:43

//go:build !windows && !darwin

package main

// markDownloaded ist unter Linux ohne Wirkung: Es gibt keine allgemeine
// Herkunftskennzeichnung; die Datei ist ohnehin nicht ausführbar (0600)
// und nur Dokument-/Medientypen werden geöffnet.
//
// @param p - lokaler Pfad
// @return error - immer nil
// @date   2026-10-22 07:10
func markDownloaded(p string) error { return nil }
