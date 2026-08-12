# Glossaire d'observabilité NemesisCode

## Identifiants métier

| Terme | Champ télémétrique | Définition |
| --- | --- | --- |
| Tâche | `nemesiscode.task.id` | Agrégat métier à longue durée de vie dans NemesisCode. Une tâche peut être associée à plusieurs traces |
| Session d'exécution de l'agent | `nemesiscode.agent.session.id` | Contexte d'une exécution de l'agent ; ne correspond pas à une session de terminal |
| Requête métier | `nemesiscode.request.id` | Identifiant d'une paire commande/interaction/réponse ; ne correspond pas à un ID de trace |
| Projet | `nemesiscode.project.id` | Identifiant de projet NemesisCode |
| Machine virtuelle | `taskflow.vm.id` | Identifiant d'environnement d'exécution géré par Taskflow |
| Session de terminal | `taskflow.terminal.session.id` | Une session de connexion terminal ; ne correspond pas à une session d'exécution de l'agent |

Les champs `task_id`, `session_id`, `request_id` existants dans les protocoles
et la base de données **ne sont pas renommés**. Lors de l'écriture d'une trace,
ils doivent être mappés vers les champs normalisés ci-dessus.

## Termes de traçage

| Terme | Définition |
| --- | --- |
| Trace | Une chaîne d'appels techniques bornée. Une tâche MonkeyCode contient généralement plusieurs traces |
| Span | Une opération d'une trace, par exemple le traitement d'une requête HTTP ou l'appel à Taskflow |
| Contexte de trace | Causalité inter-processus représentée par `traceparent` et `tracestate` |
| Lien de span | Associe une trace asynchrone à une trace antérieure, sans former une relation parent-enfant durable |
| Baggage | Conteneur de données métier propagé avec le contexte de trace. Ce design ne l'utilise pas pour propager les ID métier |
| Trace racine | Trace sans span parent en amont. L'entrée publique de NemesisCode crée une nouvelle trace racine |
| Span de connexion | Décrit l'établissement, la fermeture et la reconnexion d'une connexion longue (WebSocket ou gRPC) ; ne couvre pas tout le cycle de vie des messages |
| Span de frontière | Observation par Taskflow des envois, attentes et réceptions vers l'agent. L'intérieur de l'agent reste une boîte noire |
| Échantillonnage de queue | Après réception des traces candidates complètes, le collecteur décide de les conserver selon erreurs, durées et attributs |

## Stockage et requêtes

| Composant | Rôle |
| --- | --- |
| SDK OpenTelemetry | Crée et exporte les spans en lots dans les processus NemesisCode et Taskflow |
| Collecteur OpenTelemetry | Reçoit l'OTLP, applique l'échantillonnage de queue et transmet les traces |
| Tempo | Stocke les données de traces |
| VictoriaLogs | Stocke les journaux structurés d'exécution de NemesisCode et Taskflow |
| Grafana | Interroge traces et journaux, et fournit la navigation entre les deux |
| Journaux de tâches Loki | Stockage métier actuel des flux de sortie des tâches par Taskflow ; distinct des journaux d'exécution du service |

## Sémantique des résultats

| Résultat | `task.outcome` | Statut du span |
| --- | --- | --- |
| Succès | `succeeded` | Non défini |
| Échec système ou d'exécution | `failed` | Erreur |
| Annulation par l'utilisateur | `cancelled` | Non défini |
| Refus (paramètres, permissions, quota, concurrence) | `rejected` | Non défini |

## Frontières de confiance

- Le contexte de trace entrant d'un client public est **non fiable par
  défaut** : NemesisCode crée une nouvelle trace racine.
- Les requêtes authentifiées de NemesisCode vers Taskflow peuvent propager le
  contexte de trace.
- Les rappels authentifiés de Taskflow vers NemesisCode peuvent propager le
  contexte de trace.
- L'agent ne propage pas le contexte de trace ; il n'est pas modifié dans cette
  itération.

## Classification des données

Les données admises dans une trace incluent : informations sur les ressources
du service, modèles de routage, codes de statut, identifiants métier
normalisés, durées, nombres de tentatives et catégories d'erreurs.
