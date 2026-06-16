package handlers

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// DeviceEntry represents a device discovered on the local network.
type DeviceEntry struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	MAC      string `json:"mac"`
}

// DevicesResponse wraps the list of discovered devices.
type DevicesResponse struct {
	Devices []Device `json:"devices"`
}

// Reuse the type from client — define locally
type Device = DeviceEntry

// Devices handles GET /devices
// Runs a quick ARP ping scan using the `arp` command available on Linux.
// For a proper network scan, consider integrating nmap or arp-scan.
func Devices(w http.ResponseWriter, r *http.Request) {
	devices := scanDevices()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"devices": devices})
}

// scanDevices reads the ARP cache from /proc/net/arp.
// This lists devices that have recently communicated on the local network.
func scanDevices() []DeviceEntry {
	// Read kernel ARP table
	out, err := exec.Command("cat", "/proc/net/arp").Output()
	if err != nil {
		return []DeviceEntry{}
	}

	var devices []DeviceEntry
	lines := strings.Split(string(out), "\n")
	// Skip header line
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		// ARP table: IP | HW type | Flags | MAC | Mask | Device
		if len(fields) < 4 {
			continue
		}
		ip := fields[0]
		mac := fields[3]
		// Skip incomplete entries (00:00:00:00:00:00)
		if mac == "00:00:00:00:00:00" || ip == "0.0.0.0" {
			continue
		}

		// Try reverse DNS lookup for hostname (best-effort, short timeout)
		hostname := reverseLookup(ip)

		devices = append(devices, DeviceEntry{
			IP:       ip,
			Hostname: hostname,
			MAC:      mac,
		})
	}
	return devices
}

// reverseLookup attempts a DNS reverse lookup within a short timeout.
func reverseLookup(ip string) string {
	done := make(chan string, 1)
	go func() {
		out, err := exec.Command("getent", "hosts", ip).Output()
		if err != nil || len(out) == 0 {
			done <- ""
			return
		}
		fields := strings.Fields(string(out))
		if len(fields) >= 2 {
			done <- fields[1]
		} else {
			done <- ""
		}
	}()
	select {
	case result := <-done:
		return result
	case <-time.After(500 * time.Millisecond):
		return ""
	}
}
