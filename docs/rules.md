# Rule catalog and management

The rules page lists 26 built-in rules. Each one can be enabled, disabled, and edited for name, priority, decision and match method; custom rules can also be deleted. After a switch is saved successfully it affects later calls, and existing review records keep the rule snapshot from that time.

You can search by name, ID, command, expression and detection description, and filter by enabled or disabled. Semantic-matching entries show match samples and can be expanded to inspect the checks, the scope and the limits; here "semantic matching" means a deterministic code check in the backend and never calls a model. Regex and text entries show the full expression and can be expanded and copied. **Rule dry run** only analyzes arguments and does not execute commands.

## Built-in entries

| Category | Rule | Default state / priority |
|---|---|---|
| System | R_SYS_RM: Recursive delete rm -r / rm -rf | Enabled / 100 |
| System | R_SYS_PATH: Delete critical system directories | Enabled / 100 |
| System | R_SYS_FORMAT: Disk format mkfs / Format-Volume (also diskutil, Clear-Disk) | Enabled / 100 |
| System | R_SYS_DD: Overwrite a disk device with dd / redirection | Enabled / 100 |
| System | R_SYS_FORK: Fork bomb (recursive background pipe) | Enabled / 100 |
| System | R_SYS_SHUTDOWN: Shutdown / reboot (also init 0 or 6) | Enabled / 100 |
| System | R_SYS_KILL: Mass process termination | Enabled / 100 |
| System | R_SYS_WIPE: Disk wipe shred / wipe (also wipefs) | Enabled / 100 |
| System | R_SYS_FIREWALL: Flush firewall rules | Enabled / 100 |
| Database | R_DB_DROP: SQL DROP database / table / schema (DROP DATABASE / TABLE / SCHEMA / INDEX / VIEW / TABLESPACE / USER / ROLE) | Enabled / 90 |
| Database | R_DB_TRUNCATE: SQL TRUNCATE table | Enabled / 90 |
| Database | R_DB_DELETE: SQL unconditional DELETE (DELETE FROM without WHERE) | Enabled / 90 |
| Database | R_DB_MONGO: MongoDB drop / dropDatabase (also dropCollection) | Enabled / 90 |
| Database | R_DB_REDIS: Redis FLUSHALL / FLUSHDB | Enabled / 90 |
| HTTP | R_HTTP_DELETE: HTTP DELETE · curl / wget / request tools (also PowerShell) | Enabled / 80 |
| HTTP | R_HTTP_PYTHON: Python HTTP client DELETE (.delete()) | Enabled / 80 |
| HTTP | R_HTTP_SCRIPT: JavaScript / generic HTTP DELETE method (axios.delete / DELETE method declaration in a request) | Enabled / 80 |
| HTTP | R_HTTP_CLEAR: Bulk clear / destroy endpoints (write requests to clear, reset or destroy paths) | Enabled / 80 |
| Network | R_NET_PIPE: Data exfiltration pipe (optional) (data pipe between network clients) | **Disabled** / 80 |
| Baseline | R1-R6: password and login, configuration, account permissions, critical data, service availability, explicit flooding | Enabled / 0 |
| Read | A7: Read and query tools | Enabled / 0 |

Reject takes priority over allow; within the same decision, a higher priority matches first. The baseline categories and the specific entries are independent rules and may overlap. Turning one off does not turn off the others; to adjust a class of behavior, first search for the related command and check all enabled entries. A call that matches no deterministic rule still goes to the current human or model review mode.

## Order of rules and approval

Every tool call that is connected and actually triggers a Hook shares the server-side `Engine.Submit`:

```text
Validate session, call ID and original argument digest
  -> Project authorization scope check: out of scope is rejected directly
  -> Enabled rules: reject first; within the same decision, priority descending, ID ascending
      -> Reject matched: record the rule decision and block directly
      -> Allow matched: record the rule decision and return the execution permit directly
      -> No match: go to the review mode saved with this call
          -> Human: wait for allow / reject; reject on timeout
          -> Model: send a review request; reject on error or timeout; no handoff to a human
```

