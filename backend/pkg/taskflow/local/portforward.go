package local

import (
	"context"
	"fmt"
	"time"

	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

type portForwarder struct{ c *Client }

var _ taskflow.PortForwarder = (*portForwarder)(nil)

// List 列出 VM 的端口转发记录。En mode local le process tourne déjà sur
// l'hôte : les « forwards » sont des enregistrements locaux.
func (m *portForwarder) List(ctx context.Context, req taskflow.ListPortforwadReq) (*taskflow.ListPortforwadResp, error) {
	rec := m.c.getVM(req.ID)
	if rec == nil {
		return nil, fmt.Errorf("environment not found: %s", req.ID)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make([]*taskflow.PortForwardInfo, 0, len(rec.ports))
	for _, p := range rec.ports {
		out = append(out, p)
	}
	return &taskflow.ListPortforwadResp{RequestId: req.RequestId, Ports: out}, nil
}

func (m *portForwarder) Create(ctx context.Context, req taskflow.CreatePortForward) (*taskflow.PortForwardInfo, error) {
	rec := m.c.getVM(req.ID)
	if rec == nil {
		return nil, fmt.Errorf("environment not found: %s", req.ID)
	}
	forwardID := fmt.Sprintf("local-%d", req.LocalPort)
	info := &taskflow.PortForwardInfo{
		Port:      req.LocalPort,
		Status:    "running",
		ForwardID: &forwardID,
		AccessURL: fmt.Sprintf("http://127.0.0.1:%d", req.LocalPort),
		CreatedAt: time.Now().Unix(),
		Success:   true,
	}
	rec.mu.Lock()
	rec.ports[forwardID] = info
	rec.mu.Unlock()
	return info, nil
}

func (m *portForwarder) Close(ctx context.Context, req taskflow.ClosePortForward) error {
	rec := m.c.getVM(req.ID)
	if rec == nil {
		return nil
	}
	rec.mu.Lock()
	delete(rec.ports, req.ForwardID)
	rec.mu.Unlock()
	return nil
}

func (m *portForwarder) Update(ctx context.Context, req taskflow.UpdatePortForward) (*taskflow.PortForwardInfo, error) {
	rec := m.c.getVM(req.ID)
	if rec == nil {
		return nil, fmt.Errorf("environment not found: %s", req.ID)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	p, ok := rec.ports[req.ForwardID]
	if !ok {
		return nil, fmt.Errorf("forward not found: %s", req.ForwardID)
	}
	p.WhitelistIPs = req.WhitelistIPs
	return p, nil
}
