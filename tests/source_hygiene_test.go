// Paket main - Schutz vor versteckten Unicode-Zeichen im Quellcode (Build 51)
//
// Hintergrund (Sicherheits-Scan, CWE-116): Der Scanner meldet unsichtbare
// Zeichen in den Übersetzungen. Das ist dort U+200C (ZWNJ), das Kannada,
// Telugu und Persisch für die korrekte Schreibweise brauchen - ein
// False Positive. Damit daraus nie ein echtes Problem wird ("Trojan
// Source", CVE-2021-42574), prüft dieser Test:
//   - Bidi-Steuerzeichen (U+202A-202E, U+2066-2069, U+200E/F) sind überall
//     verboten
//   - ZWSP, Wortverbinder, BOM, Soft-Hyphen sind überall verboten
//   - ZWNJ/ZWJ (U+200C/D) nur in i18n*.go und nur innerhalb von Texten
//
// @author Kurt Ingwer
// @date   2026-10-17 22:40
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenInvisible sind Zeichen, die in keiner Quelldatei vorkommen dürfen.
var forbiddenInvisible = map[rune]string{
	0x200B: "ZERO WIDTH SPACE", 0x200E: "LRM", 0x200F: "RLM",
	0x202A: "LRE", 0x202B: "RLE", 0x202C: "PDF", 0x202D: "LRO", 0x202E: "RLO",
	0x2060: "WORD JOINER", 0x2061: "FUNCTION APPLICATION", 0x2062: "INVISIBLE TIMES",
	0x2063: "INVISIBLE SEPARATOR", 0x2064: "INVISIBLE PLUS",
	0x2066: "LRI", 0x2067: "RLI", 0x2068: "FSI", 0x2069: "PDI",
	0xFEFF: "BOM", 0x00AD: "SOFT HYPHEN",
}

// TestNoHiddenUnicodeInSource prüft alle Go-Dateien des Moduls.
func TestNoHiddenUnicodeInSource(t *testing.T) {
	checked := 0
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checked++
		isI18n := strings.HasPrefix(filepath.Base(path), "i18n")
		for lineNo, line := range strings.Split(string(data), "\n") {
			for _, r := range line {
				if name, bad := forbiddenInvisible[r]; bad {
					t.Errorf("%s:%d: verbotenes unsichtbares Zeichen U+%04X (%s)", path, lineNo+1, r, name)
				}
				if (r == 0x200C || r == 0x200D) && (!isI18n || !strings.Contains(line, "\"")) {
					t.Errorf("%s:%d: U+%04X nur in Übersetzungstexten erlaubt", path, lineNo+1, r)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 10 {
		t.Fatalf("nur %d Go-Dateien geprüft - falsches Arbeitsverzeichnis?", checked)
	}
}
