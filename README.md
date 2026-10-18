# jobradar

Finds job postings, judges them with typesafe jev, and sends the good ones to Telegram.

This started as a personal tool and grew into something other people can reuse. The searches and criteria are hardcoded in `cmd/main.go` on purpose. Treat the repository as a base for your own job search service. The example queues show how to combine the query fields with the judging criteria, and you can change both to look for whatever you want.

## How it works

Two sources are tried cheapest first.

- **freehire** is free, paginated, and indexes around 165 ATS boards. It runs first and fills the budget.
- **LinkedIn, via Apify** is billed per result and cannot be paged, so it only runs for what freehire could not supply.

Every posting that has not been seen before goes to typesafe jev, which answers a couple of yes or no questions about it. Postings that pass are sent to Telegram in batches.

Everything seen is stored in SQLite, keyed by source and posting id, so repeat runs do not re-judge or re-buy what you have already looked at.

I run it twice a day. The binary runs one pass over the queues, so the schedule lives outside it, in cron or launchd.

## Query fields

A queue starts with an `Intent`, the part the sources understand. Not every source uses every field.

| field | value | used by |
|---|---|---|
| `Terms` | free text | freehire (`q`), LinkedIn (`keywords`) |
| `Skills` | e.g. `go`, `kubernetes` | freehire |
| `Categories` | values the source understands, e.g. `backend` | freehire |
| `Seniority` | values the source understands | freehire |
| `Regions` | e.g. `global` | freehire |
| `Countries` | ISO 3166 alpha-2, lowercase, e.g. `ng` | freehire |
| `Location` | free text place name, e.g. `Nigeria` | LinkedIn |
| `WorkMode` | `remote` or `relocation` | freehire (`remote` only) |
| `PostedWithinDays` | integer | freehire |
| `Limit` | internal candidate budget | harvester |

`Limit` is not how many jobs are delivered. It is how many candidates the harvester may fetch and judge. Delivery is capped separately at ten per queue.

## Criteria

The criteria are what the judge sees. Each one has a `Context` string that describes the candidate, and a set of questions.

Questions are `choice` type. Every answer option is defined in the question's `Criteria` map, so the model chooses between your descriptions instead of inventing its own.

A posting is approved only when both hold. The `stack_fit` answer must be `yes`, and one eligibility question must be `yes`. Each approving answer also has to reach `0.7` confidence. The model reports that confidence itself, so the check ranks decisiveness, it does not guarantee correctness.

The built-in eligibility questions are:

- `remote_global`, can a candidate in Nigeria work remotely for an employer in the target markets
- `local_role`, can a candidate in Lagos take the job without moving abroad
- `relocation_supported`, does the employer offer relocation or visa sponsorship for a role in the target markets

`stack_fit` is defined per queue in `cmd/main.go`, because the answer depends on the candidate.

A queue is composed like this.

```go
shared.NewQueue().
    Name("platform").
    Skills("go", "kubernetes").
    Categories("backend", "devops").
    Regions("global").
    WorkMode(shared.Remote).
    PostedWithinDays(7).
    Criteria(shared.Crit().
        Context(pltfrmContext).
        Questions(shared.RemoteGlobal, pltfrmFit)).
    ChatIDs(chatID)
```

To search for something else, change the fields and the criteria in `cmd/main.go`. To add a source, implement `shared.Source`, which is one method.

## Example queues

Five queues ship with the repository. They are examples, not a fixed feature set.

| queue | looks for |
|---|---|
| `platform` | global remote, Go and Kubernetes |
| `platform-relocation` | EU roles offering relocation |
| `platform-local` | Nigerian companies, any work mode |
| `care-pharma` | global remote, pharmacy and clinical operations |
| `care-graduate-local` | Nigerian graduate trainee roles |

## Running it

Create an `.env` with these variables. All of them are required at startup.

| variable | purpose |
|---|---|
| `DATABASE_PATH` | SQLite file |
| `APIFY_TOKEN` | LinkedIn actor |
| `TELEGRAM_TOKEN` | Telegram bot |
| `JUDGE_API_KEY` | typesafe jev |
| `PLATFORM_CHAT_ID` | chat for the platform queues |
| `CARE_CHAT_ID` | chat for the care queues |

