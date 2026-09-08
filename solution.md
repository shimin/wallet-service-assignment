# Solution

On branch fix/solution. In the commit history I tried to keep the failing test and the fix that follows it apart; the tests live in internal/storage and internal/money:

```bash
docker compose up -d postgres nats
go test ./... -count=1
```

## How I worked through it

1. The QA complaint about balances that don't add up after load testing points at races around the balance field.

   1\.1\. HandleBalance makes you want to fix the returned balance first, but along the way it pulls in the transaction log too, because the currency is read by one query while the balance is summed from the transactions table (fixing the returned balance here, the log in item 3)

2. The ticket about money that left one account and never arrived at the other - a non-atomic transfer, two cases:

   2\.1\. Transfer to a wallet that does not exist

   2\.2\. Connection errors or locks in the database

3. Looking at the rest of Deposit/Withdraw/Transfer I noticed the balance update and the transaction record are not atomic either

4. The question from finance, "can a retried request be applied twice", points at request deduplication

5. Opposite transfers A -> B and B -> A started at the same time deadlock. Did not show up right away, only once the test started running transfers in both directions

6. Amounts were not validated at all: a deposit with a negative amount went into balance + (-100), that is, a withdrawal that skips the sufficient funds check, and the balance went negative. And 1e500 crashed json.Unmarshal, after which the message was simply dropped - no completed, no failed, the sender waited for a timeout

## Fixes

1. HandleBalance returns the wallets.balance field, not the sum from the transaction log

2. Remove the races on balance updates - and keep them from coming back:

   2\.1\. The race tests show the problem with the wallets.balance field

   ```bash
   docker compose up -d postgres
   go test ./internal/storage/ -count=10 -timeout 300s
   ```

   2\.2\. Raising the transaction isolation level was rejected as expensive in time, in retry handling, and generally awkward

   2\.3\. Withdraw and Transfer updated the balance non-atomically - the check now happens in a single statement inside debit:

   ```sql
   UPDATE wallets SET balance = balance - $1, updated_at = NOW()
   WHERE wallet_id = $2 AND balance >= $1
   ```

   2\.4\. The deadlock on opposite transfers is cured by a deterministic lock order: lockWallets takes both wallets in one SELECT ... ORDER BY wallet_id FOR UPDATE, after which the order of debit and credit no longer matters

3. Write to the transactions log inside the same transaction as Deposit/Withdraw/Transfer

4. Add a requests table guarded by ON CONFLICT (request_id) DO NOTHING:
The alternative was an idempotency_key column on the same table; a separate table was chosen so the two things don't get mixed up

   4\.1\. Existing rows are untouched, the migration only creates an empty table and does not block production, and duplicates are gone from here on

   4\.2\. claimRequest in front of Deposit/Withdraw/Transfer blocks or skips duplicate requests

   4\.3\. Added payload_fingerprint for stricter protection against the same request_id arriving with a different payload

5. Propose a way for finance to clean up the discrepancies that have already accumulated:

   5\.1\. No row in transactions: if money did move, the balance is the truth and a correcting entry goes into transactions

   5\.2\. The balance lost an update: transactions are the truth, the balance is recomputed from them and reconciled

   5\.3\. A request applied twice: find duplicates in transactions, fix with a correcting entry

   5\.4\. Half a transfer: two wallets diverged by the same amount - the pair needs manual review

   Which makes you want a separate reconciliation service (out of scope here):

   - an entries view splitting a transfer into two records
   - a wallet_reconciliation view: the full list of problem wallets with the sign of the discrepancy
   - duplicate search with GROUP BY request_id HAVING count(*) > 1
   - corrections as a new row with a separate adjustment operation in transactions
   - a reconciliation_runs snapshot table (run_at, wallet_id, balance, ledger, diff)

There is no history of balance changes, failed operations were never recorded at all (no status='failed'), and NATS events are not persisted. So for old discrepancies we can only match totals, not replay the sequence. For 5.1 the `request_id` and the counterparty of the missing row cannot be recovered - only a correcting entry marked "discrepancy, found by such and such reconciliation".

### Then the "less critical" problems, the ones that apparently never fired in production, which does not make them any less critical going forward:

6. The float64 rounding errors are solved by the new money package (would like a better name), the literal from JSON -> money.Amount -> Value() -> text -> NUMERIC(18,4) and back — text -> Scan -> Amount

   6\.1\. The amount check (positive, representable at scale 4 without rounding, no larger than 99999999999999.9999) lives in the store - where the money actually moves, so the guarantee does not depend on the caller

   6\.2\. In the handlers there is a parse layer: it decodes the payload, calls validate() and on refusal publishes failed instead of dropping the message silently. The request_id for that event is dug out of the envelope separately - the typed decode fails as a whole on a malformed amount, and the field order in the JSON is up to the client

   6\.3\. The idempotency fingerprint is built from the canonical StringFixed(4) rather than FormatFloat: it used to round along with the database, so two different requests under one request_id passed as an honest retry

