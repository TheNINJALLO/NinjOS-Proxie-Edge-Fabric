"use strict";

const $ = (selector) => document.querySelector(selector);
const $$ = (selector) => [...document.querySelectorAll(selector)];
const state = {
  token: sessionStorage.getItem("ninjos_dashboard_token") || "",
  data: null,
  registry: null,
  settings: [],
  secrets: [],
  companionManager: null,
  selectedSecret: null,
  users: [],
  selectedUser: null,
  packetsPaused: false,
  traffic: [],
  pollTimers: [],
  loginInFlight: false,
  setupRequired: false,
  recoverySession: false,
};

const views = {
  overview: "Network Pulse",
  backends: "Backends & Sessions",
  players: "Network Players",
  packets: "Packet Inspector",
  policy: "Firewall & Policy",
  transfers: "Transfer Broker",
  events: "Events & Discord",
  audit: "Audit Log",
  team: "Team & Access",
  configuration: "Configuration & Secrets",
  controls: "Controls",
};

const roleRank = { viewer: 10, moderator: 20, operator: 30, admin: 40, owner: 50 };

function updateNavigation(role = "viewer") {
  const rank = roleRank[role] || 0;
  $$("#nav button").forEach((button) => {
    const required = button.dataset.minRole;
    button.hidden = button.dataset.view === "team"
      ? role !== "owner"
      : Boolean(required && rank < (roleRank[required] || 0));
  });
  const active = $("#nav button.active");
  if (active?.hidden) $("#nav button[data-view='overview']")?.click();
}

