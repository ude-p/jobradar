package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"jobradar/internal/shared"
)

const (
	FreehireSource = "freehire"

	freehireBaseURL = "https://freehire.me"
	freehirePath    = "/api/v1/agent/jobs/search"

	freehireMaxPage = 100
	freehireMinPage = 25
	// Free and newest first, so over-fetching costs nothing and reaches un-stored postings.
	freehireOverFetch = 4
)

type Freehire struct {
	client  *http.Client
	baseURL string
}

func NewFreehire() *Freehire {
	return &Freehire{
		client:  &http.Client{Timeout: 60 * time.Second},
		baseURL: freehireBaseURL,
	}
}

func (scraper *Freehire) Name() string {
	return FreehireSource
}

type freehireItem struct {
	PublicSlug  string          `json:"public_slug"`
	Source      string          `json:"source"`
	URL         string          `json:"url"`
	Title       string          `json:"title"`
	Company     string          `json:"company"`
	Location    string          `json:"location"`
	Description string          `json:"description"`
	Skills      json.RawMessage `json:"skills"`
	PostedAt    string          `json:"posted_at"`
}

type freehireResponse struct {
	Data  []json.RawMessage `json:"data"`
	Meta  freehireMeta      `json:"meta"`
	Error string            `json:"error"`
}

type freehireMeta struct {
	Total         int `json:"total"`
	IgnoredParams []struct {
		Param string `json:"param"`
	} `json:"ignored_params"`
}

func (scraper *Freehire) Search(ctx context.Context, intent shared.Intent) ([]shared.Job, error) {
	response, err := scraper.page(ctx, intent, freehirePageSize(intent.Limit))
	if err != nil {
		return nil, err
	}

	// One posting arrives from several ats; this can suppress a new sibling of a stored mirror.
	seen := make(map[string]struct{}, len(response.Data))
	jobs := make([]shared.Job, 0, len(response.Data))
	for _, raw := range response.Data {
		var item freehireItem
		if err := json.Unmarshal(raw, &item); err != nil {
			log.Printf("freehire: skip item: %v", err)
			continue
		}

		job, ok := item.job()
		if !ok {
			continue
		}
		key := strings.ToLower(job.Company) + "\x00" +
			strings.ToLower(job.Title) + "\x00" +
			strings.ToLower(job.Location)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

func freehirePageSize(budget int) int {
	return min(max(budget*freehireOverFetch, freehireMinPage), freehireMaxPage)
}

func (scraper *Freehire) page(ctx context.Context, intent shared.Intent, limit int) (freehireResponse, error) {
	query := url.Values{}
	if keyword := strings.Join(intent.Terms, " "); keyword != "" {
		query.Set("q", keyword)
	}
	for _, skill := range intent.Skills {
		query.Add("skills", skill)
	}
	for _, category := range intent.Categories {
		query.Add("category", category)
	}
	for _, region := range intent.Regions {
		query.Add("regions", region)
	}
	for _, country := range intent.Countries {
		query.Add("countries", strings.ToLower(country))
	}
	if intent.WorkMode == shared.Remote {
		query.Set("work_mode", "remote")
	}
	if intent.PostedWithinDays > 0 {
		query.Set("posted_within_days", strconv.Itoa(intent.PostedWithinDays))
	}
	query.Set("description_format", "markdown")
	query.Set("limit", strconv.Itoa(limit))

	endpoint := scraper.baseURL + freehirePath + "?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return freehireResponse{}, fmt.Errorf("create freehire request: %w", err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := scraper.client.Do(request)
	if err != nil {
		return freehireResponse{}, fmt.Errorf("call freehire: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return freehireResponse{}, fmt.Errorf("freehire: %s: %s", response.Status, strings.TrimSpace(string(body)))
	}

	var result freehireResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return freehireResponse{}, fmt.Errorf("decode freehire response: %w", err)
	}
	// freehire reports a rejected query in the body, with a 200.
	if result.Error != "" {
		return freehireResponse{}, fmt.Errorf("freehire: %s", result.Error)
	}
	for _, ignored := range result.Meta.IgnoredParams {
		log.Printf("freehire ignored %q", ignored.Param)
	}

	return result, nil
}

func (item freehireItem) job() (shared.Job, bool) {
	link := strings.TrimSpace(item.URL)
	title := strings.TrimSpace(item.Title)
	company := strings.TrimSpace(item.Company)
	if link == "" || title == "" || company == "" {
		return shared.Job{}, false
	}

	externalID := strings.TrimSpace(item.PublicSlug)
	if externalID == "" {
		externalID = fingerprint(link)
	}

	return shared.Job{
		Source:      FreehireSource,
		ExternalID:  externalID,
		Platform:    strings.TrimSpace(item.Source),
		Company:     company,
		Title:       title,
		Location:    strings.TrimSpace(item.Location),
		URL:         link,
		Description: strings.TrimSpace(item.Description),
		Skills:      textList(item.Skills),
		PostedAt:    strings.TrimSpace(item.PostedAt),
	}, true
}
