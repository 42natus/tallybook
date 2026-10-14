<!-- GENERATED FILE - DO NOT EDIT BY HAND.
     Source of truth: docs/WAVE2_BACKLOG.json
     Regenerate with: python3 scripts/gen-wave-backlog.py  (or `make backlog`)
     Any hand edit here is lost on the next regeneration. -->

# Wave 2 backlog

**22 issues · 2800 points.**

| Tier | Label | Count | Points each | Subtotal |
|---|---|---:|---:|---:|
| high | `complexity: high` | 1 | 200 | 200 |
| medium | `complexity: medium` | 10 | 150 | 1500 |
| low | `complexity: low` | 11 | 100 | 1100 |
| **total** | | **22** | | **2800** |

Complexity uses this repository's existing `complexity: low|medium|high` labels (see `gh label list`); `trivial` in the wave brief maps to `low`. Grades describe real scope, per `MAINTAINER.md` section 2 — they are not adjusted to reach a point total.

`area` is taken from the commit scopes `CONTRIBUTING.md` already defines, since the repository has no `area:` labels. It appears in each issue body rather than as a label.

---

## 1. fix(workspace): set read, write and idle timeouts on all three http servers

`complexity: high` · 200 points · area `workspace`

**What's wrong.** cmd/collector/main.go:110, cmd/indexer/main.go:105 and cmd/settler/main.go:114 each build a server with only Addr and Handler. A connection that opens and never completes its request headers is held indefinitely, so a trivial Slowloris exhausts the listener. On the settler that takes down the /metrics endpoint §6 calls the operator's alerting surface for the nearest refund deadline — the single most important number this system produces — while the deadline keeps approaching. CLAUDE.md §8 already requires every external call to carry a timeout; the inbound direction was missed.

**Proposed fix.** All three binaries construct `&http.Server{Addr:..., Handler:...}` with no ReadHeaderTimeout, ReadTimeout, WriteTimeout or IdleTimeout. Add explicit timeouts to each, sourced from config rather than hardcoded, and document the chosen values.

**Acceptance criteria**

1. All three servers set ReadHeaderTimeout, ReadTimeout, WriteTimeout and IdleTimeout to non-zero values
2. A test opens a connection, writes a partial header, and asserts the server closes it rather than holding it open
3. Timeout values come from internal/config with validated defaults, not literals at the call site
4. The settler's /metrics endpoint still answers within its timeout under a concurrent-reader test
5. go vet and staticcheck stay clean

**Guidance**

- The three call sites are one-liners; the change is small but spans all three binaries, so keep the values consistent.
- internal/config/config.go is where the new fields belong — follow the existing validated-field pattern and fail fast per §7.
- ReadHeaderTimeout is the one that actually stops Slowloris; ReadTimeout alone does not.
- Do not set WriteTimeout shorter than the slowest legitimate upstream response the collector proxies, or you will truncate real responses.

---

## 2. refactor(settle): replace raw lifecycle status literals with typed constants

`complexity: medium` · 150 points · area `settle`

**What's wrong.** internal/settle/policy.go:95 decides the channel-closing sweep trigger on `state.Status == "closing"`. The same untyped literals recur in internal/settle/submit.go:160, internal/settle/retry.go:156, internal/meter/anchor.go:375 and internal/meter/period.go:107. A misspelling compiles and silently disables a sweep trigger; §6 lists channel-closing as one of four triggers that decide whether the operator gets paid before the funder can reclaim the balance. The repo already does this correctly for protocol names (internal/httpapi/metering.go:26) — the lifecycle statuses just never got the same treatment.

**Proposed fix.** Channel, settlement, and period lifecycle statuses are compared and assigned as bare string literals across five files. Introduce typed string constants for each lifecycle and use them everywhere, so a typo is a compile error.

**Acceptance criteria**

1. A typed constant exists for each of the channel, settlement, period and anchor lifecycles
2. No bare status literal remains in non-test code under internal/ (a grep in CI or a test asserts this)
3. Passing an unrecognised status to Decide returns a wait decision and does not settle
4. Existing settle and meter tests pass unchanged apart from constant references

**Guidance**

