// Paket main - Test für die Prüfung der Startpfade von ListTree (F4,
// validTreeStarts / errTreeListStart in filemanager_remote.go)
//
// ListTree reicht die Startpfade an ein Kommando auf dem Server weiter
// (find). Ein Pfad wie "-delete" würde dort als Option gelesen, ein Pfad mit
// NUL, Zeilenumbruch oder ".." bricht aus der Auswahl aus. Server-seitig
// gelieferte oder vom Fenster abgeleitete Pfade sind nicht vertrauenswürdig.
// Geprüft wird, bevor irgendetwas an den Server geht - darum genügt ein
// leerer sftpFS ohne SSH-Client.
//
// @author Kurt Ingwer
// @date   2026-11-15 14:44
package main

import (
	"context"
	"errors"
	"testing"
)

// listTreeBadStarts sind ungültige Startpfade (jede Zeile ein Aufruf).
var listTreeBadStarts = [][]string{
	{"-delete"},
	{""},
	{"/a\x00-delete"},
	{"relativ"},
	{"/a/../b"},
	{"/a/"},
	{"/a\nb"},
	nil,
	{},
	{"/ok", "-delete"},
}

// TestListTreeRejectsBadStartPaths - jeder ungültige Startpfad (Option,
// leer, NUL, relativ, "..", Schrägstrich am Ende, Zeilenumbruch, keine
// Liste, ein schlechter Pfad hinter einem guten) ergibt errTreeListStart.
// Ohne die Prüfung käme für alles ErrUnsupported (kein SSH-Client).
func TestListTreeRejectsBadStartPaths(t *testing.T) {
	for _, starts := range listTreeBadStarts {
		_, _, err := (&sftpFS{}).ListTree(context.Background(), starts, 3)
		if !errors.Is(err, errTreeListStart) {
			t.Errorf("ListTree(%q): Fehler %v, erwartet errTreeListStart", starts, err)
		}
		if errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("ListTree(%q): ErrUnsupported statt Ablehnung", starts)
		}
		if err := validTreeStarts(starts); !errors.Is(err, errTreeListStart) {
			t.Errorf("validTreeStarts(%q) = %v, erwartet errTreeListStart", starts, err)
		}
	}
}

// TestListTreeAcceptsGoodStartPath - ein sauberer absoluter Pfad besteht die
// Prüfung; ohne SSH-Client folgt dann ErrUnsupported (nicht errTreeListStart).
func TestListTreeAcceptsGoodStartPath(t *testing.T) {
	for _, starts := range [][]string{{"/home/a"}, {"/"}, {"/a", "/b/c"}} {
		_, _, err := (&sftpFS{}).ListTree(context.Background(), starts, 3)
		if errors.Is(err, errTreeListStart) {
			t.Errorf("ListTree(%q) lehnt gültige Pfade ab", starts)
		}
		if !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("ListTree(%q): Fehler %v, erwartet ErrUnsupported", starts, err)
		}
		if err := validTreeStarts(starts); err != nil {
			t.Errorf("validTreeStarts(%q) = %v, erwartet nil", starts, err)
		}
	}
}
