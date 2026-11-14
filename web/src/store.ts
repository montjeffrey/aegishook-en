import { defineStore } from "pinia";
import { ref, computed } from "vue";
import type {
  UsageRecord,
  AgentInfo,
  Call,
  Instance,
  Installation,
  Rule,
  Scope,
  Settings,
  ClientNode,
  ClientRequest,
  ClientIPBlock,
  ReviewPage,
} from "./types";
export const isOnlineInstance = (i: Instance) =>
  i.online && i.state !== "disconnected" && i.connectionMode !== "events";
export const isEventInstance = (i: Instance) =>
  i.connectionMode === "events" && i.state !== "disconnected";
export const isRecentEventInstance = (i: Instance) =>
  isEventInstance(i) && i.online;
export const instanceStatusClass = (i: Instance) =>
  isOnlineInstance(i) ? "approve" : isRecentEventInstance(i) ? "model" : "";

export async function api<T = any>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const res = await fetch("/api/v1" + path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const value = await res.json();
  if (!res.ok) {
    if (res.status === 401)
      window.dispatchEvent(new Event("aegis-unauthorized"));
    throw new Error(value.error || "Request failed");
  }
  return value;
}
function normalizeReviewPage(
  value: ReviewPage | Call[],
  page: number,
  pageSize: number,
): ReviewPage {
  if (!Array.isArray(value)) return value;
  return {
    items: value,
    total: value.length,
    page,
    pageSize,
    pendingTotal: value.filter((call) => call.decision === "pending" && call.mode === "human").length,
  };
}
export const useConsole = defineStore("console", () => {
  const ready = ref(false),
    authenticated = ref(false),
    error = ref(""),
    toast = ref(""),
    loading = ref(false),
    streamOnline = ref(false);
  const calls = ref<Call[]>([]),
    approvalCalls = ref<Call[]>([]),
    pendingCalls = ref<Call[]>([]),
    approvalTotal = ref(0),
    approvalPendingTotal = ref(0),
    clientNodes = ref<ClientNode[]>([]),
    clientRequests = ref<ClientRequest[]>([]),
    clientBlocks = ref<ClientIPBlock[]>([]),
    instances = ref<Instance[]>([]),
    installations = ref<Installation[]>([]),
    rules = ref<Rule[]>([]),
    scopes = ref<Scope[]>([]),
    audits = ref<any[]>([]),
    usage = ref<UsageRecord[]>([]),
    updatedAt = ref(""),
    settings = ref<Settings | null>(null),
    pi = ref<any>({}),
    agents = ref<AgentInfo[]>([]);
  const pending = computed(() => pendingCalls.value.filter((c) => c.decision === "pending" && c.mode === "human"));
  const online = computed(
    () => instances.value.filter(isOnlineInstance).length,
  );
  const eventConnections = computed(
    () => instances.value.filter(isEventInstance).length,
  );
  const recentEvents = computed(
    () => instances.value.filter(isRecentEventInstance).length,
  );
  const sessionSummary = computed(
    () =>
      `Online ${online.value} · event access ${eventConnections.value} (recently active ${recentEvents.value})`,
  );
  const sortedInstances = computed(() =>
    [...instances.value].sort(
      (a, b) =>
        Number(isOnlineInstance(b) || isRecentEventInstance(b)) -
          Number(isOnlineInstance(a) || isRecentEventInstance(a)) ||
        (Date.parse(b.heartbeat) || 0) - (Date.parse(a.heartbeat) || 0) ||
        a.id.localeCompare(b.id),
    ),
  );
  let stream: EventSource | undefined,
    refreshTimer: ReturnType<typeof setTimeout> | undefined;
  async function refresh() {
    if (loading.value) return;
    loading.value = true;
    try {
      const [c, pageValue, pendingPageValue, i, inst, r, sc, a, s, u, nodes, requests, blocks] =
        await Promise.all([
          api<Call[]>("/calls"),
          api<ReviewPage | Call[]>("/calls?page=1&size=20"),
          api<ReviewPage | Call[]>("/calls?page=1&size=100&decision=pending"),
          api<Instance[]>("/instances"),
          api<Installation[]>("/installations"),
          api<Rule[]>("/rules"),
          api<Scope[]>("/scopes"),
          api<any[]>("/audit"),
          api<Settings>("/settings"),
          api<UsageRecord[]>("/usage"),
          api<ClientNode[]>("/client-nodes"),
          api<ClientRequest[]>("/client-requests"),
          api<ClientIPBlock[]>("/client-ip-blocks"),
        ]);
      const page = normalizeReviewPage(pageValue, 1, 20);
      const pendingPage = normalizeReviewPage(pendingPageValue, 1, 100);
      calls.value = c;
      approvalCalls.value = page.items;
      pendingCalls.value = pendingPage.items;
      approvalTotal.value = page.total;
      approvalPendingTotal.value = page.pendingTotal;
      instances.value = i;
      installations.value = inst;
      rules.value = r;
      scopes.value = sc;
      audits.value = a;
      settings.value = s;
      usage.value = u;
      clientNodes.value = nodes;
      clientRequests.value = requests;
      clientBlocks.value = blocks;
      updatedAt.value = new Date().toISOString();
      error.value = "";
    } catch (e) {
      error.value = (e as Error).message;
    } finally {
      loading.value = false;
    }
  }
  async function loadApprovalCalls(params: Record<string, string | number | undefined> = {}) {
    const query = new URLSearchParams();
    query.set("page", String(params.page || 1));
    query.set("size", String(params.size || 20));
    for (const [key, value] of Object.entries(params)) {
      if (key !== "page" && key !== "size" && value !== undefined && String(value) !== "") query.set(key, String(value));
    }
    const page = normalizeReviewPage(
      await api<ReviewPage | Call[]>(`/calls?${query.toString()}`),
      Number(params.page || 1),
      Number(params.size || 20),
    );
    approvalCalls.value = page.items;
    approvalTotal.value = page.total;
    approvalPendingTotal.value = page.pendingTotal;
    return page;
  }
  function connect() {
    stream?.close();
    stream = new EventSource("/api/v1/events");
    stream.onopen = () => {
      streamOnline.value = true;
    };
    stream.onerror = () => {
      streamOnline.value = false;
      instances.value = instances.value.map((i) => ({ ...i, online: false }));
      // Refresh also detects an expired admin session after a server restart.
      void refresh();
    };
    stream.addEventListener("update", () => {
      clearTimeout(refreshTimer);
      refreshTimer = setTimeout(refresh, 150);
    });
  }
  async function init() {
    try {
      await api("/me");
      authenticated.value = true;
      await refresh();
      connect();
    } catch {
    } finally {
      ready.value = true;
    }
  }
  async function login(token: string) {
    await api("/login", "POST", { token });
    authenticated.value = true;
    await refresh();
    connect();
  }
  async function logout() {
    await api("/logout", "POST", {});
    authenticated.value = false;
    stream?.close();
  }
  function notify(text: string) {
    toast.value = text;
    setTimeout(() => {
      toast.value = "";
    }, 4000);
  }
  async function action(fn: () => Promise<unknown>, message = "Saved") {
    try {
      await fn();
      notify(message);
      await refresh();
      return true;
    } catch (e) {
      error.value = (e as Error).message;
      return false;
    }
  }
  window.addEventListener("aegis-unauthorized", () => {
    authenticated.value = false;
    stream?.close();
  });
  return {
    ready,
    authenticated,
    error,
    toast,
    loading,
    streamOnline,
    calls,
    approvalCalls,
    pendingCalls,
    approvalTotal,
    approvalPendingTotal,
    clientNodes,
    clientRequests,
    clientBlocks,
    instances,
    installations,
    rules,
    scopes,
    audits,
    usage,
    updatedAt,
    settings,
    pi,
    agents,
    pending,
    online,
    eventConnections,
    recentEvents,
    sessionSummary,
    sortedInstances,
    refresh,
    loadApprovalCalls,
    init,
    login,
    logout,
    notify,
    action,
  };
});
export const date = (s: string | null) =>
  s
    ? new Date(s).toLocaleString("zh-CN", {
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hour12: false,
      })
    : "—";