- Mirror the existing const block at internal/httpapi/metering.go:26, which already does this for protocol names.
- The database CHECK constraints in internal/store/migrations are the source of truth for the legal values — read them first and do not invent a status the schema will reject.
- Keep the constants' underlying type `string` so existing pgx scans into them keep working without conversion.

---

## 3. ci(workspace): add govulncheck and a gosec job to the pipeline

`complexity: medium` · 150 points · area `ci`

**What's wrong.** .github/workflows/ci.yml has no vulnerability or security-lint step. This is not hypothetical: the missing HTTP server timeouts filed alongside this issue are exactly gosec's G112 (Slowloris), and nothing in the current pipeline flags them. For a repository whose own CONTRIBUTING opens with "Tallybook is infrastructure that handles money" and which pins two direct dependencies handling XDR and Postgres, an unnoticed advisory in either is a live risk.

**Proposed fix.** CI runs gofmt, go vet, staticcheck, go build and go test. Add govulncheck for known module vulnerabilities and gosec for the security-lint classes staticcheck does not cover.

**Acceptance criteria**

1. govulncheck ./... runs in CI and fails the build on a known advisory in a reachable path
2. gosec runs in CI; any rule deliberately excluded is listed with a one-line reason in the workflow
3. gosec flags the G112 server-timeout finding on the pre-fix tree, proving the job actually catches it
4. The new jobs do not require Postgres and so run even when the service container is unavailable

**Guidance**

- Add both as separate steps in the existing build-vet-staticcheck-test job rather than new jobs, so they share the Go setup and module cache.
- staticcheck is already pinned to an exact version (2025.1.1) in the workflow — pin govulncheck and gosec the same way; an unpinned security tool turns CI red on someone else's release.
- govulncheck needs the module graph, so it must run after `go mod download`.

---

## 4. fix(settle): make Decide refuse a channel that is not open or closing

`complexity: medium` · 150 points · area `settle`

**What's wrong.** §6 scopes the sweep to "every channel with status IN ('open','closing')", but internal/settle/policy.go only inspects status to test for "closing" at line 95. A channel already refunded or closed, reaching Decide with a stale non-zero exposure row, satisfies the deadline-pressure branch above it and returns ShouldSettle — a settle attempt against a channel with nothing left to take, which is a pure transaction-fee loss. The daemon's own query currently filters correctly, so this is a latent gap rather than a live bug; Decide is documented as a pure total function of its inputs, which is exactly why it should not depend on a caller invariant it can check itself.

**Proposed fix.** Decide trusts its caller to have filtered channels to status open or closing. Make it reject any other status itself and return a wait decision.

**Acceptance criteria**

1. Decide returns a wait decision for status closed and for status refunded, even under deadline pressure
2. A table-driven test covers all four statuses crossed against deadline pressure present and absent
3. The existing open and closing behaviour is unchanged, proven by the current tests passing untouched
4. The daemon's channel query is left as the first line of defence, not replaced

**Guidance**

- internal/settle/policy.go:83 is the guard clause that already short-circuits on zero exposure — add the status check beside it, before the trigger switch.
- Pair this with the typed-status-constant issue; doing that one first makes this a two-line change.
- Do not return an error: Decide is deliberately a pure function with no error path, and callers treat a wait decision as the safe default.

---

## 5. fix(stellar): verify the generated auth nonce is in the range soroban accepts

`complexity: medium` · 150 points · area `stellar`

**What's wrong.** internal/stellar/invoke.go:278 returns `int64(binary.BigEndian.Uint64(b[:]))`. With the top bit set — half the time — the nonce is negative. Whether the host accepts a negative nonce in a SorobanAddressCredentials entry is not something this repo has verified, and CONTRIBUTING is explicit that a change or claim in internal/stellar must be checked "against the live Soroban RPC surface or the real contract source first". The same file already documents one hard-won nonce-footprint trap (CLAUDE.md §4), so the cost of being wrong here is a class of intermittent, hard-to-reproduce auth failure on roughly half of fresh invocations.

**Proposed fix.** The authorization-entry nonce is generated by reinterpreting eight random bytes as a signed int64, which is negative for roughly half of all draws. Confirm against the Soroban host what range it actually accepts and constrain generation to it.

**Acceptance criteria**

