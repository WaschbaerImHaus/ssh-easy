# Sicherheitsreport - ssh-easy
**Scan-Zeitpunkt:** 2026-10-15 19:13:51
**Scanner-Version:** 2.0.0
**Scan-Modus:** vollstaendig

## Projektinformationen

- **Sprachen:** Go, Shell
- **Frameworks:** Keine erkannt
- **Paketmanager:** go modules
- **Docker:** Nein
- **CI/CD:** Ja
- **Tests vorhanden:** Ja

---

## Zusammenfassung

| Schweregrad | Anzahl |
|-------------|--------|
| KRITISCH    | 0 |
| HOCH        | 0 |
| MITTEL      | 10 |
| NIEDRIG     | 44 |
| INFO        | 17 |
| **GESAMT**  | **71** |

### Findings bewerten (False Positive / Irrelevant)

Ein Finding kann auf drei Wegen markiert werden:

- **CLI (einzeln):** `python3 /home/claude-code/project/worker-security/src/scanner/triage_cli.py mark /home/claude-code/project/ssh-easy <INDEX> fp --grund "..."`
- **CLI (Regel):** `python3 /home/claude-code/project/worker-security/src/scanner/triage_cli.py regel "<Titel>" fp --grund "..."`
- **Dashboard:** http://127.0.0.1:9090 (Projektkarte oeffnen und per Klick bewerten)

Status-Aliase: `fp` = False Positive, `ignoriert` = irrelevant, `bestaetigt` = echter Befund, `offen` = noch nicht geprueft. `list` zeigt alle Findings mit Index.

---

## Statische Code-Analyse (SAST)

## [2026-10-15 19:23:23] - Scan-Ergebnis für ssh-easy

**Gefundene Schwachstellen:** 68

- **MITTEL:** 7
- **NIEDRIG:** 44
- **INFO:** 17

---

### 1. [2026-10-15 19:23:23] - MITTEL - Verwendung von SHA1 (kryptographisch gebrochen)

**Schweregrad:** MITTEL
**CVSS Score:** 5.3
**Kategorie:** Schwache Kryptographie
**CWE:** CWE-328
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 15

#### Betroffener Code
```
"crypto/sha1"
```

#### Beschreibung
SHA1 ist kryptographisch gebrochen (SHAttered-Angriff 2017). Nicht für Signaturen, Zertifikate oder Integritätsprüfungen geeignet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um schwache kryptographie-basierte Angriffe durchzuführen.

#### Lösung
crypto/sha256 oder crypto/sha512 verwenden.

#### Referenzen
- https://cwe.mitre.org/data/definitions/328.html

---

### 2. [2026-10-15 19:23:23] - MITTEL - io.Copy ohne Größenlimit (DoS-Risiko)

**Schweregrad:** MITTEL
**CVSS Score:** 5.3
**Kategorie:** Denial of Service
**CWE:** CWE-770
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 115

#### Betroffener Code
```
io.Copy(remoteConn, localConn)
```

#### Beschreibung
io.Copy() kopiert ohne Begrenzung bis EOF. Bei Netzwerk-Streams kann ein Angreifer unbegrenzt Daten senden und den Speicher des Servers erschöpfen (Out of Memory).

Mehrfach gemeldet: 2 Muster melden diese Stelle als denselben Sachverhalt. Zusammengefasst zu einem Befund (hoechster Schweregrad und CVSS der Gruppe):
- io.Copy ohne Größenlimit (DoS-Risiko)
- io.Copy() ohne Größenlimit

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um denial of service-basierte Angriffe durchzuführen.

#### Lösung
io.LimitReader() verwenden um die maximale Datenmenge zu begrenzen: io.Copy(dst, io.LimitReader(src, maxBytes)). Alternativ io.CopyN() mit explizitem Limit nutzen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/770.html

---

### 3. [2026-10-15 19:23:23] - MITTEL - io.Copy ohne Größenlimit (DoS-Risiko)

**Schweregrad:** MITTEL
**CVSS Score:** 5.3
**Kategorie:** Denial of Service
**CWE:** CWE-770
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 120

#### Betroffener Code
```
io.Copy(localConn, remoteConn)
```

#### Beschreibung
io.Copy() kopiert ohne Begrenzung bis EOF. Bei Netzwerk-Streams kann ein Angreifer unbegrenzt Daten senden und den Speicher des Servers erschöpfen (Out of Memory).

Mehrfach gemeldet: 2 Muster melden diese Stelle als denselben Sachverhalt. Zusammengefasst zu einem Befund (hoechster Schweregrad und CVSS der Gruppe):
- io.Copy ohne Größenlimit (DoS-Risiko)
- io.Copy() ohne Größenlimit

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um denial of service-basierte Angriffe durchzuführen.

#### Lösung
io.LimitReader() verwenden um die maximale Datenmenge zu begrenzen: io.Copy(dst, io.LimitReader(src, maxBytes)). Alternativ io.CopyN() mit explizitem Limit nutzen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/770.html

---

### 4. [2026-10-15 19:23:23] - MITTEL - Fehler-Rückgabewert ignoriert (Blank Identifier)

**Schweregrad:** MITTEL
**CVSS Score:** 4.3
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 563

#### Betroffener Code
```
_ = os.Remove(keyPath)
```

#### Beschreibung
Fehler-Rückgabewerte werden mit _ ignoriert. Dies kann zu unerwartetem Verhalten und 'Fail Open'-Szenarien führen (OWASP 2025 A10: Mishandling of Exceptional Conditions).

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Alle Fehler-Rückgabewerte prüfen und angemessen behandeln: if err != nil { return err }

#### Referenzen
- https://cwe.mitre.org/data/definitions/754.html

---

### 5. [2026-10-15 19:23:23] - MITTEL - Fehler-Rückgabewert ignoriert (Blank Identifier)

**Schweregrad:** MITTEL
**CVSS Score:** 4.3
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 564

#### Betroffener Code
```
_ = os.Remove(pubKeyPath)
```

#### Beschreibung
Fehler-Rückgabewerte werden mit _ ignoriert. Dies kann zu unerwartetem Verhalten und 'Fail Open'-Szenarien führen (OWASP 2025 A10: Mishandling of Exceptional Conditions).

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Alle Fehler-Rückgabewerte prüfen und angemessen behandeln: if err != nil { return err }

#### Referenzen
- https://cwe.mitre.org/data/definitions/754.html

---

### 6. [2026-10-15 19:23:23] - MITTEL - context.Background() ohne Timeout/Deadline

**Schweregrad:** MITTEL
**CVSS Score:** 4.3
**Kategorie:** Context-Handling
**CWE:** CWE-400
**Betroffene Datei:** `src/ssh_manager.go`
**Zeile:** 430

#### Betroffener Code
```
ctx, cancel := context.WithCancel(context.Background())
```

#### Beschreibung
context.Background() hat kein Timeout und kein Deadline. In externen Aufrufen kann dies zu unbegrenztem Warten fuehren (Goroutine-Leak, DoS).

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um context-handling-basierte Angriffe durchzuführen.

#### Lösung
context.WithTimeout() oder context.WithDeadline() verwenden. In HTTP-Handlern r.Context() statt context.Background() nutzen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/400.html

---

### 7. [2026-10-15 19:23:23] - MITTEL - context.Background() statt Request-Context

**Schweregrad:** MITTEL
**CVSS Score:** 4.0
**Kategorie:** Context-Analyse
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh_manager.go`
**Zeile:** 430

#### Betroffener Code
```
	ctx, cancel := context.WithCancel(context.Background())
