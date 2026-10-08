# Chatwoot Analytics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add four read-only MCP tools for attention queues, period summaries, per-conversation timing metrics, and performance comparisons using Chatwoot's native data.

**Architecture:** Preserve the operational `core.API` seam and add a focused `core.ReportsAPI` seam implemented by the existing Chatwoot client. A new `internal/analytics` module owns validation, pagination, aggregation, clocks, comparisons, and analytics error classification; the MCP adapter only validates schemas, bounds text, and maps results.

**Tech Stack:** Go 1.25, standard library HTTP/JSON/time/sort packages, official MCP Go SDK v1.8.0, `httptest`, table-driven tests.

**Spec:** `docs/superpowers/specs/2026-10-08-chatwoot-analytics-design.md`

## Global Constraints

- New functionality is read-only and must not mutate Chatwoot or persist conversation data.
- Use native Chatwoot report metrics; never replace `403`, unavailable routes, or missing fields with message-derived estimates.
- `list_attention_queue` defaults to `status=open`, `limit=20`, `start_page=1`, caps `limit` at 50, and scans at most 10 upstream pages.
- `get_analytics_summary` and `compare_performance` accept RFC 3339 timestamps with explicit offsets and reject intervals longer than 183 days.
- Summary scope defaults to `account` and accepts `account`, `agent`, `inbox`, `team`, or `label`; non-account scopes require a positive `scope_id`.
- Comparison grouping is `agent`, `team`, `inbox`, or `channel` and uses the immediately preceding interval of equal duration.
- Preserve missing numeric data as missing; never coerce it to zero. Omit percentage deltas when the previous value is absent or zero.
- Every collection exposes limit/completeness semantics. Partial queue scans must never appear globally complete.
- MCP outputs and errors must never contain the token, upstream body, redirect target, or unbounded text.
- Report requests send the configured token in both `api_access_token` and `Authorization: Bearer`; redirects remain disabled.
- Do not add a database, cache, webhook, scheduler, dashboard, CSAT, sentiment, or grouped-label comparison.
- Use TDD for each task and commit only after its focused tests pass.

## Review Focus

- Future `waiting_since`: clamp `waiting_seconds` to zero; Task 3 tests this.
- Repeated/backward pagination: stop and mark incomplete instead of looping; Task 3 tests this.
- Numeric values as number, string, `null`, or empty string: normalize supported values and reject nonnumeric text; Task 2 tests this.
- A date range spanning DST: derive the previous period from elapsed duration, not calendar subtraction; Task 4 tests this.
- One comparison period succeeds and the other fails: return the failure and no mixed result; Task 4 tests this.

---

### Task 1: Freeze reporting contracts and queue metadata

**Files:**
- Create: `internal/core/reporting.go`
- Modify: `internal/core/types.go`
- Modify: `internal/chatwoot/client.go`
- Modify: `internal/chatwoot/client_test.go`

**Interfaces:**
- Consumes: `core.Page[core.Conversation]`, `core.ListOptions`, and `chatwoot.Client.call`.
- Produces: report domain types, `core.ReportsAPI`, queue metadata on `core.Conversation`, and `TeamID` on `core.ListOptions`.

- [ ] **Step 1: Write failing queue-decoding tests**

Add `TestListConversationsDecodesAttentionMetadata`. Its server returns:

```go
payload := `{"data":{"meta":{"all_count":1},"payload":[{
  "id":123,"inbox_id":2,"status":"open","priority":"high",
  "waiting_since":1791451800,"last_activity_at":1791451805,
  "unread_count":3,"meta":{"sender":{"id":45},"assignee":{"id":8},"team":{"id":9}}
}]}}`
```

Call `ListConversations` with `core.ListOptions{Page: 1, Status: "open", InboxID: 2, TeamID: 9}`. Assert `team_id=9` and all queue fields, assignee, and team decode. Add absent/null fields and assert zero values decode safely.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/chatwoot -run TestListConversationsDecodesAttentionMetadata -count=1
```

Expected: compile failure because the new fields do not exist.

- [ ] **Step 3: Add exact report contracts**

Create `internal/core/reporting.go`:

```go
package core

import ("context"; "time")

type ReportScope string
const (
    ReportScopeAccount ReportScope = "account"
    ReportScopeAgent ReportScope = "agent"
    ReportScopeInbox ReportScope = "inbox"
    ReportScopeTeam ReportScope = "team"
    ReportScopeLabel ReportScope = "label"
)

