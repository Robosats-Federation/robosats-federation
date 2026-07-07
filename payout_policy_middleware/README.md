# RoboSats LND Payout Policy Middleware

Standalone policy gate for outgoing RoboSats LND payouts.

The Docker build selects the Go LND RPC bindings from the `LND_VERSION` build
argument, for example `v0.21.0-beta`. The build verifies the selected
`github.com/lightningnetwork/lnd` module version before compiling the service.

It exposes guarded payout endpoints:

```text
POST /v1/robosats/lnd/send-payment-v2
POST /v1/robosats/lnd/send-coins
POST /v1/robosats/lnd/send-keysend
POST /v1/robosats/lnd/send-reward
POST /v1/robosats/rewards/source
```

It also exposes optional LND adapter endpoints so RoboSats can remove direct
LND RPC access from more of `api/lightning/lnd.py`:

```text
GET  /v1/robosats/lnd/version
POST /v1/robosats/lnd/decode-payreq
POST /v1/robosats/lnd/estimate-fee
GET  /v1/robosats/lnd/wallet-balance
GET  /v1/robosats/lnd/channel-balance
POST /v1/robosats/lnd/hold-invoice
POST /v1/robosats/lnd/cancel-invoice
POST /v1/robosats/lnd/settle-invoice
POST /v1/robosats/lnd/lookup-invoice
```

RoboSats sends payout intent and context. The middleware independently reloads
RoboSats Postgres rows, enforces policy, then calls LND only if the request is
consistent.

It is intentionally outside the RoboSats codebase so the RoboSats patch stays
small across upstream upgrades.

## What It Checks

Lightning payouts:

```text
service token
amount_sats < MAX_PAYOUT_SATS
payment_hash and BOLT11 invoice match
invoice amount and expiry
escrow is settled (RoboSats DB status + LND hold invoice SETTLED)
escrow sats >= payout amount
RoboSats order/payout rows
buyer receiver identity
routing budget within limits
no settled dispute bonds (maker/taker bond invoices not SETTLED in LND)
LND TrackPaymentV2 idempotency
```

On-chain payouts:

```text
service token
amount_sats < MAX_ONCHAIN_PAYOUT_SATS
amount >= dust floor (20_000 sats)
fee rate within 1-500 sat/vbyte
address validated by bitcoind validateaddress
label matches "Payout order #<order_id>"
mining fee matches formula (fee_rate * 280, within 1 sat tolerance)
sent amount matches formula (num_sats - swap_fee - mining_fee, within 1 sat tolerance)
escrow is settled (RoboSats DB status + LND hold invoice SETTLED)
escrow sats >= gross payout
RoboSats order/on-chain payout rows
buyer receiver identity
order is successful/queued for on-chain payout (status 14, onchain_status 5)
payout not already broadcasted
bitcoind getblockchaininfo (read-only, also used by /readyz)
middleware audit-table idempotency before SendCoins
```

Devfund keysend payouts:

```text
service token
target_pubkey equals DEVFUND_PUBKEY
compressed pubkey shape
devfund message format, order id, donation fraction, allowed reason
order exists and proceeds cover the donation
amount_sats < MAX_DEVFUND_KEYSEND_SATS
fee_limit_sat matches RoboSats devfund formula
timeout_seconds matches DEVFUND_KEYSEND_TIMEOUT_SECONDS
middleware audit-table idempotency before SendPaymentV2
```

Reward withdrawals:

```text
service token
amount_sats > 0 and < MAX_REWARD_WITHDRAWAL_SATS
fee_limit_sat >= 0
timeout_seconds between 1 and 300
request_id is present
reward LNPayment (concept=WITHREWA) exists in RoboSats DB
payment_hash, invoice, amount, routing_budget_ppm, and routing_budget_sats match DB
receiver user_id matches DB
receiver robot name matches DB
earned_rewards >= amount_sats
receiver user_id is not null
no successful reward withdrawal for same receiver in cooldown window (48h)
structured reward source rows exist for the receiver (slashed_bond or counterparty_cancelled)
claimed gross reward withdrawals subtracted from source totals
RoboSats earned_rewards <= remaining structured reward source balance
invoice hash and amount match decoded BOLT11
invoice not expired (both RoboSats DB and decoded expiry)
```

Each reward source event must prove:

```text
source order exists
receiver is maker or taker of that source order
staked receiver bond belongs to that source order
staked bond concept is valid (bond or slashed_bond)
staked bond sender is the receiver
slashed_bond source:
  slashed peer bond belongs to the source order
  slashed bond is SETTLED (DB status + LND LookupInvoice)
  reward_sats <= min(slashed_bond_sats, staked_bond_sats) / 2
  slashed and staked bonds are different
counterparty_cancelled source:
  peer bond is missing on source order
  slashed bond fields are empty/zero
  reward_sats <= staked_bond_sats / 2
```

