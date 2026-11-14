package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func ValidateModelURL(base string) error {
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("model address is invalid; embedded credentials, query parameters and fragments are not allowed")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && hasExact(u.Hostname(), "localhost", "127.0.0.1", "::1")) {
		return errors.New("remote model services must use HTTPS; HTTP is allowed locally")
	}
	return nil
}
func callModel(ctx context.Context, s Settings, in any) (Decision, error) {
	var usage *TokenUsage
	return callModelWithUsage(ctx, s, in, &usage)
}
func callModelWithUsage(ctx context.Context, s Settings, in any, usage **TokenUsage) (Decision, error) {
	// All attempts share the original time budget, including configuration tests.
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.ModelSeconds)*time.Second)
	defer cancel()
	data, _ := json.Marshal(in)
	messages := []map[string]string{{"role": "user", "content": string(data)}}
	total := &TokenUsage{}
	*usage = nil
	allUsageKnown := true
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return Decision{}, errors.New("model connection failed or the request timed out")
		}
		var current *TokenUsage
		reply, err := callModelText(ctx, s, messages, &current)
		if current == nil {
			// A partial total must not be presented as a complete cost estimate.
			allUsageKnown = false
			*usage = nil
		} else {
			total.Input += current.Input
			total.Output += current.Output
			total.CacheRead += current.CacheRead
			total.CacheWrite += current.CacheWrite
			if allUsageKnown {
				*usage = total
			}
		}
		if err != nil {
			return Decision{}, err
		}
		d, err := ParseModelDecision(reply)
		var formatErr *modelJSONError
		if !errors.As(err, &formatErr) {
			return d, err
		}
		if attempt == 3 {
			return Decision{}, fmt.Errorf("%w; JSON format correction was retried 3 times and still failed", err)
		}
		if strings.TrimSpace(reply) == "" {
			reply = "(the previous model response was empty)"
		}
		messages = append(messages,
			map[string]string{"role": "assistant", "content": reply},
			map[string]string{"role": "user", "content": "The previous decision failed JSON format validation: " + formatErr.detail + ". Based on the original review input and the existing rules, output the complete decision again, fixing only the format and not changing the security judgment because of the format feedback. Output exactly one JSON object containing only the string fields decision and comment, with no Markdown, explanatory text or extra fields. decision must be approve or reject; comment must use the structure 'Actual operation: ...; Consequence if successful: ...; Matched rule: ...', and the rule ID must agree with the conclusion."},
		)
	}
}

// modelJSONError marks only verdict JSON syntax/shape errors as retryable.
// Its diagnostic never includes raw model output or arbitrary field names.
type modelJSONError struct {
	message string
	detail  string
}

func (e *modelJSONError) Error() string { return e.message }

