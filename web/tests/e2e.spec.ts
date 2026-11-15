import { test, expect, type Page } from "@playwright/test";
import { mkdtemp, mkdir, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawn, type ChildProcess } from "node:child_process";
import { once } from "node:events";
import { createServer, type Server } from "node:http";
const exe = process.platform === "win32" ? ".exe" : "";
async function choose(page: Page, name: string, option: string) {
  const trigger = page.getByRole("combobox", { name, exact: true });
  await trigger.click();
  const menu = page.getByRole("listbox", { name, exact: true });
  await expect(menu).toBeVisible();
  const box = await menu.boundingBox();
  const view = page.viewportSize()!;
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.y).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(view.width + 1);
  expect(box!.y + box!.height).toBeLessThanOrEqual(view.height + 1);
  await menu.getByRole("option", { name: option, exact: true }).click();
  await expect(menu).toHaveCount(0);
  await expect(trigger).toContainText(option);
}
let dir: string, proc: ChildProcess, modelServer: Server;
const base = "http://127.0.0.1:18794";
test.use({ deviceScaleFactor: 2 });
// Each test owns its service and data; the offline test stops its own process.
test.beforeEach(async () => {
  dir = await mkdtemp(join(tmpdir(), "aegis-browser-"));
  await mkdir(join(dir, "data"));
  await mkdir(join(dir, "project"));
  await writeFile(join(dir, "data", "admin.token"), "browser-test-admin");
  await writeFile(join(dir, "data", "hook.token"), "browser-test-hook");
  proc = spawn(
    fileURLToPath(new URL("../../bin/aegishook" + exe, import.meta.url)),
    [
      "serve",
      "--data-dir",
      join(dir, "data"),
      "--agent-dir",
      join(dir, "pi"),
      "--addr",
      "127.0.0.1:18794",
    ],
    { stdio: "pipe" },
  );
  for (let n = 0; n < 100; n++) {
    try {
      if ((await fetch(base)).ok) break;
    } catch {}
    await new Promise((r) => setTimeout(r, 30));
  }
  modelServer = createServer((req, res) => {
    let body = "";
    req.on("data", (chunk) => {
      body += chunk;
    });
    req.on("end", () => {
      const ask = body.includes("uncertain-fixture-file");
      res.setHeader("Content-Type", "application/json");
      res.end(
        JSON.stringify({
          usage: {
            prompt_tokens: 1000,
            completion_tokens: 50,
            prompt_tokens_details: { cached_tokens: 200 },
          },
          choices: [
            {
              message: {
                content: JSON.stringify({
                  decision: ask ? "ask" : "approve",
                  comment: ask
                    ? "Actual operation: delete a file of unknown origin; Consequence if successful: data may be lost, and it must be confirmed whether this is a test artifact; Matched rule: H1"
                    : "Actual operation: read a document; Consequence if successful: obtain the document contents; Matched rule: A7",
                }),
              },
            },
          ],
        }),
      );
    });
  });
  modelServer.listen(18795, "127.0.0.1");
  await once(modelServer, "listening");
});
test.afterEach(async () => {
  if (proc?.exitCode === null) {
    proc.kill("SIGTERM");
    await once(proc, "exit");
  }
  if (modelServer?.listening) {
    modelServer.closeAllConnections();
    await new Promise<void>((resolve, reject) => {
      modelServer.close((error) => (error ? reject(error) : resolve()));
    });
  }
  await rm(dir, { recursive: true, force: true });
});
test("full local console workflow and visual states", async ({
  page,
  request,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto("/approvals");
  await expect(page).toHaveTitle(/AegisHook/);
  await page.getByLabel("Admin token", { exact: true }).fill("browser-test-admin");
  await page.getByRole("button", { name: "Open console" }).click();
  await expect(
    page.getByRole("heading", { name: "Approvals & blocks", exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Agent integration", exact: true }).click();
  await page
    .getByRole("button", { name: "Install hook", exact: true })
    .first()
    .click();
  await choose(page, "Install scope", "Specific project");
  await page.getByLabel("Absolute project path").fill(join(dir, "project"));
  await page
    .locator(".modal")
    .getByRole("button", { name: "Install hook", exact: true })
    .click();
  await expect(page.locator("table").first()).toContainText("Configured");
  await expect(page.locator("table").first()).toContainText("No events yet");
  const hook = { Authorization: "Bearer browser-test-hook" };
  async function call(tool: string, args: any, id: string) {
    const r = await request.post(base + "/api/v1/reviews", {
      headers: hook,
      data: {
        instanceId: "browser-fixture",
        callId: id,
        toolName: tool,
        argumentsObj: args,
        userMessage: "Isolated UI test; verifies review only and runs no system operations.",
      },
    });
    expect(r.ok()).toBeTruthy();
    return r.json();
  }
  const registration = await request.post(base + "/api/v1/instances", {
    headers: hook,
    data: {
      id: "browser-fixture",
      sessionId: "browser-session",
      cwd: join(dir, "project"),
      piVersion: "0.85.1",
      hookVersion: "0.1.0",
    },
  });
  expect(registration.ok()).toBeTruthy();
  const beat = setInterval(() => {
    request
      .post(base + "/api/v1/instances/browser-fixture/heartbeat", {
        headers: hook,
        data: { state: "idle" },
      })
      .catch(() => {});
  }, 10000);
  try {
    const denied = await call(
      "delete_user",
      { user: "isolated-test-user" },
      "denied",
    );
    expect(denied.decision).toBe("reject");
    await call(
      "bash",
      { command: "passwd isolated-fixture" },
      "password-denied",
    );
    const allowed = await call("read", { path: "README.md" }, "read");
    expect(allowed.decision).toBe("approve");
    await request.post(base + `/api/v1/reviews/${allowed.id}/result`, {
      headers: hook,
      data: {
        instanceId: "browser-fixture",
        state: "succeeded",
        result: "fixture output",
      },
    });
    await call(
      "bash",
      { command: "unknown-fixture-script --dry-run" },
      "manual",
    );
    await page.getByRole("link", { name: /^Approvals & blocks/ }).click();
    await expect(page.getByRole("button", { name: "Allow this call" })).toBeVisible();
    const output =
      process.env.AEGIS_SCREENSHOTS || join(tmpdir(), "aegishook-restyle");
    await mkdir(output, { recursive: true });
    await expect(page.locator(".toast")).toHaveCount(0);
    await page.setViewportSize({ width: 1664, height: 785 });
    await page.screenshot({
      path: join(output, "approvals-reference-native.png"),
      animations: "disabled",
    });
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page
      .locator("tr")
      .filter({ hasText: "Change accounts or permissions" })
      .getByRole("button", { name: "View details" })
      .click();
    await expect(page.locator(".call-detail")).toContainText("Not executed");
    await expect(page.locator(".call-detail")).toContainText("Decided directly by rule");
    await expect(page.locator(".call-detail")).not.toContainText(
      "No rule matched → human approval",
    );
    await expect(page.locator(".toast")).toHaveCount(0);
    await page.screenshot({
      path: join(output, "light-1440.png"),
      fullPage: true,
      animations: "disabled",
    });
    await page.setViewportSize({ width: 1920, height: 1080 });
    await page.screenshot({
      path: join(output, "light-1920.png"),
      fullPage: true,
      animations: "disabled",
    });
    await page.setViewportSize({ width: 1672, height: 941 });
    await page.screenshot({
      path: join(output, "light-reference-size.png"),
      fullPage: true,
      animations: "disabled",
    });
    await page.getByRole("button", { name: "Switch to dark theme" }).click();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await expect(page.locator(".brand")).toHaveCSS(
      "color",
      "rgb(237, 237, 241)",
    );
    await page.screenshot({
      path: join(output, "dark-reference-size.png"),
      fullPage: true,
      animations: "disabled",
    });
    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await page.getByRole("button", { name: "Switch to light theme" }).click();
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.getByRole("button", { name: "Allow this call" })).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: join(output, "mobile-390.png"),
      fullPage: true,
      animations: "disabled",
    });
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.getByRole("button", { name: "Allow this call" }).click();
    await expect(page.getByRole("button", { name: "Allow this call" })).toHaveCount(0);
    await call("bash", { command: "second-unknown-script" }, "manual-reject");
    await expect(
      page.getByRole("button", { name: "Reject", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Reject", exact: true }).click();
    await page.getByLabel("Rejection reason").fill("Tests only allow read-only checks; please adjust the plan.");
    await page.getByRole("button", { name: "Confirm rejection" }).click();
    await expect(page.locator(".modal")).toHaveCount(0);
    await choose(page, "Filter by decision", "Rejected");
    await choose(page, "Filter by decision", "All records");
    await page.getByLabel("Search operations or rules").fill("manual-reject");
    await expect(page.locator(".records tbody>tr")).toHaveCount(1);
    await page.getByLabel("Search operations or rules").fill("");
    const download = page.waitForEvent("download");
    await page.getByRole("link", { name: "Export records" }).click();
    expect((await download).suggestedFilename()).toBe("aegishook-calls.json");
    await page.getByRole("link", { name: "Policy rules", exact: true }).click();
    await expect(page.locator("[data-rule-id]")).toHaveCount(26);
    await expect(
      page.getByRole("combobox", { name: "Policy rules rows per page" }),
    ).toContainText("50 / page");
    await page.getByRole("textbox", { name: "Search rules" }).fill("redis");
    await expect(page.locator("[data-rule-id]")).toHaveCount(1);
    await expect(page.locator('[data-rule-id="R_DB_REDIS"]')).toContainText(
      "FLUSHDB",
    );
    await page.getByRole("textbox", { name: "Search rules" }).fill("");
    await choose(page, "Rule status", "Disabled");
    await expect(page.locator("[data-rule-id]")).toHaveCount(1);
    await expect(
      page.locator('[data-rule-id="R_NET_PIPE"]').getByRole("switch"),
    ).toHaveAttribute("aria-checked", "false");
    await choose(page, "Rule status", "All statuses");
    await page.getByRole("button", { name: "New rule" }).click();
    await page.getByLabel("Name", { exact: true }).fill("Test read-only probe");
    await page.locator(".modal").getByLabel("Tool name", { exact: true }).fill("bash");
    await page.getByLabel("Argument path", { exact: true }).fill("command");
    await page.getByLabel("Match expression (Go regex)").fill("^whoami$");
    await choose(page, "Decision", "Allow");
    await page.getByRole("button", { name: "Save rule" }).click();
    await expect(page.getByText("Test read-only probe", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Run dry run" }).click();
    await expect(page.locator(".form-panel .notice")).toContainText("approve");
    // Built-in edits must affect the actual reviewer, not only the table.
    const builtinRow = () => page.locator('[data-rule-id="R1"]');
    await builtinRow()
      .getByRole("button", { name: /^View full condition of .*$/ })
      .click();
    await expect(page.locator(".rule-semantics")).toContainText(
      "passwd, chpasswd",
    );
    await expect(page.locator(".rule-semantics")).toContainText(
      "it does not judge whether the account is a test account",
    );
    await page.screenshot({
      path: join(output, "rule-semantics-expanded.png"),
      fullPage: true,
    });
    await builtinRow()
      .getByRole("button", { name: /^Hide full condition of .*$/ })
      .click();
    await builtinRow()
      .getByRole("button", { name: /^Edit / })
      .click();
    await expect(
      page.getByRole("heading", { name: "Edit built-in rule" }),
    ).toBeVisible();
    await expect(page.getByLabel("Rule ID", { exact: true })).toHaveAttribute(
      "readonly",
      "",
    );
    await expect(page.locator(".modal .rule-semantics")).toContainText(
      "passwd, chpasswd",
    );
    await page.getByRole("combobox", { name: "Match type", exact: true }).click();
    await expect(page.getByRole("listbox")).toBeVisible();
    await page.screenshot({
      path: join(output, "dropdown-dialog-light.png"),
      fullPage: false,
      animations: "disabled",
    });
    await page
      .getByRole("combobox", { name: "Match type", exact: true })
      .press("Escape");
    await expect(page.getByRole("listbox")).toHaveCount(0);
    await page.getByLabel("Name", { exact: true }).fill("Password operation test rule");
    await choose(page, "Decision", "Allow");
    await page.getByLabel("Priority", { exact: true }).fill("25");
    await page.getByLabel("Extra decision note").fill("Isolated environment test permission");
    await page.screenshot({
      path: join(output, "builtin-edit-desktop.png"),
      fullPage: false,
      animations: "disabled",
    });
    await page.setViewportSize({ width: 390, height: 844 });
    await page
      .getByRole("button", { name: "Save rule", exact: true })
      .scrollIntoViewIfNeeded();
    await expect(
      page.getByRole("button", { name: "Save rule", exact: true }),
    ).toBeInViewport();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: join(output, "builtin-edit-mobile.png"),
      fullPage: false,
      animations: "disabled",
    });
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.getByRole("button", { name: "Save rule", exact: true }).click();
    await expect(page.locator(".modal")).toHaveCount(0);
    await page.reload();
    await expect(builtinRow()).toContainText("Password operation test rule");
    await expect(builtinRow().locator(".badge")).toHaveText("Allow");
    await page
      .getByLabel("Arguments JSON")
      .fill(JSON.stringify({ command: "passwd isolated-fixture" }));
    await page.getByRole("button", { name: "Run dry run" }).click();
    await expect(page.locator(".form-panel .notice")).toContainText("approve");
    await expect(page.locator(".form-panel .notice")).toContainText(
      "Isolated environment test permission",
    );
    await builtinRow().getByRole("switch").click();
    await expect(builtinRow().getByRole("switch")).toHaveAttribute(
      "aria-checked",
      "false",
    );
    await page.getByRole("button", { name: "Run dry run" }).click();
    await expect(page.locator(".form-panel .notice")).toContainText("pending");
    await builtinRow()
      .getByRole("button", { name: /^Edit / })
      .click();
    await page.getByRole("button", { name: "Restore default", exact: true }).click();
    await expect(page.getByLabel("Name", { exact: true })).toHaveValue(
      "Block password and login state changes",
    );
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(builtinRow()).toContainText("Password operation test rule");
    await builtinRow()
      .getByRole("button", { name: /^Edit / })
      .click();
    await choose(page, "Match type", "Contains text");
    await choose(page, "Match target", "Tool name");
    await page.getByLabel("Match text (case-sensitive)").fill("fixture");
    await choose(page, "Decision", "Reject");
    await expect(
      page.getByRole("switch", { name: "Enable this rule", exact: true }),
    ).toHaveAttribute("aria-checked", "false");
    await page
      .getByRole("switch", { name: "Enable this rule", exact: true })
      .click();
    await page.getByRole("button", { name: "Save rule", exact: true }).click();
    await expect(page.locator(".modal")).toHaveCount(0);
    await page.getByLabel("Tool name", { exact: true }).fill("fixture_tool");
    await page.getByRole("button", { name: "Run dry run" }).click();
    await expect(page.locator(".form-panel .notice")).toContainText("reject");
    await builtinRow()
      .getByRole("button", { name: /^Edit / })
      .click();
    await page.getByRole("button", { name: "Restore default", exact: true }).click();
    await expect(
      page.getByRole("combobox", { name: "Match type", exact: true }),
    ).toContainText("Built-in semantic matching");
    await page.getByRole("button", { name: "Save rule", exact: true }).click();
    await expect(page.locator(".modal")).toHaveCount(0);
    await page.getByLabel("Tool name", { exact: true }).fill("bash");
    await page.getByRole("button", { name: "Run dry run" }).click();
    await expect(page.locator(".form-panel .notice")).toContainText("reject");
    await expect(page.locator(".form-panel .notice")).toContainText("R1");
    await page.screenshot({
      path: join(output, "builtin-rules-restored.png"),
      fullPage: true,
    });
    await page.getByRole("link", { name: "Authorization scope", exact: true }).click();
    await page
      .getByRole("button", { name: "Add scope", exact: true })
      .first()
      .click();
    await page.getByLabel("Name", { exact: true }).fill("Isolated test project");
    await page.getByLabel("Absolute project path").fill(join(dir, "project"));
    await page
      .getByLabel("Allowed targets (one exact hostname or IP per line)")
      .fill("staging.example.test");
    await page.getByRole("button", { name: "Save scope" }).click();
    await expect(page.getByText("Isolated test project", { exact: true })).toBeVisible();
    await page.getByRole("link", { name: "System settings", exact: true }).click();
    await choose(page, "API protocol", "Anthropic (Messages)");
    await choose(page, "API protocol", "OpenAI-compatible (Chat Completions)");
    const protocol = page.getByRole("combobox", {
      name: "API protocol",
      exact: true,
    });
    await protocol.press("ArrowDown");
    await protocol.press("End");
    await protocol.press("Enter");
    await expect(protocol).toContainText("Anthropic");
    await protocol.click();
    await page.getByRole("heading", { name: "Review model", exact: true }).click();
    await expect(page.getByRole("listbox")).toHaveCount(0);
    await choose(page, "API protocol", "OpenAI-compatible (Chat Completions)");
    await page.getByRole("button", { name: "Switch to dark theme" }).click();
    await protocol.click();
    await expect(page.getByRole("listbox")).toHaveCSS(
      "background-color",
      "rgb(17, 17, 18)",
    );
    await page.screenshot({
      path: join(output, "dropdown-settings-dark.png"),
      fullPage: false,
      animations: "disabled",
    });
    await protocol.press("Escape");
    await page.getByRole("button", { name: "Switch to light theme" }).click();
    await page.setViewportSize({ width: 390, height: 844 });
    await protocol.scrollIntoViewIfNeeded();
    await choose(page, "API protocol", "Anthropic (Messages)");
    await protocol.click();
    await page.screenshot({
      path: join(output, "dropdown-mobile.png"),
      fullPage: false,
      animations: "disabled",
    });
    await protocol.press("Escape");
    await choose(page, "API protocol", "OpenAI-compatible (Chat Completions)");
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.getByLabel("Base URL").fill("http://127.0.0.1:18795/v1");
    await page.getByLabel("Model name").fill("fixture-reviewer");
    await page
      .getByRole("switch", { name: "Enable cost estimate", exact: true })
      .check();
    await page.getByLabel("Input price", { exact: true }).fill("2");
    await page.getByLabel("Output price", { exact: true }).fill("4");
    await page.getByLabel("Cache read price", { exact: true }).fill("0.5");
    await page.getByRole("button", { name: "Save settings" }).click();
    await expect(
      page.getByRole("button", { name: "Test saved configuration" }),
    ).toBeEnabled();
    await page.getByRole("button", { name: "Test saved configuration" }).click();
    await expect(
      page
        .locator(".form-panel")
        .filter({ hasText: "Review model" })
        .getByText("Test passed", { exact: true }),
    ).toBeVisible();
    await page.locator('input[value="model"]').check();
    await page.getByRole("button", { name: "Save settings" }).click();
    await expect(page.locator('input[value="model"]')).toBeChecked();
    await call(
      "fixture_cleanup",
      { path: "uncertain-fixture-file" },
      "model-ask",
    );
    await page.getByRole("link", { name: "Approvals & blocks", exact: true }).click();
    await expect(page.locator(".pending-table")).toContainText(
      "No pending approvals",
    );
    await expect(
      page.getByRole("button", { name: "Allow this call", exact: true }),
    ).toHaveCount(0);
    await expect(page.locator(".records")).toContainText(
      "model decision is missing a valid conclusion or explanation",
    );
    await expect(page.locator(".records")).not.toContainText("Model handed to human");
    await page.screenshot({
      path: join(output, "invalid-model-ask-rejected.png"),
      fullPage: true,
      animations: "disabled",
    });
    await page.getByRole("link", { name: "System settings", exact: true }).click();
    await page.locator('input[value="human"]').check();
    await page.getByRole("button", { name: "Save settings" }).click();
    // Paginate real, isolated review records; none of these tools are executed.
    for (let n = 0; n < 11; n++) {
      await call(
        "fixture_pending",
        { note: `page-fixture-${n}` },
        `page-pending-${n}`,
      );
      await call("read", { path: `page-fixture-${n}.md` }, `page-read-${n}`);
    }
    await page.getByRole("link", { name: "Approvals & blocks", exact: true }).click();
    const pendingPager = page.getByRole("navigation", {
      name: "Pending pagination",
      exact: true,
    });
    await expect(pendingPager).toContainText("11 total");
    await expect(
      page.getByRole("button", { name: "Allow this call", exact: true }),
    ).toHaveCount(10);
    await pendingPager.getByRole("button", { name: "Next page" }).click();
    await expect(
      page.getByRole("button", { name: "Allow this call", exact: true }),
    ).toHaveCount(1);
    await page.getByRole("button", { name: "Allow this call", exact: true }).click();
    await expect(pendingPager).toContainText("1 / 1");
    await expect(
      page.getByRole("button", { name: "Allow this call", exact: true }),
    ).toHaveCount(10);
    await page.getByRole("link", { name: "Tool calls", exact: true }).click();
    const callPager = page.getByRole("navigation", {
      name: "Tool calls pagination",
      exact: true,
    });
    await expect(page.locator(".records tbody>tr")).toHaveCount(10);
    const firstID = await page
      .locator(".records .record-id")
      .first()
      .getAttribute("title");
    await callPager.getByRole("button", { name: "Next page" }).click();
    await expect(
      page.locator(".records .record-id").first(),
    ).not.toHaveAttribute("title", firstID!);
    await page.getByRole("button", { name: "Refresh", exact: true }).click();
    await expect(callPager.locator(".pagination-position")).toContainText(
      "2 /",
    );
    await page.getByLabel("Search operations or rules").fill("page-fixture-");
    await expect(callPager).toContainText("22 total");
    await expect(callPager).toContainText("1 / 3");
    await choose(page, "Tool calls rows per page", "20 / page");
    await expect(page.locator(".records tbody>tr")).toHaveCount(20);
    await callPager.getByRole("button", { name: "Next page" }).click();
    await expect(page.locator(".records tbody>tr")).toHaveCount(2);
    await page.getByLabel("Search operations or rules").fill("no-such-page-record");
    await expect(callPager).toContainText(/(?<!\d)0 total/);
    await expect(
      callPager.getByRole("button", { name: "Next page" }),
    ).toBeDisabled();
    await page.getByLabel("Search operations or rules").fill("");
    const allCalls = await (
      await page.request.get(base + "/api/v1/calls")
    ).json();
    const exported = await (
      await page.request.get(base + "/api/v1/calls/export")
    ).json();
    expect(exported.length).toBe(allCalls.length);
    await page.screenshot({
      path: join(output, "calls-pagination.png"),
      fullPage: true,
      animations: "disabled",
    });
    await page.getByRole("link", { name: "Agent integration", exact: true }).click();
    await expect(
      page.getByRole("navigation", { name: "Install scope pagination" }),
    ).toBeVisible();
    await expect(
      page.getByRole("navigation", { name: "Session status pagination" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Uninstall", exact: true }).click();
    await page.getByRole("button", { name: "Remove entry" }).click();
    await expect(page.locator("table").first()).toContainText("Uninstalled");
    await expect(page.locator("table").first()).toContainText(
      "Existing sessions still need to reload",
    );
    await expect(page.locator("table").first()).toContainText("Events received");
    await request.post(base + "/api/v1/instances/browser-fixture/shutdown", {
      headers: hook,
      data: {},
    });
    await page.getByRole("link", { name: "Audit log", exact: true }).click();
    await expect(page.locator("table")).toContainText("hook.uninstall");
    const auditPager = page.getByRole("navigation", { name: "Audit log pagination" });
    await expect(page.locator("tbody>tr")).toHaveCount(10);
    const auditFirst = await page.locator("tbody>tr").first().innerText();
    await auditPager.getByRole("button", { name: "Next page" }).click();
    await expect(page.locator("tbody>tr").first()).not.toHaveText(auditFirst);
    await choose(page, "Audit log rows per page", "20 / page");
    await expect(page.locator("tbody>tr")).toHaveCount(20);
    await expect(auditPager.locator(".pagination-position")).toContainText(
      "1 /",
    );
    await page.getByPlaceholder("Search action, object or details").fill("hook.uninstall");
    await expect(auditPager).toContainText(/(?<!\d)1 total/);
    await expect(
      auditPager.getByRole("button", { name: "Next page" }),
    ).toBeDisabled();
    await page.getByPlaceholder("Search action, object or details").fill("");
    await choose(page, "Audit log rows per page", "10 / page");
    await page.screenshot({
      path: join(output, "audit-pagination.png"),
      fullPage: true,
      animations: "disabled",
    });
    await page.getByRole("button", { name: "Switch to dark theme" }).click();
    await page.getByRole("combobox", { name: "Audit log rows per page" }).click();
    await page.screenshot({
      path: join(output, "audit-pagination-dark.png"),
      animations: "disabled",
    });
    await page
      .getByRole("combobox", { name: "Audit log rows per page" })
      .press("Escape");
    await page.getByRole("button", { name: "Switch to light theme" }).click();
    await page.setViewportSize({ width: 390, height: 844 });
    await choose(page, "Audit log rows per page", "20 / page");
    await page.screenshot({
      path: join(output, "audit-pagination-mobile.png"),
      animations: "disabled",
    });
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.getByRole("link", { name: "Sessions", exact: true }).click();
    await expect(page.locator(".session-item")).not.toContainText(
      join(dir, "project"),
    );
    const timelinePager = page.getByRole("navigation", {
      name: "Execution timeline pagination",
    });
    await expect(page.locator(".timeline-item")).toHaveCount(10);
    await timelinePager.getByRole("button", { name: "Next page" }).click();
    await expect(timelinePager.locator(".pagination-position")).toContainText(
      "2 /",
    );
    await page.locator(".session-item").first().click();
    await expect(timelinePager.locator(".pagination-position")).toContainText(
      "1 /",
    );
    await page.screenshot({
      path: join(output, "sessions-no-path.png"),
      fullPage: true,
      animations: "disabled",
    });
    await expect(page.getByText("Execution timeline", { exact: true })).toBeVisible();
    await page.getByRole("link", { name: "Overview", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
    await expect(page.locator(".dashboard")).toContainText(
      "Review model token usage",
    );
    const approvalChart = page.locator(".review-charts .trend-chart").first();
    const approvalBox = await page.locator(".review-charts").boundingBox(),
      usageBox = await page.locator(".usage-panel").boundingBox();
    expect(approvalBox!.y).toBeLessThan(usageBox!.y);
    const svg = approvalChart.locator("svg");
    await svg.focus();
    await svg.press("End");
    await expect(approvalChart.locator(".chart-tooltip")).toContainText(
      "14 calls",
    );
    await svg.press("Home");
    await expect(approvalChart.locator(".chart-tooltip")).toContainText("0 calls");
    const graphBox = await svg.boundingBox();
    await svg.hover({ position: { x: graphBox!.width - 18, y: 60 } });
    await expect(approvalChart.locator(".chart-tooltip")).toContainText(
      "14 calls",
    );
    await page
      .locator(".donut-legend button")
      .filter({ hasText: "Blocked" })
      .click();
    await expect(page.locator(".donut-center")).toContainText("14");
    await expect(page.locator(".donut-center")).toContainText("50.0%");
    await expect(page.locator(".cost-number")).toHaveText("¥0.0038");
    await expect(page.locator(".usage-summary")).toContainText("2,100");
    await page.getByRole("button", { name: "30 days", exact: true }).click();
    await expect(page).toHaveURL(/days=30/);
    await page.locator(".usage-chart summary").click();
    await expect(page.locator(".usage-chart tbody tr")).toHaveCount(30);
    await choose(page, "Statistics model", "openai:fixture-reviewer");
    await expect(page.locator(".cost-number")).toHaveText("¥0.0038");
    await choose(page, "Currency", "USD (dollar)");
    await expect(page.locator(".cost-number")).toHaveText("—");
    await choose(page, "Currency", "CNY (yuan)");
    await page.locator(".usage-chart summary").click();
    await page.setViewportSize({ width: 1920, height: 1080 });
    await page.evaluate(() => scrollTo(0, 0));
    await page.screenshot({
      path: join(output, "dashboard-1920.png"),
      fullPage: true,
    });
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.evaluate(() => scrollTo(0, 0));
    await page.screenshot({
      path: join(output, "dashboard-light.png"),
      fullPage: true,
    });
    await page.getByRole("button", { name: "Switch to dark theme" }).click();
    await page.evaluate(() => scrollTo(0, 0));
    await page.screenshot({
      path: join(output, "dashboard-dark.png"),
      fullPage: true,
    });
    await page.getByRole("button", { name: "Switch to light theme" }).click();
    await page.setViewportSize({ width: 390, height: 844 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBeTruthy();
    await page.evaluate(() => scrollTo(0, 0));
    await page.screenshot({
      path: join(output, "dashboard-mobile.png"),
      fullPage: true,
    });
    await page.setViewportSize({ width: 1440, height: 1000 });
    expect(errors).toEqual([]);
    proc.kill("SIGTERM");
    await once(proc, "exit");
    await expect(
      page.getByText("Review service connection failed", { exact: true }),
    ).toBeVisible();
  } finally {
    clearInterval(beat);
  }
});

test("post-verification data guard defaults off and preserves prompt edits", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/settings");
  await page.getByLabel("Admin token", { exact: true }).fill("browser-test-admin");
  await page.getByRole("button", { name: "Open console" }).click();
  await expect(page).toHaveURL(/\/settings$/);
  await expect(page).toHaveTitle(/AegisHook/);
  await expect(
    page.getByRole("heading", { name: "Model review prompt", exact: true }),
  ).toBeVisible();
  // The unauthenticated startup probe returns 401 before login by design.
  // Check console errors for the authenticated settings flow below.
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  const toggle = page.getByRole("switch", {
    name: /Limit bulk data retrieval after a vulnerability is confirmed/,
  });
  const editor = page.getByLabel("Prompt content", { exact: true });
  const template = await (
    await page.request.get("/api/v1/settings/default-prompt")
  ).json();
  const start = template.dataGuardStart as string;
  await expect(toggle).toBeEnabled();
  await expect(toggle).not.toBeChecked();
  await expect(editor).toHaveValue(template.prompt);
  expect(template.prompt).not.toContain(start);
  const help = page.getByRole("button", {
    name: "Limit bulk data retrieval after a vulnerability is confirmed help",
    exact: true,
  });
  const tooltip = page.getByRole("tooltip");
  await expect(tooltip).toHaveCount(0);
  await help.hover();
  await expect(tooltip).toContainText("judged only from the current tool arguments");
  await expect(tooltip).toContainText("Previous commands and results are not read");
  await expect(tooltip).toContainText("takes effect after saving settings");
  await page.mouse.move(0, 0);
  await expect(tooltip).toHaveCount(0);
  await help.focus();
  await expect(tooltip).toBeVisible();
  await help.press("Escape");
  await expect(tooltip).toHaveCount(0);
  await toggle.check();
  const original = await editor.inputValue();
  expect(original).toContain(template.dataGuardPrompt);
  expect(original.indexOf(start)).toBeLessThan(
    original.indexOf(template.dataGuardAnchor),
  );

  const customSuffix = "\nCustom requirement: keep the decision note concise.";
  await editor.fill(original + customSuffix);
  await toggle.uncheck();
  await expect(toggle).not.toBeChecked();
  const disabled = await editor.inputValue();
  expect(disabled).not.toContain(start);
  expect(disabled).toContain(customSuffix);
  async function saveAndReload() {
    const response = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/v1/settings") &&
        response.request().method() === "PUT",
    );
    await page.getByRole("button", { name: "Save settings", exact: true }).click();
    expect((await response).ok()).toBeTruthy();
    await page.reload();
    await expect(toggle).toBeEnabled();
  }
  const pricingToggle = page.getByRole("switch", {
    name: "Enable cost estimate",
    exact: true,
  });
  const originalPricing = await pricingToggle.getAttribute("aria-checked");
  await pricingToggle.click();
  await saveAndReload();
  await expect(pricingToggle).toHaveAttribute(
    "aria-checked",
    originalPricing === "true" ? "false" : "true",
  );
  await page
    .getByRole("button", { name: "Enable cost estimate help", exact: true })
    .hover();
  await expect(tooltip).toContainText("actual billing is determined by your provider");
  await page.mouse.move(0, 0);
  await pricingToggle.click();
  await expect(toggle).not.toBeChecked();
  await expect(editor).toHaveValue(disabled);
  await toggle.check();
  await expect(editor).toHaveValue(original + customSuffix);
  await toggle.uncheck();
  await toggle.check();
  expect((await editor.inputValue()).split(start)).toHaveLength(2);
  await saveAndReload();
  await expect(toggle).toBeChecked();
  await expect(editor).toHaveValue(original + customSuffix);

  // A custom prompt without the standard section gets a prepended block.
  const custom =
    "Custom review prompt. Keep this text, line breaks and output format.\nOutput JSON only.";
  await editor.fill(custom);
  await expect(toggle).not.toBeChecked();
  await toggle.check();
  expect(await editor.inputValue()).toBe(
    template.dataGuardPrompt + "\n\n" + custom,
  );
  await toggle.uncheck();
  await expect(editor).toHaveValue(custom);
  await page.getByRole("button", { name: "Restore default", exact: true }).click();
  await expect(toggle).not.toBeChecked();
  await expect(editor).toHaveValue(template.prompt);
  await saveAndReload();
  await expect(toggle).not.toBeChecked();
  await expect(editor).toHaveValue(template.prompt);
  await expect(page.locator("vite-error-overlay")).toHaveCount(0);
  await help.hover();
  await expect(tooltip).toBeVisible();
  await page.screenshot({
    path: join(tmpdir(), "aegishook-data-guard-desktop.png"),
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await help.click();
  await expect(tooltip).toBeVisible();
  await expect(toggle).toBeVisible();
  const tooltipBox = await tooltip.boundingBox();
  expect(tooltipBox!.x).toBeGreaterThanOrEqual(0);
  expect(tooltipBox!.x + tooltipBox!.width).toBeLessThanOrEqual(390);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
  await page.screenshot({
    path: join(tmpdir(), "aegishook-data-guard-mobile.png"),
  });
  expect(errors).toEqual([]);
});

test("multiple agents retain distinct sources, lifecycle labels and timelines", async ({
  page,
}) => {
  const { checkMultiAgentDisplay } = await import("./multi-agent-checks");
  await checkMultiAgentDisplay(page);
});
