package handlers

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
)

// PCControl handles POST /pc/{action}
// Supported actions: sleep, shutdown, reboot
// These endpoints MUST be protected by API-key + IP whitelist middleware.
func PCControl(w http.ResponseWriter, r *http.Request) {
	// Extract action from URL path: /pc/{action}
	path := strings.TrimPrefix(r.URL.Path, "/pc/")
	action := strings.ToLower(strings.TrimSpace(path))

	var cmd *exec.Cmd
	switch action {
	case "sleep":
		// systemctl suspend works on most modern Linux desktops
		cmd = exec.Command("systemctl", "suspend")
	case "shutdown":
		cmd = exec.Command("systemctl", "poweroff")
	case "reboot":
		cmd = exec.Command("systemctl", "reboot")
	default:
		jsonResponseErr(w, "Unknown action: "+action, http.StatusBadRequest)
		return
	}

	if err := cmd.Start(); err != nil {
		jsonResponseErr(w, "Failed to execute: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"message": "PC " + action + " command sent successfully",
	})
}

func jsonResponseErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    false,
		"error": msg,
	})
}
