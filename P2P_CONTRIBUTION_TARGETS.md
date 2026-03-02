# P2P Contribution Targets for Dendrite

Analysis of [arewep2pyet.com](https://arewep2pyet.com/) and the
[element-hq/dendrite](https://github.com/element-hq/dendrite) repository to
identify the most impactful areas for contribution toward P2P Matrix.

---

## Context

Dendrite is in **maintenance mode** (security fixes only), but Element has
[signaled intent](https://matrix.org/blog/2025/12/19/this-week-in-matrix-2025-12-19/)
to do "pragmatic P2P Matrix work in 2026." This makes well-scoped contributions
to P2P-critical subsystems particularly valuable.

The arewep2pyet.com tracker identifies four pillars:

1. **Embeddable Homeserver (Dendrite)** — 72.4% test coverage (target ≥80%)
2. **Overlay Network (Pinecone)** — attack resilience gaps remain
3. **Federation Protocol Improvements** — store-and-forward, Power DAG
4. **Bridging P2P ↔ Standard Matrix** — not started

---

## Tier 1 — High-Impact, Directly Advances P2P (Recommended)

### 1. Relay API Robustness & Test Coverage

**Why:** The relay API (`relayapi/`) is the store-and-forward backbone for P2P.
arewep2pyet.com marks "robustness testing across varied scenarios" as 🚧.

**What to do:**
- Address the TODO at `relayapi/storage/shared/storage.go:94` — incomplete
  cleanup logic for queue entries across destinations.
- Add integration tests for edge cases: relay under concurrent load, large
  payloads, node reconnection after extended offline periods.
- The relay retriever (`cmd/dendrite-demo-pinecone/relay/`) lacks tests entirely.

**Relevant files:**
- `relayapi/internal/perform.go` (uses `context.TODO()` at line 148)
- `relayapi/storage/shared/storage.go`
- `cmd/dendrite-demo-pinecone/relay/`
- `cmd/dendrite-demo-pinecone/monolith/monolith.go`

### 2. Federation Queue — Relay Fallback Gaps

**Why:** Two critical TODOs in the federation queue directly block reliable P2P
message delivery.

**What to do:**
- `federationapi/queue/destinationqueue.go:423` — Relay sends lack proper
  userID passthrough ("TODO: how to pass through actual userID here?!?!?!")
- `federationapi/queue/destinationqueue.go:438` — No handling for when a
  destination comes back online but can't reach its relay server.
- `federationapi/internal/perform.go:565,622` — Join/leave via relay is
  commented as not yet allowed ("This should be allowed via a relay. Currently
  only transactions are supported").

### 3. Improve Dendrite Test Coverage (72.4% → 80%+)

**Why:** arewep2pyet.com explicitly gates P2P readiness on ≥80% test coverage.

**What to do — highest-value untested areas:**
- `federationapi/routing/peek.go` — no tests, has two TODOs
- `federationapi/routing/threepid.go` — uses hardcoded `"TODO"` as a value
- `federationapi/routing/leave.go` — has `XXX` debug logging at line 299
- `federationapi/consumers/roomserver.go` — multiple FIXMEs about race
  conditions and missing housekeeping
- `cmd/dendrite-demo-pinecone/` — the entire P2P demo has almost no tests

---

## Tier 2 — Medium Impact, Good First Issues

### 4. Fix Open Federation Bugs (label: `C-Federation`)

These directly affect P2P reliability:

| Issue | Title | Impact |
|-------|-------|--------|
| [#2153](https://github.com/element-hq/dendrite/issues/2153) | Can't join medium-sized federated rooms | P2P rooms will be multi-node |
| [#2182](https://github.com/element-hq/dendrite/issues/2182) | Typing EDUs cause high load in federationapi | P2P nodes are resource-constrained |
| [#1848](https://github.com/element-hq/dendrite/issues/1848) | Incoming federated events slow to process | Directly affects P2P latency |
| [#2028](https://github.com/element-hq/dendrite/issues/2028) | `send_join` with unverifiable auth events causes panic | Crash in P2P = node goes offline |
| [#3124](https://github.com/element-hq/dendrite/issues/3124) | Permanent high CPU with enable_outbound on | P2P nodes always have outbound on |

### 5. Spec Compliance — Matrix Version Declarations

These are explicitly labeled **good first issue** + **help wanted**:

| Issue | Title |
|-------|-------|
| [#3218](https://github.com/element-hq/dendrite/issues/3218) | Declare support for Matrix 1.2 |
| [#3222](https://github.com/element-hq/dendrite/issues/3222) | Declare support for Matrix 1.4 |
| [#3223](https://github.com/element-hq/dendrite/issues/3223) | Declare support for Matrix 1.5 |
| [#3224](https://github.com/element-hq/dendrite/issues/3224) | Declare support for Matrix 1.6 |
| [#3225](https://github.com/element-hq/dendrite/issues/3225) | Declare support for Matrix 1.7 |
| [#3226](https://github.com/element-hq/dendrite/issues/3226) | Declare support for Matrix 1.8 |

These involve auditing which spec features Dendrite already supports and
updating the `/versions` endpoint. Good for learning the codebase.

### 6. Other Good First Issues with P2P Relevance

| Issue | Title | Why P2P-relevant |
|-------|-------|------------------|
| [#3097](https://github.com/element-hq/dendrite/issues/3097) | Implement token-authenticated registration | P2P nodes need lightweight auth |
| [#3095](https://github.com/element-hq/dendrite/issues/3095) | Implement room knocking | Federation feature for P2P rooms |
| [#1324](https://github.com/element-hq/dendrite/issues/1324) | Fix failing /sync tests | Sync correctness matters for P2P |
| [#587](https://github.com/element-hq/dendrite/issues/587) | Implement filtering | Bandwidth-critical for P2P nodes |

---

## Tier 3 — Larger Efforts / Not Yet Started

### 7. P2P ↔ Standard Matrix Bridge

**Status:** ❌ Not started on arewep2pyet.com

A gateway server that bridges traffic between P2P Matrix (Pinecone overlay) and
standard HTTPS federation. This is a significant architectural piece but would
be a major contribution.

### 8. Sliding Sync / SyncV3 Support

**Issue:** [#3236](https://github.com/element-hq/dendrite/issues/3236)

Embedded/P2P instances need efficient sync. Sliding sync (MSC4186) dramatically
reduces bandwidth, which is critical for mobile P2P nodes. This is a large
feature but aligns with Element X client requirements.

### 9. Pinecone Attack Resilience

arewep2pyet.com marks these as 🚧:
- Churn attack resistance
- Root hijacking protection
- Scaling for global demand

These are in the [Pinecone repo](https://github.com/matrix-org/pinecone), not
Dendrite, but directly advance P2P readiness.

---

## Tier 4 — Infrastructure & CI Improvements

### 10. Re-enable WASM Build in CI

**Status:** The WASM build job in `.github/workflows/dendrite.yml` is currently
**disabled** (`if: ${{ false }}`). All storage packages have WASM stubs
(SQLite-only), and fulltext search is a no-op stub in WASM
(`internal/fulltext/bleve_wasm.go`). Re-enabling and fixing the WASM build
pipeline ensures the embedded browser use case doesn't regress.

### 11. Add P2P-Specific CI Tests

There are **zero automated tests** for the Pinecone or Yggdrasil demo entry
points. The relay retriever (`cmd/dendrite-demo-pinecone/relay/retriever.go`)
has a known gap: line 169 asks "What happens if your relay receives new messages
after this point?" Adding CI that spins up multiple Pinecone nodes and tests
relay delivery end-to-end would be high value.

### 12. Relay Server Auto-Discovery

Currently relay servers must be **manually configured** in the database. There
is no auto-discovery mechanism. Implementing relay advertisement (perhaps via
room state or a well-known endpoint) would significantly improve the P2P
developer/user experience.

### 13. Update Outdated P2P Documentation

`docs/other/p2p.md` dates from May 2020 and references a hardcoded libp2p
websocket relay (`TODO` on line 35) and a missing Docker image (`TODO` on
line 74). The Tor and I2P demo docs in `contrib/` are also minimal stubs.

---

## Recommended Starting Point

For maximum impact with moderate effort, start with **Tier 1, Item 2** (Federation
Queue relay fallback gaps). The TODOs are explicit, the code paths are clear,
and fixing them directly unblocks reliable P2P message delivery. Pair this with
adding tests for the relay API (Item 1) to also push toward the 80% coverage
target.

For a gentler on-ramp, the **Matrix version declaration issues** (Tier 2, Item 5)
are great for learning the codebase structure while making real contributions.
