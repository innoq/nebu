# ADR-011: E2EE — Evaluierung & Zurückstellung

## Status

Accepted — E2EE zurückgestellt; Option 3 (Compliance-Bot) ist präferierter Ansatz für spätere Phase

## Kontext

Im Rahmen der Compliance-Architektur wurde evaluiert, ob und wie Ende-zu-Ende-Verschlüsselung (E2EE)
mit der Anforderung nach vollständigem Compliance-Zugriff vereinbar ist.

Nebu zielt auf compliance-first-Organisationen ab, die server-seitige Nachrichtensichtbarkeit für
Audit-Logging, Four-Eyes-Compliance-Zugriff und rechtlichen Export benötigen. Die E2EE-Stubs
(`keys/upload`, `keys/query`, `keys/device_signing/upload`, `room_keys/*`) bleiben in Betrieb,
um Client-Fehlerdialoge zu verhindern. Es wird kein echtes Key-Material gespeichert.

## Evaluierte Optionen

### Option 1: Managed E2EE mit Escrow-Keys

Jede Nachricht wird client-seitig verschlüsselt. Der Message Key wird zusätzlich mit einem
Escrow Public Key eingeschlossen, sodass ein privilegierter Compliance-Zugriff über den
Escrow Private Key möglich bleibt.

**Warum nicht umsetzbar:** Der Escrow-Schritt muss zwingend im sendenden Client erfolgen — der
Server sieht nur Ciphertext und kann nichts nachträglich hinzufügen. Weder Element noch FluffyChat
(Standard) implementieren diesen Schritt. Da Nebu explizit Standard-Matrix-Clients unterstützen
soll, würde jeder Nicht-Nebu-Client den Escrow-Schritt überspringen. Das Ergebnis wäre ein
Zwei-Klassen-System: Compliance-Garantie nur für Nutzer des Nebu-eigenen Clients — was die
Architekturgarantie bricht.

### Option 2: Gateway-Terminated Encryption

Der Client verschlüsselt bis zum API-Gateway. Das Gateway entschlüsselt und speichert Nachrichten
je nach Room-Policy (Plaintext oder Escrow-verschlüsselt) in PostgreSQL.

**Warum nicht als E2EE vermarktbar:** "End-to-End" impliziert, dass kein Zwischenpunkt Klartext
sieht. Da das Gateway entschlüsselt, ist dies technisch korrekt als Transport Security mit
Policy-based Storage at Rest zu bezeichnen — nicht als E2EE. Gegenüber Kunden wäre die Bezeichnung
"E2EE" irreführend. Der Mehrwert gegenüber dem bestehenden TLS 1.3 ist zudem gering, da TLS
bereits "verschlüsselt bis zum Gateway" leistet. Der echte Wert — Policy-based Encryption at Rest
pro Raum — ist unabhängig von E2EE realisierbar und bleibt als spätere Option erhalten.

### Option 3: Compliance-Bot als Raum-Mitglied (präferierter Ansatz, zurückgestellt)

Ein server-seitiger Compliance-Bot ist als legitimes Matrix-Mitglied in relevanten Räumen
eingetragen. Er empfängt Nachrichten über die normale Megolm-Session, entschlüsselt sie mit
seinem Device Key und archiviert sie Escrow-verschlüsselt in PostgreSQL. Element zeigt korrekt
das Schloss-Icon — die Verschlüsselung ist technisch echt.

**Vorteile:**
- Matrix-protokollkonform, funktioniert mit allen Standard-Clients ohne Anpassung
- Transparent für Nutzer (Bot in Mitgliederliste sichtbar)
- Passt zur bestehenden `compliance_officer`-Rolle
_Source: `README.md`, §Current Limitations (No End-to-End Encryption); `memory/project_e2ee_direction.md` (Server-side decryption model — Managed E2EE); Story 9-11 Dev Notes_

**Offene Punkte vor Implementierung:**
- Bot-Device-Key-Management (Generierung, Rotation, Backup)
- Cross-Signing-Strategie, damit Element keine "unverifizierte Geräte"-Warnung zeigt
- Verhalten bei Bot-Ausfall (Nachrichten gehen durch, Archivierung pausiert)
- Rechtliche Kommunikationspflicht gegenüber Nutzern je nach Jurisdiktion

## Entscheidung

**E2EE wird zurückgestellt.** Die Implementierung erfolgt, sobald:

1. Matrix-seitige E2EE-Anforderungen (`e2ee_required`) den Compliance-Bot-Flow stabilisiert haben
2. FTS (Full-Text Search, ADR-010) und Spaces implementiert sind

Option 3 (Compliance-Bot) bleibt der präferierte Ansatz für eine spätere Phase.

Die E2EE-Stubs bleiben unverändert in Betrieb. Epic 10 ruht bis zur Wiederaufnahme.

## Konsequenzen

- Keine neuen E2EE-Implementierungsarbeiten bis zur Wiederaufnahme
- E2EE-Stubs (`keys/upload`, `keys/query`, `keys/device_signing/upload`, `room_keys/*`) bleiben aktiv
- Option 3 (Compliance-Bot) wird als nächster Evaluierungsschritt aufgegriffen, wenn die unter
  "Entscheidung" genannten Voraussetzungen erfüllt sind
- Policy-based Encryption at Rest (unabhängig von E2EE) bleibt als eigenständige Option offen

_Entscheidungsdatum: 2026-05-08_