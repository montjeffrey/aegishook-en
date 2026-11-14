<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import type { Call } from "../types";
import {
  api,
  executionLabel,
  agentName,
  reviewPathLabel,
  matchedRuleName,
} from "../store";
import Icon from "./Icon.vue";
const props = defineProps<{ call: Call }>();
const detail = ref<Call>(props.call);
const call = computed(() => detail.value);
const more = ref(false);
const showCommand = ref(false);
const showScripts = ref(false);
const showModelInput = ref(false);
onMounted(async () => {
  try {
    detail.value = await api<Call>(`/reviews/${encodeURIComponent(props.call.id)}`);
  } catch {
    // Keep the list data if the detail request races with a refresh or deletion.
  }
});
const command = computed(() => {
  const args = detail.value.argumentsObj;
  return typeof args.command === "string"
    ? args.command
    : typeof args.cmd === "string"
      ? args.cmd
      : null;
});
</script>
<template>
  <div class="call-detail">
    <div>
      <div class="request-heading">
        <span class="eyebrow">{{ agentName(call.agent) }} · tool request</span>
        <div
          v-if="command !== null"
          class="request-display"
          role="group"
          aria-label="Argument display mode"
        >
          <button
            type="button"
            :aria-pressed="!showCommand"
            @click="showCommand = false"
          >
            JSON arguments
          </button>
          <button
            type="button"
            :aria-pressed="showCommand"
            @click="showCommand = true"
          >
            Command view
          </button>
        </div>
      </div>
      <pre class="request-content">{{
        showCommand && command !== null
          ? command
          : JSON.stringify(call.argumentsObj, null, 2)
      }}</pre>
      <span class="muted">Execution result: {{ executionLabel(call.execution) }}</span>
      <div v-if="call.scriptFiles?.length" class="script-snapshot">
        <button class="text-button" @click="showScripts = !showScripts">
          {{ showScripts ? "Hide script snapshot" : "View script snapshot" }}
          <Icon :name="showScripts ? 'ChevronUp' : 'ChevronDown'" :size="14" />
        </button>
        <span class="muted">Capture status: {{ call.scriptCaptureStatus || "Unknown" }}</span>
        <template v-if="showScripts">
          <div v-for="script in call.scriptFiles" :key="script.path + script.sha256" class="script-item">
            <p class="muted mono">{{ script.path }} · {{ script.bytes || 0 }} bytes · {{ script.sha256 || "No digest" }}</p>
            <p v-if="script.readError" class="muted">{{ script.readError }}</p>
            <pre v-if="script.content">{{ script.content }}</pre>
          </div>
        </template>
      </div>
    </div>
    <div>
      <span class="eyebrow">{{
        call.decision === "pending" ? "Review status" : "Decision returned to the Agent"
      }}</span>
      <p class="feedback">
        <Icon
          :name="
            call.decision === 'reject'
              ? 'ShieldX'
              : call.decision === 'pending'
                ? 'Clock3'
                : 'CircleCheck'
          "
          :class="call.decision"
        />{{
          call.comment ||
          (call.mode === "model"
            ? "Model review in progress; the tool has not run."
            : "Waiting for human approval; the tool has not run.")
        }}
      </p>
      <span class="muted"
        >{{ reviewPathLabel(call) }} · config version v{{ call.version }}
        <template v-if="call.ruleId">
          · {{ call.ruleId
          }}<template v-if="matchedRuleName(call)"
            > ({{ matchedRuleName(call) }})</template
          ></template
        ></span
      >
    </div>
  </div>
  <div class="detail-more">
    <button class="text-button" @click="more = !more">
      {{ more ? "Hide context" : "View context and execution result"
      }}<Icon :name="more ? 'ChevronUp' : 'ChevronDown'" :size="14" /></button
    ><template v-if="more"
      ><p class="muted">User message</p>
      <pre>{{ call.userMessage || "Not provided" }}</pre>
      <p class="muted">Visible session context</p>
      <pre>{{ call.context || "Not provided" }}</pre>
      <template v-if="call.modelVerdict">
        <p class="muted">Initial model verdict: {{ call.modelVerdict.decision }}</p>
        <pre>{{ call.modelVerdict.comment }}</pre>
      </template>
      <p class="muted">Execution output</p>
      <pre>{{ call.result || "No execution output yet" }}</pre>
      <template v-if="call.modelInput">
        <button class="text-button" @click="showModelInput = !showModelInput">
          {{ showModelInput ? "Hide model input" : "View actual model input" }}
          <Icon :name="showModelInput ? 'ChevronUp' : 'ChevronDown'" :size="14" />
        </button>
        <pre v-if="showModelInput">{{ JSON.stringify(call.modelInput, null, 2) }}</pre>
        <p class="muted">The original script was sent to the review model</p>
      </template>
      <p class="muted mono">Argument digest {{ call.digest }}</p></template
    >
  </div>
</template>
