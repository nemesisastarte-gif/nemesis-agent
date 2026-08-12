# ADR-0001 : Traçage distribué du backend NemesisCode

- Statut : accepté
- Date : 2026-07-16
- Décideurs : équipe NemesisCode

## Contexte

Le backend NemesisCode est le point d'entrée métier des tâches utilisateur
côté serveur, et il appelle Taskflow via HTTP, WebSocket, etc. Le dépannage
repose aujourd'hui principalement sur les journaux métier et les champs
`task_id`, `session_id`, `request_id`, ce qui ne permet pas de reconstituer de
manière fiable la chaîne de causalité technique d'une opération entre
NemesisCode et Taskflow.

Ce chantier démarre par le backend NemesisCode, sans modifier Web, Desktop,
Mobile, ni les agents VM ou les coding agents. Une requête client qui entre
dans NemesisCode crée une nouvelle trace ; les interactions de Taskflow avec
l'agent sont enregistrées par Taskflow comme une frontière boîte noire.

## Décision

Mettre en place un traçage distribué de NemesisCode Backend vers Taskflow avec
OpenTelemetry et le W3C Trace Context. Une tâche NemesisCode n'est pas une
trace unique durant des heures, mais un ensemble de traces à courte durée de
vie, agrégées via `nemesiscode.task.id`. Les appels synchrones utilisent des
spans parent-enfant ; les phases asynchrones utilisent des liens de span.

```text
Client public
    │  ne pas faire confiance au traceparent externe
    ▼
Backend NemesisCode ── traceparent ──▶ Taskflow ──▶ frontière boîte noire Agent
    ▲                                  │
    └────────── traceparent ───────────┘
                    rappel

NemesisCode / Taskflow ── OTLP ──▶ Collecteur OpenTelemetry ──▶ Tempo
NemesisCode / Taskflow ── journaux structurés ──▶ VictoriaLogs
```

### Entrées de trace et frontières de confiance

- Les interfaces publiques de NemesisCode ignorent les `traceparent` et
  `tracestate` envoyés par les clients et créent une nouvelle trace racine.
- À l'avenir, si une passerelle contrôlée crée des traces, seul un contexte
  nettoyé et réinjecté par la passerelle sera digne de confiance.
- Les points de rappel internes de Taskflow peuvent extraire le contexte de
  trace Taskflow authentifié.
- Le Baggage OpenTelemetry n'est pas utilisé pour propager des champs métier.

### Règles de propagation

- Lorsque NemesisCode appelle Taskflow, le contexte est propagé via les
  en-têtes W3C `traceparent` et `tracestate`.
- HTTP utilise les en-têtes standard ; WebSocket ne propage le contexte qu'à
  la phase de poignée de main.
- Tokens, fragments de journaux, flux d'octets du terminal et battements de
  cœur ne créent pas de span par élément.
- `task_id`, `session_id`, `request_id`, `vm_id` continuent de transiter par
  les paramètres et corps de messages existants ; les deux côtés les écrivent
  en attributs de span et en journaux structurés.
- Les rappels asynchrones initiés par Taskflow forment une nouvelle trace ;
  NemesisCode traite le rappel comme un span aval de cette trace.

### Granularité des spans

Seuls les appels transfrontières et les étapes métier critiques sont tracés ;
aucun span n'est créé pour les fonctions Go ordinaires.

Les spans principaux de NemesisCode incluent :

- la réception des requêtes de création, démarrage, arrêt, redémarrage et
  changement de modèle de tâche ;
- la validation utilisateur, projet, modèle et quota ;
- la création ou la mise à jour de l'enregistrement de tâche ;
- l'appel à Taskflow ;
- l'établissement des connexions longues (Task Live, Control, Terminal) ;
- la réception des rappels Taskflow ;
- les appels à la base de données, Redis et aux services externes.

Les health checks, battements de cœur, sondages et blocs de données de flux ne
produisent par défaut aucun span métier.

### Spécification des attributs

