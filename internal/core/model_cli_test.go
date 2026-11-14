package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// codexEvents is the --json stream of a run in which the reviewer only wrote messages.
const codexEvents = `{"type":"thread.started","thread_id":"t"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"thinking"}}
{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"{}"}}
{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}
`

// These fixtures never start a real CLI; the runner is replaced per test.
func stubModelCLI(t *testing.T, fn func(cliRun) ([]byte, error)) *[]cliRun {
	t.Helper()
	original := runModelCLI
	t.Cleanup(func() { runModelCLI = original })
	calls := &[]cliRun{}
	runModelCLI = func(_ context.Context, r cliRun) ([]byte, error) {
		*calls = append(*calls, r)
		return fn(r)
	}
	return calls
}

func cliSettings(protocol, model string) Settings {
	return Settings{Mode: "model", Prompt: DefaultPrompt, ModelSeconds: 60, Model: ModelConfig{Protocol: protocol, Model: model}}
}

func claudeReply(t *testing.T, result string, isError bool) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"type": "result", "is_error": isError, "result": result,
		"usage": map[string]int{"input_tokens": 80, "output_tokens": 5, "cache_read_input_tokens": 20, "cache_creation_input_tokens": 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func argAfter(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestClaudeCLIReview(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "must-not-reach-the-cli")
	var systemPrompt string
	calls := stubModelCLI(t, func(r cliRun) ([]byte, error) {
		if path, ok := argAfter(r.args, "--system-prompt-file"); ok {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			systemPrompt = string(data)
		}
		return claudeReply(t, unknownApprove, false), nil
	})
	var usage *TokenUsage
	d, err := callModelWithUsage(context.Background(), cliSettings("claude-cli", "sonnet"), map[string]any{"toolName": "bash", "argumentsObj": map[string]any{"command": "./deploy.sh"}}, &usage)
	if err != nil || d.Decision != "approve" || d.RuleID != "D1" {
		t.Fatalf("decision %+v err %v", d, err)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls %d", len(*calls))
	}
	r := (*calls)[0]
	if r.name != "claude" || r.dir == "" {
		t.Fatalf("run %+v", r)
	}
	// The reviewer must run without tools, hooks or user customizations.
	for _, want := range []string{"-p", "--safe-mode", "--strict-mcp-config", "--disable-slash-commands", "--no-session-persistence"} {
		if !hasArg(r.args, want) {
			t.Fatalf("missing %s in %v", want, r.args)
		}
	}
	if tools, ok := argAfter(r.args, "--tools"); !ok || tools != "" {
		t.Fatalf("tools not disabled: %v", r.args)
	}
	if format, _ := argAfter(r.args, "--output-format"); format != "json" {
		t.Fatalf("output format %q", format)
	}
	if model, _ := argAfter(r.args, "--model"); model != "sonnet" {
		t.Fatalf("model %q", model)
	}
	if systemPrompt != DefaultPrompt {
		t.Fatal("system prompt file does not hold the review prompt")
	}
	// Review data travels on stdin, never in argv.
	if !strings.Contains(r.stdin, "./deploy.sh") || strings.Contains(strings.Join(r.args, " "), "deploy.sh") {
		t.Fatalf("stdin %q args %v", r.stdin, r.args)
	}
	for _, kv := range r.env {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			t.Fatal("API key leaked into the subscription CLI environment")
		}
	}
	if usage == nil || usage.Input != 80 || usage.Output != 5 || usage.CacheRead != 20 || usage.CacheWrite != 10 {
		t.Fatalf("usage %+v", usage)
	}
	if _, err := os.Stat(r.dir); !os.IsNotExist(err) {
		t.Fatal("temporary review directory was not removed")
	}
}