type ReportGroup string
const (
    ReportGroupAgent ReportGroup = "agent"
    ReportGroupTeam ReportGroup = "team"
    ReportGroupInbox ReportGroup = "inbox"
    ReportGroupChannel ReportGroup = "channel"
)

type ReportRange struct { Since, Until time.Time }
type ReportSummaryRequest struct { Range ReportRange; Scope ReportScope; ScopeID int64 }
type GroupedReportRequest struct { Range ReportRange; Group ReportGroup }

type ReportMetrics struct {
    ConversationsCount, IncomingMessagesCount, OutgoingMessagesCount *int64
    ResolutionsCount, OpenCount, PendingCount, SnoozedCount *int64
    AvgFirstResponseSeconds, AvgResolutionSeconds, AvgReplySeconds *float64
}
type ReportSummary struct { Current, Previous ReportMetrics }
type ReportingEvent struct {
    ID int64
    Name string
    ValueSeconds, BusinessValueSeconds *float64
    EventStartTime, EventEndTime time.Time
    ConversationID, InboxID, UserID int64
}
type GroupedReportRow struct { Key string; ID int64; Name string; Metrics ReportMetrics }

type ReportsAPI interface {
    GetReportSummary(context.Context, ReportSummaryRequest) (ReportSummary, error)
    GetConversationReportingEvents(context.Context, int64) ([]ReportingEvent, error)
    GetGroupedReport(context.Context, GroupedReportRequest) ([]GroupedReportRow, error)
}
```

Add JSON tags matching the spec. Add `WaitingSince int64`, `LastActivityAt int64`, and `UnreadCount int` to `core.Conversation`; add `TeamID int64` to `core.ListOptions`.

- [ ] **Step 4: Decode and forward metadata**

Extend `conversationWire` with the three queue fields and `meta.assignee.id`/`meta.team.id`, copy them in `toCore`, and add `team_id` only when `opts.TeamID > 0`.

- [ ] **Step 5: Run GREEN and commit**

```bash
go test ./internal/core ./internal/chatwoot -count=1
git add internal/core/reporting.go internal/core/types.go internal/chatwoot/client.go internal/chatwoot/client_test.go
git commit -m "feat(core): define analytics reporting contracts"
```

### Task 2: Implement the Chatwoot reports adapter

**Files:**
- Create: `internal/chatwoot/reports.go`
- Create: `internal/chatwoot/reports_test.go`
- Modify: `internal/chatwoot/client.go`

**Interfaces:**
- Consumes: every type and method in `core.ReportsAPI` from Task 1.
- Produces: `var _ core.ReportsAPI = (*Client)(nil)` and all `ReportsAPI` methods.

- [ ] **Step 1: Write failing adapter tests**

Add:

```go
func TestGetReportSummary(t *testing.T)
func TestGetConversationReportingEvents(t *testing.T)
func TestGetGroupedReport(t *testing.T)
func TestReportNumberVariants(t *testing.T)
func TestReportFailuresAreTyped(t *testing.T)
```

Assert paths `/api/v2/accounts/7/reports/summary`, `/api/v1/accounts/7/conversations/123/reporting_events`, and `/api/v2/accounts/7/summary_reports/{agent|team|inbox|channel}`. Assert Unix query timestamps, scope parameters, and both auth headers. Cover numeric JSON/string, zero, `null`, `""`, nonnumeric string, 401/403/404/429/500, malformed JSON, timeout, and redirect.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/chatwoot -run 'Test(GetReport|ReportNumber|ReportFailures)' -count=1
```

Expected: report methods are missing.

- [ ] **Step 3: Add report-aware auth without changing existing calls**

Keep `call` as a wrapper over:

```go
func (c *Client) callWithBearer(ctx context.Context, method, path string, query url.Values, reqBody, resBody any, resource string, bearer bool) error
```

Preserve all current request, body-limit, redirect, decode, and error logic. Always set `api_access_token`; when `bearer`, also set `Authorization: Bearer <same token>`. Never retry.

- [ ] **Step 4: Implement tolerant optional numbers**

An unexported `optionalNumber.UnmarshalJSON` treats `null` and `""` as missing; parses JSON numbers and numeric strings with `strconv.ParseFloat`; rejects booleans, containers, NaN, infinity, and nonnumeric strings. Integer metrics require an integral, in-range `int64` value.

