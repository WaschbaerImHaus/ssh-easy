// Tests für die Build-Anzeige im Hauptmenü.
//
// Hintergrund: Die Build-Nummer wurde früher zur Laufzeit aus build.txt
// neben der EXE gelesen. Installierte Versionen haben diese Datei nicht -
// im Menü stand dann "vdev". Jetzt wird build.txt per go:embed in die
// Binary eingebaut.
//
// Prüft:
//   - readBuildNumber liefert die eingebettete Nummer (identisch mit build.txt)
//   - unabhängig vom Arbeitsverzeichnis (simuliert installierte EXE)
//   - Titel-Format "ssh-easy v0.<Build> (Build <Build>)" passend zum Release-Schema
//   - Fallback "dev" für leere/fehlende Nummer
//   - Hauptmenü zeigt den Build im Titel
//
// @author Kurt Ingwer
// @date   2026-10-14 16:05

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadBuildNumberMatchesBuildFile: eingebettete Nummer == build.txt.
func TestReadBuildNumberMatchesBuildFile(t *testing.T) {
	data, err := os.ReadFile("build.txt")
	if err != nil {
		t.Fatalf("build.txt nicht lesbar: %v", err)
	}
	want := strings.TrimSpace(string(data))
	if got := readBuildNumber(); got != want {
		t.Errorf("readBuildNumber() = %q, erwartet %q", got, want)
	}
}

// TestReadBuildNumberIndependentOfWorkingDir: Auch ohne build.txt im
// Arbeitsverzeichnis (wie bei einer installierten EXE) kommt die Nummer an.
func TestReadBuildNumberIndependentOfWorkingDir(t *testing.T) {
	want := readBuildNumber()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	got := readBuildNumber()
	if got != want || got == "dev" || got == "" {
		t.Errorf("readBuildNumber() in fremdem Verzeichnis = %q, erwartet %q", got, want)
	}
}

// TestFormatAppTitle prüft das Titel-Format inklusive Fallbacks.
func TestFormatAppTitle(t *testing.T) {
	cases := map[string]string{
		"46":    "ssh-easy v0.46 (Build 46)",
		"7":     "ssh-easy v0.7 (Build 7)",
		" 46\n": "ssh-easy v0.46 (Build 46)",
		"":      "ssh-easy (Build dev)",
		"dev":   "ssh-easy (Build dev)",
	}
	for in, want := range cases {
		if got := formatAppTitle(in); got != want {
			t.Errorf("formatAppTitle(%q) = %q, erwartet %q", in, got, want)
		}
	}
}

// TestParseEmbeddedBuild prüft die Normalisierung des eingebetteten Inhalts.
func TestParseEmbeddedBuild(t *testing.T) {
	cases := map[string]string{
		"45\n":      "45",
		"\t45 \r\n": "45",
		"":          "dev",
		"  \n":      "dev",
	}
	for in, want := range cases {
		if got := parseEmbeddedBuild(in); got != want {
			t.Errorf("parseEmbeddedBuild(%q) = %q, erwartet %q", in, got, want)
		}
	}
}

// TestMenuShowsBuildNumber: Das Hauptmenü zeigt den Build im Titel.
func TestMenuShowsBuildNumber(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "connections.json")
	m := NewAppModel(configPath, "46", NewSSHManager(NewLogger()))
	var s strings.Builder
	m.renderList(&s)
	if !strings.Contains(s.String(), "ssh-easy v0.46 (Build 46)") {
		t.Errorf("Menü enthält keinen Build-Titel:\n%s", s.String())
	}
}