func TestClaudeCLIOmitsEmptyModelAndReportsErrors(t *testing.T) {
	calls := stubModelCLI(t, func(cliRun) ([]byte, error) {
		return claudeReply(t, "Failed to authenticate: OAuth session expired", true), nil
	})
	var usage *TokenUsage
	_, err := callModelWithUsage(context.Background(), cliSettings("claude-cli", ""), map[string]any{"toolName": "read"}, &usage)
	if err == nil || !strings.Contains(err.Error(), "Claude Code CLI") || !strings.Contains(err.Error(), "Failed to authenticate") {
		t.Fatalf("err %v", err)
	}
	if len(*calls) != 1 || hasArg((*calls)[0].args, "--model") {
		t.Fatalf("calls %+v", *calls)
	}

	stubModelCLI(t, func(cliRun) ([]byte, error) { return []byte("not json"), nil })
	if _, err = callModelWithUsage(context.Background(), cliSettings("claude-cli", ""), map[string]any{"toolName": "read"}, &usage); err == nil || !strings.Contains(err.Error(), "response format is invalid") {
		t.Fatalf("err %v", err)
	}

	stubModelCLI(t, func(cliRun) ([]byte, error) { return nil, errors.New("exit status 1") })
	if _, err = callModelWithUsage(context.Background(), cliSettings("claude-cli", ""), map[string]any{"toolName": "read"}, &usage); err == nil || !strings.Contains(err.Error(), "Claude Code CLI") {
		t.Fatalf("err %v", err)
	}
}

func TestCodexCLIReview(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "must-not-reach-the-cli")
	t.Setenv("AEGIS_TEST_UNRELATED_SECRET", "must-not-reach-the-cli")
	t.Setenv("CODEX_HOME", t.TempDir())
	var schema string
	calls := stubModelCLI(t, func(r cliRun) ([]byte, error) {
		schemaPath, _ := argAfter(r.args, "--output-schema")
		data, err := os.ReadFile(schemaPath)
		if err != nil {
			t.Fatal(err)
		}
		schema = string(data)
		out, ok := argAfter(r.args, "-o")
		if !ok {
			t.Fatal("no output file argument")
		}
		return []byte(codexEvents), os.WriteFile(out, []byte(unknownApprove), 0600)
	})
	var usage *TokenUsage
	d, err := callModelWithUsage(context.Background(), cliSettings("codex-cli", "gpt-5.2"), map[string]any{"toolName": "bash", "argumentsObj": map[string]any{"command": "./deploy.sh"}}, &usage)
	if err != nil || d.Decision != "approve" {
		t.Fatalf("decision %+v err %v", d, err)
	}
	r := (*calls)[0]
	if r.name != "codex" || len(r.args) == 0 || r.args[0] != "exec" || r.args[len(r.args)-1] != "-" {
		t.Fatalf("run %+v", r)
	}
	// Hooks must be off: AegisHook's own Codex hook would otherwise review the reviewer.
	for _, want := range []string{"--skip-git-repo-check", "--ephemeral", "--ignore-user-config", "--ignore-rules"} {
		if !hasArg(r.args, want) {
			t.Fatalf("missing %s in %v", want, r.args)
		}
	}
	disabled := map[string]bool{}
	for i, a := range r.args {
		if a == "--disable" && i+1 < len(r.args) {
			disabled[r.args[i+1]] = true
		}
	}
	// The reviewer must have no tool that could act on, or read beyond, the review input.
	for _, want := range []string{"hooks", "shell_tool", "unified_exec", "view_image", "apps", "plugins", "multi_agent"} {
		if !disabled[want] {
			t.Fatalf("%s not disabled: %v", want, r.args)
		}
	}
	if !hasArg(r.args, "--json") {
		t.Fatalf("event stream not requested: %v", r.args)
	}
	if v, _ := argAfter(r.args, "--sandbox"); v != "read-only" {
		t.Fatalf("sandbox %q", v)
	}
	if v, _ := argAfter(r.args, "-m"); v != "gpt-5.2" {
		t.Fatalf("model %q", v)
	}
	if v, _ := argAfter(r.args, "-C"); v != r.dir || filepath.Dir(schemaPathOf(r.args)) != r.dir {
		t.Fatalf("working directory %q vs %q", v, r.dir)
	}
	if !strings.Contains(schema, `"additionalProperties":false`) || !strings.Contains(schema, `"decision"`) {
		t.Fatalf("schema %s", schema)
	}
	if !strings.Contains(r.stdin, DefaultPrompt) || !strings.Contains(r.stdin, "./deploy.sh") {
		t.Fatal("codex prompt must carry the review policy and the review input on stdin")
	}
	codexHome := false
	for _, kv := range r.env {
		if strings.HasPrefix(kv, "OPENAI_API_KEY=") || strings.HasPrefix(kv, "AEGIS_TEST_UNRELATED_SECRET=") {
			t.Fatalf("%s leaked into the Codex environment", kv[:strings.Index(kv, "=")])
		}
		codexHome = codexHome || strings.HasPrefix(kv, "CODEX_HOME=")
	}
	if !codexHome {
		t.Fatal("CODEX_HOME must reach Codex so it can find its sign-in")
	}
	if usage != nil {
		t.Fatalf("codex usage is unknown and must not be reported: %+v", usage)
	}
}

