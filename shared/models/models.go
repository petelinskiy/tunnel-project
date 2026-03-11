package models

import "time"

// ClientConfig конфигурация клиента
type ClientConfig struct {
	Client struct {
		WebUIPort  int    `yaml:"web_ui_port"`
		Socks5Port int    `yaml:"socks5_port"`
		DataDir    string `yaml:"data_dir"`
	} `yaml:"client"`

	Tunnel struct {
		SNIList     []string `yaml:"sni_list"`      // домены для маскировки SNI
		JitterMaxMs int      `yaml:"jitter_max_ms"` // макс. задержка перед подключением (мс)
	} `yaml:"tunnel"`

	Balancing struct {
		Strategy            string        `yaml:"strategy"`
		HealthCheckInterval time.Duration `yaml:"health_check_interval"`
		FailoverEnabled     bool          `yaml:"failover_enabled"`
	} `yaml:"balancing"`

	Servers []ServerInfo `yaml:"servers"`
}

// ServerConfig конфигурация сервера
type ServerConfig struct {
	Server struct {
		ListenPort  int `yaml:"listen_port"`
		MetricsPort int `yaml:"metrics_port"`
	} `yaml:"server"`

	Tunnel struct {
	} `yaml:"tunnel"`

	TLS struct {
		CertPath string `yaml:"cert_path"`
		KeyPath  string `yaml:"key_path"`
		AutoCert bool   `yaml:"auto_cert"`
	} `yaml:"tls"`

	Monitoring struct {
		Enabled         bool   `yaml:"enabled"`
		MetricsEndpoint string `yaml:"metrics_endpoint"`
	} `yaml:"monitoring"`
}

// ServerInfo информация о сервере
type ServerInfo struct {
	ID      string `yaml:"id" json:"id"`
	Host    string `yaml:"host" json:"host"`
	Port    int    `yaml:"port" json:"port"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
}

// ServerMetrics метрики сервера
type ServerMetrics struct {
	ServerID     string        `json:"server_id"`
	Timestamp    time.Time     `json:"timestamp"`
	Latency      time.Duration `json:"latency"`
	PacketLoss   float64       `json:"packet_loss"`
	Bandwidth    int64         `json:"bandwidth"`
	CPUUsage     float64       `json:"cpu_usage"`
	MemoryUsage  float64       `json:"memory_usage"`
	DiskUsage    float64       `json:"disk_usage"`
	ActiveConns  int           `json:"active_conns"`
	TotalTraffic int64         `json:"total_traffic"`
	Status       string        `json:"status"`
	LastSeen     time.Time     `json:"last_seen"`
}

// DeployRequest запрос на развертывание сервера
type DeployRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	KeyFile  string `json:"key_file,omitempty"`
	Region   string `json:"region,omitempty"`
}

// DeployStatus статус развертывания
type DeployStatus struct {
	ServerID string    `json:"server_id"`
	Status   string    `json:"status"` // deploying, success, failed
	Progress int       `json:"progress"`
	Message  string    `json:"message"`
	Error    string    `json:"error,omitempty"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitempty"`
}
