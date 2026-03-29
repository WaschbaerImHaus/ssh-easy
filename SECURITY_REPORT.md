# Sicherheitsreport - ssh-easy
**Scan-Zeitpunkt:** 2026-03-14 18:14:25
**Scanner-Version:** 2.0.0
**Scan-Modus:** vollstaendig

## Projektinformationen

- **Sprachen:** Go, Shell
- **Frameworks:** Keine erkannt
- **Paketmanager:** go modules
- **Docker:** Nein
- **CI/CD:** Nein
- **Tests vorhanden:** Ja

---

## Zusammenfassung

| Schweregrad | Anzahl |
|-------------|--------|
| KRITISCH    | 0 |
| HOCH        | 74 |
| MITTEL      | 8 |
| NIEDRIG     | 12 |
| INFO        | 14 |
| **GESAMT**  | **108** |

---

## Statische Code-Analyse (SAST)

## [2026-03-14 18:24:53] - Scan-Ergebnis für ssh-easy

**Gefundene Schwachstellen:** 67

- **HOCH:** 33
- **MITTEL:** 8
- **NIEDRIG:** 12
- **INFO:** 14

---

### 1. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2143

#### Betroffener Code
```
HelpList:      "  n:ಹೊಸ  e:ಸಂಪಾದಿಸಿ  d:ಅಳಿಸಿ  Enter:ಸಂಪರ್ಕ  x:ಡಿಸ್‌ಕನೆಕ್ಟ್  g:SSH-ಕೀ  l:ಭಾಷೆ  q:ನಿರ್ಗಮಿಸಿ",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 2. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2153

#### Betroffener Code
```
DisconnectedMsg: "ಡಿಸ್‌ಕನೆಕ್ಟ್: ",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 3. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2161

#### Betroffener Code
```
LabelTunnels:    "ಟನಲ್ ಪೋರ್ಟ್‌ಗಳು (ಅಲ್ಪವಿರಾಮದಿಂದ ಪ್ರತ್ಯೇಕಿಸಿ):",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 4. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2166

#### Betroffener Code
```
NoKeyFound:    "  SSH ಕೀ ಕಂಡುಬಂದಿಲ್ಲ. ಪಾಸ್‌ವರ್ಡ್ ನಮೂದಿಸಿ:",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 5. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2167

#### Betroffener Code
```
PlaceholderPW: "ಪಾಸ್‌ವರ್ಡ್/ಪಾಸ್‌ಫ್ರೇಸ್ ನಮೂದಿಸಿ",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 6. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2177

#### Betroffener Code
```
StatusDisconn:   "ಡಿಸ್‌ಕನೆಕ್ಟ್",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 7. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2181

#### Betroffener Code
```
StatusHelp:      "  t:ಟರ್ಮಿನಲ್  x:ಡಿಸ್‌ಕನೆಕ್ಟ್  Esc:ಹಿಂದೆ",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 8. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2183

#### Betroffener Code
```
DiscoMsg:        "ಡಿಸ್‌ಕನೆಕ್ಟ್",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 9. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2187

#### Betroffener Code
```
LabelPassphrase: "ಪಾಸ್‌ಫ್ರೇಸ್ (ಐಚ್ಛಿಕ):",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 10. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2188

#### Betroffener Code
```
PlaceholderPass: "ಖಾಲಿ = ಪಾಸ್‌ಫ್ರೇಸ್ ಇಲ್ಲ",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 11. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2194

#### Betroffener Code
```
KeyAddToAuth:    "  ಈ ಸಾರ್ವಜನಿಕ ಕೀಯನ್ನು ಟಾರ್ಗೆಟ್ ಸರ್ವರ್‌ನಲ್ಲಿ ~/.ssh/authorized_keys ಗೆ ಸೇರಿಸಿ.",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 12. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2198

#### Betroffener Code
```
HostKeyBoxTitle: "ಸರ್ವರ್‌ನ SSH ಕೀ ಬದಲಾಯಿತು!\n\n",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 13. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2211

