package repo

import (
	"context"

	"github.com/google/uuid"

	"github.com/teteekoue/NemesisCode/backend/consts"
	"github.com/teteekoue/NemesisCode/backend/db"
	"github.com/teteekoue/NemesisCode/backend/db/teammember"
	"github.com/teteekoue/NemesisCode/backend/db/user"
	"github.com/teteekoue/NemesisCode/backend/domain"
	"github.com/teteekoue/NemesisCode/backend/errcode"
	"github.com/teteekoue/NemesisCode/backend/pkg/crypto"
)

// CreateUsers 创建团队成员（email → 用户 + 团队关系）。
// Implémentation historique restaurée pour le mode serveur autonome
// (l'upstream l'avait déplacée dans le bridge desktop).
func (r *TeamGroupUserRepo) CreateUsers(ctx context.Context, teamID uuid.UUID, req *domain.AddTeamUserReq) ([]*db.User, error) {
	if err := r.checkTeamMemberLimit(ctx, teamID, req.Emails); err != nil {
		return nil, err
	}

	var users []*db.User

	for _, emailAddr := range req.Emails {
		existingUser, err := r.db.User.Query().Where(user.EmailEQ(emailAddr)).First(ctx)
		if err == nil && existingUser != nil {
			_, err := r.db.TeamMember.Query().Where(
				teammember.TeamIDEQ(teamID),
				teammember.UserIDEQ(existingUser.ID),
			).First(ctx)
			if err == nil {
				continue // déjà membre
			}
			_, err = r.db.TeamMember.Create().
				SetTeamID(teamID).
				SetUserID(existingUser.ID).
				SetRole(consts.TeamMemberRoleUser).
				Save(ctx)
			if err != nil {
				r.logger.ErrorContext(ctx, "add user to team failed", "error", err)
				continue
			}
			users = append(users, existingUser)
			continue
		}

		newUser, err := r.db.User.Create().
			SetName(emailAddr).
			SetEmail(emailAddr).
			SetStatus(consts.UserStatusActive).
			SetPassword("").
			SetRole(consts.UserRoleSubAccount).
			Save(ctx)
		if err != nil {
			r.logger.ErrorContext(ctx, "create user failed", "error", err, "email", emailAddr)
			continue
		}

		_, err = r.db.TeamMember.Create().
			SetTeamID(teamID).
			SetUserID(newUser.ID).
			SetRole(consts.TeamMemberRoleUser).
			Save(ctx)
		if err != nil {
			r.logger.ErrorContext(ctx, "add user to team failed", "error", err)
			continue
		}
		users = append(users, newUser)
	}
	return users, nil
}

// CreateUsersWithPassword 创建团队成员 avec mot de passe initial.
func (r *TeamGroupUserRepo) CreateUsersWithPassword(ctx context.Context, teamID uuid.UUID, req *domain.AddTeamUserWithPasswordReq) ([]*db.User, error) {
	if err := r.checkTeamMemberLimit(ctx, teamID, req.Emails); err != nil {
		return nil, err
	}

	var users []*db.User

	for _, emailAddr := range req.Emails {
		existingUser, err := r.db.User.Query().Where(user.EmailEQ(emailAddr)).First(ctx)
		if err == nil && existingUser != nil {
			_, err := r.db.TeamMember.Query().Where(
				teammember.TeamIDEQ(teamID),
				teammember.UserIDEQ(existingUser.ID),
			).First(ctx)
			if err == nil {
				continue
			}
			if existingUser.Password == "" {
				hashedPassword, err := crypto.HashPassword(req.Passwords[emailAddr])
				if err != nil {
					r.logger.ErrorContext(ctx, "hash password failed", "error", err, "email", emailAddr)
					continue
				}
				existingUser, err = r.db.User.UpdateOneID(existingUser.ID).
					SetPassword(hashedPassword).
					Save(ctx)
				if err != nil {
					r.logger.ErrorContext(ctx, "set user password failed", "error", err, "email", emailAddr)
					continue
				}
			}
			_, err = r.db.TeamMember.Create().
				SetID(uuid.New()).
				SetTeamID(teamID).
				SetUserID(existingUser.ID).
				SetRole(consts.TeamMemberRoleUser).
				Save(ctx)
			if err != nil {
				r.logger.ErrorContext(ctx, "add user to team failed", "error", err)
				continue
			}
			users = append(users, existingUser)
			continue
		}

		hashedPassword, err := crypto.HashPassword(req.Passwords[emailAddr])
		if err != nil {
			r.logger.ErrorContext(ctx, "hash password failed", "error", err, "email", emailAddr)
			continue
		}
		newUser, err := r.db.User.Create().
			SetID(uuid.New()).
			SetName(emailAddr).
			SetEmail(emailAddr).
			SetStatus(consts.UserStatusActive).
			SetPassword(hashedPassword).
			SetRole(consts.UserRoleSubAccount).
			Save(ctx)
		if err != nil {
			r.logger.ErrorContext(ctx, "create user failed", "error", err, "email", emailAddr)
			continue
		}

		_, err = r.db.TeamMember.Create().
			SetID(uuid.New()).
			SetTeamID(teamID).
			SetUserID(newUser.ID).
			SetRole(consts.TeamMemberRoleUser).
			Save(ctx)
		if err != nil {
			r.logger.ErrorContext(ctx, "add user to team failed", "error", err)
			continue
		}
		users = append(users, newUser)
	}
	return users, nil
}

