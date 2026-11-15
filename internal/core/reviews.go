package core

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (e *Engine) Register(i Instance) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if i.NodeID == "" {
		if real, err := filepath.EvalSymlinks(i.Cwd); err == nil {
			i.Cwd = real
		}
	}
	if i.NodeID != "" {
		id, err := hex.DecodeString(i.NodeID)
		if err != nil || len(id) != 16 || !hasExact(i.Platform, "linux", "darwin", "windows") || !agentPathAbs(i.Cwd, i.Platform) {
			return errors.New("remote node identity, platform or project path is invalid")
		}
		i.Installations = nil
	}
	i.Agent = AgentID(i.Agent)
	if AgentName(i.Agent) == "" {
		return errors.New("unknown agent")
	}
	if i.ID == "" || i.SessionID == "" || (i.NodeID == "" && !filepath.IsAbs(i.Cwd)) || i.HookVersion != Version {
		return errors.New("session fields or hook version are invalid")
	}
	// Keep historical validation separate from bindings for this registration.
	// Ignore caller-supplied history; only successful registrations establish it.
	i.ObservedInstallations = nil
	var prev Instance
	if e.get("instances", i.ID, &prev) == nil {
		if prev.SessionID != i.SessionID || prev.Cwd != i.Cwd || AgentID(prev.Agent) != i.Agent || prev.NodeID != i.NodeID || (i.NodeID != "" && prev.Platform != i.Platform) {
			return errors.New("instance identity conflict")
		}
		bindings := i.Installations
		i = prev
		for _, id := range prev.Installations {
			if !hasExact(id, i.ObservedInstallations...) {
				i.ObservedInstallations = append(i.ObservedInstallations, id)
			}
		}
		i.Installations = bindings
	}
	for _, id := range i.Installations {
		if !hasExact(id, i.ObservedInstallations...) {
			i.ObservedInstallations = append(i.ObservedInstallations, id)
		}
	}
	i.StartedAt = e.clock()
	i.Heartbeat = e.clock()
	i.State = "idle"
	i.DisconnectReason = ""
	i.Online = true
	return e.change("instances", i.ID, i, "instance.register", AgentName(i.Agent)+" session events connected")
}
func (e *Engine) Heartbeat(id, state string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var i Instance
	if err := e.get("instances", id, &i); err != nil {
		return err
	}
	if i.State == "disconnected" {
		return errors.New("instance was unregistered; register again")
	}
	i.Heartbeat = e.clock()
	if hasExact(state, "idle", "running", "awaiting_review") {
		i.State = state
	}
	return e.put("instances", id, i)
}
func (e *Engine) Disconnect(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var i Instance
	if err := e.get("instances", id, &i); err != nil {
		return err
	}
	i.State = "disconnected"
	i.DisconnectReason = "session_end"
	i.Online = false
	if err := e.change("instances", id, i, "instance.disconnect", AgentName(i.Agent)+" session disconnected"); err != nil {
		return err
	}
	rs, err := e.Reviews()
	if err != nil {
		return err
	}
	for _, r := range rs {
		if r.InstanceID == id && r.Decision == "pending" {
			r.Decision = "reject"
			r.NeedsHuman = false
			r.Execution = "not_executed"
			r.RuleID = "E_SESSION"
			r.Comment = "The original session ended and this review was cancelled."
			now := e.clock()
			r.DecidedAt = &now
			if err = e.commitReview(r, "review.cancel"); err != nil {
				return err
			}
		}
	}
	return nil
}
func (e *Engine) Submit(in ReviewInput) (Review, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var i Instance
	if err := e.get("instances", in.InstanceID, &i); err != nil {
		return Review{}, errors.New("agent instance is not registered yet")
	}
	if i.State == "disconnected" || e.clock().Sub(i.Heartbeat) > 30*time.Second {
		return Review{}, errors.New("agent instance lost contact")
	}
	if in.CallID == "" || in.ToolName == "" || in.Arguments == nil {
		return Review{}, errors.New("review parameters are missing")
	}
	b, err := json.Marshal(struct {
		Tool    string
		Args    map[string]any
		Scripts string
	}{in.ToolName, in.Arguments, scriptDigest(in.ScriptCaptureStatus, in.Scripts)})
	if err != nil {
		return Review{}, err
	}
	digest := sha256.Sum256(b)
	key := sha256.Sum256([]byte(in.InstanceID + "\x00" + in.CallID))
	id := hex.EncodeToString(key[:])
	var existing Review
	if err = e.get("reviews", id, &existing); err == nil {
		if existing.Digest != hex.EncodeToString(digest[:]) {
			return Review{}, errors.New("the tool call ID was already used with different arguments")
		}
		return normalizeReviewPath(existing), nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Review{}, err
	}
	rules, err := e.Rules()
	if err != nil {
		return Review{}, err
	}
	scopes, err := e.Scopes()
	if err != nil {
		return Review{}, err
	}
	applicable := []Scope{}
	for _, s := range scopes {
		if s.NodeID == i.NodeID {
			applicable = append(applicable, s)
		}
	}
	resolve, contains := policyPath, within
	if i.NodeID != "" {
		resolve = func(p, cwd string) string { return agentPath(p, cwd, i.Platform) }
		contains = func(root, p string) bool { return agentWithin(root, p, i.Platform) }
	}
	// Every adapter shares this boundary. Resolve deterministic policy against
	// the original arguments before redaction or either fallback reviewer.
	path := "rule"
	d := e.selfProtect(in, i.Cwd, resolve, i.NodeID == "")
	if d == nil {
		path = "scope"
		d = checkScopePaths(in, i.Cwd, applicable, resolve, contains)
		if d == nil {
			path = "rule"
			d = evaluatePaths(in, rules, i.Cwd, resolve)
		}
	}
	// The digest binds the approval to the original arguments, so the reviewer
	// must be shown all of them. Arguments are redacted but never truncated, and
	// the number of masked spans is recorded so a hidden span cannot be approved
	// without a warning or by a model.
	var stats redactStats
	redacted := redactArgumentsStats(in.Arguments, &stats)
	hidden := stats.hidden
	// A span that holds shell syntax in a command that will be executed is not a
	// literal secret: masking it would hide executable code from the human, so the
	// call is refused outright. In any other tool (file writes, edits, patches) the
	// text is data, so the call only goes to a human with the usual warning.
	if commandShellSpans(in) > 0 && (d == nil || d.Decision == "approve") {
		path = "rule"
		d = &Decision{Decision: "reject", RuleID: secretSyntaxRule, Comment: "Actual operation: pass a value that contains shell syntax where a secret is expected; Consequence if successful: the masked text could run arbitrary commands that no reviewer can see; Matched rule: " + secretSyntaxRule + " (fixed). Do not put commands, substitutions or redirections in a secret argument; pass the secret through an environment variable or a file instead."}
	}
	reason := humanOnlyReason(in.ScriptCaptureStatus, hidden)
	forcedHuman := false
	if d != nil && d.Decision == "approve" && reason != "" {
		forcedHuman = true
		d = nil
	}
	s := e.Settings
	in.Arguments = redacted.(map[string]any)
	in.UserMessage = RedactText(in.UserMessage)
	in.Context = RedactText(in.Context)
	r := Review{Agent: AgentID(i.Agent), ID: id, ReviewInput: in, SessionID: i.SessionID, Cwd: i.Cwd, Digest: hex.EncodeToString(digest[:]), Mode: s.Mode, Version: s.Version, Prompt: s.Prompt, ModelName: s.Model.Model, RedactedSpans: hidden, Rules: rules, Scopes: applicable, Decision: "pending", Execution: "not_executed", CreatedAt: e.clock(), Deadline: e.clock().Add(time.Duration(s.ApprovalSeconds) * time.Second)}
	if d != nil {
		r.ReviewPath = path
		r.Decision = d.Decision
		r.RuleID = d.RuleID
		r.Comment = RedactText(d.Comment)
		now := e.clock()
		r.DecidedAt = &now
		if d.Decision == "approve" {
			r.Execution = "awaiting_execution"
		}
		// A rule hit is terminal: no human queue, model request or model usage.
		return r, e.commitReview(r, "review.submit")
	}
	r.ReviewPath = s.Mode
	// Content the reviewer could not see is never decided by a model.
	forcedHuman = forcedHuman || hidden > 0
	r.NeedsHuman = s.Mode == "human" || forcedHuman
	if forcedHuman {
		r.ReviewPath = "human"
		r.Comment = reason
	}
	if s.Mode == "model" && !forcedHuman {
		s.Prompt = modelPrompt(s.Prompt)
		r.Prompt = s.Prompt
		// Explicitly record that no historical context was sent for this review.
		emptyContext := ""
		r.ModelContext = &emptyContext
		modelBytes, _ := json.Marshal(modelInput(r))
		r.ModelInput = modelBytes
		modelHash := sha256.Sum256(modelBytes)
		r.ModelInputDigest = hex.EncodeToString(modelHash[:])
		r.Deadline = e.clock().Add(time.Duration(s.ModelSeconds+5) * time.Second)
	}
	if err = e.commitReview(r, "review.submit"); err != nil {
		return Review{}, err
	}
	if r.ReviewPath == "model" {
		go e.modelReview(r, s)
	}
	return r, nil
}

