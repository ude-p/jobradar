package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"jobradar/internal/shared"
)

// Mirrors a real envelope: a greenhouse posting, the same posting mirrored on
// himalayas under a different slug, one entry with no company, and one whose
// skills arrive comma-separated instead of as a list.
const freehireFixture = `{
  "data": [
    {
      "public_slug": "platform-engineer-acme-abc123",
      "source": "greenhouse",
      "url": "https://job-boards.greenhouse.io/acme/jobs/123?utm_source=freehire.me",
      "title": "Platform Engineer",
      "company": "Acme",
      "location": "Remote - Worldwide",
      "description": "Go and Kubernetes",
      "skills": ["go", "kubernetes"],
      "posted_at": "2026-10-16T06:50:03Z"
    },
    {
      "public_slug": "platform-engineer-acme-xyz789",
      "source": "himalayas",
      "url": "https://himalayas.app/jobs/platform-engineer-acme",
      "title": "Platform Engineer",
      "company": "Acme",
      "location": "Remote - Worldwide",
      "description": "Go and Kubernetes, mirrored",
      "skills": "go, kubernetes",
      "posted_at": "2026-10-16T06:40:00Z"
    },
    {
      "public_slug": "no-company",
      "source": "lever",
      "url": "https://jobs.lever.co/unknown/1",
      "title": "Platform Engineer",
      "company": "",
      "location": "Remote"
    }
  ],
  "meta": { "total": 3, "limit": 100, "offset": 0 }
}`

func freehireServer(t *testing.T, body string) (*httptest.Server, *url.Values) {
	t.Helper()

	captured := &url.Values{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*captured = r.URL.Query()
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, captured
}

func TestFreehireSearch(t *testing.T) {
	server, _ := freehireServer(t, freehireFixture)

	scraper := &Freehire{client: server.Client(), baseURL: server.URL}
	jobs, err := scraper.Search(context.Background(), shared.Intent{
		Skills:  []string{"go", "kubernetes"},
		Regions: []string{"global"},
		Limit:   20,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The mirror shares company, title and location, so it collapses; the entry
	// with no company is dropped.
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1", len(jobs))
	}

	first := jobs[0]
	if first.Source != FreehireSource {
		t.Errorf("source = %q", first.Source)
	}
	if first.ExternalID != "platform-engineer-acme-abc123" {
		t.Errorf("external id = %q, want the public slug", first.ExternalID)
	}
	// Platform is the ats name, which is new information freehire adds.
	if first.Platform != "greenhouse" {
		t.Errorf("platform = %q", first.Platform)
	}
	if first.PostedAt != "2026-10-16T06:50:03Z" {
		t.Errorf("posted_at = %q", first.PostedAt)
	}
	if len(first.Skills) != 2 || first.Skills[0] != "go" {
		t.Errorf("skills = %v", first.Skills)
	}
}

func TestFreehireQuery(t *testing.T) {
	server, captured := freehireServer(t, `{"data":[],"meta":{"total":0}}`)

	scraper := &Freehire{client: server.Client(), baseURL: server.URL}
	_, err := scraper.Search(context.Background(), shared.Intent{
		Terms:            []string{"platform", "engineer"},
		Skills:           []string{"go", "kubernetes"},
		Categories:       []string{"backend", "devops"},
		Countries:        []string{"GB"},
		WorkMode:         shared.Remote,
		PostedWithinDays: 7,
		Limit:            200,
	})
	if err != nil {
		t.Fatal(err)
	}

	// A facet's values are ored, so each needs its own entry.
	if got := (*captured)["skills"]; len(got) != 2 || got[0] != "go" || got[1] != "kubernetes" {
		t.Errorf("skills = %v, want both values repeated", got)
	}
	if got := (*captured)["category"]; len(got) != 2 {
		t.Errorf("category = %v", got)
	}
	if got := (*captured)["q"]; len(got) != 1 || got[0] != "platform engineer" {
		t.Errorf("q = %v, want the terms joined into one query", got)
	}
	if got := (*captured)["countries"]; len(got) != 1 || got[0] != "gb" {
		t.Errorf("countries = %v, want it lowercased", got)
	}
	if got := (*captured)["work_mode"]; len(got) != 1 || got[0] != "remote" {
		t.Errorf("work_mode = %v", got)
	}
	// The api caps a page at 100 however large a limit is asked for.
	if got := (*captured)["limit"]; len(got) != 1 || got[0] != "100" {
		t.Errorf("limit = %v, want it clamped to 100", got)
	}
	// Results are ordered newest first, so a deeper offset only reaches older
	// postings and is never asked for.
	if _, present := (*captured)["offset"]; present {
		t.Error("offset was sent, want a single first page")
	}
}

func TestFreehireSoftErrorIsAnError(t *testing.T) {
	// freehire reports a rejected query in the body while still answering 200.
	server, _ := freehireServer(t, `{"error":"pagination too deep"}`)

	scraper := &Freehire{client: server.Client(), baseURL: server.URL}
	_, err := scraper.Search(context.Background(), shared.Intent{Limit: 10})
	if err == nil {
		t.Fatal("expected an error")
	}
	if want := "pagination too deep"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want it to carry %q", err, want)
	}
}

func TestFreehireUnusablePageStillReturnsJobs(t *testing.T) {
	// An ignored filter is reported, not fatal, so the page stays usable.
	server, _ := freehireServer(t, `{
		"data": [{
			"public_slug": "a", "source": "lever", "url": "https://jobs.lever.co/acme/1",
			"title": "Platform Engineer", "company": "Acme", "location": "Remote"
		}],
		"meta": {"total": 1, "ignored_params": [{"param": "nosuchparam"}]}
	}`)

	scraper := &Freehire{client: server.Client(), baseURL: server.URL}
	jobs, err := scraper.Search(context.Background(), shared.Intent{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1", len(jobs))
	}
}

func TestFreehireDropsMalformedItemOnly(t *testing.T) {
	// A field with an unexpected type must lose its own posting, not the page.
	server, _ := freehireServer(t, `{
		"data": [
			{"public_slug": "a", "source": "lever", "url": "https://jobs.lever.co/acme/1",
			 "title": "Platform Engineer", "company": "Acme", "location": "Remote"},
			{"public_slug": "b", "source": "lever", "url": "https://jobs.lever.co/acme/2",
			 "title": "SRE", "company": "Acme", "location": "Remote", "posted_at": {"nested": true}}
		],
		"meta": {"total": 2}
	}`)

	scraper := &Freehire{client: server.Client(), baseURL: server.URL}
	jobs, err := scraper.Search(context.Background(), shared.Intent{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ExternalID != "a" {
		t.Fatalf("got %d jobs, want only the well-formed one", len(jobs))
	}
}