1. The accepted nonce range is established from the Soroban host source or live network behaviour, and recorded in a doc comment with the reference
2. Nonce generation is constrained to that range, with a test asserting many generated nonces all fall inside it
3. If negative nonces turn out to be accepted, a written finding in the doc comment recording that, with evidence, closes this issue — no code change required
4. Nonce generation stays uniformly distributed over whatever range is chosen; it must not collapse to a narrow band

**Guidance**

- Read internal/stellar/invoke.go's existing doc comments first — they record the CAP-71 and footprint findings and set the evidentiary bar for this package.
- The honest outcome here may be "this is already fine"; a written, cited finding is an acceptable completion per CONTRIBUTING's verify-before-you-claim rule.
- Do not simply mask the sign bit without checking: halving the nonce space to avoid an unverified problem is its own, quieter change.

---

## 6. fix(workspace): compare ErrServerClosed with errors.Is, not !=

`complexity: low` · 100 points · area `workspace` · `good first issue`

**What's wrong.** cmd/collector/main.go:114, cmd/indexer/main.go:119 and cmd/settler/main.go:128 all use `!=`. CLAUDE.md §8 and CONTRIBUTING both require sentinel errors to be matched with errors.Is. Today net/http returns the sentinel unwrapped so the comparison happens to work; if that ever changes, or a middleware wraps it, each binary logs a normal shutdown as a fatal startup error instead — noise in exactly the logs an operator reads during an incident.

**Proposed fix.** All three binaries test the shutdown sentinel with `err != http.ErrServerClosed`. Switch to errors.Is so a wrapped sentinel is still recognised.

**Acceptance criteria**

1. All three sites use errors.Is(err, http.ErrServerClosed)
2. A test wraps http.ErrServerClosed with fmt.Errorf("%w") and asserts the shutdown path still treats it as a clean shutdown
3. A real listen failure, such as a port already in use, is still reported as a fatal error
4. gofmt, go vet and staticcheck stay clean

**Guidance**

- Three one-line changes; cmd/collector/main.go:114 is the clearest place to start.
- The errors package is already imported in each file, so no import changes are needed.
- Keep the existing log message and exit behaviour — this is a comparison fix, not a rework of shutdown.

**Entry point:** `cmd/collector/main.go`

---

## 7. refactor(stellar): remove the panic from mustScAddress

`complexity: low` · 100 points · area `stellar` · `good first issue`

**What's wrong.** internal/stellar/invoke.go:288 is the only panic in non-test code under internal/. CLAUDE.md §8 states the rule without exception — "No panics outside main startup validation. A library function returns an error" — and CONTRIBUTING repeats it. The existing comment argues the input is always a package-internal constant, which is true today, but contract IDs reach this package from TB_PRICE_BOOK_ID and TB_STATEMENT_REGISTRY_ID (§7), so the assumption is one config path away from being false, and the failure mode is a crashed daemon rather than a startup error naming the bad variable.

**Proposed fix.** mustScAddress panics when a contract ID fails to parse. Return an error instead and thread it through the callers, so internal/ contains no panic outside main.

**Acceptance criteria**

1. mustScAddress is gone or returns an error, and no panic remains in non-test code under internal/
2. A test passes a malformed contract ID and asserts an error is returned, naming the sentinel or type it matches
3. A grep-based test or CI step asserts internal/ stays panic-free, so this cannot regress
4. Callers surface the error with the wrapping style CONTRIBUTING requires, naming the offending input

**Guidance**

- Start at internal/stellar/invoke.go:281 and follow the callers upward; the chain is deep, which is the reason the panic was there.
- Config-sourced contract IDs should fail at startup in internal/config instead, per §7's fail-fast-with-the-variable-named rule — that may be the better half of the fix.
- Do not convert the panic into a silently ignored zero value; an unparseable address must stop the call.

**Entry point:** `internal/stellar/invoke.go`

---

## 8. test(workspace): convert the remaining non-table tests to subtests

`complexity: low` · 100 points · area `workspace` · `good first issue`

**What's wrong.** CONTRIBUTING requires "Table-driven tests, subtests via t.Run" and CLAUDE.md §8 repeats it, but the measured ratio across the repo is 27 t.Run sites to 281 test functions. Some of those tests are legitimately not table-shaped — an integration test against Postgres often is not — so this is an audit with a conversion, not a blanket rewrite. The payoff is that a failing case names itself instead of failing an umbrella test.