#### Betroffener Code
```
KeyDeployedMsg:   "SSH ಕೀ ಡಿಪ್ಲಾಯ್! ಮುಂದಿನ ಸಂಪರ್ಕ ಪಾಸ್‌ವರ್ಡ್ ಇಲ್ಲದೆ: %s",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 14. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2576

#### Betroffener Code
```
LabelTunnels:    "پورت‌های تونل (با کاما جدا کنید):",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 15. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2583

#### Betroffener Code
```
AfterPWHint:   "  پس از ورود، کلید SSH به صورت خودکار ایجاد می‌شود.",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 16. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2604

#### Betroffener Code
```
KeyPathRequired: "مسیر فایل نمی‌تواند خالی باشد",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 17. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2616

#### Betroffener Code
```
HostKeyCaution:  "تنها در صورتی ادامه دهید که می‌دانید کلید سرور تغییر کرده است.",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 18. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 2617

#### Betroffener Code
```
HostKeyAskYesNo: "  کلید قدیمی میزبان حذف و دوباره متصل شود؟\n\n  [j/y] بله، می‌دانم   [n/Esc] خیر، لغو",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 19. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 3139

#### Betroffener Code
```
HelpList:      "  n:కొత్తది  e:సవరించు  d:తొలగించు  Enter:కనెక్ట్  x:డిస్‌కనెక్ట్  g:SSH-కీ  l:భాష  q:నిష్క్రమించు",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 20. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 3146

#### Betroffener Code
```
DeleteConfirm:   "  '%s' కనెక్షన్‌ని నిజంగా తొలగించాలా?\n\n",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 21. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 3149

#### Betroffener Code
```
DisconnectedMsg: "డిస్‌కనెక్ట్: ",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 22. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 3157

#### Betroffener Code
```
LabelTunnels:    "టన్నెల్ పోర్ట్‌లు (కామాతో వేరు చేయండి):",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 23. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 3162

#### Betroffener Code
```
NoKeyFound:    "  SSH కీ కనుగొనబడలేదు. పాస్‌వర్డ్ నమోదు చేయండి:",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 24. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 3163

#### Betroffener Code
```
PlaceholderPW: "పాస్‌వర్డ్/పాస్‌ఫ్రేజ్ నమోదు చేయండి",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 25. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 3173

#### Betroffener Code
```
StatusDisconn:   "డిస్‌కనెక్ట్",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 26. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 3177

#### Betroffener Code
```
StatusHelp:      "  t:టెర్మినల్  x:డిస్‌కనెక్ట్  Esc:వెనుకకు",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 27. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 3179

#### Betroffener Code
```
DiscoMsg:        "డిస్‌కనెక్ట్",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 28. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 3183

#### Betroffener Code
```
LabelPassphrase: "పాస్‌ఫ్రేజ్ (ఐచ్ఛికం):",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 29. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 3184

#### Betroffener Code
```
PlaceholderPass: "ఖాళీ = పాస్‌ఫ్రేజ్ లేదు",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 30. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 3190

#### Betroffener Code
```
KeyAddToAuth:    "  ఈ పబ్లిక్ కీని లక్ష్య సర్వర్‌లో ~/.ssh/authorized_keys కి జోడించండి.",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 31. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 3196