- [ ] **Step 5: Implement summary and event methods**

```go
func (c *Client) GetReportSummary(ctx context.Context, req core.ReportSummaryRequest) (core.ReportSummary, error)
func (c *Client) GetConversationReportingEvents(ctx context.Context, id int64) ([]core.ReportingEvent, error)
```

Use `callWithBearer(..., true)`, normalize metric fields, parse event timestamps as RFC 3339, preserve order, and turn invalid timestamps/types into `KindInvalid` without response-body leakage.

- [ ] **Step 6: Implement grouped normalization**

Use a fixed `ReportGroup` to path map. Normalize IDs, keys, names, counts, and timing metrics. Channel rows use their channel key and leave unavailable timings nil. Reject unknown groups before HTTP.

- [ ] **Step 7: Run GREEN and commit**

```bash
go test ./internal/chatwoot -count=1
git add internal/chatwoot/client.go internal/chatwoot/reports.go internal/chatwoot/reports_test.go
git commit -m "feat(chatwoot): read native analytics reports"
```

### Task 3: Build the attention queue module

**Files:**
- Create: `internal/analytics/service.go`
- Create: `internal/analytics/errors.go`
- Create: `internal/analytics/queue.go`
- Create: `internal/analytics/queue_test.go`

**Interfaces:**
- Consumes: `core.ListOptions`, `core.Page[core.Conversation]`, and later `core.ReportsAPI`.
- Produces: `analytics.Service`, `ConversationSource`, queue request/result types, and typed analytics errors.

- [ ] **Step 1: Write failing queue tests**

With a scripted page source and fixed clock, add:

```go
func TestAttentionQueueDefaultsFiltersSortsAndLimits(t *testing.T)
func TestAttentionQueueFiltersTeamAndAssignee(t *testing.T)
func TestAttentionQueueReportsPartialTenPageScan(t *testing.T)
func TestAttentionQueueStopsOnRepeatedOrBackwardPage(t *testing.T)
func TestAttentionQueueClampsFutureWaitingSince(t *testing.T)
func TestAttentionQueueValidatesBeforeCallingSource(t *testing.T)
```