// humanOnlyReason explains why a call must be confirmed by a human instead of
// being approved by a rule or a model; it is empty when nothing prevents that.
// secretSyntaxRule rejects calls whose redacted span contains shell syntax.
const secretSyntaxRule = "SECRET_SYNTAX"

func humanOnlyReason(captureStatus string, hidden int) string {
	reason := ""
	if hidden > 0 {
		reason = fmt.Sprintf("%d span(s) of this call were hidden by secret redaction, so the reviewer cannot see everything an approval would cover. Human confirmation is required.", hidden)
	}
	if captureStatus == "incomplete" {
		if reason != "" {
			reason += " "
		}
		reason += "The script could not be captured in full, so hidden side effects cannot be ruled out. Human confirmation is required."
	}
	return reason
}

// Infer legacy paths from the saved verdict and rule snapshot, never today's
// rules or mode. Model verdict IDs can overlap builtin IDs (for example R1).
func normalizeReviewPath(r Review) Review {
	if r.ReviewPath != "" {
		return r
	}
	switch {
	case r.RuleID == "SCOPE_PATH" || r.RuleID == "SCOPE_TARGET":
		r.ReviewPath = "scope"
	case r.RuleID == "HUMAN" || r.NeedsHuman:
		r.ReviewPath = "human"
	case r.ModelVerdict != nil:
		r.ReviewPath = "model"
	default:
		r.ReviewPath = r.Mode
		for _, rule := range r.Rules {
			if rule.ID == r.RuleID && rule.Enabled && rule.Decision == r.Decision {
				r.ReviewPath = "rule"
				break
			}
		}
	}
	return r
}
func (e *Engine) Decide(id, digest, decision, comment string) (Review, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.Review(id)
	if err != nil {
		return r, err
	}
	if r.Digest != digest {
		return r, errors.New("argument digest does not match")
	}
	if !hasExact(decision, "approve", "reject") {
		return r, errors.New("invalid decision")
	}
	if r.Mode != "human" && r.ReviewPath != "human" {
		return r, errors.New("this call is not under human approval")
	}
	if r.Decision != "pending" {
		if r.Decision == decision && r.RuleID == "HUMAN" {
			return r, nil
		}
		return r, errors.New("this call was already decided or is no longer valid")
	}
	if e.clock().After(r.Deadline) {
		return r, errors.New("approval has expired")
	}
	if decision == "reject" && comment == "" {
		return r, errors.New("enter a rejection reason")
	}
	r.Decision = decision
	r.NeedsHuman = false
	r.RuleID = "HUMAN"
	if comment == "" {
		comment = "Human approved this tool call"
	}
	r.Comment = RedactText(comment)
	now := e.clock()
	r.DecidedAt = &now
	if decision == "approve" {
		r.Execution = "awaiting_execution"
	}
	err = e.commitReview(r, "review.human")
	return r, err
}
func (e *Engine) Result(id, instance, state, result string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.Review(id)
	if err != nil {
		return err
	}
	if r.InstanceID != instance {
		return errors.New("instance does not match")
	}
	if r.Decision == "pending" {
		return errors.New("no decision yet")
	}
	if r.Decision == "reject" {
		if state != "not_executed" {
			return errors.New("a rejected call cannot report successful execution")
		}
		return nil
	}
	if !hasExact(state, "succeeded", "failed", "not_executed") {
		return errors.New("invalid execution result")
	}
	if r.Execution != "awaiting_execution" {
		if r.Execution == state {
			return nil
		}
		return errors.New("execution result is already final")
	}
	r.Execution = state
	r.Result = RedactText(result)
	return e.commitReview(r, "review.result")
}
func (e *Engine) LocalDiagnostic() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	return map[string]any{"version": Version, "mode": e.Settings.Mode, "dataDir": e.Dir, "modelConfigured": e.Settings.Model.Tested, "toolBoundary": "Each agent's own tool-call hook (Claude/Codex PreToolUse, Grok and OpenCode hooks, Pi tool_call); excludes sub-calls inside scripts and, for Pi, pi.exec from other extensions and user ! commands"}
}
func (e *Engine) ExportCalls() ([]byte, error) {
	rs, err := e.Reviews()
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(rs, "", "  ")
}
func (e *Engine) Secret(name string) (string, error) {
	path := filepath.Join(e.Dir, name+".token")
	b, err := os.ReadFile(path)
	if err == nil {
		return string(b), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	token := ID() + ID()
	if err = AtomicFile(path, []byte(token), 0600); err != nil {
		return "", fmt.Errorf("failed to save token: %w", err)
	}
	return token, nil
}