```

#### Beschreibung
context.Background() wird verwendet. Pruefen ob ein spezifischerer Context verfuegbar ist.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um context-analyse-basierte Angriffe durchzuführen.

#### Lösung
In HTTP-Handlern r.Context() oder c.Request().Context() statt context.Background() verwenden.

#### Referenzen
- https://cwe.mitre.org/data/definitions/404.html

---

### 8. [2026-10-15 19:23:23] - NIEDRIG - defer Close() ohne Fehlerprüfung

**Schweregrad:** NIEDRIG
**CVSS Score:** 2.0
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 102

#### Betroffener Code
```
defer localConn.Close()
```

#### Beschreibung
defer file.Close() ignoriert den Fehler-Rückgabewert. Bei Schreiboperationen können Daten verloren gehen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
defer func() { if err := f.Close(); err != nil { log.Error(err) } }()

#### Referenzen
- https://cwe.mitre.org/data/definitions/754.html

---

### 9. [2026-10-15 19:23:23] - NIEDRIG - defer Close() ohne Fehlerprüfung

**Schweregrad:** NIEDRIG
**CVSS Score:** 2.0
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 108

#### Betroffener Code
```
defer remoteConn.Close()
```

#### Beschreibung
defer file.Close() ignoriert den Fehler-Rückgabewert. Bei Schreiboperationen können Daten verloren gehen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
defer func() { if err := f.Close(); err != nil { log.Error(err) } }()

#### Referenzen
- https://cwe.mitre.org/data/definitions/754.html

---

### 10. [2026-10-15 19:23:23] - NIEDRIG - defer Close() ohne Fehlerprüfung

**Schweregrad:** NIEDRIG
**CVSS Score:** 2.0
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 138

#### Betroffener Code
```
defer f.Close()
```

#### Beschreibung
defer file.Close() ignoriert den Fehler-Rückgabewert. Bei Schreiboperationen können Daten verloren gehen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
defer func() { if err := f.Close(); err != nil { log.Error(err) } }()

#### Referenzen
- https://cwe.mitre.org/data/definitions/754.html

---

### 11. [2026-10-15 19:23:23] - NIEDRIG - defer Close() ohne Fehlerprüfung

**Schweregrad:** NIEDRIG
**CVSS Score:** 2.0
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 426

#### Betroffener Code
```
defer session.Close()
```

#### Beschreibung
defer file.Close() ignoriert den Fehler-Rückgabewert. Bei Schreiboperationen können Daten verloren gehen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
defer func() { if err := f.Close(); err != nil { log.Error(err) } }()

#### Referenzen
- https://cwe.mitre.org/data/definitions/754.html

---

### 12. [2026-10-15 19:23:23] - NIEDRIG - defer Close() ohne Fehlerprüfung

**Schweregrad:** NIEDRIG
**CVSS Score:** 2.0
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 551

#### Betroffener Code
```
defer session.Close()
```

#### Beschreibung
defer file.Close() ignoriert den Fehler-Rückgabewert. Bei Schreiboperationen können Daten verloren gehen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
defer func() { if err := f.Close(); err != nil { log.Error(err) } }()

#### Referenzen
- https://cwe.mitre.org/data/definitions/754.html

---

### 13. [2026-10-15 19:23:23] - NIEDRIG - Error-Rückgabewert ignoriert (_)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 563

#### Betroffener Code
```
_ = os.Remove(keyPath)
```

#### Beschreibung
Error-Rückgabewerte werden mit _ ignoriert (CWE-252). Unbehandelte Fehler können zu undefiniertem Verhalten führen.

Mehrfach gemeldet: 2 Muster melden diese Stelle als denselben Sachverhalt. Zusammengefasst zu einem Befund (hoechster Schweregrad und CVSS der Gruppe):
- Error-Rückgabewert ignoriert (_)
- Ignorierter Error-Rueckgabewert: os.Remove()

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Fehler prüfen und angemessen behandeln: if err != nil { return fmt.Errorf(...) }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 14. [2026-10-15 19:23:23] - NIEDRIG - Error-Rückgabewert ignoriert (_)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 564

#### Betroffener Code
```
_ = os.Remove(pubKeyPath)
```

#### Beschreibung
Error-Rückgabewerte werden mit _ ignoriert (CWE-252). Unbehandelte Fehler können zu undefiniertem Verhalten führen.

Mehrfach gemeldet: 2 Muster melden diese Stelle als denselben Sachverhalt. Zusammengefasst zu einem Befund (hoechster Schweregrad und CVSS der Gruppe):
- Error-Rückgabewert ignoriert (_)
- Ignorierter Error-Rueckgabewert: os.Remove()

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Fehler prüfen und angemessen behandeln: if err != nil { return fmt.Errorf(...) }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 15. [2026-10-15 19:23:23] - NIEDRIG - Ignorierter Error-Rueckgabewert: fmt.Fprintf()

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.0
**Kategorie:** Error Handling
**CWE:** CWE-252
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 141

#### Betroffener Code
```
	_, err = fmt.Fprintf(f, "%s\n", line)
```

#### Beschreibung
Der Fehler-Rueckgabewert von fmt.Fprintf() wird mit '_' ignoriert. Unbehandelte Fehler koennen zu unerwartetem Verhalten und Sicherheitsproblemen fuehren.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um error handling-basierte Angriffe durchzuführen.

#### Lösung
Error-Rueckgabewert von fmt.Fprintf() pruefen und angemessen behandeln.

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 16. [2026-10-15 19:23:23] - NIEDRIG - Error-Rückgabewert ignoriert (_)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/osc52.go`
**Zeile:** 396

#### Betroffener Code
```
_ = outFilter.Flush()
```

#### Beschreibung
Error-Rückgabewerte werden mit _ ignoriert (CWE-252). Unbehandelte Fehler können zu undefiniertem Verhalten führen.

Mehrfach gemeldet: 2 Muster melden diese Stelle als denselben Sachverhalt. Zusammengefasst zu einem Befund (hoechster Schweregrad und CVSS der Gruppe):
- Error-Rückgabewert ignoriert (_)
- Ignorierter Error-Rueckgabewert: outFilter.Flush()

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Fehler prüfen und angemessen behandeln: if err != nil { return fmt.Errorf(...) }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 17. [2026-10-15 19:23:23] - NIEDRIG - Error-Rückgabewert ignoriert (_)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/osc52.go`
**Zeile:** 397

#### Betroffener Code
```
_ = errFilter.Flush()
```

#### Beschreibung
Error-Rückgabewerte werden mit _ ignoriert (CWE-252). Unbehandelte Fehler können zu undefiniertem Verhalten führen.

Mehrfach gemeldet: 2 Muster melden diese Stelle als denselben Sachverhalt. Zusammengefasst zu einem Befund (hoechster Schweregrad und CVSS der Gruppe):
- Error-Rückgabewert ignoriert (_)
- Ignorierter Error-Rueckgabewert: errFilter.Flush()

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Fehler prüfen und angemessen behandeln: if err != nil { return fmt.Errorf(...) }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 18. [2026-10-15 19:23:23] - NIEDRIG - Ignorierter Error-Rueckgabewert: w.out.Write()

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.0
**Kategorie:** Error Handling
**CWE:** CWE-252
**Betroffene Datei:** `src/osc52.go`
**Zeile:** 371

#### Betroffener Code
```
		_, err := w.out.Write(osc52Intro[:introLen])
```

#### Beschreibung
Der Fehler-Rueckgabewert von w.out.Write() wird mit '_' ignoriert. Unbehandelte Fehler koennen zu unerwartetem Verhalten und Sicherheitsproblemen fuehren.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um error handling-basierte Angriffe durchzuführen.

#### Lösung
Error-Rueckgabewert von w.out.Write() pruefen und angemessen behandeln.

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 19. [2026-10-15 19:23:23] - NIEDRIG - defer Close() ohne Fehlerprüfung

**Schweregrad:** NIEDRIG
**CVSS Score:** 2.0
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/ssh_terminal.go`
**Zeile:** 119

#### Betroffener Code
```
defer session.Close()
```

#### Beschreibung
defer file.Close() ignoriert den Fehler-Rückgabewert. Bei Schreiboperationen können Daten verloren gehen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
defer func() { if err := f.Close(); err != nil { log.Error(err) } }()

#### Referenzen
- https://cwe.mitre.org/data/definitions/754.html

---

### 20. [2026-10-15 19:23:23] - NIEDRIG - defer Close() ohne Fehlerprüfung

