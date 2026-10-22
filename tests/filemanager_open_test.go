// Paket main - Tests für "mit Standardprogramm öffnen" (F9 im Betrachter, Build 60)
//
// Prüft: lokaler Dateiname (Windows-sicher), gesperrte ausführbare Typen,
// Herunterladen in einen privaten Temp-Ordner (0700/0600), Aufruf des
// Öffners, Fehler, Größengrenze und Aufräumen.
//
// @author Kurt Ingwer
// @date   2026-10-22 00:40
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// stubOpener ersetzt den echten Öffner und den Temp-Ordner im Test.
func stubOpener(t *testing.T, result error) *[]string {
	t.Helper()
	var opened []string
	oldOpener, oldRoot := externalOpener, openTempRoot
	externalOpener = func(p string) error {
		opened = append(opened, p)
		return result
	}
	openTempRoot = t.TempDir()
	t.Cleanup(func() {
		cleanupExternalOpen()
		externalOpener, openTempRoot = oldOpener, oldRoot
	})
	return &opened
}

// TestLocalOpenName - lokale Dateinamen sind unter Windows und Linux gültig.
func TestLocalOpenName(t *testing.T) {
	cases := map[string]string{
		"bild.jpg":             "bild.jpg",
		"a<b>:c\"d|e?f*.txt":   "a_b__c_d_e_f_.txt",
		"pfad\\mit\\backslash": "pfad_mit_backslash",
		"CON.txt":              "_CON.txt",
		"nul":                  "_nul",
		"com1.log":             "_com1.log",
		"evil.exe. . ":         "evil.exe",
		"":                     "datei",
		"...":                  "datei",
		"steuer\x01zeichen":    "steuer_zeichen",
		"日本語.png":              "日本語.png",
	}
	for in, want := range cases {
		if got := localOpenName(in); got != want {
			t.Errorf("localOpenName(%q) = %q, erwartet %q", in, got, want)
		}
	}
	long := strings.Repeat("x", 300) + ".pdf"
	if got := localOpenName(long); len(got) > 150 || !strings.HasSuffix(got, ".pdf") {
		t.Errorf("langer Name: %d Zeichen, Endung %q", len(got), filepath.Ext(got))
	}
}

// TestBlockedOpenExtension - ausführbare und Skript-Typen werden gesperrt.
func TestBlockedOpenExtension(t *testing.T) {
	blocked := []string{"setup.exe", "x.pdf.exe", "a.BAT", "b.cmd", "c.ps1", "d.vbs", "e.js", "f.hta",
		"g.msi", "h.scr", "i.lnk", "j.url", "k.reg", "l.jar", "m.desktop", "n.sh", "o.cpl", "p.iso", "q.appimage", "r.wsf", "s.chm"}
	for _, name := range blocked {
		if _, ok := blockedOpenExtension(localOpenName(name)); !ok {
			t.Errorf("%s nicht gesperrt", name)
		}
	}
	allowed := []string{"bild.jpg", "foto.PNG", "seite.html", "doku.pdf", "text.txt", "tabelle.xlsx", "film.mp4", "ohne_endung"}
	for _, name := range allowed {
		if ext, ok := blockedOpenExtension(localOpenName(name)); ok {
			t.Errorf("%s gesperrt (%s)", name, ext)
		}
	}
	// Tricks: Punkte/Leerzeichen am Ende und alternative Datenströme
	for _, name := range []string{"evil.exe.", "evil.exe ", "harmlos.txt:evil.exe"} {
		if _, ok := blockedOpenExtension(localOpenName(name)); !ok {
			t.Errorf("Trick %q nicht gesperrt (lokal %q)", name, localOpenName(name))
		}
	}
}

// openViewerOn öffnet den Betrachter für eine Datei der Quelle.
func openViewerOn(t *testing.T, files map[string]string, name string) AppModel {
	t.Helper()
	m, _ := openViewManager(t, files)
	m = cursorTo(t, m, name)
	return pressRun(t, m, "f3")
}

