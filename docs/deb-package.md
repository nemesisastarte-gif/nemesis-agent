# Paquet .deb « tout-en-un » de NemesisCode

Un paquet Debian/Ubuntu qui contient **tout** le nécessaire pour faire
tourner NemesisCode sur un PC Linux 64 bits, **sans rien installer d'autre** :

- le binaire backend **statique** (aucune librairie système requise, aucun
  AVX requis — compatible Core 2 Duo et vieux PC) ;
- le frontend compilé, servi directement par le backend sur un seul port ;
- base de données SQLite (fichier), Redis intégré en mémoire, stockage
  fichier local — **aucun service externe**.

## Installation

```bash
sudo dpkg -i nemesiscode_1.0.0_amd64.deb
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

## Le moteur agent (ohmyagent)

L'interface complète (modèles, fournisseurs, création de tâches) fonctionne
sans le moteur. Pour que les **tâches s'exécutent réellement**, il faut le
binaire du moteur `ohmyagent` (dépôt privé `chaitin/OhMyAgent`) :

```bash
# Option 1 (recommandée) : copier le binaire compilé
cp chemin/vers/ohmyagent ~/.nemesiscode/ohmyagent
chmod +x ~/.nemesiscode/ohmyagent

# Option 2 : variable d'environnement au lancement
MCAI_TASKFLOW_LOCAL_AGENT_BIN=/chemin/vers/ohmyagent nemesiscode on
```

Emplacements reconnus automatiquement (dans l'ordre) :
`~/.nemesiscode/ohmyagent`, `/usr/share/nemesiscode/ohmyagent`,
`~/.local/bin/ohmyagent`, `/usr/local/bin/ohmyagent`, puis le `PATH`.

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
# Produit : dist-deb/nemesiscode_1.0.0_amd64.deb
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