func callModelText(ctx context.Context, s Settings, messages []map[string]string, usage **TokenUsage) (string, error) {
	if isCLIProtocol(s.Model.Protocol) {
		return callModelCLI(ctx, s, messages, usage)
	}
	if err := ValidateModelURL(s.Model.BaseURL); err != nil {
		return "", err
	}
	if s.Model.Model == "" {
		return "", errors.New("model name is empty")
	}
	endpoint := strings.TrimRight(s.Model.BaseURL, "/")
	payload := map[string]any{"model": s.Model.Model, "stream": false, "messages": append([]map[string]string{{"role": "system", "content": s.Prompt}}, messages...)}
	if s.Model.Protocol == "anthropic" {
		if strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/messages"
		} else {
			endpoint += "/v1/messages"
		}
		payload = map[string]any{"model": s.Model.Model, "stream": false, "max_tokens": 2048, "system": s.Prompt, "messages": messages}
	} else {
		endpoint += "/chat/completions"
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.Model.Protocol == "anthropic" {
		req.Header.Set("anthropic-version", "2023-06-01")
		if s.Model.APIKey != "" {
			req.Header.Set("x-api-key", s.Model.APIKey)
		}
	} else if s.Model.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.Model.APIKey)
	}

	client := &http.Client{Timeout: time.Duration(s.ModelSeconds) * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return errors.New("model endpoint does not allow redirects")
	}}
	res, err := client.Do(req)
	if err != nil {
		return "", errors.New("model connection failed or the request timed out")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("model returned HTTP %d", res.StatusCode)
	}
	if s.Model.Protocol == "anthropic" {
		var out struct {
			Usage   json.RawMessage `json:"usage"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			StopReason string `json:"stop_reason"`
		}
		if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out); err != nil {
			return "", errors.New("Anthropic model response format is invalid")
		}
		*usage = parseUsage(out.Usage, s.Model.Protocol)
		if out.StopReason != "end_turn" {
			return "", errors.New("Anthropic model did not finish a text decision")
		}
		texts := []string{}
		for _, c := range out.Content {
			if c.Type == "text" {
				texts = append(texts, c.Text)
			}
		}
		return strings.Join(texts, "\n"), nil
	}
	var out struct {
		Usage   json.RawMessage `json:"usage"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out); err != nil {
		return "", errors.New("model response format is invalid")
	}
	*usage = parseUsage(out.Usage, s.Model.Protocol)
	if len(out.Choices) != 1 {
		return "", errors.New("model response format is invalid")
	}
	if reason := out.Choices[0].FinishReason; reason != "" && reason != "stop" {
		return "", errors.New("model decision output was incomplete")
	}
	return out.Choices[0].Message.Content, nil
}
func ParseModelDecision(text string) (Decision, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &fields) == nil {
		if _, ok := fields["editedArguments"]; ok {
			return Decision{}, errors.New("editedArguments from the model are not applied in this version; have Pi adjust the arguments and resubmit for review")
		}
	}
	var out struct {
		Decision string `json:"decision"`
		Comment  string `json:"comment"`
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		detail := "only the two string fields decision and comment are allowed; extra fields are not"
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syntaxErr):
			detail = fmt.Sprintf("JSON syntax error (byte offset %d); check quotes, commas and any text or code fences outside the object", syntaxErr.Offset)
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			detail = "the response is empty or the JSON is incomplete; output a complete JSON object"
		case errors.As(err, &typeErr):
			detail = "JSON type error: the top level must be an object, and decision and comment must be strings"
		}
		return Decision{}, &modelJSONError{message: "model decision must be JSON containing only decision and comment", detail: detail}
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return Decision{}, &modelJSONError{message: "model output has extra content", detail: "there is extra content after the JSON object; output exactly one JSON object"}
	}
	for _, key := range []string{"decision", "comment"} {
		if raw, ok := fields[key]; !ok || string(raw) == "null" {
			return Decision{}, &modelJSONError{message: "model decision must be JSON containing only decision and comment", detail: "missing string field " + key + ", or the field is null"}
		}
	}
	if !hasExact(out.Decision, "approve", "reject") || !(strings.Contains(out.Comment, "Actual operation:") || strings.Contains(out.Comment, "实际操作：")) || !(strings.Contains(out.Comment, "Consequence if successful:") || strings.Contains(out.Comment, "成功后的后果：")) {
		return Decision{}, errors.New("model decision is missing a valid conclusion or explanation")
	}
	m := regexp.MustCompile(`(?:Matched rule|命中规则)\s*[:：]\s*(R[1-7]|A[1-7]|D1)\b`).FindStringSubmatch(out.Comment)
	if len(m) != 2 {
		return Decision{}, errors.New("model decision is missing a valid rule ID")
	}
	if strings.HasPrefix(m[1], "R") && out.Decision != "reject" {
		return Decision{}, errors.New("model conclusion conflicts with a reject rule")
	}
	if (strings.HasPrefix(m[1], "A") || m[1] == "D1") && out.Decision != "approve" {
		return Decision{}, errors.New("model conclusion conflicts with an allow rule")
	}
	return Decision{Decision: out.Decision, Comment: RedactText(out.Comment), RuleID: m[1]}, nil
}
func (e *Engine) modelReview(r Review, s Settings) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.ModelSeconds)*time.Second)
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-e.stop:
			cancel()
		case <-done:
		}
	}()
	var usage *TokenUsage
	d, err := callModelWithUsage(ctx, s, modelInput(r), &usage)
	if err != nil {
		d = Decision{Decision: "reject", RuleID: "E_MODEL", Comment: err.Error() + "; this call was rejected. Check the review model configuration."}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if usageErr := e.recordUsage(r.ID, r.ID, "review", s, usage, err != nil); usageErr != nil {
		return
	}
	current, err := e.Review(r.ID)
	if err != nil || current.Decision != "pending" || current.NeedsHuman {
		return
	}
	if e.clock().After(current.Deadline) {
		return
	}
	current.ModelVerdict = &d
	if current.ScriptCaptureStatus == "incomplete" && d.Decision == "approve" {
		current.Decision = "pending"
		current.NeedsHuman = true
		current.ReviewPath = "human"
		current.RuleID = ""
		current.Comment = "The script could not be captured in full, so hidden side effects cannot be ruled out. Human confirmation is required."
		current.DecidedAt = nil
		current.Execution = "not_executed"
		e.commitReview(current, "review.model.incomplete")
		return
	}
	current.Decision = d.Decision
	current.RuleID = d.RuleID
	current.Comment = d.Comment
	now := e.clock()
	current.DecidedAt = &now
	if d.Decision == "approve" {
		current.Execution = "awaiting_execution"
	}
	e.commitReview(current, "review.model")
}
func (e *Engine) TestModel(ctx context.Context) error {
	e.mu.Lock()
	s := e.Settings
	e.mu.Unlock()
	requestSettings := s
	requestSettings.Prompt = modelPrompt(s.Prompt)
	var usage *TokenUsage
	_, err := callModelWithUsage(ctx, requestSettings, modelInput(Review{ReviewInput: ReviewInput{ToolName: "read", Arguments: map[string]any{"path": "README.md"}, UserMessage: "Read the project description"}}), &usage)
	e.mu.Lock()
	defer e.mu.Unlock()
	if usageErr := e.recordUsage(ID(), "", "test", s, usage, err != nil); usageErr != nil {
		return usageErr
	}
	if err != nil {
		return err
	}
	if e.Settings.Version != s.Version {
		return errors.New("config changed; test again")
	}
	s.Model.Tested = true
	if err = e.change("settings", "current", s, "model.test", "model connection and structured decision test succeeded"); err != nil {
		return err
	}
	e.Settings = s
	return nil
}
