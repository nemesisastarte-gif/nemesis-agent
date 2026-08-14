# Paquet .deb « tout-en-un » de NemesisCode

Un paquet Debian/Ubuntu qui contient **tout** le nécessaire pour faire
tourner NemesisCode sur un PC Linux 64 bits, **sans rien installer d'autre** :

- le binaire backend **statique** (aucune librairie système requise, aucun
  AVX requis — compatible Core 2 Duo et vieux PC) ;
- le frontend compilé, servi directement par le backend sur un seul port ;
- base de données SQLite (fichier), Redis intégré en mémoire, stockage
  fichier local — **aucun service externe**.

## Téléchargement et installation

Le paquet courant est conservé dans `releases/v1.2.0/` et référencé par la
page **GitHub Releases**. Les anciens paquets non fonctionnels ont été retirés :

```bash
curl -fLO https://github.com/nemesisastarte-gif/nemesis-agent/raw/v1.2.0/releases/v1.2.0/nemesiscode_1.2.0_amd64.deb
curl -fLO https://github.com/nemesisastarte-gif/nemesis-agent/raw/v1.2.0/releases/v1.2.0/SHA256SUMS
sha256sum -c SHA256SUMS
sudo dpkg -i nemesiscode_1.2.0_amd64.deb
```

(En cas de dépendance manquante : `sudo apt-get install -f` ne sera pas
nécessaire — le paquet ne dépend de rien, en dehors de `curl` recommandé
pour la vérification de démarrage.)

## Utilisation

```bash
nemesiscode on       # démarre tout → http://localhost:5000
nemesiscode off      # arrête tout
nemesiscode status   # état actuel
nemesiscode logs     # logs en direct
nemesiscode restart
```

À la première connexion : identifiant **Admin** / mot de passe **Admin**
(surchargeable : `MCAI_INIT_TEAM_PASSWORD=... nemesiscode on`).

Toutes les données sont dans `~/.nemesiscode/` :
`nemesiscode.db` (base), `workspaces/` (espaces de travail des tâches),
`uploads/` (fichiers), `.runtime/` (logs).

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

Emplacements reconnus (dans l'ordre) : valeur explicite de
`MCAI_TASKFLOW_LOCAL_AGENT_BIN`, `/usr/share/nemesiscode/opencode` (baseline
embarqué), `~/.nemesiscode/opencode`, `~/.local/bin/opencode`,
`/usr/local/bin/opencode`, puis le `PATH`. Chaque candidat est exécuté avec
`--version` avant sélection ; une ancienne copie AVX qui plante est ignorée.

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

## Changement de port

Par défaut le port est **5000**. Pour changer :

```bash
NEMESISCODE_PORT=8080 nemesiscode on
```

## Désinstallation

```bash
sudo dpkg -r nemesiscode
```

Les données de `~/.nemesiscode/` sont conservées (supprimez le dossier
manuellement si vous voulez tout effacer).

## Reconstruire le paquet soi-même

```bash
scripts/build-deb.sh          # depuis la racine du dépôt
# Produit : dist-deb/nemesiscode_1.2.0_amd64.deb
```

Prérequis : Go 1.25+, Node 22+, pnpm, dpkg-deb. Le binaire est compilé avec
`CGO_ENABLED=0` et `GOAMD64=v1` (compatibilité maximale : SSE2 suffit, pas
d'AVX), donc le paquet s'installe sur n'importe quel Linux x86-64.

## Notes

- Si des tâches créées lors d'une session précédente semblent « bloquées »
  après un redémarrage, supprimez-les depuis l'interface (elles ne sont plus
  reliées au moteur).
- Le port 5000 peut être occupé par un autre programme : `nemesiscode on`
  le détecte et vous invite à changer de port.
