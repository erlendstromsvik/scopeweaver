package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/intercept"
	"github.com/Autumn-27/artex/locale"
	actool "github.com/Autumn-27/norma/tool"
)

func (s *Server) domainToolRegistry() map[string]actool.CoreTool {
	registry := buildDomainReg(s.m.Assets())
	if registry == nil {
		registry = make(map[string]actool.CoreTool)
	}
	metadata := agent.NewToolSet(nil, "")
	for _, tool := range []actool.CoreTool{metadata.ReportFindingTool(), metadata.ListFindingsTool()} {
		registry[tool.Name()] = conversationFindingTool{CoreTool: tool, server: s}
	}
	return registry
}

// Task-bound tools already in a runner's base set retain precedence over these
// registry entries. Chat tools resolve their graph from server-owned context only.
type conversationFindingTool struct {
	actool.CoreTool
	server *Server
}

func (t conversationFindingTool) Call(ctx context.Context, input json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
	conversationID := intercept.ConvIDFromContext(ctx)
	if conversationID <= 0 {
		return actool.Errorf(locale.Text(locale.FromContext(ctx), "conversation not found")), nil
	}
	store, agentKey, err := t.server.m.pg.ConversationExploration(ctx, conversationID)
	if errors.Is(err, sql.ErrNoRows) {
		return actool.Errorf(locale.Text(locale.FromContext(ctx), "conversation not found")), nil
	}
	if err != nil {
		return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
	}
	tools := agent.NewToolSet(store, agentKey, locale.FromContext(ctx))
	if assets := t.server.m.Assets(); assets != nil {
		tools.SetAssetStore(assets, assets.Companies())
	}
	tools.SetFindingRecorder(t.server.evidenceStore())
	if t.Name() == "report_finding" {
		return tools.ReportFindingTool().Call(ctx, input, tc)
	}
	return tools.ListFindingsTool().Call(ctx, input, tc)
}
