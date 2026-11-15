# Acceptance notes

Isolated testing used macOS, a local Pi 0.85.1, Go 1.23.5 (at the time; the project now requires Go 1.27.4+), Node 23.10 and Chromium.

## Automated coverage

- Command line: default `127.0.0.1:18790`, `--host/--port`, the compatible `--addr`, `localhost` and IPv4/IPv6 loopback addresses; rejection of non-loopback addresses, invalid ports, option conflicts and unknown options; `-h/--help` shows usage, commands, options and examples.
- Go: R1-R6 blocking, reject precedence, built-in rules that cannot be removed, argument binding, concurrent duplicate approvals, terminal execution states, timeouts, session deregistration/lost contact, restart, single-instance lock, installation ownership and dual entries, strict model JSON, model config snapshot, reject on error, redaction, scope constraints, audit-failure rollback, protocol request format, key rollback, Host/Origin and admin/Hook permission isolation, SPA deep links.
- Pi: real extension loader, symlink config location, Agent tool loop, rejection reason fed back into tool feedback, the Agent continuing with a read-only alternative, human pause, uninstall while running, reload and backend-offline blocking. The tool execution counter verifies that a rejected tool executed 0 times.
- Browser: login, installation, pending, approve, reject, argument expansion, filters, export, rule management/dry run, authorization scope, model connection test/mode switch, uninstall and session status, audit, timeline, theme persistence. No page runtime exceptions were found.
- DashScope: both real OpenAI and Anthropic addresses, model qwen-flash; the initial template returned a valid A7 approve decision; re-testing with the user's full original template returned a valid D1 approve decision on both protocols. Only synthetic read-only reviews were sent.

## Visual check

UI references are kept on the developer's machine. The test screenshots come from isolated test records and are not demo data of the normal console; screenshots are not included in the public repository. Run `AEGIS_SCREENSHOTS=/absolute/output npm run test:e2e` to regenerate them.

1. The light gray sidebar and top bar keep the same hierarchy. The purple shield-and-hook SVG mark is used on the loading page, login page, sidebar and browser tab; the transparent background has no checkerboard, and the mark stays intact when the sidebar is collapsed and on narrow screens.
2. The title, description and the refresh/export actions on the right are aligned; local system fonts are used and no external fonts are loaded.
3. Pending cards keep the orange border, the two-column layout and the approve/reject actions on the right; the details show the actual arguments.
4. Tables use thin dividers, tool tags, decision colors, expandable details and pagination; Approved and the execution result are shown separately.
5. Dark mode uses the same layout and CSS variables; button and text contrast was checked in screenshots; it is kept after the theme reloads.
6. Verified at the 1440, 1920 and 1672 reference widths and at a 390 narrow width. On narrow screens the navigation is compressed and tables can scroll horizontally, and the approval buttons stay reachable.

Intentional differences: the "demo data" button and the fake Claude session were removed; account creation/deletion is rejected directly, and the pending approval uses a script not covered by rules; the unverifiable "Agent has adjusted the plan" status is not shown. Full paths and structured arguments make some records taller than in the mockups.

The Linux Pi lifecycle is now covered by the real loader and the isolated tool loop in GitHub CI. The tests did not perform any real account change, password change or data deletion; the Hook acceptance on a real Windows machine is still pending.

## Verification of the local address, help and icon changes (2026-09-09)

This local verification was completed on macOS, with the results below; these results do not mean this change has been accepted on a real Windows machine or in a new GitHub Actions run.

| Check | Result |
| --- | --- |
| `go test -race ./...` | Passed, including argument parsing, loopback address validation, and Host/Origin and Hook tests |
| `npm --prefix web run build`, `npm run adapter:check` | Passed; frontend and Pi adapter type checks succeeded |
| `npm run test:adapter`, `npm run test:opencode` | Passed; the Pi loader/tool loop and the OpenCode callback can both connect to the local service |
| Command-line startup smoke check | `127.0.0.1`, `localhost` and `::1` each completed startup, a `status` query and Claude Hook event registration on a temporary custom port; the address in the connection config matched the startup arguments |
| Help and invalid-argument smoke check | Help for the main command and for `serve/install/uninstall/status/hook` exits normally; help and invalid arguments do not create a data directory |
| SVG asset | The service returns `image/svg+xml`, and the page and the browser tab use the same asset |
| `npm run test:e2e` | 2 flows passed, covering login, Agent integration and console operations; the new icon was checked on desktop, in light and dark themes, and at a 390 px narrow width |

Browser verification used the project's existing Playwright and the locally installed Chromium. The test service and client config use temporary directories; screenshots stay in a local temporary directory and are not committed to the repository. For how to reproduce and how to configure an existing Chromium path, see the [project development and verification notes](../../README.md#development-and-verification).

## Pre-release verification

GitHub CI runs the Go race detector, the Pi real loader/tool loop tests and the OpenCode callback tests on Ubuntu and macOS; Ubuntu additionally runs the browser end-to-end flow. The cloud model test is skipped by default and uses no real key. Release cross-compilation covers macOS AMD64/ARM64, Linux AMD64/ARM64 and Windows AMD64; a successful cross-compilation is not equivalent to Hook acceptance on a real Windows machine.

2026-09-09: GitHub Actions completed the Go race and adapter tests on Linux / macOS, and the browser end-to-end tests in Linux Chromium. The dashboard gained review trends, a decision donut chart, Token / cost statistics, hover data and keyboard date navigation, covering 1440 / 1920 and 390 px viewports.