export const decisionLabel = (s: string) =>
  ({ approve: "Approved", reject: "Rejected", pending: "Pending approval" })[s] || s;
export const reviewPathLabel = (c: Call) =>
  c.reviewPath
    ? {
        scope: "Blocked by authorization scope",
        rule: "Decided directly by rule",
        human: "No rule matched → human approval",
        model: "No rule matched → model review",
      }[c.reviewPath]
    : "Review path not recorded";
export const matchedRuleName = (c: Call) =>
  c.reviewPath === "rule"
    ? c.rules?.find((r) => r.id === c.ruleId)?.name || c.ruleId
    : "";
export const executionLabel = (s: string) =>
  ({
    not_executed: "Not executed",
    awaiting_execution: "Awaiting execution result",
    succeeded: "Succeeded",
    failed: "Failed",
  })[s] || s;
export const callTitle = (c: Call) =>
  ({
    R1: "Change user password",
    R2: "Change system configuration",
    R3: "Change accounts or permissions",
    R4: "Delete data or files",
    R5: "Stop business service",
    R6: "Denial-of-service operation",
    R7: "Bulk data retrieval after vulnerability confirmed",
    A7: "Read or query",
  })[c.ruleId] || c.toolName + " tool call";

export const agentName = (id?: string) =>
  ({
    pi: "Pi Agent",
    claude: "Claude Code",
    codex: "Codex",
    opencode: "OpenCode",
    grok: "Grok Build",
  })[id || "pi"] || id;
export const connectionLabel = (i: Instance) =>
  i.connectionMode === "events"
    ? i.state === "disconnected"
      ? i.disconnectReason === "session_end"
        ? "Ended"
        : i.disconnectReason === "service_restart"
          ? "Awaiting reconnect"
          : "Disconnected"
      : isRecentEventInstance(i)
        ? "Event access · recently active"
        : "Event access · no recent events"
    : i.online
      ? "Online"
      : "Disconnected";
