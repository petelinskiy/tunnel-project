package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	mrand "math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sort"
	"time"

	smux "github.com/xtaci/smux"
	utls "github.com/refraction-networking/utls"
	"github.com/yourusername/tunnel-project/shared/models"
	"github.com/yourusername/tunnel-project/shared/transport"
	"gopkg.in/yaml.v3"
)

// Manager управляет туннельными соединениями
type Manager struct {
	config     *models.ClientConfig
	configPath string
	servers    map[string]*ServerConnection
	balancer   *LoadBalancer
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
}

// ServerConnection представляет соединение с сервером
type ServerConnection struct {
	Info    models.ServerInfo
	Session *smux.Session
	Metrics *models.ServerMetrics
	Active  bool
	mu      sync.RWMutex
}

// NewManager создает новый менеджер туннелей
func NewManager(config *models.ClientConfig, configPath string) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		config:     config,
		configPath: configPath,
		servers:    make(map[string]*ServerConnection),
		ctx:        ctx,
		cancel:     cancel,
	}

	m.balancer = NewLoadBalancer(m, config.Balancing.Strategy)

	// Генерируем токен если его нет — сохраняем в конфиг сразу
	if config.Tunnel.AuthToken == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err == nil {
			config.Tunnel.AuthToken = hex.EncodeToString(b)
			if err := m.saveConfig(); err != nil {
				log.Printf("Warning: failed to save generated auth token: %v", err)
			} else {
				log.Printf("Generated new auth token and saved to config")
			}
		}
	}

	return m
}

// GetAuthToken возвращает токен аутентификации для передачи деплойеру
func (m *Manager) GetAuthToken() string {
	return m.config.Tunnel.AuthToken
}

// Start запускает менеджер
func (m *Manager) Start() error {
	log.Println("Starting tunnel manager...")

	for _, server := range m.config.Servers {
		if server.Enabled {
			go func(s models.ServerInfo) {
				if err := m.connectToServer(s); err != nil {
					log.Printf("connectToServer %s failed: %v", s.ID, err)
				}
			}(server)
		}
	}

	go m.healthChecker()
	return nil
}

// Stop останавливает менеджер
func (m *Manager) Stop() {
	m.cancel()

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, conn := range m.servers {
		if conn.Session != nil {
			conn.Session.Close()
		}
	}
}

// pickSNI возвращает случайный SNI из списка, или host если список пуст
func (m *Manager) pickSNI(host string) string {
	list := m.config.Tunnel.SNIList
	if len(list) == 0 {
		return host
	}
	return list[mrand.Intn(len(list))]
}

// connectToServer устанавливает uTLS соединение с Chrome fingerprint и запускает smux
func (m *Manager) connectToServer(server models.ServerInfo) error {
	// Jitter: случайная задержка перед подключением
	if ms := m.config.Tunnel.JitterMaxMs; ms > 0 {
		delay := time.Duration(mrand.Intn(ms)) * time.Millisecond
		log.Printf("Jitter delay: %v before connecting to %s:%d", delay, server.Host, server.Port)
		select {
		case <-time.After(delay):
		case <-m.ctx.Done():
			return m.ctx.Err()
		}
	}

	sni := m.pickSNI(server.Host)
	log.Printf("Connecting to server %s:%d (uTLS Chrome, SNI=%s)...", server.Host, server.Port, sni)

	addr := fmt.Sprintf("%s:%d", server.Host, server.Port)
	tcpConn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("tcp dial: %w", err)
	}

	// uTLS: TLS fingerprint Chrome — DPI видит обычный HTTPS браузер
	// Chrome fingerprint естественно включает h2 в ALPN ClientHello
	cfg := &utls.Config{
		ServerName:         sni,
		InsecureSkipVerify: true, // сертификат сервера самоподписан
	}
	uconn := utls.UClient(tcpConn, cfg, utls.HelloChrome_Auto)
	if err := uconn.HandshakeContext(m.ctx); err != nil {
		tcpConn.Close()
		return fmt.Errorf("uTLS handshake: %w", err)
	}

	// WebSocket upgrade поверх TLS — трафик выглядит как браузерный WebSocket
	wsConn, err := transport.ClientUpgrade(uconn, sni, m.config.Tunnel.AuthToken)
	if err != nil {
		uconn.Close()
		return fmt.Errorf("WebSocket upgrade: %w", err)
	}

	// smux поверх WebSocket — один коннект, много независимых потоков
	smuxCfg := smux.DefaultConfig()
	smuxCfg.KeepAliveInterval  = 10 * time.Second
	smuxCfg.KeepAliveTimeout   = 30 * time.Second
	smuxCfg.MaxFrameSize       = 65535
	smuxCfg.MaxReceiveBuffer   = 67108864 // 64 MB
	smuxCfg.MaxStreamBuffer    = 16777216 // 16 MB

	session, err := smux.Client(wsConn, smuxCfg)
	if err != nil {
		wsConn.Close()
		return fmt.Errorf("smux client: %w", err)
	}

	serverConn := &ServerConnection{
		Info:    server,
		Session: session,
		Active:  true,
		Metrics: &models.ServerMetrics{
			ServerID:  server.ID,
			Timestamp: time.Now(),
			Status:    "active",
		},
	}

	m.mu.Lock()
	m.servers[server.ID] = serverConn
	m.mu.Unlock()

	log.Printf("✓ Connected to server %s (%s)", server.ID, addr)
	return nil
}

