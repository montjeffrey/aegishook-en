<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { api, date, useConsole } from "../store";
const store = useConsole();
const installer = ref<{ shellUrl: string; powershellUrl: string } | null>(null);
const busy = ref("");
const pending = computed(() =>
  store.clientRequests.filter((r) => r.state === "pending"),
);
const recent = computed(() =>
  store.clientRequests.filter((r) => r.state !== "pending").slice(0, 5),
);
const state = (s: string) =>
  ({
    approved: "Approved, waiting to be claimed",
    rejected: "Rejected",
    consumed: "Connected",
    expired: "Expired",
  })[s] || s;
onMounted(async () => {
  try {
    installer.value = await api("/client-installer");
  } catch (e) {
    store.error = (e as Error).message;
  }
});
async function decide(id: string, decision: string) {
  busy.value = id;
  await store.action(
    () => api(`/client-requests/${id}/decision`, "POST", { decision }),
    "Access request handled",
  );
  busy.value = "";
}
async function revoke(id: string) {
  busy.value = id;
  await store.action(
    () => api(`/client-nodes/${id}`, "DELETE"),
    "Device credential revoked; later hook requests will be rejected",
  );
  busy.value = "";
}
const shellCommand = computed(() =>
  installer.value
    ? `curl -fsS '${installer.value.shellUrl}' -o hook.sh\nbash hook.sh install --agent claude --scope global`
    : "",
);
const psCommand = computed(() =>
  installer.value
    ? `Invoke-WebRequest '${installer.value.powershellUrl}' -OutFile hook.ps1\n.\\hook.ps1 -Action install -Agent claude -Scope global`
    : "",
);
async function copy(value: string) {
  try {
    await navigator.clipboard.writeText(value);
    store.notify("Command copied");
  } catch {
    store.error = "Could not copy automatically; select the command and copy it manually";
  }
}
</script>

<template>
  <section class="panel remote-clients" aria-label="Remote devices">
    <div class="panel-heading">
      <strong>Remote devices</strong
      ><span class="badge pending" v-if="pending.length"
        >{{ pending.length }} awaiting access</span
      >
    </div>
    <p class="muted">
      Run the install script on the target machine, then check the request ID in the terminal against the one below and approve. The script only installs or uninstalls the
      hook and does not start a background service.
    </p>
    <details class="download-details">
      <summary>Get Windows / macOS / Linux install commands</summary>
      <template v-if="installer">
        <div class="command-heading">
          <strong>macOS / Linux</strong
          ><button @click="copy(shellCommand)">Copy command</button>
        </div>
        <pre><code>{{ shellCommand }}</code></pre>
        <div class="command-heading">
          <strong>Windows PowerShell</strong
          ><button @click="copy(psCommand)">Copy command</button>
        </div>
        <pre><code>{{ psCommand }}</code></pre>
        <p class="muted tiny">
          For a project install use --scope project --project /absolute/path; add --switch
          to switch between global and project scope. The PowerShell equivalent is -Scope project -Project C:\path
          -SwitchScope. To uninstall, replace install with uninstall and keep the original
          Agent, scope and client directory.
        </p>
        <p class="muted tiny">
          Connect through an HTTPS address or an SSH
          tunnel. The install link carries no credentials; each approved device uses its own revocable connection credential.
        </p>
      </template>
    </details>
    <div class="request-list">
      <p v-if="!pending.length" class="muted">No pending access requests.</p>
      <article
        v-for="r in pending"
        :key="r.id"
        class="request-card"
        :data-request-id="r.id"
      >
        <div>
          <strong>{{ r.name }}</strong
          ><span class="muted"> · {{ r.platform }} · {{ r.ip }}</span>
        </div>
        <code class="request-id">{{ r.id }}</code>
        <small class="muted"
          >Requested {{ date(r.createdAt) }} · expires {{ date(r.expiresAt) }}</small
        >
        <div class="actions">
          <button
            class="primary"
            :disabled="busy === r.id"
            @click="decide(r.id, 'approve')"
          >
            Allow access</button
          ><button :disabled="busy === r.id" @click="decide(r.id, 'reject')">
            Reject</button
          ><button
            class="danger"
            :disabled="busy === r.id"
            @click="decide(r.id, 'block_ip')"
          >
            Reject and block IP
          </button>
        </div>
      </article>
    </div>
    <div class="table-scroll" v-if="store.clientNodes.length">
      <table class="remote-node-table">
        <thead>
          <tr>
            <th>Connected devices</th>
            <th>OS</th>
            <th>Status</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="n in store.clientNodes" :key="n.id">
            <td>
              <strong>{{ n.name || "Unnamed device" }}</strong
              ><small class="mono">{{ n.id }}</small>
            </td>
            <td>{{ n.platform }}</td>
            <td>{{ n.revoked ? "Revoked" : "Active" }}</td>
            <td>
              <button
                v-if="!n.revoked"
                class="danger"
                :disabled="busy === n.id"
                @click="revoke(n.id)"
              >
                Revoke access
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <details v-if="store.clientBlocks.length">
      <summary>IP blocklist ({{ store.clientBlocks.length }})</summary>
      <div v-for="b in store.clientBlocks" :key="b.id" class="blocked-row">
        <code>{{ b.ip }}</code
        ><button
          @click="
            store.action(
              () => api(`/client-ip-blocks/${b.id}`, 'DELETE'),
              'IP unblocked',
            )
          "
        >
          Unblock
        </button>
      </div>
      <p class="muted tiny">
        One egress IP may cover several devices; unblocking does not restore revoked device credentials.
      </p>
    </details>
    <details v-if="recent.length">
      <summary>Recently handled requests</summary>
      <p v-for="r in recent" :key="r.id" class="muted">
        {{ r.name }} · {{ r.ip }} · {{ state(r.state) }}
      </p>
    </details>
  </section>
</template>

<style scoped>
.remote-clients {
  margin-bottom: 24px;
}
.remote-clients > p,
.remote-clients > details,
.request-list {
  margin: 16px;
}
summary {
  cursor: pointer;
  padding: 10px 0;
  font-weight: 600;
}
pre {
  padding: 14px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: color-mix(in srgb, var(--border) 45%, var(--soft));
  color: var(--text);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.command-heading,
.blocked-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 12px 0;
}
.request-card {
  border: 1px solid var(--border, #e3e5eb);
  border-radius: 10px;
  padding: 16px;
  margin-top: 12px;
}
.request-id {
  display: block;
  overflow-wrap: anywhere;
  margin: 10px 0;
}
.request-card .actions {
  margin-top: 14px;
  flex-wrap: wrap;
}
small {
  display: block;
}
@media (max-width: 650px) {
  .remote-node-table {
    min-width: 0;
  }
  .remote-node-table thead {
    display: none;
  }
  .remote-node-table tr {
    display: grid;
    grid-template-columns: 1fr auto;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border, #e3e5eb);
  }
  .remote-node-table td {
    border: 0;
    padding: 4px 0;
    overflow-wrap: anywhere;
    white-space: normal;
  }
  .remote-node-table td:first-child {
    grid-column: 1 / -1;
  }
  .remote-node-table td:last-child {
    grid-column: 1 / -1;
  }
}
</style>
