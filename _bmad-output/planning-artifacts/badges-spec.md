# Badges Spec — Nebu

> **WICHTIG:** Diese Spec wird in Story 8.8 in die `README.md` eingefügt.
> Dieses File bitte NICHT in `README.md` einfügen — das ist explizit Out-of-Scope für Story 8.6.
>
> Quelle/Referenz: `tmp/badges.md` enthält den vollständigen Badge-Draft für alle Status/Tech-Stack-Badges.
> Diese Spec ergänzt den Draft um die CI-Live-Badges, die erst nach der Dual-Host-Veröffentlichung aktiv werden.

---

## CI Status Badges — Live (nach Veröffentlichung)

| Badge | Title | Badge-URL | Link-Ziel |
|---|---|---|---|
| GitHub Actions CI | `CI` | `https://github.com/innoq/nebu/actions/workflows/ci.yml/badge.svg` | `https://github.com/innoq/nebu/actions/workflows/ci.yml` |
| GitLab CI Pipeline | `pipeline status` | `https://gitlab.opencode.de/nebu/nebu-server/badges/main/pipeline.svg` | `https://gitlab.opencode.de/nebu/nebu-server/-/pipelines` |
| License | `License` | `https://img.shields.io/badge/license-Apache%202.0-blue.svg` | `LICENSE` |

---

## Sovereign Repos Row — beide Badges nebeneinander

```markdown
[![CI](https://github.com/innoq/nebu/actions/workflows/ci.yml/badge.svg)](https://github.com/innoq/nebu/actions/workflows/ci.yml)
[![pipeline status](https://gitlab.opencode.de/nebu/nebu-server/badges/main/pipeline.svg)](https://gitlab.opencode.de/nebu/nebu-server/-/pipelines)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
```

---

## Einzelne Badge-Snippets

### GitHub Actions Build Badge

```markdown
[![CI](https://github.com/innoq/nebu/actions/workflows/ci.yml/badge.svg)](https://github.com/innoq/nebu/actions/workflows/ci.yml)
```

### GitLab CI Pipeline Badge (opencode.de)

```markdown
[![pipeline status](https://gitlab.opencode.de/nebu/nebu-server/badges/main/pipeline.svg)](https://gitlab.opencode.de/nebu/nebu-server/-/commits/main)
```

### Apache 2.0 License Badge

```markdown
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
```

---

## Hinweise für Story 8.8

- Die GitHub-Actions-Badge wird erst aktiv, wenn `innoq/nebu` auf GitHub existiert und die erste Pipeline gelaufen ist.
- Die GitLab-CI-Badge wird erst aktiv, wenn `gitlab.opencode.de/nebu/nebu-server` existiert und eine Pipeline registriert wurde.
- Beide Badges bis zur Aktivierung weglassen oder mit einem `#` kommentieren — kein toter Badge in der README.
- Für den vollständigen Badge-Block (Tech-Stack, Protokoll, etc.) siehe `tmp/badges.md`.
- Job-Namen in der GitHub-Actions-Badge-URL müssen exakt dem `name: CI` in `.github/workflows/ci.yml` entsprechen.
