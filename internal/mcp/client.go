package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/junnhwan/bond-code/internal/observe"
)

type processClient struct {
	name        string
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	reader      *bufio.Reader
	stderr      *lockedBuffer
	mu          sync.Mutex
	nextID      int
	pending     map[int]chan callResponse
	callTimeout time.Duration
	done        chan struct{}
}

func startProcessClient(ctx context.Context, name string, command string, args []string) (*processClient, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &lockedBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	client := &processClient{
		name:    name,
		cmd:     cmd,
		stdin:   stdin,
		reader:  bufio.NewReader(stdout),
		stderr:  stderr,
		pending: map[int]chan callResponse{},
		done:    make(chan struct{}),
	}
	// readLoop runs for the whole session; a panic in it (malformed JSON-RPC
	// frame, nil deref) must not crash the process. Pending calls will simply
	// time out via callTimeout rather than hang forever.
	observe.SafeGo("mcp-readloop", func() { client.readLoop() })
	return client, nil
}

func (c *processClient) call(ctx context.Context, method string, params any, result any) error {
	timeout := c.getCallTimeout()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	b, err := json.Marshal(req)
	if err != nil {
		c.mu.Unlock()
		return err
	}
	responseCh := make(chan callResponse, 1)
	c.pending[id] = responseCh
	if _, err := c.stdin.Write(append(b, '\n')); err != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()

	var resp callResponse
	select {
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	case resp = <-responseCh:
	}
	if resp.err != nil {
		return resp.err
	}
	if resp.rpcError != nil {
		return fmt.Errorf("mcp %s error: %v", method, resp.rpcError)
	}
	if result != nil {
		return json.Unmarshal(resp.result, result)
	}
	return nil
}

// notify sends a JSON-RPC notification (no id, no response expected).
func (c *processClient) notify(ctx context.Context, method string, params any) error {
	_ = ctx
	req := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		req["params"] = params
	}
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.stdin.Write(append(b, '\n'))
	return err
}

func (c *processClient) getCallTimeout() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callTimeout
}

func (c *processClient) setCallTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.callTimeout = timeout
}

type callResponse struct {
	result   json.RawMessage
	rpcError any
	err      error
}

func (c *processClient) readLoop() {
	defer close(c.done)
	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			c.failPending(err)
			return
		}
		var resp struct {
			ID     int             `json:"id"`
			Error  any             `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(line, &resp); err != nil {
			c.failPending(err)
			return
		}
		c.deliverResponse(resp.ID, callResponse{result: resp.Result, rpcError: resp.Error})
	}
}

func (c *processClient) deliverResponse(id int, resp callResponse) {
	c.mu.Lock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.mu.Unlock()
	if ok {
		ch <- resp
	}
}

func (c *processClient) removePending(id int) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *processClient) failPending(err error) {
	c.mu.Lock()
	pending := c.pending
	c.pending = map[int]chan callResponse{}
	c.mu.Unlock()
	for _, ch := range pending {
		ch <- callResponse{err: err}
	}
}

func (c *processClient) close() error {
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}

// lockedBuffer guards a bytes.Buffer that os/exec's stderr pump writes from a
// background goroutine while stderrString reads it; bytes.Buffer is not safe
// for concurrent use, so both paths take the lock.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (c *processClient) stderrString() string {
	if c == nil || c.stderr == nil {
		return ""
	}
	text := strings.TrimSpace(c.stderr.String())
	if len(text) > 2000 {
		return text[:2000] + "\n[mcp stderr truncated]"
	}
	return text
}