#### Betroffener Code
```
HostKeyReasons:  "సాధ్యమైన కారణాలు:\n  - సర్వర్ మళ్లీ ఇన్‌స్టాల్ (చట్టబద్ధమైనది)\n  - సర్వర్ కీ పునరుద్ధరించబడింది (చట్టబద్ధమైనది)\n  - Man-in-the-middle దాడి (ప్రమాదకరమైనది!)\n\n",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 32. [2026-03-14 18:24:53] - HOCH - Unsichtbare Unicode-Zeichen (Zero-Width Characters)

**Schweregrad:** HOCH
**CVSS Score:** 7.5
**Kategorie:** Supply Chain
**CWE:** CWE-116
**Betroffene Datei:** `src/i18n.go`
**Zeile:** 3207

#### Betroffener Code
```
KeyDeployedMsg:   "SSH కీ డిప్లాయ్ అయింది! తదుపరి కనెక్షన్ పాస్‌వర్డ్ లేకుండా: %s",
```

#### Beschreibung
Unsichtbare Unicode-Zeichen (Zero-Width Space, Zero-Width Joiner, BOM, Soft-Hyphen etc.) koennen zum Verstecken von boesartigen Instruktionen verwendet werden. Bei MCP Tool-Poisoning werden versteckte Anweisungen in Tool-Beschreibungen eingebettet.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um supply chain-basierte Angriffe durchzuführen.

#### Lösung
Datei auf unsichtbare Unicode-Zeichen pruefen. In Konfigurationsdateien und Tool-Beschreibungen entfernen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/116.html

---

### 33. [2026-03-14 18:24:53] - HOCH - Kubernetes Ingress-NGINX Pfad-Injection (CVE-2026-24512)

**Schweregrad:** HOCH
**CVSS Score:** 8.8
**Kategorie:** Injection
**CWE:** CWE-74
**Betroffene Datei:** `src/config.go`
**Zeile:** 39

#### Betroffener Code
```
return &ConfigCache{path: path}
```

#### Beschreibung
Unvalidierte path-Werte in Kubernetes Ingress-Manifesten koennen nginx-Konfigurationsdirektiven injizieren und zu RCE fuehren. CVE-2026-24512 (CVSS 8.8): Angreifer kann cluster-weite Secrets lesen und beliebigen Code ausfuehren.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um injection-basierte Angriffe durchzuführen.

#### Lösung
ingress-nginx auf >= 1.13.7 / 1.14.3 aktualisieren. Ingress-Pfade auf Sonderzeichen (;, {, }, \) validieren. Network Policies fuer Ingress-Controller konfigurieren.

#### Referenzen
- https://cwe.mitre.org/data/definitions/74.html

---

### 34. [2026-03-14 18:24:53] - MITTEL - Verwendung von SHA1 (kryptographisch gebrochen)

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

### 35. [2026-03-14 18:24:53] - MITTEL - io.Copy ohne Größenlimit (DoS-Risiko)

**Schweregrad:** MITTEL
**CVSS Score:** 5.3
**Kategorie:** Denial of Service
**CWE:** CWE-770
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 94

#### Betroffener Code
```
io.Copy(remoteConn, localConn)
```

#### Beschreibung
io.Copy() kopiert ohne Begrenzung bis EOF. Bei Netzwerk-Streams kann ein Angreifer unbegrenzt Daten senden und den Speicher des Servers erschöpfen (Out of Memory).

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um denial of service-basierte Angriffe durchzuführen.

#### Lösung
io.LimitReader() verwenden um die maximale Datenmenge zu begrenzen: io.Copy(dst, io.LimitReader(src, maxBytes)). Alternativ io.CopyN() mit explizitem Limit nutzen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/770.html

---

### 36. [2026-03-14 18:24:53] - MITTEL - io.Copy() ohne Größenlimit

**Schweregrad:** MITTEL
**CVSS Score:** 5.3
**Kategorie:** Denial of Service
**CWE:** CWE-770
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 94

#### Betroffener Code
```
io.Copy(remoteConn, localConn)
```

#### Beschreibung
io.Copy() kopiert ohne Größenbegrenzung. Bei externen Datenquellen (HTTP-Responses, Datei-Uploads) kann dies zu Speichererschöpfung führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um denial of service-basierte Angriffe durchzuführen.

#### Lösung
io.LimitReader() verwenden um die Datenmenge zu begrenzen: io.Copy(dst, io.LimitReader(src, maxBytes))

#### Referenzen
- https://cwe.mitre.org/data/definitions/770.html

---

### 37. [2026-03-14 18:24:53] - MITTEL - io.Copy ohne Größenlimit (DoS-Risiko)

**Schweregrad:** MITTEL
**CVSS Score:** 5.3
**Kategorie:** Denial of Service
**CWE:** CWE-770
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 99

#### Betroffener Code
```
io.Copy(localConn, remoteConn)
```

#### Beschreibung
io.Copy() kopiert ohne Begrenzung bis EOF. Bei Netzwerk-Streams kann ein Angreifer unbegrenzt Daten senden und den Speicher des Servers erschöpfen (Out of Memory).

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um denial of service-basierte Angriffe durchzuführen.

#### Lösung
io.LimitReader() verwenden um die maximale Datenmenge zu begrenzen: io.Copy(dst, io.LimitReader(src, maxBytes)). Alternativ io.CopyN() mit explizitem Limit nutzen.

#### Referenzen
- https://cwe.mitre.org/data/definitions/770.html

---

### 38. [2026-03-14 18:24:53] - MITTEL - io.Copy() ohne Größenlimit

**Schweregrad:** MITTEL
**CVSS Score:** 5.3
**Kategorie:** Denial of Service
**CWE:** CWE-770
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 99

#### Betroffener Code
```
io.Copy(localConn, remoteConn)
```

#### Beschreibung
io.Copy() kopiert ohne Größenbegrenzung. Bei externen Datenquellen (HTTP-Responses, Datei-Uploads) kann dies zu Speichererschöpfung führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um denial of service-basierte Angriffe durchzuführen.

#### Lösung
io.LimitReader() verwenden um die Datenmenge zu begrenzen: io.Copy(dst, io.LimitReader(src, maxBytes))

#### Referenzen
- https://cwe.mitre.org/data/definitions/770.html

---

### 39. [2026-03-14 18:24:53] - MITTEL - context.Background() ohne Timeout/Deadline

**Schweregrad:** MITTEL
**CVSS Score:** 4.3
**Kategorie:** Context-Handling
**CWE:** CWE-400
**Betroffene Datei:** `src/ssh_manager.go`
**Zeile:** 199

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

### 40. [2026-03-14 18:24:53] - MITTEL - time.Sleep in Goroutine (mögliches Goroutine-Leak)

**Schweregrad:** MITTEL
**CVSS Score:** 4.3
**Kategorie:** Nebenläufigkeit / Performance
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh_manager.go`
**Zeile:** 680

