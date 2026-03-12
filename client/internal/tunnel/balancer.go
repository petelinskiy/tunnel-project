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
	pinnedID string // если задан — весь трафик идёт только через этот сервер
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

// SetPinned переключает режим: пустой serverID = балансировка, непустой = pinned.
func (lb *LoadBalancer) SetPinned(serverID string) {
	lb.mu.Lock()
	lb.pinnedID = serverID
	lb.mu.Unlock()
}

// GetMode возвращает текущий режим и pinned-сервер (если есть).
func (lb *LoadBalancer) GetMode() (mode, pinnedID string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if lb.pinnedID != "" {
		return "pinned", lb.pinnedID
	}
	return lb.strategy, ""
}

// SelectServer выбирает сервер согласно стратегии
func (lb *LoadBalancer) SelectServer() *ServerConnection {
	lb.mu.Lock()
	pinnedID := lb.pinnedID
	lb.mu.Unlock()

	if pinnedID != "" {
		return lb.findByID(pinnedID)
	}

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

// findByID возвращает активный сервер по ID, или nil.
func (lb *LoadBalancer) findByID(id string) *ServerConnection {
	for _, s := range lb.manager.GetServers() {
		if s.Info.ID == id {
			s.mu.RLock()
			active := s.Active
			s.mu.RUnlock()
			if active {
				return s
			}
		}
	}
	return nil
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
