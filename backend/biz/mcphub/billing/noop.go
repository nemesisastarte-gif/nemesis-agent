package billing

import (
	"context"

	"github.com/teteekoue/NemesisCode/backend/biz/mcphub/repo"
)

type Noop struct{}

func NewNoop() *Noop {
	return &Noop{}
}

func (n *Noop) CanConsume(context.Context, *repo.ToolCallRecord) error {
	return nil
}

func (n *Noop) Consume(context.Context, *repo.ToolCallRecord) error {
	return nil
}
