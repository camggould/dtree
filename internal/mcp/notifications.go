package mcp

import (
	"fmt"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
)

// registerAuditHook subscribes the server to audit events so it can emit MCP
// resource-change notifications. Stored unregister fn is invoked on Stop().
func (s *Server) registerAuditHook() {
	s.unregisterHook = audit.RegisterHook(s.onAuditEvent)
}

func (s *Server) onAuditEvent(ev core.Event) {
	switch ev.Kind {
	case core.KindDecision:
		// Single resource updated. URI mirrors resources.go template.
		s.mcp.SendNotificationToAllClients("notifications/resources/updated",
			map[string]any{
				"uri": fmt.Sprintf("dtree://trees/%s/decisions/%s", ev.Tree, ev.ID),
			},
		)
		// If the action affects existence (create/delete), the parent list also changed.
		switch ev.Action {
		case core.ActionCreate, core.ActionDelete,
			core.ActionExternalCreate, core.ActionExternalDelete:
			s.mcp.SendNotificationToAllClients("notifications/resources/list_changed", nil)
		}

	case core.KindTree:
		s.mcp.SendNotificationToAllClients("notifications/resources/list_changed", nil)
		if ev.ID != "" {
			s.mcp.SendNotificationToAllClients("notifications/resources/updated",
				map[string]any{
					"uri": fmt.Sprintf("dtree://trees/%s", ev.ID),
				},
			)
		}

	case core.KindActor:
		s.mcp.SendNotificationToAllClients("notifications/resources/updated",
			map[string]any{"uri": "dtree://actors"},
		)
		s.mcp.SendNotificationToAllClients("notifications/resources/list_changed", nil)
	}
}

// Stop unregisters the audit hook. Safe to call multiple times.
func (s *Server) Stop() {
	if s.unregisterHook != nil {
		s.unregisterHook()
		s.unregisterHook = nil
	}
}
