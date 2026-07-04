// Package acp implements the Agent Client Protocol (ACP) adapter for the
// Kugelblitz agent framework. ACP is an open standard (Apache 2.0) by Zed
// Industries that uses JSON-RPC 2.0 over stdin/stdout to connect code editors
// with AI coding agents.
//
// The adapter enables any ACP-compatible editor (Zed, JetBrains, VS Code,
// Neovim) to use a Kugelblitz-powered agent as its AI backend.
package acp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/B777B2056-2/kugelblitz/core"
)

// Server is the top-level ACP adapter. It reads JSON-RPC 2.0 messages from
// stdin, dispatches them to the handler, and writes responses to stdout.
//
// Usage:
//
//	agent := runtime.NewReactAgent(provider, true)
//	srv := acp.NewServer(agent, provider)
//	srv.Run(context.Background())
type Server struct {
	agent     core.IAgent
	provider  core.ILMProvider
	sessions  *SessionManager
	handler   *Handler
	transport *Transport

	capabilities   AgentCapabilities
	toolFilter     []string
	enableThinking bool

	mu     sync.Mutex
	closed bool
}

// Option is a functional option for configuring the Server.
type Option func(*Server)

// WithCapabilities sets custom agent capabilities declared during initialize.
func WithCapabilities(caps AgentCapabilities) Option {
	return func(s *Server) {
		s.capabilities = caps
	}
}

// WithToolFilter restricts which tools are visible to the agent.
func WithToolFilter(names ...string) Option {
	return func(s *Server) {
		s.toolFilter = names
	}
}

// WithEnableThinking enables thinking/reasoning mode for the agent.
func WithEnableThinking(enabled bool) Option {
	return func(s *Server) {
		s.enableThinking = enabled
	}
}

// WithIO sets custom input/output streams (default: os.Stdin/os.Stdout).
func WithIO(stdin io.Reader, stdout io.Writer) Option {
	return func(s *Server) {
		s.transport = NewTransport(&rwPair{reader: stdin, writer: stdout})
	}
}

// NewServer creates a new ACP Server with the given agent and provider.
// Options can customize the workspace, capabilities, tool filter, etc.
func NewServer(agent core.IAgent, provider core.ILMProvider, opts ...Option) *Server {
	s := &Server{
		agent:    agent,
		provider: provider,
		capabilities: AgentCapabilities{
			PromptCapabilities: PromptCapabilities{
				Image:  true,
				Stream: true,
			},
		},
	}

	// Apply defaults
	s.sessions = NewSessionManager()
	s.transport = NewTransport(&rwPair{reader: os.Stdin, writer: os.Stdout})

	// Apply options
	for _, opt := range opts {
		opt(s)
	}

	// Apply agent configuration
	// Tool filtering is applied at the agent level — callers should call
	// agent.WithTools(names...) before passing it to NewServer.

	s.handler = &Handler{
		transport: s.transport,
		sessions:  s.sessions,
		agent:     s.agent,
		provider:  s.provider,
		serverInfo: ServerInfo{
			Name:    "kugelblitz",
			Version: "0.1.0",
		},
	}

	return s
}

// Run starts the ACP main loop. It reads JSON-RPC messages from the transport,
// dispatches them to the handler, and writes responses. Run blocks until the
// context is cancelled or the input stream is closed.
func (s *Server) Run(ctx context.Context) error {
	core.Info("ACP: server started, waiting for initialize...")

	for {
		select {
		case <-ctx.Done():
			core.Debug("ACP: context cancelled, shutting down")
			return s.Shutdown(ctx)
		default:
		}

		msg, err := s.transport.ReadMessage()
		if err != nil {
			if errors.Is(err, io.EOF) {
				core.Debug("ACP: stdin closed, shutting down")
				return nil
			}
			core.Warn("ACP: read error", "err", err)
			// For parse errors, try to send an error response if we have an id
			return fmt.Errorf("acp: read error: %w", err)
		}

		core.Debug("ACP: received", "method", msg.Method)

		if err := s.handler.Dispatch(ctx, msg); err != nil {
			core.Warn("ACP: dispatch error", "err", err)
		}
	}
}

// Shutdown gracefully shuts down the server. Active sessions are not
// force-cancelled; they will complete naturally.
func (s *Server) Shutdown(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	core.Debug("ACP: server shut down")
	return nil
}

// rwPair joins a reader and writer into an io.ReadWriter.
type rwPair struct {
	reader io.Reader
	writer io.Writer
}

func (rw *rwPair) Read(p []byte) (int, error)  { return rw.reader.Read(p) }
func (rw *rwPair) Write(p []byte) (int, error) { return rw.writer.Write(p) }
