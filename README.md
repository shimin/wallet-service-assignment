# Senior Backend Developer — Take-Home Assignment

## Overview

You've just inherited `wallet-service` — a Go service that manages user balances and processes
financial operations (deposits, withdrawals, transfers) via NATS messaging, with PostgreSQL as the
data store. The team that originally built it has moved on to other projects.

The service works — it starts, connects to NATS and PostgreSQL, and processes messages. It's also
been in production for a while, and it shows: a few different hands have touched it, deadlines got
tight in places, and it was never really revisited after it shipped.

Lately that's starting to show up as complaints. QA has flagged balance numbers that don't look
right after load testing. Support has fielded a ticket about a transfer where the money left one
account and never showed up in the other. Someone on the finance side asked, offhand, whether a
retried request could end up applied twice. Nobody has had the time to sit down and actually work
through it — that's you now.

**Time expectation:** 4–6 hours. We value quality of judgment and depth of analysis over quantity.

---

## System Description

### Wallet Service

- Listens on NATS subjects:
  - `wallet.deposit` — add funds to a wallet
  - `wallet.withdraw` — remove funds from a wallet
  - `wallet.transfer` — move funds between two wallets
  - `wallet.balance` — get current balance (request-reply)
- Stores data in PostgreSQL
- Publishes events to NATS:
  - `wallet.events.completed` — successful operation
  - `wallet.events.failed` — failed operation

### Message Schemas

**`wallet.deposit` / `wallet.withdraw`**

```json
{
  "request_id": "string (UUID)",
  "wallet_id": "string (UUID)",
  "amount": "number (> 0)",
  "currency": "string (e.g. USD)"
}
```

**`wallet.transfer`**

```json
{
  "request_id": "string (UUID)",
  "from_wallet_id": "string (UUID)",
  "to_wallet_id": "string (UUID)",
  "amount": "number (> 0)",
  "currency": "string (e.g. USD)"
}
```

**`wallet.balance`** (request-reply)

```json
// Request
{ "wallet_id": "string (UUID)" }

// Response
{ "wallet_id": "...", "balance": 150.00, "currency": "USD" }
```

**`wallet.events.completed` / `wallet.events.failed`**

```json
{
  "request_id": "string (UUID)",
  "operation": "deposit | withdraw | transfer",
  "status": "completed | failed",
  "reason": "string | null",
  "timestamp": "ISO 8601"
}
```

---

## Getting Started

### Prerequisites
- Go 1.22+
- Docker and Docker Compose

### Run Everything

```bash
docker-compose up -d --build
```

This starts:
- **NATS** on port `4222` (monitoring on `8222`)
- **PostgreSQL** on port `5432` (user: `walletuser`, password: `walletpass`, database: `wallet`)
- **wallet-service** — built from the `Dockerfile`, started once NATS and PostgreSQL are healthy

There are no pre-seeded wallets — `init.sql` only creates the schema. A wallet is created
automatically on its first deposit, so send a `wallet.deposit` for a fresh `wallet_id` to bring an
account into existence.

### Running the Service Locally (for development)

When you're actively changing the code it's usually quicker to run just the infrastructure in
Docker and run the service from your shell:

```bash
docker-compose up -d nats postgres
go run ./cmd/wallet-service
```

(Stop the containerised service first — `docker-compose stop wallet-service` — so you don't have two
instances processing the same subjects.)

The service reads its configuration from environment variables (see `.env` for defaults — `NATS_URL`,
`PG_URL`). Inside Compose these point at the `nats` and `postgres` service names; running locally they
fall back to `localhost`.

### Database Schema

Created on startup:

```sql
CREATE TABLE wallets (
    wallet_id UUID PRIMARY KEY,
    balance NUMERIC(18,4) NOT NULL DEFAULT 0,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE transactions (
    id SERIAL PRIMARY KEY,
    request_id UUID,
    operation VARCHAR(20) NOT NULL,
    from_wallet UUID,
    to_wallet UUID,
    amount NUMERIC(18,4) NOT NULL,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);
```

---

## Your Task

This is deliberately open-ended. We are not going to tell you what to look for, what's wrong, or how
to go about it — working that out for yourself, and deciding what to do about it, is the exercise.

Spend real time with the service and form your own view of where it stands. Then do whatever you
judge to be the right thing to do about it.

Submit your work with a README explaining what you found, what you decided to do, and the reasoning
behind those choices. Commit as you go rather than dropping everything in one final commit.

---

## What We Evaluate

| Area | What we look for |
|---|---|
| **Problem diagnosis** | How much of what's actually wrong you uncover, including the less obvious issues |
| **Judgment & prioritization** | Whether your read on severity and your choice of approach make sense for a live financial system on a time box |
| **Fix quality** | Correctness, simplicity, and production-readiness of whatever you actually ship |
| **Financial thinking** | Whether you reason about correctness the way a system handling real money demands |
| **Testing** | Tests that prove a problem was real and that your change actually closes it |
| **Code quality** | Clean Go code, error handling, naming, structure |
| **Communication** | A clear README explaining what you found, what you decided, and why |

---

## Deliverables

- A repository (or archive) with your changes to the service
- Tests that demonstrate the problems you identified were real and that your changes address them
- A README with your findings, your approach, and your reasoning
- The code should run against the provided docker-compose setup

---

## Notes

- You have full latitude over the code — keep the structure as it is or change it however you see
  fit, as long as you explain any significant decisions in your README.
- We will review your commit history alongside the final result, so keep it legible.
- If you run out of time, be clear about what you didn't get to and why — that's more useful to us
  than a rushed attempt at everything.
- If something about the intended behavior is ambiguous, document your interpretation and
  assumptions in your README rather than guessing what we want.