**Schweregrad:** NIEDRIG
**CVSS Score:** 2.0
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/ssh_terminal.go`
**Zeile:** 140

#### Betroffener Code
```
defer stdinPipe.Close()
```

#### Beschreibung
defer file.Close() ignoriert den Fehler-Rückgabewert. Bei Schreiboperationen können Daten verloren gehen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
defer func() { if err := f.Close(); err != nil { log.Error(err) } }()

#### Referenzen
- https://cwe.mitre.org/data/definitions/754.html

---

### 21. [2026-10-15 19:23:23] - NIEDRIG - Error-Rückgabewert ignoriert (_)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/ssh_terminal.go`
**Zeile:** 236

#### Betroffener Code
```
_ = session.WindowChange(h, w)
```

#### Beschreibung
Error-Rückgabewerte werden mit _ ignoriert (CWE-252). Unbehandelte Fehler können zu undefiniertem Verhalten führen.

Mehrfach gemeldet: 2 Muster melden diese Stelle als denselben Sachverhalt. Zusammengefasst zu einem Befund (hoechster Schweregrad und CVSS der Gruppe):
- Error-Rückgabewert ignoriert (_)
- Ignorierter Error-Rueckgabewert: session.WindowChange()

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Fehler prüfen und angemessen behandeln: if err != nil { return fmt.Errorf(...) }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 22. [2026-10-15 19:23:23] - NIEDRIG - Ignorierter Error-Rueckgabewert: setConsoleWindowInfo()

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.0
**Kategorie:** Error Handling
**CWE:** CWE-252
**Betroffene Datei:** `src/console_scroll_windows.go`
**Zeile:** 75

#### Betroffener Code
```
	_ = setConsoleWindowInfo(handle, true, &rect)
```

#### Beschreibung
Der Fehler-Rueckgabewert von setConsoleWindowInfo() wird mit '_' ignoriert. Unbehandelte Fehler koennen zu unerwartetem Verhalten und Sicherheitsproblemen fuehren.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um error handling-basierte Angriffe durchzuführen.

#### Lösung
Error-Rueckgabewert von setConsoleWindowInfo() pruefen und angemessen behandeln.

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 23. [2026-10-15 19:23:23] - NIEDRIG - Ignorierter Error-Rueckgabewert: SaveConfig()

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.0
**Kategorie:** Error Handling
**CWE:** CWE-252
**Betroffene Datei:** `src/tui_language.go`
**Zeile:** 80

#### Betroffener Code
```
	_ = SaveConfig(m.configPath, cfg)
```

#### Beschreibung
Der Fehler-Rueckgabewert von SaveConfig() wird mit '_' ignoriert. Unbehandelte Fehler koennen zu unerwartetem Verhalten und Sicherheitsproblemen fuehren.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um error handling-basierte Angriffe durchzuführen.

#### Lösung
Error-Rueckgabewert von SaveConfig() pruefen und angemessen behandeln.

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 24. [2026-10-15 19:23:23] - NIEDRIG - Go Error-Wert mit _ ignoriert (OWASP A10:2025)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/console_font_windows.go`
**Zeile:** 59

#### Betroffener Code
```
ret, _, _ := procGetCurrentConsoleFontEx.Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&info)))
```

#### Beschreibung
Der Error-Rückgabewert einer Funktion wird explizit mit _ verworfen. Fehler können unbemerkt bleiben und zu unerwartetem Verhalten führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Error-Wert prüfen und behandeln: if err != nil { return err }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 25. [2026-10-15 19:23:23] - NIEDRIG - Go Error-Wert mit _ ignoriert (OWASP A10:2025)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/console_font_windows.go`
**Zeile:** 83

#### Betroffener Code
```
ret, _, _ := procGetCurrentConsoleFontEx.Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&info)))
```

#### Beschreibung
Der Error-Rückgabewert einer Funktion wird explizit mit _ verworfen. Fehler können unbemerkt bleiben und zu unerwartetem Verhalten führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Error-Wert prüfen und behandeln: if err != nil { return err }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 26. [2026-10-15 19:23:23] - NIEDRIG - Error-Rückgabewert ignoriert (_)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/console_font_windows.go`
**Zeile:** 92

#### Betroffener Code
```
ret, _, _ = procSetCurrentConsoleFontEx.Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&info)))
```

#### Beschreibung
Error-Rückgabewerte werden mit _ ignoriert (CWE-252). Unbehandelte Fehler können zu undefiniertem Verhalten führen.

Mehrfach gemeldet: 2 Muster melden diese Stelle als denselben Sachverhalt. Zusammengefasst zu einem Befund (hoechster Schweregrad und CVSS der Gruppe):
- Error-Rückgabewert ignoriert (_)
- Go Error-Wert mit _ ignoriert (OWASP A10:2025)

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Fehler prüfen und angemessen behandeln: if err != nil { return fmt.Errorf(...) }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 27. [2026-10-15 19:23:23] - NIEDRIG - defer Close() ohne Fehlerprüfung

**Schweregrad:** NIEDRIG
**CVSS Score:** 2.0
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/logger.go`
**Zeile:** 99

#### Betroffener Code
```
defer f.Close()
```

#### Beschreibung
defer file.Close() ignoriert den Fehler-Rückgabewert. Bei Schreiboperationen können Daten verloren gehen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
defer func() { if err := f.Close(); err != nil { log.Error(err) } }()

#### Referenzen
- https://cwe.mitre.org/data/definitions/754.html

---

### 28. [2026-10-15 19:23:23] - NIEDRIG - Ignorierter Error-Rueckgabewert: restrictFilePermissions()

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.0
**Kategorie:** Error Handling
**CWE:** CWE-252
**Betroffene Datei:** `src/logger.go`
**Zeile:** 105

#### Betroffener Code
```
		_ = restrictFilePermissions(l.filePath)
```

#### Beschreibung
Der Fehler-Rueckgabewert von restrictFilePermissions() wird mit '_' ignoriert. Unbehandelte Fehler koennen zu unerwartetem Verhalten und Sicherheitsproblemen fuehren.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um error handling-basierte Angriffe durchzuführen.

#### Lösung
Error-Rueckgabewert von restrictFilePermissions() pruefen und angemessen behandeln.

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 29. [2026-10-15 19:23:23] - NIEDRIG - Ignorierter Error-Rueckgabewert: cmd.Process.Release()

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.0
**Kategorie:** Error Handling
**CWE:** CWE-252
**Betroffene Datei:** `src/terminal_launcher_unix.go`
**Zeile:** 148

#### Betroffener Code
```
	_ = cmd.Process.Release()
```

#### Beschreibung
Der Fehler-Rueckgabewert von cmd.Process.Release() wird mit '_' ignoriert. Unbehandelte Fehler koennen zu unerwartetem Verhalten und Sicherheitsproblemen fuehren.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um error handling-basierte Angriffe durchzuführen.

#### Lösung
Error-Rueckgabewert von cmd.Process.Release() pruefen und angemessen behandeln.

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 30. [2026-10-15 19:23:23] - NIEDRIG - Error-Rückgabewert ignoriert (_)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/flush_stdin_windows.go`
**Zeile:** 27

#### Betroffener Code
```
_ = windows.FlushConsoleInputBuffer(handle)
```

#### Beschreibung
Error-Rückgabewerte werden mit _ ignoriert (CWE-252). Unbehandelte Fehler können zu undefiniertem Verhalten führen.

Mehrfach gemeldet: 2 Muster melden diese Stelle als denselben Sachverhalt. Zusammengefasst zu einem Befund (hoechster Schweregrad und CVSS der Gruppe):
- Error-Rückgabewert ignoriert (_)
- Ignorierter Error-Rueckgabewert: windows.FlushConsoleInputBuffer()

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Fehler prüfen und angemessen behandeln: if err != nil { return fmt.Errorf(...) }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 31. [2026-10-15 19:23:23] - NIEDRIG - Go Error-Wert mit _ ignoriert (OWASP A10:2025)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/icon_windows.go`
**Zeile:** 94

