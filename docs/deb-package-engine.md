## Le moteur agent : opencode (embarqué dans le paquet)

Le moteur d'exécution des tâches est **opencode** (https://github.com/anomalyco/opencode,
MIT) — l'agent de codage open source qui équipe NemesisCode. Le paquet .deb
**embarque le binaire** (`/usr/share/nemesiscode/opencode`, variante
« baseline » compatible vieux processeurs : SSE2 suffit, aucun AVX requis) :
**aucune installation supplémentaire**, les tâches s'exécutent dès
`nemesiscode on`.

Vérification :

```bash
nemesiscode status    # doit afficher : Moteur agent (opencode) : /usr/share/nemesiscode/opencode
nemesiscode engine    # idem, juste le chemin
```

Remplacement par un binaire plus récent (optionnel) :

```bash
# Télécharger la dernière version depuis GitHub (assets opencode-linux-x64*)
# ou via npm : npm i opencode-ai opencode-linux-x64-baseline
cp chemin/vers/opencode ~/.nemesiscode/opencode
chmod +x ~/.nemesiscode/opencode
```

Emplacements reconnus (dans l'ordre) : `~/.nemesiscode/opencode`,
`/usr/share/nemesiscode/opencode` (embarqué), `~/.local/bin/opencode`,
`/usr/local/bin/opencode`, puis le `PATH`.

### Comment NemesisCode pilote opencode

Chaque message utilisateur lance le mode non-interactif :

```text
opencode run --format json --auto [--continue] --model nemesiscode-ai/<modèle> "<message>"
```

- `--format json` : événements NDJSON sur stdout (text, tool_use, reasoning,
  error…) → mappés vers les événements ACP du frontend (messages, outils,
  erreurs) ;
- `--auto` : auto-approuve les permissions (mode local « confiance ») ;
- `--continue` : reprend la dernière session du workspace (flux « continuer
  la tâche ») ;
- cwd = workspace de la tâche (`~/.nemesiscode/workspaces/<tâche>/`), config
  LLM écrite par le backend dans `<workspace>/opencode.json` (provider
  `nemesiscode-ai` → base_url + api_key du modèle configuré dans l'UI).

Le moteur appelle directement le fournisseur du modèle configuré
(Fireworks, NVIDIA, Cohere, OpenAI-compatible, Custom…) — le réseau de la
machine doit joindre l'API du fournisseur.

Si le moteur est absent, NemesisCode affiche un avertissement au démarrage
mais continue : la configuration des modèles et la création de tâches
fonctionnent.
