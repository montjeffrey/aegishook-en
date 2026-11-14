<script setup lang="ts">
import type { RuleSemantics } from "../types";
defineProps<{ semantics: RuleSemantics; tool?: string }>();
</script>
<template>
  <section class="rule-semantics">
    <h3>Semantic checks</h3>
    <p>{{ semantics.summary }}</p>
    <p class="muted tiny">
      Detected by backend code; {{
        tool ? `currently checks only the ${tool} tool` : "no additional tool name restriction"
      }}.
    </p>
    <ul>
      <li v-for="check in semantics.checks" :key="check">{{ check }}</li>
    </ul>
    <h4>Match examples <span class="muted tiny">Arguments only, never executed</span></h4>
    <div
      v-for="(example, index) in semantics.examples"
      :key="index"
      class="semantic-example"
    >
      <code>{{ example.tool }}</code>
      <pre>{{ JSON.stringify(example.argumentsObj, null, 2) }}</pre>
    </div>
    <h4>Current detection scope</h4>
    <p class="muted">{{ semantics.limits }}</p>
    <p class="muted tiny">
      If nothing matches, other rules are checked; with no decision, the call goes to the selected human or model review mode.
    </p>
  </section>
</template>
