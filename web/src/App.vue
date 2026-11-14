<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useConsole } from "./store";
import Icon from "./components/Icon.vue";
import BrandLogo from "./components/BrandLogo.vue";
const store = useConsole(),
  router = useRouter();
const token = ref(""),
  loginError = ref(""),
  logging = ref(false),
  collapsed = ref(false),
  search = ref("");
const theme = ref(localStorage.getItem("aegis-theme") || "light");
function applyTheme() {
  document.documentElement.dataset.theme = theme.value;
  localStorage.setItem("aegis-theme", theme.value);
}
function toggleTheme() {
  theme.value = theme.value === "light" ? "dark" : "light";
  applyTheme();
}
const groups = [
  {
    name: "Workspace",
    items: [
      ["/overview", "Overview", "LayoutDashboard"],
      ["/sessions", "Sessions", "MessagesSquare"],
      ["/calls", "Tool calls", "Workflow"],
    ],
  },
  {
    name: "Security",
    items: [
      ["/rules", "Policy rules", "ClipboardList"],
      ["/approvals", "Approvals & blocks", "ShieldCheck"],
      ["/scopes", "Authorization scope", "ScanFace"],
      ["/audit", "Audit log", "ScrollText"],
    ],
  },
  {
    name: "System",
    items: [
      ["/agents", "Agent integration", "Bot"],
      ["/settings", "System settings", "Settings"],
    ],
  },
];
async function login() {
  logging.value = true;
  try {
    await store.login(token.value);
    token.value = "";
    loginError.value = "";
  } catch (e) {
    loginError.value = (e as Error).message;
  } finally {
    logging.value = false;
  }
}
function doSearch() {
  router.push({ path: "/calls", query: { q: search.value } });
}
onMounted(() => {
  applyTheme();
  store.init();
});
</script>
<template>
  <div v-if="!store.ready" class="loading-page">
    <BrandLogo :size="40" />
    <p>Connecting to the local console…</p>
  </div>
  <div v-else-if="!store.authenticated" class="login-page">
    <form class="login-panel" @submit.prevent="login">
      <div class="brand large">
        <BrandLogo :size="38" /><span
          >AegisHook<small>Agent tool call review console</small></span
        >
      </div>
      <h1>Connect to your local console</h1>
      <p class="muted">
        Sign in with the admin token in the <code>admin.token</code> file shown in the startup terminal.
      </p>
      <label
        >Admin token<input
          v-model="token"
          type="password"
          autocomplete="off"
          required
          placeholder="Enter the local admin token"
          aria-label="Admin token"
      /></label>
      <p v-if="loginError" class="form-error">{{ loginError }}</p>
      <button class="primary wide" :disabled="logging">
        {{ logging ? "Connecting…" : "Open console" }}<Icon name="ArrowRight" />
      </button>
      <p class="login-note">
        <Icon name="LockKeyhole" :size="14" />Local connection only ·
        the token is never sent to external services
      </p>
    </form>
  </div>
  <div v-else class="app-shell" :class="{ collapsed }">
    <aside class="sidebar">
      <router-link to="/overview" class="brand"
        ><BrandLogo :size="34" /><span
          >AegisHook</span
        ></router-link
      >
      <nav>
        <div v-for="group in groups" :key="group.name" class="nav-group">
          <div class="nav-label">{{ group.name }}</div>
          <router-link
            v-for="item in group.items"
            :key="item[0]"
            :to="item[0]"
            :aria-label="item[1]"
            :title="item[1]"
            class="nav-item"
            ><Icon :name="item[2]" /><span>{{ item[1] }}</span
            ><b
              v-if="item[0] === '/approvals' && store.pending.length"
              class="count-bubble"
              >{{ store.pending.length }}</b
            ></router-link
          >
        </div>
      </nav>
      <div class="sidebar-foot">
        <router-link to="/agents" class="health"
          ><i class="dot" :class="{ green: store.streamOnline && store.online > 0, event: store.streamOnline && !store.online && store.recentEvents > 0 }" /><span>{{
            !store.streamOnline
              ? "Review service connection failed"
              : store.online
                ? "Online " + store.online + " · event access " + store.eventConnections
                : store.eventConnections
                  ? "Event access " + store.eventConnections + " · recently active " + store.recentEvents
                  : "No session connections"
          }}</span></router-link
        ><button class="profile" @click="store.logout">
          <span class="avatar">A</span><span>Admin</span
          ><Icon name="LogOut" :size="16" />
        </button>
      </div>
    </aside>
    <div class="workspace">
      <header class="topbar">
        <button
          class="icon-button"
          :aria-label="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
          @click="collapsed = !collapsed"
        >
          <Icon name="PanelLeft" />
        </button>
        <form class="global-search" @submit.prevent="doSearch">
          <Icon name="Search" :size="18" />
          <input v-model="search" placeholder="Search" aria-label="Global search" />
          <kbd>↵</kbd>
        </form>
        <div class="topbar-actions">
          <router-link
            to="/settings"
            class="icon-button chrome-button"
            aria-label="Open system settings"
            ><Icon name="Settings" :size="19"
          /></router-link>
          <button
            class="icon-button chrome-button"
            @click="toggleTheme"
            :aria-label="theme === 'light' ? 'Switch to dark theme' : 'Switch to light theme'"
          >
            <Icon :name="theme === 'light' ? 'Moon' : 'Sun'" :size="19" />
          </button>
        </div>
      </header>
      <main>
        <div v-if="store.error" role="alert" class="alert danger">
          <Icon name="CircleAlert" />{{ store.error
          }}<button class="text-button" @click="store.error = ''">Dismiss</button>
        </div>
        <router-view />
      </main>
    </div>
    <div v-if="store.toast" class="toast" role="status">
      <Icon name="CircleCheck" />{{ store.toast }}
    </div>
  </div>
</template>