function fmtBytes(value = 0) {
  const n = Number(value) || 0;
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let result = n;
  let index = -1;
  do {
    result /= 1024;
    index += 1;
  } while (result >= 1024 && index < units.length - 1);
  return `${result.toFixed(result >= 100 ? 0 : result >= 10 ? 1 : 2)} ${units[index]}`;
}
function fmtRate(value = 0) {
  return `${fmtBytes(value)}/s`;
}
function fmtDuration(seconds = 0) {
  const s = Math.max(0, Math.floor(Number(seconds) || 0));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`;
  return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
}
function fmtTime(timestamp) {
  const d = new Date(Number(timestamp) || timestamp);
  return Number.isNaN(d.valueOf()) ? "—" : d.toLocaleTimeString();
}
function esc(value) {
  return String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
}
function toast(message) {
  const node = $("#toast");
  node.textContent = message;
  node.classList.add("show");
  setTimeout(() => node.classList.remove("show"), 2300);
}

function stopPolling() {
  state.pollTimers.forEach((timer) => clearInterval(timer));
  state.pollTimers = [];
}

function startPolling() {
  stopPolling();
  if (!state.token) return;
  state.pollTimers = [
    setInterval(refreshState, 2000),
    setInterval(refreshPresence, 2500),
    setInterval(refreshPackets, 1500),
    setInterval(refreshPolicy, 3000),
    setInterval(refreshTransfers, 2000),
    setInterval(refreshEvents, 4000),
    setInterval(refreshAudit, 5000),
  ];
}

function openTokenDialog({ reset = false, focus = true } = {}) {
  if (state.setupRequired) {
    openSetupDialog();
    return;
  }
  if (reset && !state.loginInFlight) {
    $("#tokenInput").value = "";
    $("#totpInput").value = "";
  }
  const dialog = $("#tokenDialog");
  if (!dialog.open) dialog.showModal();
  if (focus) setTimeout(() => $("#tokenInput").focus(), 0);
}

function openSetupDialog() {
  const dialog = $("#setupDialog");
  if (!dialog.open) dialog.showModal();
  setTimeout(() => $("#setupCodeInput").focus(), 0);
}

async function initializeAuthentication() {
  try {
    const response = await fetch("/api/setup/status", { cache: "no-store" });
    if (!response.ok) throw new Error(await response.text());
    const setup = await response.json();
    state.setupRequired = Boolean(setup.setupRequired);
  } catch (error) {
    toast(`Unable to check dashboard setup: ${error.message}`);
  }
  if (state.setupRequired) {
    state.token = "";
    sessionStorage.removeItem("ninjos_dashboard_token");
    stopPolling();
    openSetupDialog();
    return;
  }
  if (!state.token) {
    openTokenDialog({ reset: true });
  } else {
    startPolling();
    refreshAll();
  }
}

function forgetDashboardSession({ resetLogin = false } = {}) {
  state.token = "";
  sessionStorage.removeItem("ninjos_dashboard_token");
  stopPolling();
  openTokenDialog({ reset: resetLogin });
}

async function api(path, options = {}) {
  const headers = { ...(options.headers || {}) };
  if (state.token) headers.Authorization = `Bearer ${state.token}`;
  if (options.body && !headers["Content-Type"])
    headers["Content-Type"] = "application/json";
  const response = await fetch(path, { ...options, headers });
  if (response.status === 401) {
    $("#connectionState").textContent = "Sign-in required";
    $("#connectionDot").className = "dot offline";
    // Preserve anything currently being typed. A stale background request must
    // never erase the password or TOTP field.
    forgetDashboardSession({ resetLogin: false });
    throw new Error("Unauthorized");
  }
  if (!response.ok) {
    const text = await response.text();
    let message = text || `HTTP ${response.status}`;
    try {
      const parsed = JSON.parse(text);
      if (parsed?.error) message = parsed.error;
    } catch (_) {}
    throw new Error(message);
  }
  return response.json();
}

$("#setupForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  const password = $("#setupPasswordInput").value;
  if (password !== $("#setupPasswordConfirmInput").value) {
    toast("The two passwords do not match.");
    return;
  }
  const button = $("#completeSetup");
  button.disabled = true;
  button.textContent = "Creating Owner...";
  try {
    const response = await fetch("/api/setup", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        setupCode: $("#setupCodeInput").value.trim(),
        username: $("#setupUsernameInput").value.trim(),
        password,
      }),
    });
    const text = await response.text();
    let data = {};
    try { data = JSON.parse(text); } catch (_) {}
    if (!response.ok) throw new Error(data.error || text || "Owner setup failed");
    state.setupRequired = false;
    state.token = data.token;
    state.recoverySession = false;
    sessionStorage.setItem("ninjos_dashboard_token", state.token);
    $("#setupDialog").close();
    $("#setupForm").reset();
    $("#usernameInput").value = data.principal.username;
    $("#accountUsername").value = data.principal.username;
    startPolling();
    toast(`Owner ${data.principal.username} created`);
    setTimeout(refreshAll, 50);
  } catch (error) {
    toast(error.message);
  } finally {
    button.disabled = false;
    button.textContent = "Create Owner & Sign In";
  }
});

$("#tokenButton").addEventListener("click", () =>
  openTokenDialog({ reset: true }),
);
$("#closeLogin").addEventListener("click", () => $("#tokenDialog").close());
$("#loginForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  if (state.loginInFlight) return;

  // Capture all credentials before awaiting the network. This prevents any
  // unrelated UI work from changing what is submitted.
  const credentials = {
    username: $("#usernameInput").value.trim(),
    password: $("#tokenInput").value,
    totp: $("#totpInput").value.trim(),
  };
  const submitButton = $("#saveToken");
  state.loginInFlight = true;
  submitButton.disabled = true;
  submitButton.textContent = "Signing In...";

  try {
    const response = await fetch("/api/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(credentials),
    });
    if (!response.ok) {
      const text = await response.text();
      let detail = {};
      try { detail = JSON.parse(text); } catch (_) {}
      if (response.status === 409 && detail.setupRequired) {
        state.setupRequired = true;
        $("#tokenDialog").close();
        openSetupDialog();
      }
      throw new Error(detail.error || text || "Login failed");
    }
    const data = await response.json();
    state.token = data.token;
    sessionStorage.setItem("ninjos_dashboard_token", state.token);
    $("#tokenDialog").close();
    $("#tokenInput").value = "";
    $("#totpInput").value = "";
    startPolling();
    state.recoverySession = Boolean(data.recovery);
    $("#accountUsername").value = data.principal.username === "recovery" ? "" : data.principal.username;
    toast(`Signed in as ${data.principal.username}`);
    setTimeout(refreshAll, 50);
  } catch (error) {
    // Leave the entered password and TOTP code in place so the user can correct
    // one field instead of retyping everything.
    toast(error.message);
  } finally {
    state.loginInFlight = false;
    submitButton.disabled = false;
    submitButton.textContent = "Sign In";
  }
});
$("#clearToken").addEventListener("click", async () => {
  try {
    if (state.token) {
      await fetch("/api/logout", {
        method: "POST",
        headers: { Authorization: `Bearer ${state.token}` },
      });
    }
  } catch (_) {}
  forgetDashboardSession({ resetLogin: true });
  toast("Dashboard session forgotten");
});

$$("#nav button").forEach((button) =>
  button.addEventListener("click", () => {
    const view = button.dataset.view;
    $$("#nav button").forEach((item) =>
      item.classList.toggle("active", item === button),
    );
    $$(".view").forEach((item) =>
      item.classList.toggle("active", item.id === `view-${view}`),
    );
    $("#pageTitle").textContent = views[view];
    if (view === "players") refreshPresence();
    if (view === "packets") refreshPackets();
    if (view === "policy") refreshPolicy();
    if (view === "events") refreshEvents();
    if (view === "audit") refreshAudit();
    if (view === "transfers") refreshTransfers();
    if (view === "backends") refreshBackendRegistry();
    if (view === "configuration") refreshUnifiedConfiguration();
    if (view === "team") refreshDashboardUsers();
  }),
);

function metric(label, value, sub = "") {
  return `<div class="metric"><div class="label">${esc(label)}</div><div class="value">${esc(value)}</div><div class="sub">${esc(sub)}</div></div>`;
}
function detail(label, value) {
  return `<div class="detail"><span>${esc(label)}</span><strong>${esc(value)}</strong></div>`;
}


function companionStatusClass(item) {
  if (item.health === "healthy") return "good";
  if (item.health === "degraded" || item.reportStatus === "stale") return "warn";
  return "bad";
}

function companionStatusLabel(item) {
  if (item.reportStatus === "never") return "Never reported";
  if (item.reportStatus === "orphaned") return item.connected ? "Unregistered companion" : "Orphaned report";
  if (item.reportStatus === "stale") return "Stale";
  if (item.health === "degraded") return "Degraded";
  return item.connected ? "Connected" : "Offline";
}

function summarizeCompanions(companions) {
  const configured = companions.filter((item) => item.configured !== false);
  const connected = configured.filter((item) => item.connected);
  const values = (key) => connected
    .map((item) => Number(item.metrics?.[key]))
    .filter((value) => Number.isFinite(value));
  const average = (items) => items.length
    ? items.reduce((sum, value) => sum + value, 0) / items.length
    : null;
  const tps = values("currentTps");
  const mspt = values("currentMspt");
  return {
    configured,
    connected,
    missing: configured.filter((item) => !item.connected),
    averageTps: average(tps),
    averageMspt: average(mspt),
    worstMspt: mspt.length ? Math.max(...mspt) : null,
    onlinePlayers: connected.reduce((sum, item) => sum + Number(item.metrics?.onlinePlayers || 0), 0),
    maxPlayers: connected.reduce((sum, item) => sum + Number(item.metrics?.maxPlayers || 0), 0),
    queueDepth: connected.reduce((sum, item) => sum + Number(item.metrics?.queueDepth || 0), 0),
    uploadFailures: connected.reduce((sum, item) => sum + Number(item.metrics?.uploadFailures || 0), 0),
  };
}

function renderEndstonePerformance(companions) {
  const summary = summarizeCompanions(companions);
  if ($("#endstoneSummary")) {
    $("#endstoneSummary").textContent =
      `${summary.connected.length}/${summary.configured.length} companions connected`;
  }

  const summaryCards = [
    metric(
      "Reporting Servers",
      `${summary.connected.length}/${summary.configured.length}`,
      `${summary.missing.length} unavailable`,
    ),
    metric(
      "Network TPS",
      summary.averageTps != null ? summary.averageTps.toFixed(2) : "—",
      "average across reporting servers",
    ),
    metric(
      "Network MSPT",
      summary.averageMspt != null ? `${summary.averageMspt.toFixed(2)} ms` : "—",
      summary.worstMspt != null ? `${summary.worstMspt.toFixed(2)} ms worst` : "no reports",
    ),
    metric(
      "Endstone Players",
      summary.maxPlayers ? `${summary.onlinePlayers}/${summary.maxPlayers}` : summary.onlinePlayers,
      `${summary.queueDepth} queued · ${summary.uploadFailures} upload failures`,
    ),
  ].join("");

  const serverCards = companions.map((item) => {
    const metrics = item.metrics || {};
    const age = item.ageSeconds == null
      ? "No report received"
      : item.ageSeconds < 2
        ? "Reporting now"
        : `Last report ${item.ageSeconds}s ago`;
    const gatewayState = item.gatewayEnabled === false
      ? "Gateway route disabled"
      : item.gatewayHealthy === true
        ? `Gateway healthy${item.gatewayLatencyMs != null ? ` · ${Number(item.gatewayLatencyMs).toFixed(1)} ms` : ""}`
        : item.gatewayHealthy === false
          ? "Gateway backend offline"
          : "Gateway state pending";
    const secretState = item.secretConfigured
      ? "Companion secret configured"
      : "Companion secret missing";
    return `<article class="endstone-server-card">
      <div class="endstone-server-head">
        <div><h3>${esc(item.displayName || item.serverId || "Unknown server")}</h3><code>${esc(item.serverId || "unknown")}</code></div>
        <span class="status ${companionStatusClass(item)}">${esc(companionStatusLabel(item))}</span>
      </div>
      <div class="endstone-primary-metrics">
        <div><span>TPS</span><strong>${metrics.currentTps != null ? Number(metrics.currentTps).toFixed(2) : "—"}</strong><small>${metrics.averageTps != null ? `${Number(metrics.averageTps).toFixed(2)} average` : "waiting for report"}</small></div>
        <div><span>MSPT</span><strong>${metrics.currentMspt != null ? `${Number(metrics.currentMspt).toFixed(2)} ms` : "—"}</strong><small>${metrics.tickUsage != null ? `${Number(metrics.tickUsage).toFixed(1)}% tick use` : "waiting for report"}</small></div>
        <div><span>Players</span><strong>${metrics.onlinePlayers != null ? `${metrics.onlinePlayers}/${metrics.maxPlayers ?? "?"}` : "—"}</strong><small>${item.activeSessions ?? 0} proxied sessions</small></div>
      </div>
      <div class="endstone-detail-grid">
        ${detail("BDS CPU", metrics.processCpuPercent != null ? `${Number(metrics.processCpuPercent).toFixed(1)}%` : "—")}
        ${detail("Process RAM", metrics.processMemoryBytes ? fmtBytes(metrics.processMemoryBytes) : "—")}
        ${detail("Container RAM", metrics.containerMemoryBytes ? fmtBytes(metrics.containerMemoryBytes) : "—")}
        ${detail("Queue", `${metrics.queueDepth ?? 0} · ${metrics.queueDrops ?? 0} dropped`)}
        ${detail("Upload Failures", metrics.uploadFailures ?? 0)}
        ${detail("Companion", metrics.companionVersion || "—")}
        ${detail("Minecraft", metrics.minecraftVersion || "—")}
        ${detail("Protocol", metrics.protocolVersion || "—")}
      </div>
      <div class="endstone-server-foot"><span>${esc(age)}</span><span>${esc(gatewayState)}</span><span>${esc(secretState)}</span></div>
    </article>`;
  }).join("");

  $("#endstoneMetrics").innerHTML = `${summaryCards}<div class="endstone-server-grid">${serverCards || '<div class="empty-state">No backend servers are configured.</div>'}</div>`;
}

function renderState(data) {
  state.data = data;
  const gateway = data.gateway || {};
  const counters = gateway.counters || {};
  const companions = data.companions || [];
  const companionSummary = summarizeCompanions(companions);
  const sys = data.system || {};
  const backends = gateway.backends || [];
  const healthy = backends.filter(
    (item) => item.healthy && item.enabled,
  ).length;
  $("#connectionDot").className = "dot online";
  $("#connectionState").textContent = "Connected";
  $("#lastRefresh").textContent = `Updated ${new Date().toLocaleTimeString()}`;
  $("#roleBadge").textContent =
    `${data.principal?.username || "unknown"} · ${data.principal?.role || "unknown"}`;
  updateNavigation(data.principal?.role);
  $("#ownerAccountPanel").style.display = data.principal?.role === "owner" ? "block" : "none";
  if (data.principal?.role === "owner" && data.principal?.username !== "recovery") {
    $("#accountUsername").value = data.principal.username;
  }
  const incident = gateway.incident || {};
  $("#modeBadge").textContent = gateway.maintenance
    ? "Maintenance"
    : gateway.drain
      ? "Drain Mode"
      : incident.active
        ? "Incident Mode"
        : `Online · v${data.version || "7.3.8"}`;
  $("#modeBadge").style.color =
    gateway.maintenance || gateway.drain || incident.active
      ? "var(--amber)"
      : "var(--green)";

  $("#metricCards").innerHTML = [
    metric(
      "Active Sessions",
      gateway.activeSessions || 0,
      `${gateway.trackedIps || 0} tracked IPs`,
    ),
    metric(
      "Network Players",
      data.presence?.online || 0,
      `${data.presence?.tracked || 0} recently tracked`,
    ),
    metric(
      "Backend Health",
      `${healthy}/${backends.length}`,
      gateway.routingMode || "routing",
    ),
    metric(
      "Network TPS",
      companionSummary.averageTps != null
        ? companionSummary.averageTps.toFixed(2)
        : "—",
      `${companionSummary.connected.length}/${companionSummary.configured.length} companions reporting`,
    ),
    metric(
      "Network MSPT",
      companionSummary.averageMspt != null
        ? `${companionSummary.averageMspt.toFixed(2)} ms`
        : "—",
      companionSummary.worstMspt != null
        ? `${companionSummary.worstMspt.toFixed(2)} ms worst server`
        : "no Endstone reports",
    ),
    metric(
      "Dropped Packets",
      counters.droppedPackets || 0,
      `${counters.rateLimited || 0} rate-limited`,
    ),
    metric(
      "Container Memory",
      fmtBytes(sys.memoryBytes || 0),
      sys.memoryLimitBytes
        ? `${((sys.memoryBytes / sys.memoryLimitBytes) * 100).toFixed(1)}% used`
        : "cgroup",
    ),
  ].join("");

  const warnings = [];
  if (data.security?.companionSecretDefault)
    warnings.push(
      "Change COMPANION_SHARED_SECRET before installing the Endstone companion.",
    );
  if (companionSummary.configured.length === 0) {
    warnings.push("No Endstone backends are configured for performance reporting.");
  } else if (companionSummary.connected.length === 0) {
    warnings.push(
      `None of the ${companionSummary.configured.length} configured Endstone companions are reporting. Transport protection remains active, but gameplay packets and TPS metrics are unavailable.`,
    );
  } else if (companionSummary.missing.length > 0) {
    warnings.push(
      `${companionSummary.missing.length} of ${companionSummary.configured.length} Endstone companions are not reporting: ${companionSummary.missing.map((item) => item.displayName || item.serverId).join(", ")}.`,
    );
  }
  $("#securityWarnings").innerHTML = warnings
    .map((item) => `<div class="warning">${esc(item)}</div>`)
    .join("");

  $("#backendSummary").textContent = `${healthy} healthy of ${backends.length}`;
  $("#backendMini").innerHTML =
    backends
      .map(
        (item) =>
          `<div class="backend-row"><b>${esc(item.name)}</b><span>${Number(item.latencyMs || 0).toFixed(1)} ms · ${item.activeSessions || 0} sessions</span><span class="status ${item.healthy && item.enabled ? "good" : "bad"}">${item.enabled ? (item.healthy ? "Healthy" : "Offline") : "Disabled"}</span></div>`,
      )
      .join("") || "<p>No backends configured.</p>";

  renderEndstonePerformance(companions);

  $("#firewallMetrics").innerHTML = [
    detail("Temporary Bans", counters.temporaryBans || 0),
    detail("Adaptive Warnings", counters.adaptiveWarnings || 0),
    detail("Active Bans", gateway.firewall?.activeBans || 0),
    detail("Denylist Drops", counters.denylistDrops || 0),
    detail("Rate Limited", counters.rateLimited || 0),
    detail("Cached Pings", counters.cachedPingReplies || 0),
    detail("Health Failures", counters.healthFailures || 0),
    detail("Incident Mode", incident.active ? "ACTIVE" : "Normal"),
    detail("Incident PPS", incident.packetsPerSecond || 0),
    detail(
      "Incident Drop Ratio",
      `${(Number(incident.dropRatio || 0) * 100).toFixed(2)}%`,
    ),
    detail("Sessions Opened", counters.sessionsOpened || 0),
    detail("Sessions Closed", counters.sessionsClosed || 0),
  ].join("");

  renderBackends(data);
  renderSystem(sys);
  updateTraffic(gateway);
  renderDiscord(data.discord || {});
  renderCompanionFleet(data.companions || []);
  renderPolicyState(gateway);
  renderTransferBroker(data.transferBroker || gateway.transferBroker || {});
}

function renderBackends(data) {
  const gateway = data.gateway || {};
  const backends = gateway.backends || [];
  $("#backendSelect").innerHTML = backends
    .map(
      (item) => `<option value="${esc(item.name)}">${esc(item.name)}</option>`,
    )
    .join("");
  $("#transferDestination").innerHTML = backends
    .map(
      (item) =>
        `<option value="${esc(item.name)}">${esc(item.name)}${item.healthy && item.enabled ? "" : " (unavailable)"}</option>`,
    )
    .join("");
  const routes = gateway.staticRoutes || [];
  const publicHost = window.location.hostname || "proxy-host";
  $("#staticRoutesTable").innerHTML =
    routes
      .map(
        (route) =>
          `<tr><td><b>:${route.listenerPort}</b></td><td>${esc(route.backend)}</td><td><span class="status ${route.available ? "good" : "bad"}">${route.available ? "Ready" : "Unavailable"}</span></td><td>${esc(publicHost)}:${route.listenerPort}</td></tr>`,
      )
      .join("") ||
    '<tr><td colspan="4">No static portal routes configured.</td></tr>';
  const sessions = data.sessions || [];
  $("#sessionCount").textContent = `${sessions.length} active`;
  $("#sessionsTable").innerHTML =
    sessions
      .map(
        (session) =>
          `<tr><td>${esc(session.client)}</td><td>${esc(session.backend)}${session.listenerPort ? `<br><small>via :${session.listenerPort}</small>` : ""}</td><td>${fmtDuration(session.ageSeconds)}</td><td>${fmtDuration(session.idleSeconds)}</td><td>${fmtBytes(session.clientBytes)} · ${session.clientPackets} pkt</td><td>${fmtBytes(session.serverBytes)} · ${session.serverPackets} pkt</td></tr>`,
      )
      .join("") || '<tr><td colspan="6">No active sessions.</td></tr>';
}

function backendPayloadFromForm() {
  return {
    id: $("#backendId").value.trim().toLowerCase(),
    displayName: $("#backendDisplayName").value.trim(),
    host: $("#backendHost").value.trim(),
    backendPort: Number($("#backendPort").value),
    publicPort: Number($("#backendPublicPort").value),
    profile: $("#backendProfile").value.trim(),
    connectionMode: $("#backendConnectionMode").value,
    backendAdapter: $("#backendAdapter").value,
    backendOnlineMode: $("#backendOnlineMode").checked,
    requireProxyIdentity: $("#backendRequireIdentity").checked,
    capacity: Number($("#backendCapacity").value),
    fallbackBackend: $("#backendFallbackBackend").value.trim().toLowerCase(),
    enabled: $("#backendEnabled").checked,
    fallback: $("#backendFallback").checked,
  };
}

function updateBackendModeHelp() {
  const full = $("#backendConnectionMode").value === "full_proxy";
  $("#backendOnlineMode").disabled = full;
  if (full) $("#backendOnlineMode").checked = false;
  $("#backendRequireIdentity").disabled = !full;
  if (!full) $("#backendRequireIdentity").checked = false;
  $("#backendModeHelp").textContent = full
    ? "Full Proxy authenticates players at Ninj-OS. The backend must use online-mode=false. Endstone or Vanilla Bridge restores identity and permissions."
    : "Transparent Auth keeps the backend on online-mode=true and passes Microsoft authentication through unchanged. Full proxy commands and identity forwarding are not applied.";
}

function openBackendDialog(backend = null) {
  const creating = !backend;
  $("#backendDialogTitle").textContent = creating
    ? "Add Server"
    : `Edit ${backend.displayName}`;
  $("#backendOriginalId").value = backend?.id || "";
  $("#backendId").value = backend?.id || "";
  $("#backendDisplayName").value = backend?.displayName || "";
  $("#backendHost").value = backend?.host || "";
  $("#backendPort").value = backend?.backendPort || 19132;
  $("#backendPublicPort").value = backend?.publicPort || 0;
  $("#backendProfile").value = backend?.profile || backend?.id || "";
  $("#backendConnectionMode").value = backend?.connectionMode || "transparent";
  $("#backendAdapter").value = backend?.backendAdapter || "proxy_only";
  $("#backendOnlineMode").checked = backend?.backendOnlineMode ?? true;
  $("#backendRequireIdentity").checked = backend?.requireProxyIdentity ?? false;
  $("#backendCapacity").value = backend?.capacity || 50;
  $("#backendFallbackBackend").value = backend?.fallbackBackend || "";
  $("#backendEnabled").checked = backend?.enabled ?? true;
  $("#backendFallback").checked = backend?.fallback ?? false;
  updateBackendModeHelp();
  $("#backendTestResult").textContent =
    "Test checks the backend host and UDP port directly.";
  $("#backendDialog").showModal();
}

function renderBackendRegistry(data) {
  state.registry = data;
  const topology = data.topology || {};
  const backends = topology.backends || [];
  const liveBackends = state.data?.gateway?.backends || [];
  const transferPorts = data.availableTransferPorts || [];
  const transferSummary = $("#transferPortAvailability");
  if (transferSummary) {
    transferSummary.textContent = transferPorts.length
      ? `Temporary transfers will use unassigned ports: ${transferPorts.join(", ")}`
      : "No temporary transfer ports remain after permanent backend assignments.";
  }
  $("#backendRestartBadge").textContent = data.restartPending
    ? "Restart pending"
    : "Configuration active";
  $("#backendRestartBadge").className =
    `badge ${data.restartPending ? "warn" : ""}`;
  $("#registryPrimaryBackend").innerHTML = backends
    .map(
      (item) =>
        `<option value="${esc(item.id)}">${esc(item.displayName)}</option>`,
    )
    .join("");
  $("#registryPrimaryBackend").value =
    topology.primaryBackend || backends[0]?.id || "";
  $("#registryRoutingMode").value = topology.routingMode || "primary";
  $("#managedPortNote").textContent = data.allowedPublicPorts?.length
    ? `Available Pterodactyl UDP allocations: ${data.allowedPublicPorts.join(", ")}. Add an allocation in Pterodactyl before assigning a new public port.`
    : "No managed port list is configured. Public port validation is permissive.";
  $("#backendRegistryTable").innerHTML =
    backends
      .map((backend) => {
        const live = liveBackends.find(
          (item) => (item.name || item.id) === backend.id,
        );
        const route =
          backend.publicPort > 0
            ? `:${backend.publicPort}/UDP`
            : "No public route";
        const secretDescriptor = state.secrets.find(
          (item) => item.id === `backend.${backend.id}.companion_secret`,
        );
        const secret = secretDescriptor?.reference || "Open Secret Vault";
        const compatibility = live?.protocolCompatibility;
        const activeProtocols = live?.activeClientProtocols?.length
          ? ` · active ${live.activeClientProtocols.join(", ")}`
          : "";
        const protocolSummary = compatibility?.supported
          ? `Protocol ${compatibility.protocol} · ${compatibility.mode} · ${compatibility.distinctPackets || 0} packet types observed${activeProtocols}`
          : backend.connectionMode === "full_proxy"
            ? "Protocol pack unavailable"
            : "Protocol-agnostic relay";
        return `<tr>
        <td><b>${esc(backend.displayName)}</b><br><small>${esc(backend.id)}</small></td>
        <td><code>${esc(backend.host)}:${backend.backendPort}</code></td>
        <td>${esc(route)}</td>
        <td><b>${backend.connectionMode === "full_proxy" ? "Full Proxy" : "Transparent Auth"}</b><br><small>${esc(backend.backendAdapter || "proxy_only")} · backend online-mode=${backend.backendOnlineMode ? "true" : "false"}</small></td>
        <td>${esc(backend.profile || backend.id)}<br><small>capacity ${backend.capacity || 50}${backend.requireProxyIdentity ? " · identity required" : ""}</small></td>
        <td><code>${esc(secret)}</code><br><small>${secretDescriptor?.configured ? `Configured · ${esc(secretDescriptor.fingerprint || "")}` : "Not configured"}</small></td>
        <td><span class="status ${!backend.enabled ? "warn" : live?.healthy ? "good" : "bad"}">${!backend.enabled ? "Disabled" : live?.healthy ? "Healthy" : "Offline"}</span><br><small>${live ? `${Number(live.latencyMs || 0).toFixed(1)} ms · ${live.activeSessions || 0} sessions` : "Awaiting health check"}${backend.fallback ? " · fallback" : ""}</small></td>
        <td><div class="button-row compact"><small>${esc(protocolSummary)}</small><button class="ghost backend-test" data-id="${esc(backend.id)}">Test</button><button class="ghost backend-companion" data-id="${esc(backend.id)}">Secret Vault</button><button class="ghost backend-edit" data-id="${esc(backend.id)}">Edit</button><button class="danger backend-delete" data-id="${esc(backend.id)}">Delete</button></div></td>
      </tr>`;
      })
      .join("") ||
    '<tr><td colspan="8">No backend servers are configured.</td></tr>';

  $$(".backend-edit").forEach((button) =>
    button.addEventListener("click", () => {
      const backend = backends.find((item) => item.id === button.dataset.id);
      if (backend) openBackendDialog(backend);
    }),
  );
  $$(".backend-test").forEach((button) =>
    button.addEventListener("click", async () => {
      const backend = backends.find((item) => item.id === button.dataset.id);
      if (!backend) return;
      button.disabled = true;
      try {
        const result = await api("/api/backend-registry/test", {
          method: "POST",
          body: JSON.stringify({
            host: backend.host,
            port: backend.backendPort,
          }),
        });
        toast(
          result.reachable
            ? `${backend.displayName} replied in ${result.latencyMs} ms`
            : `${backend.displayName}: ${result.error}`,
        );
      } catch (error) {
        toast(error.message);
      } finally {
        button.disabled = false;
      }
    }),
  );
  $$(".backend-companion").forEach((button) =>
    button.addEventListener("click", async () => {
      const nav = document.querySelector('#nav button[data-view="configuration"]');
      nav?.click();
      await refreshUnifiedConfiguration();
      const secret = state.secrets.find(
        (item) => item.id === `backend.${button.dataset.id}.companion_secret`,
      );
      if (secret) openSecretEditor(secret);
      else toast("No backend Secret Vault entry was found.");
    }),
  );
  $$(".backend-delete").forEach((button) =>
    button.addEventListener("click", async () => {
      const backend = backends.find((item) => item.id === button.dataset.id);
      if (
        !backend ||
        !window.confirm(`Delete ${backend.displayName} from the proxy?`)
      )
        return;
      try {
        const result = await api(
          `/api/backend-registry?id=${encodeURIComponent(backend.id)}`,
          { method: "DELETE" },
        );
        if (!result.saved) throw new Error("The deletion was not confirmed on disk.");
        $("#backendSaveStatus").textContent = `${backend.displayName} was removed from ${result.configPath || "the unified configuration"}. Only the gateway is restarting.`;
        toast(`${backend.displayName} removed and verified. Gateway restart queued.`);
        await refreshBackendRegistry(true);
      } catch (error) {
        $("#backendSaveStatus").textContent = `Backend deletion failed: ${error.message}`;
        toast(error.message);
      }
    }),
  );
}

async function refreshBackendRegistry(force = false) {
  if (!force && !$("#view-backends")?.classList.contains("active")) return;
  try {
    renderBackendRegistry(await api("/api/backend-registry"));
  } catch (error) {
    toast(error.message);
  }
}

async function saveBackend(event) {
  event.preventDefault();
  const originalId = $("#backendOriginalId").value.trim();
  const payload = backendPayloadFromForm();
  const saveButton = $("#saveBackendButton");
  const originalLabel = saveButton.textContent;
  saveButton.disabled = true;
  saveButton.textContent = "Saving…";
  $("#backendTestResult").textContent = "Writing and verifying the unified configuration…";

  try {
    const path = originalId
      ? `/api/backend-registry?id=${encodeURIComponent(originalId)}`
      : "/api/backend-registry";
    const result = await api(path, {
      method: originalId ? "PUT" : "POST",
      body: JSON.stringify(payload),
    });
    if (!result.saved) throw new Error("The dashboard did not confirm that the backend was persisted.");

    if (result.topology) {
      renderBackendRegistry({
        topology: result.topology,
        allowedPublicPorts: state.registry?.allowedPublicPorts || [],
        availableTransferPorts: state.registry?.availableTransferPorts || [],
        restartPending: true,
        path: result.configPath,
      });
    }
    $("#backendDialog").close();
    $("#backendSaveStatus").textContent = `${payload.displayName} saved to ${result.configPath || "the unified configuration"}. The gateway restart is queued; the dashboard remains online.`;
    toast(`${payload.displayName} saved and verified. Gateway restart queued.`);

    await refreshBackendRegistry(true);
    setTimeout(refreshState, 600);
    setTimeout(() => refreshBackendRegistry(true), 1500);
  } catch (error) {
    const message = error?.message || "Backend save failed";
    $("#backendTestResult").textContent = `Save failed: ${message}`;
    $("#backendSaveStatus").textContent = `Last backend change failed: ${message}`;
    toast(message);
  } finally {
    saveButton.disabled = false;
    saveButton.textContent = originalLabel;
  }
}

async function testBackendForm() {
  const payload = backendPayloadFromForm();
  $("#backendTestResult").textContent = "Testing Bedrock RakNet response…";
  try {
    const result = await api("/api/backend-registry/test", {
      method: "POST",
      body: JSON.stringify({ host: payload.host, port: payload.backendPort }),
    });
    $("#backendTestResult").textContent = result.reachable
      ? `Reachable in ${result.latencyMs} ms${result.motd ? ` · ${result.motd}` : ""}`
      : `Not reachable: ${result.error}`;
  } catch (error) {
    $("#backendTestResult").textContent = error.message;
  }
}

async function saveRegistrySettings() {
  try {
    const result = await api("/api/backend-registry/settings", {
      method: "PUT",
      body: JSON.stringify({
        primaryBackend: $("#registryPrimaryBackend").value,
        routingMode: $("#registryRoutingMode").value,
      }),
    });
    if (!result.saved) throw new Error("Routing settings were not confirmed on disk.");
    $("#backendSaveStatus").textContent = `Routing settings saved to ${result.configPath || "the unified configuration"}. Only the gateway is restarting.`;
    toast("Routing settings saved and verified. Gateway restart queued.");
    await refreshBackendRegistry(true);
  } catch (error) {
    toast(error.message);
  }
}

function renderSystem(sys) {
  $("#systemMetrics").innerHTML = [
    detail(
      "CPU Usage",
      sys.cpuPercent != null ? `${Number(sys.cpuPercent).toFixed(1)}%` : "—",
    ),
    detail("Memory", fmtBytes(sys.memoryBytes || 0)),
    detail(
      "Memory Limit",
      sys.memoryLimitBytes ? fmtBytes(sys.memoryLimitBytes) : "Host",
    ),
    detail("Disk Used", fmtBytes(sys.diskUsedBytes || 0)),
    detail("Network In", fmtBytes(sys.networkRxBytes || 0)),
    detail("Network Out", fmtBytes(sys.networkTxBytes || 0)),
    detail("Dashboard Uptime", fmtDuration(sys.dashboardUptimeSeconds || 0)),
    detail(
      "Load Average",
      Array.isArray(sys.loadAverage)
        ? sys.loadAverage.map((n) => Number(n).toFixed(2)).join(" / ")
        : "—",
    ),
  ].join("");
}

function updateTraffic(gateway) {
  const now = Date.now();
  const c = gateway.counters || {};
  const previous = state.traffic.at(-1);
  let inbound = 0;
  let outbound = 0;
  if (previous && gateway.timestamp > previous.gatewayTimestamp) {
    const seconds = (gateway.timestamp - previous.gatewayTimestamp) / 1000;
    inbound = Math.max(
      0,
      (Number(c.clientBytes || 0) - previous.clientBytes) / seconds,
    );
    outbound = Math.max(
      0,
      (Number(c.serverBytes || 0) - previous.serverBytes) / seconds,
    );
  }
  state.traffic.push({
    time: now,
    gatewayTimestamp: gateway.timestamp || now,
    clientBytes: Number(c.clientBytes || 0),
    serverBytes: Number(c.serverBytes || 0),
    inbound,
    outbound,
  });
  if (state.traffic.length > 60) state.traffic.shift();
  drawTrafficChart();
}

function drawTrafficChart() {
  const canvas = $("#trafficChart");
  const rect = canvas.getBoundingClientRect();
  const ratio = devicePixelRatio || 1;
  canvas.width = Math.max(300, rect.width) * ratio;
  canvas.height = 220 * ratio;
  const context = canvas.getContext("2d");
  context.scale(ratio, ratio);
  const width = canvas.width / ratio;
  const height = 220;
  context.clearRect(0, 0, width, height);
  const values = state.traffic;
  const max = Math.max(1, ...values.flatMap((v) => [v.inbound, v.outbound]));
  context.strokeStyle = "#203444";
  context.lineWidth = 1;
  for (let i = 0; i < 5; i++) {
    const y = 15 + i * 45;
    context.beginPath();
    context.moveTo(0, y);
    context.lineTo(width, y);
    context.stroke();
  }
  [
    ["inbound", "#27d7ff"],
    ["outbound", "#50e38b"],
  ].forEach(([key, color]) => {
    context.strokeStyle = color;
    context.lineWidth = 2;
    context.beginPath();
    values.forEach((item, index) => {
      const x = values.length <= 1 ? 0 : (index / (values.length - 1)) * width;
      const y = height - 20 - (item[key] / max) * (height - 45);
      if (index === 0) context.moveTo(x, y);
      else context.lineTo(x, y);
    });
    context.stroke();
  });
  context.fillStyle = "#8299a8";
  context.font = "11px system-ui";
  context.fillText(
    `Client → backend ${fmtRate(values.at(-1)?.inbound || 0)}`,
    8,
    14,
  );
  context.fillText(
    `Backend → client ${fmtRate(values.at(-1)?.outbound || 0)}`,
    180,
    14,
  );
}

async function refreshState() {
  try {
    renderState(await api("/api/state"));
  } catch (error) {
    if (error.message !== "Unauthorized") {
      $("#connectionDot").className = "dot offline";
      $("#connectionState").textContent = "Disconnected";
      $("#lastRefresh").textContent = error.message;
    }
  }
}

function renderCompanionFleet(companions) {
  if (!$("#companionsTable")) return;
  const configured = companions.filter((item) => item.configured !== false);
  $("#companionFleetCount").textContent =
    `${configured.filter((item) => item.connected).length}/${configured.length} connected`;
  $("#companionsTable").innerHTML =
    companions
      .map((item) => {
        const m = item.metrics || {};
        const lastReport = item.ageSeconds == null
          ? "Never"
          : item.ageSeconds < 2
            ? "Now"
            : `${item.ageSeconds}s ago`;
        return `<tr><td><b>${esc(item.displayName || item.serverId || "unknown")}</b><br><small>${esc(item.serverId || "unknown")}</small></td><td><span class="status ${companionStatusClass(item)}">${esc(companionStatusLabel(item))}</span></td><td>${m.currentTps != null ? Number(m.currentTps).toFixed(2) : "—"}</td><td>${m.currentMspt != null ? Number(m.currentMspt).toFixed(2) + " ms" : "—"}</td><td>${m.onlinePlayers ?? "—"}/${m.maxPlayers ?? "?"}</td><td>${m.queueDepth ?? "—"}${m.queueDrops ? ` · ${m.queueDrops} dropped` : ""}</td><td>${esc(lastReport)}</td></tr>`;
      })
      .join("") ||
    '<tr><td colspan="8">No backend servers are configured.</td></tr>';
}

async function refreshPresence() {
  if (!$("#view-players")?.classList.contains("active")) return;
  try {
    const data = await api("/api/presence");
    const summary = data.summary || {};
    $("#presenceCount").textContent =
      `${summary.online || 0} online · ${summary.tracked || 0} tracked`;
    $("#presenceCards").innerHTML = [
      metric(
        "Online Now",
        summary.online || 0,
        `${summary.ttlSeconds || 0}s activity window`,
      ),
      metric("Tracked Profiles", summary.tracked || 0, "XUID-first identity"),
      ...Object.entries(summary.byServer || {}).map(([server, count]) =>
        metric(server, count, "active players"),
      ),
    ].join("");
    $("#presenceTable").innerHTML =
      (data.records || [])
        .map(
          (item) =>
            `<tr><td><b>${esc(item.playerName || "Unknown")}</b></td><td><code>${esc(item.xuid || "—")}</code></td><td>${esc(item.serverId || "—")}</td><td><span class="status ${item.online ? "good" : "warn"}">${item.online ? "Online" : "Recently seen"}</span></td><td>${fmtTime(item.lastSeen)}</td><td>${esc(item.address || "redacted")}</td></tr>`,
        )
        .join("") ||
      '<tr><td colspan="6">No player presence has been reported.</td></tr>';

    const profiles = await api("/api/profiles?limit=1000");
    $("#networkProfileCount").textContent =
      `${profiles.profiles.length} profiles`;
    $("#networkProfilesTable").innerHTML =
      profiles.profiles
        .map((item) => {
          let access = {};
          try {
            access = JSON.parse(item.access_json || "{}");
          } catch (_) {}
          const denied = Object.entries(access)
            .filter(([, allowed]) => !allowed)
            .map(([name]) => name);
          const accessLabel = Number(item.network_banned)
            ? "Network banned"
            : denied.length
              ? `Denied: ${denied.join(", ")}`
              : "Allowed";
          return `<tr><td><b>${esc(item.gamertag || "—")}</b></td><td><code>${esc(item.xuid || "—")}</code></td><td>${esc(item.network_role || "member")}</td><td>${esc(item.current_server || "—")}</td><td><span class="status ${Number(item.network_banned) ? "bad" : "good"}">${esc(accessLabel)}</span></td><td>${fmtTime(item.last_seen)}</td></tr>`;
        })
        .join("") ||
      '<tr><td colspan="6">No XUID profiles recorded yet.</td></tr>';
  } catch (_) {}
}

function renderPolicyState(gateway) {
  if (!$("#adaptiveFirewallDetails")) return;
  const firewall = gateway.firewall || {};
  const counters = gateway.counters || {};
  $("#firewallMode").textContent = firewall.adaptive
    ? "adaptive"
    : "fixed limits";
  $("#configVersionLabel").textContent =
    `config v${gateway.configVersion || 1}`;
  $("#configStatus").textContent = gateway.lastConfigError
    ? `Last reload failed: ${gateway.lastConfigError}`
    : `Last reload ${gateway.lastConfigReload ? fmtTime(gateway.lastConfigReload) : "not recorded"}`;
  $("#adaptiveFirewallDetails").innerHTML = [
    detail("Firewall", firewall.enabled ? "Enabled" : "Disabled"),
    detail("Adaptive Scoring", firewall.adaptive ? "Enabled" : "Disabled"),
    detail("Warning Score", firewall.warningThreshold ?? "—"),
    detail("Ban Score", firewall.banThreshold ?? "—"),
    detail("Active Bans", firewall.activeBans || 0),
    detail("Allowlist", firewall.allowlistCount || 0),
    detail("Denylist", firewall.denylistCount || 0),
    detail("Reload Failures", counters.configReloadFailures || 0),
  ].join("");
  const risk = firewall.topRisk || [];
  $("#riskCount").textContent = `${risk.length} shown`;
  $("#riskTable").innerHTML =
    risk
      .map(
        (item) =>
          `<tr><td><code>${esc(item.ip)}</code></td><td><b>${item.risk}</b></td><td>${item.offenses}</td><td>${item.banLevel}</td><td>${item.bannedUntil ? new Date(item.bannedUntil).toLocaleString() : "—"}</td><td>${esc(item.reason || "—")}</td><td><button class="ghost risk-reset" data-ip="${esc(item.ip)}">Reset</button></td></tr>`,
      )
      .join("") ||
    '<tr><td colspan="7">No risky addresses are currently tracked.</td></tr>';
  $$(".risk-reset").forEach((button) =>
    button.addEventListener("click", () =>
      control("risk_reset", button.dataset.ip),
    ),
  );
}

async function refreshPolicy() {
  if (!$("#view-policy")?.classList.contains("active")) return;
  renderPolicyState(state.data?.gateway || {});
  try {
    const data = await api("/api/config");
    if (document.activeElement !== $("#liveConfigEditor"))
      $("#liveConfigEditor").value = data.content || "";
    $("#rollbackLiveConfig").disabled = !data.backupAvailable;
    const profiles = await api("/api/protection-profiles");
    if (document.activeElement !== $("#protectionProfilesEditor"))
      $("#protectionProfilesEditor").value = profiles.content || "";
  } catch (_) {}
}
async function applyLiveConfig() {
  try {
    await api("/api/config", {
      method: "POST",
      body: JSON.stringify({ content: $("#liveConfigEditor").value }),
    });
    toast("Live policy validated and queued");
    setTimeout(refreshState, 800);
  } catch (error) {
    toast(error.message);
  }
}
async function applyProtectionProfiles() {
  try {
    await api("/api/protection-profiles", {
      method: "POST",
      body: JSON.stringify({ content: $("#protectionProfilesEditor").value }),
    });
    toast("Protection profiles validated and queued");
    setTimeout(refreshState, 800);
  } catch (error) {
    toast(error.message);
  }
}
async function rollbackLiveConfig() {
  try {
    await api("/api/config/rollback", { method: "POST", body: "{}" });
    toast("Previous live policy restored");
    setTimeout(() => {
      refreshState();
      refreshPolicy();
    }, 800);
  } catch (error) {
    toast(error.message);
  }
}

function renderManagedSettings(settings) {
  state.settings = settings || [];
  const groups = new Map();
  for (const setting of state.settings) {
    if (!groups.has(setting.section)) groups.set(setting.section, []);
    groups.get(setting.section).push(setting);
  }
  $("#managedSettingsGrid").innerHTML = [...groups.entries()]
    .map(([section, fields], index) => {
      const controls = fields
        .map((field) => {
          const id = esc(field.id);
          const label = esc(field.label);
          const description = esc(field.description || "");
          if (field.type === "boolean") {
            return `<label class="setting-field checkbox-setting"><span><input class="managed-setting" data-setting-id="${id}" type="checkbox" ${field.value === "true" ? "checked" : ""} /> ${label}</span><small>${description}</small></label>`;
          }
          if (field.type === "select") {
            return `<label class="setting-field">${label}<select class="managed-setting" data-setting-id="${id}">${(field.options || []).map((option) => `<option value="${esc(option)}" ${option === field.value ? "selected" : ""}>${esc(option)}</option>`).join("")}</select><small>${description}</small></label>`;
          }
          const type = field.type === "number" ? "number" : "text";
          const min = field.minimum != null ? ` min="${field.minimum}"` : "";
          const max = field.maximum != null ? ` max="${field.maximum}"` : "";
          return `<label class="setting-field">${label}<input class="managed-setting" data-setting-id="${id}" type="${type}" value="${esc(field.value || "")}"${min}${max} /><small>${description}</small></label>`;
        })
        .join("");
      return `<details class="settings-section" ${index < 3 ? "open" : ""}><summary>${esc(section)}</summary><div class="settings-grid">${controls}</div></details>`;
    })
    .join("");
}

async function saveManagedSettings() {
  const values = {};
  $$(".managed-setting").forEach((input) => {
    values[input.dataset.settingId] = input.type === "checkbox" ? String(input.checked) : input.value.trim();
  });
  if (!window.confirm("Validate and save all managed settings? The dashboard and gateway will restart.")) return;
  try {
    const result = await api("/api/settings", { method: "PUT", body: JSON.stringify({ values }) });
    const verification = await api("/api/unified-config");
    if (!result.saved || !result.revision || verification.revision !== result.revision) {
      throw new Error("Managed settings were written but could not be verified from the canonical configuration.");
    }
    toast(`Managed settings saved and verified (${result.revision.slice(0, 12)}). Services are restarting.`);
    setTimeout(refreshAll, 2200);
  } catch (error) {
    toast(error.message);
  }
}

function renderSecretVault(secrets) {
  state.secrets = secrets || [];
  $("#secretSourcesTable").innerHTML = state.secrets
    .map((secret) => `<tr>
      <td><b>${esc(secret.label)}</b><br><small>${esc(secret.component)}</small></td>
      <td><code>${esc(secret.reference || "Not configured")}</code></td>
      <td><span class="status ${secret.configured ? "good" : "warn"}">${secret.configured ? "Configured" : "Missing"}</span></td>
      <td><code>${esc(secret.fingerprint || "—")}</code></td>
      <td><button class="ghost secret-edit" data-secret-id="${esc(secret.id)}">Set / Rotate</button></td>
    </tr>`)
    .join("") || '<tr><td colspan="5">No managed secrets were found.</td></tr>';
  $$(".secret-edit").forEach((button) => button.addEventListener("click", () => {
    const secret = state.secrets.find((item) => item.id === button.dataset.secretId);
    if (secret) openSecretEditor(secret);
  }));
}

function openSecretEditor(secret) {
  state.selectedSecret = secret;
  $("#secretId").value = secret.id;
  $("#secretDialogTitle").textContent = secret.label;
  $("#secretDialogDescription").textContent = `${secret.reference}. Current fingerprint: ${secret.fingerprint || "not configured"}.`;
  const mode = secret.mode === "unset" ? "dashboard" : secret.mode;
  $("#secretMode").value = mode;
  const inheritOption = $('#secretMode option[value="inherit"]');
  inheritOption.disabled = !secret.canInherit;
  if (!secret.canInherit && mode === "inherit") $("#secretMode").value = "dashboard";
  $("#secretEnvironmentVariable").value = secret.environmentVariable || secret.suggestedEnvironmentVariable || "";
  $("#secretValue").value = "";
  $("#secretValue").minLength = secret.minimumLength || 12;
  $("#generateSecret").disabled = !secret.canGenerate;
  syncSecretEditorMode();
  $("#secretDialog").showModal();
}

function syncSecretEditorMode() {
  const mode = $("#secretMode").value;
  $("#secretEnvironmentLabel").style.display = mode === "environment" ? "flex" : "none";
  $("#secretValueLabel").style.display = mode === "dashboard" ? "flex" : "none";
  $("#generateSecret").style.display = mode === "dashboard" && state.selectedSecret?.canGenerate ? "inline-flex" : "none";
}

async function submitSecretChange(event, generate = false) {
  if (event) event.preventDefault();
  const request = {
    id: $("#secretId").value,
    mode: generate ? "dashboard" : $("#secretMode").value,
    value: $("#secretValue").value,
    environmentVariable: $("#secretEnvironmentVariable").value.trim(),
    generate,
  };
  if (!generate && request.mode === "dashboard" && !request.value) {
    toast("Enter a new value or use Generate Secure Value.");
    return;
  }
  try {
    const result = await api("/api/secrets", { method: "PUT", body: JSON.stringify(request) });
    const verification = await api("/api/unified-config");
    if (!result.saved || !result.revision || verification.revision !== result.revision) {
      throw new Error("The secret source was written but could not be verified from the canonical configuration.");
    }
    $("#secretDialog").close();
    if (result.generatedValue) {
      try { await navigator.clipboard.writeText(result.generatedValue); } catch (_) {}
      window.prompt("Copy this generated value now. It will not be shown again.", result.generatedValue);
    }
    toast(`Secret source saved and verified (${result.revision.slice(0, 12)}). Services are restarting.`);
    if (result.sessionWillEnd) {
      state.token = "";
      sessionStorage.removeItem("ninjos_dashboard_token");
      stopPolling();
      setTimeout(() => openTokenDialog({ reset: false }), 1900);
    } else {
      setTimeout(refreshAll, 1800);
    }
  } catch (error) {
    toast(error.message);
  }
}

function renderCompanionManager(data) {
  state.companionManager = data;
  const selected = $("#companionBackendSelect").value;
  $("#companionBackendSelect").innerHTML = (data.backends || [])
    .map((backend) => `<option value="${esc(backend.id)}">${esc(backend.displayName)}</option>`)
    .join("");
  if ((data.backends || []).some((backend) => backend.id === selected)) {
    $("#companionBackendSelect").value = selected;
  }
  const artifact = data.artifact || {};
  $("#companionArtifactBadge").textContent = artifact.compiledAvailable ? "Compiled artifact ready" : "Compiled artifact needed";
  $("#companionArtifactBadge").className = `badge ${artifact.compiledAvailable ? "" : "warn"}`;
  $("#removeCompanionArtifact").disabled = !artifact.compiledAvailable;
  $("#downloadCompanionPackage").disabled = !artifact.compiledAvailable;
  $("#downloadCompanionSource").disabled = !artifact.sourceAvailable;
  $("#companionBuilderStatus").textContent = artifact.compiledAvailable
    ? `Install packages are ready. Artifact SHA-256: ${artifact.compiledSHA256 || "unknown"}`
    : "Upload the compiled .so or the GitHub Actions artifact ZIP to enable install-ready downloads.";
  renderSelectedCompanion();
}

function renderSelectedCompanion() {
  const id = $("#companionBackendSelect").value;
  const backend = state.companionManager?.backends?.find((item) => item.id === id);
  $("#companionConfigPreview").textContent = backend?.preview || backend?.error || "Select a backend server.";
  $("#downloadCompanionProperties").disabled = !backend?.configured;
  if (!backend?.configured) {
    $("#companionBuilderStatus").textContent = backend?.error || "Configure a companion shared secret first. This is separate from the dashboard owner password and browser session.";
    return;
  }
  const connection = backend.connection || {};
  const status = connection.connected
    ? `Connected · last report ${connection.ageSeconds || 0}s ago · TPS ${Number(connection.currentTps || 0).toFixed(2)} · MSPT ${Number(connection.currentMspt || 0).toFixed(2)}`
    : connection.lastSeenAt
      ? `Not currently reporting · last report ${connection.ageSeconds || 0}s ago`
      : "No signed report has been accepted from this backend yet";
  $("#companionBuilderStatus").textContent = `${status}. Expected secret fingerprint: ${backend.secretFingerprint || "unset"}. Install the generated package, then run /npm probe and /npm status in the Endstone console.`;
}

async function authenticatedDownload(path, fallbackName) {
  const response = await fetch(path, { headers: { Authorization: `Bearer ${state.token}` } });
  if (!response.ok) throw new Error((await response.text()) || `HTTP ${response.status}`);
  const blob = await response.blob();
  const disposition = response.headers.get("Content-Disposition") || "";
  const match = disposition.match(/filename="?([^";]+)"?/i);
  const link = document.createElement("a");
  link.href = URL.createObjectURL(blob);
  link.download = match?.[1] || fallbackName;
  link.click();
  URL.revokeObjectURL(link.href);
}

async function downloadCompanion(type) {
  const backend = $("#companionBackendSelect").value;
  try {
    await authenticatedDownload(`/api/companion-download?type=${encodeURIComponent(type)}&backend=${encodeURIComponent(backend)}`, `companion-${backend || "source"}.zip`);
    toast(`Companion ${type} downloaded.`);
  } catch (error) {
    toast(error.message);
  }
}

async function uploadCompanionArtifact() {
  const file = $("#companionArtifactInput").files[0];
  if (!file) {
    toast("Choose the compiled .so or GitHub artifact ZIP first.");
    return;
  }
  const form = new FormData();
  form.append("artifact", file);
  try {
    const response = await fetch("/api/companion-manager", { method: "POST", headers: { Authorization: `Bearer ${state.token}` }, body: form });
    if (!response.ok) throw new Error((await response.text()) || `HTTP ${response.status}`);
    toast("Compiled companion artifact uploaded.");
    $("#companionArtifactInput").value = "";
    await refreshUnifiedConfiguration();
  } catch (error) {
    toast(error.message);
  }
}

async function removeCompanionArtifact() {
  if (!window.confirm("Remove the stored compiled companion artifact?")) return;
  try {
    await api("/api/companion-manager", { method: "DELETE" });
    toast("Compiled companion artifact removed.");
    await refreshUnifiedConfiguration();
  } catch (error) {
    toast(error.message);
  }
}

async function refreshUnifiedConfiguration() {
  if (!$("#view-configuration")?.classList.contains("active")) return;
  try {
    const [configuration, settings, secrets, companion] = await Promise.all([
      api("/api/unified-config"),
      api("/api/settings"),
      api("/api/secrets"),
      api("/api/companion-manager"),
    ]);
    $("#unifiedConfigPath").textContent = `${configuration.path || "config/edge-fabric.ini"} · ${configuration.revision?.slice(0, 12) || "unverified"}`;
    if (document.activeElement !== $("#unifiedConfigEditor")) {
      $("#unifiedConfigEditor").value = configuration.content || "";
    }
    renderManagedSettings(settings.settings || []);
    renderSecretVault(secrets.secrets || []);
    renderCompanionManager(companion);
    $("#configurationLayout").innerHTML = [
      detail("Canonical file", configuration.path || "config/edge-fabric.ini"),
      detail("Automatic backup", `${configuration.path || "config/edge-fabric.ini"}.bak`),
      detail("Generated gateway file", "/home/container/gateway.conf"),
      detail("Generated companion secrets", "/home/container/runtime/companion-secrets.properties"),
      detail("Compiled companion artifact", "/home/container/runtime/companion-artifacts/ninjos_proxie_companion.so"),
      detail("Generated dashboard environment", "/home/container/runtime/generated/dashboard.env"),
      detail("Generated summary", "/home/container/runtime/config-summary.json"),
    ].join("");
  } catch (error) {
    toast(error.message);
  }
}

async function saveUnifiedConfiguration() {
  if (!window.confirm("Validate, save, and verify the unified configuration?"))
    return;
  const button = $("#saveUnifiedConfig");
  const originalLabel = button?.textContent || "Validate & Save Configuration";
  if (button) {
    button.disabled = true;
    button.textContent = "Saving and verifying…";
  }
  try {
    const result = await api("/api/unified-config", {
      method: "PUT",
      body: JSON.stringify({ content: $("#unifiedConfigEditor").value }),
    });
    if (!result.saved || !result.revision) throw new Error("The configuration was not confirmed on disk.");
    const verification = await api("/api/unified-config");
    if (verification.revision !== result.revision) {
      throw new Error("The configuration revision read back from disk does not match the saved revision.");
    }
    toast(`${result.message || "Unified configuration saved and verified."} Revision ${result.revision.slice(0, 12)}.`);
    if (result.restartScope === "services") {
      setTimeout(refreshAll, 4200);
    } else {
      setTimeout(refreshAll, 1200);
    }
  } catch (error) {
    toast(error.message);
  } finally {
    if (button) {
      button.disabled = false;
      button.textContent = originalLabel;
    }
  }
}

async function refreshAudit() {
  if (!$("#view-audit")?.classList.contains("active")) return;
  try {
    const data = await api("/api/audit?limit=1000");
    $("#auditCount").textContent = `${data.records.length} recent`;
    $("#auditTable").innerHTML =
      data.records
        .map(
          (item) =>
            `<tr><td>${fmtTime(item.timestamp)}</td><td><b>${esc(item.actor || "—")}</b></td><td>${esc(item.role || "—")}</td><td>${esc(item.action || "—")}</td><td><span class="status ${item.result === "failed" || item.result === "denied" ? "bad" : item.result === "rejected" ? "warn" : "good"}">${esc(item.result || "—")}</span></td><td>${esc(item.remoteIp || "—")}</td><td><code>${esc(JSON.stringify(item.details || {}))}</code></td></tr>`,
        )
        .join("") ||
      '<tr><td colspan="7">No administrative actions have been recorded.</td></tr>';
  } catch (_) {}
}

function packetQuery() {
  const params = new URLSearchParams({
    layer: $("#packetLayer").value,
    limit: $("#packetLimit").value,
  });
  if ($("#packetDirection").value)
    params.set("direction", $("#packetDirection").value);
  if ($("#packetTier").value) params.set("tier", $("#packetTier").value);
  if ($("#packetPlayer").value.trim())
    params.set("player", $("#packetPlayer").value.trim());
  if ($("#packetId").value.trim())
    params.set("packetId", $("#packetId").value.trim());
  if ($("#packetSearch").value.trim())
    params.set("q", $("#packetSearch").value.trim());
  return params;
}
async function refreshPackets() {
  if (state.packetsPaused || !$("#view-packets").classList.contains("active"))
    return;
  try {
    const data = await api(`/api/packets?${packetQuery()}`);
    const tierSummary = Object.entries(data.tiers || {})
      .map(([tier, count]) => `${tier.replaceAll("_", " ")} ${count}`)
      .join(" · ");
    $("#packetStats").textContent = `${data.count} records shown${tierSummary ? ` · ${tierSummary}` : ""}`;
    $("#packetsTable").innerHTML =
      data.records
        .map((packet, index) => {
          const layer =
            packet.layer ||
            (packet.type === "packet" ? "gameplay" : "transport");
          const direction = packet.direction || "—";
          const label =
            packet.packetName ||
            packet.raknetName ||
            `Packet #${packet.packetId ?? "?"}`;
          const player =
            packet.playerName ||
            packet.player ||
            packet.xuid ||
            packet.client ||
            packet.ip ||
            "—";
          return `<tr data-packet-index="${index}"><td>${fmtTime(packet.timestamp || packet.receivedAt)}</td><td><span class="layer-chip ${esc(layer)}">${esc(layer)}</span></td><td>${esc(direction)}</td><td><b>${esc(label)}</b>${packet.packetId != null ? `<br><small>ID ${packet.packetId}</small>` : ""}</td><td>${esc(player)}</td><td>${packet.bytes ?? packet.size ?? packet.payloadBytes ?? "—"}</td><td>${esc(packet.action || "forward")}</td><td>${esc(packet.backend || packet.serverId || "—")}</td></tr>`;
        })
        .join("") || '<tr><td colspan="8">No matching packets.</td></tr>';
    $$("#packetsTable tr[data-packet-index]").forEach((row) =>
      row.addEventListener("click", () =>
        showPacket(data.records[Number(row.dataset.packetIndex)]),
      ),
    );
  } catch (_) {}
}
async function showPacket(packet) {
  if (packet.hasDetails && packet.recordId) {
    try {
      const response = await api(`/api/packets?layer=protocol&details=1&limit=1&recordId=${encodeURIComponent(packet.recordId)}`);
      packet = response.records?.[0] || packet;
    } catch (error) {
      toast(`Packet details could not be loaded: ${error.message}`);
    }
  }
  const label = packet.packetName || packet.raknetName || `Packet #${packet.packetId ?? "?"}`;
  const tiers = packet.captureTiers || [];
  $("#packetDetailTitle").textContent = label;
  $("#packetDetailSubtitle").textContent = `${packet.backend || packet.serverId || "Unknown backend"} · ${fmtTime(packet.timestamp || packet.receivedAt)}`;
  $("#packetDetailBadges").innerHTML = [packet.layer || "unknown", packet.direction || "unknown", ...tiers]
    .map((value) => `<span class="layer-chip ${esc(value)}">${esc(String(value).replaceAll("_", " "))}</span>`)
    .join("");
  $("#packetDetailSummary").innerHTML = [
    detail("Packet ID", packet.packetId ?? "—"),
    detail("Protocol", packet.protocol ?? "—"),
    detail("Bytes", packet.bytes ?? packet.size ?? packet.payloadBytes ?? "—"),
    detail("Action", packet.action || "forward"),
    detail("Pack", packet.pack || "—"),
    detail("Sensitive", packet.sensitive ? "Protected — values withheld" : "No sensitive classification"),
  ].join("");
  const decoded = packet.decoded ?? packet.payload;
  $("#packetDecodedSection").hidden = decoded == null;
  $("#packetDecoded").textContent = decoded == null ? "" : JSON.stringify(decoded, null, 2);
  $("#packetTranslatedSection").hidden = packet.translatedDecoded == null;
  $("#packetTranslated").textContent = packet.translatedDecoded == null ? "" : JSON.stringify(packet.translatedDecoded, null, 2);
  const wire = packet.wire;
  $("#packetWireSection").hidden = !wire;
  $("#packetWire").textContent = wire ? formatHexDump(wire.data || "") : "";
  $("#packetWireSummary").textContent = wire
    ? `${wire.capturedBytes} of ${wire.originalBytes} bytes${wire.truncated ? " · truncated" : ""} · SHA-256 ${wire.sha256}`
    : "";
  const roundTrip = packet.roundTrip;
  $("#packetRoundTripSection").hidden = !roundTrip;
  $("#packetRoundTrip").innerHTML = roundTrip ? [
    detail("Result", roundTrip.exact ? "Exact byte match" : "Mismatch"),
    detail("First mismatch", roundTrip.mismatchOffset == null || roundTrip.mismatchOffset < 0 ? "None" : `Byte ${roundTrip.mismatchOffset}`),
    detail("Original", `${roundTrip.originalBytes ?? "—"} bytes`),
    detail("Re-encoded", `${roundTrip.encodedBytes ?? "—"} bytes`),
    detail("Original SHA-256", roundTrip.originalSha256 || "—"),
    detail("Encoded SHA-256", roundTrip.encodedSha256 || roundTrip.error || "—"),
  ].join("") : "";
  $("#packetFailureSection").hidden = !packet.decodeError;
  $("#packetFailure").textContent = packet.decodeError || "";
  $("#packetDetails").textContent = JSON.stringify(packet, null, 2);
  $("#packetDialog").showModal();
}

