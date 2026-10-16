package clients

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"jobradar/internal/shared"
)

const linkedInFixture = `[
  {
    "id": 4012345678,
    "link": "https://www.linkedin.com/jobs/view/4012345678",
    "title": "Platform Engineer",
    "companyName": "Acme",
    "location": "Austin, Texas, United States",
    "descriptionText": "Go and Kubernetes",
    "descriptionHtml": "<p>Go and Kubernetes</p>",
    "postedAt": "2026-10-01"
  },
  {
    "id": "4012345679",
    "link": "",
    "applyUrl": "https://acme.com/apply",
    "title": "Site Reliability Engineer",
    "companyName": "Globex",
    "location": "Remote",
    "descriptionHtml": "<p>Own <b>reliability</b> and   on-call</p>",
    "postedAt": "2026-10-02"
  },
  {
    "id": 3,
    "link": "https://www.linkedin.com/jobs/view/3",
    "title": "",
    "companyName": "Initech",
    "location": "Remote"
  }
]`

func linkedInServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(linkedInFixture))
	}))
}

func TestLinkedInSearch(t *testing.T) {
	server := linkedInServer(t)
	defer server.Close()

	scraper := &LinkedIn{runner: &apifyRunner{client: server.Client(), baseURL: server.URL}}
	jobs, err := scraper.Search(context.Background(), shared.Intent{
		Terms:            []string{"kubernetes"},
		Location:         []string{"United States"},
		WorkMode:         shared.Remote,
		PostedWithinDays: 14,
		Limit:            50,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The third item has no title, so it is dropped.
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(jobs))
	}

	first := jobs[0]
	// The id arrives as a JSON number but the actor does not document its type,
	// so both shapes have to work.
	if first.ExternalID != "4012345678" {
		t.Errorf("external id = %q, want the numeric id as text", first.ExternalID)
	}
	if first.Source != LinkedInSource || first.Platform != "LinkedIn" {
		t.Errorf("source/platform = %q / %q", first.Source, first.Platform)
	}
	if first.URL != "https://www.linkedin.com/jobs/view/4012345678" {
		t.Errorf("url = %q", first.URL)
	}
	if first.Description != "Go and Kubernetes" {
		t.Errorf("description = %q, want the plain text field", first.Description)
	}
	if first.PostedAt != "2026-10-01" {
		t.Errorf("posted_at = %q", first.PostedAt)
	}

	second := jobs[1]
	if second.ExternalID != "4012345679" {
		t.Errorf("external id = %q, want the string id", second.ExternalID)
	}
	if second.URL != "https://acme.com/apply" {
		t.Errorf("url = %q, want the apply url when link is empty", second.URL)
	}
	if second.Description != "Own reliability and on-call" {
		t.Errorf("description = %q, want markup stripped and whitespace collapsed", second.Description)
	}
}

func TestLinkedInInput(t *testing.T) {
	cases := []struct {
		name         string
		workMode     shared.WorkMode
		location     string
		wantLocation string
	}{
		{name: "remote still sends a place, for the local-remote queues", workMode: shared.Remote, location: "Nigeria", wantLocation: "Nigeria"},
		{name: "relocation sends the place", workMode: shared.Relocation, location: "European Union", wantLocation: "European Union"},
		{name: "no place is omitted", workMode: shared.Remote, location: "", wantLocation: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var captured map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
					t.Errorf("decode request: %v", err)
				}
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`[]`))
			}))
			defer server.Close()

			scraper := &LinkedIn{runner: &apifyRunner{client: server.Client(), baseURL: server.URL}}
			_, err := scraper.Search(context.Background(), shared.Intent{
				Terms:            []string{"kubernetes"},
				Location:         []string{tc.location},
				WorkMode:         tc.workMode,
				PostedWithinDays: 14,
				Limit:            30,
			})
			if err != nil {
				t.Fatal(err)
			}

			if captured["keywords"] != "kubernetes" {
				t.Errorf("keywords = %v", captured["keywords"])
			}
			if tc.wantLocation == "" {
				if _, present := captured["location"]; present {
					t.Errorf("location = %v, want it omitted for a remote search", captured["location"])
				}
			} else if captured["location"] != tc.wantLocation {
				t.Errorf("location = %v, want %v", captured["location"], tc.wantLocation)
			}
			// LinkedIn has no two-week window, so the widest useful bucket is used.
			if captured["datePosted"] != linkedInDatePosted {
				t.Errorf("datePosted = %v", captured["datePosted"])
			}
			if captured["limitPerSource"] != float64(30) {
				t.Errorf("limitPerSource = %v", captured["limitPerSource"])
			}
			// Both of these multiply per-job requests.
			if captured["scrapeCompany"] != false {
				t.Errorf("scrapeCompany = %v, want false", captured["scrapeCompany"])
			}
			if captured["splitByLocation"] != false {
				t.Errorf("splitByLocation = %v, want false", captured["splitByLocation"])
			}
		})
	}
}

func TestLinkedInDatePostedOmittedWithoutWindow(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	scraper := &LinkedIn{runner: &apifyRunner{client: server.Client(), baseURL: server.URL}}
	_, err := scraper.Search(context.Background(), shared.Intent{
		Terms:    []string{"kubernetes"},
		Location: []string{"United States"},
		Limit:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := captured["datePosted"]; present {
		t.Errorf("datePosted = %v, want it omitted when no window is set", captured["datePosted"])
	}
}
