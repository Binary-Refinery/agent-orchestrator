# Bauen und Installieren (Modell A)

Belegt am 2026-09-29: `go build ./...` (backend) ok; NSIS-Defaults per Code
geprüft (`frontend/makers/maker-nsis.ts`).

## Voraussetzungen

- Go (Version aus `backend/go.mod`; Toolchain wird bei Bedarf automatisch geladen)
- Node 22, npm (siehe `.github/workflows/build-artifacts.yml`)

## Backend bauen und testen

```powershell
cd backend
go build ./...
go test ./internal/domain/ ./internal/service/... ./internal/session_manager/ ./internal/lifecycle/
```

Hinweise:

- `go.yml` läuft auf `ubuntu-latest`. Einige Tests sind Windows-empfindlich
  (FS-Listing-Codes, Mobile-Pairing-Dateizugriffe, Clone-URL-Tests) und
  scheitern lokal auf Windows; sie scheitern auf pristine `main` identisch
  und sind kein Release-Blocker für A.
- API-Artefakte regenerieren (Repo-Root): `npm run api` = `api:spec`
  (`go generate ./internal/httpd/apispec/...` → `openapi.yaml`) + `api:ts`
  (openapi-typescript → `frontend/src/api/schema.ts`).

## Desktop-App und Installer bauen

```powershell
cd frontend
npm ci
npm run make
```

Ergebnis Windows: NSIS-`Setup.exe` unter `frontend/out/make` (per-user,
kein Admin).

## CI

- `go.yml` / `frontend.yml`: laufen bei PRs mit Backend-/Frontend-Pfaden.
- `build-artifacts.yml`: per `workflow_dispatch` (Parameter `ref`, `version`)
  unsignierte Installer für alle Plattformen als kurzlebige Artefakte plus
  `digests.json`. Benötigt die Repo-Variable `VITE_WORKOS_CLIENT_ID`; ohne
  sie bricht der Build ab. Signierung erfolgt nachgelagert
  (Upstream-Konzept); lokale und Fork-Builds bleiben unsigniert.

## Betrieb (Modell A)

1. `Setup.exe` als normaler Benutzer ausführen (kein Admin).
2. App öffnen, Projekt anlegen, Orchestrator spawnen (Harness z. B. opencode).
3. Daten liegen unter `~/.ao`; keine Umgebungs-Overrides nötig.
