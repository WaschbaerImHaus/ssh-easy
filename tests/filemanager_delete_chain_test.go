// Paket main - Test für die Kettenprüfung beim Betreten eines Verzeichnisses
// beim Löschen (F8, remove() in filemanager_delete.go, Audit Build 90)
//
// Angriff: Ein Verzeichnis in der Mitte der Kette wird NACH dem Vorlesen
// (prefetchDelete) und NACH dem letzten Remove, aber VOR dem Betreten des
// nächsten Geschwister-Verzeichnisses durch einen Symlink ersetzt. Ein Server
// mit wörtlichem realpath (pkg/sftp-Server) fällt dann nur auf, wenn beim
// Betreten die GANZE Lstat-Kette geprüft wird: vor dem Remove einer Datei
// prüft remove() sonst nur das Elternverzeichnis, und das ist durch den Link
// ein echtes Verzeichnis.
//
// Grundlage ist ein echtes localFS auf t.TempDir().
//
// @author Kurt Ingwer
// @date   2026-11-15 14:44
package main

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// chainSwapFS ist ein localFS mit wörtlichem RealPath (nur path.Clean, wie
// literalRealPathFS) und einem Austausch nach dem erfolgreichen Remove von
// swapAfter: dort wird die Kette durch einen Symlink auf victim ersetzt.
type chainSwapFS struct {
	*localFS
	// Pfad, nach dessen erfolgreichem Remove getauscht wird (leer = nie)
	swapAfter string
	// Verzeichnis, das umbenannt wird, und das Ziel des Symlinks an seiner Stelle
	swapDir, victim string
	mu              sync.Mutex
	swapped         bool
}

// RealPath löst keine Symlinks auf: die RealPath-Prüfung allein wäre wirkungslos.
func (c *chainSwapFS) RealPath(p string) (string, error) { return path.Clean(p), nil }

// Remove löscht echt und tauscht danach einmalig das Verzeichnis aus.
func (c *chainSwapFS) Remove(p string) error {
	if err := c.localFS.Remove(p); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.swapAfter != "" && p == c.swapAfter && !c.swapped {
		c.swapped = true
		if err := os.Rename(c.swapDir, c.swapDir+".weg"); err != nil {
			panic(err)
		}
		if err := os.Symlink(c.victim, c.swapDir); err != nil {
			panic(err)
		}
	}
	return nil
}

// runChainDelete führt runDelete synchron mit Zeitgrenze aus.
func runChainDelete(t *testing.T, fsys remoteFS, dir string, names ...string) deleteResult {
	t.Helper()
	events := make(chan deleteEvent, 16)
	go runDelete(context.Background(), deleteRequest{fs: fsys, dir: dir, names: names}, events)
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.result != nil {
				return *ev.result
			}
		case <-timeout:
			t.Fatal("runDelete hängt")
		}
	}
}

// buildChainTree legt D/t/1/a/f1, D/t/1/b/f2 und das Opfer V/b/f2 (außerhalb
// von D) an und liefert D und V.
func buildChainTree(t *testing.T) (dir, victim string) {
	t.Helper()
	_, root := newLocalTestFS(t)
	dir = filepath.Join(root, "fenster")
	victim = filepath.Join(root, "opfer")
	for _, d := range []string{
		filepath.Join(dir, "t", "1", "a"),
		filepath.Join(dir, "t", "1", "b"),
		filepath.Join(victim, "b"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		filepath.Join(dir, "t", "1", "a", "f1"),
		filepath.Join(dir, "t", "1", "b", "f2"),
		filepath.Join(victim, "b", "f2"),
	} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, victim
}

// TestDeleteChainCheckedOnEnteringDirectory - wird D/t/1 nach dem Löschen von
// t/1/a durch einen Symlink auf das Opferverzeichnis ersetzt, muss beim
// Betreten von t/1/b die ganze Kette geprüft werden (errDeleteRace); die
// Datei außerhalb des Fensters bleibt. Ohne die Betreten-Prüfung prüft
// remove() vor dem Remove von f2 nur t/1/b (durch den Link echt, realpath
// wörtlich) und löscht Opfer/b/f2. Randfall: Austausch zwischen zwei
// Geschwistern, also nach dem Vorlesen und nach dem letzten Remove.
func TestDeleteChainCheckedOnEnteringDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Symlinks brauchen unter Windows besondere Rechte")
	}
	dir, victim := buildChainTree(t)
	local, _ := newLocalTestFS(t)
	fsys := &chainSwapFS{
		localFS:   local,
		swapAfter: filepath.ToSlash(filepath.Join(dir, "t", "1", "a")),
		swapDir:   filepath.Join(dir, "t", "1"),
		victim:    victim,
	}
	result := runChainDelete(t, fsys, filepath.ToSlash(dir), "t")
	if !fsys.swapped {
		t.Fatal("Austausch nie ausgelöst - Test prüft nichts")
	}
	found := false
	for _, f := range result.failures {
		found = found || errors.Is(f.err, errDeleteRace)
	}
	if !found {
		t.Errorf("keine errDeleteRace-Meldung: %s", failureErrors(result.failures))
	}
	if _, err := os.Lstat(filepath.Join(victim, "b", "f2")); err != nil {
		t.Errorf("Datei außerhalb des Fensters gelöscht: %v", err)
	}
}

// TestDeleteChainWithoutSwapDeletesEverything - Gegenprobe zum Aufbau: ohne
// Austausch wird der ganze Baum gelöscht, ohne Fehler, und die Datei
// außerhalb bleibt. Sichert, dass die Betreten-Prüfung ehrliche Bäume nicht
// bremst (kein falscher errDeleteRace).
func TestDeleteChainWithoutSwapDeletesEverything(t *testing.T) {
	dir, victim := buildChainTree(t)
	local, _ := newLocalTestFS(t)
	fsys := &chainSwapFS{localFS: local}
	result := runChainDelete(t, fsys, filepath.ToSlash(dir), "t")
	if len(result.failures) != 0 {
		t.Fatalf("Fehler: %s", failureErrors(result.failures))
	}
	if _, err := os.Lstat(filepath.Join(dir, "t")); !os.IsNotExist(err) {
		t.Errorf("Baum nicht gelöscht: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(victim, "b", "f2")); err != nil {
		t.Errorf("Datei außerhalb gelöscht: %v", err)
	}
}
