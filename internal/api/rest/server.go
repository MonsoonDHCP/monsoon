package rest

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	httpServer  *http.Server
	tlsCertFile string
	tlsKeyFile  string
}

type ServerOption func(*Server)

func WithTLS(certFile, keyFile string) ServerOption {
	return func(s *Server) {
		s.tlsCertFile = strings.TrimSpace(certFile)
		s.tlsKeyFile = strings.TrimSpace(keyFile)
	}
}

func NewServer(listenAddr string, handler http.Handler, opts ...ServerOption) *Server {
	server := &Server{
		httpServer: &http.Server{
			Addr:              listenAddr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(server)
		}
	}
	return server
}

func (s *Server) Start() error {
	var err error
	switch {
	case s.tlsCertFile != "" || s.tlsKeyFile != "":
		err = s.httpServer.ListenAndServeTLS(s.tlsCertFile, s.tlsKeyFile)
	default:
		err = s.httpServer.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
