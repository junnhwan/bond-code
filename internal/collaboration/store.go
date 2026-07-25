package collaboration

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/junnhwan/bond-code/internal/fsx"
)

var (
	ErrTeamNotFound        = errors.New("team not found")
	ErrMemberNotFound      = errors.New("team member not found")
	ErrDuplicateTeamName   = errors.New("duplicate team name")
	ErrDuplicateMemberName = errors.New("duplicate member name")
	ErrUnauthorized        = errors.New("collaboration action unauthorized")
)

const schemaVersion = 1

type state struct {
	SchemaVersion  int                   `json:"schema_version"`
	Teams          map[string]Team       `json:"teams"`
	Members        map[string]Member     `json:"members"`
	Messages       map[string]Message    `json:"messages"`
	Deliveries     map[string][]Delivery `json:"deliveries"`
	InboxHighWater map[string]uint64     `json:"inbox_high_water"`
	Requests       map[string]string     `json:"requests"`
	Assignments    map[string]Assignment `json:"assignments"`
}
type envelope struct {
	Payload  json.RawMessage `json:"payload"`
	Checksum string          `json:"checksum"`
}
type Store struct {
	mu    sync.Mutex
	path  string
	state state
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, state: state{SchemaVersion: schemaVersion, Teams: map[string]Team{}, Members: map[string]Member{}, Messages: map[string]Message{}, Deliveries: map[string][]Delivery{}, InboxHighWater: map[string]uint64{}, Requests: map[string]string{}, Assignments: map[string]Assignment{}}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var env envelope
	if err = json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(env.Payload)
	if env.Checksum != hex.EncodeToString(sum[:]) {
		return nil, errors.New("collaboration store checksum mismatch")
	}
	if err = json.Unmarshal(env.Payload, &s.state); err != nil {
		return nil, err
	}
	if s.state.Assignments == nil {
		s.state.Assignments = map[string]Assignment{}
	}
	if s.state.SchemaVersion != schemaVersion {
		return nil, fmt.Errorf("unsupported collaboration schema %d", s.state.SchemaVersion)
	}
	return s, nil
}
func (s *Store) CreateTeam(in CreateTeamInput) (Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id := s.state.Requests["team:"+in.RequestID]; id != "" {
		return s.state.Teams[id], nil
	}
	for _, t := range s.state.Teams {
		if t.State != TeamDeleted && strings.EqualFold(t.Name, in.Name) {
			return Team{}, ErrDuplicateTeamName
		}
	}
	now := time.Now().UTC()
	t := Team{ID: newID("team"), Name: in.Name, SessionID: in.SessionID, OwnerID: in.OwnerID, Objective: in.Objective, State: TeamActive, CreatedAt: now, UpdatedAt: now}
	next := cloneState(s.state)
	next.Teams[t.ID] = t
	next.Requests["team:"+in.RequestID] = t.ID
	if err := s.persist(next); err != nil {
		return Team{}, err
	}
	s.state = next
	return t, nil
}
func (s *Store) AddMember(in AddMemberInput) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id := s.state.Requests["member:"+in.RequestID]; id != "" {
		return s.state.Members[id], nil
	}
	t, ok := s.state.Teams[in.TeamID]
	if !ok || t.State == TeamDeleted {
		return Member{}, ErrTeamNotFound
	}
	for _, id := range t.MemberIDs {
		if m := s.state.Members[id]; m.State != MemberStopped && strings.EqualFold(m.Name, in.Name) {
			return Member{}, ErrDuplicateMemberName
		}
	}
	now := time.Now().UTC()
	m := Member{ID: newID("member"), TeamID: t.ID, Name: in.Name, Role: in.Role, Profile: in.Profile, Backend: in.Backend, PermissionMode: in.PermissionMode, State: MemberActive, CreatedAt: now, UpdatedAt: now}
	next := cloneState(s.state)
	next.Members[m.ID] = m
	t.MemberIDs = append(t.MemberIDs, m.ID)
	t.UpdatedAt = now
	next.Teams[t.ID] = t
	next.Requests["member:"+in.RequestID] = m.ID
	if err := s.persist(next); err != nil {
		return Member{}, err
	}
	s.state = next
	return m, nil
}
func (s *Store) Send(in SendInput) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id := s.state.Requests["message:"+in.RequestID]; id != "" {
		return s.state.Messages[id], nil
	}
	t, ok := s.state.Teams[in.TeamID]
	if !ok || t.State == TeamDeleted {
		return Message{}, ErrTeamNotFound
	}
	if !authorizedSender(t, s.state.Members, in.Sender) {
		return Message{}, ErrUnauthorized
	}
	recipients := append([]string(nil), in.Recipients...)
	if in.Broadcast {
		recipients = append([]string(nil), t.MemberIDs...)
	}
	seen := map[string]bool{}
	for _, id := range recipients {
		m, ok := s.state.Members[id]
		if !ok || m.TeamID != t.ID || m.State == MemberStopped {
			return Message{}, ErrMemberNotFound
		}
		if seen[id] {
			return Message{}, fmt.Errorf("duplicate recipient %s", id)
		}
		seen[id] = true
	}
	if in.Kind == "" {
		if in.Broadcast {
			in.Kind = MessageBroadcast
		} else {
			in.Kind = MessageDirect
		}
	}
	now := time.Now().UTC()
	msg := Message{ID: newID("message"), RequestID: in.RequestID, TeamID: t.ID, Sender: in.Sender, Recipients: recipients, Kind: in.Kind, Body: in.Body, CorrelationID: in.CorrelationID, TaskID: in.TaskID, Generation: in.Generation, CreatedAt: now}
	next := cloneState(s.state)
	next.Messages[msg.ID] = msg
	next.Requests["message:"+in.RequestID] = msg.ID
	for _, id := range recipients {
		next.InboxHighWater[id]++
		next.Deliveries[id] = append(next.Deliveries[id], Delivery{MessageID: msg.ID, TeamID: t.ID, RecipientID: id, Sequence: next.InboxHighWater[id]})
	}
	if err := s.persist(next); err != nil {
		return Message{}, err
	}
	s.state = next
	return msg, nil
}
func (s *Store) Inbox(teamID, recipient string, after uint64, unreadOnly bool) ([]InboxItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.state.Members[recipient]
	if !ok || m.TeamID != teamID {
		return nil, ErrMemberNotFound
	}
	out := []InboxItem{}
	for _, d := range s.state.Deliveries[recipient] {
		if d.TeamID != teamID || d.Sequence <= after || (unreadOnly && d.ReadAt != nil) {
			continue
		}
		out = append(out, InboxItem{Sequence: d.Sequence, ReadAt: d.ReadAt, Message: s.state.Messages[d.MessageID]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out, nil
}
func (s *Store) MarkRead(teamID, recipient, messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.state)
	found := false
	for i, d := range next.Deliveries[recipient] {
		if d.TeamID == teamID && d.MessageID == messageID {
			if d.ReadAt == nil {
				now := time.Now().UTC()
				d.ReadAt = &now
				next.Deliveries[recipient][i] = d
			}
			found = true
			break
		}
	}
	if !found {
		return ErrMemberNotFound
	}
	if err := s.persist(next); err != nil {
		return err
	}
	s.state = next
	return nil
}
func authorizedSender(t Team, members map[string]Member, p Principal) bool {
	switch p.Kind {
	case PrincipalUser:
		return p.ID != ""
	case PrincipalOwner:
		return p.ID == t.OwnerID
	case PrincipalMember:
		m, ok := members[p.ID]
		return ok && m.TeamID == t.ID && m.State != MemberStopped
	default:
		return false
	}
}
func (s *Store) persist(st state) error {
	payload, err := json.Marshal(st)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(payload)
	data, err := json.Marshal(envelope{Payload: payload, Checksum: hex.EncodeToString(sum[:])})
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	return fsx.WriteFileAtomic(s.path, data, 0600)
}
func cloneState(in state) state {
	b, _ := json.Marshal(in)
	var out state
	_ = json.Unmarshal(b, &out)
	return out
}
func newID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}