func schemaPathOf(args []string) string {
	v, _ := argAfter(args, "--output-schema")
	return v
}

func TestCodexCLIMissingOutput(t *testing.T) {
	stubModelCLI(t, func(cliRun) ([]byte, error) { return []byte(codexEvents), nil })
	var usage *TokenUsage
	_, err := callModelWithUsage(context.Background(), cliSettings("codex-cli", ""), map[string]any{"toolName": "read"}, &usage)
	if err == nil || !strings.Contains(err.Error(), "did not return a decision") {
		t.Fatalf("err %v", err)
	}
}

func codexStub(t *testing.T, events, decision string, runErr error) {
	t.Helper()
	stubModelCLI(t, func(r cliRun) ([]byte, error) {
		out, _ := argAfter(r.args, "-o")
		if err := os.WriteFile(out, []byte(decision), 0600); err != nil {
			t.Fatal(err)
		}
		return []byte(events), runErr
	})
}

// A reviewer that used a tool may have been steered by the data under review.
// Its decision, even a well-formed approval, must never be used.
func TestCodexCLIRejectsToolUse(t *testing.T) {
	input := map[string]any{"toolName": "read"}
	var usage *TokenUsage
	for _, item := range []string{"command_execution", "mcp_tool_call", "web_search", "file_change", "something_new"} {
		events := codexEvents + `{"type":"item.completed","item":{"id":"item_2","type":"` + item + `","command":"type admin.token"}}` + "\n"
		codexStub(t, events, unknownApprove, nil)
		if _, err := callModelWithUsage(context.Background(), cliSettings("codex-cli", ""), input, &usage); err == nil || !strings.Contains(err.Error(), "tried to use a tool") {
			t.Fatalf("%s: err %v", item, err)
		}
	}
	for name, events := range map[string]string{
		"empty stream":     "",
		"not JSONL":        "plain text\n",
		"truncated stream": codexEvents + `{"type":"item.completed","item":{"type":"comm`,
		"oversized stream": codexEvents + strings.Repeat(" ", cliOutputLimit),
	} {
		codexStub(t, events, unknownApprove, nil)
		if _, err := callModelWithUsage(context.Background(), cliSettings("codex-cli", ""), input, &usage); err == nil || !strings.Contains(err.Error(), "Codex CLI review rejected") {
			t.Fatalf("%s: err %v", name, err)
		}
	}
	codexStub(t, codexEvents, `{"decision":"approve","comment":"`+strings.Repeat("A", codexDecisionLimit)+`"}`, nil)
	if _, err := callModelWithUsage(context.Background(), cliSettings("codex-cli", ""), input, &usage); err == nil || !strings.Contains(err.Error(), "did not return a decision") {
		t.Fatalf("oversized decision: err %v", err)
	}
}

