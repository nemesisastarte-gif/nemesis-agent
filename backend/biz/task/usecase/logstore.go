package usecase

import "github.com/teteekoue/NemesisCode/backend/consts"

func normalizeTaskLogStore(store *consts.LogStore) string {
	if store == nil || *store == "" {
		return string(consts.LogStoreLoki)
	}
	return string(*store)
}
