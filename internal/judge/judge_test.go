package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jobradar/internal/shared"
)

func TestOutcome(t *testing.T) {
	cases := []struct {
		name string
		v    Verdict
		want Outcome
	}{
		{"remote yes", Verdict{RemoteGlobal: "yes", Fit: "yes"}, OutcomeApproved},
		{"relocation yes", Verdict{Relocation: "yes", Fit: "yes"}, OutcomeApproved},
		{"local yes", Verdict{Local: "yes", Fit: "yes"}, OutcomeApproved},
		{"nothing open", Verdict{RemoteGlobal: "no", Relocation: "no", Local: "no", Fit: "yes"}, OutcomeRejected},
		{"wrong stack rejects", Verdict{RemoteGlobal: "yes", Relocation: "yes", Local: "yes", Fit: "no"}, OutcomeRejected},
		// Only an affirmative yes approves, so an answer the prompt cannot
		// produce still rejects rather than falling through.
		{"unanswered rejects", Verdict{Fit: "yes"}, OutcomeRejected},
		{"unrecognised answer rejects", Verdict{RemoteGlobal: "maybe", Fit: "yes"}, OutcomeRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := outcome(tc.v); got != tc.want {
				t.Fatalf("outcome(%+v) = %s, want %s", tc.v, got, tc.want)
			}
		})
	}
}

func TestEvaluate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "jev-1.13.0",
			"answers": {
				"remote_global": { "type": "choice", "choice": "yes" },
				"relocation_supported": { "type": "choice", "choice": "no" },
				"stack_fit": { "type": "choice", "choice": "yes" }
			},
			"usage": { "input_tokens": 500, "output_tokens": 30 }
		}`))
	}))
	defer server.Close()

	j := &Judge{
		client:  server.Client(),
		baseURL: server.URL,
		apiKey:  "test-key",
		model:   defaultModel,
	}
	verdict, err := j.Evaluate(context.Background(), shared.Criteria{
		Context: "Platform engineer, Go and Kubernetes",
		Questions: map[string]shared.Question{
			"remote_global":        {Type: "noul", Instructions: "Remote worldwide?"},
			"relocation_supported": {Type: "noul", Instructions: "Relocation offered?"},
			"stack_fit": {
				Type:         "choice",
				Instructions: "How well does this role match?",
				Criteria:     map[string]string{"strong": "", "partial": "", "none": ""},
			},
		},
	}, shared.Job{
		Title:       "Platform Engineer",
		Company:     "Acme",
		Description: "Go and Kubernetes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Outcome != OutcomeApproved {
		t.Fatalf("outcome = %s, want approved", verdict.Outcome)
	}
	if verdict.RemoteGlobal != "yes" || verdict.Relocation != "no" {
		t.Fatalf("remote/relocation = %q / %q", verdict.RemoteGlobal, verdict.Relocation)
	}
	if verdict.Fit != "yes" {
		t.Fatalf("fit = %q", verdict.Fit)
	}
}

// The question name keys the request map, so it must not reach the wire itself:
// typesafe would reject an extra field or, worse, read it as a question.
func TestCritBuilderBuildsTheRequest(t *testing.T) {
	var body bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = body.ReadFrom(r.Body)
		_, _ = w.Write([]byte(`{"answers":{}}`))
	}))
	defer server.Close()

	j := &Judge{client: server.Client(), baseURL: server.URL, model: defaultModel}
	_, err := j.Evaluate(context.Background(), shared.Crit().
		Context("Platform engineer in Lagos.").
		Questions(
			shared.RemoteGlobal,
			shared.Choice("stack_fit", "In the candidate's field?", map[string]string{"yes": "Overlaps."}),
		), shared.Job{Title: "Platform Engineer", Company: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	var sent struct {
		State     string `json:"state"`
		Questions map[string]struct {
			Type         string            `json:"type"`
			Instructions string            `json:"instructions"`
			Criteria     map[string]string `json:"criteria"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(body.Bytes(), &sent); err != nil {
		t.Fatal(err)
	}

	if len(sent.Questions) != 2 {
		t.Fatalf("questions = %v, want them keyed by the question name", sent.Questions)
	}
	if _, present := sent.Questions["remote_global"]; !present {
		t.Error("remote_global is missing from the request")
	}
	fit, present := sent.Questions["stack_fit"]
	if !present || fit.Type != "choice" || fit.Criteria["yes"] != "Overlaps." {
		t.Errorf("stack_fit = %+v", fit)
	}
	if !strings.Contains(sent.State, "Platform engineer in Lagos.") {
		t.Error("the context is missing from the state")
	}
	if strings.Contains(body.String(), `"Name"`) {
		t.Error("the question name was serialized into the request")
	}
}
