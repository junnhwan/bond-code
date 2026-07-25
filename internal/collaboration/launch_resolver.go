package collaboration

import (
	"context"
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/agenttask"
	execution "github.com/junnhwan/bond-code/internal/collaboration/backend"
)

// LaunchResolver binds a durable AgentTask to its authoritative team-member
// execution settings before the task is persisted.
type LaunchResolver struct {
	store    *Store
	backends BackendSelector
}

func NewLaunchResolver(store *Store, backends BackendSelector) *LaunchResolver {
	return &LaunchResolver{store: store, backends: backends}
}

func (r *LaunchResolver) ResolveLaunch(ctx context.Context, in agenttask.StartInput) (agenttask.StartInput, error) {
	if r == nil || r.store == nil || r.backends == nil {
		return in, fmt.Errorf("collaboration launch resolver dependencies are required")
	}
	if (in.TeamID == "") != (in.MemberID == "") {
		return in, fmt.Errorf("team ID and member ID must be provided together")
	}
	if in.TeamID != "" {
		team, ok := r.store.GetTeam(in.TeamID)
		if !ok || team.State != TeamActive {
			return in, fmt.Errorf("active team not found")
		}
		if team.SessionID != in.SessionID || team.OwnerID != in.OwnerID {
			return in, fmt.Errorf("team does not belong to the task session owner")
		}
		member, ok := r.store.GetMember(in.MemberID)
		if !ok || member.TeamID != team.ID || member.State != MemberActive {
			return in, fmt.Errorf("active team member not found")
		}
		in.Backend = member.Backend
		if strings.TrimSpace(in.Profile) == "" {
			in.Profile = member.Profile
		}
	}
	if strings.TrimSpace(in.Backend) == "" {
		in.Backend = string(execution.KindInProcess)
	}
	if _, _, err := r.backends.Resolve(ctx, in.Backend); err != nil {
		return in, err
	}
	if strings.TrimSpace(in.OwnershipID) == "" {
		in.OwnershipID = newID("backend-owner")
	}
	return in, nil
}