function formatHexDump(hex) {
  const clean = String(hex || "").replace(/[^0-9a-f]/gi, "").toLowerCase();
  const rows = [];
  for (let offset = 0; offset < clean.length; offset += 32) {
    const bytes = clean.slice(offset, offset + 32).match(/.{1,2}/g) || [];
    const groups = bytes.join(" ").padEnd(47, " ");
    const ascii = bytes.map((value) => {
      const code = Number.parseInt(value, 16);
      return code >= 32 && code <= 126 ? String.fromCharCode(code) : ".";
    }).join("");
    rows.push(`${String(offset / 2).padStart(8, "0")}  ${groups}  |${ascii}|`);
  }
  return rows.join("\n") || "No wire bytes captured.";
}
$("#closePacketDialog").addEventListener("click", () =>
  $("#packetDialog").close(),
);
$("#pausePackets").addEventListener("click", (event) => {
  state.packetsPaused = !state.packetsPaused;
  event.target.textContent = state.packetsPaused ? "Resume" : "Pause";
});
[
  "packetLayer",
  "packetTier",
  "packetDirection",
  "packetPlayer",
  "packetId",
  "packetSearch",
  "packetLimit",
].forEach((id) =>
  $(`#${id}`).addEventListener(
    id.startsWith("packet") &&
      ["packetPlayer", "packetId", "packetSearch"].includes(id)
      ? "input"
      : "change",
    () => {
      clearTimeout(window.packetFilterTimer);
      window.packetFilterTimer = setTimeout(refreshPackets, 250);
    },
  ),
);
$("#clearPacketFilters").addEventListener("click", () => {
  $("#packetLayer").value = "all";
  $("#packetTier").value = "";
  $("#packetDirection").value = "";
  $("#packetPlayer").value = "";
  $("#packetId").value = "";
  $("#packetSearch").value = "";
  refreshPackets();
});