**Proposed fix.** The suite has 281 test functions but only 27 t.Run call sites. Audit which tests are genuinely table-shaped and convert those, leaving a short note where a flat test is the right shape.

**Acceptance criteria**

1. Every test that asserts the same behaviour over more than two input sets uses t.Run with a named case per set
2. Any test deliberately left flat carries a one-line comment saying why
3. Total test count does not fall, and go test ./... still passes with -count=1
4. Subtest names are stable strings, not derived from loop indices, so a failure names the case

**Guidance**

- Start with internal/merkle and internal/meter, the two most input-driven packages.
- internal/settle/policy_test.go already follows the target shape — copy its structure rather than inventing one.
- Do not convert the Postgres integration tests for the sake of the count; their setup cost per subtest is real.

**Entry point:** `internal/merkle/leaf_test.go`

---

## 9. chore(workspace): add CODEOWNERS routing money-path packages to review

`complexity: low` · 100 points · area `ci` · `good first issue`

**What's wrong.** There is no CODEOWNERS file anywhere in the repo. CONTRIBUTING says changes touching money handling, secret handling or an on-chain interface "get the closest review", but nothing enforces that routing, so such a PR can be approved by whoever is free. §5 calls commitments "THE MOST IMPORTANT TABLE IN THIS SCHEMA" and a bad migration against it is unrecoverable.

**Proposed fix.** Add a CODEOWNERS file routing review of the packages that move money — internal/custody, internal/settle, internal/store/migrations and internal/stellar — to named owners.

**Acceptance criteria**

1. A .github/CODEOWNERS exists covering internal/custody, internal/settle, internal/store/migrations and internal/stellar
2. GitHub's CODEOWNERS syntax check passes and the repo settings show the rules as active
3. A PR touching only docs/ does not trigger the money-path owners
4. The file carries a comment explaining why these four paths are singled out

**Guidance**

- CONTRIBUTING's "Opening a PR" section already names the three categories to protect — mirror those paths rather than inventing a split.
- Ownership must be a team or a user that actually exists, or GitHub silently ignores the rule.
- Keep the catch-all last; CODEOWNERS applies the last matching pattern, not the most specific.

**Entry point:** `CONTRIBUTING.md`

---

## 10. docs(workspace): add a pull request template carrying the money-path checklist

`complexity: low` · 100 points · area `workspace` · `good first issue`

**What's wrong.** No PR template exists. CONTRIBUTING's "Opening a PR" section lists exactly what a PR must say — including that "the requirement as written would lose money" is a legitimate reason to flag rather than build — but a contributor only learns this by reading CONTRIBUTING first. A template puts the prompt where the work happens.

**Proposed fix.** Add a PR template prompting for the disclosures CONTRIBUTING already asks for in prose: what changed, whether money, secret or on-chain handling is touched, and any deliberate CLAUDE.md deviation.

**Acceptance criteria**

1. A .github/PULL_REQUEST_TEMPLATE.md exists and renders on a new PR
2. It prompts for money, secret and on-chain-interface impact as three separate explicit questions
3. It asks whether make check passed locally, matching CONTRIBUTING's stated gate
4. It stays under roughly 30 lines, so it is read rather than deleted

**Guidance**

- Lift the wording from CONTRIBUTING's "Opening a PR" section verbatim so the two cannot drift.
- Use comment markers for instructions so they do not end up in the PR body.
- Do not add a checklist nobody can fail; each item should be answerable with real content.

**Entry point:** `CONTRIBUTING.md`

---

## 11. chore(workspace): add an editorconfig covering go, sql and yaml

`complexity: low` · 100 points · area `workspace` · `good first issue`

**What's wrong.** No .editorconfig exists. gofmt keeps Go consistent, but the SQL migrations under internal/store/migrations and the YAML in .github/workflows have no formatter at all, and a migration is a file nobody may edit once applied (§5) — so a whitespace-only diff on one is noise on exactly the files that most need a clean history.

**Proposed fix.** Add an .editorconfig fixing indentation, final newline and trailing whitespace for the file types in the repo: Go, SQL migrations, YAML workflows and Markdown.

**Acceptance criteria**