Privilégier les Semantic Conventions OpenTelemetry. Les attributs personnalisés
utilisent les noms suivants :

| Attribut | Signification |
| --- | --- |
| `nemesiscode.task.id` | ID de tâche NemesisCode |
| `nemesiscode.agent.session.id` | ID d'une session d'exécution de l'agent |
| `nemesiscode.request.id` | ID d'une commande ou interaction métier |
| `nemesiscode.project.id` | ID de projet NemesisCode |
| `taskflow.vm.id` | ID de machine virtuelle Taskflow |
| `taskflow.terminal.session.id` | ID de session terminal |
| `task.outcome` | `succeeded`, `failed`, `cancelled` ou `rejected` |

Les champs métier, protocoles et bases de données existants restent inchangés ;
la normalisation des noms n'a lieu qu'au niveau de la télémétrie.

### Sécurité des données

Les attributs de trace suivent une liste blanche stricte. Sont autorisés : nom
du service, version, environnement, modèle de routage, méthode, code de
statut, identifiants métier normalisés, étape d'opération, nombre de tentatives
et catégorie d'erreur assainie.

Les éléments suivants sont interdits dans les traces :

- prompts, réponses de modèles, code, contenu de fichiers et chemins de
  fichiers complets ;
- corps de requêtes/réponses et paramètres d'URL ;
- URL des dépôts Git ;
- Authorization, cookies, tokens et clés secrètes ;
- noms d'utilisateur, e-mails, numéros de téléphone et IP clientes ;
- paramètres SQL, clés et valeurs Redis ;
- messages d'erreur bruts pouvant contenir le corps de réponses tierces.

Les spans de base de données ne conservent que le type d'opération, le nom de
table, la durée et la catégorie d'erreur ; les spans Redis ne conservent que le
nom de commande.

### Corrélation des journaux

Les journaux d'exécution du service continuent d'être écrits dans
VictoriaLogs ; Loki n'est pas introduit comme backend de journaux
d'exécution. Le handler de journaux extrait et complète depuis le
`context.Context` :

- `trace_id` ;
- `span_id` ;
- `nemesiscode.task.id` ;
- `nemesiscode.agent.session.id` ;
- `nemesiscode.request.id` ;
- `service.name`.

Grafana doit pouvoir ouvrir une trace Tempo depuis un journal VictoriaLogs via
`trace_id`, et interroger VictoriaLogs depuis Tempo via `trace_id` et une
fenêtre temporelle.

La capacité métier Loki utilisée par Taskflow pour les flux de sortie de
tâches ne fait pas partie de cette migration.

### Sémantique des erreurs

- Exceptions système, échecs réseau, dépassements de délai, échecs base de
  données et échecs d'ordonnancement Taskflow → marqués `Error`.
- HTTP 5xx et gRPC `Internal`, `Unavailable`, `DeadlineExceeded` → marqués
  `Error`.
- Annulation explicite par l'utilisateur → `task.outcome=cancelled`, sans
  marquage d'erreur système.
- Échec de validation des paramètres, non-autorisation, quota insuffisant et
  limite de concurrence atteinte → `task.outcome=rejected` avec raison
  normalisée, sans marquage d'erreur système.
- Le corps d'erreur brut n'entre pas dans les traces.

### Export et isolation de panne

- Exportateur asynchrone par lots vers le collecteur OpenTelemetry via OTLP.
- Provider Noop lorsque l'endpoint du collecteur n'est pas configuré ou que le
  traçage est désactivé.
- File d'attente bornée ; en cas de file pleine, de blocage réseau ou de
  collecteur indisponible, les traces sont abandonnées sans bloquer le métier.
- Flush borné dans le temps à l'arrêt de l'application, puis sortie.
- Les échecs d'export, les spans abandonnés et l'utilisation de la file sont
  exposés via Prometheus, avec limitation du débit des journaux d'erreur.

Privilégier les variables d'environnement OpenTelemetry standard pour
configurer l'adresse d'export, le protocole, le nom du service, l'environnement
et les attributs de ressource. La configuration applicative n'ajoute que
l'interrupteur on/off et les limites que les variables standard ne peuvent pas
exprimer.