The request body is treated as untrusted context. Postgres, decoded invoice data,
bitcoind, and LND state are the sources of truth.

## Compose Notes

Your coordinator compose uses `network_mode: service:tor`. Add the service from
`docker-compose.snippet.yml` to the same compose file. Because the containers
share the Tor service network namespace, RoboSats should call the middleware at:

```env
PAYOUT_POLICY_URL=http://127.0.0.1:8088/v1/robosats/lnd/send-payment-v2
PAYOUT_POLICY_ONCHAIN_URL=http://127.0.0.1:8088/v1/robosats/lnd/send-coins
PAYOUT_POLICY_KEYSEND_URL=http://127.0.0.1:8088/v1/robosats/lnd/send-keysend
PAYOUT_POLICY_REWARD_URL=http://127.0.0.1:8088/v1/robosats/lnd/send-reward
PAYOUT_POLICY_REWARD_SOURCE_URL=http://127.0.0.1:8088/v1/robosats/rewards/source
PAYOUT_POLICY_REWARD_SOURCE_TIMEOUT_SECONDS=5
```

Postgres is also reachable as `127.0.0.1:5432` from the middleware when it uses
the same network namespace.

The middleware is implemented as a Go binary and uses LND's Go gRPC bindings.
It no longer copies generated Python protobuf modules from the RoboSats image,
so the middleware image does not need a `ROBOSATS_IMAGE` build argument.
Set the LND binding version in the Docker Compose `.env` used for build-time
variable interpolation:

```env
LND_VERSION=v0.21.0-beta
```

If the selected LND version changes generated Go RPC types incompatibly, the
image build should fail and the adapter code must be reviewed before deployment.

bitcoind is expected to be reachable through the same env names RoboSats already
uses:

```env
BITCOIND_RPCURL=http://127.0.0.1:8332
BITCOIND_RPCUSER=...
BITCOIND_RPCPASSWORD=...
```

The exact port depends on your `bitcoin.conf` and network.

## Middleware Env

Copy `.env.example` and set at least:

```env
DATABASE_URL=postgresql://postgres:<password>@127.0.0.1:5432/<db>
AUDIT_DATABASE_URL=postgresql://postgres:<password>@127.0.0.1:5432/<db>
ROBOSATS_SERVICE_TOKEN=replace-with-long-random-token

LND_GRPC_HOST=127.0.0.1:10009
LND_TLS_CERT=/run/lnd/tls.cert
LND_MACAROON=/run/lnd/data/chain/bitcoin/mainnet/payout-policy.macaroon

BITCOIND_RPCURL=http://127.0.0.1:8332
BITCOIND_RPCUSER=...
BITCOIND_RPCPASSWORD=...

MAX_PAYOUT_SATS=5000000
MAX_ONCHAIN_PAYOUT_SATS=5000000
MAX_DEVFUND_KEYSEND_SATS=5000000
MAX_REWARD_WITHDRAWAL_SATS=5000000
REWARD_ROUTING_FEE_LIMIT_PPM=1000
REWARD_MIN_FLAT_FEE_LIMIT_SATS=2
REWARD_WITHDRAWAL_COOLDOWN_HOURS=48
DEVFUND_PUBKEY=<pubkey from robosats/devfund_pubkey.json for your network>
DEVFUND_KEYSEND_TIMEOUT_SECONDS=280
BITCOIND_RPC_TIMEOUT_SECONDS=10
REQUIRE_BITCOIND_VALIDATION=true
ALLOW_SPEND_UNCONFIRMED=false
BITCOIN_NETWORK=mainnet
STRICT_ADDRESS_NETWORK=true
LN_TRACK_TIMEOUT_SECONDS=3
LN_STREAM_TIMEOUT_SECONDS=160
LND_ADAPTER_RPC_TIMEOUT_SECONDS=5
LND_ADAPTER_SLOW_TIMEOUT_SECONDS=10
READYZ_CACHE_TTL_SECONDS=10
LOG_CONTEXT_CACHE_TTL_SECONDS=30
HTTP_READ_HEADER_TIMEOUT_SECONDS=10
HTTP_READ_TIMEOUT_SECONDS=30
HTTP_WRITE_TIMEOUT_SECONDS=310
HTTP_IDLE_TIMEOUT_SECONDS=120
SHUTDOWN_TIMEOUT_SECONDS=30
DB_POOL_MIN=1
DB_POOL_MAX=10
```

The middleware audit migration runs automatically when the `payout-policy`
container starts. It is idempotent and creates only middleware-owned audit
tables. If startup logs show `relation "payout_policy_attempts" does not exist`,
the container is using a different `AUDIT_DATABASE_URL` than expected or the
startup migration failed before the Go HTTP server launched.

