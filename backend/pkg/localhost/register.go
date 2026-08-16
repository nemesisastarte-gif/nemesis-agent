// Package localhost 在 MCAI_TASKFLOW_MODE=local 时，把运行 backend 的机器
// 注册到数据库的 host 表。任务创建（PrepareCreate）按 host.ID 查询该表，
// 没有这条记录就无法创建任务 —— 本机模式必须自注册。
package localhost

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/teteekoue/NemesisCode/backend/config"
	"github.com/teteekoue/NemesisCode/backend/consts"
	"github.com/teteekoue/NemesisCode/backend/db"
	"github.com/teteekoue/NemesisCode/backend/db/host"
	"github.com/teteekoue/NemesisCode/backend/db/task"
	"github.com/teteekoue/NemesisCode/backend/db/user"
	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

const (
	ownerRetryAttempts = 60
	ownerRetryInterval = time.Second
)

// EnsureHost 幂等地注册本机宿主。非 local 模式直接返回 nil。
//
// Propriétaire de l'hôte (ordre de priorité) :
//  1. utilisateur init-team (MCAI_INIT_TEAM_EMAIL) — créé de façon asynchrone
//     au démarrage, on attend donc un peu s'il n'existe pas encore ;
//  2. premier utilisateur admin ;
//  3. premier utilisateur quelconque (avec warning : l'hôte ne sera visible
//     que de ce user, pas de toute l'équipe).
func EnsureHost(ctx context.Context, cfg *config.Config, dbc *db.Client, l *slog.Logger) error {
	if cfg.TaskFlow.Mode != "local" {
		return nil
	}

	hostID := cfg.TaskFlow.Local.HostID
	if hostID == "" {
		hostname, _ := os.Hostname()
		hostID = "local-" + hostname
	}

	owner, err := resolveOwner(ctx, cfg, dbc)
	if err != nil {
		return fmt.Errorf("resolve local host owner: %w (configurez MCAI_INIT_TEAM_EMAIL / MCAI_INIT_TEAM_PASSWORD ou créez un admin)", err)
	}

	info := &taskflow.Host{
		ID:        hostID,
		UserID:    owner.String(),
		Hostname:  hostID,
		Name:      hostID,
		Version:   "nemesis-local-1.2.2",
		TTL:       taskflow.TTL{Kind: taskflow.TTLForever},
		CreatedAt: time.Now().Unix(),
	}

	if err := upsertHost(ctx, dbc, hostID, owner, info); err != nil {
		return fmt.Errorf("upsert local host %s: %w", hostID, err)
	}
	l.InfoContext(ctx, "local host registered", "host_id", hostID, "owner", owner)

	// Au démarrage local, aucune tâche ne peut être légitimement en cours
	// (le moteur n'a pas encore tourné). Les tâches laissées en pending /
	// processing par une session précédente (redémarrage, crash) bloqueraient
	// la création de nouvelles tâches (hook de concurrence) — on les passe
	// en error, état terminal visible dans l'interface.
	if err := reconcileOrphanTasks(ctx, dbc, l); err != nil {
		return fmt.Errorf("reconcile orphan tasks: %w", err)
	}
	return nil
}

// reconcileOrphanTasks marque en error les tâches restées en pending /
// processing au démarrage (mode local uniquement — appelé par EnsureHost).
func reconcileOrphanTasks(ctx context.Context, dbc *db.Client, l *slog.Logger) error {
	upd, err := dbc.Task.Update().
		Where(
			task.DeletedAtIsNil(),
			task.StatusIn(consts.TaskStatusPending, consts.TaskStatusProcessing),
		).
		SetStatus(consts.TaskStatusError).
		SetCompletedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return err
	}
	if upd > 0 {
		l.InfoContext(ctx, "local startup: orphan tasks marked error", "count", upd)
	}
	return nil
}

// resolveOwner 查找 host 的属主用户。
//
// Avec init-team : on préfère le compte non-enterprise (subaccount) — c'est
// le compte de connexion web (le user enterprise est exclu du password
// login). Sans init-team : premier admin, sinon premier user.
func resolveOwner(ctx context.Context, cfg *config.Config, dbc *db.Client) (uuid.UUID, error) {
	for i := 0; i < ownerRetryAttempts; i++ {
		if cfg.InitTeam.Email != "" {
			u, err := dbc.User.Query().
				Where(user.EmailEQ(cfg.InitTeam.Email), user.RoleNEQ(consts.UserRoleEnterprise)).
				Order(user.ByCreatedAt()).
				First(ctx)
			if err == nil {
				return u.ID, nil
			}
			u, err = dbc.User.Query().Where(user.EmailEQ(cfg.InitTeam.Email)).Order(user.ByCreatedAt()).First(ctx)
			if err == nil {
				return u.ID, nil
			}
		} else {
			// 无 init-team 配置：优先 admin，其次任意第一个用户。
			u, err := dbc.User.Query().Where(user.RoleEQ(consts.UserRoleAdmin)).Order(user.ByCreatedAt()).First(ctx)
			if err == nil {
				return u.ID, nil
			}
			u, err = dbc.User.Query().Order(user.ByCreatedAt()).First(ctx)
			if err == nil {
				return u.ID, nil
			}
		}
		select {
		case <-ctx.Done():
			return uuid.Nil, ctx.Err()
		case <-time.After(ownerRetryInterval):
		}
	}
	return uuid.Nil, fmt.Errorf("no user found after %ds", ownerRetryAttempts)
}

// upsertHost 插入或更新 host 行。
func upsertHost(ctx context.Context, dbc *db.Client, hostID string, owner uuid.UUID, info *taskflow.Host) error {
	exists, err := dbc.Host.Query().Where(host.ID(hostID)).Exist(ctx)
	if err != nil {
		return err
	}
	if exists {
		_, err := dbc.Host.UpdateOneID(hostID).
			SetArch(info.Arch).
			SetCores(int(info.Cores)).
			SetOs(info.OS).
			SetHostname(info.Hostname).
			SetMemory(int64(info.Memory)).
			SetDisk(int64(info.Disk)).
			SetVersion(info.Version).
			Save(ctx)
		return err
	}
	_, err = dbc.Host.Create().
		SetID(hostID).
		SetUserID(owner).
		SetArch(info.Arch).
		SetCores(int(info.Cores)).
		SetOs(info.OS).
		SetHostname(info.Hostname).
		SetMemory(int64(info.Memory)).
		SetDisk(int64(info.Disk)).
		SetVersion(info.Version).
		Save(ctx)
	return err
}
