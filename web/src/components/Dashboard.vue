<script setup lang="ts">
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useConsole, date, callTitle, decisionLabel } from "../store";
import AppSelect from "./AppSelect.vue";
import Icon from "./Icon.vue";
import DecisionDonut from "./DecisionDonut.vue";
import TrendChart from "./TrendChart.vue";
import type { Point } from "./TrendChart.vue";
const store = useConsole(),
  route = useRoute(),
  router = useRouter();
const days = computed(() =>
  [7, 30, 90].includes(Number(route.query.days)) ? Number(route.query.days) : 7,
);
const model = computed({
  get: () => (typeof route.query.model === "string" ? route.query.model : ""),
  set: (v: string) =>
    void router.replace({ query: { ...route.query, model: v || undefined } }),
});
const currency = computed({
  get: () => (route.query.currency === "USD" ? "USD" : "CNY"),
  set: (v: string) =>
    void router.replace({ query: { ...route.query, currency: v } }),
});
const dayKey = (d: Date) =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
const dateKeys = computed(() => {
  const end = new Date(store.updatedAt || Date.now());
  return Array.from({ length: days.value }, (_, i) => {
    const d = new Date(end);
    d.setDate(end.getDate() - (days.value - 1 - i));
    return dayKey(d);
  });
});
const inRange = (s: string) => dateKeys.value.includes(dayKey(new Date(s)));
const calls = computed(() => store.calls.filter((c) => inRange(c.createdAt)));
const records = computed(() =>
  store.usage.filter(
    (u) =>
      inRange(u.createdAt) &&
      (!model.value || `${u.protocol}:${u.model}` === model.value),
  ),
);
const models = computed(() => [
  { value: "", label: "All review models" },
  ...Array.from(new Set(store.usage.map((u) => `${u.protocol}:${u.model}`)))
    .sort()
    .map((v) => ({ value: v, label: v })),
]);
const known = computed(() => records.value.filter((r) => r.usage));
const totals = computed(() =>
  known.value.reduce(
    (a, r) => {
      const u = r.usage!;
      a.input += u.input;
      a.output += u.output;
      a.cacheRead += u.cacheRead;
      a.cacheWrite += u.cacheWrite;
      return a;
    },
    { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
  ),
);
const totalTokens = computed(() =>
  Object.values(totals.value).reduce((a, b) => a + b, 0),
);
const priced = computed(() =>
  records.value.filter(
    (r) => r.cost !== null && r.pricing.currency === currency.value,
  ),
);
const cost = computed(() => priced.value.reduce((a, r) => a + r.cost!, 0));
const legacy = computed(
  () =>
    calls.value.filter(
      (c) => c.modelVerdict && !store.usage.some((u) => u.reviewId === c.id),
    ).length,
);
const missing = computed(() => records.value.length - known.value.length);
const money = (n: number) =>
  `${currency.value === "USD" ? "$" : "¥"}${n.toLocaleString("zh-CN", { minimumFractionDigits: 4, maximumFractionDigits: 6 })}`;
const fmt = (n: number) => n.toLocaleString("zh-CN");
const compact = (n: number) =>
  n >= 1e6
    ? (n / 1e6).toFixed(2) + "M"
    : n >= 10000
      ? (n / 1000).toFixed(1) + "k"
      : fmt(n);
const inputTotal = computed(
  () => totals.value.input + totals.value.cacheRead + totals.value.cacheWrite,
);
const cacheRate = computed(() =>
  inputTotal.value
    ? `${((totals.value.cacheRead / inputTotal.value) * 100).toFixed(1)}%`
    : "—",
);
const points = computed(() =>
  dateKeys.value.map((key) => {
    const cs = calls.value.filter((c) => dayKey(new Date(c.createdAt)) === key),
      us = records.value.filter((u) => dayKey(new Date(u.createdAt)) === key);
    const p: Point = {
      date: key,
      approve: cs.filter((c) => c.decision === "approve").length,
      reject: cs.filter((c) => c.decision === "reject").length,
      pending: cs.filter((c) => c.decision === "pending").length,
      input: 0,
      output: 0,
      cacheRead: 0,
      cacheWrite: 0,
      cost: 0,
    };
    for (const r of us) {
      if (r.usage)
        for (const k of ["input", "output", "cacheRead", "cacheWrite"] as const)
          p[k] = (p[k] as number) + r.usage[k];
      if (r.cost !== null && r.pricing.currency === currency.value)
        p.cost = (p.cost as number) + r.cost;
    }
    const unknown = us.some((r) => !r.usage),
      old = calls.value.some(
        (c) =>
          c.modelVerdict &&
          dayKey(new Date(c.createdAt)) === key &&
          !store.usage.some((u) => u.reviewId === c.id),
      );
    if ((unknown || old) && !us.some((r) => r.usage))
      for (const k of ["input", "output", "cacheRead", "cacheWrite"])
        p[k] = null;
    if (old || us.some((r) => r.cost === null)) p.cost = null;
    return p;
  }),
);
const tokenSeries = [
  { key: "input", label: "Input (uncached)", color: "#3b82f6" },
  { key: "cacheRead", label: "Cache read", color: "#20b486" },
  { key: "cacheWrite", label: "Cache write", color: "#e6a236" },
  { key: "output", label: "Output", color: "#8b5cf6" },
];
const recent = computed(() =>
  calls.value.filter((c) => c.decision === "reject").slice(0, 5),
);
</script>
<template>
  <div class="dashboard">
    <div class="dashboard-toolbar">
      <span class="muted tiny"
        ><i class="dot" :class="{ green: store.streamOnline }" />{{
          store.streamOnline ? "Live updates" : "Disconnected · showing last data"
        }}
        · {{ date(store.updatedAt) }} · local time zone</span
      >
      <div class="segmented" aria-label="Statistics time range">
        <button
          v-for="n in [7, 30, 90]"
          :key="n"
          :aria-pressed="days === n"
          :class="{ active: days === n }"
          @click="router.replace({ query: { ...route.query, days: n } })"
        >
          {{ n }} days
        </button>
      </div>
    </div>
    <div class="dashboard-metrics">
      <article class="panel metric-card">
        <span><Icon name="Activity" />Tool calls</span
        ><strong>{{ fmt(calls.length) }}</strong
        ><small
          >Last {{ days }} days ·
          {{ calls.filter((c) => c.execution === "succeeded").length }}
          succeeded</small
        >
      </article>
      <article class="panel metric-card">
        <span><Icon name="ShieldX" />Blocked</span
        ><strong class="reject">{{
          fmt(calls.filter((c) => c.decision === "reject").length)
        }}</strong
        ><small
          >{{
            calls.filter((c) => c.decision === "approve").length
          }}
          approved</small
        >
      </article>
      <article class="panel metric-card">
        <span><Icon name="Clock3" />Pending review</span
        ><strong class="pending">{{
          store.calls.filter((c) => c.decision === "pending").length
        }}</strong
        ><small>{{ store.sessionSummary }}</small>
      </article>
      <article class="panel metric-card">
        <span><Icon name="Bot" />Review tokens</span
        ><strong>{{ known.length ? compact(totalTokens) : "—" }}</strong
        ><small>{{ known.length }} / {{ records.length }} requests with usage</small>
      </article>
      <article class="panel metric-card">
        <span><Icon name="Circle" />Estimated cost · {{ currency }}</span
        ><strong class="cost-number">{{
          priced.length ? money(cost) : "—"
        }}</strong
        ><small
          >{{ priced.length }} priced ·
          <router-link to="/settings">Set pricing</router-link></small
        >
      </article>
    </div>
    <div class="dashboard-charts review-charts">
      <section class="panel">
        <div class="panel-heading">
          <strong><Icon name="Activity" />Tool approval trend</strong
          ><span class="muted tiny">By submission date · current decision</span>
        </div>
        <TrendChart
          title="Tool approval trend"
          :points="points"
          :series="[
            { key: 'approve', label: 'Approved', color: '#20b486' },
            { key: 'reject', label: 'Blocked', color: '#f05265' },
            { key: 'pending', label: 'Pending review', color: '#e6a236' },
          ]"
          unit="calls"
        />
      </section>
      <section class="panel decision-panel">
        <div class="panel-heading">
          <strong><Icon name="ShieldCheck" />Decision breakdown</strong
          ><span class="muted tiny">Last {{ days }} days · count and share</span>
        </div>
        <DecisionDonut :calls="calls" />
      </section>
    </div>
    <section class="panel usage-panel">
      <div class="panel-heading">
        <strong><Icon name="Bot" />Review model token usage</strong
        ><AppSelect v-model="model" label="Statistics model" :options="models" />
      </div>
      <div class="usage-layout">
        <div class="usage-summary">
          <span class="muted">Total (input + output)</span
          ><strong>{{ known.length ? compact(totalTokens) : "—" }}</strong
          ><small class="muted">Includes connection tests · excludes the Agent's own usage</small>
          <div v-for="s in tokenSeries" :key="s.key" class="token-breakdown">
            <div>
              <span>{{ s.label }}</span
              ><b :style="{ color: s.color }">{{
                fmt(totals[s.key as keyof typeof totals])
              }}</b>
            </div>
            <div class="token-track">
              <i
                :style="{
                  width:
                    (totalTokens
                      ? (totals[s.key as keyof typeof totals] / totalTokens) *
                        100
                      : 0) + '%',
                  background: s.color,
                }"
              />
            </div>
          </div>
          <div class="cache-rate">
            <span>Input cache hit rate</span><b>{{ cacheRate }}</b>
          </div>
        </div>
        <div class="usage-chart">
          <div v-if="!known.length" class="chart-notice">
            No token records yet; counting starts once model requests return usage.
          </div>
          <TrendChart
            title="Review token trend"
            :points="points"
            :series="tokenSeries"
            unit="Token"
          />
        </div>
      </div>
      <div class="dashboard-footnote">
        {{ missing }} requests with unknown usage ·
        {{ legacy }} historical reviews not captured; unknown data is not
        counted as 0 in cost calculations. Usage statistics start from this upgrade.
      </div>
    </section>
    <section class="panel">
      <div class="panel-heading">
        <strong><Icon name="Activity" />Cost trend</strong
        ><AppSelect
          v-model="currency"
          label="Currency"
          :options="[
            { value: 'CNY', label: 'CNY (yuan)' },
            { value: 'USD', label: 'USD (dollar)' },
          ]"
        />
      </div>
      <TrendChart
        title="Cost trend"
        :points="points"
        :series="[{ key: 'cost', label: 'Priced cost', color: '#8b5cf6' }]"
        kind="line"
        :unit="currency"
      />
      <div class="dashboard-footnote">
        {{ records.filter((r) => r.cost === null).length }}
        not priced; days with unknown cost are left blank. Currencies are counted separately, with no exchange-rate conversion.
      </div>
    </section>
    <section class="panel recent-blocks">
      <div class="panel-heading">
        <strong><Icon name="ShieldAlert" />Recent blocks</strong
        ><router-link class="text-button" to="/approvals"
          >View records<Icon name="ArrowRight" :size="14"
        /></router-link>
      </div>
      <div v-if="!recent.length" class="dashboard-empty">
        No blocked calls in the selected time range
      </div>
      <div v-for="c in recent" :key="c.id" class="recent-block">
        <code class="tool-tag">{{ c.toolName }}</code>
        <div>
          <strong>{{ callTitle(c) }}</strong>
          <p class="muted">{{ c.comment }}</p>
        </div>
        <span class="muted tiny">{{ date(c.createdAt) }}</span
        ><span class="badge reject">{{ decisionLabel(c.decision) }}</span>
      </div>
    </section>
  </div>
</template>