// ── Dial ──────────────────────────────────────────────────────────────────────

// Dial открывает новый поток через выбранный сервер
func (m *Manager) Dial(network, address string) (net.Conn, error) {
	server := m.balancer.SelectServer()
	if server == nil {
		return nil, fmt.Errorf("no available servers")
	}
	return m.dialThroughServer(server, address)
}

// dialThroughServer открывает smux-поток и согласовывает адрес с сервером
func (m *Manager) dialThroughServer(server *ServerConnection, address string) (net.Conn, error) {
	server.mu.RLock()
	session := server.Session
	server.mu.RUnlock()

	if session == nil || session.IsClosed() {
		return nil, fmt.Errorf("server %s has no active session", server.Info.ID)
	}

	stream, err := session.OpenStream()
	if err != nil {
		return nil, fmt.Errorf("open stream: %w", err)
	}

	// Отправляем адрес назначения
	if _, err := fmt.Fprintf(stream, "%s\n", address); err != nil {
		stream.Close()
		return nil, fmt.Errorf("write address: %w", err)
	}

	// Ждём подтверждения от сервера
	resp, err := readLine(stream)
	if err != nil {
		stream.Close()
		return nil, fmt.Errorf("read server response: %w", err)
	}
	if resp != "OK" {
		stream.Close()
		return nil, fmt.Errorf("server rejected %s: %s", address, resp)
	}

	server.mu.Lock()
	server.Metrics.ActiveConns++
	server.mu.Unlock()

	return &trackedStream{Stream: stream, server: server}, nil
}

// trackedStream оборачивает smux.Stream и отслеживает счётчик соединений
type trackedStream struct {
	*smux.Stream
	server *ServerConnection
	once   sync.Once
}

func (t *trackedStream) Close() error {
	t.once.Do(func() {
		t.server.mu.Lock()
		if t.server.Metrics.ActiveConns > 0 {
			t.server.Metrics.ActiveConns--
		}
		t.server.mu.Unlock()
	})
	return t.Stream.Close()
}

// ── Health check ──────────────────────────────────────────────────────────────

func (m *Manager) healthChecker() {
	ticker := time.NewTicker(m.config.Balancing.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkServersHealth()
		}
	}
}

func (m *Manager) checkServersHealth() {
	m.mu.Lock()
	var dead []models.ServerInfo
	var alive []*ServerConnection
	for id, server := range m.servers {
		if server.Session == nil || server.Session.IsClosed() {
			server.Active = false
			server.Metrics.Status = "down"
			dead = append(dead, server.Info)
			delete(m.servers, id)
		} else {
			server.Active = true
			server.Metrics.Status = "active"
			server.Metrics.Timestamp = time.Now()
			alive = append(alive, server)
		}
	}
	m.mu.Unlock()

	// Замер RTT (TCP dial к серверу) + сбор метрик с сервера
	for _, server := range alive {
		go func(s *ServerConnection) {
			addr := fmt.Sprintf("%s:%d", s.Info.Host, s.Info.Port)
			start := time.Now()
			conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
			if err == nil {
				conn.Close()
				latency := time.Since(start)
				s.mu.Lock()
				s.Metrics.Latency = latency
				s.mu.Unlock()
			}

			fetchServerMetrics(s)
		}(server)
	}

	for _, srv := range dead {
		go func(s models.ServerInfo) {
			log.Printf("Reconnecting to %s...", s.ID)
			if err := m.connectToServer(s); err != nil {
				log.Printf("Reconnect to %s failed: %v", s.ID, err)
			}
		}(srv)
	}
}

// ── Exported API ──────────────────────────────────────────────────────────────

