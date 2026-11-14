# Session status and subagent Hook validation

Validation date: 2026-09-09. The console ran on `127.0.0.1:18790` with a separate `.data/test`, and project Hooks were installed, integration status checked and approvals handled through the built-in browser. The test projects held only harmless marker files. The model connections of Codex and Grok used the local 7897 proxy, and Hook callbacks connected directly to the loopback address.

## Status fixes

Installation config, historical integration verification and current session status are shown separately. A session ending, a heartbeat expiring, a service restart, or a re-registration that loses the current installation binding must never erase the historical evidence that events were received. A broken or uninstalled entry is still shown independently, and a successful historical verification does not make it appear as a healthy installation.

Continuously online applies only to connections that have a heartbeat; an event-based connection shows "event access", and is additionally marked "recently active" when an event or a review heartbeat was received within 30 seconds. The list puts continuously online or recently active sessions first.

## Native client test results

| Client | Verified facts | Limitations |
| --- | --- | --- |
| Claude Code 2.1.263 | The parent session called `Agent`, and the subagent used `Read` to read the marker; both calls entered review and were recorded as succeeded. After the session ended, the installation scope still showed "Events received". | Child calls appear inside the parent session; there is no subagent identifier yet. |
| Codex CLI 0.153.4 | The subagent's `cat marker.txt` entered the `Bash` pre-execution review, and after approval the subtask returned the correct marker; the subtask coordination call also entered review. | In this run some post-execution results had no recognized explicit execution status and stayed as awaiting result; a permit must not be equated with execution success. |
| Grok Build 1.0.24 | Native `spawn_subagent` created a separate child session; that child session's `read_file` was reviewed, and after approval both the parent and child calls were recorded as succeeded and returned the correct marker. Native subtask metadata confirmed the parent and child IDs match. | The console does not yet show the parent-child link. |
| OpenCode 1.17.9 | Native `task` created a `general` subagent; the parent task and the child session's `read` both entered review and were recorded as succeeded, and the subtask returned the correct marker. Native task metadata confirmed the parent and child IDs match; the integration verification history is kept after the session ended and after uninstalling. | The console does not yet show the parent-child link. |
| Pi 0.85.1 | The installation and historical verification lifecycle regression passed. | The native child-process tool test was not run in this round; child processes must load the extension on their own. |

Codex CLI was upgraded from 0.142.0 to the official npm 0.153.4. The runtime bundled in the desktop app is also 0.153.4, but calling its binary with `exec` is still a command-line test and is not the same as testing inside a desktop task. The version the page detects comes from the CLI on `PATH`, and receiving events for the same installation scope also cannot prove that both the CLI and the desktop app have been verified.

Codex's project config layer must actually be trusted, and Hook definition trust is a separate check. In this run an early command-line trust argument did not make the project layer take effect; the reason it was disabled was confirmed through the local config interface, and events were received only after adding exact trust for the temporary project. These early runs do not count toward the Hook coverage pass results.

Grok's first non-interactive call was canceled by the client permission flow; another workflow attempt did not complete a native subtask and also does not count as a pass. The run that finally passed used the explicit native `spawn_subagent` plus the child session `read_file` path, keeping AegisHook human approval. The existing history still contains one unexplained difference in tool counts, so this success cannot be used to claim that all historical calls were fully covered.

OpenCode's existing model connection returned API 404 errors, an invalid credential or no response. With the user's explicit authorization, this run temporarily reused Claude's model service connection through the process environment, using the Anthropic SDK with the actual model ID `glm-5.3` and removing Claude's special `[1M]` context marker; the client's persisted connection config was not modified, and no credentials were written to the test records. The nested test directory must become a separate project and be given the correct working directory, otherwise the client may fall back to the outer repository and fail to load the temporary project plugin. The run that finally passed was the native parent task and child session calls in a separate project; early runs where the plugin did not load do not count toward coverage results.

## Regression scope

New tests with no listening port cover the historical integration verification of the five Agents, including old records, session end, lost contact, service restart, re-registration with no current binding, a broken entry, uninstallation, cross-Agent evidence isolation, and ignoring historical fields forged by the caller. The related installation and session regressions passed, and the production build passed.

Browser verification covers the separate installation status and integration verification columns, ended and restarted states, the color and ordering of recently active, expired activity, retention of historical evidence, and desktop and phone widths. Test data is kept separate from real console data, and the temporary Hooks and the added project trust were cleaned up after the tests ended.
