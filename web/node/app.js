// Hopper node console — polls the agent's local status API (same origin).
const $ = (s) => document.querySelector(s);

function esc(s) {
  return String(s).replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c]));
}

function uptime(startedAt) {
  if (!startedAt) return "—";
  let s = Math.max(0, (Date.now() - new Date(startedAt).getTime()) / 1000);
  const d = Math.floor(s / 86400); s -= d * 86400;
  const h = Math.floor(s / 3600); s -= h * 3600;
  const m = Math.floor(s / 60);
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m}m`;
  return `${m}m`;
}

async function tick() {
  try {
    const st = await (await fetch("/api/status")).json();
    const state = st.state || "offline";

    $("#state-dot").className = "dot " + state;
    $("#state").textContent = state;
    $("#big-state").textContent = state;
    $("#host").textContent = st.hostname || "—";
    $("#wid").textContent = st.worker_id || "(registering)";
    $("#ver").textContent = st.version || "";
    $("#s-done").textContent = st.jobs_done ?? 0;
    $("#s-failed").textContent = st.jobs_failed ?? 0;
    $("#s-labels").textContent = (st.labels && st.labels.length) ? st.labels.join(", ") : "general";
    $("#s-uptime").textContent = uptime(st.started_at);

    if (st.current_job) {
      const j = st.current_job;
      $("#current").innerHTML = `<div class="job-row">
        <span class="spinner"></span>
        <div>
          <div class="img mono">${esc(j.image)}</div>
          <div class="cmd mono">${esc((j.command || []).join(" ")) || "(default command)"}</div>
        </div>
      </div>`;
    } else {
      $("#current").innerHTML = `<p class="muted">Idle — waiting for work.</p>`;
    }

    $("#log").textContent = st.last_log || st.last_error || "—";
    $("#log").className = "log" + (st.last_log ? "" : " muted");
  } catch (e) {
    $("#state-dot").className = "dot offline";
    $("#state").textContent = "agent unreachable";
  }
}

tick();
setInterval(tick, 2000);
