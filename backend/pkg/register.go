package pkg

import (
	"context"
	"log/slog"

	"github.com/GoYoko/web"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/samber/do"

	"github.com/teteekoue/NemesisCode/backend/config"
	"github.com/teteekoue/NemesisCode/backend/consts"
	"github.com/teteekoue/NemesisCode/backend/db"
	"github.com/teteekoue/NemesisCode/backend/db/user"
	"github.com/teteekoue/NemesisCode/backend/domain"
	"github.com/teteekoue/NemesisCode/backend/middleware"
	"github.com/teteekoue/NemesisCode/backend/pkg/asr"
	"github.com/teteekoue/NemesisCode/backend/pkg/captcha"
	"github.com/teteekoue/NemesisCode/backend/pkg/cvt"
	"github.com/teteekoue/NemesisCode/backend/pkg/clickhouse"
	"github.com/teteekoue/NemesisCode/backend/pkg/delayqueue"
	"github.com/teteekoue/NemesisCode/backend/pkg/doubao"
	"github.com/teteekoue/NemesisCode/backend/pkg/email"
	"github.com/teteekoue/NemesisCode/backend/pkg/lifecycle"
	"github.com/teteekoue/NemesisCode/backend/pkg/llm"
	"github.com/teteekoue/NemesisCode/backend/pkg/logger"
	"github.com/teteekoue/NemesisCode/backend/pkg/loki"
	"github.com/teteekoue/NemesisCode/backend/pkg/modelusage"
	"github.com/teteekoue/NemesisCode/backend/pkg/msgpush"
	"github.com/teteekoue/NemesisCode/backend/pkg/nls"
	"github.com/teteekoue/NemesisCode/backend/pkg/notify/channel"
	"github.com/teteekoue/NemesisCode/backend/pkg/notify/dispatcher"
	"github.com/teteekoue/NemesisCode/backend/pkg/notify/template"
	"github.com/teteekoue/NemesisCode/backend/pkg/session"
	"github.com/teteekoue/NemesisCode/backend/pkg/store"
	"github.com/teteekoue/NemesisCode/backend/pkg/tasker"
	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow/local"
	"github.com/teteekoue/NemesisCode/backend/pkg/tasklog"
	"github.com/teteekoue/NemesisCode/backend/pkg/telemetry"
	"github.com/teteekoue/NemesisCode/backend/pkg/vmrecycle"
	"github.com/teteekoue/NemesisCode/backend/pkg/ws"
)

