# Rebranding — MonkeyCode → NemesisCode

Document récapitulatif du rebranding du fork vers **NemesisCode** (aigle de la
justice), à destination de l'équipe.

## Ce qui a été renommé (commit initial du rebranding)

- **Marque** : `MonkeyCode` → `NemesisCode`, `monkeycode` → `nemesiscode`,
  `MONKEYCODE` → `NEMESISCODE` (y compris `Monkeycode` / `monkeyCode` dans les
  identifiants générés et le code client).
- **Module Go** : `github.com/chaitin/MonkeyCode/backend` →
  `github.com/teteekoue/NemesisCode/backend` (go.mod + ~700 imports, ent
  compris).
- **Packages npm** : `monkeycode-ai`, `nemesiscode-ui`, `nemesiscode-ui-next`,
  `nemesiscode-mobile-expo`, `nemesiscode-browser-extension` (+ lockfiles).
- **Desktop (Tauri)** : `productName: NemesisCode`, identifiant
  `com.nemesiscode.app`, crate `nemesiscode-desktop`, icônes Windows/macOS
  régénérées (ico multi-tailles + icns).
- **Mobile** : nom/slug/scheme `nemesiscode`, identifiants iOS/Android
  `com.nemesiscode.app` / `com.nemesiscode.mobile.alipay`.
- **Infra Docker** : noms de conteneurs et upstreams nginx
  (`nemesiscode-ai-backend`, `nemesiscode-ai-rustfs`, …).
- **Prompts/UI** : chaînes i18n, terminal de démo (`nemesis account --pro`,
  `dev@nemesis`, `nemesis deploy --self-hosted`), composant `NemesisLogo`
  (mobile), titres, README (EN/CN), fichiers screenshot renommés
  (`nemesiscode-1.png`…).
- **Logo** : nouvel emblème « aigle de la justice » (aigle doré géométrique),
  décliné en :
  - version or sur transparent (interfaces sombres : `logo-light.png`,
    `icon-dark.png`…),
  - version marine sur transparent (interfaces claires : `logo-dark.png`),
  - icône carrée marine + aigle or (app icons, favicon, extension, ico, icns),
  - splash screens mobile (clair + sombre), adaptive icon (fond `#0F1B33`).

## Conservé volontairement (à décider plus tard)

- **URLs externes réelles** inchangées (service en ligne, docs, galerie) :
  `monkeycode-ai.com`, `monkeycode-ai.net`, `monkeycode-ai.online`,
  `monkeycode-ai.gallery`, `monkeycode.docs.baizhi.cloud`. À remplacer par les
  vôtres quand le déploiement personnel aura son domaine.
- **URLs d'install** de la page self-hosting : renommées
  (`nemesiscode-ai.com/online/install`, `nemesiscode-release.oss-…`) mais ces
  domaines/buckets **n'existent pas encore** — à créer ou à pointer vers votre
  infra.
- **Références Chaitin** : liens footer/legal du site web (长亭科技 / Chaitin
  Tech, `www.chaitin.cn`), identifiants de types OpenAPI générés
  (`GitInChaitinNetAi…` dans `frontend/src/api/Api.ts`), email du bot git
  `nemesiscode-ai@chaitin.com`. À nettoyer si souhaité.

## Étapes restantes (pas à pas)

1. **Prompt système de l'agent** : il vit dans le sous-module `agent/`
   (dépôt privé `chaitin/OhMyAgent`, URL passée en HTTPS dans `.gitmodules`).
   Une fois le dépôt accessible, renommer les occurrences `MonkeyCode` dans ses
   fichiers de prompt (`agent/` côté Rust/Go), puis `git submodule update --init agent`.
2. **Régénérer le client API** : `frontend/src/api/Api.ts` contient des noms de
   types générés avec l'ancien chemin (`GitInChaitinNetAi…`). Après un
   `swag init` + `pnpm api` côté backend, les noms seront régénérés à partir du
   nouveau module.
3. **Déploiement** : choisir le domaine/serveur de l'équipe et remplacer les
   URLs ci-dessus (mobile `DEFAULT_BASE_URL`, `app.json` updates, desktop
   `latest.json`…).
4. **Sous-module** : le sous-module `agent` n'est pas clonable publiquement ;
   l'initialiser en interne (`git submodule update --init agent`).

## Validation effectuée

- `tsc -b` (frontend) : OK.
- Suite de tests frontend (`node --test` — 255 tests) : 246 OK, 9 échecs
  **préexistants** (identiques sur l'arbre d'origine, sans lien avec le
  rebranding).
- Imports Go : 0 référence restante à l'ancien module (vérification statique ;
  pas de toolchain Go dans cet environnement).