### Échantillonnage et rétention

L'application envoie les spans candidats au collecteur, qui applique
l'échantillonnage de queue :

- environnements de dev/test : rétention 100 % ;
- production, requêtes normales : rétention 10 % ;
- erreurs, dépassements de délai, opérations de tâche critiques et requêtes
  lentes : rétention 100 % ;
- health checks, battements de cœur et sondages : abandon par défaut ;
- seuil initial des requêtes HTTP lentes : 2 secondes ; les autres opérations
  utilisent des seuils dédiés.

Rétention des traces : 7 jours en production, 3 jours en test, 24 heures en
développement local. VictoriaLogs conserve sa politique de rétention actuelle.

### Budget de performance

- Surcoût supplémentaire HTTP P95 ≤ 5 ms ;
- augmentation CPU applicative ≤ 3 % ;
- mémoire maximale de la file télémétrique par instance : 64 Mio ;
- débit des connexions longues WebSocket et gRPC : dégradation ≤ 2 % ;
- aucun appel réseau télémétrique synchrone dans le chemin des requêtes ;
- en cas de panne du collecteur, interfaces métier et connexions longues ne
  doivent pas se dégrader de façon perceptible.

### Ordre de mise en service

1. Déployer le collecteur, Tempo et la source de données Grafana ; configurer
   la navigation bidirectionnelle VictoriaLogs.
2. Taskflow : instrumentation des points de réception et corrélation des
   journaux, export désactivé par défaut.
3. NemesisCode : instrumentation serveur et propagation du contexte vers
   Taskflow.
4. Activer 100 % en test et valider l'acceptation.
5. Activer en production sur une instance ou un petit flux, surveiller les
   performances et la santé de l'export.
6. Une fois le budget de performance respecté, activer la politique
   d'échantillonnage de production complète.

### Critères d'acceptation

- La création normale d'une tâche montre l'arrivée NemesisCode, les opérations
  de données, l'appel Taskflow et la phase d'ordonnancement ;
- les résultats asynchrones de Taskflow peuvent être reliés à la phase de
  création via les liens de span et `task_id` ;
- l'indisponibilité de Taskflow, l'échec de création de VM et le dépassement
  de délai de l'agent sont correctement conservés et marqués comme erreurs ;
- l'annulation utilisateur et les refus métier ne sont pas signalés à tort
  comme erreurs système ;
- une reconnexion WebSocket forme un span de connexion distinct, agrégable par
  identifiant métier ;
- VictoriaLogs et Tempo permettent la navigation bidirectionnelle ;
- collecteur arrêté : fonctionnalités métier, latences et connexions longues
  restent normales ;
- aucune donnée interdite dans les traces ;
- les tests de performance respectent le budget défini.

## Impacts

### Impacts positifs

- Depuis `trace_id`, on peut reconstituer un appel synchrone ; depuis
  `task_id`, on peut agréger les multiples traces d'une tâche à longue durée
  de vie.
- Journaux et traces forment une entrée de dépannage unifiée.
- L'agent n'est pas modifié : risque de protocole et de livraison réduit.
- Les pannes de télémétrie sont isolées des pannes métier.

### Coûts

- Maintenance du collecteur, de Tempo, des règles d'échantillonnage et de la
  configuration Grafana.
- La corrélation asynchrone exige que Taskflow persiste le contexte de trace
  initial.
- Le code asynchrone critique doit propager correctement le `context.Context`
  pour que les journaux se corrèlent automatiquement.
- L'échantillonnage de queue augmente le trafic entrant du collecteur et la
  pression mémoire à court terme.

## Hors périmètre

- Instrumentation des clients Web, Desktop, Mobile ;
- traçage interne ou modification de protocole des agents VM / coding agents ;
- migration des journaux métier de tâches de Loki vers VictoriaLogs ;
- maintenance d'un cluster Tempo de niveau production dans ce dépôt.
