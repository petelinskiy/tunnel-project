package socks5

import (
	"context"
	"fmt"
	"log"
	"net"

	"github.com/armon/go-socks5"
	"github.com/yourusername/tunnel-project/client/internal/tunnel"
)

// Server SOCKS5 прокси сервер
type Server struct {
	port          int
	tunnelManager *tunnel.Manager
	server        *socks5.Server
}

// NewServer создает новый SOCKS5 сервер
func NewServer(port int, tunnelManager *tunnel.Manager) *Server {
	return &Server{
		port:          port,
		tunnelManager: tunnelManager,
	}
}

// Start запускает SOCKS5 сервер
func (s *Server) Start() error {
	// Создаем кастомный Dialer, который использует туннель
	dialer := &TunnelDialer{manager: s.tunnelManager}
	
	// Конфигурация SOCKS5
	conf := &socks5.Config{
		Dial: dialer.Dial,
	}
	
	// Создаем сервер
	server, err := socks5.New(conf)
	if err != nil {
		return err
	}
	
	s.server = server
	
	// Запускаем
	addr := fmt.Sprintf("0.0.0.0:%d", s.port)
	log.Printf("Starting SOCKS5 server on %s", addr)
	
	return server.ListenAndServe("tcp", addr)
}

// TunnelDialer использует туннель для соединений
type TunnelDialer struct {
	manager *tunnel.Manager
}

// Dial создает соединение через туннель
func (d *TunnelDialer) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	log.Printf("SOCKS5: Dialing %s via tunnel", addr)
	return d.manager.Dial(network, addr)
}
