// Hopper operator console — a dependency-free SPA over the control-plane API.
const $ = (sel) => document.querySelector(sel);

const cfg = {
  url: localStorage.getItem("hopper.url") || location.origin,
  token: localStorage.getItem("hopper.token") || "",
};
let filter = "";
let searchImage = "";
let timer = null;

// ---- API ----
async function api(path, opts = {}) {
  const headers = { ...(opts.headers || {}) };
  if (cfg.token) headers["Authorization"] = "Bearer " + cfg.token;
  if (opts.body) headers["Content-Type"] = "application/json";
  const res = await fetch(cfg.url.replace(/\/$/, "") + path, { ...opts, headers });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(`${res.status} ${res.statusText} — ${text}`);
  }
  return res.status === 204 ? null : res.json();
}

// ---- rendering ----
function setConn(ok) {
  $("#conn-dot").className = "dot " + (ok ? "dot-on" : "dot-off");
  $("#conn-text").textContent = ok ? cfg.url.replace(/^https?:\/\//, "") : "disconnected";
}

function fmtTime(s) {
  if (!s) return "—";
  const d = new Date(s);
  const diff = (Date.now() - d.getTime()) / 1000;
  if (diff < 60) return `${Math.floor(diff)}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  return d.toLocaleString();
}

function jobDuration(j) {
  if (!j.started_at || !j.finished_at) return "—";
  const s = new Date(j.started_at).getTime(), e = new Date(j.finished_at).getTime();
  if (!(e >= s)) return "—";
  const sec = Math.round((e - s) / 1000);
  if (sec < 60) return `${sec}s`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m${sec % 60}s`;
  return `${Math.floor(sec / 3600)}h${Math.floor((sec % 3600) / 60)}m`;
}

function renderJobs(jobs) {
  const body = $("#jobs-body");
  if (!jobs.length) {
    body.innerHTML = `<tr><td colspan="9" class="empty">No ${filter || ""} jobs yet.</td></tr>`;
    return;
  }
  body.innerHTML = jobs.map((j) => `
    <tr data-id="${j.id}">
      <td class="mono">${j.id.replace("job_", "")}</td>
      <td class="mono">${esc(j.image)}</td>
      <td><span class="badge s-${j.status}">${j.status.replace("_", " ")}</span></td>
      <td class="mono">${j.claimed_by ? j.claimed_by.replace("wrk_", "") : "—"}</td>
      <td>${j.attempts}/${j.max_attempts}</td>
      <td>${j.exit_code ?? "—"}</td>
      <td class="muted">${jobDuration(j)}</td>
      <td class="muted">${fmtTime(j.updated_at)}</td>
      <td>›</td>
    </tr>`).join("");
  body.querySelectorAll("tr").forEach((tr) =>
    tr.addEventListener("click", () => showDetail(tr.dataset.id)));
}

let lastWorkers = [];
function renderFleet(workers) {
  lastWorkers = workers;
  $("#fleet-count").textContent = workers.length;
  const el = $("#fleet");
  if (!workers.length) { el.innerHTML = `<p class="muted">No nodes registered.</p>`; return; }
  el.innerHTML = workers.map((w) => {
    const color = w.status === "online" ? "var(--green)" : w.status === "dead" ? "var(--red)" : "var(--iron)";
    const t = w.telemetry || {};
    const load = t.load1 != null ? `load ${t.load1.toFixed(2)}` : "";
    const slots = t.slots != null ? `${t.running || 0}/${t.slots} slots` : "";
    const imgs = t.images ? `${t.images.length} img` : "";
    const meta = [load, slots, imgs].filter(Boolean).join(" · ");
    return `<div class="node" data-id="${w.id}">
      <span class="dot" style="background:${color}"></span>
      <span class="name">${esc(w.hostname)}</span>
      <span class="labels">${meta || (w.labels || []).join(", ") || "general"}</span>
    </div>`;
  }).join("");
  el.querySelectorAll(".node").forEach((n) =>
    n.addEventListener("click", () => showNode(n.dataset.id)));
}

function showNode(id) {
  const w = lastWorkers.find((x) => x.id === id);
  if (!w) return;
  const t = w.telemetry || {};
  const imgs = (t.images || []).map((i) => `  ${i.repo}  (${i.size_mb}MB)`).join("\n") || "  (none reported)";
  $("#node-title").textContent = w.hostname + "  ·  " + w.status;
  $("#node-body").textContent =
    `worker:   ${w.id}\nlabels:   ${(w.labels || []).join(", ") || "—"}\n` +
    `cpus:     ${t.cpus ?? "—"}\nload:     ${t.load1 ?? "—"} / ${t.load5 ?? "—"} / ${t.load15 ?? "—"}\n` +
    `memory:   ${t.mem_free_mb ?? "—"} / ${t.mem_total_mb ?? "—"} MB free\n` +
    `disk:     ${t.disk_free_mb ?? "—"} MB free\nslots:    ${t.running ?? 0}/${t.slots ?? "—"} busy\n` +
    `heartbeat:${w.last_heartbeat || "—"}\n\ncached images (${(t.images || []).length}):\n${imgs}`;
  $("#node-detail").showModal();
}

function renderStats(jobs) {
  const c = { queued: 0, in_flight: 0, done: 0, failed: 0 };
  jobs.forEach((j) => { if (c[j.status] !== undefined) c[j.status]++; });
  $("#stats").innerHTML = `
    <div class="stat"><div class="n" style="color:var(--blue)">${c.in_flight}</div><div class="l">running</div></div>
    <div class="stat"><div class="n" style="color:var(--muted)">${c.queued}</div><div class="l">queued</div></div>
    <div class="stat"><div class="n" style="color:var(--green)">${c.done}</div><div class="l">done</div></div>
    <div class="stat"><div class="n" style="color:var(--red)">${c.failed}</div><div class="l">failed</div></div>`;
}

function esc(s) { return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c])); }