function renderTransferBroker(broker) {
  if (!$("#transferBrokerDetails")) return;
  const start = broker.portStart ?? "—";
  const end = broker.portEnd ?? "—";
  $("#transferPoolLabel").textContent = `${broker.activeTickets || 0} active`;
  $("#transferBrokerDetails").innerHTML = [
    detail("Public Host", broker.host || "—"),
    detail("Port Pool", `${start}–${end}`),
    detail(
      "Ticket TTL",
      broker.ttlSeconds != null ? `${broker.ttlSeconds}s` : "—",
    ),
    detail(
      "Source-IP Lock",
      broker.requireSourceIp === false ? "Disabled" : "Enabled",
    ),
    detail("Active Tickets", broker.activeTickets || 0),
    detail("Mode", "One-use transparent routes"),
  ].join("");
}

async function refreshTransfers() {
  if (!$("#view-transfers").classList.contains("active")) return;
  try {
    const data = await api("/api/transfers");
    $("#transferPoolLabel").textContent =
      `${data.host}:${data.portStart}–${data.portEnd}`;
    $("#activeTransferCount").textContent = `${data.active.length} active`;
    $("#activeTransfersTable").innerHTML =
      data.active
        .map(
          (ticket) =>
            `<tr><td><b>${esc(ticket.playerName || "—")}</b><br><small>${esc(ticket.xuid || "")}</small></td><td>${esc(ticket.destination)}</td><td>${esc(data.host)}:${ticket.port}</td><td>${esc(ticket.sourceIp || "any")}</td><td>${Math.max(0, Math.ceil((ticket.expiresAt - Date.now()) / 1000))}s</td><td><code>${esc((ticket.ticketId || "").slice(0, 12))}</code></td></tr>`,
        )
        .join("") ||
      '<tr><td colspan="6">No active transfer tickets.</td></tr>';
    $("#transferHistoryCount").textContent = `${data.history.length} recent`;
    $("#transferHistoryTable").innerHTML =
      data.history
        .map(
          (item) =>
            `<tr><td>${fmtTime(item.timestamp)}</td><td>${esc(item.player || item.playerName || "—")}</td><td>${esc(item.sourceServer || "—")}</td><td>${esc(item.destination || item.backend || "—")}</td><td>${item.port || "—"}</td><td>${esc(item.type || "transfer")}</td></tr>`,
        )
        .join("") || '<tr><td colspan="6">No transfer history.</td></tr>';
    const transactions = await api("/api/transfer-transactions?limit=500");
    $("#transferTransactionCount").textContent =
      `${transactions.transactions.length} recent`;
    $("#transferTransactionsTable").innerHTML =
      transactions.transactions
        .map(
          (item) =>
            `<tr><td>${fmtTime(item.requested_at)}</td><td><b>${esc(item.player_name || "—")}</b><br><small>${esc(item.xuid || "")}</small></td><td>${esc(item.source_server || "—")} → ${esc(item.destination || "—")}</td><td>${item.proxy_port || "—"}</td><td><span class="status ${item.state === "arrived" ? "good" : item.state === "expired" || item.state === "failed" ? "bad" : "warn"}">${esc(item.state || "unknown")}</span></td><td>${fmtTime(item.updated_at)}</td><td>${esc(item.failure_reason || "")}</td></tr>`,
        )
        .join("") ||
      '<tr><td colspan="7">No transfer transactions yet.</td></tr>';
  } catch (_) {}
}

