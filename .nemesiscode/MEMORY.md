# 用户指令记忆

本文件记录了用户的指令、偏好和教导，用于在未来的交互中提供参考。

## 格式

### 用户指令条目
用户指令条目应遵循以下格式：

[用户指令摘要]
- Date: [YYYY-MM-DD]
- Context: [提及的场景或时间]
- Instructions:
  - [用户教导或指示的内容，逐行描述]

### 项目知识条目
Agent 在任务执行过程中发现的条目应遵循以下格式：

[项目知识摘要]
- Date: [YYYY-MM-DD]
- Context: Agent 在执行 [具体任务描述] 时发现
- Category: [运维部署|构建方法|测试方法|排错调试|工作流协作|环境配置]
- Instructions:
  - [具体的知识点，逐行描述]

## 去重策略
- 添加新条目前，检查是否存在相似或相同的指令
- 若发现重复，跳过新条目或与已有条目合并
- 合并时，更新上下文或日期信息
- 这有助于避免冗余条目，保持记忆文件整洁

## 条目

[Online 预览构建与验证码验收]
- Date: 2026-07-26
- Context: Agent 在排查 online 构建后登录验证码失败时发现
- Category: 构建方法|测试方法|排错调试
- Instructions:
  - 在 `frontend` 运行 `pnpm run build:online` 验证 online 生产构建。
  - 启动 online 开发预览时显式设置 API 目标，例如 `TARGET=https://monkeycode-ai.com pnpm run dev:online -- --host 0.0.0.0 --port <PORT>`。
  - 获得预览地址后运行 `PREVIEW_URL=<URL> pnpm run check:online-preview`，验证 CAP JavaScript、WASM 和 challenge API。
  - 自动健康检查通过后，在浏览器完成一次真实验证码求解和登录，再开始登录后页面的 UI 验收。
  - UI 验收需等待 `document.fonts.ready`，确认 JetBrains Mono Variable 与 Noto Sans SC Variable 已加载，并检查浏览器控制台和 Network 中没有字体资源失败。
  - 在 320px、375px、390px、430px 和 1280px 对照基准页面核对字体族、字号、字重和行高，字体变化应作为构建后高频回归项记录和处理。
  - Vite 日志出现 `Must set target or forward` 表示 `/api` proxy 缺少 `TARGET`，应使用显式目标重启预览。

[Mode local backend (machine hôte = environnement de dev)]
- Date: 2026-08-12
- Context: Rebranding NemesisCode + passage en usage local (hors cloud)
- Category: 环境配置|排错调试
- Instructions:
  - Le backend consomme taskflow uniquement via l'interface `taskflow.Clienter` (backend/pkg/taskflow/client.go) — le mode local (`MCAI_TASKFLOW_MODE=local`) branche `pkg/taskflow/local` qui exécute l'agent directement sur la machine hôte (workspace = ~/.nemesiscode/workspaces/<vm_id>, agent = $MCAI_TASKFLOW_LOCAL_AGENT_BIN --task-config <ws>/nemesis-task.json).
  - `PrepareCreate` exige une ligne host en base : en mode local `pkg/localhost.EnsureHost` (appelé dans cmd/server/main.go) upsert l'hôte `local-<hostname>` au nom de l'init-team user (MCAI_INIT_TEAM_EMAIL) — sans admin, l'hôte n'est visible que du premier user.
  - ClickHouse/Loki non configurés (addr vide) sont déjà no-op (tasklog providers et modelusage.Recorder gèrent nil) — pas besoin de les lancer en local.
  - Contrat agent v1 : processus sur stdout émet soit des octets bruts (→ chunk output), soit des lignes JSON {"event","kind","data","seq"} (→ chunks structurés) ; `task-ended` = fin de processus.
  - Limites v1 documentées dans docs/local-mode-design.md : terminal sans PTY (resize ignoré), buffer TaskLive en mémoire, AutoApprove/AskUserQuestion journalisés seulement, protocole réel ohmyagent à aligner (étape 4).
  - Vérifier le Go avec le parseur tree-sitter (npm: web-tree-sitter + tree-sitter-go.wasm) car le sandbox n'a pas de toolchain Go (go.dev/dl.google.com bloqués).

[Frontend: providers API + sélection de modèles]
- Date: 2026-08-12
- Context: Le frontend web avait un champ provider figé (BaiZhiCloud) dans les dialogues de modèles
- Category: 环境配置|排错调试
- Instructions:
  - La gestion des modèles vit dans `frontend/src/components/console/settings/` (user) et `frontend/src/components/manager/` (admin) : `add-model.tsx` / `edit-model.tsx` + `provider-model-combobox.tsx` (liste des modèles du provider via `getProviderModelList`).
  - Les presets de providers (NVIDIA NIM, Fireworks, Cohere, OpenAI, DeepSeek, Custom OpenAI-format…) sont définis dans `PROVIDER_PRESETS` (add-model.tsx des deux dossiers) ; choisir un preset remplit base URL + interface type (`applyProviderPreset`).
  - Backend : `biz/setting/usecase/model.go` GetProviderModelList — providers OpenAI-compatibles (dont NVIDIA/Fireworks/Custom) → GET {base}/models ; Cohere/AzureOpenAI/Volcengine → liste statique `domain.ModelProviderBrandModelsList` (consts/model.go pour les noms).
  - Nouveaux providers à ajouter : consts/model.go + domain/model.go (liste statique si besoin) + switch GetProviderModelList + (si hors Chine) isOverseasProvider.
  - Les fichiers de composants ne doivent JAMAIS contenir de caractères CJK (tests i18n `assert.doesNotMatch(cjkPattern)`) — commentaires en anglais.
  - Terminal web : page task → panneaux files/terminal/changes/preview ; terminal VM aussi dans settings → VMs → bouton → /console/terminal?envid=. En mode local le terminal = shell sur la machine hôte (workspace de la tâche).