#### Betroffener Code
```
time.Sleep(ReconnectDelay)
```

#### Beschreibung
time.Sleep() in Goroutinen blockiert ohne Abbruchmöglichkeit. Bei Signal oder Shutdown kann die Goroutine hängen bleiben.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um nebenläufigkeit / performance-basierte Angriffe durchzuführen.

#### Lösung
select mit context.Done() und time.After() verwenden statt time.Sleep() für abbrechbares Warten.

#### Referenzen
- https://cwe.mitre.org/data/definitions/404.html

---

### 41. [2026-03-14 18:24:53] - MITTEL - context.Background() statt Request-Context

**Schweregrad:** MITTEL
**CVSS Score:** 4.0
**Kategorie:** Context-Analyse
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh_manager.go`
**Zeile:** 199

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

### 42. [2026-03-14 18:24:53] - NIEDRIG - defer Close() ohne Fehlerprüfung

**Schweregrad:** NIEDRIG
**CVSS Score:** 2.0
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 81

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

### 43. [2026-03-14 18:24:53] - NIEDRIG - defer Close() ohne Fehlerprüfung

**Schweregrad:** NIEDRIG
**CVSS Score:** 2.0
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 87

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

### 44. [2026-03-14 18:24:53] - NIEDRIG - defer Close() ohne Fehlerprüfung

**Schweregrad:** NIEDRIG
**CVSS Score:** 2.0
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 117

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

### 45. [2026-03-14 18:24:53] - NIEDRIG - defer Close() ohne Fehlerprüfung

**Schweregrad:** NIEDRIG
**CVSS Score:** 2.0
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 399

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

### 46. [2026-03-14 18:24:53] - NIEDRIG - Ignorierter Error-Rueckgabewert: fmt.Fprintf()

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.0
**Kategorie:** Error Handling
**CWE:** CWE-252
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 120

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

### 47. [2026-03-14 18:24:53] - NIEDRIG - defer Close() ohne Fehlerprüfung

**Schweregrad:** NIEDRIG
**CVSS Score:** 2.0
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/ssh_terminal.go`
**Zeile:** 106

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

### 48. [2026-03-14 18:24:53] - NIEDRIG - Error-Rückgabewert ignoriert (_)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/ssh_terminal.go`
**Zeile:** 186

