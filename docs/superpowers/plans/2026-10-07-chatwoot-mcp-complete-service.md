# Chatwoot MCP Complete Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the local Chatwoot MCP from consultation and text replies to the full agent workflow across every inbox the configured user can access.

**Architecture:** Extend the v1 service and API ports with narrowly scoped operations. Register new MCP tools in independent groups while preserving the v1 contracts. Apply channel capability checks before writing and return the actual Chatwoot state after every action.

**Tech Stack:** Go 1.25+, official MCP Go SDK, Chatwoot Application API, existing panel and configuration from v1.

**Spec:** [Chatwoot MCP design](../specs/2026-10-07-chatwoot-mcp-design.md). **Predecessor:** [v1 plan](2026-10-07-chatwoot-mcp-v1.md).

## Global Constraints

- Preserve the six v1 tool names and their schemas unless a versioned migration is approved.
- Require explicit conversation/contact/agent/team IDs for mutations; validate IDs against the configured account.
- Return permission and channel restriction errors as results; never claim delivery solely because Chatwoot accepted a request.
- No automatic replay of message creation or attachment POST after an ambiguous timeout.
- Attachment tools accept an explicit local file path supplied by the user/client, never a remote URL; reject directories and files above the configured size cap before reading into memory.
- Respect the channel support matrix. WhatsApp replies outside the allowed window require a preapproved template.
- The panel remains a setup and diagnostic surface, not a second inbox.

## Review Focus

1. `add_labels` must preserve existing labels even though Chatwoot's add-labels endpoint replaces the list. Task 2 exercises this.
2. Notes must use `private=true` and must never be exposed as a customer reply. Task 1 exercises this.
3. A WhatsApp conversation outside the reply window must not receive ordinary text. Task 3 exercises this.
4. A file that exceeds a channel limit must be rejected before upload when the limit is known. Task 3 exercises this.
5. A requested assignment to an unavailable agent or team must return a specific error. Task 2 exercises this.

## File Map and Parallel Boundaries

| Task | Files owned | Output |
|---|---|---|
| 0 | `internal/core/operations.go`, `internal/core/ports.go` | Frozen extended request/response types. |
| 1 | `internal/chatwoot/conversation_actions.go`, `internal/service/conversation_actions.go`, `internal/mcpserver/conversation_tools.go` and matching tests | Notes, status and priority. |
| 2 | `internal/chatwoot/organization_actions.go`, `internal/service/organization_actions.go`, `internal/mcpserver/organization_tools.go` and matching tests | Inboxes, agents, teams, assignments and labels. |
| 3 | `internal/chatwoot/rich_messages.go`, `internal/service/rich_messages.go`, `internal/mcpserver/rich_message_tools.go` and matching tests | Attachments and approved templates. |
| 4 | `internal/chatwoot/contact_actions.go`, `internal/service/contact_actions.go`, `internal/mcpserver/contact_tools.go` and matching tests | Contact details/update and supported conversation creation. |
| 5 | `internal/panel/tools_catalog.go`, `web/app.js`, `README.md`, `docs/configuration.md` | Expanded documentation and end-to-end integration. |

Task 0 extends the core interface before agents branch. Tasks 1, 2, 3 and 4 use separate files and can run in four worktrees in parallel. Task 5 begins only after all four pass review. Workers do not edit the shared v1 files except the coordinator's Task 0 changes. If a worker needs a shared change, it reports the proposed patch for coordinator integration.

## Task 0: Freeze expanded operation contracts

**Files:** Create `internal/core/operations.go`; modify `internal/core/ports.go`.

**Interfaces:** Extend `core.API` with the methods below. Service request types contain explicit IDs and return resource IDs plus the state reported by Chatwoot.

```go
type MessageRequest struct { ConversationID int64; Content string; Private bool; Template *TemplateRequest; Attachment *AttachmentRequest }
type StatusRequest struct { ConversationID int64; Status string; SnoozedUntil *time.Time }
type AssignmentRequest struct { ConversationID int64; AgentID, TeamID int64 }
type LabelsRequest struct { ConversationID int64; Labels []string }
type ContactUpdate struct { ContactID int64; Name, Email, Phone *string }
```

- [ ] **Step 1:** Add the structs and API method signatures for `CreateMessage`, `SetStatus`, `SetPriority`, `Assign`, `GetLabels`, `SetLabels`, `ListInboxes`, `ListAgents`, `ListTeams`, `GetContact`, `UpdateContact`, `CreateConversation` and `ListTemplates`. Migrate v1 `CreateMessage` to `MessageRequest` behind its unchanged `send_reply` tool.
- [ ] **Step 2:** Add a compile-time test that the concrete Chatwoot client implements the extended `core.API`. Update its existing fake with explicit unsupported errors until each task supplies the method.
- [ ] **Step 3:** Run `go test ./internal/core ./internal/chatwoot ./internal/service ./internal/mcpserver`, resolve compile errors and commit the contract before parallel dispatch.

## Task 1: Notes and conversation state

**Files:** Create `internal/chatwoot/conversation_actions.go`, `internal/service/conversation_actions.go`, `internal/mcpserver/conversation_tools.go` plus `_test.go` peers.

**Interfaces:** Adds `add_private_note`, `set_conversation_status` and `set_priority` tools.

- [ ] **Step 1: Write API and service tests.** Assert a note request sends `message_type=outgoing` and `private=true`; status accepts only `open`, `pending`, `resolved`, `snoozed`; `snoozed_until` is valid only with `snoozed`; priority reports the resulting value.

```go
if body.Private != true || body.MessageType != "outgoing" { t.Fatal("note could become public") }
```

