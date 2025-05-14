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

type HostStatus struct {
	Host       string  `json:"host"`
	Alive      bool    `json:"alive"`
	LatencyMs  int     `json:"latency_ms"`
	PacketLoss float64 `json:"packet_loss"`
}

type PingResult struct {
	Statuses []HostStatus `json:"statuses"`
	ShowLoss bool         `json:"show_loss"`
}

type HostStats struct {
	Sent int
	Recv int
}

var (
	hosts []string
	clientsMu sync.Mutex
	clients   = make(map[*websocket.Conn]bool)
	hostStatsMu sync.Mutex
	hostStats = make(map[string]*HostStats)
)

func readHosts(file string, cliHosts string) []string {
	result := []string{}
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			log.Fatalf("Failed to open hosts file: %v", err)
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
	return result
}

func pingHost(host string) (bool, int, float64) {
	pinger, err := ping.NewPinger(host)
	if err != nil {
		return false, 0, 100.0
	}
	pinger.Count = 4
	pinger.Timeout = 4 * time.Second
	pinger.SetPrivileged(true)
	err = pinger.Run()
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

func broadcast(result PingResult) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	data, _ := json.Marshal(result)
	for c := range clients {
		if err := websocket.Message.Send(c, string(data)); err != nil {
			c.Close()
			delete(clients, c)
		}
	}
}

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

func main() {
	file := flag.String("file", "", "File with hosts (one per line)")
	hostsArg := flag.String("hosts", "", "Comma-separated hosts")
	showLoss := flag.Bool("show-loss", false, "Show packet loss instead of latency on dashboard")
	flag.Parse()

	hosts = readHosts(*file, *hostsArg)
	if len(hosts) == 0 {
		log.Fatal("No hosts provided!")
	}

	http.Handle("/ws", websocket.Handler(wsHandler))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "dashboard.html")
	})

	go pingLoop(*showLoss)
	log.Println("Server running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
