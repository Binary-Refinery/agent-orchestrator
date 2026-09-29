# ARTIFAKTORY AO-Produkt: Zielbild (Erst A, dann B)

Stand: 2026-09-29. Fork: `Binary-Refinery/agent-orchestrator`
(Upstream: `Untrivial-ai/agent-orchestrator`, Apache-2.0).

## Ausgangslage

Der Windows-Pilot (AO-351) wurde mit getrennten Windows-Konten,
ProgramData-Pfaden und Admin-Skripten (M1–M4) betrieben. Jede neue Komponente
(Daemon-Start, Worktrees, Login-Daten) brauchte weitere ACL-Freigaben.
Diese Betriebsweise wird nicht produktisiert.

## Entscheidung

**Erst A, dann B.**

### A) Desktop-App, ein Benutzer (aktuell)

- Installation per NSIS-Installer **ohne Admin-Rechte**: `oneClick: false`,
  `perMachine: false`, Installationsverzeichnis wählbar, Desktop- und
  Startmenü-Verknüpfungen, eigener Uninstaller
  (siehe `frontend/makers/maker-nsis.ts`).
- Betrieb im selben Benutzerkonto: Der Daemon startet mit der GUI, keine
  Kontowechsel, keine Scheduled Tasks.
- Datenpfade: Upstream-Defaults unter `~/.ao`, über `AO_DATA_DIR` /
  `AO_RUN_FILE` übersteuerbar. **Kein** ProgramData-Zwang, **kein**
  XDG-Override.
- Updates über electron-updater; das Feed-Repo wird beim Build eingebacken
  (`AO_RELEASE_REPO`, siehe `.github/workflows/build-artifacts.yml`).
- Worker-/Orchestrator-Harnesses (z. B. opencode) laufen im selben Konto
  und nutzen dessen Login.

A löst bewusst nicht: Worker-Isolation, Audit-Ziel, mandantenfähigen
Betrieb (siehe B).

### B) Governance-Service (später)

- Installer/Service richtet Konten, ACLs, Autostart und Updates automatisch ein.
- GUI kontoübergreifend verbindungsfähig; Worker-Isolation; Audit-Anbindung.
- Voraussetzung: Erfahrungen und Patches aus A.

## Referenzen

- Governance-Feature `af-ao-governance-v1`: Branch
  `windows/artifaktory-governance` (PR #1 im Fork).
- Pilot-Tracking: `Artifaktory/Artifaktory#351`.
- Bauen/Installieren/Betrieb: `docs/artifaktory/10-bauen-und-installieren.md`.