// TestViewerF9OpensLocalCopy - F9 lädt die Datei in einen privaten Ordner
// und übergibt sie dem Standardprogramm.
func TestViewerF9OpensLocalCopy(t *testing.T) {
	opened := stubOpener(t, nil)
	m := openViewerOn(t, map[string]string{"bild.jpg": "JPEGDATEN"}, "bild.jpg")
	m = pressRun(t, m, "f9")
	if len(*opened) != 1 {
		t.Fatalf("Öffner %d-mal aufgerufen (Meldung %q)", len(*opened), m.fm.view.message)
	}
	local := (*opened)[0]
	if filepath.Base(local) != "bild.jpg" {
		t.Errorf("lokaler Name %q", filepath.Base(local))
	}
	data, err := os.ReadFile(local)
	if err != nil || string(data) != "JPEGDATEN" {
		t.Errorf("Inhalt %q / %v", data, err)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(local); info.Mode().Perm() != 0o600 {
			t.Errorf("Datei-Rechte %v, erwartet 0600", info.Mode().Perm())
		}
		if info, _ := os.Stat(filepath.Dir(local)); info.Mode().Perm() != 0o700 {
			t.Errorf("Ordner-Rechte %v, erwartet 0700", info.Mode().Perm())
		}
	}
	if !strings.HasPrefix(filepath.Dir(filepath.Dir(local)), openTempRoot) {
		t.Errorf("Datei liegt nicht im Temp-Bereich: %s", local)
	}
	if m.fm.view.message != fmt.Sprintf(m.tr(txtFMOpenDone), "bild.jpg") {
		t.Errorf("Meldung = %q", m.fm.view.message)
	}
	// Zweites Öffnen derselben Datei: eigener Name, kein Überschreiben
	m = pressRun(t, m, "f9")
	if len(*opened) != 2 || (*opened)[1] == local {
		t.Errorf("zweites Öffnen: %v", *opened)
	}
	cleanupExternalOpen()
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Error("lokale Kopie nach dem Aufräumen noch vorhanden")
	}
}

// TestViewerF9BlocksExecutables - ausführbare Typen werden nicht geladen.
func TestViewerF9BlocksExecutables(t *testing.T) {
	opened := stubOpener(t, nil)
	m := openViewerOn(t, map[string]string{"setup.exe": "MZ\x00\x00"}, "setup.exe")
	m = pressRun(t, m, "f9")
	if len(*opened) != 0 {
		t.Error("ausführbare Datei geöffnet")
	}
	if m.fm.view.message != fmt.Sprintf(m.tr(txtFMOpenBlocked), ".exe") {
		t.Errorf("Meldung = %q", m.fm.view.message)
	}
	entries, _ := os.ReadDir(openTempRoot)
	if len(entries) != 0 {
		t.Errorf("trotz Sperre heruntergeladen: %v", entries)
	}
}

// TestViewerF9OpenerError - Fehler des Öffners wird gemeldet.
func TestViewerF9OpenerError(t *testing.T) {
	stubOpener(t, errors.New("xdg-open fehlt"))
	m := openViewerOn(t, map[string]string{"seite.html": "<p>x</p>"}, "seite.html")
	m = pressRun(t, m, "f9")
	if !strings.Contains(m.fm.view.message, "xdg-open fehlt") {
		t.Errorf("Meldung = %q", m.fm.view.message)
	}
}

// TestViewerF9TooLarge - zu große Dateien werden nicht heruntergeladen.
func TestViewerF9TooLarge(t *testing.T) {
	opened := stubOpener(t, nil)
	defer func(old int64) { fmOpenMaxBytes = old }(fmOpenMaxBytes)
	fmOpenMaxBytes = 4
	m := openViewerOn(t, map[string]string{"gross.pdf": "123456789"}, "gross.pdf")
	m = pressRun(t, m, "f9")
	if len(*opened) != 0 {
		t.Error("zu große Datei geöffnet")
	}
	if m.fm.view.message != fmt.Sprintf(m.tr(txtFMOpenTooLarge), formatSizeWithUnit(4)) {
		t.Errorf("Meldung = %q", m.fm.view.message)
	}
}

// TestViewerKeyBarHasF9 - F9 ist in der Leiste belegt.
func TestViewerKeyBarHasF9(t *testing.T) {
	for _, opt := range AvailableLanguages {
		labels := strings.Split(extraText(opt.Code, txtFMViewFKeys), "|")
		if len(labels) != 10 || strings.TrimSpace(labels[8]) == "" {
			t.Errorf("%s: F9 nicht beschriftet: %v", opt.Code, labels)
		}
	}
}

// TestCleanupStaleOpenDirs - alte Reste früherer Sitzungen werden entfernt,
// fremde Ordner bleiben.
func TestCleanupStaleOpenDirs(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, openTempPrefix+"alt")
	fresh := filepath.Join(root, openTempPrefix+"neu")
	foreign := filepath.Join(root, "fremd")
	for _, dir := range []string{old, fresh, foreign} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-48 * time.Hour)
	os.Chtimes(old, past, past)
	os.Chtimes(foreign, past, past)
	cleanupStaleOpenDirs(root, 24*time.Hour)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("alter Rest nicht entfernt")
	}
	for _, dir := range []string{fresh, foreign} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("%s fälschlich entfernt", dir)
		}
	}
}
