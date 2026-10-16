package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFingerprint(t *testing.T) {
	plain := fingerprint("https://acme.com/careers/123")
	tracked := fingerprint("https://acme.com/careers/123?utm_source=indeed&utm_medium=feed")
	if plain != tracked {
		t.Fatalf("tracking params changed the fingerprint: %s vs %s", plain, tracked)
	}

	other := fingerprint("https://acme.com/careers/456")
	if plain == other {
		t.Fatal("different urls produced the same fingerprint")
	}

	if fingerprint("https://ACME.com/careers/123") != plain {
		t.Fatal("host case changed the fingerprint")
	}

	// Stability is the whole contract: the database dedups on this value.
	if fingerprint("https://acme.com/careers/123") != plain {
		t.Fatal("fingerprint is not stable across calls")
	}
}

func TestRunActorSkipsMalformedItem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`[
			{"companyName": "Acme", "title": "Platform Engineer"},
			{"title": 123}
		]`))
	}))
	defer server.Close()

	runner := &apifyRunner{client: server.Client(), baseURL: server.URL}
	items, err := runActor[linkedInItem](context.Background(), runner, linkedInActorID, map[string]any{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1: a malformed item should drop itself, not the run", len(items))
	}
	if items[0].CompanyName != "Acme" {
		t.Fatalf("company = %q", items[0].CompanyName)
	}
}

func TestRunActorErrorCarriesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid platform value"}}`))
	}))
	defer server.Close()

	runner := &apifyRunner{client: server.Client(), baseURL: server.URL}
	_, err := runActor[linkedInItem](context.Background(), runner, linkedInActorID, map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "Invalid platform value") {
		t.Fatalf("error = %v, want the api message so a bad param is distinguishable from a bad token", err)
	}
}
