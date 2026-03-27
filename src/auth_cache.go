// Paket main - Auth-Methoden-Cache für ssh-easy
//
// Merkt sich für jede Verbindung welche SSH-Authentifizierungsmethode
// (Agent oder konkreter Schlüsselpfad) zuletzt erfolgreich war.
// Beim nächsten Verbindungsaufbau wird diese Methode zuerst probiert,
// was unnötige Versuche spart. Nach 2 aufeinanderfolgenden Fehlern
// mit der gecachten Methode wird der Eintrag gelöscht und alle Methoden
// werden neu durchprobiert.
//
// Cache-Datei: ~/.ssh-easy/auth-cache.json
// Format: map[connection-id] → AuthCacheEntry
//
// @author Kurt Ingwer
// @date   2026-03-23 12:00
package main

import (
	"encoding/json"
	"os"
	"sync"
)

// authCacheMethodAgent ist der Cache-Schlüssel für SSH-Agent-Authentifizierung.
const authCacheMethodAgent = "agent"

// authCacheKeyPrefix ist das Präfix für schlüsselbasierte Methoden im Cache.
// Format: "key:/absoluter/pfad/zum/schluessel"
const authCacheKeyPrefix = "key:"

// maxAuthFailures ist die Anzahl aufeinanderfolgender Fehlversuche,
// nach der ein Cache-Eintrag als ungültig gilt und gelöscht wird.
const maxAuthFailures = 2

// AuthCacheEntry speichert die zuletzt erfolgreiche Auth-Methode einer Verbindung.
//
// @date 2026-03-23 12:00
type AuthCacheEntry struct {
	// Method: "agent" oder "key:/abs/pfad"
	Method string `json:"method"`
	// FailureCount: Anzahl aufeinanderfolgender Fehlversuche mit dieser Methode.
	// Erreicht dieser Wert maxAuthFailures, wird der Eintrag gelöscht.
	FailureCount int `json:"failure_count"`
}

// AuthCache verwaltet den persistenten Auth-Methoden-Cache.
// Thread-sicher durch RWMutex. Änderungen werden sofort auf Disk gespeichert.
//
// @date 2026-03-23 12:00
type AuthCache struct {
	// mu schützt entries vor gleichzeitigem Zugriff
	mu sync.RWMutex
	// entries bildet Connection.ID auf den zuletzt erfolgreichen Auth-Eintrag ab
	entries map[string]*AuthCacheEntry
	// path ist der absolute Pfad zur Cache-Datei
	path string
}

// NewAuthCache erstellt einen neuen AuthCache und lädt bestehende Einträge
// aus der angegebenen Datei. Falls die Datei nicht existiert, startet der
// Cache leer (kein Fehler).
//
// @param path - Absoluter Pfad zur Cache-Datei (z.B. ~/.ssh-easy/auth-cache.json)
// @return *AuthCache - Initialisierter Cache
// @date 2026-03-23 12:00
func NewAuthCache(path string) *AuthCache {
	ac := &AuthCache{
		entries: make(map[string]*AuthCacheEntry),
		path:    path,
	}
	ac.load()
	return ac
}

// load liest den Cache aus der JSON-Datei ein.
// Fehler werden still ignoriert – der Cache startet dann einfach leer.
//
// @date 2026-03-23 12:00
func (ac *AuthCache) load() {
	data, err := os.ReadFile(ac.path)
	if err != nil {
		// Datei existiert noch nicht – normaler Erstzustand
		return
	}
	var entries map[string]*AuthCacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		// Defekte Datei – ignorieren, Cache bleibt leer
		return
	}
	ac.entries = entries
}

// save schreibt den aktuellen Cache in die JSON-Datei.
// Muss unter gehaltenem Schreib-Lock aufgerufen werden.
//
// @date 2026-03-23 12:00
func (ac *AuthCache) save() {
	data, err := json.MarshalIndent(ac.entries, "", "  ")
	if err != nil {
		return
	}
	// 0600: nur der eigene Benutzer darf lesen/schreiben (Auth-Daten)
	_ = os.WriteFile(ac.path, data, 0600)
}

// Get gibt den Cache-Eintrag für eine Verbindung zurück.
// Gibt nil zurück, wenn kein Eintrag vorhanden ist oder zu viele
// Fehler aufgezeichnet wurden.
//
// @param connID - Connection.ID der gesuchten Verbindung
// @return *AuthCacheEntry - Gültiger Eintrag oder nil
// @date 2026-03-23 12:00
func (ac *AuthCache) Get(connID string) *AuthCacheEntry {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	entry, ok := ac.entries[connID]
	if !ok {
		return nil
	}
	// Eintrag mit zu vielen Fehlern gilt als ungültig
	if entry.FailureCount >= maxAuthFailures {
		return nil
	}
	return entry
}

// RecordSuccess speichert eine erfolgreiche Auth-Methode für eine Verbindung
// und setzt den Fehlerzähler zurück. Überschreibt einen vorhandenen Eintrag.
//
// @param connID  - Connection.ID der Verbindung
// @param method  - Erfolgreiche Methode ("agent" oder "key:/pfad")
// @date 2026-03-23 12:00
func (ac *AuthCache) RecordSuccess(connID, method string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.entries[connID] = &AuthCacheEntry{
		Method:       method,
		FailureCount: 0,
	}
	ac.save()
}

// RecordFailure erhöht den Fehlerzähler für eine Verbindung.
// Erreicht der Zähler maxAuthFailures, wird der Eintrag gelöscht,
// damit beim nächsten Versuch alle Methoden neu durchprobiert werden.
//
// @param connID - Connection.ID der Verbindung
// @date 2026-03-23 12:00
func (ac *AuthCache) RecordFailure(connID string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	entry, ok := ac.entries[connID]
	if !ok {
		return
	}
	entry.FailureCount++
	if entry.FailureCount >= maxAuthFailures {
		// Zu viele Fehler: Eintrag entfernen, nächstes Mal alles neu probieren
		delete(ac.entries, connID)
	}
	ac.save()
}

// Remove löscht den Cache-Eintrag für eine Verbindung manuell.
// Wird z.B. aufgerufen wenn eine Verbindung aus der Konfiguration entfernt wird.
//
// @param connID - Connection.ID der zu entfernenden Verbindung
// @date 2026-03-23 12:00
func (ac *AuthCache) Remove(connID string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	delete(ac.entries, connID)
	ac.save()
}
