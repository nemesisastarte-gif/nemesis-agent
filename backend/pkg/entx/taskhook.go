package entx

import (
	"context"
	"fmt"

	"entgo.io/ent"

	"github.com/teteekoue/NemesisCode/backend/consts"
	"github.com/teteekoue/NemesisCode/backend/db"
	"github.com/teteekoue/NemesisCode/backend/db/hook"
	"github.com/teteekoue/NemesisCode/backend/db/task"
	"github.com/teteekoue/NemesisCode/backend/errcode"
)

// sqliteMode indique si la base est SQLite (mode local). Posé par
// pkg/store.NewEntDBV2. En SQLite, l'écriture est déjà sérialisée par le
// verrou d'écriture (une seule connexion) — pas besoin d'advisory lock.
var sqliteMode bool

// SetSQLiteMode configure le mode base de données pour les hooks entx.
func SetSQLiteMode(v bool) { sqliteMode = v }

func taskConcurrencyExceeded(count, limit int) bool {
	if limit <= 0 {
		limit = 1
	}
	return count >= limit
}

// TaskConcurrencyHook prevents a user from creating more than one active task
// (pending or processing). Uses pg_advisory_xact_lock to serialize concurrent
// create requests for the same user within a transaction.
//
// NOTE: This hook assumes it runs inside a transaction (tx.Task.Create).
// The advisory lock and count query must hit the same DB connection.
// If called outside a transaction with a read/write split driver,
// the count query may route to a replica, breaking the lock guarantee.
func TaskConcurrencyHook(next ent.Mutator) ent.Mutator {
	return hook.TaskFunc(func(ctx context.Context, m *db.TaskMutation) (db.Value, error) {
		if !m.Op().Is(db.OpCreate) {
			return next.Mutate(ctx, m)
		}
		userID, ok := m.UserID()
		if !ok {
			return next.Mutate(ctx, m)
		}
		// Advisory lock serializes concurrent creates for the same user.
		// Postgres only : pg_advisory_xact_lock / hashtext n'existent pas en
		// SQLite. En SQLite (mode local) l'écriture est déjà sérialisée par le
		// verrou d'écriture (une seule connexion d'écriture) — le count + create
		// dans la même transaction est donc atomique sans verrou supplémentaire.
		if !sqliteMode {
			_, err := m.Client().ExecContext(ctx,
				"SELECT pg_advisory_xact_lock(hashtext($1))", userID.String())
			if err != nil {
				return nil, fmt.Errorf("acquire task concurrency lock: %w", err)
			}
		}
		limit := 1
		if v, ok := TaskConcurrencyLimitFromContext(ctx); ok && v > 0 {
			limit = v
		}
		count, err := m.Client().Task.Query().
			Where(
				task.UserIDEQ(userID),
				task.StatusIn(consts.TaskStatusPending, consts.TaskStatusProcessing),
				task.HasProjectTasks(),
			).
			Count(ctx)
		if err != nil {
			return nil, fmt.Errorf("check task concurrency: %w", err)
		}
		if taskConcurrencyExceeded(count, limit) {
			return nil, errcode.ErrTaskConcurrencyLimit
		}
		return next.Mutate(ctx, m)
	})
}
