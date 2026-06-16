package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ScriptRunner handles POST /scripts/run
// Accepts: { "script": "backup" }
// Executes scripts from a whitelisted directory: $SCRIPTS_DIR (default: ./scripts)
// NEVER execute arbitrary user-supplied commands — only pre-approved script names.
func ScriptRunner(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Script string `json:"script"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Script == "" {
		jsonResponseErr(w, "Invalid request: 'script' field required", http.StatusBadRequest)
		return
	}

	// Sanitize: only allow alphanumeric, dash, underscore — no path traversal
	safe := sanitizeScriptName(req.Script)
	if safe == "" {
		jsonResponseErr(w, "Invalid script name", http.StatusBadRequest)
		return
	}

	// Resolve the scripts directory
	scriptsDir := os.Getenv("SCRIPTS_DIR")
	if scriptsDir == "" {
		scriptsDir = "./scripts"
	}

	scriptPath := filepath.Join(scriptsDir, safe)

	// Ensure the resolved path stays within scriptsDir (prevent path traversal)
	absScriptsDir, _ := filepath.Abs(scriptsDir)
	absScript, _ := filepath.Abs(scriptPath)
	if !strings.HasPrefix(absScript, absScriptsDir) {
		jsonResponseErr(w, "Forbidden: path traversal detected", http.StatusForbidden)
		return
	}

	// Check script exists and is executable
	info, err := os.Stat(absScript)
	if os.IsNotExist(err) || info.IsDir() {
		jsonResponseErr(w, "Script not found: "+safe, http.StatusNotFound)
		return
	}

	// Execute with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "/bin/bash", absScript)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\nSTDERR:\n" + stderr.String()
	}
	// Truncate very long output
	if len(output) > 4000 {
		output = output[:4000] + "\n...(truncated)"
	}

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":     false,
			"output": output,
			"error":  err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"output": output,
	})
}

// sanitizeScriptName strips any characters that are not alphanumeric, dash, or underscore.
func sanitizeScriptName(name string) string {
	var b strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' {
			b.WriteRune(c)
		}
	}
	return b.String()
}