// ---- detail ----
let currentJob = null;
async function showDetail(id) {
  try {
    const j = await api("/api/jobs/" + id);
    currentJob = j;
    $("#d-title").textContent = j.id;
    $("#d-body").textContent = JSON.stringify(j, null, 2);
    $("#d-cancel").style.display = ["queued", "in_flight"].includes(j.status) ? "" : "none";
    $("#job-detail").showModal();
  } catch (e) { alert(e.message); }
}

// ---- refresh loop ----
async function refresh() {
  try {
    const params = new URLSearchParams();
    if (filter) params.set("status", filter);
    if (searchImage) params.set("image", searchImage);
    const qs = params.toString();
    const [jobs, workers] = await Promise.all([
      api("/api/jobs" + (qs ? "?" + qs : "")),
      api("/api/workers"),
    ]);
    setConn(true);
    renderJobs(jobs);
    renderFleet(workers);
    // stats always over all jobs
    const all = filter ? await api("/api/jobs") : jobs;
    renderStats(all);
  } catch (e) {
    setConn(false);
    $("#jobs-body").innerHTML = `<tr><td colspan="9" class="empty">${esc(e.message)}</td></tr>`;
  }
}

function startLoop() { clearInterval(timer); refresh(); timer = setInterval(refresh, 3000); }

// ---- events ----
$("#settings-btn").addEventListener("click", () => {
  $("#cfg-url").value = cfg.url;
  $("#cfg-token").value = cfg.token;
  $("#settings").showModal();
});
$("#settings-form").addEventListener("submit", (e) => {
  if (e.submitter && e.submitter.value === "save") {
    cfg.url = $("#cfg-url").value.trim() || location.origin;
    cfg.token = $("#cfg-token").value.trim();
    localStorage.setItem("hopper.url", cfg.url);
    localStorage.setItem("hopper.token", cfg.token);
    startLoop();
  }
});

$("#submit-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const msg = $("#submit-msg");
  const spec = {
    image: $("#f-image").value.trim(),
    command: $("#f-cmd").value.trim() ? $("#f-cmd").value.trim().split(/\s+/) : [],
    labels: $("#f-labels").value.trim() ? $("#f-labels").value.split(",").map((s) => s.trim()).filter(Boolean) : [],
    priority: parseInt($("#f-prio").value || "0", 10),
  };
  try {
    const job = await api("/api/jobs", { method: "POST", body: JSON.stringify(spec) });
    msg.textContent = `Queued ${job.id} ▾`;
    msg.className = "msg ok";
    $("#f-cmd").value = "";
    refresh();
  } catch (err) {
    msg.textContent = err.message;
    msg.className = "msg err";
  }
});

$("#filters").addEventListener("click", (e) => {
  if (!e.target.dataset.status && e.target.dataset.status !== "") return;
  if (e.target.tagName !== "BUTTON") return;
  filter = e.target.dataset.status;
  document.querySelectorAll(".chip").forEach((c) => c.classList.toggle("active", c === e.target));
  refresh();
});

$("#node-close").addEventListener("click", () => $("#node-detail").close());
let searchTimer = null;
$("#job-search").addEventListener("input", (e) => {
  searchImage = e.target.value.trim();
  clearTimeout(searchTimer);
  searchTimer = setTimeout(refresh, 250);
});

$("#d-close").addEventListener("click", () => $("#job-detail").close());
$("#d-cancel").addEventListener("click", async () => {
  if (!currentJob) return;
  try { await api("/api/jobs/" + currentJob.id + "/cancel", { method: "POST" }); $("#job-detail").close(); refresh(); }
  catch (e) { alert(e.message); }
});

// ---- boot ----
setConn(false);
if (!cfg.token) $("#settings").showModal();
startLoop();
