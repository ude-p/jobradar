package clients

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	apifyBase = "https://api.apify.com/v2/acts"

	// Apify caps this endpoint at ~300s. A run that times out has still been
	// billed for whatever it produced.
	apifySyncTimeout = 330 * time.Second
)

type apifyRunner struct {
	client  *http.Client
	baseURL string
	token   string
}

func newApifyRunner(token string) *apifyRunner {
	return &apifyRunner{
		client:  &http.Client{Timeout: apifySyncTimeout},
		baseURL: apifyBase,
		token:   token,
	}
}

// runActor runs actorID synchronously and returns its dataset items. options
// carries run options such as maxItems, which is how a pay-per-result actor is
// capped before it bills. Items are decoded one at a time so one untyped field
// drops one item, not the whole run.
func runActor[T any](ctx context.Context, runner *apifyRunner, actorID string, input any, options url.Values) ([]T, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode apify %s input: %w", actorID, err)
	}

	query := url.Values{"token": {runner.token}}
	for key, values := range options {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	endpoint := fmt.Sprintf(
		"%s/%s/run-sync-get-dataset-items?%s",
		runner.baseURL, actorID, query.Encode(),
	)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create apify %s request: %w", actorID, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := runner.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call apify %s: %w", actorID, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	// Success is 201 Created, so bound the failure rather than testing for 200.
	if response.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return nil, fmt.Errorf("apify %s: %s: %s", actorID, response.Status, strings.TrimSpace(string(body)))
	}

	var raw []json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode apify %s response: %w", actorID, err)
	}

	items := make([]T, 0, len(raw))
	for index, element := range raw {
		var item T
		if err := json.Unmarshal(element, &item); err != nil {
			log.Printf("apify %s: skip item %d: %v", actorID, index, err)
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

// fingerprint is a stable identity for a posting URL, with tracking parameters
// and host case removed so it survives between runs.
func fingerprint(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if parsed, err := url.Parse(rawURL); err == nil {
		parsed.RawQuery = ""
		parsed.Fragment = ""
		parsed.Host = strings.ToLower(parsed.Host)
		rawURL = parsed.String()
	}
	sum := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(sum[:12])
}

// scalar renders a JSON value as text: the actors' field types are unpublished.
func scalar(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return number.String()
	}
	return ""
}

// textList renders a JSON value as a list. The all-jobs actor normalises many
// platforms, so a field that is a list on one arrives comma-separated on another.
func textList(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}

	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}

	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return nil
	}

	var items []string
	for item := range strings.SplitSeq(text, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

// firstText returns the first non-blank entry, for a preferred field and its fallback.
func firstText(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
