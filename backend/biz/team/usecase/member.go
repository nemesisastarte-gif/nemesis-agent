package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/teteekoue/NemesisCode/backend/consts"
	"github.com/teteekoue/NemesisCode/backend/db"
	"github.com/teteekoue/NemesisCode/backend/domain"
	"github.com/teteekoue/NemesisCode/backend/pkg/crypto"
	"github.com/teteekoue/NemesisCode/backend/pkg/cvt"
	"github.com/teteekoue/NemesisCode/backend/pkg/random"
)

// Les méthodes suivantes implémentent domain.MemberManager sur
// TeamGroupUserUsecase. Implémentation historique restaurée pour le mode
// serveur autonome (l'upstream l'avait déplacée dans le bridge desktop).

var _ domain.MemberManager = (*TeamGroupUserUsecase)(nil)

// AddUser 创建团队成员（发送重置密码邮件 si email).
func (u *TeamGroupUserUsecase) AddUser(ctx context.Context, teamUser *domain.TeamUser, req *domain.AddTeamUserReq) (*domain.AddTeamUserResp, error) {
	users, err := u.repo.CreateUsers(ctx, teamUser.GetTeamID(), req)
	if err != nil {
		return nil, err
	}
	u.notifyMembersAdded(ctx, teamUser, users)
	u.sendResetPasswordEmails(ctx, users)
	return &domain.AddTeamUserResp{
		Users: cvt.Iter(users, func(_ int, user *db.User) *domain.TeamUser {
			return cvt.From(user, &domain.TeamUser{})
		}),
	}, nil
}

// AddUserWithPassword 创建团队成员 avec mot de passe initial.
func (u *TeamGroupUserUsecase) AddUserWithPassword(ctx context.Context, teamUser *domain.TeamUser, req *domain.AddTeamUserReq) (*domain.AddTeamUserWithPasswordResp, error) {
	passwords := make(map[string]string, len(req.Emails))
	for _, email := range req.Emails {
		passwords[email] = random.String(16)
	}
	users, err := u.repo.CreateUsersWithPassword(ctx, teamUser.GetTeamID(), &domain.AddTeamUserWithPasswordReq{
		Emails:    req.Emails,
		GroupID:   req.GroupID,
		Passwords: passwords,
	})
	if err != nil {
		return nil, err
	}
	u.notifyMembersAdded(ctx, teamUser, users)
	return &domain.AddTeamUserWithPasswordResp{
		Users: cvt.Iter(users, func(_ int, user *db.User) *domain.TeamUser {
			return cvt.From(user, &domain.TeamUser{})
		}),
		Passwords: cvt.Filter(users, func(_ int, user *db.User) (*domain.TeamUserPassword, bool) {
			password, ok := passwords[user.Email]
			if !ok || user.Password == "" || crypto.VerifyPassword(user.Password, password) != nil {
				return nil, false
			}
			return &domain.TeamUserPassword{
				Email:    user.Email,
				Password: password,
			}, true
		}),
	}, nil
}

// AddAdmin 创建团队管理员.
func (u *TeamGroupUserUsecase) AddAdmin(ctx context.Context, teamUser *domain.TeamUser, req *domain.AddTeamAdminReq) (*domain.AddTeamAdminResp, error) {
	user, err := u.repo.CreateAdmin(ctx, teamUser.GetTeamID(), req)
	if err != nil {
		return nil, err
	}
	if u.teamHook != nil {
		if err := u.teamHook.OnMemberAdded(ctx, teamUser.GetTeamID(), user.ID); err != nil {
			u.logger.WarnContext(ctx, "teamHook.OnMemberAdded failed", "user_id", user.ID, "error", err)
		}
	}
	if user.Email != "" {
		u.sendResetPasswordEmails(ctx, []*db.User{user})
	}
	return &domain.AddTeamAdminResp{
		User: cvt.From(user, &domain.TeamUser{}),
	}, nil
}

// AutoCreateOIDCMember 自动创建 OIDC 成员 (SSO) : utilisateur + membre d'équipe.
func (u *TeamGroupUserUsecase) AutoCreateOIDCMember(ctx context.Context, teamID uuid.UUID, external *domain.OIDCExternalUser) (*domain.User, error) {
	name := external.Name
	if name == "" {
		name = external.Email
	}
	user, err := u.findOrCreateUser(ctx, name, external.Email)
	if err != nil {
		return nil, err
	}
	if _, err := u.repo.CreateTeamMember(ctx, teamID, user.ID, consts.TeamMemberRoleUser); err != nil {
		return nil, err
	}
	return cvt.From(user, &domain.User{}), nil
}

// findOrCreateUser retrouve l'utilisateur par email, sinon le crée.
func (u *TeamGroupUserUsecase) findOrCreateUser(ctx context.Context, name, email string) (*db.User, error) {
	if email != "" {
		existing, err := u.repo.GetUserByEmail(ctx, email)
		if err == nil && existing != nil {
			return existing, nil
		}
	}
	return u.repo.CreateUser(ctx, uuid.New(), name, email)
}

func (u *TeamGroupUserUsecase) notifyMembersAdded(ctx context.Context, teamUser *domain.TeamUser, users []*db.User) {
	if u.teamHook == nil {
		return
	}
	for _, user := range users {
		if err := u.teamHook.OnMemberAdded(ctx, teamUser.GetTeamID(), user.ID); err != nil {
			u.logger.WarnContext(ctx, "teamHook.OnMemberAdded failed", "user_id", user.ID, "error", err)
		}
	}
}

func (u *TeamGroupUserUsecase) sendResetPasswordEmails(ctx context.Context, users []*db.User) {
	for _, user := range users {
		if user.Email == "" {
			continue
		}
		token, err := u.generateResetPWDToken(ctx, user.ID)
		if err != nil {
			u.logger.ErrorContext(ctx, "generate reset password token failed", "error", err)
			continue
		}
		key := fmt.Sprintf("reset_password_token:%s", token)
		if err := u.redisClient.Set(ctx, key, user.ID.String(), time.Hour*24).Err(); err != nil {
			u.logger.ErrorContext(ctx, "set redis failed", "key", key, "token", token, "error", err)
			continue
		}
		u.logger.InfoContext(ctx, "set redis success", "key", key, "token", token)
		go u.sendResetPasswordEmail(ctx, user.Email, user.Name, token)
	}
}
