const state = {
  csrf: "",
  activeTab: "status",
  eventSource: null
};

const titles = {
  status: "环境",
  issue: "申请",
  install: "安装",
  uninstall: "卸载",
  certs: "证书",
  danger: "注销/删除",
  jobs: "任务"
};

document.addEventListener("DOMContentLoaded", async () => {
  bindNavigation();
  bindForms();
  const session = await api("/api/session", { allowUnauthenticated: true });
  if (session.authenticated) {
    state.csrf = session.csrf;
    showApp();
    await refreshStatus();
  } else {
    showLogin();
  }
});

function bindNavigation() {
  document.querySelectorAll("nav button[data-tab]").forEach((button) => {
    button.addEventListener("click", () => showTab(button.dataset.tab));
  });
  document.getElementById("refreshBtn").addEventListener("click", () => {
    if (state.activeTab === "status") refreshStatus();
    if (state.activeTab === "certs") loadCerts();
    if (state.activeTab === "jobs") loadJobs();
  });
  document.getElementById("logoutBtn").addEventListener("click", async () => {
    await api("/api/logout", { method: "POST" });
    showLogin();
  });
}

function bindForms() {
  document.getElementById("loginForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    const masterKey = document.getElementById("masterKey").value;
    try {
      const result = await api("/api/login", {
        method: "POST",
        body: { masterKey },
        allowUnauthenticated: true
      });
      state.csrf = result.csrf;
      document.getElementById("loginError").textContent = "";
      showApp();
      await refreshStatus();
    } catch (error) {
      document.getElementById("loginError").textContent = error.message;
    }
  });

  document.getElementById("issueForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const body = {
      domains: String(form.get("domains") || "").split(/\s|,/).filter(Boolean),
      cfMode: form.get("cfMode"),
      cfToken: form.get("cfToken"),
      cfAccountId: form.get("cfAccountId"),
      cfZoneId: form.get("cfZoneId"),
      cfKey: form.get("cfKey"),
      cfEmail: form.get("cfEmail"),
      ca: form.get("ca"),
      keyLength: form.get("keyLength"),
      dnsSleep: numberValue(form.get("dnsSleep")),
      force: form.has("force"),
      staging: form.has("staging"),
      debug: form.has("debug")
    };
    const job = await api("/api/issue", { method: "POST", body });
    openJob(job.id);
  });

  document.getElementById("installForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const body = {
      domain: form.get("domain"),
      ecc: form.get("ecc") === "true",
      server: form.get("server"),
      keyFile: form.get("keyFile"),
      fullchainFile: form.get("fullchainFile"),
      workDir: form.get("workDir"),
      pemFile: form.get("pemFile"),
      reloadService: form.has("reloadService")
    };
    const job = await api("/api/install", { method: "POST", body });
    openJob(job.id);
  });

  document.getElementById("installAcmeForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    if (!confirm("将使用官方 https://get.acme.sh 脚本安装 acme.sh，并可能写入 ~/.acme.sh、shell alias 和 cron。确认继续？")) {
      return;
    }
    const form = new FormData(event.currentTarget);
    const body = {
      email: form.get("email"),
      confirm: true
    };
    const job = await api("/api/acme/install", { method: "POST", body });
    openJob(job.id);
  });

  document.getElementById("uninstallForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const body = {
      paths: String(form.get("paths") || "").split(/\n|,/).filter(Boolean),
      service: form.get("service"),
      reloadService: form.has("reloadService"),
      confirm: form.get("confirm")
    };
    const job = await api("/api/uninstall", { method: "POST", body });
    openJob(job.id);
  });

  document.getElementById("loadCertsBtn").addEventListener("click", loadCerts);

  document.getElementById("infoForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const query = new URLSearchParams({
      domain: form.get("domain"),
      ecc: form.has("ecc") ? "1" : "0"
    });
    const result = await api(`/api/cert/info?${query.toString()}`);
    document.getElementById("infoOutput").textContent = result.output || result.error || "";
  });

  document.getElementById("dangerForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const domain = String(form.get("domain") || "").trim();
    const action = form.get("action");
    const confirm = String(form.get("confirm") || "").trim();
    if ((action === "revoke" || action === "remove") && confirm !== domain) {
      alert("危险操作需要在确认文本中输入完整域名");
      return;
    }
    const body = {
      domain,
      ecc: form.get("ecc") === "true",
      force: action === "renew"
    };
    const job = await api(`/api/${action}`, { method: "POST", body });
    openJob(job.id);
  });
}