7. Cross-currency transfers were not blocked; added validation, the currency is passed through, and requireSameCurrency runs inside the transaction

   7\.1\. requireSameCurrency compares the currencies of the two wallets under the locks already taken, requireCurrency checks the currency from the request against the wallet's

   7\.2\. In Deposit the wallet may not exist yet, so the check is built into the upsert itself (WHERE $3 = '' OR wallets.currency = $3), and affected == 0 means a mismatch

   7\.3\. The handler checks the format of the code: ^[A-Z]{3}$ or empty

   7\.4\. The currency went into payload_fingerprint - otherwise a retry with the same request_id and a different currency reads as an honest retry

8. The config filled in defaults silently: with NATS_URL and PG_URL empty the service went to localhost with dev credentials baked into the code. In production a lost environment variable would not stop the service, it would send it somewhere else. Load() now returns an error, main exits on it at startup, and the docker-compose credentials moved into the test fixture - go test still runs without any environment variables, while running the service locally needs them set:

   ```bash
   set -a; source .env; set +a
   go run ./cmd/wallet-service
   ```

## Contract changes

The message schema did not change: amount is still a JSON number, and so is balance in the wallet.balance response (printed as 150.0000). But the behaviour at the boundary did, and it is worth checking against the senders before a rollout:

- an amount below 0.0001 is now rejected instead of being rounded silently. A client that sends dust today and gets it rounded up will start getting a refusal. Silent rounding of money is worse than an explicit refusal, but if there are logs it is worth a look at whether such senders exist
- amount is also accepted as a decimal string - for clients whose own serialisation goes through a float
- Withdraw with an explicit currency against a wallet that does not exist returns "wallet not found", it used to be "insufficient funds"

## Tests

Every problem is first shown by a failing test, then closed by a fix:

| Problem | Tests | Commits |
|---|---|---|
| Races on the balance | TestConcurrentWithdraws, TestWithdrawAndTransfer | 7eb36ab → 4433b28 |
| Deadlock on opposite transfers | TestOppositeTransfers | a3fd516 → 2b79012 |
| A request applied twice | TestRetriedRequestAppliedOnce, TestConcurrentRetriesOfSameTransfer, TestConcurrentRetriesOfSameRequest, TestReusedRequestIDWithDifferentPayload | 77920b1, 2adc26a → d5e55a5, 611ee12 |
| Precision and rounding | TestDepositFinerThanScaleIsRefused, TestWithdrawalFinerThanScaleIsRefused, TestLargeAmountsSurviveTheRoundTrip, TestSmallestAmountsAccumulateExactly, money_test.go | c9506e6 → d5b2af6, a08aa83, 09aa9e2 |
| Cross-currency operations | TestTransferBetweenCurrenciesIsRefused, TestOperationInForeignCurrencyIsRefused | 0c17b69 → 30c1999 |

## Assumptions

- Production runs a single instance, so I assume the plain NATS nc.Conn.Subscribe does not have to become a QueueSubscribe with a shared queue group yet (worth adding once we decide to scale the service out)

- HandleBalance returned the sum over the wallet's transaction rows instead of the wallets.balance field - looks like an unfinished plan for the future; the quickest and most urgent fix is to return the balance field, since the check for whether a transfer is possible was done against exactly that field

  ```go
  SELECT balance FROM wallets...
  if balance < amount {
      return fmt.Errorf("insufficient funds")
  }
  ```

- even though restoring the balance from transactions is semantically more correct, given the current state I treat wallets.balance as the source of truth, because a debit or credit is applied to that field first and writing the transaction row could fail - the audit of discrepancies in the log will be done relative to the balance field

- an empty currency I read as the client meaning the wallet's own currency. Otherwise every sender that does not send the field today gets a refusal. A cross-currency transfer is still blocked - the wallets are compared against each other

- a wallet created by its first deposit takes the currency from the request, USD if it is not given

- only 'completed' is ever written to the status field - I assume it has always been that way, worth checking in production with SELECT status, count(*) FROM transactions GROUP BY status;

## Not done

- The status column is effectively useless - always 'completed', it does not tell anything apart, and failed operations never reach the database at all, they are only published to the bus. After moving the operations into a single transaction a refusal leaves no trace in requests either: claimRequest is rolled back along with the operation. Recording the attempt would need a separate transaction after the rollback - and whether we want a log of attempts at all is a question in itself, so I left it