Rules use the actual tool name and arguments before redaction; storage, model input and the returned message are still redacted by the existing flow. A rule match does not enter the human queue, does not call a model and produces no model usage. Unknown scripts, unrecognized tools or calls that do not satisfy the deterministic conditions continue into the selected approval mode, and a non-match is never approved directly. Receiving an execution permit does not mean the tool has been executed.

This order was already implemented at the submission entry. This change makes the rule-match branch an explicit early return and saves `reviewPath` (`scope` / `rule` / `human` / `model`). The call record shows the actual path; `mode` still means the configured handling for a non-match at that time, and does not mean a model or a human was actually involved. For historical records, the path is inferred from the rule snapshot, the model decision and the human marker of that time, and current rules are not used to overwrite history. IDs such as R1 and A7 in a model reply belong to the model's decision explanation, and an ID alone cannot establish that a deterministic rule matched.

After a call enters model review, the model input contains only this call's tool name, tool arguments and `hitlMode`, and carries no historical commands, results or user session. Multiple operations in one call are still checked as a whole; the historical verification state across calls and cumulative retrieval are no longer used for decisions. This input boundary applies to all Agents and both model protocols, and does not change the rule-first order above.

### Check against ARTEX approval code

This approval flow was checked against the local ARTEX commit `a3bb2c79592c4d79a41479da1f5d52657bfad708`, recorded separately from the commit used for the first rule catalog comparison below:

- `applyIntercept` in `guard/guard.go`: first checks whether interception is enabled for the tool, then calls `Match`; only when nothing matches does it call `Judge`. `allow` / `deny` return immediately, and `ask` waits for a human through `HandleAsk`.
- `ListInterceptRules` in `db/intercept.go`, and `loadLocked`, `Match` and `ruleMatches` in `intercept/intercept.go`: enabled rules are sorted by `priority DESC, id`, and the first match decides the result; the match target is the tool name or the full input JSON, using a Go regex or case-sensitive contains-text. Existing records with an invalid regex are skipped, and the edit endpoint validates new regexes.
- `Judge` in `intercept/intercept.go`: when the model is not enabled or not wired up it returns "not judged" and the upper layer approves directly; the model failure behavior is configurable and the code default is `allow`; the model can return `ask` and then hand off to a human. AegisHook keeps its existing behavior of sending a non-match to the selected mode and rejecting on model errors, and does not introduce these automatic-allow or human-handoff options.

ARTEX's fixed tool list is `Bash`, `WebFetch`, `web_search`, `shell_open`, `shell_send`, `Write`, `Edit`, `MultiEdit`, and anything outside the list skips interception. AegisHook checks rules first for every tool event it receives, so it does not copy this list, and it does not switch to ARTEX's ordering in which a high-priority allow can come before a reject.

### How it applies to each Agent

| Integration | Pre-execution entry | Content passed to the matcher |
|---|---|---|
| Pi | `tool_call` in `adapter/index.ts` | `event.toolName` and `event.input` submitted as-is, for example `bash.command`, `read.path` |
| Claude Code / Codex / Grok Build | `PreToolUse` in `internal/bridge/bridge.go` | `tool_name` / `toolName` and `tool_input` / `toolInput`, for example `Bash.command`, `Read.file_path` |
| OpenCode | `tool.execute.before` in `adapter/opencode.mjs`, through the same bridge | `input.tool` and `output.args`, for example `bash.command`, `edit.filePath` |

All adapters wait for the final allow / reject from `/api/v1/reviews`, so the relationship between rules and approval can be reused. The table lists the argument shapes covered by the current adapters and existing verification; it does not guarantee that every client version provides the same events, and tools the client does not report, calls made inside external scripts, and later Codex `write_stdin` input cannot gain coverage from this server-side flow.