Use `2026-10-08T15:00:00Z`, include equal wait timestamps, and assert ID tie-breaking. Repeated cursor must return without looping with `Complete=false`.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/analytics -run AttentionQueue -count=1
```

- [ ] **Step 3: Define interfaces and errors**

```go
type ConversationSource interface {
    ListConversations(context.Context, core.ListOptions) (core.Page[core.Conversation], error)
}
type Service interface {
    ListAttentionQueue(context.Context, QueueRequest) (QueueResult, error)
    GetSummary(context.Context, SummaryRequest) (SummaryResult, error)
    GetConversationMetrics(context.Context, int64) (ConversationMetricsResult, error)
    ComparePerformance(context.Context, CompareRequest) (ComparisonResult, error)
}
func New(conversations ConversationSource, reports core.ReportsAPI, now func() time.Time) Service
```

`New` defaults nil `now` to `time.Now`. Define error wire codes matching the operational service plus `unsupported_feature`; retain an optional typed `*chatwoot.Error`, never raw body text.

- [ ] **Step 4: Define queue types and validation**

Define these JSON-tagged types:

```go
type QueueRequest struct {
    Status string
    InboxID, TeamID, AssigneeID int64
    Limit, StartPage int
}
type AttentionConversation struct {
    ConversationID, ContactID, InboxID int64
    ChannelType, Status, Priority string
    AssigneeID, TeamID int64
    WaitingSince time.Time
    WaitingSeconds int64
    LastActivityAt time.Time
    UnreadCount int
}
type QueueResult struct {
    ObservedAt time.Time
    Conversations []AttentionConversation
    ScannedPages, ScannedConversations int
    Complete bool
    NextPage int
    Truncated bool
}
```

Apply exact defaults/caps from Global Constraints. Reject unknown status, negative IDs, negative limit/page, and limit over 50 before source access.

- [ ] **Step 5: Implement bounded scanning**

Read at most 10 pages. Send status/inbox/team upstream, filter assignee locally, keep only positive `WaitingSince`, detect unusable cursors, sort by waiting time then ID, limit rows, set completeness/truncation independently, and clamp future wait seconds to zero.

- [ ] **Step 6: Run GREEN and commit**

```bash
go test ./internal/analytics -run AttentionQueue -count=1
git add internal/analytics/service.go internal/analytics/errors.go internal/analytics/queue.go internal/analytics/queue_test.go
git commit -m "feat(analytics): add bounded attention queue"
```

### Task 4: Implement summaries, conversation metrics, and comparisons

**Files:**
- Create: `internal/analytics/reports.go`
- Create: `internal/analytics/reports_test.go`
- Modify: `internal/analytics/errors.go`
- Modify: `internal/analytics/service.go` (replace the temporary reporting request/result placeholders and unsupported-feature stubs from Task 3)

**Interfaces:**
- Consumes: `core.ReportsAPI` and report types from Task 1; `analytics.Service` from Task 3.
- Produces: `SummaryRequest`, `SummaryResult`, `ConversationMetricsSummary`, `ConversationMetricsResult`, `CompareRequest`, `MetricDelta`, `ComparisonRow`, and `ComparisonResult`.

- [ ] **Step 1: Write failing validation and summary tests**

Use a fake `ReportsAPI` and cover malformed timestamps, missing offsets, equal/reversed endpoints, 183 days plus one second, invalid scopes, account with nonzero ID, and non-account with zero ID. Assert zero calls on invalid requests. A valid case must verify UTC-normalized instants and unchanged optional pointers.

- [ ] **Step 2: Write failing conversation-metric tests**

Cover no events, multiple `reply_time` events, over 50 events, absent values, first of multiple `first_response` events, and last of multiple `resolution` events. Assert the summary uses every event while the result retains the newest 50 in chronological order with exact counts.

- [ ] **Step 3: Write failing comparison tests**

```go
func TestComparisonUsesPreviousEqualElapsedRangeAcrossDST(t *testing.T)
func TestComparisonJoinsRowsByStableKey(t *testing.T)
func TestComparisonOmitsPercentWhenPreviousIsZeroOrMissing(t *testing.T)
func TestComparisonFailsWholeResultWhenEitherPeriodFails(t *testing.T)
func TestComparisonBoundsRowsDeterministically(t *testing.T)
```

For DST, assert the previous range is `{Since: currentSince.Add(-currentUntil.Sub(currentSince)), Until: currentSince}`.

- [ ] **Step 4: Run RED**

```bash
go test ./internal/analytics -run 'Summary|ConversationMetrics|Comparison' -count=1
```

- [ ] **Step 5: Implement range/scope validation**

Parse `time.RFC3339`, require explicit `Z` or numeric offset, normalize UTC, and enforce positive duration no greater than `183*24*time.Hour`. Return `CodeInvalidInput` before API access.

Define the public shapes before implementing methods:

```go
type SummaryRequest struct { Since, Until string; Scope string; ScopeID int64 }
type SummaryResult struct {
    Since, Until time.Time
    Scope string
    ScopeID int64
    Current, Previous core.ReportMetrics
}
type ConversationMetricsSummary struct {
    FirstResponseSeconds, FirstResponseBusinessSeconds *float64
    ResolutionSeconds, ResolutionBusinessSeconds *float64
    ReplyEvents int
    AvgReplySeconds *float64
}
type ConversationMetricsResult struct {
    ConversationID int64
    Summary ConversationMetricsSummary
    Events []core.ReportingEvent
    TotalEvents, ReturnedEvents int
    Truncated bool
}
type CompareRequest struct { Since, Until, GroupBy string; Limit int }
type MetricDelta struct { Absolute float64; Percent *float64 }
type MetricDeltas struct {
    ConversationsCount, IncomingMessagesCount, OutgoingMessagesCount *MetricDelta
    ResolutionsCount, OpenCount, PendingCount, SnoozedCount *MetricDelta
    AvgFirstResponseSeconds, AvgResolutionSeconds, AvgReplySeconds *MetricDelta
}
type ComparisonRow struct {
    Key string
    ID int64
    Name string
    Current, Previous core.ReportMetrics
    Delta MetricDeltas
}
type ComparisonResult struct {
    CurrentRange, PreviousRange core.ReportRange
    GroupBy string
    Rows []ComparisonRow
    TotalRows, ReturnedRows int
    Truncated bool
}
```

Add JSON tags matching the spec's wire names to every result field.

- [ ] **Step 6: Implement summary and event aggregation**

`GetSummary` builds `core.ReportSummaryRequest` and echoes normalized range/scope. `GetConversationMetrics` rejects nonpositive IDs, fetches once, uses the first first-response event, last resolution event, and mean of present reply-time values, then keeps the newest 50 events.

- [ ] **Step 7: Implement atomic two-period comparison**

Validate group/limit, derive the previous elapsed range, call current then previous, and abort on either error. Join by `Key`, falling back to decimal ID; reject a row with neither. Calculate absolute deltas for present pairs and percentage only for nonzero previous values. Sort by stable key and limit after computing `TotalRows`.

- [ ] **Step 8: Map upstream errors safely**

Use `errors.As` for `*chatwoot.Error`. Map summary/group `404` to `unsupported_feature`, conversation-event `404` to `not_found`, context cancellation to `timeout`, and all other kinds to their matching stable codes. Retain only the typed API error for safe diagnostics.

- [ ] **Step 9: Run GREEN and commit**

```bash
go test ./internal/analytics -count=1
git add internal/analytics/errors.go internal/analytics/reports.go internal/analytics/reports_test.go
git commit -m "feat(analytics): aggregate chatwoot report metrics"
```

### Task 5: Expose four bounded MCP tools

**Files:**
- Create: `internal/mcpserver/analytics_tools.go`
- Create: `internal/mcpserver/analytics_tools_test.go`
- Modify: `internal/mcpserver/server.go`
- Modify: `internal/mcpserver/server_test.go`

**Interfaces:**
- Consumes: `analytics.Service` from Tasks 3–4.
- Produces: `Run(context.Context, service.Service, analytics.Service, io.Reader, io.Writer) error` and the four tool registrations.

- [ ] **Step 1: Extend schema tests and create a fake analytics service**

Add sorted names and required fields:

```go
"list_attention_queue": nil,
"get_analytics_summary": {"since", "until"},
"get_conversation_metrics": {"conversation_id"},
"compare_performance": {"since", "until", "group_by"},
```

Update server test helpers to receive operational and analytics fakes separately, and change the compile-time `Run` assertion.

- [ ] **Step 2: Write failing in-memory MCP calls**

Assert exact input mapping and preservation of completeness, ranges, missing values, truncation, totals, and seconds. Include oversized UTF-8 names/keys and require 256-byte-safe truncation plus `text_truncated=true`. Verify no message content appears.

- [ ] **Step 3: Run RED**

```bash
go test ./internal/mcpserver -run 'Registers|AttentionQueue|AnalyticsSummary|ConversationMetrics|ComparePerformance' -count=1
```

- [ ] **Step 4: Register explicit tool schemas**

In `analytics_tools.go`, define input structs with JSON schema descriptions and call each analytics method once. Use local output projections only for text-bearing rows; do not apply another row cap beyond the service's maximum 50.

- [ ] **Step 5: Generalize safe error mapping**

Make `errorCodeOf`/`fixedErrorMessage` use stable strings and recognize both `*service.Error` and `*analytics.Error`. Extract safe HTTP diagnostics directly through `errors.As` to `*chatwoot.Error`. Add this fixed message:

```go
case string(analytics.CodeUnsupportedFeature):
    return "this analytics feature is not available in the configured Chatwoot version"