- [ ] **Step 2:** Run focused package tests and confirm expected failures.
- [ ] **Step 3:** Implement API calls using Chatwoot message creation, explicit status toggle and priority endpoints. Register three tools with IDs, accepted values and descriptions that separate notes from customer replies.
- [ ] **Step 4:** Run focused tests; commit only Task 1 files.

## Task 2: Routing and organization

**Files:** Create `internal/chatwoot/organization_actions.go`, `internal/service/organization_actions.go`, `internal/mcpserver/organization_tools.go` plus `_test.go` peers.

**Interfaces:** Adds `list_inboxes`, `list_agents`, `list_teams`, `assign_conversation`, `get_conversation_labels`, `add_conversation_labels`, `remove_conversation_labels`.

- [ ] **Step 1: Write fixture tests.** Verify assignment payloads and refusal of unknown agent/team IDs. For labels, start with `existing=["vip"]`, add `"urgent"` and assert the outgoing set is `["vip","urgent"]`; remove `"vip"` and assert `["urgent"]`.
- [ ] **Step 2:** Run focused tests and confirm failures.
- [ ] **Step 3:** Implement read/union or read/difference/write around the Chatwoot labels endpoint, which replaces the whole label set. Return both previous and resulting sets so concurrent-change risk is visible. List inboxes, agents and teams before accepting routing IDs; return the actual assignment from Chatwoot.
- [ ] **Step 4:** Register tools and run focused tests; commit Task 2 files.

## Task 3: Attachments and channel templates

**Files:** Create `internal/chatwoot/rich_messages.go`, `internal/service/rich_messages.go`, `internal/mcpserver/rich_message_tools.go` plus `_test.go` peers.

**Interfaces:** Adds `send_attachment`, `list_message_templates` and `send_template`.

- [ ] **Step 1: Write tests.** Cover multipart field `attachments[]`, MIME/type and size checks, unsupported-channel rejection, template ID/name and parameter validation, `can_reply=false`, and ambiguous timeout without retry.

```go
if requestCount != 1 { t.Fatalf("duplicate outbound request: %d", requestCount) }
```

- [ ] **Step 2:** Run focused tests to confirm failures.
- [ ] **Step 3:** Implement local file reading with an explicit maximum size, MIME detection, channel policy and multipart upload. Reject remote URLs and directories. For WhatsApp, obtain available approved templates from the inbox endpoint and send template parameters through the documented message endpoint. For channels with no template requirement, return a clear unsupported result instead of guessing.
- [ ] **Step 4:** Register tools, run focused tests and commit Task 3 files.

## Task 4: Contact maintenance and new conversations

**Files:** Create `internal/chatwoot/contact_actions.go`, `internal/service/contact_actions.go`, `internal/mcpserver/contact_tools.go` plus `_test.go` peers.

**Interfaces:** Adds `get_contact`, `update_contact` and `create_conversation`.

- [ ] **Step 1: Write tests.** Cover partial contact updates without blanking omitted fields, wrong-account IDs, required inbox/contact/source identifiers, and outbound conversation refusal when the channel does not support initiation.
- [ ] **Step 2:** Run focused tests to confirm failures.
- [ ] **Step 3:** Implement contact reads/updates and conversation creation for supported channels only. Return the new conversation ID and inbox/channel type. Validate the selected contact can be reached through that inbox before calling create.
- [ ] **Step 4:** Register tools, run focused tests and commit Task 4 files.

## Task 5: Integration and complete-service acceptance

**Files:** Modify `internal/panel/tools_catalog.go`, `web/app.js`, `README.md`, `docs/configuration.md`.

**Interfaces:** Consumes all Task 1–4 tools; no new domain contract.

- [ ] **Step 1:** Update the panel's tool catalog and examples for notes, routing, labels, attachments and templates. Keep the panel focused on setup and diagnostics.
- [ ] **Step 2:** Run `go test ./...`, build the binary and inspect tool schemas with an MCP client. Exercise a disposable account with at least a web/API inbox and a WhatsApp inbox where available. Check a complete flow: locate → read → note → assign → label → reply → resolve.
- [ ] **Step 3:** Check result IDs and statuses, no leaked token, no duplicate sends and correct channel restrictions. Document any capability that depends on a specific Chatwoot version or inbox provider.
- [ ] **Step 4:** Review and integrate the four worker commits, resolve conflicts in coordinator-owned files and commit the complete-service release candidate.

## Parallel OpenCode Dispatch

Create four worktrees from the accepted Task 0 commit. Through the OpenCode MCP bridge, start an OpenCode server in each worktree and call `opencode_start_task` with model `opencode-go/deepseek-v4.1-flash`, the relevant task text and a file ownership boundary. Wait on the four task IDs and fetch each result. Dispatch Tasks 1–4 concurrently only after Task 0 compiles. Each agent reports changed files, focused check output and commit hash. The coordinator reviews API semantics against the official Chatwoot docs and integrates one commit at a time, running the combined checks after each merge. Task 5 is sequential because it touches shared panel documentation and verifies cross-feature behavior.

The MCP bridge must be registered before starting the Codex session. Direct `opencode run` calls from this Codex sandbox cannot reach the provider; the user's terminal successfully called `opencode-go/deepseek-v4.1-flash`. Confirm the MCP tools are present and run a trivial delegated task before dispatching implementation.

## Later Optional Tracks

**Historical search and monitoring:** First measure what the Chatwoot version and documented API already provide. If inadequate, design a separate opt-in index with a declared `history_since`, retention, deletion and coverage semantics. Event ingestion via webhooks belongs in a reachable service; local polling is possible for bounded use. This track needs its own spec and plan because it introduces persistent message data.

**Remote MCP:** Add Streamable HTTP as another adapter to the same service, with TLS, client authentication, token rotation, audit logging and deployment guidance. This track needs its own spec and plan because it changes the trust boundary. The local panel and `stdio` continue to work.