#### Betroffener Code
```
hwnd, _, _ := procGetConsoleWindow.Call()
```

#### Beschreibung
Der Error-Rückgabewert einer Funktion wird explizit mit _ verworfen. Fehler können unbemerkt bleiben und zu unerwartetem Verhalten führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Error-Wert prüfen und behandeln: if err != nil { return err }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 32. [2026-10-15 19:23:23] - NIEDRIG - Go Error-Wert mit _ ignoriert (OWASP A10:2025)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/icon_windows.go`
**Zeile:** 101

#### Betroffener Code
```
ret, _, _ := procAllocConsole.Call()
```

#### Beschreibung
Der Error-Rückgabewert einer Funktion wird explizit mit _ verworfen. Fehler können unbemerkt bleiben und zu unerwartetem Verhalten führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Error-Wert prüfen und behandeln: if err != nil { return err }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 33. [2026-10-15 19:23:23] - NIEDRIG - Error-Rückgabewert ignoriert (_)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/icon_windows.go`
**Zeile:** 136

#### Betroffener Code
```
_ = windows.SetStdHandle(windows.STD_INPUT_HANDLE, conin)
```

#### Beschreibung
Error-Rückgabewerte werden mit _ ignoriert (CWE-252). Unbehandelte Fehler können zu undefiniertem Verhalten führen.

Mehrfach gemeldet: 2 Muster melden diese Stelle als denselben Sachverhalt. Zusammengefasst zu einem Befund (hoechster Schweregrad und CVSS der Gruppe):
- Error-Rückgabewert ignoriert (_)
- Ignorierter Error-Rueckgabewert: windows.SetStdHandle()

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Fehler prüfen und angemessen behandeln: if err != nil { return fmt.Errorf(...) }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 34. [2026-10-15 19:23:23] - NIEDRIG - Error-Rückgabewert ignoriert (_)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/icon_windows.go`
**Zeile:** 137

#### Betroffener Code
```
_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, conout)
```

#### Beschreibung
Error-Rückgabewerte werden mit _ ignoriert (CWE-252). Unbehandelte Fehler können zu undefiniertem Verhalten führen.

Mehrfach gemeldet: 2 Muster melden diese Stelle als denselben Sachverhalt. Zusammengefasst zu einem Befund (hoechster Schweregrad und CVSS der Gruppe):
- Error-Rückgabewert ignoriert (_)
- Ignorierter Error-Rueckgabewert: windows.SetStdHandle()

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Fehler prüfen und angemessen behandeln: if err != nil { return fmt.Errorf(...) }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 35. [2026-10-15 19:23:23] - NIEDRIG - Error-Rückgabewert ignoriert (_)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/icon_windows.go`
**Zeile:** 138

#### Betroffener Code
```
_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, conout)
```

#### Beschreibung
Error-Rückgabewerte werden mit _ ignoriert (CWE-252). Unbehandelte Fehler können zu undefiniertem Verhalten führen.

