// Package teammate implements the restricted external-terminal client. It has
// no model or tool execution capability and communicates only with the local
// parent runtime using task-bound credentials.
package teammate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ParentEndpoint, LaunchTokenFile     string
	TaskID, SessionID, TeamID, MemberID string
	Generation                          uint64
	OwnershipID                         string
	PollInterval                        time.Duration
	HTTPClient                          *http.Client
}
type handshakeRequest struct {
	TaskID      string `json:"task_id"`
	SessionID   string `json:"session_id"`
	TeamID      string `json:"team_id,omitempty"`
	MemberID    string `json:"member_id,omitempty"`
	Generation  uint64 `json:"generation"`
	OwnershipID string `json:"ownership_id"`
}
type handshakeResponse struct {
	SessionToken   string    `json:"session_token"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
	PollAfterMS    int       `json:"poll_after_ms"`
}
type pollResponse struct {
	State          string    `json:"state"`
	Output         []string  `json:"output"`
	Detail         string    `json:"detail"`
	Cursor         uint64    `json:"cursor"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}

func Run(ctx context.Context, cfg Config, stdin io.Reader, stdout io.Writer) error {
	base, err := validateConfig(cfg)
	if err != nil {
		return err
	}
	info, err := os.Stat(cfg.LaunchTokenFile)
	if err != nil {
		return fmt.Errorf("stat launch token: %w", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return errors.New("launch token file permissions must not grant group or other access")
	}
	raw, err := os.ReadFile(cfg.LaunchTokenFile)
	if err != nil {
		return fmt.Errorf("read launch token: %w", err)
	}
	launchToken := strings.TrimSpace(string(raw))
	if launchToken == "" {
		return errors.New("launch token is empty")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	request := handshakeRequest{cfg.TaskID, cfg.SessionID, cfg.TeamID, cfg.MemberID, cfg.Generation, cfg.OwnershipID}
	var handshake handshakeResponse
	if err := requestJSON(ctx, client, http.MethodPost, base+"/v1/handshake", launchToken, request, &handshake); err != nil {
		return fmt.Errorf("authenticate with parent: %w", err)
	}
	if handshake.SessionToken == "" || !handshake.LeaseExpiresAt.After(time.Now()) {
		return errors.New("parent returned an invalid task lease")
	}
	if err := os.Remove(cfg.LaunchTokenFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("consume launch token: %w", err)
	}
	inputErrors := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdin)
		scanner.Buffer(make([]byte, 4096), 64<<10)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if err := requestJSON(ctx, client, http.MethodPost, base+"/v1/input", handshake.SessionToken, map[string]string{"input": line}, &map[string]bool{}); err != nil {
				inputErrors <- fmt.Errorf("send task input: %w", err)
				return
			}
		}
		if err := scanner.Err(); err != nil {
			inputErrors <- err
		}
	}()
	interval := cfg.PollInterval
	if interval <= 0 {
		interval = time.Duration(handshake.PollAfterMS) * time.Millisecond
	}
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	var cursor uint64
	for {
		select {
		case err := <-inputErrors:
			return err
		default:
		}
		var poll pollResponse
		endpoint := base + "/v1/task?cursor=" + strconv.FormatUint(cursor, 10)
		if err := requestJSON(ctx, client, http.MethodGet, endpoint, handshake.SessionToken, nil, &poll); err != nil {
			return fmt.Errorf("poll parent task: %w", err)
		}
		if !poll.LeaseExpiresAt.After(time.Now()) {
			return errors.New("parent task lease expired")
		}
		for _, line := range poll.Output {
			if _, err := fmt.Fprintln(stdout, line); err != nil {
				return err
			}
		}
		cursor = poll.Cursor
		switch poll.State {
		case "completed":
			return nil
		case "failed", "canceled", "interrupted":
			if poll.Detail == "" {
				poll.Detail = poll.State
			}
			return fmt.Errorf("task %s: %s", poll.State, poll.Detail)
		case "running", "waiting", "queued":
		default:
			return fmt.Errorf("parent returned unknown task state %q", poll.State)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func validateConfig(cfg Config) (string, error) {
	if cfg.TaskID == "" || cfg.SessionID == "" || cfg.Generation == 0 || cfg.OwnershipID == "" {
		return "", errors.New("task identity, generation, and ownership are required")
	}
	parsed, err := url.Parse(cfg.ParentEndpoint)
	if err != nil {
		return "", fmt.Errorf("parse parent endpoint: %w", err)
	}
	if parsed.Scheme != "http" {
		return "", errors.New("parent endpoint must use local HTTP")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", errors.New("parent endpoint must be loopback-only")
	}
	if parsed.Port() == "" {
		return "", errors.New("parent endpoint port is required")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}
func requestJSON(ctx context.Context, client *http.Client, method, endpoint, token string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("parent returned HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out)
}
