# Authenticode-Signierung für ssh-easy: Optionen

Datum der Notiz: 2026-11-12 (alle Abrufdaten unten: 2026-11-12)
Erstellt von: analyst-research
Zweck: Entscheidungsgrundlage, wie die Windows-Dateien (`ssh-easy-windows-*.exe`, `ssh-easy-setup-*.exe`) signiert werden können, damit SmartScreen und "Unbekannter Herausgeber" weniger abschrecken.

Kennzeichnung: **belegt** = auf der genannten Seite am 2026-11-12 gelesen. **ungeprüft** = nur aus einem Suchtreffer-Auszug, einer Zweitquelle oder eigener Schlussfolgerung, nicht an der Primärquelle bestätigt. Preise sind Stand 2026-11-12 und ändern sich.

Ausgangslage des Projekts (aus dem Repo geprüft am 2026-11-12):
- Einzelentwickler, Privatperson, Hobby-/OSS-Projekt ohne Firma. README nennt Lizenz "MIT", aber im Repo ist keine Datei `LICENSE` getrackt (`git ls-files`).
- Gebaut wird lokal per `build.sh`; unter `.github/workflows/` liegt nur `pages.yml` (Projektseite), kein Build-Workflow.
- Windows-Artefakte: 2 EXEs (amd64, arm64, `-H windowsgui`) und 2 NSIS-Setups (amd64, arm64). Der Auto-Updater prüft zusätzlich eine Ed25519-signierte `SHA256SUMS`.

---

## 1. SignPath Foundation (kostenloses Signieren für OSS)

