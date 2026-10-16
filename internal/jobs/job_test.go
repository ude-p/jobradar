package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"

	"jobradar/internal/ent"
	"jobradar/internal/ent/job"
	"jobradar/internal/judge"
	"jobradar/internal/shared"
)

// fakeSource returns listings the harvester accepts, so they count against the
// budget. With repeat set they all share one identity, which is how a source
// returning mostly already-stored postings is simulated.
type fakeSource struct {
	name    string
	results int
	repeat  bool
	limits  []int
}

func (source *fakeSource) Name() string { return source.name }

func (source *fakeSource) Search(_ context.Context, intent shared.Intent) ([]shared.Job, error) {
	source.limits = append(source.limits, intent.Limit)

	count := min(source.results, intent.Limit)
	found := make([]shared.Job, 0, count)
	for index := range count {
		externalID := fmt.Sprintf("%s-%d", source.name, index)
		if source.repeat {
			externalID = source.name + "-0"
		}
		found = append(found, shared.Job{
			Source:     source.name,
			ExternalID: externalID,
			Company:    "Acme",
			Title:      "Platform Engineer",
			URL:        fmt.Sprintf("https://example.com/%s/%d", source.name, index),
		})
	}
	return found, nil
}

func testDB(t *testing.T) *ent.Client {
	t.Helper()

	database, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatal(err)
	}

	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, database)))
	t.Cleanup(func() {
		_ = client.Close()
	})

	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	return client
}

// answering stands in for the judge: the budget counts postings that reached it,
// whether or not it approved them.
type answering struct{}

func (answering) Evaluate(context.Context, shared.Criteria, shared.Job) (judge.Verdict, error) {
	return judge.Verdict{Outcome: judge.OutcomeApproved}, nil
}

func harvest(t *testing.T, client *ent.Client, budget int, search shared.Intent, sources ...shared.Source) {
	t.Helper()

	err := Harvest(context.Background(), client, Request{
		Queue:     shared.Queue{Name: "test", Search: search},
		Limit:     budget,
		Sources:   sources,
		Evaluator: answering{},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHarvestStopsAtTheFetchTarget(t *testing.T) {
	first := &fakeSource{name: "first", results: 6}
	second := &fakeSource{name: "second", results: 6}

	harvest(t, testDB(t), 6, shared.Intent{}, first, second)

	if len(first.limits) != 1 || first.limits[0] != 6 {
		t.Fatalf("first source limits = %v, want [6]", first.limits)
	}
	if len(second.limits) != 0 {
		t.Fatalf("second source was asked for %v, want it skipped once the budget is met", second.limits)
	}
}

func TestHarvestGivesTheRemainderToTheNextSource(t *testing.T) {
	first := &fakeSource{name: "first", results: 2}
	second := &fakeSource{name: "second", results: 6}

	harvest(t, testDB(t), 6, shared.Intent{}, first, second)

	if len(first.limits) != 1 || first.limits[0] != 6 {
		t.Fatalf("first source limits = %v, want [6]", first.limits)
	}
	if len(second.limits) != 1 || second.limits[0] != 4 {
		t.Fatalf("second source limits = %v, want [4]", second.limits)
	}
}

// A page of postings we already hold fills none of the budget, so the chain has
// to continue. Counting returned listings instead of accepted ones ended the
// chain early and is the bug this guards.
func TestHarvestCountsAcceptedNotReturned(t *testing.T) {
	client := testDB(t)
	_, err := client.Job.Create().
		SetSource("first").
		SetQueue("test").
		SetExternalID("first-0").
		SetURL("https://example.com/first/0").
		SetCompany("Acme").
		SetTitle("Platform Engineer").
		SetDecision(job.DecisionRejected).
		Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	first := &fakeSource{name: "first", results: 6, repeat: true}
	second := &fakeSource{name: "second", results: 6}

	harvest(t, client, 6, shared.Intent{}, first, second)

	if len(first.limits) != 1 || first.limits[0] != 6 {
		t.Fatalf("first source limits = %v, want [6]", first.limits)
	}
	if len(second.limits) != 1 || second.limits[0] != 6 {
		t.Fatalf("second source limits = %v, want [6]: an all-duplicate page must not fill the budget", second.limits)
	}
}