// CreateAdmin 创建团队管理员.
func (r *TeamGroupUserRepo) CreateAdmin(ctx context.Context, teamID uuid.UUID, req *domain.AddTeamAdminReq) (*db.User, error) {
	if err := r.checkTeamMemberLimit(ctx, teamID, []string{req.Email}); err != nil {
		return nil, err
	}

	existingUser, err := r.db.User.Query().Where(user.EmailEQ(req.Email)).First(ctx)
	if err == nil && existingUser != nil {
		_, err := r.db.TeamMember.Query().Where(
			teammember.TeamIDEQ(teamID),
			teammember.UserIDEQ(existingUser.ID),
		).First(ctx)
		if err == nil {
			return nil, errcode.ErrUserAlreadyExists
		}
		_, err = r.db.TeamMember.Create().
			SetTeamID(teamID).
			SetUserID(existingUser.ID).
			SetRole(consts.TeamMemberRoleAdmin).
			Save(ctx)
		if err != nil {
			return nil, err
		}
		return existingUser, nil
	}

	newUser, err := r.db.User.Create().
		SetName(req.Name).
		SetEmail(req.Email).
		SetPassword("").
		SetRole(consts.UserRoleIndividual).
		Save(ctx)
	if err != nil {
		return nil, err
	}

	_, err = r.db.TeamMember.Create().
		SetTeamID(teamID).
		SetUserID(newUser.ID).
		SetRole(consts.TeamMemberRoleAdmin).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return newUser, nil
}

// checkTeamMemberLimit 检查团队成员数量上限.
func (r *TeamGroupUserRepo) checkTeamMemberLimit(ctx context.Context, teamID uuid.UUID, emails []string) error {
	team, err := r.db.Team.Get(ctx, teamID)
	if err != nil {
		return err
	}
	if team.MemberLimit <= 0 {
		return nil
	}

	existingCount, err := r.db.TeamMember.Query().
		Where(teammember.TeamIDEQ(teamID)).
		Count(ctx)
	if err != nil {
		return err
	}

	addCount, err := r.countNewTeamMembers(ctx, teamID, emails)
	if err != nil {
		return err
	}
	if existingCount+addCount > team.MemberLimit {
		return errcode.ErrTeamMemberLimitExceeded
	}
	return nil
}

// GetUserByEmail 查找用户 par email.
func (r *TeamGroupUserRepo) GetUserByEmail(ctx context.Context, email string) (*db.User, error) {
	return r.db.User.Query().Where(user.EmailEQ(email)).First(ctx)
}

// CreateUser 创建用户 (email optionnel).
func (r *TeamGroupUserRepo) CreateUser(ctx context.Context, id uuid.UUID, name, email string) (*db.User, error) {
	builder := r.db.User.Create().
		SetID(id).
		SetName(name).
		SetStatus(consts.UserStatusActive).
		SetRole(consts.UserRoleIndividual)
	if email != "" {
		builder = builder.SetEmail(email)
	}
	return builder.Save(ctx)
}

// CreateTeamMember 创建团队成员关系.
func (r *TeamGroupUserRepo) CreateTeamMember(ctx context.Context, teamID, userID uuid.UUID, role consts.TeamMemberRole) (*db.TeamMember, error) {
	return r.db.TeamMember.Create().
		SetID(uuid.New()).
		SetTeamID(teamID).
		SetUserID(userID).
		SetRole(role).
		Save(ctx)
}
