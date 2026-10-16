package jobs

import (
	"context"
	"log"
	"strings"
	"time"

	"jobradar/internal/ent"
	"jobradar/internal/ent/job"
	"jobradar/internal/judge"
	"jobradar/internal/shared"
)

// Evaluator is what Harvest judges a listing with, defined here so a test can
// substitute one.
type Evaluator interface {
	Evaluate(ctx context.Context, criteria shared.Criteria, job shared.Job) (judge.Verdict, error)
}

type Request struct {
	Queue shared.Queue
	// Limit is how many new candidates to fetch and judge across all sources.
	Limit     int
	Sources   []shared.Source
	Evaluator Evaluator
}

// Harvest tries sources in order, so a costlier one is only reached for what a
// cheaper one failed to supply.
func Harvest(ctx context.Context, db *ent.Client, request Request) error {
	h := &harvester{
		db:      db,
		request: request,
		seen:    make(map[string]struct{}),
	}

	// Counted as judged, not returned: an all-duplicate page fills none of the budget.
	judged := 0
	for _, source := range request.Sources {
		if judged >= request.Limit {
			break
		}

		search := request.Queue.Search
		search.Limit = request.Limit - judged

		found, err := source.Search(ctx, search)
		if err != nil {
			log.Printf("%s: %v", source.Name(), err)
			continue
		}

		for _, listing := range found {
			if judged >= request.Limit {
				break
			}
			accepted, err := h.process(ctx, source, listing)
			if err != nil {
				return err
			}
			if accepted {
				judged++
			}
		}
	}

	return nil
}

func Pending(ctx context.Context, db *ent.Client, queue string, limit int) ([]*ent.Job, error) {
	return db.Job.Query().
		Where(job.QueueEQ(queue), job.DecisionEQ(job.DecisionApproved), job.DeliveredAtIsNil()).
		Order(ent.Desc(job.FieldDiscoveredAt)).
		Limit(limit).
		All(ctx)
}

func MarkDelivered(ctx context.Context, db *ent.Client, delivered []*ent.Job) error {
	if len(delivered) == 0 {
		return nil
	}

	ids := make([]int, 0, len(delivered))
	for _, item := range delivered {
		ids = append(ids, item.ID)
	}

	return db.Job.Update().
		Where(job.IDIn(ids...)).
		SetDeliveredAt(time.Now().UTC()).
		Exec(ctx)
}

type harvester struct {
	db      *ent.Client
	request Request
	seen    map[string]struct{}
}

// Every check that can drop a listing runs before the judge, so judge spend stays
// proportional to new postings.
func (h *harvester) process(ctx context.Context, source shared.Source, listing shared.Job) (bool, error) {
	if listing.ExternalID == "" || strings.TrimSpace(listing.URL) == "" {
		return false, nil
	}
	if strings.TrimSpace(listing.Company) == "" || strings.TrimSpace(listing.Title) == "" {
		return false, nil
	}

	key := listing.Source + "\x00" + listing.ExternalID
	if _, ok := h.seen[key]; ok {
		return false, nil
	}
	h.seen[key] = struct{}{}

	exists, err := h.db.Job.Query().
		Where(job.SourceEQ(listing.Source), job.ExternalIDEQ(listing.ExternalID)).
		Exist(ctx)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}

	verdict, err := h.request.Evaluator.Evaluate(ctx, h.request.Queue.Criteria, listing)
	if err != nil {
		log.Printf("%s judge %s: %v", source.Name(), listing.ExternalID, err)
		return false, nil
	}

	decision := job.DecisionRejected
	if verdict.Outcome != judge.OutcomeRejected {
		decision = job.DecisionApproved
	}

	details := verdict.Details
	if details == nil {
		details = map[string]any{}
	}
	if len(listing.Skills) > 0 {
		details["skills"] = listing.Skills
	}
	if listing.PostedAt != "" {
		details["posted_at"] = listing.PostedAt
	}
	if listing.Platform != "" {
		details["platform"] = listing.Platform
	}

	_, err = h.db.Job.Create().
		SetSource(listing.Source).
		SetQueue(h.request.Queue.Name).
		SetExternalID(listing.ExternalID).
		SetURL(strings.TrimSpace(listing.URL)).
		SetCompany(strings.TrimSpace(listing.Company)).
		SetTitle(strings.TrimSpace(listing.Title)).
		SetLocation(strings.TrimSpace(listing.Location)).
		SetDecision(decision).
		SetDetails(details).
		Save(ctx)
	if err != nil {
		return false, err
	}

	return true, nil
}