```

Test analytics 403, unsupported route, invalid response diagnostics, and secret-bearing upstream messages for non-leakage.

- [ ] **Step 6: Change server composition**

Use exact shapes:

```go
func Run(ctx context.Context, svc service.Service, insights analytics.Service, stdin io.Reader, stdout io.Writer) error
func newServer(svc service.Service, insights analytics.Service) *mcp.Server
func newServerWithLogger(svc service.Service, insights analytics.Service, logger *slog.Logger) *mcp.Server
```

Register analytics after existing tools. Production and tests must pass a non-nil analytics service.

- [ ] **Step 7: Run GREEN and commit**

```bash
go test ./internal/mcpserver -count=1
git add internal/mcpserver/analytics_tools.go internal/mcpserver/analytics_tools_test.go internal/mcpserver/server.go internal/mcpserver/server_test.go
git commit -m "feat(mcp): expose chatwoot analytics tools"
```

### Task 6: Wire production, panel, and documentation

**Files:**
- Modify: `cmd/chatwoot-mcp/main.go`
- Modify: `cmd/chatwoot-mcp/main_test.go`
- Modify: `internal/panel/tools_catalog.go`
- Modify: `internal/panel/server_test.go`
- Modify: `README.md`
- Modify: `docs/configuration.md`

**Interfaces:**
- Consumes: `analytics.New(api, api, nil)` and the new `mcpserver.Run` signature.
- Produces: production composition, panel discovery for 26 tools, and usage/troubleshooting docs.

- [ ] **Step 1: Write failing composition/catalog tests**

Change the fake `mcpRunner` to receive both services; capture them in the `mcp` command test and assert neither is nil. Extend panel expectations with all four names and ensure every tool appears once.

- [ ] **Step 2: Run RED**

```bash
go test ./cmd/chatwoot-mcp ./internal/panel -count=1
```

- [ ] **Step 3: Wire one client into both modules**

```go
type mcpRunner func(context.Context, service.Service, analytics.Service, io.Reader, io.Writer) error

