package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type AgentInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Found        bool   `json:"found"`
	Version      string `json:"version"`
	Path         string `json:"path"`
	Platform     string `json:"platform"`
	Integration  string `json:"integration"`
	Instructions string `json:"instructions"`
	Limitations  string `json:"limitations"`
}

func AgentID(s string) string {
	if s == "" {
		return "pi"
	}
	return s
}
func AgentName(s string) string {
	return map[string]string{"pi": "Pi", "claude": "Claude Code", "codex": "Codex", "opencode": "OpenCode", "grok": "Grok Build"}[AgentID(s)]
}
func DetectAgents() []AgentInfo {
	out := []AgentInfo{}
	for _, id := range []string{"pi", "claude", "codex", "opencode", "grok"} {
		a := AgentInfo{ID: id, Name: AgentName(id), Platform: runtime.GOOS, Integration: "command", Instructions: "Restart the client after installing and check in the client that the hook is loaded.", Limitations: "Covers only the tool events the client provides; if the hook command cannot start, is disabled or is killed by a client timeout, the client may let the call through."}
		switch id {
		case "pi":
			a.Integration = "extension"
			a.Instructions = "Run /reload when Pi is idle, or restart it, and wait for a real heartbeat."
			a.Limitations = "Pi 0.85.1; does not cover user ! commands or execution inside other extensions. A child process must load this extension itself; the parent's hook does not automatically cover tools inside a child process. Windows needs Git Bash; symlinks need developer mode or equivalent permission."
		case "claude":
			a.Instructions = "Restart Claude Code and check the config with /hooks; the client's own permission checks are kept."
			a.Limitations += " Sub-agent identity is not shown yet, so sub-agent tool events may be grouped under the parent session; no separate sub-session in the list does not mean unreviewed."
		case "codex":
			a.Instructions = "Restart the Codex CLI or desktop app you use, separately, and verify the connection; in the CLI, review and trust the new hook with /hooks, and the project itself must also be trusted."
			a.Limitations += " The version detected here is the CLI on PATH, not the version bundled with the desktop app; check compatibility and events for each separately, and verifying one does not verify both. Managed tools and later write_stdin input do not trigger a new pre-tool review. Sub-agent hooks use the parent session ID, and sub-agent identity is not shown yet."
		case "opencode":
			a.Integration = "plugin"
			a.Instructions = "Restart OpenCode; the plugin registers on a session event or the first tool call."
			a.Limitations = "Does not cover the plugin's own execution; the execution result is updated only when the client explicitly reports completion or failure. Sub-sessions are recorded under their own sessionID, and the parent-child relationship is not shown yet."
		case "grok":
			a.Instructions = "Restart Grok Build; for a project install, run /hooks-trust in the client and check with /hooks."
			a.Limitations += " Without a native call ID, only the pre-execution decision is recorded and the execution result is not guessed; one data directory allows only one Grok install scope. Sub-sessions can be recorded separately, and the parent-child relationship is not shown yet; running or thinking does not mean a tool call happened."
		}
		if p, err := exec.LookPath(id); err == nil {
			a.Path = p
			a.Found = true
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			b, err := exec.CommandContext(ctx, p, "--version").Output()
			cancel()
			if err == nil {
				a.Version = strings.TrimSpace(string(b))
			}
		}
		out = append(out, a)
	}
	return out
}
func agentHome(agent string) string {
	home, _ := os.UserHomeDir()
	switch agent {
	case "claude":
		if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
			return v
		}
		return filepath.Join(home, ".claude")
	case "codex":
		if v := os.Getenv("CODEX_HOME"); v != "" {
			return v
		}
		return filepath.Join(home, ".codex")
	case "grok":
		return filepath.Join(home, ".grok")
	case "opencode":
		if v := os.Getenv("OPENCODE_CONFIG_DIR"); v != "" {
			return v
		}
		if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
			return filepath.Join(v, "opencode")
		}
		return filepath.Join(home, ".config", "opencode")
	}
	return AgentDir()
}
