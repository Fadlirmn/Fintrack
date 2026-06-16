package handlers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// StatusResponse contains basic server info.
type StatusResponse struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Uptime   string `json:"uptime"`
	LoadAvg  string `json:"load_avg"`
}

// startTime records when the server started (used to compute uptime).
var startTime = time.Now()

// Status handles GET /status
// Returns hostname, OS, uptime, and load average.
func Status(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()

	uptime := time.Since(startTime).Round(time.Second).String()

	loadAvg := readLoadAvg()

	resp := StatusResponse{
		Hostname: hostname,
		OS:       runtime.GOOS + "/" + runtime.GOARCH,
		Uptime:   uptime,
		LoadAvg:  loadAvg,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// readLoadAvg reads the 1/5/15 minute load average from /proc/loadavg (Linux only).
func readLoadAvg() string {
	f, err := os.Open("/proc/loadavg")
	if err != nil {
		return "n/a"
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 3 {
			return fmt.Sprintf("%s %s %s", fields[0], fields[1], fields[2])
		}
	}
	return "n/a"
}
