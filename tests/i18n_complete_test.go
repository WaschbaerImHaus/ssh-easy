// Paket main - Vollständigkeitsprüfung aller Übersetzungen
//
// Stellt sicher, dass JEDER UI-Text in JEDER angebotenen Sprache vorhanden
// ist - sowohl die Felder des Translations-Structs (i18n.go) als auch die
// Zusatztexte (i18n_extra.go). Zusätzlich müssen die Platzhalter (%s, %d)
// mit dem englischen Original übereinstimmen, sonst stürzt fmt.Sprintf ab
// oder zeigt "%!d(MISSING)". Ganze Sätze, die wörtlich dem Englischen
// entsprechen, gelten als vergessene Übersetzung.
//
// Dieser Test läuft im normalen Testlauf (also vor jedem Build) und wird
// zusätzlich vom Claude-Code-Hook .claude/hooks/check-translations.sh
// am Ende jeder Antwort ausgeführt, wenn sich Quellcode geändert hat.
//
// @author Kurt Ingwer
// @date   2026-10-16 10:00
package main

import (
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// formatVerbPattern findet fmt-Platzhalter wie %s, %d, %v (ohne %%).
var formatVerbPattern = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)

// wordPattern findet Wörter aus Buchstaben (für die Kopie-Erkennung).
var wordPattern = regexp.MustCompile(`\pL{2,}`)

// looksUntranslated meldet Texte, die wörtlich dem Englischen entsprechen,
// obwohl sie ganze Sätze sind (mindestens 3 Wörter). Kurze Begriffe wie
// "Host:" oder " [Tunnel: %s]" sind in vielen Sprachen gleich und erlaubt.
func looksUntranslated(lang Language, text, english string) bool {
	return lang != LangEnglish && text == english && len(wordPattern.FindAllString(english, -1)) >= 3
}

// formatVerbs liefert die sortierten Platzhalter eines Textes.
func formatVerbs(text string) []string {
	verbs := formatVerbPattern.FindAllString(strings.ReplaceAll(text, "%%", ""), -1)
	sort.Strings(verbs)
	return verbs
}

// TestAllTranslationsComplete prüft alle Felder des Translations-Structs.
func TestAllTranslationsComplete(t *testing.T) {
	english := reflect.ValueOf(GetTranslations(LangEnglish))
	typ := english.Type()
	for _, opt := range AvailableLanguages {
		if _, ok := allTranslations[opt.Code]; !ok {
			t.Errorf("Sprache %s (%s) hat keinen Eintrag in allTranslations", opt.Code, opt.English)
			continue
		}
		value := reflect.ValueOf(GetTranslations(opt.Code))
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.Type.Kind() != reflect.String {
				continue
			}
			text := value.Field(i).String()
			if strings.TrimSpace(text) == "" {
				t.Errorf("Übersetzung fehlt: %s (%s) Feld %s", opt.Code, opt.English, field.Name)
				continue
			}
			if looksUntranslated(opt.Code, text, english.Field(i).String()) {
				t.Errorf("nicht übersetzt (englische Kopie): %s (%s) Feld %s", opt.Code, opt.English, field.Name)
			}
			want := strings.Join(formatVerbs(english.Field(i).String()), " ")
			if got := strings.Join(formatVerbs(text), " "); got != want {
				t.Errorf("Platzhalter falsch: %s Feld %s hat [%s], Englisch [%s]", opt.Code, field.Name, got, want)
			}
		}
	}
}

// TestLooksUntranslated prüft die Kopie-Erkennung selbst.
func TestLooksUntranslated(t *testing.T) {
	if !looksUntranslated(LangDeutsch, "This name is already in use", "This name is already in use") {
		t.Error("englische Satz-Kopie nicht erkannt")
	}
	if looksUntranslated(LangDeutsch, " [Tunnel: %s]", " [Tunnel: %s]") || looksUntranslated(LangEnglish, "a b c d", "a b c d") {
		t.Error("kurze Begriffe bzw. Englisch selbst fälschlich gemeldet")
	}
	if looksUntranslated(LangDeutsch, "Dieser Name ist vergeben", "This name is already in use") {
		t.Error("echte Übersetzung fälschlich gemeldet")
	}
}

// TestAllExtraTranslationsComplete prüft alle Zusatztexte aus i18n_extra.go.
func TestAllExtraTranslationsComplete(t *testing.T) {
	english := extraTranslations[LangEnglish]
	if len(english) == 0 {
		t.Fatal("keine englischen Zusatztexte gefunden")
	}
	keys := make([]string, 0, len(english))
	for key := range english {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)

	for _, opt := range AvailableLanguages {
		texts, ok := extraTranslations[opt.Code]
		if !ok {
			t.Errorf("Zusatztexte fehlen komplett: %s (%s)", opt.Code, opt.English)
			continue
		}
		for _, key := range keys {
			text := texts[textKey(key)]
			if strings.TrimSpace(text) == "" {
				t.Errorf("Zusatztext fehlt: %s (%s) Schlüssel %s", opt.Code, opt.English, key)
				continue
			}
			if looksUntranslated(opt.Code, text, english[textKey(key)]) {
				t.Errorf("nicht übersetzt (englische Kopie): %s (%s) Schlüssel %s", opt.Code, opt.English, key)
			}
			want := strings.Join(formatVerbs(english[textKey(key)]), " ")
			if got := strings.Join(formatVerbs(text), " "); got != want {
				t.Errorf("Platzhalter falsch: %s Schlüssel %s hat [%s], Englisch [%s]", opt.Code, key, got, want)
			}
		}
		// Keine Schlüssel, die es im Englischen nicht gibt (Tippfehler)
		for key := range texts {
			if _, ok := english[key]; !ok {
				t.Errorf("unbekannter Schlüssel %s in Sprache %s", key, opt.Code)
			}
		}
	}
}