// A failed or timed-out CLI run must end in an error (a reject), whatever it printed.
func TestCLIFailedRunNeverApproves(t *testing.T) {
	input := map[string]any{"toolName": "read"}
	var usage *TokenUsage
	for _, runErr := range []error{errors.New("exit status 1"), errors.New("timed out")} {
		stubModelCLI(t, func(cliRun) ([]byte, error) { return claudeReply(t, unknownApprove, false), runErr })
		if _, err := callModelWithUsage(context.Background(), cliSettings("claude-cli", ""), input, &usage); err == nil || !strings.Contains(err.Error(), runErr.Error()) {
			t.Fatalf("claude: err %v", err)
		}
		codexStub(t, codexEvents, unknownApprove, runErr)
		if _, err := callModelWithUsage(context.Background(), cliSettings("codex-cli", ""), input, &usage); err == nil || !strings.Contains(err.Error(), runErr.Error()) {
			t.Fatalf("codex: err %v", err)
		}
	}
}

func TestCodexFailureReason(t *testing.T) {
	events := `{"type":"turn.started"}
{"type":"item.completed","item":{"type":"agent_message","text":"secret review data"}}
{"type":"error","message":"Your access token could not be refreshed."}
{"type":"turn.failed","error":{"message":"Your access token could not be refreshed. Please sign in again."}}
`
	codexStub(t, events, "", errors.New("exit status 1"))
	var usage *TokenUsage
	_, err := callModelWithUsage(context.Background(), cliSettings("codex-cli", ""), map[string]any{"toolName": "read"}, &usage)
	if err == nil || err.Error() != "Codex CLI failed: Your access token could not be refreshed. Please sign in again." {
		t.Fatalf("err %v", err)
	}
	if got := codexFailure([]byte(events), "timed out"); got != "timed out" {
		t.Fatalf("got %q", got)
	}
	if got := codexFailure([]byte("not json"), "exit status 1"); got != "exit status 1" {
		t.Fatalf("got %q", got)
	}
}

func TestCLIEnvFilters(t *testing.T) {
	for _, name := range []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "anthropic_api_key", "CODEX_API_KEY"} {
		t.Setenv(name, "x")
	}
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "subscription")
	kept := false
	for _, kv := range cliEnv("ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL") {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(strings.ToUpper(name), "ANTHROPIC_") {
			t.Fatalf("%s reached the Claude Code CLI", name)
		}
		kept = kept || name == "CLAUDE_CODE_OAUTH_TOKEN"
	}
	if !kept {
		t.Fatal("the subscription token must stay in the Claude Code environment")
	}
	for _, kv := range cliEnvOnly(codexEnv...) {
		name, _, _ := strings.Cut(kv, "=")
		if !hasExact(strings.ToUpper(name), codexEnv...) {
			t.Fatalf("%s reached the Codex CLI", name)
		}
	}
}

// TestCLIHelperProcess is not a test: execModelCLI runs this binary as a stand-in CLI.
func TestCLIHelperProcess(t *testing.T) {
	switch os.Getenv("AEGIS_CLI_HELPER") {
	case "hang":
		os.Stdout.WriteString("started")
		time.Sleep(time.Minute)
	case "fail":
		os.Stdout.WriteString(`{"partial":true}`)
		os.Stderr.WriteString("user\nsecret review data\nERROR: sign-in expired\n")
		os.Exit(3)
	case "flood":
		os.Stdout.WriteString(strings.Repeat("x", cliOutputLimit+4096))
	}
}

func TestExecModelCLI(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	run := func(mode string, timeout time.Duration) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return execModelCLI(ctx, cliRun{name: self, args: []string{"-test.run=^TestCLIHelperProcess$"}, dir: t.TempDir(), env: append(os.Environ(), "AEGIS_CLI_HELPER="+mode)})
	}
	start := time.Now()
	if _, err = run("hang", 500*time.Millisecond); err == nil || err.Error() != "timed out" {
		t.Fatalf("hang: err %v", err)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Fatalf("timeout did not stop the CLI: %s", elapsed)
	}
	// Stdout survives a failure (Claude Code reports errors there); stderr yields only the marked error line.
	out, err := run("fail", 30*time.Second)
	if err == nil || err.Error() != "sign-in expired" || string(out) != `{"partial":true}` {
		t.Fatalf("fail: out %q err %v", out, err)
	}
	if out, err = run("flood", 30*time.Second); err != nil || len(out) != cliOutputLimit {
		t.Fatalf("flood: len %d err %v", len(out), err)
	}
}

