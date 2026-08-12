# Documentation NemesisCode

Bienvenue dans la documentation de NemesisCode — une plateforme de
développement pilotée par l'IA, conçue pour un usage personnel ou en petite
équipe, avec la possibilité de fonctionner entièrement en local.

## Prise en main

- **[README](../README.md)** — présentation, démarrage rapide, configuration
  des fournisseurs d'API.

## Fonctionnement

| Document | Contenu |
|---|---|
| [Mode local](local-mode-design.md) | La machine hôte comme environnement de développement : architecture, contrat agent v1, variables d'environnement, feuille de route |
| [Rebranding](rebranding.md) | Historique du passage de MonkeyCode à NemesisCode (renommages, logo, URLs conservées, étapes restantes) |

## Architecture

| Document | Contenu |
|---|---|
| [Glossaire d'observabilité](architecture/observability-glossary.md) | Termes, traces et sémantique des résultats (FR) |
| [ADR-0001 — Traçage distribué](architecture/adr/0001-backend-distributed-tracing.md) | Décision d'architecture : OpenTelemetry et W3C Trace Context entre le backend et Taskflow (FR) |

## Notes de développement (historique)

Les dossiers `docs/superpowers/plans/` et `docs/superpowers/specs/`
contiennent les plans et spécifications de développement au fil de l'eau
(rédigés à l'époque en chinois, conservés tels quels pour l'historique).

## Ressources externes

- Projet d'origine : [MonkeyCode](https://github.com/chaitin/MonkeyCode)
- Licence : [AGPL-3.0](../LICENSE)