`LND_TLS_CERT_BASE64` and `LND_MACAROON_BASE64` are also supported instead of
file paths, but mounted files are simpler with the compose layout above.

## Replay Audit Dry Run

The Go binary can replay recent audit snapshots without sending payments:

```bash
/app/payout-policy replay-audit --kind all --limit 100
```

Supported kinds are `all`, `ln`, `onchain`, and `keysend`. Replay revalidates
stored `request_snapshot` payloads against the current RoboSats database,
bitcoind, and invoice decoding. It does not call `SendPaymentV2`, `SendCoins`,
or keysend send.

## Required RoboSats Env

```env
PAYOUT_POLICY_ENABLED=true
PAYOUT_POLICY_ONCHAIN_ENABLED=true
PAYOUT_POLICY_KEYSEND_ENABLED=true
PAYOUT_POLICY_REWARD_ENABLED=true
PAYOUT_POLICY_URL=http://127.0.0.1:8088/v1/robosats/lnd/send-payment-v2
PAYOUT_POLICY_ONCHAIN_URL=http://127.0.0.1:8088/v1/robosats/lnd/send-coins
PAYOUT_POLICY_KEYSEND_URL=http://127.0.0.1:8088/v1/robosats/lnd/send-keysend
PAYOUT_POLICY_REWARD_URL=http://127.0.0.1:8088/v1/robosats/lnd/send-reward
PAYOUT_POLICY_REWARD_SOURCE_URL=http://127.0.0.1:8088/v1/robosats/rewards/source
PAYOUT_POLICY_REWARD_SOURCE_TIMEOUT_SECONDS=5
LND_ADAPTER_ENABLED=true
LND_ADAPTER_URL=http://127.0.0.1:8088
LND_ADAPTER_TOKEN=replace-with-long-random-token
LND_ADAPTER_CONNECT_TIMEOUT_SECONDS=2
LND_ADAPTER_POOL_MAXSIZE=32
LND_ADAPTER_TIMEOUT_SECONDS=10
LND_ADAPTER_PAYMENT_TIMEOUT_SECONDS=170
PAYOUT_POLICY_TOKEN=replace-with-long-random-token
PAYOUT_POLICY_CONNECT_TIMEOUT_SECONDS=2
PAYOUT_POLICY_POOL_MAXSIZE=32
PAYOUT_POLICY_TIMEOUT_SECONDS=170
PAYOUT_POLICY_KEYSEND_TIMEOUT_SECONDS=290
PAYOUT_POLICY_REWARD_TIMEOUT_SECONDS=170
PAYOUT_POLICY_FAIL_CLOSED=true
```

`PAYOUT_POLICY_TOKEN` and `LND_ADAPTER_TOKEN` must match middleware
`ROBOSATS_SERVICE_TOKEN`.

The patched Python helpers use process-local `requests.Session` pools for both
the payout policy and LND adapter calls. Keep the URLs on `127.0.0.1` in the
shared Tor network namespace; the connect timeout and pool size are controlled
by `*_CONNECT_TIMEOUT_SECONDS` and `*_POOL_MAXSIZE`.

For on-chain payouts, RoboSats' existing local gate still runs before the
middleware hook:

```env
DISABLE_ONCHAIN=false
MAX_SWAP_AMOUNT=5000000
```

Use the same or lower limit than `MAX_ONCHAIN_PAYOUT_SATS`.

## LND Macaroon

The Go service should hold the macaroon that can call:

```text
/routerrpc.Router/SendPaymentV2
/routerrpc.Router/TrackPaymentV2
/verrpc.Versioner/GetVersion
/lnrpc.Lightning/GetInfo
/lnrpc.Lightning/DecodePayReq
/lnrpc.Lightning/EstimateFee
/lnrpc.Lightning/WalletBalance
/lnrpc.Lightning/ChannelBalance
/lnrpc.Lightning/SendCoins
/invoicesrpc.Invoices/AddHoldInvoice
/invoicesrpc.Invoices/CancelInvoice
/invoicesrpc.Invoices/SettleInvoice
/invoicesrpc.Invoices/LookupInvoiceV2
```

With `LND_ADAPTER_ENABLED=true` and the helper-method patch applied, RoboSats
does not need a macaroon for the covered `api/lightning/lnd.py` paths. If you
keep direct fallback code enabled, the fallback macaroon must still exclude
`SendPaymentV2`, `SendCoins`, and `SendMany`.

The strict RoboSats runtime macaroon should be baked with only these RPCs
(URI-level permissions, no broad action permissions):

