package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/yourusername/tunnel-project/server/internal/tunnel"
	"github.com/yourusername/tunnel-project/shared/models"
	"gopkg.in/yaml.v3"
)

func main() {
	configPath := flag.String("config", "configs/server.yml", "Path to config file")
	flag.Parse()

	// Загрузка конфигурации
	config, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Println("🚀 Starting Tunnel Server...")

	// Создание и запуск сервера
	server := tunnel.NewServer(config)
	if err := server.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	log.Printf("✅ Tunnel server started successfully on port %d", config.Server.ListenPort)
	log.Printf("   - Metrics endpoint: http://localhost:%d/metrics", config.Server.MetricsPort)

	// Ожидание сигнала завершения
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("🛑 Shutting down gracefully...")
}

func loadConfig(path string) (*models.ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config models.ServerConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
