# jobradar

Finds job postings, scores them with typesafe jev, and sends the good ones to
Telegram twice a day.

## How it works

Two sources, tried cheapest first:

- **freehire** — free and paginated, indexes ~165 ATS boards. Runs first and
  fills the budget.
- **LinkedIn, via Apify** — billed per result and can't be paged, so it only
  runs for whatever freehire couldn't supply.

Anything new goes to typesafe jev, which answers a couple of yes/no questions
about the posting — is it open to someone in Nigeria, does the stack match.
Postings that pass get sent to Telegram in batches.

Everything seen is stored in SQLite, keyed by source and posting id, so repeat
runs don't re-judge or re-buy what they've already looked at.

## Queues

Five independent searches, each with its own filters, questions and chat:

| queue | what it looks for |
|---|---|
| `platform` | global remote, Go/Kubernetes |
| `platform-relocation` | EU roles offering relocation |
| `platform-local` | Nigerian companies, any work mode |
| `care` | global remote |
| `care-local` | Nigerian companies, any work mode |
