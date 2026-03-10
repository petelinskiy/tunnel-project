package tunnel

import (
	"math/rand"
	"sync"
	"time"
)

// LoadBalancer балансировщик нагрузки между серверами
type LoadBalancer struct {
	manager  *Manager
	strategy string
	index    int
	mu       sync.Mutex
}

// NewLoadBalancer создает новый балансировщик
func NewLoadBalancer(manager *Manager, strategy string) *LoadBalancer {
	return &LoadBalancer{
		manager:  manager,
		strategy: strategy,
		index:    0,
	}
}

// SelectServer выбирает сервер согласно стратегии
func (lb *LoadBalancer) SelectServer() *ServerConnection {
	switch lb.strategy {
	case "round-robin":
		return lb.roundRobin()
	case "latency":
		return lb.lowestLatency()
	case "least-loaded":
		return lb.leastLoaded()
	default:
		return lb.roundRobin()
	}
}

// roundRobin выбор по кругу
func (lb *LoadBalancer) roundRobin() *ServerConnection {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	
	servers := lb.getActiveServers()
	if len(servers) == 0 {
		return nil
	}
	
	server := servers[lb.index%len(servers)]
	lb.index++
	
	return server
}

// lowestLatency выбор с наименьшей задержкой
func (lb *LoadBalancer) lowestLatency() *ServerConnection {
	servers := lb.getActiveServers()
	if len(servers) == 0 {
		return nil
	}
	
	var best *ServerConnection
	var bestLatency time.Duration = time.Hour
	
	for _, s := range servers {
		s.mu.RLock()
		latency := s.Metrics.Latency
		s.mu.RUnlock()
		
		if latency < bestLatency {
			bestLatency = latency
			best = s
		}
	}
	
	return best
}

// leastLoaded выбор наименее загруженного
func (lb *LoadBalancer) leastLoaded() *ServerConnection {
	servers := lb.getActiveServers()
	if len(servers) == 0 {
		return nil
	}
	
	var best *ServerConnection
	var lowestLoad int = 1000000
	
	for _, s := range servers {
		s.mu.RLock()
		load := s.Metrics.ActiveConns
		s.mu.RUnlock()
		
		if load < lowestLoad {
			lowestLoad = load
			best = s
		}
	}
	
	return best
}

// getActiveServers возвращает активные серверы
func (lb *LoadBalancer) getActiveServers() []*ServerConnection {
	allServers := lb.manager.GetServers()
	active := make([]*ServerConnection, 0, len(allServers))
	
	for _, s := range allServers {
		s.mu.RLock()
		isActive := s.Active
		s.mu.RUnlock()
		
		if isActive {
			active = append(active, s)
		}
	}
	
	// Если нет активных, пробуем случайный (для reconnect)
	if len(active) == 0 && len(allServers) > 0 {
		return []*ServerConnection{allServers[rand.Intn(len(allServers))]}
	}
	
	return active
}