// SetBalancing переключает режим балансировки.
// mode="round-robin" (или другая стратегия) — балансировка по всем серверам.
// mode="pinned", serverID="server-X.X.X.X" — весь трафик через один сервер.
func (m *Manager) SetBalancing(mode, serverID string) {
	if mode == "pinned" {
		m.balancer.SetPinned(serverID)
	} else {
		m.balancer.SetPinned("") // снимаем pin, возвращаемся к стратегии
	}
}

// GetBalancing возвращает текущий режим и ID pinned-сервера (если есть).
func (m *Manager) GetBalancing() (mode, pinnedID string) {
	return m.balancer.GetMode()
}

func (m *Manager) GetServers() []*ServerConnection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	servers := make([]*ServerConnection, 0, len(m.servers))
	for _, s := range m.servers {
		servers = append(servers, s)
	}
	sort.Slice(servers, func(i, j int) bool {
		return servers[i].Info.ID < servers[j].Info.ID
	})
	return servers
}

func (m *Manager) AddServer(server models.ServerInfo) {
	// Сохраняем сервер в конфиг-файл если его там ещё нет
	m.mu.Lock()
	found := false
	for _, s := range m.config.Servers {
		if s.ID == server.ID {
			found = true
			break
		}
	}
	if !found {
		m.config.Servers = append(m.config.Servers, server)
		if err := m.saveConfig(); err != nil {
			log.Printf("Warning: failed to persist server %s to config: %v", server.ID, err)
		}
	}
	m.mu.Unlock()

	go func() {
		if err := m.connectToServer(server); err != nil {
			log.Printf("connectToServer %s failed: %v", server.ID, err)
		}
	}()
}

// RemoveServer отключает сервер и удаляет его из конфига.
func (m *Manager) RemoveServer(id string) error {
	m.mu.Lock()
	conn, ok := m.servers[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("server %s not found", id)
	}
	delete(m.servers, id)

	// Убираем из конфига
	filtered := m.config.Servers[:0]
	for _, s := range m.config.Servers {
		if s.ID != id {
			filtered = append(filtered, s)
		}
	}
	m.config.Servers = filtered
	saveErr := m.saveConfig()

	// Если был pinned на этот сервер — сбрасываем
	if mode, pinnedID := m.balancer.GetMode(); mode == "pinned" && pinnedID == id {
		m.balancer.SetPinned("")
	}
	m.mu.Unlock()

	// Закрываем сессию вне лока
	conn.mu.Lock()
	if conn.Session != nil {
		conn.Session.Close()
	}
	conn.mu.Unlock()

	return saveErr
}

// saveConfig записывает текущий конфиг обратно в YAML-файл
func (m *Manager) saveConfig() error {
	if m.configPath == "" {
		return nil
	}
	data, err := yaml.Marshal(m.config)
	if err != nil {
		return err
	}
	return os.WriteFile(m.configPath, data, 0644)
}

func (m *Manager) GetMetrics() []*models.ServerMetrics {
	servers := m.GetServers()
	metrics := make([]*models.ServerMetrics, len(servers))
	for i, s := range servers {
		s.mu.RLock()
		metrics[i] = s.Metrics
		s.mu.RUnlock()
	}
	return metrics
}

// ── Server metrics polling ────────────────────────────────────────────────────

func fetchServerMetrics(s *ServerConnection) {
	metricsPort := s.Info.Port + 8000
	url := fmt.Sprintf("http://%s:%d/metrics", s.Info.Host, metricsPort)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var data struct {
		CPUUsage     float64 `json:"cpu_usage"`
		MemoryUsage  float64 `json:"memory_usage"`
		ActiveConns  int     `json:"active_conns"`
		TotalTraffic int64   `json:"total_traffic"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return
	}

	s.mu.Lock()
	s.Metrics.CPUUsage    = data.CPUUsage
	s.Metrics.MemoryUsage = data.MemoryUsage
	s.Metrics.ActiveConns = data.ActiveConns
	s.Metrics.TotalTraffic = data.TotalTraffic
	s.Metrics.Bandwidth   = data.TotalTraffic
	s.mu.Unlock()
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func readLine(r io.Reader) (string, error) {
	var buf []byte
	b := [1]byte{}
	for {
		if _, err := r.Read(b[:]); err != nil {
			return strings.TrimRight(string(buf), "\r"), err
		}
		if b[0] == '\n' {
			return strings.TrimRight(string(buf), "\r"), nil
		}
		buf = append(buf, b[0])
	}
}
