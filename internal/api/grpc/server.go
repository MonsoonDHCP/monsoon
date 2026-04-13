package grpc

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	restapi "github.com/monsoondhcp/monsoon/internal/api/rest"
	"github.com/monsoondhcp/monsoon/internal/auth"
	"github.com/monsoondhcp/monsoon/internal/discovery"
	"github.com/monsoondhcp/monsoon/internal/events"
	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

const (
	codeOK                 = 0
	codeCanceled           = 1
	codeInvalidArgument    = 3
	codeNotFound           = 5
	codePermissionDenied   = 7
	codeFailedPrecondition = 9
	codeInternal           = 13
	codeUnauthenticated    = 16
)

type HandlerDeps struct {
	LeaseStore       lease.Store
	IPAMEngine       *ipam.Engine
	DiscoveryEngine  *discovery.Engine
	DiscoveryEnabled bool
	Version          string
	StartedAt        time.Time
	StorageReady     func(context.Context) error
	DHCPv4Enabled    bool
	DHCPv4Listen     string
	DHCPv4Running    func() bool
	HAEnabled        bool
	HAStatus         func() any
	EventBroker      *events.Broker
}

type Handler struct {
	deps    HandlerDeps
	methods map[string]methodDesc
}

type methodDesc struct {
	requiredRole string
	decode       func([]byte) (any, error)
	unary        func(context.Context, any) (protoMarshaler, error)
	stream       func(context.Context, any, *serverStream) error
}

type Server struct {
	httpServer *http.Server
	listener   net.Listener
}

type serverOptions struct {
	tlsConfig *tls.Config
}

type ServerOption func(*serverOptions)

func WithTLSConfig(cfg *tls.Config) ServerOption {
	return func(opts *serverOptions) {
		opts.tlsConfig = cfg
	}
}

func NewHandler(deps HandlerDeps) *Handler {
	h := &Handler{
		deps:    deps,
		methods: make(map[string]methodDesc),
	}
	h.registerSubnetService()
	h.registerLeaseService()
	h.registerAddressService()
	h.registerDiscoveryService()
	h.registerSystemService()
	return h
}

func (h *Handler) Handler() http.Handler {
	return h
}

func NewServer(listenAddr string, handler http.Handler, opts ...ServerOption) *Server {
	var options serverOptions
	for _, opt := range opts {
		opt(&options)
	}

	protocols := &http.Protocols{}
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	if options.tlsConfig != nil {
		protocols.SetHTTP2(true)
	}

	return &Server{
		httpServer: &http.Server{
			Addr:              listenAddr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       30 * time.Second,
			Protocols:         protocols,
			TLSConfig:         options.tlsConfig,
		},
	}
}

func (s *Server) Start() error {
	var err error
	s.listener, err = net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}
	listener := s.listener
	if s.httpServer.TLSConfig != nil {
		listener = tls.NewListener(listener, s.httpServer.TLSConfig)
	}
	if err := s.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

