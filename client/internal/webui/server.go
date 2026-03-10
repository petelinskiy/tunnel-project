package webui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/yourusername/tunnel-project/client/internal/deploy"
	"github.com/yourusername/tunnel-project/client/internal/tunnel"
	"github.com/yourusername/tunnel-project/shared/models"
)

// Server веб-сервер для UI
type Server struct {
	port          int
	tunnelManager *tunnel.Manager
	router        *mux.Router
	wsHub         *WebSocketHub
}

// NewServer создает новый веб-сервер
func NewServer(port int, tunnelManager *tunnel.Manager) *Server {
	s := &Server{
		port:          port,
		tunnelManager: tunnelManager,
		router:        mux.NewRouter(),
		wsHub:         NewWebSocketHub(),
	}

	s.setupRoutes()
	return s
}

// Start запускает веб-сервер
func (s *Server) Start() error {
	go s.wsHub.Run()
	go s.metricsStreamer()

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("Starting Web UI on %s", addr)

	return http.ListenAndServe(addr, s.router)
}

// setupRoutes настраивает маршруты
func (s *Server) setupRoutes() {
	s.router.PathPrefix("/static/").Handler(http.StripPrefix("/static/",
		http.FileServer(http.Dir("web/dist"))))

	api := s.router.PathPrefix("/api").Subrouter()
	api.HandleFunc("/servers", s.handleGetServers).Methods("GET")
	api.HandleFunc("/servers", s.handleAddServer).Methods("POST")
	api.HandleFunc("/servers/{id}", s.handleDeleteServer).Methods("DELETE")
	api.HandleFunc("/metrics", s.handleGetMetrics).Methods("GET")
	api.HandleFunc("/deploy", s.handleDeploy).Methods("POST")

	s.router.HandleFunc("/ws/monitor", s.handleWebSocket)
	s.router.HandleFunc("/health", s.handleHealth).Methods("GET")
	s.router.HandleFunc("/", s.handleIndex).Methods("GET")
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "web/dist/index.html")
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleGetServers возвращает список серверов
func (s *Server) handleGetServers(w http.ResponseWriter, r *http.Request) {
	servers := s.tunnelManager.GetServers()

	response := make([]map[string]interface{}, len(servers))
	for i, srv := range servers {
		response[i] = map[string]interface{}{
			"id":      srv.Info.ID,
			"host":    srv.Info.Host,
			"port":    srv.Info.Port,
			"enabled": srv.Info.Enabled,
			"active":  srv.Active,
			"metrics": srv.Metrics,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleAddServer добавляет уже работающий сервер без деплоя
func (s *Server) handleAddServer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Host == "" {
		http.Error(w, "host is required", http.StatusBadRequest)
		return
	}
	port := req.Port
	if port == 0 {
		port = 443
	}

	info := models.ServerInfo{
		ID:      fmt.Sprintf("server-%s", req.Host),
		Host:    req.Host,
		Port:    port,
		Enabled: true,
	}
	s.tunnelManager.AddServer(info)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "connecting",
		"id":     info.ID,
	})
}

// handleDeleteServer удаляет сервер
func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{"error": "not implemented"})
}

// handleGetMetrics возвращает метрики
func (s *Server) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := s.tunnelManager.GetMetrics()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// deployRequest — тело запроса на деплой
type deployRequest struct {
	Host     string `json:"host"`
	SSHUser  string `json:"username"`
	SSHPass  string `json:"password"`
	TunPort  int    `json:"port"` // tunnel port, default 443
}

// handleDeploy разворачивает сервер через SSH и подключается к нему
func (s *Server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	var req deployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Host == "" || req.SSHUser == "" || req.SSHPass == "" {
		http.Error(w, "host, username and password are required", http.StatusBadRequest)
		return
	}
	if req.TunPort == 0 {
		req.TunPort = 443
	}

	serverID := fmt.Sprintf("server-%s", req.Host)

	// Деплой запускается асинхронно; прогресс идёт через WebSocket.
	go func() {
		broadcast := func(payload map[string]interface{}) {
			data, _ := json.Marshal(payload)
			s.wsHub.broadcast <- data
		}

		d := deploy.NewDeployer(s.tunnelManager.GetObfuscationKey())
		err := d.Deploy(req.Host, req.SSHUser, req.SSHPass, func(progress int, msg string) {
			log.Printf("[deploy %s] %d%% %s", serverID, progress, msg)
			broadcast(map[string]interface{}{
				"type":     "deploy_progress",
				"server":   serverID,
				"progress": progress,
				"message":  msg,
			})
		})

		if err != nil {
			log.Printf("[deploy %s] error: %v", serverID, err)
			broadcast(map[string]interface{}{
				"type":    "deploy_error",
				"server":  serverID,
				"message": err.Error(),
			})
			return
		}

		// Добавляем сервер в менеджер
		s.tunnelManager.AddServer(models.ServerInfo{
			ID:      serverID,
			Host:    req.Host,
			Port:    req.TunPort,
			Enabled: true,
		})

		broadcast(map[string]interface{}{
			"type":   "deploy_done",
			"server": serverID,
		})
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "deploying",
		"serverID": serverID,
	})
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleWebSocket обрабатывает WebSocket соединения
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	client := &WebSocketClient{
		hub:  s.wsHub,
		conn: conn,
		send: make(chan []byte, 256),
	}

	client.hub.register <- client
	go client.writePump()
	go client.readPump()
}

// metricsStreamer периодически отправляет метрики через WebSocket
func (s *Server) metricsStreamer() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		metrics := s.tunnelManager.GetMetrics()
		data, _ := json.Marshal(map[string]interface{}{
			"type":    "metrics",
			"metrics": metrics,
		})
		s.wsHub.broadcast <- data
	}
}