Mehrfach gemeldet: 2 Muster melden diese Stelle als denselben Sachverhalt. Zusammengefasst zu einem Befund (hoechster Schweregrad und CVSS der Gruppe):
- Error-Rückgabewert ignoriert (_)
- Ignorierter Error-Rueckgabewert: windows.SetStdHandle()

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Fehler prüfen und angemessen behandeln: if err != nil { return fmt.Errorf(...) }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 36. [2026-10-15 19:23:23] - NIEDRIG - Error-Rückgabewert ignoriert (_)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/icon_windows.go`
**Zeile:** 156

#### Betroffener Code
```
_ = windows.SetConsoleMode(conout, outMode|
```

#### Beschreibung
Error-Rückgabewerte werden mit _ ignoriert (CWE-252). Unbehandelte Fehler können zu undefiniertem Verhalten führen.

Mehrfach gemeldet: 2 Muster melden diese Stelle als denselben Sachverhalt. Zusammengefasst zu einem Befund (hoechster Schweregrad und CVSS der Gruppe):
- Error-Rückgabewert ignoriert (_)
- Ignorierter Error-Rueckgabewert: windows.SetConsoleMode()

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Fehler prüfen und angemessen behandeln: if err != nil { return fmt.Errorf(...) }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 37. [2026-10-15 19:23:23] - NIEDRIG - Error-Rückgabewert ignoriert (_)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/icon_windows.go`
**Zeile:** 162

#### Betroffener Code
```
_ = windows.SetConsoleMode(conin, inMode|
```

#### Beschreibung
Error-Rückgabewerte werden mit _ ignoriert (CWE-252). Unbehandelte Fehler können zu undefiniertem Verhalten führen.

Mehrfach gemeldet: 2 Muster melden diese Stelle als denselben Sachverhalt. Zusammengefasst zu einem Befund (hoechster Schweregrad und CVSS der Gruppe):
- Error-Rückgabewert ignoriert (_)
- Ignorierter Error-Rueckgabewert: windows.SetConsoleMode()

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Fehler prüfen und angemessen behandeln: if err != nil { return fmt.Errorf(...) }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 38. [2026-10-15 19:23:23] - NIEDRIG - Go Error-Wert mit _ ignoriert (OWASP A10:2025)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/icon_windows.go`
**Zeile:** 167

#### Betroffener Code
```
title, _ := windows.UTF16PtrFromString("ssh-easy")
```

#### Beschreibung
Der Error-Rückgabewert einer Funktion wird explizit mit _ verworfen. Fehler können unbemerkt bleiben und zu unerwartetem Verhalten führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Error-Wert prüfen und behandeln: if err != nil { return err }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 39. [2026-10-15 19:23:23] - NIEDRIG - Go Error-Wert mit _ ignoriert (OWASP A10:2025)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/icon_windows.go`
**Zeile:** 197

#### Betroffener Code
```
hwnd, _, _ := procGetConsoleWindow.Call()
```

#### Beschreibung
Der Error-Rückgabewert einer Funktion wird explizit mit _ verworfen. Fehler können unbemerkt bleiben und zu unerwartetem Verhalten führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Error-Wert prüfen und behandeln: if err != nil { return err }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 40. [2026-10-15 19:23:23] - NIEDRIG - Go Error-Wert mit _ ignoriert (OWASP A10:2025)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/icon_windows.go`
**Zeile:** 203

#### Betroffener Code
```
hInst, _, _ := procGetModuleHandleW.Call(0)
```

#### Beschreibung
Der Error-Rückgabewert einer Funktion wird explizit mit _ verworfen. Fehler können unbemerkt bleiben und zu unerwartetem Verhalten führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Error-Wert prüfen und behandeln: if err != nil { return err }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 41. [2026-10-15 19:23:23] - NIEDRIG - Go Error-Wert mit _ ignoriert (OWASP A10:2025)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/icon_windows.go`
**Zeile:** 210

#### Betroffener Code
```
smallIcon, _, _ := procLoadImageW.Call(
```

#### Beschreibung
Der Error-Rückgabewert einer Funktion wird explizit mit _ verworfen. Fehler können unbemerkt bleiben und zu unerwartetem Verhalten führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Error-Wert prüfen und behandeln: if err != nil { return err }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 42. [2026-10-15 19:23:23] - NIEDRIG - Go Error-Wert mit _ ignoriert (OWASP A10:2025)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/icon_windows.go`
**Zeile:** 216

#### Betroffener Code
```
bigIcon, _, _ := procLoadImageW.Call(
```

#### Beschreibung
Der Error-Rückgabewert einer Funktion wird explizit mit _ verworfen. Fehler können unbemerkt bleiben und zu unerwartetem Verhalten führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Error-Wert prüfen und behandeln: if err != nil { return err }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 43. [2026-10-15 19:23:23] - NIEDRIG - Go Error-Wert mit _ ignoriert (OWASP A10:2025)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/icon_windows.go`
**Zeile:** 242

#### Betroffener Code
```
hwnd, _, _ := procGetConsoleWindow.Call()
```

#### Beschreibung
Der Error-Rückgabewert einer Funktion wird explizit mit _ verworfen. Fehler können unbemerkt bleiben und zu unerwartetem Verhalten führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Error-Wert prüfen und behandeln: if err != nil { return err }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 44. [2026-10-15 19:23:23] - NIEDRIG - Ignorierter Error-Rueckgabewert: restrictFilePermissions()

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.0
**Kategorie:** Error Handling
**CWE:** CWE-252
**Betroffene Datei:** `src/auth_cache.go`
**Zeile:** 109

#### Betroffener Code
```
	_ = restrictFilePermissions(ac.path)
```

#### Beschreibung
Der Fehler-Rueckgabewert von restrictFilePermissions() wird mit '_' ignoriert. Unbehandelte Fehler koennen zu unerwartetem Verhalten und Sicherheitsproblemen fuehren.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um error handling-basierte Angriffe durchzuführen.

#### Lösung
Error-Rueckgabewert von restrictFilePermissions() pruefen und angemessen behandeln.

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 45. [2026-10-15 19:23:23] - NIEDRIG - Go Error-Wert mit _ ignoriert (OWASP A10:2025)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/tui_status.go`
**Zeile:** 34

#### Betroffener Code
```
status, _ := m.sshManager.GetStatus(m.activeID)
```

#### Beschreibung
Der Error-Rückgabewert einer Funktion wird explizit mit _ verworfen. Fehler können unbemerkt bleiben und zu unerwartetem Verhalten führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Error-Wert prüfen und behandeln: if err != nil { return err }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 46. [2026-10-15 19:23:23] - NIEDRIG - Go Error-Wert mit _ ignoriert (OWASP A10:2025)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/tui_status.go`
**Zeile:** 60

#### Betroffener Code
```
status, _ := m.sshManager.GetStatus(m.activeID)
```

#### Beschreibung
Der Error-Rückgabewert einer Funktion wird explizit mit _ verworfen. Fehler können unbemerkt bleiben und zu unerwartetem Verhalten führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Error-Wert prüfen und behandeln: if err != nil { return err }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 47. [2026-10-15 19:23:23] - NIEDRIG - Go Error-Wert mit _ ignoriert (OWASP A10:2025)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/tui_status.go`
**Zeile:** 101

#### Betroffener Code
```
status, _ := m.sshManager.GetStatus(m.activeID)
```

#### Beschreibung
Der Error-Rückgabewert einer Funktion wird explizit mit _ verworfen. Fehler können unbemerkt bleiben und zu unerwartetem Verhalten führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Error-Wert prüfen und behandeln: if err != nil { return err }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 48. [2026-10-15 19:23:23] - NIEDRIG - defer Close() ohne Fehlerprüfung

**Schweregrad:** NIEDRIG
**CVSS Score:** 2.0
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/ssh_manager.go`
**Zeile:** 286

#### Betroffener Code
```
defer f.Close()
```

#### Beschreibung
defer file.Close() ignoriert den Fehler-Rückgabewert. Bei Schreiboperationen können Daten verloren gehen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
defer func() { if err := f.Close(); err != nil { log.Error(err) } }()

#### Referenzen
- https://cwe.mitre.org/data/definitions/754.html

---

### 49. [2026-10-15 19:23:23] - NIEDRIG - Go Error-Wert mit _ ignoriert (OWASP A10:2025)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/ssh_manager.go`
**Zeile:** 289

#### Betroffener Code
```
n, _ := f.Read(buf)
```

#### Beschreibung
Der Error-Rückgabewert einer Funktion wird explizit mit _ verworfen. Fehler können unbemerkt bleiben und zu unerwartetem Verhalten führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Error-Wert prüfen und behandeln: if err != nil { return err }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 50. [2026-10-15 19:23:23] - NIEDRIG - Hardcodierte IP-Adresse im Quellcode

**Schweregrad:** NIEDRIG
**CVSS Score:** 2.0
**Kategorie:** Hardcodierte Konfiguration
**CWE:** CWE-547
**Betroffene Datei:** `src/ssh_manager.go`
**Zeile:** 730

#### Betroffener Code
```
addr := listener.Addr().String() // "127.0.0.1:8080"
```

#### Beschreibung
IP-Adressen im Quellcode erschweren die Portabilitaet und koennen bei Infrastruktur-Aenderungen zu Ausfaellen fuehren. Ausnahme: 127.0.0.1/0.0.0.0 sind akzeptable Defaults.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um hardcodierte konfiguration-basierte Angriffe durchzuführen.

#### Lösung
IP-Adressen in Umgebungsvariablen oder Konfigurationsdateien auslagern. DNS-Namen statt IP-Adressen verwenden.

#### Referenzen
- https://cwe.mitre.org/data/definitions/547.html

---

### 51. [2026-10-15 19:23:23] - NIEDRIG - Ignorierter Error-Rueckgabewert: restrictFilePermissions()

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.0
**Kategorie:** Error Handling
**CWE:** CWE-252
**Betroffene Datei:** `src/ssh_manager.go`
**Zeile:** 840

#### Betroffener Code
```
		_ = restrictFilePermissions(knownHostsPath)
```

#### Beschreibung
Der Fehler-Rueckgabewert von restrictFilePermissions() wird mit '_' ignoriert. Unbehandelte Fehler koennen zu unerwartetem Verhalten und Sicherheitsproblemen fuehren.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um error handling-basierte Angriffe durchzuführen.

#### Lösung
Error-Rueckgabewert von restrictFilePermissions() pruefen und angemessen behandeln.

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 52. [2026-10-15 19:23:23] - INFO - Goroutine gefunden - Race-Condition-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-362
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 75

#### Betroffener Code
```
go func() {
```

#### Beschreibung
Goroutinen greifen möglicherweise auf geteilte Variablen zu. Ohne Synchronisation (Mutex, Channel, atomic) können Race Conditions auftreten (OWASP: CWE-362).

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
go test -race ausführen. sync.Mutex oder Channels für geteilte Daten verwenden. Shared State vermeiden wo möglich.

#### Referenzen
- https://cwe.mitre.org/data/definitions/362.html

---

### 53. [2026-10-15 19:23:23] - INFO - Goroutine-Aufruf - Lifecycle-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 75

#### Betroffener Code
```
go func() {
```

#### Beschreibung
Goroutinen ohne ordnungsgemäßes Lifecycle-Management können zu Goroutine-Leaks führen (Speicher und CPU werden nie freigegeben).

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
sync.WaitGroup oder context.Context mit cancel() verwenden. Sicherstellen, dass alle Goroutinen beendet werden können.

#### Referenzen
- https://cwe.mitre.org/data/definitions/404.html

---

### 54. [2026-10-15 19:23:23] - INFO - select ohne ctx.Done()-Case prüfen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 77

#### Betroffener Code
```
select {
```

#### Beschreibung
Ein select-Statement ohne case <-ctx.Done() kann nicht von außen abgebrochen werden, was zu Goroutine-Leaks führen kann.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
case <-ctx.Done(): return zum select-Statement hinzufügen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/404.html

---

### 55. [2026-10-15 19:23:23] - INFO - Goroutine-Aufruf - Lifecycle-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 86

#### Betroffener Code
```
go handleTunnelConnection(localConn, client, remoteAddr)
```

#### Beschreibung
Goroutinen ohne ordnungsgemäßes Lifecycle-Management können zu Goroutine-Leaks führen (Speicher und CPU werden nie freigegeben).

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
sync.WaitGroup oder context.Context mit cancel() verwenden. Sicherstellen, dass alle Goroutinen beendet werden können.

#### Referenzen
- https://cwe.mitre.org/data/definitions/404.html

---

### 56. [2026-10-15 19:23:23] - INFO - Goroutine gefunden - Race-Condition-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-362
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 113

#### Betroffener Code
```
go func() {
```

#### Beschreibung
Goroutinen greifen möglicherweise auf geteilte Variablen zu. Ohne Synchronisation (Mutex, Channel, atomic) können Race Conditions auftreten (OWASP: CWE-362).

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
go test -race ausführen. sync.Mutex oder Channels für geteilte Daten verwenden. Shared State vermeiden wo möglich.

#### Referenzen
- https://cwe.mitre.org/data/definitions/362.html

---

### 57. [2026-10-15 19:23:23] - INFO - Goroutine-Aufruf - Lifecycle-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 113

#### Betroffener Code
```
go func() {
```

#### Beschreibung
Goroutinen ohne ordnungsgemäßes Lifecycle-Management können zu Goroutine-Leaks führen (Speicher und CPU werden nie freigegeben).

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
sync.WaitGroup oder context.Context mit cancel() verwenden. Sicherstellen, dass alle Goroutinen beendet werden können.

#### Referenzen
- https://cwe.mitre.org/data/definitions/404.html

---

### 58. [2026-10-15 19:23:23] - INFO - Goroutine gefunden - Race-Condition-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-362
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 118

#### Betroffener Code
```
go func() {
```

#### Beschreibung
Goroutinen greifen möglicherweise auf geteilte Variablen zu. Ohne Synchronisation (Mutex, Channel, atomic) können Race Conditions auftreten (OWASP: CWE-362).

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
go test -race ausführen. sync.Mutex oder Channels für geteilte Daten verwenden. Shared State vermeiden wo möglich.

#### Referenzen
- https://cwe.mitre.org/data/definitions/362.html

---

### 59. [2026-10-15 19:23:23] - INFO - Goroutine-Aufruf - Lifecycle-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 118

#### Betroffener Code
```
go func() {
```

#### Beschreibung
Goroutinen ohne ordnungsgemäßes Lifecycle-Management können zu Goroutine-Leaks führen (Speicher und CPU werden nie freigegeben).

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
sync.WaitGroup oder context.Context mit cancel() verwenden. Sicherstellen, dass alle Goroutinen beendet werden können.

#### Referenzen
- https://cwe.mitre.org/data/definitions/404.html

---

### 60. [2026-10-15 19:23:23] - INFO - Goroutine-Aufruf - Lifecycle-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh_terminal.go`
**Zeile:** 177

#### Betroffener Code
```
go forwardStdinWithPaste(os.Stdin, stdinPipe, func() {
```

#### Beschreibung
Goroutinen ohne ordnungsgemäßes Lifecycle-Management können zu Goroutine-Leaks führen (Speicher und CPU werden nie freigegeben).

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
sync.WaitGroup oder context.Context mit cancel() verwenden. Sicherstellen, dass alle Goroutinen beendet werden können.

#### Referenzen
- https://cwe.mitre.org/data/definitions/404.html

---

### 61. [2026-10-15 19:23:23] - INFO - Goroutine gefunden - Race-Condition-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-362
**Betroffene Datei:** `src/ssh_terminal.go`
**Zeile:** 188

#### Betroffener Code
```
go func() {
```

#### Beschreibung
Goroutinen greifen möglicherweise auf geteilte Variablen zu. Ohne Synchronisation (Mutex, Channel, atomic) können Race Conditions auftreten (OWASP: CWE-362).

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
go test -race ausführen. sync.Mutex oder Channels für geteilte Daten verwenden. Shared State vermeiden wo möglich.

#### Referenzen
- https://cwe.mitre.org/data/definitions/362.html

---

### 62. [2026-10-15 19:23:23] - INFO - Goroutine-Aufruf - Lifecycle-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh_terminal.go`
**Zeile:** 188

#### Betroffener Code
```
go func() {
```

#### Beschreibung
Goroutinen ohne ordnungsgemäßes Lifecycle-Management können zu Goroutine-Leaks führen (Speicher und CPU werden nie freigegeben).

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
sync.WaitGroup oder context.Context mit cancel() verwenden. Sicherstellen, dass alle Goroutinen beendet werden können.

#### Referenzen
- https://cwe.mitre.org/data/definitions/404.html

---

### 63. [2026-10-15 19:23:23] - INFO - select ohne ctx.Done()-Case prüfen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh_terminal.go`
**Zeile:** 228

#### Betroffener Code
```
select {
```

#### Beschreibung
Ein select-Statement ohne case <-ctx.Done() kann nicht von außen abgebrochen werden, was zu Goroutine-Leaks führen kann.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
case <-ctx.Done(): return zum select-Statement hinzufügen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/404.html

---

### 64. [2026-10-15 19:23:23] - INFO - select ohne ctx.Done()-Case prüfen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/terminal_resize_unix.go`
**Zeile:** 39

#### Betroffener Code
```
select {
```

#### Beschreibung
Ein select-Statement ohne case <-ctx.Done() kann nicht von außen abgebrochen werden, was zu Goroutine-Leaks führen kann.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
case <-ctx.Done(): return zum select-Statement hinzufügen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/404.html

---

### 65. [2026-10-15 19:23:23] - INFO - select ohne ctx.Done()-Case prüfen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/terminal_resize_windows.go`
**Zeile:** 30

#### Betroffener Code
```
select {
```

#### Beschreibung
Ein select-Statement ohne case <-ctx.Done() kann nicht von außen abgebrochen werden, was zu Goroutine-Leaks führen kann.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
case <-ctx.Done(): return zum select-Statement hinzufügen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/404.html

---

### 66. [2026-10-15 19:23:23] - INFO - select ohne ctx.Done()-Case prüfen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh_manager.go`
**Zeile:** 110

#### Betroffener Code
```
select {
```

#### Beschreibung
Ein select-Statement ohne case <-ctx.Done() kann nicht von außen abgebrochen werden, was zu Goroutine-Leaks führen kann.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
case <-ctx.Done(): return zum select-Statement hinzufügen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/404.html

---

### 67. [2026-10-15 19:23:23] - INFO - select ohne ctx.Done()-Case prüfen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh_manager.go`
**Zeile:** 880

#### Betroffener Code
```
select {
```

#### Beschreibung
Ein select-Statement ohne case <-ctx.Done() kann nicht von außen abgebrochen werden, was zu Goroutine-Leaks führen kann.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
case <-ctx.Done(): return zum select-Statement hinzufügen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/404.html

---

### 68. [2026-10-15 19:23:23] - INFO - select ohne ctx.Done()-Case prüfen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh_manager.go`
**Zeile:** 948

#### Betroffener Code
```
select {
```

#### Beschreibung
Ein select-Statement ohne case <-ctx.Done() kann nicht von außen abgebrochen werden, was zu Goroutine-Leaks führen kann.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit-basierte Angriffe durchzuführen.

#### Lösung
case <-ctx.Done(): return zum select-Statement hinzufügen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/404.html

---


## Dependency-Analyse

## [2026-10-15 19:23:23] - Dependency-Analyse für ssh-easy

**Geprüfte Abhängigkeiten:** 27
**Gefundene Probleme:** 1

### 1. MITTEL - GO-2026-5932: The golang.org/x/crypto/openpgp package is unmaintained, unsafe by design, and has known security issues

**Paket:** golang.org/x/crypto (Version: 0.58.0)
**Paketmanager:** go
**Definiert in:** go.mod

**Beschreibung:** OSV.dev meldet für golang.org/x/crypto 0.58.0 das Advisory GO-2026-5932: The golang.org/x/crypto/openpgp package is unmaintained, unsafe by design, and has known security issues. Quelle: OSV (aggregiert u.a. GHSA, PYSEC, RUSTSEC, Go Vulndb). Die betroffene Versionsspanne wurde von OSV serverseitig geprüft, nicht aus einer lokalen Tabelle.

**Lösung:** Für golang.org/x/crypto ist noch kein Fix veröffentlicht. Advisory GO-2026-5932 beobachten und Einsatz prüfen.

---


## Secrets-Scan

## [2026-10-15 19:23:23] - Secrets-Scan für ssh-easy

Keine hardcodierten Geheimnisse gefunden.


## Konfigurationsanalyse

## [2026-10-15 19:23:23] - Konfigurationsanalyse für ssh-easy

Keine Konfigurationsprobleme gefunden.


## CodeQL-Analyse (Datenfluss)

### MITTEL - CodeQL: go/unhandled-writable-file-close (go/unhandled-writable-file-close)
**Datei:** /home/claude-code/project/ssh-easy/src/logger.go:99
**CWE:** CWE-252
**Beschreibung:** CodeQL (go/unhandled-writable-file-close): File handle may be writable as a result of data flow from a [call to OpenFile](1) and closing it may result in data loss upon failure, which is not handled explicitly. Writable file handle closed without error handling
**Loesung:** Siehe CodeQL-Dokumentation fuer Regel go/unhandled-writable-file-close.

### MITTEL - CodeQL: go/unhandled-writable-file-close (go/unhandled-writable-file-close)
**Datei:** /home/claude-code/project/ssh-easy/src/ssh.go:138
**CWE:** CWE-252
**Beschreibung:** CodeQL (go/unhandled-writable-file-close): File handle may be writable as a result of data flow from a [call to OpenFile](1) and closing it may result in data loss upon failure, which is not handled explicitly. Writable file handle closed without error handling
**Loesung:** Siehe CodeQL-Dokumentation fuer Regel go/unhandled-writable-file-close.

## Manuell verifizierte Befunde (2026-09-21)

### M1. [2026-09-21 14:45:00] - KRITISCH - Als "nicht erreichbar" ignorierte x/crypto-SSH-CVEs sind in diesem Projekt sehr wohl erreichbar

**Schweregrad:** KRITISCH
**CVSS Score:** 9.1
**Kategorie:** A06:2021 Vulnerable and Outdated Components / CWE-1395 Dependency on Vulnerable Third-Party Component
**Betroffene Datei(en):** src/go.mod (golang.org/x/crypto v0.48.0); src/ssh.go:29-30, src/ssh_manager.go:22-24, src/ssh_terminal.go:24, src/types.go:16, src/tui_status.go:15 (5 Dateien)
**Bewertung:** offen (bewertbar als False Positive / Irrelevant ueber `triage_cli.py mark`)

### Beschreibung
Sieben KRITISCHE CVEs in `golang.org/x/crypto` stehen in der Triage-Datenbank
auf **ignoriert**, mit der Begruendung:

> "Nicht erreichbarer Code-Pfad: CVE betrifft ausschliesslich
> golang.org/x/crypto/ssh* (OSV-Import-Pfade + Advisory-Details).
> 'go list -deps ./...' zeigt 0 ssh-Packages; x/crypto kommt nur ueber
> quic-go/chacha20 bzw. certmagic/ocsp herein. Verwundbarer Code wird nicht
> gelinkt. **NEU PRUEFEN, falls das Projekt SSH-Funktionen aufnimmt.**"

Diese Begruendung wurde fuer *ein* Projekt erhoben, wirkt aber **global fuer
alle Projekte**: Der Triage-Hash eines Dependency-Befunds wird allein aus dem
CVE-Titel gebildet (`datei_muster` und `code_snippet` sind leer), und die
Tabelle `triage_eintraege` hat keine Projektspalte. Eine projektweise
Unterscheidung ist im aktuellen Schema nicht moeglich.

In **diesem** Projekt trifft die Begruendung nicht zu. `golang.org/x/crypto/ssh`
wird direkt importiert:

  ssh-easy/src/ssh.go:29:	"golang.org/x/crypto/ssh"
  ssh-easy/src/ssh.go:30:	"golang.org/x/crypto/ssh/knownhosts"
  ssh-easy/src/ssh_terminal.go:24:	"golang.org/x/crypto/ssh"
  ssh-easy/src/tui_status.go:15:	cryptossh "golang.org/x/crypto/ssh"
  ssh-easy/src/ssh_manager.go:22:	sshagent "golang.org/x/crypto/ssh/agent"
  ssh-easy/src/ssh_manager.go:23:	"golang.org/x/crypto/ssh"
  ssh-easy/src/ssh_manager.go:24:	"golang.org/x/crypto/ssh/knownhosts"
  ssh-easy/src/types.go:16:	"golang.org/x/crypto/ssh"

`knownhosts.New()` (ssh_manager.go:845) als HostKeyCallback UND `sshagent.NewClient()` (ssh_manager.go:808) fuer Agent-Nutzung. Damit sind BEIDE verwundbaren Teilbereiche im Einsatz: die known_hosts-Auswertung (CVE-2026-42508, @revoked) und der Agent-Pfad (CVE-2026-39832/39833, Constraint-Umgehung).

Die in der Begruendung selbst formulierte Auflage ("NEU PRUEFEN, falls das
Projekt SSH-Funktionen aufnimmt") ist damit erfuellt - die Pruefung ergibt:
erreichbar.

### Auswirkung
Die betroffenen CVEs erlauben je nach Pfad die Umgehung einer
Authentifizierungsentscheidung (CVE-2026-42508: ein als `@revoked` markierter
Host-Key wird nicht durchgesetzt; CVE-2026-46595: uebersprungene
Berechtigungspruefung in `VerifiedPublicKeyCallback`), die Umgehung von
Schluessel-Beschraenkungen beim Agent-Forwarding (CVE-2026-39832/39833), die
Umgehung des physischen Anwesenheitsnachweises bei FIDO/U2F-Sicherheitsschluesseln
(CVE-2026-39831) sowie Denial-of-Service (CVE-2026-39830 Deadlock,
CVE-2026-39834 Endlosschleife).

Schwerer als die einzelnen CVEs wiegt der Umstand, dass alle Beteiligten diese
Befunde fuer abgehandelt halten: Sie erscheinen in der Triage-Liste als
"ignoriert" und fallen bei jeder Filterung auf offene Punkte heraus.

### Proof of Concept
```
$ grep -rn "golang.org/x/crypto/ssh" src --include="*.go"
  ssh-easy/src/ssh.go:29:	"golang.org/x/crypto/ssh"
  ssh-easy/src/ssh.go:30:	"golang.org/x/crypto/ssh/knownhosts"
  ssh-easy/src/ssh_terminal.go:24:	"golang.org/x/crypto/ssh"
  ssh-easy/src/tui_status.go:15:	cryptossh "golang.org/x/crypto/ssh"
  ssh-easy/src/ssh_manager.go:22:	sshagent "golang.org/x/crypto/ssh/agent"
  ssh-easy/src/ssh_manager.go:23:	"golang.org/x/crypto/ssh"
  ssh-easy/src/ssh_manager.go:24:	"golang.org/x/crypto/ssh/knownhosts"
  ssh-easy/src/types.go:16:	"golang.org/x/crypto/ssh"

$ grep "golang.org/x/crypto" src/go.mod
	golang.org/x/crypto v0.48.0
```
Die Fassung v0.48.0 liegt unterhalb des Fixes 0.52.0 aller sieben Advisories.

### Loesung
1. `golang.org/x/crypto` auf **0.52.0** oder neuer heben:
   `cd src && go get golang.org/x/crypto@v0.52.0 && go mod tidy`
   Das behebt alle sieben CVEs auf einmal und ist ein reines Patch-Update.
2. Die globale Ignorierung der sieben CVEs zurueckziehen oder projektweise
   fassen. Im Scanner ist das als BUG-032 vermerkt
   (`worker-security/BUGS.md`): Dependency-Bewertungen brauchen das Projekt
   als Teil der Finding-Koordinate - dieselbe Regel, die bei Multi-App-Reports
   schon fuer `--app` gilt.

### Referenzen
- https://pkg.go.dev/golang.org/x/crypto (Fix-Version 0.52.0)
- https://cwe.mitre.org/data/definitions/1395.html
- CVE-2026-42508, CVE-2026-39829, CVE-2026-39830, CVE-2026-39831,
  CVE-2026-39832, CVE-2026-39833, CVE-2026-39834, CVE-2026-46595, CVE-2026-46597

---

## Manuell nachgetragen (2026-09-29)

### M-2026-09-29. [2026-09-29 14:45:00] - KRITISCH - x/crypto-SSH-CVEs: Bewertung jetzt projektgenau erhoben (Nachtrag zu M1)

**Schweregrad:** KRITISCH
**CVSS Score:** 9.1
**Kategorie:** A06:2021 Vulnerable and Outdated Components / CWE-1395
**Betroffene Datei(en):** src/go.mod (golang.org/x/crypto)
**Bewertung:** bestaetigt, projektspezifisch erhoben am 29.09.2026 (bewertbar als False Positive / Irrelevant ueber `triage_cli.py mark ssh-easy <Index> fp`)

### Beschreibung
M1 (21.09.2026) hielt fest, dass die sieben KRITISCHEN `golang.org/x/crypto`-CVEs
in diesem Projekt erreichbar sind, obwohl sie global als *ignoriert* gefuehrt
wurden. Der Grund war ein Schemamangel (BUG-032): Der Triage-Hash eines
Dependency-Befunds besteht allein aus dem CVE-Titel, und die Tabelle kannte
kein Projekt. Eine fuer *ein* Projekt erhobene Bewertung wirkte fuer **alle**.

Seit Build 154 traegt `triage_eintraege` eine Spalte `projekt`. Die Bewertung
ist damit heute projektgenau nachgeholt worden: Die sieben CVEs stehen fuer
dieses Projekt auf **bestaetigt**. Die 406 Altbewertungen ohne Projektbezug
bleiben unveraendert global wirksam; es wurde keine davon umgeworfen.

**Belegt statt vermutet** - gemessen wurde nicht per grep, sondern mit
`go list -deps ./...` (GOPROXY=off, rein lokaler Modulcache):

```
golang.org/x/crypto/ssh
golang.org/x/crypto/ssh/agent
golang.org/x/crypto/ssh/knownhosts
golang.org/x/crypto/ssh/internal/bcrypt_pbkdf
```

Direkte Imports in 5 Dateien (`src/ssh.go:29-30`, `ssh_manager.go:22-24`,
`ssh_terminal.go:24`, `types.go:16`, `tui_status.go:15`). SSH ist der Kernzweck
der Anwendung; Agent-Nutzung und known_hosts sind aktiv.


### Auswirkung
Unveraendert gegenueber M1. Neu ist allein, dass die Fehleinschaetzung nicht
mehr nur im Report steht, sondern in der Triage-Datenbank korrigiert ist: Eine
Filterung auf offene Punkte zeigt diese sieben Befunde ab sofort wieder an.

### Loesung
`golang.org/x/crypto` auf **mindestens 0.56.0** heben. Die im Scanner-Report
je CVE genannte Zielversion 0.52.0 schliesst zwar diese sieben, laesst aber
CVE-2026-56854 (Fix 0.55.0) sowie CVE-2026-56855 und CVE-2026-78662
(Fix 0.56.0) offen. Fuer GO-2026-5932 existiert noch kein Fix.

### Referenzen
- CVE-2026-42508, CVE-2026-46595, CVE-2026-39830 bis CVE-2026-39834
- worker-security BUG-032 (Projekt als Teil der Finding-Koordinate)

## Manuell nachgetragen (2026-10-14)

**Herkunft:** Neu seit dem letzten Lauf (29.09.2026) sind in diesem Projekt
ausschließlich Dependency-Advisories, die zwischen 29.09. und 14.10. veröffentlicht
wurden. Am ausgelieferten Quellcode hat sich nichts geändert.

**Messmethode:** Erreichbarkeit mit `go list -deps ./...` bei `GOPROXY=off`,
`GOTOOLCHAIN=local`, in einer **Kopie** von `src/` unter `/tmp` (damit das
Go-Tooling die `go.mod` des Projekts nicht umschreibt). Ein direkter Import
belegt Erreichbarkeit; ein abgebrochener Lauf belegt nichts.

### M-2026-10-14-1. [2026-10-14 14:45:00] - MITTEL - CVE-2026-56851 (x/text precis): gemessen NICHT erreichbar

**Schweregrad:** MITTEL
**CVSS Score:** 5.3
**Kategorie:** A06:2021 Vulnerable and Outdated Components / CWE-1395
**Betroffene Datei(en):** src/go.mod (golang.org/x/text v0.34.0)
**Bewertung:** **ignoriert**, projektspezifisch gesetzt am 14.10.2026 (Index 98; zurücksetzbar über `triage_cli.py mark ssh-easy 98 offen`)

### Beschreibung
Gemessen (203 Packages, 0 Fehler): `golang.org/x/text/secure/precis` ist nicht gelinkt.

### Auswirkung
Keine, solange das Paket nicht ins Binary kommt (z. B. über einen PostgreSQL-Treiber mit SCRAM).

### Lösung
`golang.org/x/text` auf **mindestens 0.41.0** heben (`go get golang.org/x/text@v0.41.0 && go mod tidy`).

### Referenzen
- GO-2026-6629 (CVE-2026-56851)
- https://pkg.go.dev/vuln/GO-2026-6629

## Manuell nachgetragen (2026-10-15)

**Behoben seit dem Lauf vom 14.10.2026:** ssh-easy hat `golang.org/x/crypto` auf
**0.58.0** gehoben. Damit entfallen alle 7 KRITISCHEN und 2 HOCH
x/crypto-SSH-CVEs (CVE-2026-39830 … -39834, -42508, -46595, -46597, -39829) und
die übrigen x/crypto-MITTEL-Befunde. Ebenfalls angehoben: `golang.org/x/sys`
(war 0.41.0, CVE-2026-39824 entfällt) und `golang.org/x/text` (war 0.34.0,
CVE-2026-56851/-56852 entfallen; der Nachtrag M-2026-10-14-1 ist damit gegenstandslos). Der Abschnitt „Manuell verifizierte Befunde
(2026-09-21)“ (Erreichbarkeit dieser CVEs) ist damit **erledigt** und bleibt
nur als Nachweis stehen. Ebenso erledigt ist die Zielversion-Warnung (0.56.0) aus
den Nachträgen vom 29.09./14.10. Dependency-Bestand jetzt: 27 Pakete, 1 Advisory.

### M-2026-10-15-1. [2026-10-15 15:00:00] - MITTEL - GO-2026-5932 (x/crypto/openpgp unmaintained): gemessen NICHT erreichbar

**Schweregrad:** MITTEL
**CVSS Score:** 5.3
**Kategorie:** A06:2021 Vulnerable and Outdated Components / CWE-1395
**Betroffene Datei(en):** src/go.mod (golang.org/x/crypto v0.58.0)
**Bewertung:** **ignoriert**, projektspezifisch gesetzt am 15.10.2026 (Index 69; zurücksetzbar über `triage_cli.py mark /home/claude-code/project/ssh-easy 69 offen`)

### Beschreibung
Gemessen mit `go list -deps ./...` (`GOPROXY=off`, `GOTOOLCHAIN=local`, Kopie
unter `/tmp`): 217 Packages, 0 Fehler. Gelinkt sind `ssh`, `ssh/agent`,
`ssh/knownhosts`, `curve25519`, `chacha20`, `blowfish` u. a., aber **nicht**
`golang.org/x/crypto/openpgp`.

### Auswirkung
Keine, solange `openpgp` nicht importiert wird. Für das Advisory gibt es keinen
Fix, nur den Rat, das Paket nicht zu verwenden.

### Lösung
Kein Handlungsbedarf. Bei einem künftigen `openpgp`-Import stattdessen
`github.com/ProtonMail/go-crypto/openpgp` verwenden.

### Referenzen
- https://pkg.go.dev/vuln/GO-2026-5932

**Neuer Code seit 14.10.:** `src/osc52.go` (OSC-52-Filter, bereits am 14.10.
betrachtet). Neu gemeldet werden dort nur NIEDRIG-Befunde (ignorierte
`Write`-Fehler, Zeilen 371/396/397). Im Filterpfad ist das vertretbar, aber
ein fehlgeschlagener Schreibvorgang wird stumm verschluckt.
