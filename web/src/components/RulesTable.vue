<script setup lang="ts">
import { computed, ref } from "vue";
import type { Rule } from "../types";
import { api, useConsole } from "../store";
import { usePagination } from "../pagination";
import Pagination from "./Pagination.vue";
import AppSelect from "./AppSelect.vue";
import Icon from "./Icon.vue";
import RuleSemantics from "./RuleSemantics.vue";
import RuleSwitch from "./RuleSwitch.vue";

const props = defineProps<{ rules: Rule[] }>();
defineEmits<{ edit: [rule: Rule] }>();
const store = useConsole();
const search = ref(""),
  state = ref("all"),
  expanded = ref("");
const saving = ref(new Set<string>());
const enabledCount = computed(
  () => props.rules.filter((r) => r.enabled).length,
);
const matcherName = (r: Rule) =>
  r.matcher === "semantic"
    ? "Semantic matching"
    : r.matcher === "contains"
      ? "Contains text"
      : "Regular expression";
const category = (r: Rule) =>
  r.id.startsWith("R_SYS_")
    ? "System command"
    : r.id.startsWith("R_DB_")
      ? "Database"
      : r.id.startsWith("R_HTTP_")
        ? "HTTP request"
        : r.id === "R_NET_PIPE"
          ? "Network transfer"
          : r.builtin
            ? "Base rule"
            : "Custom";
const preview = (r: Rule) => {
  const example = r.semantics?.examples[0];
  return r.matcher === "semantic"
    ? example
      ? String(
          example.argumentsObj.command || JSON.stringify(example.argumentsObj),
        )
      : r.semantics?.summary || "Built-in detection"
    : r.pattern;
};
const filtered = computed(() =>
  props.rules.filter(
    (r) =>
      (state.value === "all" || r.enabled === (state.value === "enabled")) &&
      [
        r.id,
        r.name,
        r.tool,
        r.field,
        r.pattern,
        r.message,
        category(r),
        matcherName(r),
        JSON.stringify(r.semantics),
      ]
        .join(" ")
        .toLowerCase()
        .includes(search.value.trim().toLowerCase()),
  ),
);
const page = usePagination(() => filtered.value, [search, state]);
page.pageSize = 50;
async function toggle(rule: Rule, enabled: boolean) {
  if (saving.value.has(rule.id)) return;
  saving.value.add(rule.id);
  try {
    await store.action(
      () => api("/rules", "POST", { ...rule, enabled }),
      enabled ? "Rule enabled; applies to later calls" : "Rule disabled; applies to later calls",
    );
  } finally {
    saving.value.delete(rule.id);
  }
}
async function copy(rule: Rule) {
  try {
    await navigator.clipboard.writeText(
      rule.matcher === "semantic"
        ? [
            rule.semantics?.summary,
            ...(rule.semantics?.checks || []),
            JSON.stringify(rule.semantics?.examples, null, 2),
            rule.semantics?.limits,
          ].join("\n")
        : rule.pattern,
    );
    store.toast = "Rule content copied";
  } catch {
    store.error = "Copy failed; select and copy the expanded rule content manually";
  }
}
</script>

