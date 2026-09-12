package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Voice agent: the browser talks to OpenAI directly over WebRTC using an
// ephemeral client secret minted here. Our servers never relay the audio —
// that is what keeps voice-to-voice latency at the API's documented ~800ms
// median instead of adding a round trip through Goonj.

// AgentSessionRequest is the body for POST /ai/agent/session.
type AgentSessionRequest struct {
	// Instructions shape the assistant's persona/task for this session.
	Instructions string `json:"instructions,omitempty"`
	// Voice is one of the Realtime API voices (alloy, verse, ...).
	Voice string `json:"voice,omitempty"`
	// Language hint for the agent's responses ("en", "hi", ...).
	Language string `json:"language,omitempty"`
}

// AgentSessionResponse mirrors the subset of OpenAI's client secret response
// the browser needs to open its peer connection.
type AgentSessionResponse struct {
	ClientSecret struct {
		Value     string    `json:"value"`
		ExpiresAt time.Time `json:"expires_at"`
	} `json:"client_secret"`
	// Model the session was created with (echoed for the client).
	Model string `json:"model"`
	// RealtimeURL is the WebRTC SDP exchange endpoint.
	RealtimeURL string `json:"realtime_url"`
}

// agentModel is the speech-to-speech realtime model for conversational use.
const agentModel = "gpt-realtime"

// realtimeHTTPBase is the REST base used for client-secret minting.
const realtimeHTTPBase = "https://api.openai.com/v1/realtime"

// MintAgentSession creates a short-lived Realtime session for the browser.
// The API key stays server-side; the returned secret is ephemeral (expires
// in minutes) and cannot be used for anything but this session.
func MintAgentSession(ctx context.Context, apiKey string, req AgentSessionRequest) (*AgentSessionResponse, error) {
	if apiKey == "" {
		return nil, ErrUnavailable
	}
	if req.Voice == "" {
		req.Voice = "alloy"
	}
	if req.Language == "" {
		req.Language = "en"
	}
	if req.Instructions == "" {
		req.Instructions = "You are Goonj's live radio companion. Answer briefly like a knowledgeable co-host; the listener is watching a live audio show."
	}
	if len(req.Instructions) > 4000 {
		req.Instructions = req.Instructions[:4000]
	}

	body := map[string]any{
		"model": agentModel,
		"audio": map[string]any{
			"output": map[string]any{
				"voice":  req.Voice,
				"format": map[string]any{"type": "audio/pcmu"},
			},
		},
	}
	instr := strings.TrimSpace(req.Instructions)
	if instr != "" {
		body["instructions"] = instr
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		realtimeHTTPBase+"/client_secrets", strings.NewReader(mustJSON(body)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("OpenAI-Safety-Identifier", "goonj-voice-agent")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai client_secrets: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai client_secrets: %s", resp.Status)
	}
	var out AgentSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode client secret: %w", err)
	}
	out.Model = agentModel
	out.RealtimeURL = "https://api.openai.com/v1/realtime/calls"
	return &out, nil
}

// apiKeyFromEnv reads the shared OpenAI key (same var the embedders use).
func apiKeyFromEnv() string { return os.Getenv("OPENAI_API_KEY") }

// realtimeConfigured reports whether the deployment actually targets OpenAI
// for the Realtime API (live captions / voice agent). Other providers (Groq,
// Azure, llama.cpp) speak the chat/STT API but not Realtime; surfacing that
// as 503 lets the client hide the feature instead of erroring.
func realtimeConfigured() bool {
	base := strings.ToLower(os.Getenv("OPENAI_BASE_URL"))
	// Empty base means the default (api.openai.com) — configured.
	return base == "" || strings.Contains(base, "api.openai.com")
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
