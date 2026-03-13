package tunnel

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/yourusername/tunnel-project/shared/models"
	"github.com/yourusername/tunnel-project/shared/transport"
)

// cpuSnapshot — снимок /proc/stat для расчёта дельты CPU
type cpuSnapshot struct {
	total, idle int64
}

// Server туннельный сервер
type Server struct {
	config      *models.ServerConfig
	listener    net.Listener
	ctx         context.Context
	cancel      context.CancelFunc
	activeConns int64 // atomic
	totalBytes  int64 // atomic
	statsMu     sync.RWMutex
	cpuUsage    float64
	memUsage    float64
	prevCPU     cpuSnapshot
}

// NewServer создает новый сервер
func NewServer(config *models.ServerConfig) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		config: config,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start запускает TLS сервер с smux
func (s *Server) Start() error {
	transport.FallbackHandler = serveStaticFile

	cert, err := tls.LoadX509KeyPair(s.config.TLS.CertPath, s.config.TLS.KeyPath)
	if err != nil {
		log.Println("Generating self-signed certificate...")
		cert, err = s.generateSelfSignedCert()
		if err != nil {
			return fmt.Errorf("failed to generate certificate: %w", err)
		}
	}

	// Только http/1.1 — наш сервер говорит HTTP/1.1 WebSocket, не HTTP/2.
	// Chrome fingerprint отправляет h2 в ClientHello, но сервер отвечает http/1.1 — это нормально.
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"http/1.1"},
	}

	addr := fmt.Sprintf(":%d", s.config.Server.ListenPort)
	ln, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = ln

	log.Printf("Server listening on %s (TLS 1.3 + yamux)", addr)

	s.prevCPU, _ = readCPUStat()
	go s.acceptLoop()
	go s.statsCollector()

	if s.config.Server.MetricsPort > 0 {
		go s.startMetricsHTTP()
	}

	return nil
}

// acceptLoop принимает входящие TLS соединения
func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				log.Printf("Accept error: %v", err)
				continue
			}
		}
		log.Printf("New connection from %s", conn.RemoteAddr())
		go s.handleConn(conn)
	}
}

// handleConn выполняет WebSocket upgrade, затем запускает yamux-сессию
func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	wsConn, err := transport.ServerUpgrade(conn, s.config.AuthToken)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	if wsConn == nil {
		// не WebSocket-запрос — фейковая страница уже отдана
		return
	}

	yamuxCfg := yamux.DefaultConfig()
	yamuxCfg.EnableKeepAlive     = true
	yamuxCfg.KeepAliveInterval   = 10 * time.Second
	yamuxCfg.MaxStreamWindowSize = 16 * 1024 * 1024 // 16 MB window
	yamuxCfg.LogOutput           = io.Discard

	session, err := yamux.Server(wsConn, yamuxCfg)
	if err != nil {
		log.Printf("yamux server error: %v", err)
		return
	}
	defer session.Close()

	for {
		stream, err := session.Accept()
		if err != nil {
			if !session.IsClosed() {
				log.Printf("Accept error: %v", err)
			}
			return
		}
		go s.handleStream(stream)
	}
}

// Stop останавливает сервер
func (s *Server) Stop() {
	s.cancel()
	if s.listener != nil {
		s.listener.Close()
	}
}

// handleStream читает адрес назначения, подключается и проксирует трафик
func (s *Server) handleStream(stream net.Conn) {
	defer stream.Close()

	stream.SetReadDeadline(time.Now().Add(5 * time.Second))
	addr, err := readLine(stream)
	stream.SetReadDeadline(time.Time{})
	if err != nil || addr == "" {
		return
	}

	log.Printf("Stream → %s", addr)

	targetConn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		log.Printf("Stream: failed to connect to %s: %v", addr, err)
		fmt.Fprintf(stream, "ERR %v\n", err)
		return
	}
	defer targetConn.Close()

	if _, err := fmt.Fprintf(stream, "OK\n"); err != nil {
		return
	}

	atomic.AddInt64(&s.activeConns, 1)
	defer atomic.AddInt64(&s.activeConns, -1)

	cw := &countingWriter{counter: &s.totalBytes}

	done := make(chan struct{}, 2)
	go func() {
		io.Copy(io.MultiWriter(targetConn, cw), stream)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(io.MultiWriter(stream, cw), targetConn)
		done <- struct{}{}
	}()
	<-done
}

// ── Stats ─────────────────────────────────────────────────────────────────────

func (s *Server) statsCollector() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			cpu := s.calcCPU()
			mem := readMemUsage()
			s.statsMu.Lock()
			s.cpuUsage = cpu
			s.memUsage = mem
			s.statsMu.Unlock()
		}
	}
}

func (s *Server) startMetricsHTTP() {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		s.statsMu.RLock()
		cpu := s.cpuUsage
		mem := s.memUsage
		s.statsMu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"cpu_usage":     cpu,
			"memory_usage":  mem,
			"active_conns":  atomic.LoadInt64(&s.activeConns),
			"total_traffic": atomic.LoadInt64(&s.totalBytes),
		})
	})

	addr := fmt.Sprintf(":%d", s.config.Server.MetricsPort)
	log.Printf("Metrics endpoint: http://localhost%s/metrics", addr)
	http.ListenAndServe(addr, mux)
}

// ── CPU ───────────────────────────────────────────────────────────────────────

func readCPUStat() (cpuSnapshot, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuSnapshot{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		var vals [8]int64
		for i := 0; i < len(fields) && i < 8; i++ {
			vals[i], _ = strconv.ParseInt(fields[i], 10, 64)
		}
		idle := vals[3] + vals[4]
		total := vals[0] + vals[1] + vals[2] + vals[3] + vals[4] + vals[5] + vals[6] + vals[7]
		return cpuSnapshot{total: total, idle: idle}, nil
	}
	return cpuSnapshot{}, fmt.Errorf("cpu line not found")
}

func (s *Server) calcCPU() float64 {
	cur, err := readCPUStat()
	if err != nil {
		return 0
	}
	prev := s.prevCPU
	s.prevCPU = cur

	totalDelta := cur.total - prev.total
	idleDelta := cur.idle - prev.idle
	if totalDelta == 0 {
		return 0
	}
	return 100.0 * float64(totalDelta-idleDelta) / float64(totalDelta)
}

// ── Memory ────────────────────────────────────────────────────────────────────

func readMemUsage() float64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()

	var total, available int64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseInt(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			total = val
		case "MemAvailable:":
			available = val
		}
		if total > 0 && available > 0 {
			break
		}
	}
	if total == 0 {
		return 0
	}
	return 100.0 * float64(total-available) / float64(total)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

type countingWriter struct {
	counter *int64
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	atomic.AddInt64(cw.counter, int64(len(p)))
	return len(p), nil
}

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

// generateSelfSignedCert генерирует самоподписанный TLS-сертификат
func (s *Server) generateSelfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate key: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"Tunnel"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create cert: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshal key: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return tls.X509KeyPair(certPEM, keyPEM)
}
