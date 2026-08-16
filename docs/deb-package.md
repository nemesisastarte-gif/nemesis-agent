# Paquet .deb « tout-en-un » de NemesisCode

Un paquet Debian/Ubuntu qui contient **tout** le nécessaire pour faire
tourner NemesisCode sur un PC Linux 64 bits, **sans rien installer d'autre** :

- le binaire backend **statique** (aucune librairie système requise, aucun
  AVX requis — compatible Core 2 Duo et vieux PC) ;
- le frontend compilé, servi directement par le backend sur un seul port ;
- base de données SQLite (fichier), Redis intégré en mémoire, stockage
  fichier local — **aucun service externe**.

## Téléchargement et installation

Le paquet courant est conservé dans `releases/v1.2.2/` et référencé par la
page **GitHub Releases**. Les anciens paquets non fonctionnels ont été retirés :

```bash
curl -fLO https://github.com/nemesisastarte-gif/nemesis-agent/raw/v1.2.2/releases/v1.2.2/nemesiscode_1.2.2_amd64.deb
curl -fLO https://github.com/nemesisastarte-gif/nemesis-agent/raw/v1.2.2/releases/v1.2.2/SHA256SUMS
sha256sum -c SHA256SUMS
sudo dpkg -i nemesiscode_1.2.2_amd64.deb
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

## Le moteur agent portable (embarqué dans le paquet)

Le CLI officiel récent d'opencode utilise Bun et son build « baseline »
requiert encore SSE4.2. NemesisCode embarque à la place la dernière version
intégralement Go d'opencode (`v0.0.52`, MIT), épinglée au commit
`2b258b14732c9a0f50cc3552a27ebf0f68be4e53` et compilée avec
`GOAMD64=v1`, sans SSE4.2 ni AVX.

- `/usr/share/nemesiscode/opencode` : adaptateur CLI ;
- `/usr/share/nemesiscode/opencode-portable` : moteur statique.

```bash
nemesiscode engine
nemesiscode doctor
```

Le moteur reçoit directement le modèle, l'URL, la clé, le type d'API et les
limites configurés dans l'interface. Il prend en charge Fireworks et les API
OpenAI-compatible, Anthropic, les outils de fichiers/shell et la reprise de
session entre les tours.

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
# Produit : dist-deb/nemesiscode_1.2.2_amd64.deb
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