// RegisterInfra 注册基础设施依赖
func RegisterInfra(i *do.Injector, w ...*web.Web) error {
	// Logger
	do.Provide(i, func(i *do.Injector) (*slog.Logger, error) {
		cfg := do.MustInvoke[*config.Config](i)
		return logger.NewLogger(&cfg.Logger), nil
	})

	// Redis
	do.Provide(i, func(i *do.Injector) (*redis.Client, error) {
		cfg := do.MustInvoke[*config.Config](i)
		return store.NewRedisCli(cfg), nil
	})

	// Ent DB
	do.Provide(i, func(i *do.Injector) (*db.Client, error) {
		cfg := do.MustInvoke[*config.Config](i)
		l := do.MustInvoke[*slog.Logger](i)
		return store.NewEntDBV2(cfg, l)
	})

	// Web
	if len(w) > 0 && w[0] != nil {
		w[0].Echo().Pre(telemetry.SanitizeIncomingTrace)
		do.ProvideValue(i, w[0])
	} else {
		do.Provide(i, func(i *do.Injector) (*web.Web, error) {
			w := web.New()
			w.Echo().Pre(telemetry.SanitizeIncomingTrace)
			return w, nil
		})
	}

	// Captcha
	do.Provide(i, func(i *do.Injector) (*captcha.Captcha, error) {
		cfg := do.MustInvoke[*config.Config](i)
		return captcha.NewCaptcha(cfg.Security.CaptchaEnabled), nil
	})

	do.Provide(i, email.NewSMTPClient)

	// Session
	do.Provide(i, func(i *do.Injector) (*session.Session, error) {
		cfg := do.MustInvoke[*config.Config](i)
		return session.New(cfg), nil
	})

	// Auth Middleware
	do.Provide(i, func(i *do.Injector) (*middleware.AuthMiddleware, error) {
		sess := do.MustInvoke[*session.Session](i)
		l := do.MustInvoke[*slog.Logger](i)
		cfg := do.MustInvoke[*config.Config](i)

		var opts []func(*middleware.AuthMiddleware)
		if cfg.TaskFlow.Mode == "local" {
			// Mode local : auto-auth — résout le compte admin (init-team)
			// quand aucune session cookie n'est présente. L'interface web
			// est utilisable sans page de connexion.
			dbc := do.MustInvoke[*db.Client](i)
			opts = append(opts, middleware.WithLocalBootstrap(func(ctx context.Context) (*domain.User, error) {
				q := dbc.User.Query()
				if email := cfg.InitTeam.Email; email != "" {
					q = q.Where(user.EmailEQ(email), user.RoleNEQ(consts.UserRoleEnterprise))
				} else {
					q = q.Where(user.RoleNEQ(consts.UserRoleEnterprise))
				}
				u, err := q.Order(user.ByCreatedAt()).First(ctx)
				if err != nil {
					return nil, err
				}
				return cvt.From(u, &domain.User{}), nil
			}))
		}

		return middleware.NewAuthMiddleware(sess, nil, l, opts...), nil
	})

	// TargetActive Middleware
	do.Provide(i, func(i *do.Injector) (*middleware.TargetActiveMiddleware, error) {
		l := do.MustInvoke[*slog.Logger](i)
		activeRepo := do.MustInvoke[domain.UserActiveRepo](i)
		return middleware.NewTargetActiveMiddleware(l, activeRepo), nil
	})

	// Audit Middleware
	do.Provide(i, func(i *do.Injector) (*middleware.AuditMiddleware, error) {
		l := do.MustInvoke[*slog.Logger](i)
		auditUc := do.MustInvoke[domain.AuditUsecase](i)
		userUc := do.MustInvoke[domain.UserUsecase](i)
		return middleware.NewAuditMiddleware(l, auditUc, userUc), nil
	})

	do.Provide(i, func(i *do.Injector) (taskflow.Clienter, error) {
		cfg := do.MustInvoke[*config.Config](i)
		l := do.MustInvoke[*slog.Logger](i)
		// Mode local : la machine hôte est l'environnement de développement.
		// Aucun service taskflow/rustfs requis (voir docs/local-mode-design.md).
		if cfg.TaskFlow.Mode == "local" {
			return local.NewClient(cfg.TaskFlow.Local, l)
		}
		return taskflow.NewClient(taskflow.WithDebug(cfg.Debug), taskflow.WithLogger(l)), nil
	})

	// Tasker（任务状态机）
	do.Provide(i, func(i *do.Injector) (*tasker.Tasker[*domain.TaskSession], error) {
		r := do.MustInvoke[*redis.Client](i)
		l := do.MustInvoke[*slog.Logger](i)
		return tasker.NewTasker(r, tasker.WithLogger[*domain.TaskSession](l)), nil
	})

	// LLM Client
	do.Provide(i, func(i *do.Injector) (*llm.Client, error) {
		cfg := do.MustInvoke[*config.Config](i)
		return llm.NewClient(llm.Config{
			BaseURL:       cfg.LLM.BaseURL,
			APIKey:        cfg.LLM.APIKey,
			Model:         cfg.LLM.Model,
			InterfaceType: llm.InterfaceType(cfg.LLM.InterfaceType),
		}), nil
	})

	// Loki Client
	do.Provide(i, func(i *do.Injector) (*loki.Client, error) {
		cfg := do.MustInvoke[*config.Config](i)
		return loki.NewClient(cfg.Loki.Addr), nil
	})

	do.Provide(i, func(i *do.Injector) (*clickhouse.Client, error) {
		cfg := do.MustInvoke[*config.Config](i)
		l := do.MustInvoke[*slog.Logger](i)
		return clickhouse.New(cfg.ClickHouse, l)
	})

	do.Provide(i, func(i *do.Injector) (*modelusage.Recorder, error) {
		clickhouseClient := do.MustInvoke[*clickhouse.Client](i)
		dbClient := do.MustInvoke[*db.Client](i)
		logger := do.MustInvoke[*slog.Logger](i)
		return modelusage.NewRecorder(clickhouseClient, modelusage.NewEntContextRepo(dbClient), logger), nil
	})

	do.Provide(i, func(i *do.Injector) (*tasklog.Gateway, error) {
		lokiClient := do.MustInvoke[*loki.Client](i)
		clickhouseClient := do.MustInvoke[*clickhouse.Client](i)

		return &tasklog.Gateway{
			Loki:       tasklog.NewLokiProvider(lokiClient),
			ClickHouse: tasklog.NewClickHouseProvider(clickhouseClient),
		}, nil
	})

	// TaskSummary Queue
	do.Provide(i, func(i *do.Injector) (*delayqueue.TaskSummaryQueue, error) {
		r := do.MustInvoke[*redis.Client](i)
		l := do.MustInvoke[*slog.Logger](i)
		return delayqueue.NewTaskSummaryQueue(r, l), nil
	})

	// VM Idle Sleep Queue
	do.Provide(i, func(i *do.Injector) (*delayqueue.VMSleepQueue, error) {
		r := do.MustInvoke[*redis.Client](i)
		l := do.MustInvoke[*slog.Logger](i)
		return delayqueue.NewVMSleepQueue(r, l), nil
	})

	// VM Idle Notify Queue
	do.Provide(i, func(i *do.Injector) (*delayqueue.VMNotifyQueue, error) {
		r := do.MustInvoke[*redis.Client](i)
		l := do.MustInvoke[*slog.Logger](i)
		return delayqueue.NewVMNotifyQueue(r, l), nil
	})

	// VM Idle Recycle Queue
	do.Provide(i, func(i *do.Injector) (*delayqueue.VMRecycleQueue, error) {
		r := do.MustInvoke[*redis.Client](i)
		l := do.MustInvoke[*slog.Logger](i)
		return delayqueue.NewVMRecycleQueue(r, l), nil
	})

	// VM Expire Queue（手动创建的 VM 过期队列）
	do.Provide(i, func(i *do.Injector) (*delayqueue.VMExpireQueue, error) {
		r := do.MustInvoke[*redis.Client](i)
		l := do.MustInvoke[*slog.Logger](i)
		return delayqueue.NewVMExpireQueue(r, l), nil
	})

	do.Provide(i, vmrecycle.NewRecorder)
	do.Provide(i, vmrecycle.NewRecycler)
	do.Provide(i, vmrecycle.NewAnalyzer)
	do.Provide(i, vmrecycle.NewDeadlineRepairer)

	// Channel Registry（通知渠道）
	do.Provide(i, func(i *do.Injector) (*msgpush.WechatClient, error) {
		cfg := do.MustInvoke[*config.Config](i)
		l := do.MustInvoke[*slog.Logger](i)
		r := do.MustInvoke[*redis.Client](i)
		return msgpush.NewWechatClient(cfg, l, r), nil
	})
	do.Provide(i, func(i *do.Injector) (*channel.Registry, error) {
		cfg := do.MustInvoke[*config.Config](i)
		wc := do.MustInvoke[*msgpush.WechatClient](i)
		return channel.NewRegistry(
			channel.NewDingTalkSender(),
			channel.NewFeishuSender(),
			channel.NewWeComSender(),
			channel.NewWebhookSender(),
			channel.NewWechatMPSender(cfg, wc),
		), nil
	})

	// Template Registry（通知模板）
	do.Provide(i, func(i *do.Injector) (*template.Registry, error) {
		return template.NewDefaultRegistry(), nil
	})

	// Dispatcher（通知分发器）
	do.Provide(i, func(i *do.Injector) (*dispatcher.Dispatcher, error) {
		return dispatcher.NewDispatcher(i, nil), nil
	})

	// WebSocket TaskConn
	do.Provide(i, func(i *do.Injector) (*ws.TaskConn, error) {
		return ws.NewTaskConn(), nil
	})

	// WebSocket ControlConn
	do.Provide(i, func(i *do.Injector) (*ws.ControlConn, error) {
		return ws.NewControlConn(), nil
	})

	// NLS 语音识别（可选，配置为空时不注册）—— 仅供一段录音 POST 接口使用
	do.Provide(i, func(i *do.Injector) (*nls.NLS, error) {
		cfg := do.MustInvoke[*config.Config](i)
		if cfg.NLS.AppKey == "" || cfg.NLS.AkID == "" || cfg.NLS.AkKey == "" {
			return nil, nil
		}
		l := do.MustInvoke[*slog.Logger](i)
		r := do.MustInvoke[*redis.Client](i)
		return nls.NewNLS(cfg, l, r), nil
	})

	// 豆包流式 ASR（可选，配置为空时不注册）—— 流式 WS 接口使用
	// 通过 asr.Transcriber interface 暴露,handler 不依赖具体厂商。
	do.Provide(i, func(i *do.Injector) (asr.Transcriber, error) {
		cfg := do.MustInvoke[*config.Config](i)
		l := do.MustInvoke[*slog.Logger](i)
		d := doubao.NewDoubao(cfg, l)
		if d == nil {
			return nil, nil
		}
		return d, nil
	})

	// 任务生命周期管理
	do.Provide(i, func(i *do.Injector) (*lifecycle.Manager[uuid.UUID, consts.TaskStatus, lifecycle.TaskMetadata], error) {
		r := do.MustInvoke[*redis.Client](i)
		l := do.MustInvoke[*slog.Logger](i)

		lc := lifecycle.NewManager(
			r,
			lifecycle.WithLogger[uuid.UUID, consts.TaskStatus, lifecycle.TaskMetadata](l),
			lifecycle.WithTransitions[uuid.UUID, consts.TaskStatus, lifecycle.TaskMetadata](lifecycle.TaskTransitions()),
		)

		lc.Register(
			lifecycle.NewTaskCreateHook(i, lc),
			lifecycle.NewTaskNotifyHook(i),
		)

		return lc, nil
	})

	do.Provide(i, func(i *do.Injector) (*lifecycle.Manager[string, lifecycle.VMState, lifecycle.VMMetadata], error) {
		r := do.MustInvoke[*redis.Client](i)
		l := do.MustInvoke[*slog.Logger](i)
		lc := lifecycle.NewManager(
			r,
			lifecycle.WithLogger[string, lifecycle.VMState, lifecycle.VMMetadata](l),
			lifecycle.WithTransitions[string, lifecycle.VMState, lifecycle.VMMetadata](lifecycle.VMTransitions()),
		)

		lc.Register(
			lifecycle.NewVMTaskHook(i),
			lifecycle.NewVMRecycleHook(i),
		)

		return lc, nil
	})

	return nil
}