### Bedingungen (belegt, https://signpath.org/terms)
- Lizenz: OSI-anerkannte Open-Source-Lizenz, keine kommerzielle Dual-Lizenzierung, kein proprietärer Code in den signierten Komponenten (Systembibliotheken ausgenommen).
- Repository: Das Signier-Team muss das Projekt selbst entwickeln und das Quell-Repository besitzen. Signiert werden nur eigene Binaries aus eigenem Quellcode.
- Build: Binaries müssen **automatisierte, nachprüfbare Builds aus dem Repository** sein. Build-Skripte und CI-Konfiguration gelten als Quellcode. Jede Freigabe braucht eine **manuelle Genehmigung** (Approver) vor dem Signieren.
- Reputation/Nutzung: Das Projekt muss aktiv gepflegt sein, in der zu signierenden Form bereits veröffentlicht sein, und seine Funktion muss auf der Download-Seite beschrieben sein. Die Stiftung macht eine einmalige Reputations- und Kontrollprüfung.
- Team: Alle Teammitglieder brauchen Multi-Faktor-Authentisierung für SignPath und das Repository. Rollen: Autoren, Reviewer, Approver (bei einem Einzelentwickler fallen alle auf eine Person; ob das akzeptiert wird, ist **ungeprüft**).
- Nennung/Datenschutz: Eine Seite "Code signing policy" auf Start- und Download-Seite, Hinweis "Free code signing provided by SignPath.io, certificate by SignPath Foundation" (Wortlaut belegt über https://signpath.org/terms und die Policy-Seiten von Projekten, z. B. https://pandoc.org/code-signing-policy.html), Teamrollen/Mitglieder nennen, Link auf eine Datenschutzerklärung. Software, die Daten an unbestimmte Systeme sendet, muss das im Installer anzeigen und abschaltbar machen (ssh-easy sendet bis auf den Update-Check nichts; Update-Check-Abschaltbarkeit wäre zu prüfen).
- Metadaten: In SignPath wird erzwungen, dass der Produktname dem Projektnamen entspricht und die Produktversion über Builds hinweg identisch ist.
- Verbote: Malware/PUA, Funktionen zum Ausnutzen oder Umgehen von Sicherheitsmaßnahmen, ungemeldete Systemänderungen; Installer brauchen eine passende Deinstallation (NSIS-Setup hat sie).
- Haftung: Die Stiftung haftet nicht für Schäden durch signierte Software; bei Verstößen kann das Zertifikat (auch rückwirkend) widerrufen werden.
- Das Zertifikat lautet auf die **SignPath Foundation** als Herausgeber (Zweitquelle im Suchtreffer; ungeprüft an Primärquelle, aber mit dem Zweck der Stiftung vereinbar, siehe https://signpath.org/). Vorteil: Der Name der Privatperson erscheint nicht im Zertifikat.

### Antragsablauf
- Antrag über https://signpath.org/apply ("Apply for a free SignPath.io subscription"). Die Seite nennt beim Abruf keine Schritte (belegt: Seite zeigt nur Überschrift). Die Stiftung ist laut Terms nicht zur Annahme verpflichtet und bietet keine Schlichtung. Bearbeitungsdauer: **ungeprüft**.

### Lokaler Build-Rechner oder CI-Pflicht?
- **CI ist praktisch Pflicht.** https://docs.signpath.io/origin-verification (belegt): Origin Verification ist für "Open Source Code Signing (required)". Sie prüft Repository-URL, Branch, Commit und die CI-Job-URL; die Build-Einstellungen müssen unter Versionskontrolle stehen, ohne manuelle Überschreibung. https://docs.signpath.io/trusted-build-systems (belegt): unterstützte Trusted Build Systems sind Jenkins, GitHub, GitLab, Azure DevOps, TeamCity, AppVeyor; der alte mTLS-Weg für beliebige Agents ("Double Authentication Proxy") ist veraltet.
- Schluss (eigene Folgerung, die Seiten sagen es nicht wörtlich): Ein lokaler Build per `build.sh` kann nicht ursprungsgeprüft werden und ist für das Foundation-Programm daher nicht nutzbar. Es bräuchte einen GitHub-Actions-Workflow, der die Windows-EXEs und Setups baut, die Artefakte zu SignPath hochlädt und das signierte Ergebnis zurückholt. Für Go (`CGO_ENABLED=0`, Cross-Compile) und NSIS (`makensis` per apt) ist ein Linux-Runner machbar (Machbarkeit **ungeprüft** im Detail).
- Bauabfolge-Falle: Die EXE muss vor dem Verpacken ins Setup signiert sein, danach das Setup selbst; das sind zwei Signieranfragen mit je manueller Genehmigung. Ob SignPath ein NSIS-Setup "tief" signiert (wie MSI), ist **ungeprüft**; MSI-Deep-Signing ist belegt (https://docs.signpath.io/artifact-configuration/syntax).

### Einschränkungen
- Dateitypen (belegt, https://docs.signpath.io/artifact-configuration/syntax): PE-Dateien (`.exe`, `.dll`) per Authenticode, MSI, ZIP, Office-Makros, Prüfen bereits signierter Dateien. Nur Windows-Artefakte relevant; die Linux-Binaries bleiben unsigniert (es gibt für sie ohnehin die Ed25519-Prüfsummen).
- ARM64: Die gelesenen Seiten erwähnen weder Unterstützung noch Ausschluss (belegt: nicht genannt). Da Authenticode-Signatur von PE-Dateien architekturunabhängig ist, ist ARM64 sehr wahrscheinlich kein Problem, aber **ungeprüft**.
- Das signierte Ergebnis ändert die Dateihashes; `SHA256SUMS` und die Ed25519-Signatur müssen danach erzeugt werden (siehe Fazit).

---

## 2. Azure Artifact Signing (früher "Trusted Signing")

### Name und Stand
- Umbenannt in **Artifact Signing**; Microsoft Learn führt es unter `azure/artifact-signing`, der CLI-Befehl heißt `az artifact-signing` (belegt, https://learn.microsoft.com/azure/trusted-signing/quickstart, Seitenstand 2026-10-08). Die Hilfeseite "Code signing options" (Seitenstand 2026-11-10) nennt es "Microsoft's recommended code signing service for non-Store distribution" (belegt, https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options).

### Preis (Stand 2026-11-12)
- Basic **9,99 USD pro Konto und Monat**, 5.000 Signaturen/Monat, 1 Zertifikatsprofil je Typ. Premium **99,99 USD pro Konto und Monat**, 100.000 Signaturen, 10 Profile je Typ. Jede Signatur über der Quote 0,005 USD (belegt, https://learn.microsoft.com/en-us/azure/trusted-signing/how-to-change-sku, Seitenstand 2026-08-03).
- Die offizielle Azure-Preisseite zeigte beim Abruf nur Platzhalter ("$-"), dort steht außerdem: Abrechnung ab Anlegen des Kontos, nicht anteilig (belegt, https://azure.microsoft.com/de-de/pricing/details/artifact-signing/). Aktuelle Beträge im Azure-Preisrechner prüfen.

### Wer ist berechtigt?
- Public-Trust-Zertifikate: Organisationen in USA, Kanada, EU, UK, Australien, Neuseeland, Japan, Südkorea, Singapur, Schweiz, Norwegen, Israel. **Einzelentwickler (Individuals) nur in den USA und Kanada** (belegt, Quickstart-Seite oben; ebenso Hilfeseite "Code signing options").
- Folge für das Projekt: Eine Privatperson in Deutschland ist **nicht** berechtigt. Eine Organisationsvalidierung braucht eine "legal business entity" mit Geschäftsadresse, Domain-E-Mail und Business Identifier (belegt, Quickstart). Ob ein deutsches Einzelunternehmen/Kleingewerbe als Organisation gilt, ist **ungeprüft**; für ein reines Hobbyprojekt ohne Firma ist der Weg nicht vorgesehen.
- Ablauf (belegt, Quickstart): Entra-Tenant und Azure-Subscription, Resource Provider `Microsoft.CodeSigning` registrieren, Konto anlegen (Region wie West Europe/North Europe), Identitätsvalidierung nur im Portal (1 bis 20 Werktage, ausweisbasiert über Verified ID/AU10TIX), Zertifikatsprofil anlegen, Rolle "Certificate Profile Signer" vergeben.

### Technik auf Linux
- Von Microsoft unterstützte Integrationen (belegt, https://learn.microsoft.com/azure/artifact-signing/how-to-signing-integrations, Seitenstand 2026-08-03): SignTool (nur Windows: Windows-SDK-SignTool, .NET 8, Dlib), GitHub Actions, Azure DevOps, PowerShell, Azure PowerShell (CI-Policy), Artifact-Signing-SDK. **osslsigncode steht nicht in der Liste.**
- Zertifikate sind nur **3 Tage gültig**; deshalb ist ein Zeitstempel zwingend, empfohlen wird `http://timestamp.acs.microsoft.com/` (belegt, gleiche Seite). Es gibt keine nutzbare PKCS#12-Datei; die Signatur entsteht im Dienst.
- Linux-Wege nur aus Suchtreffern (**ungeprüft**): Jsign (Java) und das Drittwerkzeug SignUniversal.Azure (https://www.nuget.org/packages/SignUniversal.Azure) sowie ein Blogbeitrag über ein eigenes Werkzeug `aas-sign` (https://nullprogram.com/blog/2026/04/25/). Die dort beschriebene Trust-Store-Eigenheit unter Linux (`--trust-signing-root`) wurde nicht nachgeprüft.

---

## 3. OV-/EV-Zertifikat einer klassischen CA

### Preis-Spanne
- Microsoft nennt OV **typisch 150 bis 300 USD pro Jahr**, EV **ab 400 USD pro Jahr** (belegt, "Code signing options"-Seite oben).
- Zweitquellen (**ungeprüft**, Händlerangaben): Sectigo OV über Händler ab 219 USD/Jahr (https://www.ssldragon.com/ssl-certificates/code-signing/); SSL.com EV Sole Proprietor 359 USD/Jahr bei 1 Jahr (https://www.ssl.com/products/software-integrity/code-signing/ev-sole-proprietor/).

### Pflicht für HSM/Hardware-Token
- Seit **1. Juni 2023** verlangen die Code Signing Baseline Requirements des CA/Browser-Forums, dass der private Schlüssel in einem geeigneten Hardware-Kryptomodul liegt (Abschnitt 6.2.7.4.2; Mindestniveau FIPS 140-2 Level 2 oder Common Criteria EAL 4+) (Quelle: https://cabforum.org/working-groups/code-signing/requirements/, dort aktuelle Version 3.11.0 vom 2026-06-16; Details laut Zweitquellen, https://www.encryptionconsulting.com/what-are-the-ca-browser-forum-requirements-for-code-signing-certificate-private-keys-are-you-prepared/, **Detailwortlaut ungeprüft**). Microsoft bestätigt die HSM-Pflicht für OV seit Juni 2023 (belegt, "Code signing options"-Seite). Hinweis aus Zweitquelle: FIPS-140-2-Zertifikate werden ab 2026-09-21 "Historical"; Auswirkung auf die Baseline **ungeprüft**.
- Konsequenz: Ein exportierbares `.pfx` gibt es von öffentlichen CAs praktisch nicht mehr neu. Der Weg `SSH_EASY_CODESIGN_PKCS12` passt nur zu älteren Zertifikaten oder zu privaten Testzertifikaten.

### Laufzeit: 460 Tage seit 2026-03-01
- Beschluss der CA/Browser-Forum-Abstimmung (CSC-31): Höchstlaufzeit öffentlich vertrauter Code-Signing-Zertifikate sinkt von 39 Monaten auf **460 Tage**, wirksam **1. März 2026**; vorher ausgestellte Zertifikate behalten ihre Laufzeit (belegt für die 460 Tage und das Datum: https://www.digicert.com/blog/understanding-the-new-code-signing-certificate-validity-change; Zweitquelle zum Beschlussdatum 2025-11-17 und zur Bestandsregel: https://www.thesslstore.com/blog/code-signing-certificates-lifespans-to-drop-to-one-year/, **ungeprüft**). Ein Suchtreffer (SSL.com) nannte vorübergehend 458 Tage ab 2026-02-27; **ungeprüft**. Ein Treffer von AppViewX mit "200 Tagen" verwechselt TLS-Zertifikate mit Code Signing und wurde verworfen.
- Folge: Rund alle 15 Monate neu ausstellen und das Signierverfahren neu einrichten (Token/Cloud-Zugang).

### Cloud-HSM-Optionen
- DigiCert KeyLocker, DigiCert Software Trust Manager, SSL.com eSigner: Cloud-Signieren mit osslsigncode über PKCS#11 (belegt für DigiCert: https://docs.digicert.com/en/software-trust-manager/signing-tools/osslsigncode-with-pkcs11.html; "DigiCert KeyLocker unterstützt unter Linux nur osslsigncode" laut Suchtreffer https://docs.digicert.com/fr/digicert-keylocker/signing-tools/osslsigncode-with-pkcs11.html, **ungeprüft**). Preise für Privatpersonen: nicht gefunden.
- Für Privatpersonen interessant, weil günstig und europäisch:
  - **Certum "Open Source Code Signing in the Cloud"**: **49,00 EUR** brutto = netto (Preis laut Shop; ob pro Jahr, Laufzeit nicht angegeben, **ungeprüft**), nur für natürliche Personen, RSA mindestens 3072 Bit, Schlüssel in FIPS-140-2-Level-3-/CC-EAL-4+-Modul im SimplySign-Cloud-Dienst, Limit **5.000 Signaturen pro Monat**, Name im Zertifikat mit Präfix "Open Source Developer" (belegt, https://shop.certum.eu/open-source-code-signing-on-simplysign.html).
  - Nachweise (belegt, https://support.certum.eu/en/code-signing-required-documents/): Identitätsprüfung (automatisch, Ausweisfotos o. Ä.), Adressnachweis (Rechnung auf den Antragsteller), **Beleg für ein laufendes, öffentliches Open-Source-Projekt**, das klar mit dem Antragsteller verknüpft ist. Das Zertifikat wird **widerrufen, wenn damit kommerziell vertriebene Software signiert wird**. Es wird nur an Einzelpersonen ausgestellt. Eine Länderbeschränkung (z. B. Deutschland) wurde nicht gefunden; **ungeprüft**, ob der Antrag aus Deutschland ohne Weiteres durchgeht.
  - **HARICA** "IV" (Individual Validated) für natürliche Personen, Code Signing ab **59 EUR pro Jahr** (belegt, https://www.harica.gr/en/code-signing; Preis je Typ, Laufzeit und Token/Cloud-Lieferung steht dort nicht, **ungeprüft**).
  - SSL.com "IV" (Individual Validated) für Privatpersonen (Zweitquelle, **ungeprüft**, https://www.ssl.com/products/software-integrity/code-signing/iv/).

### Für Privatpersonen erhältlich?
- Ja, aber nur über IV-/Sole-Proprietor-/Open-Source-Produkte bestimmter CAs. Viele große CAs lehnen reine Privatpersonen ab; Sectigo stellt laut Zweitquelle teils an Einzelunternehmer aus (**ungeprüft**, aus Suchtreffer).
- Der Name der Person steht dann im Zertifikat und im Windows-Dialog. Das ist ein Datenschutzpunkt, den der Nutzer bewusst entscheiden sollte.

### Verwendung unter Linux mit osslsigncode
- Auf diesem Rechner ist `osslsigncode 2.13` (OpenSSL 3.5) installiert (geprüft per `osslsigncode --version`). Es kennt `-pkcs12`, `-pkcs11module`, `-pkcs11cert` und `-key <URI>` (geprüft per `osslsigncode sign --help`).
- Hardware-Token oder Cloud-HSM: `-pkcs11module <Bibliothek> -pkcs11cert <URI> -key <URI>` bzw. Provider-Weg ab OpenSSL 3. Belegt als Muster bei DigiCert (Link oben).
- **Certum SimplySign unter Linux**: Eine offizielle Certum-Anleitung für osslsigncode fand die Suche nicht. Das Drittprojekt `ssign` (MIT, Rust, https://docs.rs/ssign) signiert PE-Dateien per HTTPS ohne SimplySign-Desktop und liefert ein `ssign-pkcs11`-Modul für osslsigncode; Zugang über E-Mail und ein TOTP-Seed in Umgebungsvariablen (Seed wie einen privaten Schlüssel behandeln). Einschränkungen laut Seite: MSI-Signatur noch nicht nativ, ARM64 nicht erwähnt, nur Certum SimplySign. Projektreife und Sicherheit: **ungeprüft**, Quellcode vor Einsatz prüfen.

---

## 4. SmartScreen-Reputation

Quelle für alles in diesem Abschnitt, sofern nicht anders genannt: https://learn.microsoft.com/windows/apps/package-and-deploy/smartscreen-reputation (Seitenstand 2026-08-17) und https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options (Seitenstand 2026-11-10); beide belegt.

- **Zwei Signale:** Herausgeber-Reputation (Zertifikat) und Datei-Hash-Reputation. Auch signierte, neue Dateien können warnen, bis Hash oder Zertifikat genug saubere Downloads gesammelt haben.
- **EV-Sofortvorteil entfällt:** "EV certificates no longer bypass SmartScreen." EV-Aufpreis nur für SmartScreen ist laut Microsoft nicht mehr gerechtfertigt; seit 2024 verhalten sich OV und EV gleich.
- **Reputation pro Zertifikat/Identität und pro Datei:** Ohne Signatur beginnt jede neue Version bei null. Mit gleicher Herausgeber-Identität kann die Reputation auf neue Versionen übergehen. Microsoft rät: jede Version signieren, signierte Dateien nicht mehr verändern, konsistente Signaturidentität verwenden.
- **Dauer bis die Warnung verschwindet:** Kein fester Schwellwert; laut Microsoft "several weeks and hundreds of clean installs from a wide audience". Anekdoten aus Foren (7 Monate bzw. über 4 Monate) sind **ungeprüft** (Suchtreffer, https://forum.xojo.com/t/microsoft-smartscreen-and-ov-certificates/86735).
- **Einreichen bei Microsoft:** Laut Microsoft gibt es für Endkunden-Rechner "no need (or mechanism) to manually submit a file for SmartScreen reputation review". Unternehmens-Admins können Dateien über https://www.microsoft.com/en-us/wdsi/filesubmission einreichen. (Ein Fehlalarm-Report bei Defender ist davon getrennt und wurde nicht geprüft.)
- **Zertifikatswechsel:** Ein Microsoft-Q&A-Beitrag vom Mai 2026 berichtet, dass eine EV-Erneuerung die Reputation zurückgesetzt habe (Suchtreffer, **ungeprüft**: https://learn.microsoft.com/en-us/answers/questions/5900208/smartscreen-reputation-reset-following-ev-certific). Microsofts Doku sagt dagegen, gleiche Identität könne Reputation tragen. Bei 460-Tage-Laufzeit ist das ein Risiko, wenn die Zertifikatsidentität wechselt.
- **Smart App Control (Windows 11):** Kann SmartScreen ersetzen und blockiert unsignierte Dateien ohne positive Reputation, auch lokal ausgeführte. Signatur hilft hier unabhängig von der Download-Quelle.
- **Microsoft Store:** Einziger Weg ohne Warnung von Anfang an (MSIX wird von Microsoft neu signiert). Für ein NSIS-/EXE-Projekt ein größerer Umbau; nicht Teil dieser Notiz.

---

## 5. Fazit und Empfehlung

### Reihenfolge der Optionen für ssh-easy
1. **SignPath Foundation** (kostenlos, Zertifikat auf die Stiftung, kein HSM, kein Personenname im Zertifikat). Voraussetzungen: Datei `LICENSE` (MIT) ins Repo, Seite "Code signing policy" und Datenschutzhinweis, vor allem ein **GitHub-Actions-Build** der Windows-Artefakte mit SignPath-Connector (lokaler `build.sh` reicht nicht). Annahme des Antrags nicht garantiert; Wartezeit unbekannt.
2. **Certum Open Source Code Signing in the Cloud** (49 EUR, nur natürliche Personen, lokaler Build ist erlaubt, kein CI nötig). Nachteile: Personenname im Zertifikat, Limit 5.000 Signaturen/Monat (ausreichend), Widerruf bei kommerziellem Vertrieb (für ssh-easy kein Thema), Cloud-Signieren unter Linux nur über `ssign` oder die SimplySign-Software (Stabilität ungeprüft), 460-Tage-Erneuerung.
3. **HARICA IV** (ab 59 EUR/Jahr) oder ein anderes IV-/OV-Angebot: erst nach Klärung, ob Token oder Cloud-HSM mit PKCS#11 unter Linux funktioniert.
4. **Azure Artifact Signing**: für diesen Fall **ausgeschlossen**, solange keine Organisation besteht, weil Einzelentwickler außerhalb USA/Kanada nicht berechtigt sind. Auch technisch ohne osslsigncode-Unterstützung (nur SignTool/Dlib unter Windows, GitHub Action, SDK).
5. **EV-Zertifikat**: nicht empfohlen, kein SmartScreen-Vorteil mehr.

Unabhängig von der Wahl: Reputation baut sich nur über Wochen und viele saubere Downloads auf; mit Signatur ist der Herausgeber sichtbar, die Warnung bleibt am Anfang bestehen.

### Stand in `build.sh` (aktualisiert 2026-11-12, 20:50)
- Der optionale Codesign-Schritt ist inzwischen eingebaut (Berichtigung: die erste Fassung dieser Notiz entstand, bevor er geschrieben war). Er ist aktiv, wenn `SSH_EASY_CODESIGN_PKCS12` auf eine PKCS#12-Datei zeigt; Passwort über `SSH_EASY_CODESIGN_PASSFILE` oder `SSH_EASY_CODESIGN_PASSWORD`, Zeitstempelserver über `SSH_EASY_CODESIGN_TSA` (Standard `http://timestamp.digicert.com`, `none` nur für Tests), CA-Datei für `osslsigncode verify` über `SSH_EASY_CODESIGN_CAFILE`. Ohne Zertifikat wird mit Hinweis übersprungen.
- Reihenfolge in `build.sh`: EXEs signieren und prüfen, Setups bauen (enthalten die signierte EXE), Setups signieren und prüfen, zuletzt `SHA256SUMS` und Ed25519-Signatur (die Hashes ändern sich durch die Authenticode-Signatur). Mit einem Wegwerf-Zertifikat im Scratchpad getestet (osslsigncode 2.13, alle vier Windows-Dateien signiert und geprüft).
- Für HSM/Cloud-Signieren reicht PKCS#12 nicht (der Schlüssel ist nicht exportierbar). Nötig wäre statt `-pkcs12`: `-pkcs11module <Modul>` plus `-pkcs11cert <URI>` und `-key <URI>` (bei Certum etwa mit dem `ssign-pkcs11`-Modul), PIN/TOTP über Umgebungsvariablen statt Dateien. Für SignPath entfiele der lokale Schritt ganz: Signiert wird in GitHub Actions, `build.sh` bliebe nur für Tests/Linux-Builds und die Prüfsummen müssten nach dem Rückholen der signierten Dateien neu erzeugt werden (Release-Prozess ändert sich).

### Vorgeschlagene Aufgaben (an worker-docs/specialist-release über den Lead)
1. `LICENSE` (MIT) im Repo ergänzen (Voraussetzung für SignPath und gute OSS-Praxis); Eintrag in FEATURES.md.
2. Entscheidung des Nutzers: SignPath (CI-Umbau) oder Certum (lokal, 49 EUR, Personenname im Zertifikat).
3. Falls SignPath: GitHub-Actions-Workflow für Windows-Build + SignPath-Connector entwerfen, Seite "Code signing policy" und Datenschutztext (DE/EN) für `docs/` vorbereiten; offene Frage: Genehmigung per Einzelperson und NSIS-Deep-Signing klären.
4. Falls Certum: Probelauf von `ssign` bzw. `ssign-pkcs11` mit `osslsigncode` gegen eine Testdatei unter Linux, danach den `build.sh`-Schritt auf PKCS#11 statt PKCS#12 auslegen.
5. `build.sh`: PKCS#12-Variante ist eingebaut (siehe oben); bei Certum/HSM auf PKCS#11 (`-pkcs11module`, `-pkcs11cert`, `-key <URI>`) erweitern.
6. Updater (`updater*.go`): bei signierten Dateien prüfen, dass die Hashprüfung weiter gegen die signierte Datei läuft (bereits so, da `SHA256SUMS` nachgelagert erzeugt wird).

---

## Quellenliste (alle am 2026-11-12 abgerufen)

SignPath
- https://signpath.org/terms (belegt)
- https://signpath.org/ (belegt)
- https://signpath.org/apply (belegt: nur Überschrift)
- https://docs.signpath.io/origin-verification (belegt)
- https://docs.signpath.io/trusted-build-systems (belegt)
- https://docs.signpath.io/artifact-configuration/syntax (belegt)
- https://pandoc.org/code-signing-policy.html (Beispiel einer Policy-Seite, nur aus Suchtreffer)

Azure Artifact Signing
- https://learn.microsoft.com/azure/trusted-signing/quickstart (belegt, Seitenstand 2026-10-08)
- https://learn.microsoft.com/azure/artifact-signing/how-to-signing-integrations (belegt, Seitenstand 2026-08-03)
- https://learn.microsoft.com/en-us/azure/trusted-signing/how-to-change-sku (belegt, Seitenstand 2026-08-03)
- https://azure.microsoft.com/de-de/pricing/details/artifact-signing/ (belegt: Preise als Platzhalter)
- https://www.nuget.org/packages/SignUniversal.Azure (ungeprüft, Suchtreffer)
- https://nullprogram.com/blog/2026/04/25/ (ungeprüft, Suchtreffer)

CA-Zertifikate, Laufzeit, HSM
- https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options (belegt, Seitenstand 2026-11-10)
- https://cabforum.org/working-groups/code-signing/requirements/ (Suchtreffer-Auszug, Version 3.11.0 vom 2026-06-16)
- https://www.digicert.com/blog/understanding-the-new-code-signing-certificate-validity-change (belegt)
- https://www.thesslstore.com/blog/code-signing-certificates-lifespans-to-drop-to-one-year/ (ungeprüft)
- https://www.encryptionconsulting.com/what-are-the-ca-browser-forum-requirements-for-code-signing-certificate-private-keys-are-you-prepared/ (ungeprüft)
- https://shop.certum.eu/open-source-code-signing-on-simplysign.html (belegt)
- https://support.certum.eu/en/code-signing-required-documents/ (belegt)
- https://www.harica.gr/en/code-signing (belegt, nur Teilangaben)
- https://www.ssl.com/products/software-integrity/code-signing/iv/ und .../ev-sole-proprietor/ (ungeprüft, Suchtreffer)
- https://www.ssldragon.com/ssl-certificates/code-signing/ (ungeprüft, Händler)
- https://docs.digicert.com/en/software-trust-manager/signing-tools/osslsigncode-with-pkcs11.html (Suchtreffer, ungeprüft im Detail)
- https://docs.rs/ssign (belegt)

SmartScreen
- https://learn.microsoft.com/windows/apps/package-and-deploy/smartscreen-reputation (belegt, Seitenstand 2026-08-17)
- https://forum.xojo.com/t/microsoft-smartscreen-and-ov-certificates/86735 (ungeprüft, Anekdote)
- https://learn.microsoft.com/en-us/answers/questions/5900208/smartscreen-reputation-reset-following-ev-certific (ungeprüft)

Lokal geprüft (2026-11-12)
- `/home/claude-code/project/ssh-easy/build.sh` (kein Codesign-Block), `README.md` (Lizenz MIT), `git ls-files` (keine LICENSE-Datei), `.github/workflows/pages.yml` (einziger Workflow), `osslsigncode --version` (2.13).