func TestCLIModelJSONRetryCarriesCorrection(t *testing.T) {
	replies := []string{"```json\n" + unknownApprove + "\n```", unknownApprove}
	calls := stubModelCLI(t, func(cliRun) ([]byte, error) {
		reply := replies[0]
		replies = replies[1:]
		return claudeReply(t, reply, false), nil
	})
	var usage *TokenUsage
	d, err := callModelWithUsage(context.Background(), cliSettings("claude-cli", ""), map[string]any{"toolName": "read"}, &usage)
	if err != nil || d.Decision != "approve" || len(*calls) != 2 {
		t.Fatalf("decision %+v err %v calls %d", d, err, len(*calls))
	}
	second := (*calls)[1].stdin
	if !strings.Contains(second, `"toolName":"read"`) || !strings.Contains(second, "```json") || !strings.Contains(second, "failed JSON format validation") {
		t.Fatalf("retry prompt lost context: %q", second)
	}
	if usage == nil || usage.Input != 160 {
		t.Fatalf("usage %+v", usage)
	}
}

func TestCLIModelSettings(t *testing.T) {
	e := engine(t)
	s := e.Config()
	s.Model = ModelConfig{Protocol: "claude-cli", BaseURL: "https://ignored.example/v1", Model: "claude-sonnet-4-5[1m]"}
	if err := e.SaveSettings(s, nil); err != nil {
		t.Fatal(err)
	}
	if got := e.Config().Model; got.Protocol != "claude-cli" || got.BaseURL != "" || got.Tested {
		t.Fatalf("saved %+v", got)
	}
	// The model name becomes a CLI argument, so it must not be able to pass as a flag.
	for _, bad := range []string{"--dangerously-skip-permissions", "-m", "sonnet --tools Bash", "a;b", "sonnet\n", "[x]", strings.Repeat("a", 101)} {
		s = e.Config()
		s.Model.Model = bad
		if e.SaveSettings(s, nil) == nil {
			t.Fatalf("accepted model name %q", bad)
		}
	}
	s = e.Config()
	s.Model.Protocol = "gemini-cli"
	if e.SaveSettings(s, nil) == nil {
		t.Fatal("accepted unknown protocol")
	}
	s = e.Config()
	s.Model = ModelConfig{Protocol: "codex-cli"}
	if err := e.SaveSettings(s, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCLICommandNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := execModelCLI(context.Background(), cliRun{name: "claude", dir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "not found on PATH") {
		t.Fatalf("err %v", err)
	}
}

func TestCLIStderrErrorNeverEchoesReviewData(t *testing.T) {
	stderr := "user\nYou are the AegisHook operation security reviewer\n{\"command\":\"cat /etc/shadow\"}\n2026-11-14T14:21:31Z ERROR codex_login: Failed to refresh token\nERROR: Your access token could not be refreshed. Please log out and sign in again.\r\n2026-11-14T14:21:31Z ERROR codex_login: trailing log line\n"
	if got := cliStderrError(stderr, "exit status 1"); got != "Your access token could not be refreshed. Please log out and sign in again." {
		t.Fatalf("got %q", got)
	}
	if got := cliStderrError("user\nYou are the AegisHook operation security reviewer\ncat /etc/shadow\n", "exit status 1"); got != "exit status 1" {
		t.Fatalf("review data leaked into the error: %q", got)
	}
	tail := &tailBuffer{limit: 8}
	tail.Write([]byte("0123456789"))
	tail.Write([]byte("ab"))
	if tail.buf.String() != "456789ab" {
		t.Fatalf("tail %q", tail.buf.String())
	}
}
