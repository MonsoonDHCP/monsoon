package mcp

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	ssePath        = "/sse"
	messagePath    = "/message"
	sseRetryMillis = 5000
)

type Server struct {
	toolset   *Toolset
	mux       *http.ServeMux
	sessionMu sync.RWMutex
	sessions  map[string]*session
}

type session struct {
	id              string
	messages        chan rpcResponse
	protocolVersion string
	initialized     bool
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
	ClientInfo      map[string]any `json:"clientInfo,omitempty"`
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

func NewServer(deps HandlerDeps) *Server {
	s := &Server{
		toolset:  NewToolset(deps),
		mux:      http.NewServeMux(),
		sessions: map[string]*session{},
	}
	s.mux.HandleFunc("GET "+ssePath, s.handleSSE)
	s.mux.HandleFunc("POST "+messagePath, s.handleMessage)
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	if !allowOrigin(r) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	id, err := randomSessionID()
	if err != nil {
		http.Error(w, "session init failed", http.StatusInternalServerError)
		return
	}
	sess := &session{
		id:       id,
		messages: make(chan rpcResponse, 16),
	}
	s.setSession(sess)
	defer s.deleteSession(id)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("MCP-Protocol-Version", defaultProtocolVersion)

	writer := bufio.NewWriter(w)
	if err := writeSSE(writer, "endpoint", messagePath+"?session_id="+id); err != nil {
		return
	}
	if _, err := writer.WriteString("retry: 5000\n\n"); err != nil {
		return
	}
	if err := writer.Flush(); err != nil {
		return
	}
	flusher.Flush()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if _, err := writer.WriteString(": keepalive\n\n"); err != nil {
				return
			}
			if err := writer.Flush(); err != nil {
				return
			}
			flusher.Flush()
		case msg, ok := <-sess.messages:
			if !ok {
				return
			}
			payload, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			if err := writeSSE(writer, "message", string(payload)); err != nil {
				return
			}
			if err := writer.Flush(); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	if !allowOrigin(r) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	defer r.Body.Close()

	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	var req rpcRequest
	if err := dec.Decode(&req); err != nil {
		s.writeInlineResponse(w, rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32700, Message: "parse error", Data: err.Error()},
		})
		return
	}
	if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" {
		s.writeInlineResponse(w, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32600, Message: "invalid request"},
		})
		return
	}

	resp, respond := s.dispatch(r.Context(), r.URL.Query().Get("session_id"), req)
	if !respond {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if sessionID != "" {
		if sess, ok := s.getSession(sessionID); ok {
			select {
			case sess.messages <- resp:
				w.Header().Set("MCP-Protocol-Version", sess.protocolVersionOrDefault())
				w.WriteHeader(http.StatusAccepted)
				return
			default:
				s.writeInlineResponse(w, rpcResponse{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error:   &rpcError{Code: -32001, Message: "session queue full"},
				})
				return
			}
		}
	}

	s.writeInlineResponse(w, resp)
}

func (s *Server) dispatch(ctx context.Context, sessionID string, req rpcRequest) (rpcResponse, bool) {
	resp := rpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}
	switch req.Method {
	case "initialize":
		var params initializeParams
		if err := decodeParams(req.Params, &params); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "invalid params", Data: err.Error()}
			return resp, true
		}
		version := negotiateProtocolVersion(params.ProtocolVersion)
		if sessionID != "" {
			if sess, ok := s.getSession(sessionID); ok {
				sess.protocolVersion = version
			}
		}
		resp.Result = map[string]any{
			"protocolVersion": version,
			"capabilities": map[string]any{
				"tools": map[string]any{
					"listChanged": false,
				},
			},
			"serverInfo": map[string]any{
				"name":    "monsoon",
				"version": s.toolset.deps.Version,
			},
		}
		return resp, true
	case "notifications/initialized":
		if sessionID != "" {
			if sess, ok := s.getSession(sessionID); ok {
				sess.initialized = true
			}
		}
		return rpcResponse{}, false
	case "ping":
		resp.Result = map[string]any{}
		return resp, true
	case "tools/list":
		resp.Result = map[string]any{
			"tools": s.toolset.List(),
		}
		return resp, true
	case "tools/call":
		var params toolCallParams
		if err := decodeParams(req.Params, &params); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "invalid params", Data: err.Error()}
			return resp, true
		}
		result, err := s.toolset.Call(ctx, params.Name, params.Arguments)
		if err != nil {
			var paramErr paramError
			if errors.As(err, &paramErr) {
				resp.Error = &rpcError{Code: -32602, Message: "invalid params", Data: err.Error()}
				return resp, true
			}
			resp.Error = &rpcError{Code: -32000, Message: "tool execution failed", Data: err.Error()}
			return resp, true
		}
		resp.Result = result
		return resp, true
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found"}
		return resp, true
	}
}

func (s *Server) setSession(sess *session) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.sessions[sess.id] = sess
}

func (s *Server) getSession(id string) (*session, bool) {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

func (s *Server) deleteSession(id string) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if _, ok := s.sessions[id]; ok {
		delete(s.sessions, id)
	}
}

func (s *Server) writeInlineResponse(w http.ResponseWriter, resp rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("MCP-Protocol-Version", defaultProtocolVersion)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *session) protocolVersionOrDefault() string {
	if strings.TrimSpace(s.protocolVersion) == "" {
		return defaultProtocolVersion
	}
	return s.protocolVersion
}

func decodeParams(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	return dec.Decode(out)
}

func negotiateProtocolVersion(requested string) string {
	switch strings.TrimSpace(requested) {
	case "", defaultProtocolVersion, "2024-11-05":
		return defaultProtocolVersion
	default:
		return defaultProtocolVersion
	}
}

func randomSessionID() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func writeSSE(writer *bufio.Writer, event string, data string) error {
	if _, err := writer.WriteString("event: " + event + "\n"); err != nil {
		return err
	}
	for _, line := range strings.Split(data, "\n") {
		if _, err := writer.WriteString("data: " + line + "\n"); err != nil {
			return err
		}
	}
	_, err := writer.WriteString("\n")
	return err
}

func allowOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	originHost := normalizeHost(parsed.Hostname())
	requestHost := normalizeHost(requestHostName(r.Host))
	if originHost == requestHost {
		return true
	}
	switch originHost {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func requestHostName(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return host
}

func normalizeHost(host string) string {
	return strings.ToLower(strings.TrimSpace(strings.Trim(host, "[]")))
}

var _ = context.Background
