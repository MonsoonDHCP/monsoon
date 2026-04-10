package dhcpv6

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
		return fmt.Errorf("dhcpv6 server already running")
	}
	addr, err := net.ResolveUDPAddr("udp6", s.listenAddr)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	conn, err := net.ListenUDP("udp6", addr)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.conn = conn
	s.running = true
	s.mu.Unlock()

	defer s.Close()
	buf := make([]byte, 2048)
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
			log.Printf("dhcpv6 read error: %v", err)
			continue
		}
		req, err := DecodePacket(buf[:n])
		if err != nil {
			log.Printf("dhcpv6 decode error: %v", err)
			continue
		}
		resp, err := s.handler.Handle(ctx, req, remote)
		if err != nil {
			log.Printf("dhcpv6 handle error: %v", err)
			continue
		}
		if resp == nil {
			continue
		}
		raw, err := resp.Encode()
		if err != nil {
			log.Printf("dhcpv6 encode error: %v", err)
			continue
		}
		target := remote
		if resp.IsRelay() && req.IsRelay() && req.PeerAddress != nil {
			target = &net.UDPAddr{IP: remote.IP, Port: remote.Port, Zone: remote.Zone}
		}
		if _, err := conn.WriteToUDP(raw, target); err != nil {
			log.Printf("dhcpv6 write error: %v", err)
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
