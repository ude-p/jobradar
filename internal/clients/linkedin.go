package clients

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"jobradar/internal/shared"
)

const (
	LinkedInSource  = "linkedin"
	linkedInActorID = "hKByXkMQaC5Qt9UMN"

	// LinkedIn offers only week and month buckets, so any window rounds to this.
	linkedInDatePosted = "pastWeek"

	// Bills per posting and cannot be paged, so a short run is still paid for.
	linkedInMaxChargeUSD = 0.50
)

type LinkedIn struct {
	runner *apifyRunner
}

func NewLinkedIn(apifyToken string) *LinkedIn {
	return &LinkedIn{runner: newApifyRunner(apifyToken)}
}

func (scraper *LinkedIn) Name() string {
	return LinkedInSource
}

type linkedInInput struct {
	Keywords       string `json:"keywords"`
	Location       string `json:"location,omitempty"`
	DatePosted     string `json:"datePosted,omitempty"`
	LimitPerSource int    `json:"limitPerSource"`
	// Both multiply per-job requests, so they stay off.
	ScrapeCompany   bool `json:"scrapeCompany"`
	SplitByLocation bool `json:"splitByLocation"`
}

type linkedInItem struct {
	ID              json.RawMessage `json:"id"`
	Link            string          `json:"link"`
	ApplyURL        string          `json:"applyUrl"`
	Title           string          `json:"title"`
	CompanyName     string          `json:"companyName"`
	Location        string          `json:"location"`
	DescriptionText string          `json:"descriptionText"`
	DescriptionHTML string          `json:"descriptionHtml"`
	PostedAt        string          `json:"postedAt"`
}

func (scraper *LinkedIn) Search(ctx context.Context, intent shared.Intent) ([]shared.Job, error) {
	// Free text only, so an intent targeting freehire's facets searches by skill.
	keyword := strings.Join(intent.Terms, " ")
	if keyword == "" {
		keyword = strings.Join(intent.Skills, " ")
	}

	// There is no remote filter: a place is sent when the intent names one, and
	// the judge decides eligibility.
	location := firstText(intent.Location...)

	// Every source runs for every intent, so inexpressible filters yield nothing.
	if keyword == "" {
		return nil, nil
	}

	input := linkedInInput{
		Keywords:       keyword,
		Location:       location,
		LimitPerSource: intent.Limit,
	}
	if intent.PostedWithinDays > 0 {
		input.DatePosted = linkedInDatePosted
	}

	options := url.Values{
		"maxItems":          {strconv.Itoa(intent.Limit)},
		"maxTotalChargeUsd": {strconv.FormatFloat(linkedInMaxChargeUSD, 'f', 2, 64)},
	}
	items, err := runActor[linkedInItem](ctx, scraper.runner, linkedInActorID, input, options)
	if err != nil {
		return nil, err
	}

	jobs := make([]shared.Job, 0, len(items))
	for _, item := range items {
		if job, ok := item.job(); ok {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

func (item linkedInItem) job() (shared.Job, bool) {
	url := firstText(item.Link, item.ApplyURL)
	title := strings.TrimSpace(item.Title)
	company := strings.TrimSpace(item.CompanyName)
	if url == "" || title == "" || company == "" {
		return shared.Job{}, false
	}

	externalID := scalar(item.ID)
	if externalID == "" {
		externalID = fingerprint(url)
	}

	return shared.Job{
		Source:      LinkedInSource,
		ExternalID:  externalID,
		Platform:    "LinkedIn",
		Company:     company,
		Title:       title,
		Location:    strings.TrimSpace(item.Location),
		URL:         url,
		Description: linkedInDescription(item),
		PostedAt:    strings.TrimSpace(item.PostedAt),
	}, true
}

func linkedInDescription(item linkedInItem) string {
	if text := strings.TrimSpace(item.DescriptionText); text != "" {
		return text
	}
	// The markup carries nothing the judge needs and would inflate the prompt.
	text := htmlTag.ReplaceAllString(item.DescriptionHTML, " ")
	return strings.Join(strings.Fields(text), " ")
}

var htmlTag = regexp.MustCompile(`<[^>]*>`)
