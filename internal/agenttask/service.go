package agenttask

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type RunRequest struct {
	TaskID      string
	Generation  uint64
	SessionID   string
	Description string
	Prompt      string
	Profile     string
	TeamID      string
	MemberID    string
	Backend     string
	OwnershipID string
}
type RunResult struct {
	Summary     string
	ResultPath  string
	ErrorText   string
	LegacyAlias string
	Err         error
}
type InputJournal interface {
	RecordInput(context.Context, Task, string) error
}
type LaunchResolver interface {
	ResolveLaunch(context.Context, StartInput) (StartInput, error)
}
type Runner interface {
	Run(context.Context, RunRequest) RunResult
	Stop(context.Context, string, uint64) error
	SendInput(context.Context, string, uint64, string) error
}
type StartInput struct {
	IdempotencyKey string
	SessionID      string
	OwnerID        string
	TeamID         string
	MemberID       string
	Description    string
	Prompt         string
	Profile        string
	Backend        string
	OwnershipID    string
	Mode           Mode
}
type runningKey struct {
	id         string
	generation uint64
}
type Service struct {
	ledger   *Ledger
	runner   Runner
	mu       sync.Mutex
	cancels  map[runningKey]context.CancelFunc
	done     map[runningKey]chan struct{}
	journal  InputJournal
	resolver LaunchResolver
}

func NewService(ledger *Ledger, runner Runner) *Service {
	return &Service{ledger: ledger, runner: runner, cancels: map[runningKey]context.CancelFunc{}, done: map[runningKey]chan struct{}{}}
}
func (s *Service) Start(ctx context.Context, in StartInput) (Task, error) {
	if s == nil || s.ledger == nil || s.runner == nil {
		return Task{}, errors.New("agent task service dependencies are required")
	}
	s.mu.Lock()
	resolver := s.resolver
	s.mu.Unlock()
	if resolver != nil {
		var err error
		in, err = resolver.ResolveLaunch(ctx, in)
		if err != nil {
			return Task{}, err
		}
	}
	task, created, err := s.ledger.Create(CreateInput{IdempotencyKey: in.IdempotencyKey, Kind: KindAgent, Mode: in.Mode, SessionID: in.SessionID, OwnerID: in.OwnerID, TeamID: in.TeamID, MemberID: in.MemberID, Description: in.Description, Prompt: in.Prompt, Profile: in.Profile, Backend: in.Backend, BackendOwnershipID: in.OwnershipID})
	if err != nil {
		return Task{}, err
	}
	if created {
		s.launch(task)
	}
	if task.Mode == ModeForeground {
		return s.Wait(ctx, task.ID)
	}
	return task, nil
}
func (s *Service) Get(id string) (Task, bool)   { return s.ledger.Get(id) }
func (s *Service) List(sessionID string) []Task { return s.ledger.List(sessionID) }
func (s *Service) Wait(ctx context.Context, id string) (Task, error) {
	for {
		task, ok := s.ledger.Get(id)
		if !ok {
			return Task{}, ErrTaskNotFound
		}
		if task.State.terminal() || task.State == StateInterrupted {
			return task, nil
		}
		ch := s.doneChannel(runningKey{id: id, generation: task.Generation})
		select {
		case <-ctx.Done():
			return Task{}, ctx.Err()
		case <-ch:
		}
	}
}
func (s *Service) Stop(ctx context.Context, id string) (Task, error) {
	task, ok := s.ledger.Get(id)
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	if task.State.terminal() || task.State == StateInterrupted {
		return task, nil
	}
	key := runningKey{id: id, generation: task.Generation}
	s.mu.Lock()
	cancel := s.cancels[key]
	s.mu.Unlock()
	if err := s.runner.Stop(ctx, id, task.Generation); err != nil {
		return Task{}, err
	}
	if cancel != nil {
		cancel()
	}
	stopped, err := s.ledger.Transition(id, task.Generation, s.ledger.RuntimeLease(), StateCanceled, ResultRef{})
	if err != nil {
		return Task{}, err
	}
	s.signalDone(key)
	return stopped, nil
}
func (s *Service) Resume(ctx context.Context, id, idempotencyKey, prompt string) (Task, error) {
	task, err := s.ledger.Resume(id, idempotencyKey, prompt)
	if err != nil {
		return Task{}, err
	}
	s.launch(task)
	if task.Mode == ModeForeground {
		return s.Wait(ctx, id)
	}
	return task, nil
}
func (s *Service) SetLaunchResolver(resolver LaunchResolver) {
	s.mu.Lock()
	s.resolver = resolver
	s.mu.Unlock()
}
func (s *Service) SetInputJournal(journal InputJournal) {
	s.mu.Lock()
	s.journal = journal
	s.mu.Unlock()
}