<template>
  <p class="rules-note muted tiny">
    Expand a rule to see its full condition, examples and detection scope. Each switch works independently; base rules and specific rules may overlap, and the remaining enabled rules are still checked after you turn one off.
  </p>
  <section class="panel rules-panel">
    <div class="rules-toolbar">
      <div class="search-input">
        <Icon name="Search" :size="16" /><input
          v-model="search"
          aria-label="Search rules"
          placeholder="Search name, command, match content…"
        />
      </div>
      <AppSelect
        v-model="state"
        label="Rule status"
        :options="[
          { value: 'all', label: 'All statuses' },
          { value: 'enabled', label: 'Enabled' },
          { value: 'disabled', label: 'Disabled' },
        ]"
      />
      <span class="rules-count"
        >{{ rules.length }} rules · {{ enabledCount }} enabled</span
      >
    </div>
    <div
      class="table-scroll rules-scroll"
      tabindex="0"
      aria-label="Policy rules list, scrollable horizontally"
    >
      <table class="rules-table">
        <thead>
          <tr>
            <th>Priority</th>
            <th>Name</th>
            <th>Target</th>
            <th>Type</th>
            <th>Match</th>
            <th>Decision</th>
            <th>Enabled</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="r in page.rows" :key="r.id">
            <tr :data-rule-id="r.id" :class="{ 'rule-disabled': !r.enabled }">
              <td class="rule-priority mono" data-label="Priority">
                {{ r.priority }}
              </td>
              <td class="rule-name">
                <strong>{{ r.name }}</strong
                ><small
                  ><span class="mono">{{ r.id }}</span> ·
                  {{ category(r) }}</small
                >
              </td>
              <td class="rule-target" data-label="Target">
                {{ r.target === "toolName" ? "Tool name" : r.field || "Tool arguments"
                }}<small>{{ r.tool || "All tools" }}</small>
              </td>
              <td class="rule-type" data-label="Type">{{ matcherName(r) }}</td>
              <td class="rule-condition">
                <p v-if="r.matcher === 'semantic'">
                  {{ r.semantics?.summary }}
                </p>
                <code class="rule-preview">{{ preview(r) }}</code>
                <div class="rule-condition-footer">
                  <span v-if="r.matcher === 'semantic'" class="muted tiny"
                    >Match example · not executed</span
                  >
                  <button
                    class="text-button"
                    :aria-label="`${expanded === r.id ? 'Hide' : 'View'} full condition of ${r.name}`"
                    :aria-expanded="expanded === r.id"
                    :aria-controls="`rule-detail-${r.id}`"
                    @click="expanded = expanded === r.id ? '' : r.id"
                  >
                    {{ expanded === r.id ? "Hide condition" : "View full condition"
                    }}<Icon
                      :name="expanded === r.id ? 'ChevronUp' : 'ChevronDown'"
                      :size="14"
                    />
                  </button>
                </div>
              </td>
              <td class="rule-decision" data-label="Decision">
                <span class="badge" :class="r.decision">{{
                  r.decision === "reject" ? "Reject" : "Allow"
                }}</span>
              </td>
              <td class="rule-enabled">
                <RuleSwitch
                  :model-value="r.enabled"
                  :label="`Enable ${r.name}`"
                  :disabled="saving.has(r.id)"
                  @update:model-value="toggle(r, $event)"
                /><small>{{ r.enabled ? "Enabled" : "Disabled" }}</small>
              </td>
              <td class="rule-actions">
                <div class="actions">
                  <button
                    class="icon-button"
                    :aria-label="`Edit ${r.name}`"
                    :title="`Edit ${r.name}`"
                    :disabled="saving.has(r.id)"
                    @click="$emit('edit', r)"
                  >
                    <Icon name="Pencil" :size="16" /></button
                  ><button
                    v-if="!r.builtin"
                    class="icon-button reject"
                    :aria-label="`Delete ${r.name}`"
                    :title="`Delete ${r.name}`"
                    :disabled="saving.has(r.id)"
                    @click="
                      store.action(
                        () => api('/rules/' + r.id, 'DELETE'),
                        'Rule deleted',
                      )
                    "
                  >
                    <Icon name="Trash2" :size="16" />
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="expanded === r.id" class="expanded-row rule-detail-row">
              <td colspan="8">
                <div :id="`rule-detail-${r.id}`" class="rule-detail">
                  <div class="rule-detail-heading">
                    <strong>{{ r.name }} · full condition</strong
                    ><button class="text-button" @click="copy(r)">
                      <Icon name="Copy" :size="14" />Copy content
                    </button>
                  </div>
                  <RuleSemantics
                    v-if="r.matcher === 'semantic' && r.semantics"
                    :semantics="r.semantics"
                    :tool="r.tool"
                  />
                  <template v-else
                    ><p>
                      {{ matcherName(r) }} · {{ r.tool || "All tools" }} ·
                      {{
                        r.target === "toolName"
                          ? "Tool name"
                          : r.field || "Full arguments JSON"
                      }}
                    </p>
                    <pre>{{ r.pattern }}</pre>
                    <p class="muted tiny">
                      {{
                        r.matcher === "contains"
                          ? "Matches the literal text, case-sensitive."
                          : "Uses Go regular expression syntax; use (?i) in the expression to ignore case."
                      }}
                    </p></template
                  >
                  <p v-if="r.message">Decision note: {{ r.message }}</p>
                </div>
              </td>
            </tr>
          </template>
          <tr v-if="!filtered.length">
            <td colspan="8">
              <div class="empty">
                <Icon name="Search" :size="28" /><strong>No matching rules</strong>
                <p>Try other keywords or change the rule status.</p>
                <button
                  @click="
                    search = '';
                    state = 'all';
                  "
                >
                  Clear filters
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <Pagination
      v-model:page="page.page"
      v-model:page-size="page.pageSize"
      :total="page.total"
      label="Policy rules"
    />
  </section>
