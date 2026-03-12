package webui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ClientMetrics — метрики самого клиентского узла.
type ClientMetrics struct {
	CPU      float64 `json:"cpu"`         // % загрузки CPU
	MemUsed  float64 `json:"mem_used"`    // % использованной RAM
	NetIface string  `json:"net_iface"`   // интерфейс default gateway
	NetRxBps int64   `json:"net_rx_bps"`  // входящий трафик, байт/с
	NetTxBps int64   `json:"net_tx_bps"`  // исходящий трафик, байт/с
}

type cpuSnap struct{ total, idle int64 }
type netSnap struct{ rx, tx int64 }

type clientMetricsCollector struct {
	mu       sync.RWMutex
	current  ClientMetrics
	prevCPU  cpuSnap
	prevNet  netSnap
	prevTime time.Time
	iface    string // интерфейс с default gateway
}

func newClientMetricsCollector() *clientMetricsCollector {
	c := &clientMetricsCollector{}
	c.iface = defaultGatewayIface()
	c.prevCPU, _ = readClientCPU()
	if c.iface != "" {
		c.prevNet, _ = readNetStats(c.iface)
	}
	c.prevTime = time.Now()
	return c
}

// collect обновляет текущие метрики. Вызывается каждые N секунд.
func (c *clientMetricsCollector) collect() ClientMetrics {
	now := time.Now()
	dt := now.Sub(c.prevTime).Seconds()
	if dt < 0.1 {
		dt = 1
	}

	// CPU
	cur, err := readClientCPU()
	cpu := 0.0
	if err == nil {
		dTotal := cur.total - c.prevCPU.total
		dIdle := cur.idle - c.prevCPU.idle
		if dTotal > 0 {
			cpu = 100.0 * float64(dTotal-dIdle) / float64(dTotal)
		}
		c.prevCPU = cur
	}

	// RAM
	mem := readClientMem()

	// Network
	var rxBps, txBps int64
	if c.iface != "" {
		ns, err := readNetStats(c.iface)
		if err == nil {
			rxBps = int64(float64(ns.rx-c.prevNet.rx) / dt)
			txBps = int64(float64(ns.tx-c.prevNet.tx) / dt)
			if rxBps < 0 {
				rxBps = 0
			}
			if txBps < 0 {
				txBps = 0
			}
			c.prevNet = ns
		}
	}
	c.prevTime = now

	m := ClientMetrics{
		CPU:      cpu,
		MemUsed:  mem,
		NetIface: c.iface,
		NetRxBps: rxBps,
		NetTxBps: txBps,
	}

	c.mu.Lock()
	c.current = m
	c.mu.Unlock()

	return m
}

func (c *clientMetricsCollector) get() ClientMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// ── /proc/net/route — ищем интерфейс default gateway ─────────────────────────

// defaultGatewayIface возвращает имя интерфейса, через который идёт маршрут по умолчанию.
// В /proc/net/route строка с Destination=00000000 — это default route.
func defaultGatewayIface() string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Scan() // skip header

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		// Destination == 00000000 → default route
		if fields[1] == "00000000" {
			return fields[0]
		}
	}
	return ""
}

// ── /proc/stat ────────────────────────────────────────────────────────────────

func readClientCPU() (cpuSnap, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuSnap{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		var v [8]int64
		for i := 0; i < len(fields) && i < 8; i++ {
			v[i], _ = strconv.ParseInt(fields[i], 10, 64)
		}
		idle := v[3] + v[4]
		total := v[0] + v[1] + v[2] + v[3] + v[4] + v[5] + v[6] + v[7]
		return cpuSnap{total: total, idle: idle}, nil
	}
	return cpuSnap{}, fmt.Errorf("cpu line not found")
}

// ── /proc/meminfo ─────────────────────────────────────────────────────────────

func readClientMem() float64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()

	var total, available int64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
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

// ── /proc/net/dev ─────────────────────────────────────────────────────────────

func readNetStats(iface string) (netSnap, error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return netSnap{}, err
	}
	defer f.Close()

	prefix := iface + ":"
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		// Формат: iface: rx_bytes ... tx_bytes ...
		// Поля: [0]=iface: [1]=rx_bytes [2..8]=rx other [9]=tx_bytes ...
		fields := strings.Fields(strings.TrimPrefix(line, prefix))
		if len(fields) < 9 {
			continue
		}
		rx, _ := strconv.ParseInt(fields[0], 10, 64)
		tx, _ := strconv.ParseInt(fields[8], 10, 64)
		return netSnap{rx: rx, tx: tx}, nil
	}
	return netSnap{}, fmt.Errorf("interface %s not found in /proc/net/dev", iface)
}