1. An .editorconfig covers *.go, *.sql, *.yml and *.md with explicit indent style and size
2. Settings for *.go match what gofmt already produces, so the two never disagree
3. Running the repo's existing make check produces no new diff after the file is added
4. insert_final_newline and trim_trailing_whitespace are set for the SQL and YAML globs

**Guidance**

- Check the existing migrations under internal/store/migrations for their current indentation before choosing a value — match reality, do not reformat applied migrations.
- gofmt uses tabs for Go; setting spaces there would fight the formatter CI enforces.
- This is configuration only — do not reformat existing files in the same PR.

**Entry point:** `internal/store/migrations/0001_periods.sql`

---

## 12. ci(workspace): evaluate golangci-lint against the existing staticcheck job

`complexity: low` · 100 points · area `ci` · `good first issue`

**What's wrong.** There is no .golangci.yml and CI runs staticcheck alone (.github/workflows/ci.yml). staticcheck is strong but does not cover several classes this codebase cares about — errorlint for the %w and errors.Is rules CLAUDE.md §8 mandates, and bodyclose on the Soroban RPC client's HTTP responses. Whether that is worth a second linter is a real question, not a foregone conclusion.

**Proposed fix.** Assess whether golangci-lint adds findings staticcheck does not on this codebase. If it does, add a pinned config and job; if it does not, record that finding and close.

**Acceptance criteria**

1. golangci-lint is run once over the tree and its findings are triaged into real and noise
2. If adopted: a pinned version and a config enabling only the linters that found something, with staticcheck not duplicated
3. If not adopted: a written finding in the PR recording what it found and why it was not worth the job, which closes this issue
4. Either way CI runtime is reported before and after, so the cost is explicit

**Guidance**

- errorlint and bodyclose are the two most likely to earn their place here; start by enabling just those.
- staticcheck is already pinned at 2025.1.1 in the workflow — an unpinned golangci-lint would make CI fail on someone else's release.
- An honest "not worth it" outcome is a valid completion; do not add a job that only reports noise.

**Entry point:** `.github/workflows/ci.yml`

---

## 13. ci(workspace): enable dependabot for go modules and github actions

`complexity: low` · 100 points · area `ci` · `good first issue`

**What's wrong.** No .github/dependabot.yml exists. go.mod pins two direct dependencies — github.com/jackc/pgx/v5 and github.com/stellar/go-stellar-sdk — that respectively hold the database credentials and build the transactions that move money, and the workflow pins actions/checkout and actions/setup-go by major version. Nothing currently tells anyone when an update to either lands. CONTRIBUTING's "justify every third-party module" rule makes the small dependency surface a deliberate choice, which makes watching it cheap.

**Proposed fix.** Add a dependabot config watching the Go module requirements and the pinned GitHub Actions, grouped so routine action bumps do not each open a PR.

**Acceptance criteria**

1. A dependabot config watches gomod and github-actions ecosystems
2. Action updates are grouped into a single PR; the two direct Go modules are not grouped
3. The schedule is weekly or less frequent, so the backlog is not flooded
4. A comment records that each Go module bump still needs the CONTRIBUTING justification in its commit

**Guidance**

- Keep the two direct Go dependencies ungrouped: each needs its own justification commit per CONTRIBUTING's dependency rule.
- Indirect dependencies can be grouped; there are eight of them in go.mod today.
- Do not enable version updates for the Postgres service image pinned in the workflow — that one has to match the migrations.

**Entry point:** `.github/workflows/ci.yml`

---

## 14. refactor(store): consolidate the three copies of numericToBigInt

`complexity: medium` · 150 points · area `store`

**What's wrong.** internal/settle/numeric.go:23, internal/meter/numeric.go:22 and internal/indexer/reconcile.go:138 each define their own numericToBigInt, converting a pgtype.Numeric into the *big.Int that every amount in this system is carried as. This is the single conversion standing between NUMERIC(39,0) in Postgres and the money arithmetic CLAUDE.md §8 insists is integer-only. Three copies means a correctness fix — negative exponents, NaN, a null Int — lands in one and silently misses two, and the three are not guaranteed identical today. A rounding bug here is a financial bug by the repo's own framing.

**Proposed fix.** numericToBigInt is implemented three separate times — in internal/settle, internal/meter and internal/indexer. Move one canonical implementation into a shared package and delete the copies.

**Acceptance criteria**