api := chatwoot.NewClient(settings, nil)
operations := service.New(api)
insights := analytics.New(api, api, nil)
return serveMCP(ctx, operations, insights, stdin, stdout)
```

Do not create a second client or token copy.

- [ ] **Step 4: Update panel and docs**

Add concise Portuguese descriptions for the four tools. In `README.md`, add example prompts for oldest wait, weekly summary, conversation timing, and agent comparison; document `complete`, `next_page`, `truncated`, permissions, version sensitivity, and the 183-day cap. In `docs/configuration.md`, add `forbidden` and `unsupported_feature` troubleshooting without promising an unvalidated version.

- [ ] **Step 5: Run targeted tests**

```bash
go test ./cmd/chatwoot-mcp ./internal/panel -count=1
```

- [ ] **Step 6: Run full verification**

```bash
gofmt -w internal/core/reporting.go internal/core/types.go internal/chatwoot/client.go internal/chatwoot/reports.go internal/chatwoot/reports_test.go internal/analytics/*.go internal/mcpserver/analytics_tools.go internal/mcpserver/analytics_tools_test.go internal/mcpserver/server.go internal/mcpserver/server_test.go cmd/chatwoot-mcp/main.go cmd/chatwoot-mcp/main_test.go internal/panel/tools_catalog.go internal/panel/server_test.go
gofmt -l .
go test ./...
go vet ./...
git diff --check
```

Expected: no formatter/diff output; tests and vet pass.

- [ ] **Step 7: Commit composition/docs**

```bash
git add cmd/chatwoot-mcp/main.go cmd/chatwoot-mcp/main_test.go internal/panel/tools_catalog.go internal/panel/server_test.go README.md docs/configuration.md
git commit -m "docs: document chatwoot analytics workflows"
```

### Task 7: Validate compatibility against a real Chatwoot instance

**Files:**
- Create if a real validation occurs: `docs/analytics-validation.md`
- Modify for confirmed incompatibilities only: owning implementation and test files from Tasks 1–6.

**Interfaces:**
- Consumes: built binary and an externally configured test account.
- Produces: credential-safe compatibility evidence or an explicit unvalidated status.

- [ ] **Step 1: Build and re-run automation**

```bash
go build -o ./bin/chatwoot-mcp ./cmd/chatwoot-mcp
go test ./...
```

- [ ] **Step 2: Run a safe smoke matrix if a test account exists**

Invoke all four tools through MCP. Record only Chatwoot version, status category, route family, field presence/type, completeness flags, and role. Never record token, names, contacts, message content, emails, phone numbers, or raw bodies. Cover open/pending queue, account and one scoped summary, existing/missing conversation metrics, all four comparison groups, and admin/non-admin where available.

- [ ] **Step 3: Convert incompatibilities into regression tests first**

For any differing route/shape, add the smallest failing `httptest` fixture in the owning package, implement normalization, then rerun the package and `go test ./...`. Do not add estimates or untested version branching.

- [ ] **Step 4: Record truthful status**

If validated, create `docs/analytics-validation.md` with date, version, roles, supported matrix, and limitations. If no account is available, do not create the file; state the missing external validation in the final handoff.

- [ ] **Step 5: Commit only actual validation changes**

```bash
git add docs/analytics-validation.md internal/core internal/chatwoot internal/analytics internal/mcpserver
git commit -m "test: validate chatwoot analytics compatibility"
```

Skip the commit when no validation file or compatibility change exists.