</template>

<style scoped>
.rules-panel {
  overflow: hidden;
}
.rules-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  padding: 16px;
  border-bottom: 1px solid var(--border);
}
.rules-toolbar .search-input {
  flex: 1;
  min-width: 220px;
  max-width: 420px;
}
.rules-toolbar :deep(.app-select) {
  width: 130px;
}
.rules-count {
  margin-left: auto;
  color: var(--muted);
  font-size: 12px;
}
.rules-table {
  min-width: 1040px;
  table-layout: fixed;
}
.rules-table th,
.rules-table td {
  padding: 12px;
}
.rules-table th:nth-child(1) {
  width: 68px;
}
.rules-table th:nth-child(2) {
  width: 22%;
}
.rules-table th:nth-child(3) {
  width: 104px;
}
.rules-table th:nth-child(4) {
  width: 82px;
}
.rules-table th:nth-child(6) {
  width: 70px;
}
.rules-table th:nth-child(7) {
  width: 72px;
}
.rules-table th:nth-child(8) {
  width: 80px;
}
.rule-name strong {
  line-height: 1.6;
  font-weight: 550;
}
.rule-name,
.rule-target,
.rule-condition {
  overflow-wrap: anywhere;
}
.rule-type,
.rule-target {
  font-size: 12px;
}
.rule-condition p {
  margin: 0 0 7px;
  font-size: 12px;
  line-height: 1.6;
}
.rule-preview,
.rule-detail pre {
  display: block;
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: color-mix(in srgb, var(--border) 45%, var(--soft));
  color: var(--text);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  line-height: 1.6;
}
.rule-condition-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 5px;
}
.rule-condition-footer button {
  padding: 2px 0;
  font-size: 12px;
}
.rule-enabled {
  text-align: center;
}
.rule-disabled .rule-name strong,
.rule-disabled .rule-priority {
  color: var(--muted);
}
.rule-disabled .rule-preview {
  color: var(--muted);
}
.rule-detail {
  padding: 8px;
}
.rule-detail-heading {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}
.rule-detail pre {
  max-height: none;
}
.rules-note {
  line-height: 1.8;
  margin: 12px 0 22px;
}
@media (max-width: 760px) {
  .rules-table {
    min-width: 0;
    display: block;
  }
  .rules-table thead {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  .rules-table tbody {
    display: block;
  }
  .rules-table tr:not(.rule-detail-row) {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px 16px;
    padding: 16px;
    border-bottom: 1px solid var(--border);
  }
  .rules-table td {
    display: block;
    border: 0;
    padding: 0;
    height: auto;
    min-width: 0;
  }
  .rule-name {
    grid-column: 1 / -1;
    grid-row: 1;
  }
  .rule-condition {
    grid-column: 1 / -1;
  }
  .rule-enabled {
    text-align: left;
    display: flex !important;
    align-items: center;
    gap: 9px;
  }
  .rule-enabled small {
    margin: 0;
  }
  .rule-actions {
    align-self: center;
  }
  .rules-table td[data-label]::before {
    content: attr(data-label) " ";
    color: var(--muted);
    margin-right: 6px;
    font-size: 12px;
  }
  .rule-target small {
    display: inline;
  }
  .rules-table .rule-detail-row,
  .rule-detail-row td {
    display: block;
  }
  .rule-detail {
    padding: 16px;
    border-bottom: 1px solid var(--border);
  }
  .rule-detail-heading {
    align-items: flex-start;
  }
  .rules-count {
    margin-left: 0;
  }
  .rules-toolbar .search-input {
    min-width: 100%;
  }
}
</style>