async function refreshStatus() {
  const status = await api("/api/status");
  document.getElementById("versionText").textContent = `${status.version} ${status.commit}`;
  const rows = [
    ["监听", `${status.bind}:${status.port}`],
    ["当前用户", status.user?.username || ""],
    ["HOME", status.home || ""],
    ["acme.sh", status.acme?.found ? status.acme.path : `未找到 ${status.acme?.error || ""}`],
    ["acme home", status.acme?.home || ""],
    ["acme version", status.acmeVersion || status.acmeVersionErr || ""]
  ];
  document.getElementById("installAcmeForm").hidden = Boolean(status.acme?.found);
  const grid = document.getElementById("statusGrid");
  grid.innerHTML = "";
  rows.forEach(([key, value]) => {
    const dt = document.createElement("dt");
    const dd = document.createElement("dd");
    dt.textContent = key;
    dd.textContent = value;
    grid.append(dt, dd);
  });

  const toolsBody = document.getElementById("toolsBody");
  toolsBody.innerHTML = "";
  Object.keys(status.tools || {}).sort().forEach((name) => {
    const tr = document.createElement("tr");
    const path = status.tools[name];
    tr.innerHTML = `<td>${escapeHTML(name)}</td><td>${path ? "可用" : "未找到"}</td><td>${escapeHTML(path || "")}</td>`;
    toolsBody.appendChild(tr);
  });
}

async function loadCerts() {
  const result = await api("/api/certs");
  document.getElementById("certsOutput").textContent = result.output || result.error || "";
}

async function loadJobs() {
  const result = await api("/api/jobs");
  const list = document.getElementById("jobsList");
  list.innerHTML = "";
  (result.jobs || []).forEach((job) => {
    const button = document.createElement("button");
    button.textContent = `${job.kind} ${job.status}`;
    button.className = `status-${job.status}`;
    button.addEventListener("click", () => openJob(job.id));
    list.appendChild(button);
  });
}

async function openJob(id) {
  showTab("jobs");
  await loadJobs();
  const snapshot = await api(`/api/jobs/${id}`);
  renderJob(snapshot);
  if (state.eventSource) state.eventSource.close();
  state.eventSource = new EventSource(`/api/jobs/${id}/events`);
  state.eventSource.addEventListener("log", (event) => {
    const payload = JSON.parse(event.data);
    appendLog(payload.log || payload);
  });
  state.eventSource.addEventListener("state", (event) => {
    const payload = JSON.parse(event.data);
    renderJob(payload.job || payload);
    if ((payload.job || payload).status !== "running") {
      state.eventSource.close();
      loadJobs();
    }
  });
}

function renderJob(job) {
  const log = document.getElementById("jobLog");
  const header = [
    `job: ${job.id}`,
    `kind: ${job.kind}`,
    `status: ${job.status}`,
    `command: ${job.command}`,
    job.error ? `error: ${job.error}` : ""
  ].filter(Boolean).join("\n");
  const lines = (job.logs || []).map((entry) => `[${entry.stream}] ${entry.text}`).join("\n");
  log.textContent = `${header}\n\n${lines}`;
}

function appendLog(entry) {
  const log = document.getElementById("jobLog");
  log.textContent += `\n[${entry.stream}] ${entry.text}`;
  log.scrollTop = log.scrollHeight;
}

function showLogin() {
  document.getElementById("app").hidden = true;
  document.getElementById("login").hidden = false;
}

function showApp() {
  document.getElementById("login").hidden = true;
  document.getElementById("app").hidden = false;
  showTab(state.activeTab);
}

function showTab(tab) {
  state.activeTab = tab;
  document.getElementById("pageTitle").textContent = titles[tab] || tab;
  document.querySelectorAll("nav button[data-tab]").forEach((button) => {
    button.classList.toggle("active", button.dataset.tab === tab);
  });
  document.querySelectorAll(".tab").forEach((panel) => {
    panel.classList.toggle("active", panel.id === `tab-${tab}`);
  });
  if (tab === "jobs") loadJobs();
}

async function api(url, options = {}) {
  const init = {
    method: options.method || "GET",
    headers: {}
  };
  if (options.body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(options.body);
  }
  if (state.csrf && init.method !== "GET") {
    init.headers["X-CSRF-Token"] = state.csrf;
  }
  const response = await fetch(url, init);
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    if (!options.allowUnauthenticated && response.status === 401) showLogin();
    throw new Error(data.error || response.statusText);
  }
  return data;
}

function numberValue(value) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function escapeHTML(value) {
  return String(value).replace(/[&<>"']/g, (ch) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    "\"": "&quot;",
    "'": "&#39;"
  }[ch]));
}
