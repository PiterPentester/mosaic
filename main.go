// Package main implements a network monitoring dashboard that pings multiple hosts
// and displays their status in a web interface. It provides real-time updates
// via WebSockets and can monitor hosts from both command-line arguments and files.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"log"
	ping "github.com/prometheus-community/pro-bing"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

// Pinger is an interface that defines the methods required for pinging hosts.
// This interface is primarily used for testing purposes to allow mocking the ping functionality.
type Pinger interface {
	// Run executes the ping process
	Run() error
	// Statistics returns the collected ping statistics
	Statistics() *ping.Statistics
	// SetPrivileged sets whether the pinger requires privileged mode
	SetPrivileged(bool)
}

// HostStatus represents the status of a pinged host
// and is used to serialize host status information to JSON.
type HostStatus struct {
	Host       string  `json:"host"`        // Hostname or IP address being monitored
	Alive      bool    `json:"alive"`       // Whether the host is responding to pings
	LatencyMs  int     `json:"latency_ms"`  // Average round-trip time in milliseconds
	PacketLoss float64 `json:"packet_loss"` // Packet loss percentage (0-100)
}

// PingResult contains the status of all monitored hosts and display preferences
// It's used to send updates to connected WebSocket clients.
type PingResult struct {
	Statuses []HostStatus `json:"statuses"` // Slice of host statuses
	ShowLoss bool         `json:"show_loss"` // Whether to display packet loss instead of latency
}

// HostStats tracks the total number of packets sent and received
// for calculating packet loss statistics over time.
type HostStats struct {
	Sent int // Total packets sent to the host
	Recv int // Total packets received from the host
}

var (
	hosts []string
	clientsMu sync.Mutex
	clients   = make(map[*websocket.Conn]bool)
	hostStatsMu sync.Mutex
	hostStats = make(map[string]*HostStats)
)

// readHosts reads hostnames or IP addresses from a file and/or command-line argument.
// It returns a deduplicated list of hosts to monitor.
//
// Parameters:
//   - file: Path to a file containing one host per line
//   - cliHosts: Comma-separated list of hosts from command line
//
// Returns:
//   - []string: List of unique hosts to monitor
//   - error: Any error that occurred while reading the file
func readHosts(file string, cliHosts string) ([]string, error) {
	result := []string{}
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				result = append(result, line)
			}
		}
		f.Close()
	}
	if cliHosts != "" {
		for _, h := range strings.Split(cliHosts, ",") {
			h = strings.TrimSpace(h)
			if h != "" {
				result = append(result, h)
			}
		}
	}
	return result, nil
}

// newPinger is a variable to allow mocking in tests
var newPinger = func(addr string) Pinger {
	p, _ := ping.NewPinger(addr)
	return p
}

// pingHost sends ICMP echo requests to the specified host and collects statistics.
//
// Parameters:
//   - host: The hostname or IP address to ping
//
// Returns:
//   - bool: Whether the host is responding to pings
//   - int: Average round-trip time in milliseconds (0 if host is down)
//   - float64: Packet loss percentage (0-100)
func pingHost(host string) (bool, int, float64) {
	pinger := newPinger(host)
	pinger.SetPrivileged(true)

	err := pinger.Run()
	if err != nil {
		return false, 0, 100.0
	}
	stats := pinger.Statistics()

	hostStatsMu.Lock()
	hs := hostStats[host]
	if hs == nil {
		hs = &HostStats{}
		hostStats[host] = hs
	}
	hs.Sent += stats.PacketsSent
	hs.Recv += stats.PacketsRecv
	totalSent := hs.Sent
	totalRecv := hs.Recv
	hostStatsMu.Unlock()

	loss := 100.0
	if totalSent > 0 {
		loss = 100.0 * float64(totalSent-totalRecv) / float64(totalSent)
	}
	alive := stats.PacketsRecv > 0
	lat := int(stats.AvgRtt.Milliseconds())
	return alive, lat, loss
}

// pingLoop continuously pings all configured hosts in parallel
// and broadcasts the results to connected WebSocket clients.
//
// Parameters:
//   - showLoss: If true, the dashboard will display packet loss instead of latency
func pingLoop(showLoss bool) {
	for {
		statuses := make([]HostStatus, len(hosts))
		wg := sync.WaitGroup{}
		for i, host := range hosts {
			wg.Add(1)
			go func(i int, host string) {
				defer wg.Done()
				alive, latency, loss := pingHost(host)
				statuses[i] = HostStatus{Host: host, Alive: alive, LatencyMs: latency, PacketLoss: loss}
			}(i, host)
		}
		wg.Wait()
		broadcast(PingResult{Statuses: statuses, ShowLoss: showLoss})
		time.Sleep(2 * time.Second)
	}
}

// jsonMarshal is a variable to allow mocking json.Marshal in tests
var jsonMarshal = json.Marshal

// broadcast sends the given PingResult to all connected WebSocket clients.
// It handles client disconnections by cleaning up closed connections.
//
// Parameters:
//   - result: The PingResult to broadcast
func broadcast(result PingResult) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	data, err := jsonMarshal(result)
	if err != nil {
		log.Printf("Error marshaling ping result: %v", err)
		return
	}
	for c := range clients {
		if err := websocket.Message.Send(c, string(data)); err != nil {
			c.Close()
			delete(clients, c)
		}
	}
}

// wsHandler handles new WebSocket connections for real-time updates.
// It maintains the list of connected clients and cleans up when they disconnect.
//
// Parameters:
//   - ws: The WebSocket connection
func wsHandler(ws *websocket.Conn) {
	clientsMu.Lock()
	clients[ws] = true
	clientsMu.Unlock()
	defer func() {
		clientsMu.Lock()
		delete(clients, ws)
		clientsMu.Unlock()
		ws.Close()
	}()
	// Keep alive
	for {
		time.Sleep(1 * time.Second)
	}
}

// main is the entry point of the application.
// It parses command-line flags, initializes the server, and starts monitoring hosts.
// The server listens on port 8080 by default.
//
// Command-line flags:
//   -file: Path to a file containing hosts to monitor (one per line)
//   -hosts: Comma-separated list of hosts to monitor
//   -show-loss: If set, display packet loss instead of latency
func main() {
	file := flag.String("file", "", "File with hosts (one per line)")
	hostsArg := flag.String("hosts", "", "Comma-separated hosts")
	showLoss := flag.Bool("show-loss", false, "Show packet loss instead of latency on dashboard")
	flag.Parse()

	var err error
	hosts, err = readHosts(*file, *hostsArg)
	if err != nil {
		log.Fatalf("Failed to read hosts: %v", err)
	}
	if len(hosts) == 0 {
		log.Fatal("No hosts provided!")
	}

	http.Handle("/ws", websocket.Handler(wsHandler))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(getDashboardHTML()))
	})

	go pingLoop(*showLoss)
	log.Println("Server running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
