<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { useRoute } from "vue-router";
import {
  api,
  useConsole,
  date,
  decisionLabel,
  callTitle,
  agentName,
  reviewPathLabel,
  matchedRuleName,
} from "../store";
import type { Call } from "../types";
import { quickApproveBlock } from "../approvalGuard";
import Pagination from "../components/Pagination.vue";
import Icon from "../components/Icon.vue";
import AppSelect from "../components/AppSelect.vue";
import AppDatePicker from "../components/AppDatePicker.vue";
import CallDetail from "../components/CallDetail.vue";
const store = useConsole(),
  route = useRoute();
const tab = ref("all"),
  search = ref(""),
  reviewPath = ref(""),
  execution = ref(""),
  from = ref(""),
  to = ref(""),
  expanded = ref(""),
  pendingExpanded = ref(""),
  busy = ref(""),
  rejecting = ref<Call | null>(null),
  reason = ref("");
const isCalls = computed(() => route.path === "/calls");
const recordsPage = reactive({ page: 1, pageSize: 10, total: 0, rows: [] as Call[] });
const pendingPage = reactive({ page: 1, pageSize: 10, total: 0, rows: [] as Call[] });
const tabs = [
  ["all", "All records"],
  ["reject", "Rejected"],
  ["approve", "Approved"],
  ["pending", "Pending approval"],
];
const reviewPathOptions = [
  ["", "All methods"],
  ["scope", "Scope check"],
  ["rule", "Rule decision"],
  ["human", "Human approval"],
  ["model", "Model approval"],
];
const executionOptions = [
  ["", "All executions"],
  ["not_executed", "Not executed"],
  ["awaiting_execution", "Awaiting execution"],
  ["succeeded", "Succeeded"],
  ["failed", "Failed"],
  ["unknown", "Execution unknown"],
];
async function decide(c: Call, d: string) {
  busy.value = c.id;
  const ok = await store.action(
    () =>
      api(`/approvals/${c.id}/decision`, "POST", {
        digest: c.digest,
        decision: d,
        comment: d === "reject" ? reason.value : "",
      }),
    d === "approve" ? "This call was approved" : "Rejected; the reason was returned to the Agent",
  );
  if (ok) {
    rejecting.value = null;
    reason.value = "";
  }
  busy.value = "";
}
async function loadRecords() {
  recordsPage.page = Math.max(1, recordsPage.page);
  const page = await store.loadApprovalCalls({
    page: recordsPage.page,
    size: recordsPage.pageSize,
    q: search.value || String(route.query.q || ""),
    decision: tab.value === "all" ? undefined : tab.value,
    reviewPath: reviewPath.value,
    execution: execution.value,
    from: from.value,
    to: to.value,
  });
  recordsPage.total = page.total;
  recordsPage.rows = page.items;
}
watch([tab, search, reviewPath, execution, from, to, () => route.query.q], () => {
  recordsPage.page = 1;
  void loadRecords();
}, { immediate: true });
watch([() => recordsPage.page, () => recordsPage.pageSize], () => void loadRecords());
watch(
  () => [store.approvalPendingTotal, store.pending, pendingPage.page, pendingPage.pageSize],
  () => {
    pendingPage.total = store.pending.length;
    pendingPage.page = Math.min(
      pendingPage.page,
      Math.max(1, Math.ceil(pendingPage.total / pendingPage.pageSize)),
    );
    pendingPage.rows = store.pending.slice((pendingPage.page - 1) * pendingPage.pageSize, pendingPage.page * pendingPage.pageSize);
  },
  { immediate: true },
);
</script>
<template>
  <div class="page-heading">
    <h1>
      <Icon :name="isCalls ? 'Workflow' : 'ClipboardList'" :size="23" />{{
        isCalls ? "Tool calls" : "Approvals & blocks"
      }}<span v-if="store.pending.length" class="pending-count"
        >{{ store.pending.length }} pending approval</span
      >
    </h1>
    <button @click="store.refresh" :disabled="store.loading">
      <Icon name="RefreshCw" :size="16" />Refresh
    </button>
  </div>
  <section v-if="!isCalls" class="panel pending-panel">
    <div class="panel-heading">
      <strong
        ><Icon name="ShieldAlert" :size="17" />Pending ({{
          store.pending.length
        }})</strong
      >
    </div>
    <div class="table-scroll">
      <table class="review-table pending-table">
        <colgroup>
          <col class="col-id" />
          <col class="col-tool" />
          <col class="col-source" />
          <col class="col-rule" />
          <col class="col-args" />
          <col class="col-time" />
          <col class="col-pending-actions" />
        </colgroup>
        <thead>
          <tr>
            <th>#</th>
            <th>Tool</th>
            <th>Source</th>
            <th>Matched rule</th>
            <th>Arguments</th>
            <th>Time</th>
            <th class="align-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="c in pendingPage.rows" :key="c.id">
            <tr>
              <td class="muted record-id" :title="c.id">
                {{ c.id.slice(0, 6) }}
              </td>
              <td>
                <code class="tool-tag">{{ c.toolName }}</code>
              </td>
              <td class="source-cell">
                <strong>{{ agentName(c.agent) }}</strong
                ><small :title="c.cwd"
                  >Project: {{
                    c.cwd.split(/[\\/]/).filter(Boolean).pop() || "Not provided"
                  }}</small
                ><small :title="c.sessionId">{{
                  c.sessionId.slice(0, 12)
                }}</small>
              </td>
              <td class="reason-cell">
                <span class="badge model"
                  ><Icon
                    :name="c.mode === 'model' ? 'Bot' : 'UserRound'"
                    :size="12"
                  />Human review</span
                ><small :title="c.comment">{{
                  c.comment || "No explicit rule matched; waiting for approval"
                }}</small>
              </td>
              <td>
                <button
                  class="parameter-preview"
                  :title="JSON.stringify(c.argumentsObj)"
                  @click="
                    pendingExpanded = pendingExpanded === c.id ? '' : c.id
                  "
                  aria-label="View pending approval details"
                >
                  {{ JSON.stringify(c.argumentsObj) }}
                </button>
              </td>
              <td class="muted nowrap">{{ date(c.createdAt) }}</td>
              <td>
                <div class="actions row-actions">
                  <button
                    class="primary"
                    :disabled="!!busy || !!quickApproveBlock(c)"
                    :title="
                      quickApproveBlock(c)
                        ? `Open the details to approve: ${quickApproveBlock(c)}`
                        : undefined
                    "
                    @click="decide(c, 'approve')"
                  >
                    <Icon name="Check" :size="15" />Allow this call</button
                  ><button
                    class="danger-button"
                    :disabled="!!busy"
                    @click="rejecting = c"
                  >
                    <Icon name="X" :size="15" />Reject
                  </button>
                </div>
                <small v-if="quickApproveBlock(c)" class="muted"
                  >Open the details to approve: {{ quickApproveBlock(c) }}.</small
                >
              </td>
            </tr>
            <tr v-if="pendingExpanded === c.id" class="expanded-row">
              <td colspan="7">
                <CallDetail :call="c" />
                <div v-if="quickApproveBlock(c)" class="actions">
                  <button
                    class="primary"
                    :disabled="!!busy"
                    @click="decide(c, 'approve')"
                  >
                    <Icon name="Check" :size="15" />Approve after reading the full call
                  </button>
                </div>
              </td>
            </tr>
          </template>
          <tr v-if="!store.pending.length">
            <td colspan="7" class="empty-cell">No pending approvals</td>
          </tr>
        </tbody>
      </table>
    </div>
    <Pagination
      v-model:page="pendingPage.page"
      v-model:page-size="pendingPage.pageSize"
      :total="pendingPage.total"
      label="Pending"
    />
  </section>
  <section class="panel records">
    <div class="records-heading">
      <h2>All records ({{ recordsPage.total }})</h2>
      <div class="record-filters">
        <div class="search-input">
          <Icon name="Search" :size="15" /><input
            v-model="search"
            placeholder="Search operations or rules"
            aria-label="Search operations or rules"
          />
        </div>
        <AppSelect
          v-model="tab"
          label="Filter by decision"
          :options="tabs.map((t) => ({ value: t[0]!, label: t[1]! }))"
        />
        <AppSelect
          v-model="reviewPath"
          label="Filter by approval method"
          :options="reviewPathOptions.map((t) => ({ value: t[0]!, label: t[1]! }))"
        />
        <AppSelect
          v-model="execution"
          label="Filter by execution status"
          :options="executionOptions.map((t) => ({ value: t[0]!, label: t[1]! }))"
        />
        <AppDatePicker v-model="from" label="Start date" prefix="From" />
        <AppDatePicker v-model="to" label="End date" prefix="To" />
        <a
          class="icon-button"
          href="/api/v1/calls/export"
          download
          aria-label="Export records"
          title="Export records"
          ><Icon name="Download" :size="17"
        /></a>
      </div>
    </div>
    <div class="table-scroll">
      <table class="review-table records-table">
        <colgroup>
          <col class="col-id" />
          <col class="col-tool" />
          <col class="col-source" />
          <col class="col-rule" />
          <col class="col-args" />
          <col class="col-state" />
          <col class="col-time" />
          <col class="col-time" />
          <col class="col-details" />
        </colgroup>
        <thead>
          <tr>
            <th>#</th>
            <th>Tool</th>
            <th>Source</th>
            <th>Matched rule</th>
            <th>Arguments</th>
            <th>Status</th>
            <th>Requested</th>
            <th>Decided</th>
            <th><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody>
          <template v-for="c in recordsPage.rows" :key="c.id">
            <tr :class="{ selected: expanded === c.id }">
              <td class="muted record-id" :title="c.id">
                {{ c.id.slice(0, 6) }}
              </td>
              <td>
                <code class="tool-tag" :title="callTitle(c)">{{
                  c.toolName
                }}</code>
              </td>
              <td class="source-cell">
                <strong>{{ agentName(c.agent) }}</strong
                ><small :title="c.cwd"
                  >Project: {{
                    c.cwd.split(/[\\/]/).filter(Boolean).pop() || "Not provided"
                  }}</small
                ><small :title="c.sessionId">{{
                  c.sessionId.slice(0, 12)
                }}</small>
              </td>
              <td class="reason-cell">
                <span v-if="c.reviewPath === 'model'" class="badge model"
                  ><Icon name="Bot" :size="12" />{{ "Model decision" }}</span
                ><strong v-else>{{
                  matchedRuleName(c) || reviewPathLabel(c)
                }}</strong
                ><small v-if="c.reviewPath === 'rule'">Decided directly by rule</small
                ><small :title="c.comment">{{
                  c.comment ||
                  (c.mode === "model" ? "Model review in progress" : "Waiting for a human decision")
                }}</small>
              </td>
              <td>
                <button
                  class="parameter-preview"
                  :title="JSON.stringify(c.argumentsObj)"
                  @click="expanded = expanded === c.id ? '' : c.id"
                  aria-label="View arguments"
                >
                  {{ JSON.stringify(c.argumentsObj) }}
                </button>
              </td>
              <td>
                <span class="badge" :class="c.decision">{{
                  c.decision === "pending" && c.mode === "model"
                    ? "Model review in progress"
                    : decisionLabel(c.decision)
                }}</span>
              </td>
              <td class="muted nowrap">{{ date(c.createdAt) }}</td>
              <td class="muted nowrap">{{ date(c.decidedAt) }}</td>
              <td>
                <button
                  class="icon-button detail-toggle"
                  @click="expanded = expanded === c.id ? '' : c.id"
                  :aria-label="expanded === c.id ? 'Hide details' : 'View details'"
                >
                  <Icon
                    :name="expanded === c.id ? 'ChevronUp' : 'ChevronRight'"
                    :size="15"
                  />
                </button>
              </td>
            </tr>
            <tr v-if="expanded === c.id" class="expanded-row">
              <td colspan="9"><CallDetail :call="c" /></td>
            </tr>
          </template>
          <tr v-if="!recordsPage.rows.length">
            <td colspan="9" class="empty-cell">
              {{
                recordsPage.total ? "No matching records" : "No tool call records"
              }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <Pagination
      v-model:page="recordsPage.page"
      v-model:page-size="recordsPage.pageSize"
      :total="recordsPage.total"
      :label="isCalls ? 'Tool calls' : 'Approval records'"
    />
  </section>
  <div v-if="rejecting" class="modal-backdrop" @click.self="rejecting = null">
    <form class="modal" @submit.prevent="decide(rejecting!, 'reject')">
      <div class="panel-heading">
        <h2>Reject this call</h2>
        <button
          type="button"
          class="icon-button"
          @click="rejecting = null"
          aria-label="Close"
        >
          <Icon name="X" />
        </button>
      </div>
      <p class="muted">The reason is returned to the Agent to help it adjust its next steps.</p>
      <label
        >Rejection reason<textarea
          v-model="reason"
          required
          rows="4"
          placeholder="Explain why this operation is not allowed and suggest a workable alternative"
        />
      </label>
      <div class="actions end">
        <button type="button" @click="rejecting = null">Cancel</button
        ><button class="danger-button" :disabled="!!busy">Confirm rejection</button>
      </div>
    </form>
  </div>
</template>