1. One exported conversion exists in a shared package and all three call sites use it
2. The three former copies are deleted, verified by a grep-based assertion in the test or CI
3. A table-driven test covers a negative exponent, a positive exponent, a zero value, a NaN and an invalid Numeric, asserting the error for each rejected case
4. The three packages' existing tests pass unchanged, proving behaviour was preserved
5. No call site silently drops the error the conversion returns

**Guidance**

- Diff the three implementations before moving any of them — if they already differ, the difference is itself the finding and belongs in the PR description.
- internal/store is the natural home: all three consumers already depend on the database layer, so it adds no new dependency edge.
- CONTRIBUTING's sibling-package convention prefers a small interface over a concrete dependency; a free function is the exception that fits here.
- Watch the Exp handling: internal/indexer/reconcile.go:148 scales by 10**Exp, so check whether the other two do the same before unifying.

---

## 15. test(stellar): add a fuzz target for the eight contract event decoders

`complexity: medium` · 150 points · area `stellar`

**What's wrong.** internal/stellar/events.go exposes eight decoders taking a base64 string straight off the chain — DecodePriceBookPublishEvent at line 98 through DecodeChannelRefundEvent at line 381. The repo has zero fuzz targets. This input is attacker-adjacent: anyone can emit an event with matching topics on a public network, and issue #7 is a live case of a decoder meeting a real payload it did not expect and wedging cmd/indexer's cursor permanently. A fuzzer is the cheapest way to find the next one before the network does.

**Proposed fix.** Add a fuzz target driving the eight Decode*Event functions with arbitrary base64 input, asserting each either returns a typed error or a fully-populated struct, and never panics.

**Acceptance criteria**

1. A Fuzz function covers all eight Decode*Event entry points
2. No input causes a panic, an out-of-range slice index, or an unbounded allocation
3. Every rejection path returns an error that names the field and the expected type, not a bare failure
4. A seed corpus is committed from the real recorded payloads already in testdata/stellar/
5. The target runs in CI with a bounded -fuzztime so it cannot hang the pipeline

**Guidance**

- testdata/stellar/ already holds recorded real events — seed from those rather than inventing corpus entries, per CLAUDE.md §8's recorded-responses-only rule.
- Issue #7's three-field Withdraw payload is the known-bad case; assert the fuzzer reaches that shape.
- Keep the fuzz target in package stellar so it can see the unexported helpers the decoders share.

---

## 16. test(stellar): add a fuzz target for the ScVal decode helpers

`complexity: medium` · 150 points · area `stellar`

**What's wrong.** internal/stellar/xdr.go:177-295 defines twelve decoders — DecodeBool through DecodeMap — each of which must reject a mismatched ScVal type. There are no fuzz targets in the repo. DecodeI128 at line 218 is the one that matters most: it reassembles a 128-bit amount from hi and lo words, and every charged amount and settlement figure in the system passes through it. A sign or shift error there is a money error, and it is exactly the class a fuzzer finds.

**Proposed fix.** Add a fuzz target over the twelve Decode* ScVal helpers, asserting type confusion is always reported as ErrWrongScValType rather than producing a wrong value.

**Acceptance criteria**

1. A Fuzz function reaches all twelve Decode* helpers with both matching and mismatched ScVal types
2. Every type mismatch returns *ErrWrongScValType and never a zero value with a nil error
3. DecodeI128 round-trips against ScvI128 for arbitrary big.Int inputs across the full signed 128-bit range, including both extremes
4. No input panics or allocates unboundedly

**Guidance**

- internal/stellar/xdr_test.go already covers round trips — extend that file's helpers rather than duplicating the setup.
- The I128 two's-complement handling at xdr.go:222-226 is the highest-value thing to fuzz; build the corpus around its boundaries.
- Use the xdr package's own types to construct inputs; fuzzing raw bytes through the XDR unmarshaller tests the SDK, not this code.

---

## 17. test(meter): add a fuzz target for ParseSchedule

`complexity: medium` · 150 points · area `meter`

**What's wrong.** internal/meter/schedule.go:81 parses an operator's price schedule from raw bytes, guarded by six sentinels at lines 24-29 including ErrUnitPriceOutOfRange, which exists specifically to keep a price inside the i128 range statement_registry uses. The repo has no fuzz targets. This function decides what every request is charged; a schedule that parses into a valid-looking struct with a bad unit price prices real traffic wrongly, and the pricing path has no second check behind it.

