package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/yourusername/tunnel-project/client/internal/socks5"
	"github.com/yourusername/tunnel-project/client/internal/tunnel"
	"github.com/yourusername/tunnel-project/client/internal/webui"
	"github.com/yourusername/tunnel-project/shared/models"
	"gopkg.in/yaml.v3"
)

func main() {
	configPath := flag.String("config", "configs/client.yml", "Path to config file")
	flag.Parse()

	// Загрузка конфигурации
	config, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Println("🚀 Starting Tunnel Client...")

	// Инициализация туннельного менеджера
	tunnelManager := tunnel.NewManager(config)
	if err := tunnelManager.Start(); err != nil {
		log.Fatalf("Failed to start tunnel manager: %v", err)
	}
	defer tunnelManager.Stop()

	// Запуск SOCKS5 сервера
	socks5Server := socks5.NewServer(config.Client.Socks5Port, tunnelManager)
	go func() {
		log.Printf("✓ SOCKS5 proxy listening on :%d", config.Client.Socks5Port)
		if err := socks5Server.Start(); err != nil {
			log.Fatalf("SOCKS5 server failed: %v", err)
		}
	}()

	// Запуск Web UI
	webServer := webui.NewServer(config.Client.WebUIPort, tunnelManager)
	go func() {
		log.Printf("✓ Web UI available at http://localhost:%d", config.Client.WebUIPort)
		if err := webServer.Start(); err != nil {
			log.Fatalf("Web UI server failed: %v", err)
		}
	}()

	log.Println("✅ Tunnel client started successfully!")
	log.Printf("   - Web UI: http://localhost:%d", config.Client.WebUIPort)
	log.Printf("   - SOCKS5: localhost:%d", config.Client.Socks5Port)

	// Ожидание сигнала завершения
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("🛑 Shutting down gracefully...")
}

func loadConfig(path string) (*models.ClientConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config models.ClientConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