// ContinueInput sends input to a live task or resumes a terminal/interrupted
// task with the same stable identity and a new generation.
func (s *Service) ContinueInput(ctx context.Context, id, input, idempotencyKey string) (Task, error) {
	task, ok := s.ledger.Get(id)
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	switch task.State {
	case StateRunning, StateWaiting:
		if err := s.SendInput(ctx, id, input); err != nil {
			return Task{}, err
		}
		return task, nil
	case StateCompleted, StateFailed, StateCanceled, StateInterrupted:
		return s.Resume(ctx, id, idempotencyKey, input)
	default:
		return Task{}, fmt.Errorf("%w: cannot continue input for %s", ErrInvalidTransition, task.State)
	}
}

func (s *Service) SendInput(ctx context.Context, id, input string) error {
	task, ok := s.ledger.Get(id)
	if !ok {
		return ErrTaskNotFound
	}
	if task.State != StateRunning && task.State != StateWaiting {
		return fmt.Errorf("%w: cannot send input to %s", ErrInvalidTransition, task.State)
	}
	s.mu.Lock()
	journal := s.journal
	s.mu.Unlock()
	if journal != nil {
		if err := journal.RecordInput(ctx, task, input); err != nil {
			return err
		}
	}
	return s.runner.SendInput(ctx, id, task.Generation, input)
}
func (s *Service) launch(task Task) {
	key := runningKey{id: task.ID, generation: task.Generation}
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[key] = cancel
	if s.done[key] == nil {
		s.done[key] = make(chan struct{})
	}
	s.mu.Unlock()
	go s.run(ctx, key, task)
}
func (s *Service) run(ctx context.Context, key runningKey, task Task) {
	defer func() { s.mu.Lock(); delete(s.cancels, key); s.mu.Unlock() }()
	running, err := s.ledger.Transition(task.ID, task.Generation, s.ledger.RuntimeLease(), StateRunning, ResultRef{})
	if err != nil {
		s.signalDone(key)
		return
	}
	result := s.runner.Run(ctx, RunRequest{TaskID: running.ID, Generation: running.Generation, SessionID: running.SessionID, Description: running.Description, Prompt: running.Prompt, Profile: running.Profile, TeamID: running.TeamID, MemberID: running.MemberID, Backend: running.Backend, OwnershipID: running.BackendOwnershipID})
	if result.LegacyAlias != "" {
		_ = s.ledger.AddLegacyAlias(task.ID, key.generation, s.ledger.RuntimeLease(), result.LegacyAlias)
	}
	state := StateCompleted
	ref := ResultRef{Path: result.ResultPath, Summary: result.Summary, Error: result.ErrorText}
	if result.Err != nil {
		state = StateFailed
		if ref.Error == "" {
			ref.Error = result.Err.Error()
		}
	}
	_, err = s.ledger.Transition(task.ID, key.generation, s.ledger.RuntimeLease(), state, ref)
	if err == nil {
		s.signalDone(key)
	}
}
func (s *Service) doneChannel(key runningKey) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.done[key]
	if ch == nil {
		ch = make(chan struct{})
		s.done[key] = ch
	}
	return ch
}
func (s *Service) signalDone(key runningKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch := s.done[key]; ch != nil {
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
}