**Proposed fix.** Add a fuzz target for ParseSchedule over arbitrary JSON, asserting every malformed schedule is rejected with one of the six named sentinels and no partially-populated Schedule escapes.

**Acceptance criteria**

1. A Fuzz function drives ParseSchedule with arbitrary bytes and never panics
2. Every returned error matches one of the six package sentinels via errors.Is; no bare or wrapped-unknown error escapes
3. A successful parse always yields a Schedule whose every rule has a non-empty method, a path_template starting with /, and a unit price within the i128 range
4. A rejected parse returns a nil Schedule, never a partially-filled one

**Guidance**

- internal/meter/schedule_test.go already enumerates the rejection cases — seed the corpus from its table so the fuzzer starts from known-interesting inputs.
- The i128 bound at schedule.go:29 is the subtle one: check it against the exact range, not math.MaxInt64.
- Assert the nil-on-error contract explicitly; a non-nil Schedule alongside an error is the bug most likely hiding here.

---

## 18. test(merkle): add a fuzz target asserting tree and proof agreement

`complexity: medium` · 150 points · area `merkle`

**What's wrong.** internal/merkle/tree.go builds the sorted-pair tree whose roots the deployed statement_registry accepts or rejects, and enforces the 32-node cap at line 93. The repo has no fuzz targets. CLAUDE.md §4 warns that sorted-pair implementations diverge on unbalanced trees and that the seven-leaf fixture is where that shows; a fuzzer over arbitrary leaf counts covers the unbalanced shapes no fixture enumerates. A root this package gets wrong is a proof the contract refuses on chain, which is an anchor that cannot be submitted.

**Proposed fix.** Add a fuzz target that builds a tree from an arbitrary leaf set and asserts every generated proof verifies against the root, and that no proof exceeds the contract's 32-node cap.

**Acceptance criteria**

1. A Fuzz function builds trees from 1 to at least 1024 arbitrary leaves without panicking
2. Every leaf's generated proof satisfies VerifyProof against the tree root
3. No generated proof exceeds 32 nodes, and a leaf set large enough to breach it returns the documented error rather than a long proof
4. Duplicate leaves in one set behave as the existing documented duplicate handling specifies, not by silent dedup
5. The existing testdata/merkle fixture roots still match exactly

**Guidance**

- Do not regenerate testdata/merkle — CLAUDE.md §2 requires those stay byte-identical to the Rust reference; the fuzzer complements them.
- tree.go:45's duplicate-leaf map is the subtle interaction; fuzz leaf sets containing repeats deliberately.
- Sorted-pair hashing means a proof node can equal its sibling — contracts repo issue #13 tracks exactly that fixture gap, so include the case here.

---

## 19. test(store): add a fuzz target for ParseMigrations

`complexity: low` · 100 points · area `store` · `good first issue`

**What's wrong.** internal/store/migrate.go:39 parses numbered migration files into an ordered list, and §5 is explicit that an applied migration must never be edited. The repo has no fuzz targets. Migration ordering decides the shape of the commitments table — "THE MOST IMPORTANT TABLE IN THIS SCHEMA" — so a filename the parser mis-sorts or silently skips is a schema applied in the wrong order on a fresh database, which is a failure nobody sees until production.

**Proposed fix.** Add a fuzz target for ParseMigrations over arbitrary in-memory filesystems, asserting malformed or out-of-order migration filenames are rejected rather than silently reordered.

**Acceptance criteria**

1. A Fuzz function drives ParseMigrations with arbitrary fstest.MapFS contents and never panics
2. Duplicate version numbers, gaps, non-numeric prefixes and zero-padded variants each return a named error rather than being skipped
3. A successful parse always returns migrations in strictly ascending version order
4. ErrNoMigrations is returned for an empty filesystem, matched with errors.Is

**Guidance**

- testing/fstest.MapFS is the right input type; internal/store/migrations_fs_test.go already uses it.
- The ordering assertion matters more than the error text: a silently reordered list is worse than a rejected one.
- Zero-padding is the trap — 0010 must sort after 0009, not lexically before 002.