async function createTransferTicket() {
  const request = {
    destination: $("#transferDestination").value,
    sourceIp: $("#transferSourceIp").value.trim(),
    playerName: $("#transferPlayer").value.trim(),
    xuid: $("#transferXuid").value.trim(),
    sourceServer: $("#transferSourceServer").value.trim(),
  };
  try {
    const data = await api("/api/transfers", {
      method: "POST",
      body: JSON.stringify(request),
    });
    toast(`Ticket ready: ${data.host}:${data.ticket.port}`);
    refreshTransfers();
  } catch (error) {
    toast(error.message);
  }
}

$("#createTransferTicket").addEventListener("click", createTransferTicket);

async function refreshEvents() {
  if (!$("#view-events").classList.contains("active")) return;
  try {
    const data = await api("/api/events?limit=500");
    $("#eventCount").textContent = `${data.records.length} recent`;
    $("#eventsList").innerHTML =
      data.records
        .map(
          (event) =>
            `<div class="event ${esc(event.severity || "info")}"><b>${esc(event.type || "event")}</b><div>${esc(event.message || JSON.stringify(event))}</div><small>${fmtTime(event.timestamp)}${event.backend ? ` · ${esc(event.backend)}` : ""}${event.ip ? ` · ${esc(event.ip)}` : ""}</small></div>`,
        )
        .join("") || "<p>No events recorded.</p>";
  } catch (_) {}
}
function renderDiscord(discord) {
  $("#discordStatus").textContent = discord.enabled
    ? `${discord.mode} enabled`
    : "disabled";
  $("#discordDetails").innerHTML = [
    detail("Status", discord.enabled ? "Enabled" : "Disabled"),
    detail("Delivery Mode", discord.mode || "disabled"),
  ].join("");
}

