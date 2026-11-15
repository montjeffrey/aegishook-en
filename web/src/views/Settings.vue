<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { api, useConsole } from "../store";
import type { Settings } from "../types";
import Icon from "../components/Icon.vue";
import AppSelect from "../components/AppSelect.vue";
import SettingSwitch from "../components/SettingSwitch.vue";
import {
  hasDataGuard,
  setDataGuard,
  type PromptTemplate,
} from "../prompt-guard";
const store = useConsole();
const promptTemplate = ref<PromptTemplate | null>(null);
const dataGuardEnabled = computed(
  () =>
    !!form.value &&
    !!promptTemplate.value &&
    hasDataGuard(form.value.prompt, promptTemplate.value),
);
const form = ref<Settings | null>(null),
  apiKey = ref(""),
  testing = ref(false),
  dirty = ref(false);
const cliModel = computed(() =>
  ["claude-cli", "codex-cli"].includes(form.value?.model.protocol ?? ""),
);
async function loadPromptTemplate() {
  try {
    promptTemplate.value = await api<PromptTemplate>(
      "/settings/default-prompt",
    );
    return promptTemplate.value;
  } catch (error) {
    store.error = String(error);
    return null;
  }
}
onMounted(loadPromptTemplate);
function toggleDataGuard(enabled: boolean) {
  if (!form.value || !promptTemplate.value) return;
  try {
    form.value.prompt = setDataGuard(
      form.value.prompt,
      enabled,
      promptTemplate.value,
    );
    dirty.value = true;
  } catch (error) {
    store.error = String(error);
  }
}
watch(
  () => store.settings,
  (s) => {
    if (s && !dirty.value) {
      form.value = JSON.parse(JSON.stringify(s));
      if (!form.value!.model.pricing.currency)
        form.value!.model.pricing.currency = "CNY";
    }
  },
  { immediate: true },
);
async function save() {
  if (!form.value) return;
  const payload: any = { ...form.value };
  if (apiKey.value) payload.apiKey = apiKey.value;
  const ok = await store.action(() => api("/settings", "PUT", payload));
  if (ok) {
    dirty.value = false;
    form.value = JSON.parse(JSON.stringify(store.settings!));
    apiKey.value = "";
  }
}
async function test() {
  testing.value = true;
  await store.action(
    () => api("/model/test", "POST", {}),
    "Model connection and decision format test passed",
  );
  testing.value = false;
  if (!dirty.value && store.settings)
    form.value = JSON.parse(JSON.stringify(store.settings));
}
async function restore() {
  const p = promptTemplate.value || (await loadPromptTemplate());
  if (!p || !form.value) return;
  form.value!.prompt = p.prompt;
  dirty.value = true;
}
</script>
<template>
  <div class="page-heading">
    <div>
      <h1><Icon name="Settings" :size="24" />System settings</h1>
      <p>Configure the review mode and model. Changes only affect tool calls submitted afterwards.</p>
    </div>
    <button class="primary" @click="save">
      <Icon name="Check" :size="17" />Save settings
    </button>
  </div>
  <form
    v-if="form"
    class="settings-grid"
    @submit.prevent="save"
    @input="dirty = true"
    @change="dirty = true"
  >
    <section class="panel form-panel">
      <h2>Review mode</h2>
      <p class="muted">
        Every tool call is first checked against the authorization scope and enabled rules. If a rule matches and approves or rejects, the result is returned directly; only calls that match no rule go to the mode selected below. Reject rules take priority over allow rules.
      </p>
      <label class="choice" :class="{ chosen: form.mode === 'human' }"
        ><input type="radio" v-model="form.mode" value="human" /><Icon
          name="UserRound"
        /><span
          ><strong>Human review</strong
          ><small>Calls not decided by a rule wait for your approval one by one.</small></span
        ></label
      ><label class="choice" :class="{ chosen: form.mode === 'model' }"
        ><input
          type="radio"
          v-model="form.mode"
          value="model"
          :disabled="!store.settings?.model.tested"
        /><Icon name="Bot" /><span
          ><strong>Model review</strong
          ><small>Calls that match no rule are approved or rejected by the model, without human handoff.</small></span
        ></label
      >
      <div class="notice">
        <Icon name="Info" />
        <p>
          The human approval timer starts at submission and the call is rejected automatically on timeout; a model connection failure, timeout or invalid output also rejects the call.
        </p>
      </div>
      <div class="form-two">
        <label
          >Human approval timeout (seconds)<input
            type="number"
            min="10"
            max="86400"
            v-model.number="form.approvalSeconds" /></label
        ><label
          >Model request timeout (seconds)<input
            type="number"
            min="1"
            max="120"
            v-model.number="form.modelSeconds"
        /></label>
      </div>
    </section>
    <section class="panel form-panel">
      <div class="inline">
        <h2>Review model</h2>
        <span
          class="badge"
          :class="store.settings?.model.tested ? 'approve' : 'pending'"
          >{{ store.settings?.model.tested ? "Test passed" : "Not verified" }}</span
        >
      </div>
      <p class="muted">
        Supports OpenAI-compatible and Anthropic Messages APIs, or your Claude or ChatGPT subscription through the Claude Code or Codex CLI signed in on this machine. Independent of Pi's model login.
      </p>
      <label
        >API protocol<AppSelect
          v-model="form.model.protocol"
          label="API protocol"
          :options="[
            { value: 'openai', label: 'OpenAI-compatible (Chat Completions)' },
            { value: 'anthropic', label: 'Anthropic (Messages)' },
            { value: 'claude-cli', label: 'Claude subscription (Claude Code CLI)' },
            { value: 'codex-cli', label: 'ChatGPT subscription (Codex CLI)' },
          ]" /></label
      ><p v-if="cliModel" class="tiny muted">
        Runs the {{ form.model.protocol === "codex-cli" ? "codex" : "claude" }} command for each review, using its own sign-in; no API key is stored. Each review starts the CLI, so set the model request timeout to 60 seconds or more. {{ form.model.protocol === "codex-cli" ? "Codex runs with its tools turned off, and a review in which it uses one anyway is rejected." : "Claude Code runs with all tools, hooks and plugins turned off." }}
      </p>
      <label v-if="!cliModel"
        >Base URL<input
          v-model="form.model.baseUrl"
          placeholder="https://your-provider.example/v1" /></label
      ><label
        >Model name<input
          v-model="form.model.model"
          :placeholder="
            cliModel ? 'Optional; leave blank for the CLI default' : 'Enter the model ID from your provider'
          " /></label
      ><label v-if="!cliModel"
        >API Key<input
          type="password"
          v-model="apiKey"
          autocomplete="new-password"
          :placeholder="
            form.model.hasKey ? 'Saved; leave blank to keep the current value' : 'Leave blank for a local service without authentication'
          "
      /></label>
      <div class="inline">
        <button type="button" @click="test" :disabled="testing || dirty">
          <Icon name="Plug" :size="16" />{{
            testing ? "Testing…" : "Test saved configuration"
          }}</button
        ><span class="tiny muted">Save the configuration first, then test the connection.</span>
      </div>
    </section>
    <section class="panel form-panel prompt-panel pricing-panel">
      <h2>Review model cost estimate</h2>
      <p class="muted">
        Enter the current model's price per million tokens.
        Connection tests are included and the price at request time is kept; historical requests without pricing are not back-filled. Actual billing is determined by your provider.
      </p>
      <SettingSwitch
        v-model="form.model.pricing.enabled"
        label="Enable cost estimate"
        description="When enabled, review request costs are estimated from the model prices you enter, including connection tests. Historical requests without pricing are not back-filled, and actual billing is determined by your provider. Takes effect after saving settings."
        @update:model-value="dirty = true"
      />
      <div class="pricing-fields">
        <label
          >Pricing currency<AppSelect
            v-model="form.model.pricing.currency"
            label="Pricing currency"
            :options="[
              { value: 'CNY', label: 'CNY (yuan)' },
              { value: 'USD', label: 'USD (dollar)' },
            ]"
        /></label>
        <label
          >Input price<input
            type="number"
            min="0"
            step="any"
            v-model.number="form.model.pricing.input"
        /></label>
        <label
          >Output price<input
            type="number"
            min="0"
            step="any"
            v-model.number="form.model.pricing.output"
        /></label>
        <label
          >Cache read price<input
            type="number"
            min="0"
            step="any"
            v-model.number="form.model.pricing.cacheRead"
        /></label>
        <label
          >Cache write price<input
            type="number"
            min="0"
            step="any"
            v-model.number="form.model.pricing.cacheWrite"
        /></label>
      </div>
      <small class="muted"
        >0
        means free; when you switch model or provider, check the prices too. Cache writes use a single price regardless of cache duration.</small
      >
    </section>
    <section class="panel form-panel prompt-panel">
      <div class="panel-heading">
        <div>
          <h2>Model review prompt</h2>
          <p class="muted">Reviews only the current call · reject rules take priority · the model decides directly</p>
        </div>
        <button type="button" @click="restore">
          <Icon name="RotateCcw" :size="16" />Restore default
        </button>
      </div>
      <SettingSwitch
        :model-value="dataGuardEnabled"
        :disabled="!promptTemplate"
        label="Limit bulk data retrieval after a vulnerability is confirmed"
        :description="'Verification status and the scope of this retrieval are judged only from the current tool arguments; loops or multiple requests within one call are checked together. Previous commands and results are not read, and cumulative amounts across calls are not computed.\nOff by default; enable as needed. Toggling inserts or removes the matching rule block and takes effect after saving settings.\nApplies only to calls that enter model review; an earlier allow rule may still approve a call directly.'"
        @update:model-value="toggleDataGuard"
      />
      <label
        >Prompt content<textarea
          class="prompt-editor mono"
          v-model="form.prompt"
          rows="19"
        />
      </label>
      <div class="inline muted tiny">
        <Icon name="History" :size="15" />Current config v{{
          form.version
        }}; every save creates a new version, and past calls keep a snapshot of their original version.
      </div>
    </section>
  </form>
</template>
