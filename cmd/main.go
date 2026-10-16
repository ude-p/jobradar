package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"

	"jobradar/internal/clients"
	"jobradar/internal/ent"
	"jobradar/internal/jobs"
	"jobradar/internal/judge"
	"jobradar/internal/shared"
)

const (
	fetchTarget  = 20
	deliverBatch = 10
)

type config struct {
	databasePath   string
	apifyToken     string
	telegramToken  string
	judgeAPIKey    string
	platformChatID string
	careChatID     string
}

func loadConfig() config {
	cfg := config{
		databasePath:   os.Getenv("DATABASE_PATH"),
		apifyToken:     os.Getenv("APIFY_TOKEN"),
		telegramToken:  os.Getenv("TELEGRAM_TOKEN"),
		judgeAPIKey:    os.Getenv("JUDGE_API_KEY"),
		platformChatID: os.Getenv("PLATFORM_CHAT_ID"),
		careChatID:     os.Getenv("CARE_CHAT_ID"),
	}

	for _, required := range []struct{ name, value string }{
		{"DATABASE_PATH", cfg.databasePath},
		{"APIFY_TOKEN", cfg.apifyToken},
		{"TELEGRAM_TOKEN", cfg.telegramToken},
		{"JUDGE_API_KEY", cfg.judgeAPIKey},
		{"PLATFORM_CHAT_ID", cfg.platformChatID},
		{"CARE_CHAT_ID", cfg.careChatID},
	} {
		if required.value == "" {
			log.Fatalf("%s is not set; source .env", required.name)
		}
	}

	return cfg
}

type app struct {
	db        *ent.Client
	telegram  *clients.Telegram
	evaluator *judge.Judge
	sources   []shared.Source
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := loadConfig()

	database, err := sql.Open("sqlite", cfg.databasePath+"?_fk=1&_pragma=busy_timeout(10000)")
	if err != nil {
		log.Fatal(err)
	}

	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, database)))
	defer func() {
		_ = client.Close()
	}()

	if err := client.Schema.Create(ctx); err != nil {
		log.Fatal(err)
	}

	application := &app{
		db:        client,
		telegram:  clients.NewTelegram(cfg.telegramToken),
		evaluator: judge.New(cfg.judgeAPIKey),
		// Cheapest first: a paid source is only reached for what freehire could not supply.
		sources: []shared.Source{clients.NewFreehire(), clients.NewLinkedIn(cfg.apifyToken)},
	}

	queues := []shared.Queue{
		pltfrmRemote(cfg.platformChatID),
		pltfrmReloc(cfg.platformChatID),
		pltfrmLocal(cfg.platformChatID),
		careRemote(cfg.careChatID),
		careLocal(cfg.careChatID),
	}

	application.tick(ctx, queues)
}

func pltfrmRemote(chatID string) shared.Queue {
	return shared.NewQueue().
		Name("platform").
		Skills("go", "kubernetes").
		Categories("backend", "devops").
		Regions("global").
		WorkMode(shared.Remote).
		PostedWithinDays(7).
		Criteria(pltfrmRemoteCrit).
		ChatIDs(chatID)
}

// The apify actors look for a place name where freehire looks for a code.
func pltfrmReloc(chatID string) shared.Queue {
	return shared.NewQueue().
		Name("platform-relocation").
		Skills("go", "kubernetes").
		Regions("eu").
		Location("European Union").
		WorkMode(shared.Relocation).
		PostedWithinDays(7).
		Criteria(pltfrmRelocCrit).
		ChatIDs(chatID)
}

func pltfrmLocal(chatID string) shared.Queue {
	return shared.NewQueue().
		Name("platform-local").
		Skills("go", "kubernetes").
		Categories("backend", "devops").
		Countries("ng").
		Location("Nigeria").
		PostedWithinDays(7).
		Criteria(pltfrmLocalCrit).
		ChatIDs(chatID)
}

func careRemote(chatID string) shared.Queue {
	return shared.NewQueue().
		Name("care").
		Terms("customer success").
		Regions("global").
		WorkMode(shared.Remote).
		PostedWithinDays(7).
		Criteria(careRemoteCrit).
		ChatIDs(chatID)
}

func careLocal(chatID string) shared.Queue {
	return shared.NewQueue().
		Name("care-local").
		Terms("customer success").
		Countries("ng").
		Location("Nigeria").
		PostedWithinDays(7).
		Criteria(careLocalCrit).
		ChatIDs(chatID)
}