function renderDashboardUsers() {
  $("#dashboardUsersTable").innerHTML = state.users.map((user) => {
    const protectedAccount = user.role === "owner" || user.managed;
    const actions = protectedAccount
      ? `<span class="muted">${user.role === "owner" ? "Owner account" : "Managed externally"}</span>`
      : `<div class="button-row compact"><button class="ghost user-edit" data-username="${esc(user.username)}">Edit</button><button class="danger user-delete" data-username="${esc(user.username)}">Delete</button></div>`;
    return `<tr>
      <td><b>${esc(user.username)}</b>${user.totpEnabled ? "<br><small>Two-step verification enabled</small>" : ""}</td>
      <td><span class="account-role">${esc(user.role)}</span></td>
      <td>${esc(user.authentication)}</td>
      <td><span class="status ${user.enabled ? "good" : "warn"}">${user.enabled ? "Active" : "Disabled"}</span></td>
      <td>${actions}</td>
    </tr>`;
  }).join("") || '<tr><td colspan="5">No dashboard accounts were found.</td></tr>';

  $$(".user-edit").forEach((button) => button.addEventListener("click", () => {
    openUserDialog(state.users.find((user) => user.username === button.dataset.username));
  }));
  $$(".user-delete").forEach((button) => button.addEventListener("click", async () => {
    const username = button.dataset.username;
    if (!window.confirm(`Delete the dashboard account “${username}”?`)) return;
    try {
      await api("/api/users", { method: "DELETE", body: JSON.stringify({ username }) });
      toast(`${username} was removed`);
      await refreshDashboardUsers();
    } catch (error) {
      toast(error.message);
    }
  }));
}

