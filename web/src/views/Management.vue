<script setup lang="ts">
import { computed, ref, watch, onMounted } from "vue";
import { useRoute } from "vue-router";
import {
  api,
  useConsole,
  date,
  agentName,
  connectionLabel,
  instanceStatusClass,
} from "../store";
import type { Rule, Scope } from "../types";
import { usePagination } from "../pagination";
import Pagination from "../components/Pagination.vue";
import Icon from "../components/Icon.vue";
import AppSelect from "../components/AppSelect.vue";
import RuleSemantics from "../components/RuleSemantics.vue";
import RemoteClients from "../components/RemoteClients.vue";
import RulesTable from "../components/RulesTable.vue";
import RuleSwitch from "../components/RuleSwitch.vue";
const semanticDefaults = ref<Rule[]>([]);
const formSemantics = computed(
  () =>
    ruleForm.value?.semantics ||
    semanticDefaults.value.find((r) => r.id === ruleForm.value?.id)?.semantics,
);
onMounted(async () => {
  try {
    semanticDefaults.value = await api("/rules/defaults");
  } catch (e) {
    store.error = (e as Error).message;
  }
});
const store = useConsole(),
  route = useRoute();
const kind = computed(() => route.path.slice(1));
const deviceLabel = (id?: string) =>
  id ? store.clientNodes.find((n) => n.id === id)?.name || id : "Console machine";
const headings: Record<string, string> = {
  agents: "Agent integration",
  rules: "Policy rules",
  scopes: "Authorization scope",
  audit: "Audit log",
};
const sub: Record<string, string> = {
  agents: "Manage hook installation, integration verification and current session status for each Agent.",
  rules: "Built-in and custom rules are both editable; reject takes priority, and rules of the same kind match by priority from high to low.",
  scopes: "Set explicit file paths and target scope for linked projects.",
  audit: "Trace every config change, review decision and hook lifecycle operation.",
};
const selectedAgent = ref("pi");
const currentAgent = computed(() =>
  store.agents.find((a) => a.id === selectedAgent.value),
);
const agentOptions = computed(() =>
  store.agents.map((a) => ({ value: a.id, label: a.name })),
);
const installScope = ref("global"),
  project = ref(""),
  busy = ref(false),
  showInstall = ref(false),
  removeId = ref(""),
  filter = ref("");
const ruleForm = ref<Rule | null>(null),
  scopeForm = ref<Scope | null>(null),
  targets = ref(""),
  paths = ref("");
const testTool = ref("bash"),
  testArgs = ref('{"command":"whoami"}'),
  testResult = ref<any>(null);