#### Betroffener Code
```
_ = session.WindowChange(h, w)
```

#### Beschreibung
Error-Rückgabewerte werden mit _ ignoriert (CWE-252). Unbehandelte Fehler können zu undefiniertem Verhalten führen.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um fehlerbehandlung-basierte Angriffe durchzuführen.

#### Lösung
Fehler prüfen und angemessen behandeln: if err != nil { return fmt.Errorf(...) }

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 49. [2026-03-14 18:24:53] - NIEDRIG - Ignorierter Error-Rueckgabewert: session.WindowChange()

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.0
**Kategorie:** Error Handling
**CWE:** CWE-252
**Betroffene Datei:** `src/ssh_terminal.go`
**Zeile:** 186

#### Betroffener Code
```
				_ = session.WindowChange(h, w)
```

#### Beschreibung
Der Fehler-Rueckgabewert von session.WindowChange() wird mit '_' ignoriert. Unbehandelte Fehler koennen zu unerwartetem Verhalten und Sicherheitsproblemen fuehren.

#### Auswirkung
Ein Angreifer könnte diese Schwachstelle nutzen um error handling-basierte Angriffe durchzuführen.

#### Lösung
Error-Rueckgabewert von session.WindowChange() pruefen und angemessen behandeln.

#### Referenzen
- https://cwe.mitre.org/data/definitions/252.html

---

### 50. [2026-03-14 18:24:53] - NIEDRIG - Ignorierter Error-Rueckgabewert: SaveConfig()

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

### 51. [2026-03-14 18:24:53] - NIEDRIG - defer Close() ohne Fehlerprüfung

**Schweregrad:** NIEDRIG
**CVSS Score:** 2.0
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-754
**Betroffene Datei:** `src/logger.go`
**Zeile:** 94

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

### 52. [2026-03-14 18:24:53] - NIEDRIG - Go Error-Wert mit _ ignoriert (OWASP A10:2025)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/tui_status.go`
**Zeile:** 30

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

### 53. [2026-03-14 18:24:53] - NIEDRIG - Go Error-Wert mit _ ignoriert (OWASP A10:2025)

**Schweregrad:** NIEDRIG
**CVSS Score:** 3.7
**Kategorie:** Fehlerbehandlung
**CWE:** CWE-252
**Betroffene Datei:** `src/tui_status.go`
**Zeile:** 66

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

### 54. [2026-03-14 18:24:53] - INFO - Goroutine gefunden - Race-Condition-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-362
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 54

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

### 55. [2026-03-14 18:24:53] - INFO - Goroutine-Aufruf - Lifecycle-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 54

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

### 56. [2026-03-14 18:24:53] - INFO - select ohne ctx.Done()-Case prüfen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 56

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

### 57. [2026-03-14 18:24:53] - INFO - Goroutine-Aufruf - Lifecycle-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 65

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

### 58. [2026-03-14 18:24:53] - INFO - Goroutine gefunden - Race-Condition-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-362
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 92

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

### 59. [2026-03-14 18:24:53] - INFO - Goroutine-Aufruf - Lifecycle-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 92

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

### 60. [2026-03-14 18:24:53] - INFO - Goroutine gefunden - Race-Condition-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-362
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 97

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

### 61. [2026-03-14 18:24:53] - INFO - Goroutine-Aufruf - Lifecycle-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh.go`
**Zeile:** 97

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

### 62. [2026-03-14 18:24:53] - INFO - Goroutine gefunden - Race-Condition-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-362
**Betroffene Datei:** `src/ssh_terminal.go`
**Zeile:** 149

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

### 63. [2026-03-14 18:24:53] - INFO - Goroutine-Aufruf - Lifecycle-Prüfung empfohlen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh_terminal.go`
**Zeile:** 149

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

### 64. [2026-03-14 18:24:53] - INFO - select ohne ctx.Done()-Case prüfen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh_terminal.go`
**Zeile:** 178

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

### 65. [2026-03-14 18:24:53] - INFO - select ohne ctx.Done()-Case prüfen

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

### 66. [2026-03-14 18:24:53] - INFO - select ohne ctx.Done()-Case prüfen

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