**Entry point:** `internal/store/migrate.go`

---

## 20. test(httpapi): add a benchmark for the metering middleware hot path

`complexity: medium` · 150 points · area `httpapi`

**What's wrong.** CLAUDE.md §5 calls the requests table "The hot path", and internal/httpapi/metering.go is the code on it — every metered request passes through it, generating a request ID, hashing the endpoint and pricing against the catalog. The repo has zero benchmarks, so the per-request cost of metering a paid API is simply unknown, and so is whether a change to it regressed anything. An operator deciding whether to put this in front of a high-volume API has no number to reason about.

**Proposed fix.** Add a benchmark over the metering middleware measuring per-request overhead and allocations, with the Recorder and LedgerSource stubbed so it measures this package only.

**Acceptance criteria**

1. A Benchmark function measures the middleware end to end with stubbed Recorder and LedgerSource
2. Allocations per request are reported via b.ReportAllocs and recorded in the PR description as the baseline
3. The benchmark does not touch Postgres or the network, so it runs in CI without the service container
4. A second benchmark isolates endpoint hashing and request-ID generation, so a regression can be attributed

**Guidance**

- internal/httpapi/metering_test.go already has stub implementations of both interfaces — reuse them rather than writing new ones.
- crypto/rand for the request ID is likely the dominant cost; measure it separately before concluding anything about the rest.
- Report the baseline numbers in the PR body; a benchmark with no recorded baseline cannot detect a future regression.

---

## 21. test(merkle): add benchmarks for tree building and proof generation

`complexity: low` · 100 points · area `merkle` · `good first issue`

**What's wrong.** internal/merkle/tree.go builds a tree over every request in a billing period before a statement can be anchored. The repo has zero benchmarks. A period covering a month of a busy API is a large leaf set, and nothing records how long that takes or how much it allocates — so nobody knows whether period closing is a millisecond or a minute, which is the difference between anchoring on time and holding a transaction open against the price-change boundary §4 forbids a statement from spanning.

**Proposed fix.** Add benchmarks for BuildTree and proof generation across representative leaf counts, so the cost of closing a billing period is a known number.

**Acceptance criteria**

1. Benchmarks cover BuildTree at several leaf counts spanning at least three orders of magnitude
2. A separate benchmark measures generating proofs for every leaf in a built tree
3. b.ReportAllocs is enabled and baseline numbers are recorded in the PR description
4. The benchmarks use deterministic synthetic leaves, not the fixtures, so timings are stable

**Guidance**

- internal/merkle/tree_test.go has leaf-generation helpers already — reuse them.
- Use b.ResetTimer after generating leaves so setup is excluded from the measurement.
- Do not benchmark against testdata/merkle fixtures: they are tiny and fixed, which tells you nothing about scale.

**Entry point:** `internal/merkle/tree_test.go`

---

## 22. test(settle): add a benchmark for exposure calculation

`complexity: low` · 100 points · area `settle` · `good first issue`

**What's wrong.** internal/settle/exposure.go:72 runs two correlated subqueries over commitments for every channel, on every settler tick. The repo has zero benchmarks. §6 requires this to run for every open channel each tick, so the per-channel cost sets the ceiling on how many channels one settler can watch — and the settler is the component that decides whether the operator gets paid. That ceiling is currently unmeasured.

**Proposed fix.** Add a benchmark for Calculator.Calculate against a seeded Postgres instance, measuring the per-channel cost of the exposure query at realistic commitment counts.

**Acceptance criteria**

1. A benchmark measures Calculate against a channel seeded with commitment counts spanning at least three orders of magnitude
2. Baseline timings are recorded in the PR description for each size
3. The benchmark skips cleanly when Postgres is unavailable, matching the existing integration-test guard
4. Whether the index at migration 0003 is actually used is confirmed with EXPLAIN and stated in the PR

**Guidance**

- internal/settle/helpers_test.go:28 holds the existing Postgres-availability guard — reuse it so the benchmark behaves like the integration tests.
- The MIN(received_at) subquery filtered on cumulative_amount > last_settled_amount is the one most likely to scan; check it with EXPLAIN first.
- scripts/test-ci.sh fails CI on a skipped test — confirm a skipped benchmark does not trip it.

**Entry point:** `internal/merkle/tree_test.go`

---