Built-in semantic matchers check the supported tool aliases and fields, for example read-tool case variants, `command` / `cmd`, and `path` / `file_path` / `filePath`. The **Tool name** restriction of a custom rule is still case-sensitive: `Bash` is not equal to `bash`. For a cross-Agent tool name regex you can use `(?i)^bash$`; the argument path must correspond to a real field, and leaving it empty matches against the whole JSON-encoded argument object, in which quotes, newlines and similar characters in strings are escaped. For a rule dry run, copy the actual tool name and argument format from the call record; redacted fields cannot be used to reproduce a rule that depends on the original secret value.

For example, `Read` reading an ordinary file can match A7, but `cat` or `pwd` inside `Bash` is not the read tool itself; if no other allow rule matches, the call still goes to a human or the model. That is a difference in match conditions, not a skipped rule stage.

ARTEX's full-JSON regex can match command-like text that is written into source code or explanatory text; AegisHook's built-in command rules analyze the execution context to reduce such false positives. Dynamic behavior that cannot be recognized statically is left to approval; this change did not turn plain text matching into a script interpreter.

`TestRuleFirstAcrossAgentsAndModes` uses five Agent identities and two modes to verify direct rule allow/reject, tool name regex, full JSON and field matching, redaction order, rule disabling, scope precedence, repeated calls and model usage; model responses are simulated in memory, with no dangerous commands executed, no real model used and no port listened on. `TestLegacyReviewPathUsesSavedEvidence` verifies that a historical record does not mistake the model's R1 for rule R1.

## Comparison with ARTEX

This time the local ARTEX commit `8dae851b9b622f2ff2631f332fde9719d0b16fba` was compared item by item:

- [`db/db.go`](https://github.com/Autumn-27/ARTEX/blob/8dae851b9b622f2ff2631f332fde9719d0b16fba/db/db.go): the 19 rule entries in the v1 / v2 initialization and their default states.
- [`db/intercept.go`](https://github.com/Autumn-27/ARTEX/blob/8dae851b9b622f2ff2631f332fde9719d0b16fba/db/intercept.go): rule fields, priority ordering, editing and enabling/disabling.
- [`intercept/intercept.go`](https://github.com/Autumn-27/ARTEX/blob/8dae851b9b622f2ff2631f332fde9719d0b16fba/intercept/intercept.go): tool name / input matching, enabled filtering and first-match behavior.
- `web/src/app/(main)/system/intercept/page.tsx`: the rules table, match content and sliding switches.

AegisHook follows its coverage categories and interaction, and implements the matchers independently. ARTEX's summary entry for "destructive system commands" is covered by the existing baseline rules and the specific system entries, with an unconditional SQL DELETE added. A network pipe is also used for normal transfers, so it stays disabled by default.

The new shell rules use a syntax tree to tell commands, arguments, pipes and output redirection apart; the SQL / MongoDB / Python / JavaScript checks are limited to the matching execution context. Merely printing a dangerous command or writing it as an ordinary source example is not the same as executing that operation. AegisHook keeps its reject-first rules and does not change the reject handling on model errors.

Detection is a limited static check, not a complete script interpreter: it does not expand variables, external scripts or arbitrary dynamic calls; Python session / client rely on naming conventions, and HTTP methods and calls are associated by code snippet. The full conditions of each rule list its specific limits. Windows commands are covered only in the literal forms that can currently be parsed; the current tests analyze these inputs on macOS and do not execute Windows system commands.

## Upgrading from older versions

At startup only missing built-in entries are added; existing names, priorities, match expressions, decisions and switches are not overwritten. New entries, the config version and the upgrade audit are written in the same transaction, and starting repeatedly does not reset rules.

If the related old baseline category has been disabled, changed to allow, given a different match method or restricted to certain tools, the new entry stays off when it is first added. Changes to name, description and priority do not affect the default enabled state. After that, each switch works independently and no longer follows the baseline category. Custom rules and historical review snapshots are kept.