### 67. [2026-03-14 18:24:53] - INFO - select ohne ctx.Done()-Case prüfen

**Schweregrad:** INFO
**CVSS Score:** 0.0
**Kategorie:** Nebenläufigkeit
**CWE:** CWE-404
**Betroffene Datei:** `src/ssh_manager.go`
**Zeile:** 613

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

## [2026-03-14 18:24:53] - Dependency-Analyse für ssh-easy

**Geprüfte Abhängigkeiten:** 27
**Gefundene Probleme:** 0


## Secrets-Scan

## [2026-03-14 18:24:53] - Secrets-Scan für ssh-easy

**Gefundene Geheimnisse:** 41

### 1. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 279
**Fundstelle:** `Pass: "l**********************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 2. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 362
**Fundstelle:** `Pass: "e*********************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 3. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 445
**Fundstelle:** `Pass: "v**************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 4. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 528
**Fundstelle:** `Pass: "v*************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 5. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 611
**Fundstelle:** `Pass: "v*********************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 6. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 694
**Fundstelle:** `Pass: "空*************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 7. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 777
**Fundstelle:** `Pass: "空**********`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 8. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 860
**Fundstelle:** `Pass: "v***********************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 9. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 943
**Fundstelle:** `Pass: "п***************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 10. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 1026
**Fundstelle:** `Pass: "k**************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 11. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 1109
**Fundstelle:** `Pass: "ख**************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 12. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 1192
**Fundstelle:** `Pass: "খ*******************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 13. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 1275
**Fundstelle:** `Pass: "خ********************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 14. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 1358
**Fundstelle:** `Pass: "ف**********************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 15. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 1441
**Fundstelle:** `Pass: "ባ***********`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 16. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 1524
**Fundstelle:** `Pass: "п***************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 17. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 1607
**Fundstelle:** `Pass: "p***************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 18. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 1690
**Fundstelle:** `Pass: "w********************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 19. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 1773
**Fundstelle:** `Pass: "ც***********************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 20. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 1856
**Fundstelle:** `Pass: "κ****************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 21. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 1939
**Fundstelle:** `Pass: "ખ****************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 22. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 2022
**Fundstelle:** `Pass: "w***********************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 23. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 2105
**Fundstelle:** `Pass: "e***********************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 24. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 2188
**Fundstelle:** `Pass: "ಖ***********************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 25. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 2271
**Fundstelle:** `Pass: "비*****************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 26. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 2354
**Fundstelle:** `Pass: "र**********************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 27. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 2437
**Fundstelle:** `Pass: "l********************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 28. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 2520
**Fundstelle:** `Pass: "d****************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 29. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 2603
**Fundstelle:** `Pass: "خ**********************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 30. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 2686
**Fundstelle:** `Pass: "p**************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 31. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 2769
**Fundstelle:** `Pass: "ਖ**************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 32. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 2852
**Fundstelle:** `Pass: "g*************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 33. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 2935
**Fundstelle:** `Pass: "t*************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 34. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 3018
**Fundstelle:** `Pass: "t**********************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 35. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 3101
**Fundstelle:** `Pass: "க****************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 36. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 3184
**Fundstelle:** `Pass: "ఖ***********************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 37. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 3267
**Fundstelle:** `Pass: "ว***********************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 38. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 3350
**Fundstelle:** `Pass: "b****************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 39. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 3433
**Fundstelle:** `Pass: "п*****************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 40. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 3516
**Fundstelle:** `Pass: "t*****************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---

### 41. HOCH - Hardcodiertes Passwort

**Typ:** Passwort
**Datei:** `src/i18n.go`
**Zeile:** 3599
**Fundstelle:** `Pass: "o*******************************`

**Beschreibung:** Ein hartcodiertes Passwort wurde im Quellcode gefunden. Es kann leicht extrahiert und missbraucht werden.

**Lösung:** Geheimnis aus dem Code entfernen und rotieren. Umgebungsvariablen oder einen Secret-Manager verwenden.

---


## Konfigurationsanalyse

## [2026-03-14 18:24:53] - Konfigurationsanalyse für ssh-easy

Keine Konfigurationsprobleme gefunden.

