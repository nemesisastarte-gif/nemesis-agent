package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/GoYoko/web"
	"github.com/samber/do"

	"github.com/teteekoue/NemesisCode/backend/biz"
	"github.com/teteekoue/NemesisCode/backend/config"
	"github.com/teteekoue/NemesisCode/backend/db"
	"github.com/teteekoue/NemesisCode/backend/pkg"
	"github.com/teteekoue/NemesisCode/backend/pkg/localhost"
	"github.com/teteekoue/NemesisCode/backend/pkg/service"
	"github.com/teteekoue/NemesisCode/backend/pkg/store"
	"github.com/teteekoue/NemesisCode/backend/pkg/telemetry"
)

func main() {
	// 初始化配置
	cfg, err := config.Init("./config/server")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 创建 DI 容器
	injector := do.New()

	// 注入配置
	do.ProvideValue(injector, cfg)

	// 注册基础设施
	if err := pkg.RegisterInfra(injector); err != nil {
		fmt.Fprintf(os.Stderr, "failed to register infra: %v\n", err)
		os.Exit(1)
	}

	l := do.MustInvoke[*slog.Logger](injector)
	l.With("config", cfg).Debug("print config")
	w := do.MustInvoke[*web.Web](injector)
	shutdownTelemetry, err := telemetry.Setup(context.Background(), cfg.Telemetry)
	if err != nil {
		l.Warn("failed to setup telemetry, tracing disabled", "error", err)
		shutdownTelemetry = func(context.Context) error { return nil }
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(ctx); err != nil {
			l.Warn("failed to shutdown telemetry", "error", err)
		}
	}()

	// 运行数据库迁移
	if err := store.MigrateSQL(cfg, l); err != nil {
		l.Error("failed to migrate database", "error", err)
		os.Exit(1)
	}

	// 注册业务模块
	biz.RegisterAll(injector)
	biz.RegisterOpenSource(injector)
	biz.InvokeAll(injector)
	biz.InvokeOpenSource(injector)

	// Mode local (MCAI_TASKFLOW_MODE=local) : la machine hôte est
	// l'environnement de développement — on l'enregistre comme hôte en base,
	// sinon la création de tâche échoue (PrepareCreate requiert host.ID).
	if err := localhost.EnsureHost(context.Background(), cfg, do.MustInvoke[*db.Client](injector), l); err != nil {
		l.Warn("failed to register local host (task creation will fail until a host exists)", "error", err)
	}

	// 获取 web 实例并启动服务
	w.PrintRoutes()
	svc := service.NewService(
		service.WithPprof(),
		service.WithLogger(l),
	)
	svc.Add(&server{w: w, addr: cfg.Server.Addr})

	l.Info("starting server", "addr", cfg.Server.Addr)
	if err := svc.Run(); err != nil {
		l.Error("server error", "error", err)
	}
}

type server struct {
	w    *web.Web
	addr string
}

func (s *server) Name() string { return "NemesisCode Service" }
func (s *server) Start() error { return s.w.Run(s.addr) }
func (s *server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.w.Echo().Shutdown(ctx)
}