async function refreshDashboardUsers() {
  if (state.data?.principal?.role !== "owner") return;
  try {
    const data = await api("/api/users");
    state.users = data.users || [];
    renderDashboardUsers();
  } catch (error) {
    toast(error.message);
  }
}

function openUserDialog(user = null) {
  state.selectedUser = user || null;
  const editing = Boolean(user);
  $("#userDialogTitle").textContent = editing ? "Edit Team Account" : "Add Team Account";
  $("#userDialogDescription").textContent = editing
    ? `Update ${user.username}’s access or reset their password.`
    : "Create an individual dashboard login.";
  $("#userUsername").value = user?.username || "";
  $("#userUsername").disabled = editing;
  $("#userRole").value = user?.role || "operator";
  $("#userPassword").value = "";
  $("#userPassword").required = !editing;
  $("#userPasswordHelp").textContent = editing
    ? "Leave blank to keep the current password. New passwords need at least 12 characters."
    : "At least 12 characters.";
  $("#userEnabled").checked = user?.enabled ?? true;
  $("#userEnabledLabel").hidden = !editing;
  $("#saveUserButton").textContent = editing ? "Save Changes" : "Create Account";
  $("#userDialog").showModal();
}

async function saveDashboardUser(event) {
  event.preventDefault();
  const editing = Boolean(state.selectedUser);
  const payload = {
    username: editing ? state.selectedUser.username : $("#userUsername").value.trim(),
    role: $("#userRole").value,
    password: $("#userPassword").value,
    enabled: $("#userEnabled").checked,
  };
  try {
    await api("/api/users", {
      method: editing ? "PUT" : "POST",
      body: JSON.stringify(payload),
    });
    $("#userDialog").close();
    toast(editing ? `${payload.username} was updated` : `${payload.username} can now sign in`);
    await refreshDashboardUsers();
  } catch (error) {
    toast(error.message);
  }
}

