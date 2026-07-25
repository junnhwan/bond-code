package collaboration

import (
	"context"
	"encoding/json"
	"fmt"

	execution "github.com/junnhwan/bond-code/internal/collaboration/backend"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
)

type collaborationTool struct {
	name, description string
	risk              tool.RiskLevel
	schema            any
	run               func(context.Context, json.RawMessage) (any, error)
}

func (t *collaborationTool) Name() string                        { return t.name }
func (t *collaborationTool) Description() string                 { return t.description }
func (t *collaborationTool) Schema() any                         { return t.schema }
func (t *collaborationTool) Risk(json.RawMessage) tool.RiskLevel { return t.risk }
func (t *collaborationTool) Execute(ctx context.Context, raw json.RawMessage) (*tool.Result, error) {
	v, err := t.run(ctx, raw)
	if err != nil {
		return tool.ErrorResult(t.name, "collaboration operation failed", err.Error()), nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return tool.Success(t.name, "collaboration operation completed", string(data)), nil
}
func Tools(store *Store, sessionID string) []tool.Tool {
	return ToolsWithPolicy(store, sessionID, safety.ModeDefault, false)
}

type BackendSelector interface {
	Resolve(context.Context, string) (execution.Backend, execution.Detection, error)
}

func ToolsWithPolicy(store *Store, sessionID string, parentMode safety.PermissionMode, bypassEnabled bool) []tool.Tool {
	return ToolsWithBackends(store, sessionID, parentMode, bypassEnabled, nil)
}

func ToolsWithBackends(store *Store, sessionID string, parentMode safety.PermissionMode, bypassEnabled bool, backends BackendSelector) []tool.Tool {
	return ToolsWithBackendsModeSource(store, sessionID, parentMode, bypassEnabled, nil, backends)
}

func ToolsWithBackendsModeSource(store *Store, sessionID string, parentMode safety.PermissionMode, bypassEnabled bool, modeSource *safety.PermissionModeSource, backends BackendSelector) []tool.Tool {
	owner := Principal{Kind: PrincipalOwner, ID: "session:" + sessionID}
	obj := func(p map[string]any, req ...string) any {
		schema := map[string]any{"type": "object", "properties": p}
		if len(req) > 0 {
			schema["required"] = req
		}
		return schema
	}
	str := map[string]any{"type": "string"}
	makeTool := func(name, desc string, risk tool.RiskLevel, schema any, run func(context.Context, json.RawMessage) (any, error)) tool.Tool {
		return &collaborationTool{name: name, description: desc, risk: risk, schema: schema, run: run}
	}
	create := makeTool("team_create", "Create a persistent agent team.", tool.RiskMedium, obj(map[string]any{"request_id": str, "name": str, "objective": str}, "request_id", "name"), func(_ context.Context, raw json.RawMessage) (any, error) {
		var in struct {
			RequestID string `json:"request_id"`
			Name      string `json:"name"`
			Objective string `json:"objective"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		return store.CreateTeam(CreateTeamInput{RequestID: in.RequestID, Name: in.Name, Objective: in.Objective, SessionID: sessionID, OwnerID: owner.ID})
	})
	list := makeTool("team_list", "List teams and members for this session.", tool.RiskLow, obj(map[string]any{}), func(context.Context, json.RawMessage) (any, error) {
		teams := store.ListTeams(sessionID)
		type view struct {
			Team    Team     `json:"team"`
			Members []Member `json:"members"`
		}
		out := make([]view, 0, len(teams))
		for _, t := range teams {
			out = append(out, view{t, store.ListMembers(t.ID)})
		}
		return out, nil
	})
	add := makeTool("team_add_member", "Add an agent member to a team.", tool.RiskMedium, obj(map[string]any{"request_id": str, "team_id": str, "name": str, "role": str, "profile": str, "backend": str, "permission_mode": str}, "request_id", "team_id", "name"), func(_ context.Context, raw json.RawMessage) (any, error) {
		var in AddMemberInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		if in.Backend == "" {
			in.Backend = string(execution.KindInProcess)
		}
		if backends != nil {
			if _, _, err := backends.Resolve(context.Background(), in.Backend); err != nil {
				return nil, err
			}
		}
		effectiveParent, effectiveBypass := parentMode, bypassEnabled
		if modeSource != nil {
			effectiveParent, effectiveBypass = modeSource.Mode(), modeSource.BypassEnabled()
		}
		mode, err := resolveChildPermissionMode(in.PermissionMode, effectiveParent, effectiveBypass)
		if err != nil {
			return nil, err
		}
		in.PermissionMode = string(mode)
		return store.AddMember(in)
	})
	assign := makeTool("team_assign", "Assign one canonical AgentTask generation to a team member.", tool.RiskMedium, obj(map[string]any{"request_id": str, "team_id": str, "member_id": str, "task_id": str, "generation": map[string]any{"type": "integer", "minimum": 1}}, "request_id", "team_id", "member_id", "task_id", "generation"), func(_ context.Context, raw json.RawMessage) (any, error) {
		var in AssignInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		in.Issuer = owner
		return store.Assign(in)
	})
	shutdown := makeTool("team_shutdown", "Request graceful shutdown of a team member.", tool.RiskMedium, obj(map[string]any{"request_id": str, "team_id": str, "member_id": str}, "request_id", "team_id", "member_id"), func(_ context.Context, raw json.RawMessage) (any, error) {
		var in ShutdownInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		in.Issuer = owner
		return store.RequestShutdown(in)
	})
	del := makeTool("team_delete", "Delete a team after all members have stopped.", tool.RiskMedium, obj(map[string]any{"team_id": str}, "team_id"), func(_ context.Context, raw json.RawMessage) (any, error) {
		var in struct {
			TeamID string `json:"team_id"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"deleted": true}, store.DeleteTeam(in.TeamID, owner)
	})
	sendFunc := func(broadcast bool) func(context.Context, json.RawMessage) (any, error) {
		return func(_ context.Context, raw json.RawMessage) (any, error) {
			var in struct {
				RequestID  string      `json:"request_id"`
				TeamID     string      `json:"team_id"`
				Recipients []string    `json:"recipients"`
				Body       string      `json:"body"`
				Kind       MessageKind `json:"kind"`
				TaskID     string      `json:"task_id"`
				Generation uint64      `json:"generation"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, err
			}
			if in.Body == "" {
				return nil, fmt.Errorf("body is required")
			}
			return store.Send(SendInput{RequestID: in.RequestID, TeamID: in.TeamID, Sender: owner, Recipients: in.Recipients, Broadcast: broadcast, Kind: in.Kind, Body: in.Body, TaskID: in.TaskID, Generation: in.Generation})
		}
	}
	send := makeTool("mailbox_send", "Send an immutable message to selected team members.", tool.RiskMedium, obj(map[string]any{"request_id": str, "team_id": str, "recipients": map[string]any{"type": "array", "items": str}, "body": str, "kind": str, "task_id": str, "generation": map[string]any{"type": "integer"}}, "request_id", "team_id", "recipients", "body"), sendFunc(false))
	broadcast := makeTool("mailbox_broadcast", "Broadcast a message to the current team membership snapshot.", tool.RiskMedium, obj(map[string]any{"request_id": str, "team_id": str, "body": str, "kind": str}, "request_id", "team_id", "body"), sendFunc(true))
	inbox := makeTool("mailbox_inbox", "Read a member mailbox using its stable inbox cursor.", tool.RiskLow, obj(map[string]any{"team_id": str, "member_id": str, "after": map[string]any{"type": "integer"}, "unread_only": map[string]any{"type": "boolean"}}, "team_id", "member_id"), func(_ context.Context, raw json.RawMessage) (any, error) {
		var in struct {
			TeamID     string `json:"team_id"`
			MemberID   string `json:"member_id"`
			After      uint64 `json:"after"`
			UnreadOnly bool   `json:"unread_only"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		return store.Inbox(in.TeamID, in.MemberID, in.After, in.UnreadOnly)
	})
	read := makeTool("mailbox_read", "Mark one mailbox delivery read.", tool.RiskLow, obj(map[string]any{"team_id": str, "member_id": str, "message_id": str}, "team_id", "member_id", "message_id"), func(_ context.Context, raw json.RawMessage) (any, error) {
		var in struct {
			TeamID    string `json:"team_id"`
			MemberID  string `json:"member_id"`
			MessageID string `json:"message_id"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"read": true}, store.MarkRead(in.TeamID, in.MemberID, in.MessageID)
	})
	return []tool.Tool{create, list, add, assign, shutdown, del, send, broadcast, inbox, read}
}

func resolveChildPermissionMode(requested string, parent safety.PermissionMode, bypassEnabled bool) (safety.PermissionMode, error) {
	if parent == "" {
		parent = safety.ModeDefault
	}
	if requested == "" {
		return parent, nil
	}
	mode, err := safety.ParsePermissionMode(requested)
	if err != nil {
		return "", err
	}
	if mode == safety.ModeBypass && !bypassEnabled {
		return "", fmt.Errorf("child bypass requires parent bypass capability")
	}
	parentCaps := safety.CapabilitiesForMode(parent, bypassEnabled)
	childCaps := safety.CapabilitiesForMode(mode, bypassEnabled)
	for capability := range childCaps {
		if !parentCaps.Has(capability) {
			return "", fmt.Errorf("child permission mode %q would widen parent mode %q", mode, parent)
		}
	}
	return mode, nil
}