```bash
lncli bakemacaroon \
  uri:/verrpc.Versioner/GetVersion \
  uri:/lnrpc.Lightning/GetInfo \
  uri:/lnrpc.Lightning/DecodePayReq \
  uri:/lnrpc.Lightning/EstimateFee \
  uri:/lnrpc.Lightning/WalletBalance \
  uri:/lnrpc.Lightning/ChannelBalance \
  uri:/invoicesrpc.Invoices/AddHoldInvoice \
  uri:/invoicesrpc.Invoices/CancelInvoice \
  uri:/invoicesrpc.Invoices/SettleInvoice \
  uri:/invoicesrpc.Invoices/LookupInvoiceV2 \
  --save_to=/path/to/robosats-runtime.macaroon
```

Verify before deploying:

```bash
lncli printmacaroon --macaroon_file=/path/to/robosats-runtime.macaroon
```

The printed permissions must not include:

```text
/routerrpc.Router/SendPaymentV2
/lnrpc.Lightning/SendCoins
/lnrpc.Lightning/SendMany
offchain:write
onchain:write
router:write
address:write
walletkit
admin
```

Before removing `SendPaymentV2` from the RoboSats macaroon, account for any
direct fallback paths left enabled in the deployed RoboSats patch:

```text
LNDNode.pay_invoice()
  reward withdrawal fallback path, only when PAYOUT_POLICY_REWARD_ENABLED=false
```

Devfund `LNDNode.send_keysend()` is gated by this middleware when
`PAYOUT_POLICY_KEYSEND_ENABLED=true`.

## Health

Unauthenticated liveness:

```text
GET /healthz
```

Authenticated readiness:

```text
GET /readyz
Authorization: Bearer <ROBOSATS_SERVICE_TOKEN>
```

`/readyz` checks RoboSats Postgres, audit Postgres, middleware audit tables,
and bitcoind.

## Operational Notes

Production should use:

```env
PAYOUT_POLICY_FAIL_CLOSED=true
REQUIRE_BITCOIND_VALIDATION=true
STRICT_ADDRESS_NETWORK=true
LN_STREAM_TIMEOUT_SECONDS=160
```

Workflow and retry behavior:

```text
LN buyer payouts:
  validate RoboSats DB and invoice details
  TrackPaymentV2 before SendPaymentV2
  return IN_FLIGHT/INITIATED/ALREADY_PAID without creating a new send
  track before marking an expired invoice EXPIRED
  treat POLICY_REJECTED as fail-closed, not as an LND routing failure
  alert TELEGRAM_COORDINATOR_CHAT_ID on POLICY_REJECTED

On-chain buyer payouts:
  reserve the onchain_payment_id before SendCoins
  return stored txid on exact duplicate after broadcast
  block retry for reserved attempts without txid until operator reconciliation
  alert TELEGRAM_COORDINATOR_CHAT_ID on POLICY_REJECTED

Devfund keysend:
  deterministic request_id from pubkey/message/amount/fee/timeout
  reserve generated preimage/payment_hash before SendPaymentV2
  return existing terminal attempt on exact duplicate
  TrackPaymentV2 exact duplicate attempts that are still non-terminal

Reward withdrawals:
  require a structured reward source row before sending
  reject manual or legacy rewards that only changed RoboSats earned_rewards
  reject if the same receiver already had a successful reward withdrawal in the cooldown window
  reserve request/payment before SendPaymentV2
  exact duplicate terminal attempt returns the stored result
  exact duplicate non-terminal attempt tries TrackPaymentV2 before rejecting
  different invoice after a terminal FAILED/ERROR/EXPIRED result can be tried again
  different invoice is blocked while a prior reward attempt is INITIATED/IN_FLIGHT or recently successful
```

With a restricted RoboSats runtime macaroon, keep `PAYOUT_POLICY_FAIL_CLOSED=true`.
Fail-open fallback cannot call direct payout RPCs after `SendPaymentV2` and
`SendCoins` are removed from the RoboSats macaroon.

If the middleware reserves an on-chain attempt and then cannot record a txid, the
same `onchain_payment_id` is blocked until an operator reconciles it. That is
intentional: it avoids blindly retrying a possibly broadcast transaction.

The Go service uses HTTP read/write timeouts from `HTTP_*_TIMEOUT_SECONDS`.
Keep `LN_STREAM_TIMEOUT_SECONDS` lower than RoboSats
`PAYOUT_POLICY_TIMEOUT_SECONDS` for trade payouts, and keep RoboSats
`PAYOUT_POLICY_KEYSEND_TIMEOUT_SECONDS` higher than middleware
`DEVFUND_KEYSEND_TIMEOUT_SECONDS` for devfund keysend donations.

`READYZ_CACHE_TTL_SECONDS` caches the expensive readiness probe briefly so
healthcheck bursts do not repeatedly hit RoboSats Postgres, audit Postgres, and
bitcoind. `LOG_CONTEXT_CACHE_TTL_SECONDS` caches the DB lookup used only for
human-readable request log fields. Normal `lookup-invoice` polling logs only
the payment hash and LND invoice state; DB-backed robot/order enrichment is
reserved for state-changing invoice calls and lookup errors/not-found results.