async function control(command, argument = "", argument2 = "") {
  try {
    await api("/api/control", {
      method: "POST",
      body: JSON.stringify({ command, argument, argument2 }),
    });
    toast(`Queued: ${command} ${argument}`);
    setTimeout(refreshState, 700);
  } catch (error) {
    toast(error.message);
  }
}
$$("[data-command]").forEach((button) =>
  button.addEventListener("click", () =>
    control(button.dataset.command, button.dataset.argument || ""),
  ),
);
$("#banButton").addEventListener("click", () =>
  control("ban", $("#banIp").value.trim(), $("#banSeconds").value),
);
$("#unbanButton").addEventListener("click", () =>
  control("unban", $("#banIp").value.trim()),
);
$("#riskResetButton").addEventListener("click", () =>
  control("risk_reset", $("#banIp").value.trim()),
);
$("#enableBackend").addEventListener("click", () =>
  control("backend", $("#backendSelect").value, "true"),
);
$("#disableBackend").addEventListener("click", () =>
  control("backend", $("#backendSelect").value, "false"),
);
$("#applyLiveConfig").addEventListener("click", applyLiveConfig);
$("#applyProtectionProfiles").addEventListener(
  "click",
  applyProtectionProfiles,
);
$("#rollbackLiveConfig").addEventListener("click", rollbackLiveConfig);
$("#reloadLiveConfig").addEventListener("click", () => control("reload"));
$("#downloadSupportBundle").addEventListener("click", async () => {
  try {
    const response = await fetch("/api/support-bundle", {
      method: "POST",
      headers: { Authorization: `Bearer ${state.token}` },
    });
    if (!response.ok) throw new Error(await response.text());
    const blob = await response.blob();
    const link = document.createElement("a");
    link.href = URL.createObjectURL(blob);
    link.download = "NinjOS-Edge-Fabric-v7.3.8-Support.zip";
    link.click();
    URL.revokeObjectURL(link.href);
    toast("Redacted support bundle generated");
  } catch (error) {
    toast(error.message);
  }
});
$("#backendConnectionMode").addEventListener("change", updateBackendModeHelp);
$("#addBackendButton").addEventListener("click", () => openBackendDialog());
$("#closeBackendDialog").addEventListener("click", () =>
  $("#backendDialog").close(),
);
$("#backendForm").addEventListener("submit", saveBackend);
$("#testBackendButton").addEventListener("click", testBackendForm);
$("#saveRegistrySettings").addEventListener("click", saveRegistrySettings);
$("#saveUnifiedConfig").addEventListener("click", saveUnifiedConfiguration);
$("#reloadUnifiedConfig").addEventListener(
  "click",
  refreshUnifiedConfiguration,
);
$("#saveManagedSettings").addEventListener("click", saveManagedSettings);
$("#reloadManagedSettings").addEventListener("click", refreshUnifiedConfiguration);
$("#closeSecretDialog").addEventListener("click", () => $("#secretDialog").close());
$("#secretMode").addEventListener("change", syncSecretEditorMode);
$("#secretForm").addEventListener("submit", (event) => submitSecretChange(event, false));
$("#generateSecret").addEventListener("click", () => submitSecretChange(null, true));
$("#companionBackendSelect").addEventListener("change", renderSelectedCompanion);
$("#setCompanionBackendSecret").addEventListener("click", () => {
  const id = $("#companionBackendSelect").value;
  const secret = state.secrets.find((item) => item.id === `backend.${id}.companion_secret`);
  if (secret) openSecretEditor(secret);
  else toast("No backend secret entry was found.");
});
$("#uploadCompanionArtifact").addEventListener("click", uploadCompanionArtifact);
$("#removeCompanionArtifact").addEventListener("click", removeCompanionArtifact);
$("#downloadCompanionProperties").addEventListener("click", () => downloadCompanion("properties"));
$("#downloadCompanionPackage").addEventListener("click", () => downloadCompanion("package"));
$("#downloadCompanionSource").addEventListener("click", () => downloadCompanion("source"));
$("#refreshButton").addEventListener("click", refreshAll);
$("#addUserButton").addEventListener("click", () => openUserDialog());
$("#closeUserDialog").addEventListener("click", () => $("#userDialog").close());
$("#userForm").addEventListener("submit", saveDashboardUser);

async function refreshAll() {
  await refreshState();
  await Promise.all([
    refreshPresence(),
    refreshPackets(),
    refreshPolicy(),
    refreshTransfers(),
    refreshEvents(),
    refreshAudit(),
    refreshBackendRegistry(),
    refreshUnifiedConfiguration(),
    refreshDashboardUsers(),
  ]);
}
$("#ownerAccountForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  const password = $("#accountNewPassword").value;
  if (password !== $("#accountConfirmPassword").value) {
    toast("The two new passwords do not match.");
    return;
  }
  try {
    const data = await api("/api/account", {
      method: "PUT",
      body: JSON.stringify({
        currentPassword: $("#accountCurrentPassword").value,
        username: $("#accountUsername").value.trim(),
        password,
      }),
    });
    state.token = data.token;
    state.recoverySession = false;
    sessionStorage.setItem("ninjos_dashboard_token", state.token);
    $("#usernameInput").value = data.principal.username;
    $("#accountUsername").value = data.principal.username;
    $("#accountCurrentPassword").value = "";
    $("#accountNewPassword").value = "";
    $("#accountConfirmPassword").value = "";
    startPolling();
    toast("Owner username and password updated");
    setTimeout(refreshAll, 50);
  } catch (error) {
    toast(error.message);
  }
});

window.addEventListener("resize", drawTrafficChart);
initializeAuthentication();