const configStatuses: Record<string, string> = {
  configured: "Configured",
  uninstalled: "Uninstalled",
  entry_error: "Entry error",
};
async function detect() {
  try {
    store.agents = await api("/agents");
  } catch (e) {
    store.error = (e as Error).message;
  }
}
onMounted(detect);
watch(kind, () => {
  filter.value = "";
  ruleForm.value = null;
  scopeForm.value = null;
});
async function install() {
  busy.value = true;
  const ok = await store.action(
    () =>
      api("/installations", "POST", {
        agent: selectedAgent.value,
        scope: installScope.value,
        project: project.value,
      }),
    "Install entry created. " +
      (currentAgent.value?.instructions || "Restart the client."),
  );
  if (ok) showInstall.value = false;
  busy.value = false;
}
async function uninstall() {
  const ok = await store.action(
    () => api("/installations/" + removeId.value, "DELETE"),
    "Install entry removed; loaded sessions must reload or exit",
  );
  if (ok) removeId.value = "";
}
function newRule() {
  ruleForm.value = {
    id: "U_" + Math.random().toString(36).slice(2, 8).toUpperCase(),
    name: "",
    decision: "reject",
    enabled: true,
    builtin: false,
    tool: "",
    field: "",
    pattern: "",
    matcher: "regex",
    target: "arguments",
    priority: 0,
    message: "",
  };
}
async function restoreRule() {
  const id = ruleForm.value?.id;
  await store.action(async () => {
    const defaults: Rule[] = await api("/rules/defaults");
    const preset = defaults.find((r) => r.id === id);
    if (preset) ruleForm.value = { ...preset };
  }, "Defaults filled in; they take effect after saving");
}
watch(
  () => ruleForm.value?.matcher,
  (matcher) => {
    if (matcher === "semantic" && ruleForm.value) {
      ruleForm.value.field = "";
      ruleForm.value.pattern = "";
      ruleForm.value.target = "arguments";
    }
  },
);
async function saveRule() {
  if (
    await store.action(
      () => api("/rules", "POST", ruleForm.value),
      "Rule saved; applies to later calls",
    )
  )
    ruleForm.value = null;
}
async function tryRule() {
  try {
    testResult.value = await api("/rules/test", "POST", {
      toolName: testTool.value,
      argumentsObj: JSON.parse(testArgs.value),
    });
  } catch (e) {
    store.error = (e as Error).message;
  }
}
function editScope(s?: Scope) {
  scopeForm.value = s
    ? JSON.parse(JSON.stringify(s))
    : { id: "", name: "", project: "", targets: [], paths: [] };
  targets.value = s?.targets.join("\n") || "";
  paths.value = s?.paths.join("\n") || "";
}
async function saveScope() {
  if (!scopeForm.value) return;
  scopeForm.value.platform =
    store.clientNodes.find((n) => n.id === scopeForm.value?.nodeId)?.platform ||
    "";
  scopeForm.value.targets = targets.value
    .split("\n")
    .map((x) => x.trim().toLowerCase())
    .filter(Boolean);
  scopeForm.value.paths = paths.value
    .split("\n")
    .map((x) => x.trim())
    .filter(Boolean);
  if (await store.action(() => api("/scopes", "POST", scopeForm.value)))
    scopeForm.value = null;
}
const auditRows = computed(() =>
  store.audits.filter((a) =>
    JSON.stringify(a).toLowerCase().includes(filter.value.toLowerCase()),
  ),
);
const installationPage = usePagination(() => store.installations, [kind]);
const instancePage = usePagination(() => store.sortedInstances, [kind]);
const scopePage = usePagination(() => store.scopes, [kind]);
const auditPage = usePagination(() => auditRows.value, [kind, filter]);
</script>
<template>
  <div class="page-heading">
    <div>
      <h1>
        <Icon
          :name="
            kind === 'agents'
              ? 'Bot'
              : kind === 'rules'
                ? 'ClipboardList'
                : kind === 'scopes'
                  ? 'ScanFace'
                  : 'ScrollText'
          "
          :size="24"
        />{{ headings[kind] }}
      </h1>
      <p>{{ sub[kind] }}</p>
    </div>
    <div class="actions">
      <button
        @click="
          store.refresh();
          detect();
        "
      >
        <Icon name="RefreshCw" :size="16" />Refresh</button
      ><button
        v-if="kind === 'agents'"
        class="primary"
        @click="showInstall = true"
      >
        <Icon name="Plus" :size="17" />Install hook</button
      ><button v-if="kind === 'rules'" class="primary" @click="newRule">
        <Icon name="Plus" :size="17" />New rule</button
      ><button v-if="kind === 'scopes'" class="primary" @click="editScope()">
        <Icon name="Plus" :size="17" />Add scope
      </button>
    </div>
  </div>
  <template v-if="kind === 'agents'"
    ><RemoteClients />
    <section class="panel connection-card">
      <div class="agent-emblem"><Icon name="Bot" :size="32" /></div>
      <div class="agent-summary">
        <div class="inline">
          <h2>{{ currentAgent?.name || "Agent" }}</h2>
          <span
            class="badge"
            :class="currentAgent?.found ? 'approve' : 'pending'"
            >{{ currentAgent?.found ? "Detected" : "Command not found" }}</span
          >
        </div>
        <p class="muted">
          {{ currentAgent?.version || "Version unknown" }} ·
          {{ currentAgent?.platform || "This machine" }} ·
          {{
            currentAgent?.integration === "command"
              ? "Command hook"
              : currentAgent?.integration === "plugin"
                ? "Plugin"
                : "Extension"
          }}
        </p>
        <code v-if="currentAgent?.path">{{ currentAgent.path }}</code>
      </div>
      <AppSelect
        v-model="selectedAgent"
        label="Select Agent"
        :options="agentOptions"
      />
    </section>
    <div class="notice">
      <Icon name="Info" />
      <div>
        <p>{{ currentAgent?.instructions }}</p>
        <p class="muted tiny">{{ currentAgent?.limitations }}</p>
      </div>
    </div>
    <section class="panel">
      <div class="panel-heading">
        <strong>Install scope</strong
        ><span class="muted"
          >Current user or a specific project · past integration verification is not cleared when a session ends</span
        >
      </div>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Agent / install scope</th>
              <th>Entry path</th>
              <th>Install status</th>
              <th>Integration check</th>
              <th>Version</th>
              <th>Action</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="i in installationPage.rows" :key="i.id">
              <td>
                <small>{{ agentName(i.agent) }}</small
                ><strong>{{
                  i.scope === "global" ? "Current user (global)" : "Specific project"
                }}</strong
                ><small>{{ i.project || "Current user config directory" }}</small>
              </td>
              <td class="path-text mono">{{ i.entry }}</td>
              <td>
                <span
                  class="badge"
                  :class="
                    i.configStatus === 'configured'
                      ? 'approve'
                      : i.configStatus === 'entry_error'
                        ? 'reject'
                        : ''
                  "
                  >{{ configStatuses[i.configStatus] }}</span
                >
                <small v-if="i.status === 'waiting_reload'"
                  >Existing sessions still need to reload</small
                >
              </td>
              <td>
                <span class="badge" :class="i.observed ? 'approve' : ''">
                  {{ i.observed ? "Events received" : "No events yet" }}
                </span>
                <small>Historical record; does not mean the session is online</small>
              </td>
              <td>{{ i.version }}</td>
              <td>
                <button
                  v-if="i.installed"
                  class="text-button reject"
                  @click="removeId = i.id"
                >
                  Uninstall</button
                ><span v-else class="muted">Entry removed</span>
              </td>
            </tr>
            <tr v-if="!store.installations.length">
              <td colspan="6">
                <div class="empty">
                  <Icon name="Plug" :size="32" /><strong
                    >AegisHook is not installed yet</strong
                  >
                  <p>Choose global or project scope to enable tool call review for the selected Agent.</p>
                  <button @click="showInstall = true">Install hook</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <Pagination
        v-model:page="installationPage.page"
        v-model:page-size="installationPage.pageSize"
        :total="installationPage.total"
        label="Install scope"
      />
    </section>
    <section class="panel section-gap">
      <div class="panel-heading">
        <strong>Live session status</strong
        ><span class="muted"
          >Pi: 10 s heartbeat / 30 s lost contact; other Agents: by event records</span
        >
      </div>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Session</th>
              <th>Working directory</th>
              <th>Connection</th>
              <th>Last heartbeat / event</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="i in instancePage.rows" :key="i.id">
              <td>
                <strong>{{ agentName(i.agent) }}</strong
                ><small class="mono">{{ i.sessionId.slice(0, 16) }}</small>
              </td>
              <td class="path-text">
                {{ i.cwd }}<small>{{ deviceLabel(i.nodeId) }}</small>
              </td>
              <td>
                <span class="badge" :class="instanceStatusClass(i)">{{
                  connectionLabel(i)
                }}</span>
              </td>
              <td>{{ date(i.heartbeat) }}</td>
            </tr>
            <tr v-if="!store.instances.length">
              <td colspan="4" class="empty-cell">
                No Agent session events received yet. Installed files do not mean the session has loaded them.
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <Pagination
        v-model:page="instancePage.page"
        v-model:page-size="instancePage.pageSize"
        :total="instancePage.total"
        label="Session status"
      />
    </section>
    <p class="muted tiny">
      Review scope is based on the tool events the client provides; script internals are not intercepted item by item. Event access means the session has reported events; recently active means
      an event or review heartbeat was received within 30
      seconds, which does not mean it is continuously online. Integration verification for an install scope keeps historical evidence, and a session ending, lost contact or a service restart does not clear it.
    </p></template
  >
  <template v-if="kind === 'rules'">
    <RulesTable
      :rules="store.rules"
      @edit="ruleForm = JSON.parse(JSON.stringify($event))"
    />
    <section class="panel form-panel section-gap">
      <h2>Rule dry run</h2>
      <p class="muted">
        Checks deterministic rules only; no tool is run and no model is called. Use the actual tool name and arguments from a call record; tool name restrictions are case-sensitive (for example
        Bash vs bash), and argument paths must match the fields reported by that
        Agent. The dry run does not include project authorization scope checks.
      </p>
      <div class="form-two">
        <label>Tool name<input v-model="testTool" /></label
        ><label
          >Arguments JSON<textarea v-model="testArgs" class="mono" rows="3" />
        </label>
      </div>
      <button @click="tryRule">
        <Icon name="FlaskConical" :size="16" />Run dry run
      </button>
      <div v-if="testResult" class="notice">
        <span class="badge" :class="testResult.decision">{{
          testResult.decision
        }}</span>
        <p>{{ testResult.comment }}</p>
      </div>
    </section></template
  >
  <template v-if="kind === 'scopes'"
    ><div class="notice">
      <Icon name="Info" />
      <p>
        Once configured, the path, url, host and target fields of recognizable tools in linked projects are checked against the scope.
        An empty list adds no restriction for that dimension; this is not a network sandbox.
      </p>
    </div>
    <section class="panel">
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Name / project</th>
              <th>Allowed targets</th>
              <th>Allowed paths</th>
              <th>Action</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in scopePage.rows" :key="s.id">
              <td>
                <strong>{{ s.name }}</strong
                ><small>{{ deviceLabel(s.nodeId) }} · {{ s.project }}</small>
              </td>
              <td>{{ s.targets.join(", ") || "No extra restriction" }}</td>
              <td class="path-text">
                {{ s.paths.join(", ") || "No extra restriction" }}
              </td>
              <td>
                <div class="actions">
                  <button class="text-button" @click="editScope(s)">Edit</button
                  ><button
                    class="text-button reject"
                    @click="
                      store.action(
                        () => api('/scopes/' + s.id, 'DELETE'),
                        'Scope deleted',
                      )
                    "
                  >
                    Delete
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="!store.scopes.length">
              <td colspan="4">
                <div class="empty">
                  <Icon name="ScanFace" :size="32" /><strong
                    >No authorization scope added yet</strong
                  >
                  <p>Configure exact target hosts and allowed paths for a project.</p>
                  <button @click="editScope()">Add scope</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <Pagination
        v-model:page="scopePage.page"
        v-model:page-size="scopePage.pageSize"
        :total="scopePage.total"
        label="Authorization scope"
      /></section
  ></template>
  <template v-if="kind === 'audit'"
    ><div class="audit-search search-input">
      <Icon name="Search" :size="16" /><input
        v-model="filter"
        placeholder="Search action, object or details"
      />
    </div>
    <section class="panel">
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Time</th>
              <th>Action</th>
              <th>Object</th>
              <th>Details</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="a in auditPage.rows" :key="a.id">
              <td class="nowrap muted">{{ date(a.createdAt) }}</td>
              <td>
                <code>{{ a.action }}</code>
              </td>
              <td class="mono muted">{{ a.subject.slice(0, 16) }}</td>
              <td class="audit-detail">{{ a.detail }}</td>
            </tr>
            <tr v-if="!auditRows.length">
              <td colspan="4" class="empty-cell">No matching audit records</td>
            </tr>
          </tbody>
        </table>
      </div>
      <Pagination
        v-model:page="auditPage.page"
        v-model:page-size="auditPage.pageSize"
        :total="auditPage.total"
        label="Audit log"
      /></section
  ></template>
  <div
    v-if="showInstall"
    class="modal-backdrop"
    @click.self="showInstall = false"
  >
    <form class="modal" @submit.prevent="install">
      <div class="panel-heading">
        <h2>Install {{ currentAgent?.name || "Agent" }} hook</h2>
        <button
          type="button"
          class="icon-button"
          @click="showInstall = false"
          aria-label="Close"
        >
          <Icon name="X" />
        </button>
      </div>
      <label
        >Agent<AppSelect
          v-model="selectedAgent"
          label="Agent to install"
          :options="agentOptions"
      /></label>
      <p class="muted tiny">{{ currentAgent?.instructions }}</p>
      <label
        >Install scope<AppSelect
          v-model="installScope"
          label="Install scope"
          :options="[
            { value: 'global', label: 'Current user (global)' },
            { value: 'project', label: 'Specific project' },
          ]" /></label
      ><label v-if="installScope === 'project'"
        >Absolute project path<input
          v-model="project"
          required
          placeholder="/absolute/path/to/project"
      /></label>
      <p class="muted">
        Only creates AegisHook's own entry; other extensions and project trust settings are not modified.
      </p>
      <div class="actions end">
        <button type="button" @click="showInstall = false">Cancel</button
        ><button class="primary" :disabled="busy">Install hook</button>
      </div>
    </form>
  </div>
  <div v-if="removeId" class="modal-backdrop" @click.self="removeId = ''">
    <div class="modal">
      <h2>Uninstall this install entry</h2>
      <p>
        Other install scopes stay in effect. The client may cache the loaded
        hook, so reload or restart the client after removal; existing connection settings are kept.
      </p>
      <div class="actions end">
        <button @click="removeId = ''">Cancel</button
        ><button class="danger-button" @click="uninstall">Remove entry</button>
      </div>
    </div>
  </div>
  <div v-if="ruleForm" class="modal-backdrop" @click.self="ruleForm = null">
    <form class="modal" @submit.prevent="saveRule">
      <h2>{{ ruleForm.builtin ? "Edit built-in rule" : "Custom rule" }}</h2>
      <p class="muted tiny">
        Takes effect for new calls after saving. Calls that match no rule go to the current review mode; the AI
        prompt is managed separately in System settings.
      </p>
      <div class="form-two">
        <label
          >Rule ID<input
            v-model="ruleForm.id"
            required
            :readonly="ruleForm.builtin"
            :pattern="
              ruleForm.builtin ? undefined : 'U_[A-Za-z0-9_-]+'
            " /></label
        ><label>Name<input v-model="ruleForm.name" required /></label>
      </div>
      <div class="form-two">
        <label
          >Decision<AppSelect
            v-model="ruleForm.decision"
            label="Decision"
            :options="[
              { value: 'reject', label: 'Reject' },
              { value: 'approve', label: 'Allow' },
            ]" /></label
        ><label
          >Tool name<input v-model="ruleForm.tool" placeholder="Leave blank to match all"
        /></label>
      </div>
      <div class="form-two">
        <label
          >Match type<AppSelect
            v-model="ruleForm.matcher"
            label="Match type"
            :options="[
              ...(ruleForm.builtin
                ? [{ value: 'semantic', label: 'Built-in semantic matching' }]
                : []),
              { value: 'regex', label: 'Regular expression' },
              { value: 'contains', label: 'Contains text' },
            ]"
        /></label>
        <label
          >Priority<input
            type="number"
            step="1"
            v-model.number="ruleForm.priority"
            required
        /></label>
      </div>
      <div v-if="ruleForm.matcher === 'semantic'">
        <RuleSemantics
          v-if="formSemantics"
          :semantics="formSemantics"
          :tool="ruleForm.tool"
        />
        <p class="muted tiny">
          To replace the detection condition, switch to a regular expression or contains text; the extra note only affects the returned message.
        </p>
      </div>
      <template v-else>
        <label
          >Match target<AppSelect
            v-model="ruleForm.target"
            label="Match target"
            :options="[
              { value: 'arguments', label: 'Tool arguments' },
              { value: 'toolName', label: 'Tool name' },
            ]"
        /></label>
        <label v-if="ruleForm.target === 'arguments'"
          >Argument path<input
            v-model="ruleForm.field"
            placeholder="For example command or request.method; leave blank to match the full arguments"
        /></label>
        <label
          >{{
            ruleForm.matcher === "regex"
              ? "Match expression (Go regex)"
              : "Match text (case-sensitive)"
          }}<textarea v-model="ruleForm.pattern" required rows="3" />
        </label>
      </template>
      <label
        >Extra decision note<textarea
          v-model="ruleForm.message"
          rows="2"
          maxlength="4000"
          placeholder="Note returned to the Agent on a match; may be left blank"
        />
      </label>
      <div class="inline">
        <RuleSwitch v-model="ruleForm.enabled" label="Enable this rule" /><span
          >Enable rule</span
        >
      </div>
      <div class="actions end">
        <button v-if="ruleForm.builtin" type="button" @click="restoreRule">
          Restore default
        </button>
        <button type="button" @click="ruleForm = null">Cancel</button
        ><button class="primary">Save rule</button>
      </div>
    </form>
  </div>
  <div v-if="scopeForm" class="modal-backdrop" @click.self="scopeForm = null">
    <form class="modal" @submit.prevent="saveScope">
      <h2>Authorization scope</h2>
      <label
        >Device<AppSelect
          :model-value="scopeForm.nodeId || ''"
          @update:model-value="scopeForm.nodeId = $event"
          label="Device"
          :options="[
            { value: '', label: 'Console machine' },
            ...store.clientNodes
              .filter((n) => !n.revoked)
              .map((n) => ({
                value: n.id,
                label: `${n.name || n.id} (${n.platform})`,
              })),
          ]"
      /></label>
      <label>Name<input v-model="scopeForm.name" required /></label
      ><label>Absolute project path<input v-model="scopeForm.project" required /></label
      ><label
        >Allowed targets (one exact hostname or IP per line)<textarea
          v-model="targets"
          rows="3"
          placeholder="staging.example.test"
        /></label
      ><label
        >Allowed file paths (one absolute path per line)<textarea v-model="paths" rows="3" />
      </label>
      <div class="actions end">
        <button type="button" @click="scopeForm = null">Cancel</button
        ><button class="primary">Save scope</button>
      </div>
    </form>
  </div>
</template>
