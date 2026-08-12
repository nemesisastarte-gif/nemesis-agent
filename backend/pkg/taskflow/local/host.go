package local

import (
	"context"

	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

type hostManager struct{ c *Client }

var _ taskflow.Hoster = (*hostManager)(nil)

// List 返回本机这台「宿主机」—— 机器 hôte = environnement de dev。
func (m *hostManager) List(ctx context.Context, userID string) (map[string]*taskflow.Host, error) {
	return map[string]*taskflow.Host{m.c.hostID: m.c.hostInfo()}, nil
}

func (m *hostManager) IsOnline(ctx context.Context, req *taskflow.IsOnlineReq[string]) (*taskflow.IsOnlineResp, error) {
	online := make(map[string]bool, len(req.IDs))
	for _, id := range req.IDs {
		online[id] = id == m.c.hostID
	}
	return &taskflow.IsOnlineResp{OnlineMap: online}, nil
}