func (a *app) tick(ctx context.Context, queues []shared.Queue) {
	for _, item := range queues {
		pending, err := jobs.Pending(ctx, a.db, item.Name, deliverBatch)
		if err != nil {
			log.Printf("%s: %v", item.Name, err)
			continue
		}

		// Refill only when dry, so a stalled delivery cannot keep buying unsendable jobs.
		if len(pending) == 0 {
			if !online(ctx) {
				continue
			}

			if err := jobs.Harvest(ctx, a.db, jobs.Request{
				Queue:     item,
				Limit:     fetchTarget,
				Sources:   a.sources,
				Evaluator: a.evaluator,
			}); err != nil {
				log.Printf("%s: harvest: %v", item.Name, err)
				continue
			}

			pending, err = jobs.Pending(ctx, a.db, item.Name, deliverBatch)
			if err != nil {
				log.Printf("%s: %v", item.Name, err)
				continue
			}
		}

		if len(pending) == 0 {
			log.Printf("%s: no new jobs", item.Name)
			continue
		}

		if err := a.telegram.Send(ctx, item.Name, item.ChatIDs, pending); err != nil {
			log.Printf("%s: %v", item.Name, err)
			continue
		}

		if err := jobs.MarkDelivered(ctx, a.db, pending); err != nil {
			log.Printf("%s: mark delivered: %v", item.Name, err)
			continue
		}
		log.Printf("%s: sent %d new jobs", item.Name, len(pending))
	}
}

func online(ctx context.Context) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://api.typesafe.ai", nil)
	if err != nil {
		log.Printf("connectivity check: %v", err)
		return false
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		log.Printf("offline: %v", err)
		return false
	}
	_ = response.Body.Close()
	return true
}

var (
	pltfrmContext = "Platform and infrastructure engineer (4+ years, Lagos, Nigeria). Language & data: Go, PostgreSQL, TimescaleDB, Redis. Cloud & infrastructure: AWS (EC2, EBS, VPC, S3, CloudFront, Route 53, IAM, SSM, CloudWatch), OpenTofu. Kubernetes & platform: Kubernetes, k3s, controller-runtime, client-go, CRDs, Operators, Helm, Cilium, Longhorn, CloudNativePG. Observability & networking: OpenTelemetry, Grafana, pprof, Pyroscope, gRPC, WebSockets, Protocol Buffers, NATS. CI/CD & automation: GitHub Actions, Ansible, Docker, ko, GHCR. Owns backend services, compute, storage, networking, deployments, and production operations."
	careContext   = "Candidate seeking remote roles in pharmacy operations, clinical research, customer success, operations, or project coordination."

	pltfrmFit = shared.Choice("stack_fit",
		"Is the core of this role in the candidate's field: backend, infrastructure, platform, or reliability engineering built on Go and Kubernetes, with cloud (AWS), infrastructure-as-code, Linux, networking, distributed systems, or observability?",
		map[string]string{
			"yes": "The core work clearly overlaps the candidate's stack: Go; Kubernetes/platform (operators, Helm, k3s); cloud/infrastructure (AWS); infrastructure-as-code; platform/SRE/DevOps; backend or distributed systems; observability; Linux, networking, CI/CD, or automation.",
			"no":  "Everything else. The core work is a different primary language (Java, .NET, Python, Ruby, PHP, JS/TS, C/C++) or a non-engineering discipline (sales, marketing, product management, data science, ML research, security-only, QA, management) with no meaningful overlap, or the description is too thin or missing to tell what the core work is.",
		})

	careFit = shared.Choice("stack_fit",
		"Is this role in one of the candidate's target areas: pharmacy or clinical operations, customer success, operations, project coordination, or quality assurance?",
		map[string]string{
			"yes": "Role is clearly in pharmacy/clinical operations, customer success, operations, project coordination, or QA.",
			"no":  "Everything else. The role is in a different field with no meaningful overlap, or the description is too thin or missing to tell.",
		})

	pltfrmRemoteCrit = shared.Crit().Context(pltfrmContext).Questions(shared.RemoteGlobal, pltfrmFit)
	pltfrmRelocCrit  = shared.Crit().Context(pltfrmContext).Questions(shared.RelocationSupported, pltfrmFit)
	pltfrmLocalCrit  = shared.Crit().Context(pltfrmContext).Questions(shared.LocalRole, pltfrmFit)
	careRemoteCrit   = shared.Crit().Context(careContext).Questions(shared.RemoteGlobal, careFit)
	careLocalCrit    = shared.Crit().Context(careContext).Questions(shared.LocalRole, careFit)
)