type serverStream struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (s *serverStream) Send(msg protoMarshaler) error {
	frame, err := encodeGRPCFrame(msg.marshalProto())
	if err != nil {
		return err
	}
	if _, err := s.w.Write(frame); err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

type statusError struct {
	code    int
	message string
}

func (e statusError) Error() string {
	return e.message
}

func grpcError(code int, message string) error {
	return statusError{code: code, message: message}
}

func statusFromError(err error) (int, string) {
	if err == nil {
		return codeOK, ""
	}
	var se statusError
	if errors.As(err, &se) {
		return se.code, se.message
	}
	switch {
	case errors.Is(err, context.Canceled):
		return codeCanceled, "request canceled"
	case errors.Is(err, storage.ErrNotFound):
		return codeNotFound, "resource not found"
	default:
		return codeInternal, err.Error()
	}
}

func encodeGRPCFrame(payload []byte) ([]byte, error) {
	payloadLen, err := lenToUint32(len(payload), "grpc payload")
	if err != nil {
		return nil, err
	}
	frame := make([]byte, 5+len(payload))
	frame[0] = 0
	binary.BigEndian.PutUint32(frame[1:5], payloadLen)
	copy(frame[5:], payload)
	return frame, nil
}

func decodeGRPCFrame(data []byte) ([]byte, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("grpc frame too short")
	}
	if data[0] != 0 {
		return nil, fmt.Errorf("compressed grpc frames are not supported")
	}
	length := binary.BigEndian.Uint32(data[1:5])
	if len(data[5:]) > int(^uint32(0)) {
		return nil, fmt.Errorf("grpc frame payload too large")
	}
	// #nosec G115 -- payload length is bounded to uint32 above.
	if uint32(len(data[5:])) != length {
		return nil, fmt.Errorf("grpc frame length mismatch")
	}
	return data[5:], nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "grpc requires POST", http.StatusMethodNotAllowed)
		return
	}
	if r.ProtoMajor < 2 {
		http.Error(w, "grpc requires HTTP/2", http.StatusHTTPVersionNotSupported)
		return
	}
	if ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))); !strings.HasPrefix(ct, "application/grpc") {
		http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
		return
	}

	method, ok := h.methods[r.URL.Path]
	if !ok {
		h.writeGRPCStatus(w, codeNotFound, "unknown rpc method")
		return
	}
	if err := authorize(r.Context(), method.requiredRole); err != nil {
		code, message := statusFromError(err)
		h.writeGRPCStatus(w, code, message)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeGRPCStatus(w, codeInternal, err.Error())
		return
	}
	payload, err := decodeGRPCFrame(body)
	if err != nil {
		h.writeGRPCStatus(w, codeInvalidArgument, err.Error())
		return
	}
	req, err := method.decode(payload)
	if err != nil {
		h.writeGRPCStatus(w, codeInvalidArgument, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/grpc+proto")
	w.Header().Set("Trailer", "Grpc-Status, Grpc-Message")

	if method.stream != nil {
		flusher, _ := w.(http.Flusher)
		stream := &serverStream{w: w, flusher: flusher}
		w.WriteHeader(http.StatusOK)
		if flusher != nil {
			flusher.Flush()
		}
		err = method.stream(r.Context(), req, stream)
		code, message := statusFromError(err)
		w.Header().Set("Grpc-Status", strconv.Itoa(code))
		if message != "" {
			w.Header().Set("Grpc-Message", message)
		}
		return
	}

	resp, err := method.unary(r.Context(), req)
	code, message := statusFromError(err)
	if err == nil && resp != nil {
		frame, frameErr := encodeGRPCFrame(resp.marshalProto())
		if frameErr != nil {
			code = codeInternal
			message = frameErr.Error()
		} else {
			_, _ = w.Write(frame)
		}
	}
	w.Header().Set("Grpc-Status", strconv.Itoa(code))
	if message != "" {
		w.Header().Set("Grpc-Message", message)
	}
}

func lenToUint32(value int, field string) (uint32, error) {
	if value < 0 || uint64(value) > uint64(^uint32(0)) {
		return 0, fmt.Errorf("%s too large", field)
	}
	// #nosec G115 -- bounded to uint32 range above.
	return uint32(value), nil
}

func (h *Handler) writeGRPCStatus(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/grpc+proto")
	w.Header().Set("Trailer", "Grpc-Status, Grpc-Message")
	w.Header().Set("Grpc-Status", strconv.Itoa(code))
	if message != "" {
		w.Header().Set("Grpc-Message", message)
	}
	w.WriteHeader(http.StatusOK)
}

func authorize(ctx context.Context, requiredRole string) error {
	if requiredRole == "" {
		return nil
	}
	identity, ok := restapi.IdentityFromContext(ctx)
	if !ok {
		if restapi.AuthEnforcedFromContext(ctx) {
			return grpcError(codeUnauthenticated, "authentication required")
		}
		return nil
	}
	if !auth.HasRole(requiredRole, identity.Role) {
		return grpcError(codePermissionDenied, "permission denied")
	}
	return nil
}
