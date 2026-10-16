package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"jobradar/internal/shared"
)

const (
	defaultBaseURL = "https://api.typesafe.ai"
	defaultModel   = "jev-latest"
)

type Outcome string

const (
	OutcomeApproved Outcome = "approved"
	OutcomeRejected Outcome = "rejected"
)

type Verdict struct {
	RemoteGlobal string
	Relocation   string
	Local        string
	Fit          string
	Confidence   map[string]float64
	Outcome      Outcome
	Details      map[string]any
}

type evaluateRequest struct {
	State     string                     `json:"state"`
	Model     string                     `json:"model"`
	Questions map[string]shared.Question `json:"questions"`
}

type evaluateResponse struct {
	Model   string                     `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type choiceAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

type Judge struct {
	client  *http.Client
	baseURL string
	apiKey  string
	model   string
}

func New(apiKey string) *Judge {
	return &Judge{
		client:  &http.Client{Timeout: 60 * time.Second},
		baseURL: defaultBaseURL,
		apiKey:  apiKey,
		model:   defaultModel,
	}
}

func (j *Judge) Evaluate(ctx context.Context, criteria shared.Criteria, job shared.Job) (Verdict, error) {
	payload, err := json.Marshal(evaluateRequest{
		State:     state(criteria.Context, job),
		Model:     j.model,
		Questions: criteria.Questions,
	})
	if err != nil {
		return Verdict{}, fmt.Errorf("encode judge request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, j.baseURL+"/v1/systemone", bytes.NewReader(payload))
	if err != nil {
		return Verdict{}, fmt.Errorf("create judge request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if j.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+j.apiKey)
	}

	response, err := j.client.Do(request)
	if err != nil {
		return Verdict{}, fmt.Errorf("call judge: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode >= http.StatusBadRequest {
		return Verdict{}, fmt.Errorf("judge: %s", response.Status)
	}

	var result evaluateResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return Verdict{}, fmt.Errorf("decode judge response: %w", err)
	}

	return verdict(result), nil
}

func verdict(result evaluateResponse) Verdict {
	verdict := Verdict{Details: map[string]any{}, Confidence: map[string]float64{}}

	for name, raw := range result.Answers {
		var answer choiceAnswer
		if err := json.Unmarshal(raw, &answer); err != nil {
			continue
		}
		verdict.Confidence[name] = answer.Confidence
		switch name {
		case "remote_global":
			verdict.RemoteGlobal = answer.Choice
		case "relocation_supported":
			verdict.Relocation = answer.Choice
		case "local_role":
			verdict.Local = answer.Choice
		case "stack_fit":
			verdict.Fit = answer.Choice
		}
	}

	verdict.Outcome = outcome(verdict)
	verdict.Details["remote_global"] = verdict.RemoteGlobal
	verdict.Details["relocation_supported"] = verdict.Relocation
	verdict.Details["local_role"] = verdict.Local
	verdict.Details["stack_fit"] = verdict.Fit
	verdict.Details["confidence"] = verdict.Confidence
	verdict.Details["usage"] = result.Usage

	return verdict
}

// minConfidence is the certainty an approving answer has to reach. The model
// reports this itself, so it ranks decisiveness rather than guaranteeing
// correctness.
const minConfidence = 0.7

// outcome requires an affirmative answer rather than the absence of a negative,
// and a decisive one. Exactly one eligibility question is asked per queue, so
// whichever is open is the one that can approve.
func outcome(verdict Verdict) Outcome {
	if verdict.Fit == "no" || verdict.Confidence["stack_fit"] < minConfidence {
		return OutcomeRejected
	}
	for _, eligibility := range []struct {
		name   string
		choice string
	}{
		{"remote_global", verdict.RemoteGlobal},
		{"relocation_supported", verdict.Relocation},
		{"local_role", verdict.Local},
	} {
		if eligibility.choice != "yes" {
			continue
		}
		if verdict.Confidence[eligibility.name] < minConfidence {
			return OutcomeRejected
		}
		return OutcomeApproved
	}
	return OutcomeRejected
}

func state(candidateContext string, job shared.Job) string {
	var state strings.Builder
	state.WriteString("Candidate context:\n")
	state.WriteString(candidateContext)
	state.WriteString("\n\nJob posting:\n")
	fmt.Fprintf(&state, "Title: %s\n", job.Title)
	fmt.Fprintf(&state, "Company: %s\n", job.Company)
	fmt.Fprintf(&state, "Location: %s\n", job.Location)
	if job.URL != "" {
		fmt.Fprintf(&state, "URL: %s\n", job.URL)
	}
	state.WriteString("\nDescription:\n")
	state.WriteString(job.Description)
	return state.String()
}
