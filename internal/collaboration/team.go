package collaboration

import "time"

var (
	ErrMemberBusy          = errText("team member already has an active primary task")
	ErrStaleTaskGeneration = errText("stale task generation")
	ErrLiveMembers         = errText("team has live members")
)

type errText string

func (e errText) Error() string { return string(e) }

func (s *Store) GetTeam(id string) (Team, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.state.Teams[id]
	t.MemberIDs = append([]string(nil), t.MemberIDs...)
	return t, ok
}
func (s *Store) GetMember(id string) (Member, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	member, ok := s.state.Members[id]
	return member, ok
}
func (s *Store) ListTeams(sessionID string) []Team {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Team{}
	for _, t := range s.state.Teams {
		if sessionID == "" || t.SessionID == sessionID {
			t.MemberIDs = append([]string(nil), t.MemberIDs...)
			out = append(out, t)
		}
	}
	return out
}
func (s *Store) ListMembers(teamID string) []Member {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.state.Teams[teamID]
	if !ok {
		return nil
	}
	out := make([]Member, 0, len(t.MemberIDs))
	for _, id := range t.MemberIDs {
		out = append(out, s.state.Members[id])
	}
	return out
}

func (s *Store) Assign(in AssignInput) (Assignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id := s.state.Requests["assignment:"+in.RequestID]; id != "" {
		return s.state.Assignments[id], nil
	}
	t, ok := s.state.Teams[in.TeamID]
	if !ok || t.State != TeamActive {
		return Assignment{}, ErrTeamNotFound
	}
	if !authorizedSender(t, s.state.Members, in.Issuer) || (in.Issuer.Kind != PrincipalOwner && in.Issuer.Kind != PrincipalUser) {
		return Assignment{}, ErrUnauthorized
	}
	m, ok := s.state.Members[in.MemberID]
	if !ok || m.TeamID != t.ID {
		return Assignment{}, ErrMemberNotFound
	}
	if m.PrimaryTaskID != "" {
		return Assignment{}, ErrMemberBusy
	}
	now := time.Now().UTC()
	a := Assignment{ID: newID("assignment"), TeamID: t.ID, MemberID: m.ID, TaskID: in.TaskID, Generation: in.Generation, State: AssignmentActive, CreatedAt: now, UpdatedAt: now}
	next := cloneState(s.state)
	next.Assignments[a.ID] = a
	next.Requests["assignment:"+in.RequestID] = a.ID
	m.PrimaryTaskID = in.TaskID
	m.UpdatedAt = now
	next.Members[m.ID] = m
	if err := s.persist(next); err != nil {
		return Assignment{}, err
	}
	s.state = next
	return a, nil
}
func (s *Store) CompleteAssignment(teamID, memberID, taskID string, generation uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.state.Members[memberID]
	if !ok || m.TeamID != teamID {
		return ErrMemberNotFound
	}
	var id string
	var a Assignment
	for candidate, v := range s.state.Assignments {
		if v.TeamID == teamID && v.MemberID == memberID && v.TaskID == taskID && v.State == AssignmentActive {
			id = candidate
			a = v
			break
		}
	}
	if id == "" {
		return ErrMemberNotFound
	}
	if a.Generation != generation {
		return ErrStaleTaskGeneration
	}
	next := cloneState(s.state)
	now := time.Now().UTC()
	a.State = AssignmentCompleted
	a.UpdatedAt = now
	next.Assignments[id] = a
	m.PrimaryTaskID = ""
	m.UpdatedAt = now
	next.Members[m.ID] = m
	if err := s.persist(next); err != nil {
		return err
	}
	s.state = next
	return nil
}
func (s *Store) RequestShutdown(in ShutdownInput) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id := s.state.Requests["shutdown:"+in.RequestID]; id != "" {
		return s.state.Members[id], nil
	}
	t, ok := s.state.Teams[in.TeamID]
	if !ok {
		return Member{}, ErrTeamNotFound
	}
	if !authorizedSender(t, s.state.Members, in.Issuer) || (in.Issuer.Kind != PrincipalOwner && in.Issuer.Kind != PrincipalUser) {
		return Member{}, ErrUnauthorized
	}
	m, ok := s.state.Members[in.MemberID]
	if !ok || m.TeamID != t.ID {
		return Member{}, ErrMemberNotFound
	}
	next := cloneState(s.state)
	now := time.Now().UTC()
	m.State = MemberStopping
	m.UpdatedAt = now
	next.Members[m.ID] = m
	next.Requests["shutdown:"+in.RequestID] = m.ID
	if err := s.persist(next); err != nil {
		return Member{}, err
	}
	s.state = next
	return m, nil
}
func (s *Store) AcknowledgeShutdown(teamID, memberID string, issuer Principal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.state.Members[memberID]
	if !ok || m.TeamID != teamID {
		return ErrMemberNotFound
	}
	if issuer.Kind != PrincipalMember || issuer.ID != memberID {
		return ErrUnauthorized
	}
	next := cloneState(s.state)
	now := time.Now().UTC()
	m.State = MemberStopped
	m.PrimaryTaskID = ""
	m.UpdatedAt = now
	next.Members[m.ID] = m
	if err := s.persist(next); err != nil {
		return err
	}
	s.state = next
	return nil
}
func (s *Store) DeleteTeam(teamID string, issuer Principal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.state.Teams[teamID]
	if !ok {
		return ErrTeamNotFound
	}
	if !authorizedSender(t, s.state.Members, issuer) || (issuer.Kind != PrincipalOwner && issuer.Kind != PrincipalUser) {
		return ErrUnauthorized
	}
	for _, id := range t.MemberIDs {
		if s.state.Members[id].State != MemberStopped {
			return ErrLiveMembers
		}
	}
	next := cloneState(s.state)
	now := time.Now().UTC()
	t.State = TeamDeleted
	t.UpdatedAt = now
	next.Teams[t.ID] = t
	if err := s.persist(next); err != nil {
		return err
	}
	s.state = next
	return nil
}
