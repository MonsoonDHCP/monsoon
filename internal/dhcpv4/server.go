package dhcpv4

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

type Server struct {
	listenAddr string
	handler    *Handler

	mu      sync.Mutex
	conn    *net.UDPConn
	running bool
}

func NewServer(listenAddr string, handler *Handler) *Server {
	return &Server{listenAddr: listenAddr, handler: handler}
}

func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("dhcpv4 server already running")
	}
	addr, err := net.ResolveUDPAddr("udp4", s.listenAddr)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.conn = conn
	s.running = true
	s.mu.Unlock()

	defer s.Close()
	buf := make([]byte, 1500)

	for {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					return nil
				default:
					continue
				}
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			log.Printf("dhcpv4 read error: %v", err)
			continue
		}
		req, err := DecodePacket(buf[:n])
		if err != nil {
			log.Printf("dhcpv4 decode error: %v", err)
			continue
		}
		resp, err := s.handler.Handle(ctx, req, remote)
		if err != nil {
			log.Printf("dhcpv4 handle error: %v", err)
			continue
		}
		if resp == nil {
			continue
		}
		raw, err := resp.Encode()
		if err != nil {
			log.Printf("dhcpv4 encode error: %v", err)
			continue
		}
		target := ResponseTarget(req, remote, *resp)
		if _, err := conn.WriteToUDP(raw, target); err != nil {
			log.Printf("dhcpv4 write error: %v", err)
		}
	}
}

func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	if s.conn == nil {
		return nil
	}
	err := s.conn.Close()
	s.conn = nil
	return err
}

func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}
