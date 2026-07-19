"use strict";

/* SteamOS NVIDIA Installer — wizard logic.
   Backend methods live on window.go.main.App; events arrive on "evt". */

const $ = (id) => document.getElementById(id);
const ORDER = ["check", "builder", "image", "build", "flash", "done"];

const state = {
  step: "check",
  sys: null,
  image: null,     // {path,name,sizeGB}
  output: null,    // {linuxPath,name,sizeGB}
  disk: null,      // selected disk number
  furthest: 0,     // highest step index reached
  confirmArmed: null,
};

function App() { return window.go.main.App; }

/* ------------------------------------------------------------ steps */

function show(step) {
  state.step = step;
  const idx = ORDER.indexOf(step);
  state.furthest = Math.max(state.furthest, idx);
  document.querySelectorAll(".pane").forEach(p =>
    p.classList.toggle("active", p.dataset.pane === step));
  document.querySelectorAll(".step").forEach(s => {
    const i = ORDER.indexOf(s.dataset.step);
    s.classList.toggle("active", s.dataset.step === step);
    s.classList.toggle("done", i < idx || (i <= state.furthest && i < idx));
  });
  if (step === "flash") enterFlash();
}

document.querySelectorAll(".step").forEach(s => {
  s.addEventListener("click", () => {
    const i = ORDER.indexOf(s.dataset.step);
    if (i <= state.furthest) show(s.dataset.step);
  });
});

/* ------------------------------------------------------------- logs */

function appendLog(id, msg, cls) {
  const el = $(id);
  if (el.dataset.fresh !== "1") { el.textContent = ""; el.dataset.fresh = "1"; }
  const line = document.createElement("span");
  if (cls) line.className = cls;
  line.textContent = msg + "\n";
  el.appendChild(line);
  if (el.childNodes.length > 3000) el.removeChild(el.firstChild);
  el.scrollTop = el.scrollHeight;
}

function fmtGB(b) { return (b / (1024 ** 3)).toFixed(2) + " GB"; }
function fmtMB(b) { return Math.round(b / (1024 ** 2)) + " MB"; }

function setProgress(prefix, cur, total) {
  const wrap = $(prefix + "-progresswrap");
  wrap.classList.remove("hidden");
  const pct = total > 0 ? Math.min(100, (cur / total) * 100) : 0;
  $(prefix + "-progressbar").style.width = pct.toFixed(1) + "%";
  $(prefix + "-progresstext").textContent =
    total > 0 ? `${fmtMB(cur)} / ${fmtMB(total)}  (${pct.toFixed(0)}%)` : fmtMB(cur);
}

/* ----------------------------------------------------- double confirm */

function armConfirm(btn, armedLabel, fn) {
  if (state.confirmArmed === btn) {
    state.confirmArmed = null;
    btn.textContent = btn.dataset.label;
    fn();
    return;
  }
  disarmConfirm();
  state.confirmArmed = btn;
  btn.dataset.label = btn.dataset.label || btn.textContent;
  btn.textContent = armedLabel;
  setTimeout(() => { if (state.confirmArmed === btn) disarmConfirm(); }, 5000);
}
function disarmConfirm() {
  const b = state.confirmArmed;
  if (b) { b.textContent = b.dataset.label; state.confirmArmed = null; }
}

/* ------------------------------------------------------- system check */

async function recheck() {
  const sys = await App().CheckSystem();
  state.sys = sys;
  const items = [
    ["Administrator rights", sys.isAdmin, sys.isAdmin ? "elevated" : "required"],
    ["Windows version", sys.windowsOK, "build " + sys.windowsBuild],
    ["Not in S Mode", sys.sModeOK, sys.sModeOK ? "full Windows" : "S Mode — Store apps only"],
    ["CPU virtualization", sys.virtOK, sys.virtInfo],
    ["Free disk space", sys.spaceOK, sys.freeSpaceGB.toFixed(0) + " GB free (need ~30)"],
    ["WSL 2", sys.wslInstalled, sys.wslInstalled ? "installed" : "not installed"],
    ["Build environment", sys.builderReady, sys.builderReady ? "ready" : "will be set up next"],
  ];
  $("checklist").innerHTML = "";
  for (const [name, ok, detail] of items) {
    const li = document.createElement("li");
    li.className = ok ? "ok" : (name === "Build environment" ? "pend" : "bad");
    li.innerHTML = `<span class="mark">${ok ? "✓" : (name === "Build environment" ? "•" : "✗")}</span>
      <span>${name}</span><span class="detail"></span>`;
    li.querySelector(".detail").textContent = detail;
    $("checklist").appendChild(li);
  }
  $("check-warnings").innerHTML = "";
  for (const w of sys.warnings) {
    const d = document.createElement("div");
    d.className = "w";
    d.textContent = w;
    $("check-warnings").appendChild(d);
  }
  const wslBtn = $("btn-install-wsl");
  wslBtn.classList.toggle("hidden", sys.wslInstalled);
  wslBtn.disabled = !sys.virtOK;
  wslBtn.title = sys.virtOK ? "" : "Enable virtualization in the UEFI/BIOS first (see warning below)";
  $("btn-check-next").disabled = !(sys.isAdmin && sys.windowsOK && sys.wslInstalled);
  return sys;
}

$("btn-recheck").addEventListener("click", recheck);

$("btn-install-wsl").addEventListener("click", async () => {
  $("wsl-logwrap").classList.remove("hidden");
  $("btn-install-wsl").disabled = true;
  const err = await App().StartWSLInstall();
  if (err) { appendLog("wsl-log", err, "err"); $("btn-install-wsl").disabled = false; }
});

$("btn-check-next").addEventListener("click", () => {
  show(state.sys && state.sys.builderReady ? "image" : "builder");
});

/* ------------------------------------------------------ builder setup */

$("btn-setup").addEventListener("click", async () => {
  $("btn-setup").disabled = true;
  $("btn-setup-cancel").classList.remove("hidden");
  const err = await App().StartSetup();
  if (err) {
    appendLog("setup-log", err, "err");
    $("btn-setup").disabled = false;
    $("btn-setup-cancel").classList.add("hidden");
  }
});

$("btn-setup-cancel").addEventListener("click", () => App().Cancel());
$("btn-builder-next").addEventListener("click", () => show("image"));

/* -------------------------------------------------------------- image */

$("btn-valve").addEventListener("click", () => App().OpenValveHelp());

async function acceptImage(promise) {
  try {
    const pick = await promise;
    if (!pick) return;
    state.image = pick;
    const card = $("image-info");
    card.classList.remove("hidden");
    card.innerHTML = `<span class="fc-icon">💿</span><span class="fc-name"></span><span class="fc-size"></span>`;
    card.querySelector(".fc-name").textContent = pick.name;
    card.querySelector(".fc-size").textContent = pick.sizeGB.toFixed(2) + " GB";
    $("btn-image-next").disabled = false;
  } catch (e) {
    state.image = null;
    $("btn-image-next").disabled = true;
    const card = $("image-info");
    card.classList.remove("hidden");
    card.innerHTML = `<span class="fc-icon">⚠</span><span class="fc-name err"></span>`;
    card.querySelector(".fc-name").textContent = String(e);
    card.querySelector(".fc-name").style.color = "var(--bad)";
  }
}

$("btn-browse").addEventListener("click", () => acceptImage(App().ChooseImage()));
$("dropzone").addEventListener("click", (e) => {
  if (e.target.id !== "btn-browse") acceptImage(App().ChooseImage());
});
$("btn-image-next").addEventListener("click", () => show("build"));

/* -------------------------------------------------------------- build */

function buildOpts() {
  return {
    imagePath: state.image.path,
    updateMode: document.querySelector('input[name="updmode"]:checked').value,
    trimCuda: $("opt-trimcuda").checked,
    skipSig: $("opt-skipsig").checked,
  };
}

$("btn-build").addEventListener("click", async () => {
  if (!state.image) { show("image"); return; }
  $("btn-build").disabled = true;
  $("btn-build-cancel").classList.remove("hidden");
  $("btn-build-next").disabled = true;
  const err = await App().StartBuild(buildOpts());
  if (err) {
    appendLog("build-log", err, "err");
    $("btn-build").disabled = false;
    $("btn-build-cancel").classList.add("hidden");
  }
});

$("btn-build-cancel").addEventListener("click", (e) =>
  armConfirm(e.target, "Really cancel the build?", () => App().Cancel()));
$("btn-build-next").addEventListener("click", () => show("flash"));

/* -------------------------------------------------------------- flash */

async function enterFlash() {
  try {
    const out = await App().GetBuildOutput();
    if (out) {
      state.output = out;
      const card = $("output-card");
      card.classList.remove("hidden");
      card.innerHTML = `<span class="fc-icon">📦</span><span class="fc-name"></span><span class="fc-size"></span>`;
      card.querySelector(".fc-name").textContent = out.name;
      card.querySelector(".fc-size").textContent = out.sizeGB.toFixed(2) + " GB";
    }
  } catch (e) {
    appendLog("flash-log", String(e), "err");
  }
  refreshDisks();
}

async function refreshDisks() {
  let disks = [];
  try { disks = await App().ListDisks() || []; }
  catch (e) { appendLog("flash-log", String(e), "err"); }
  const list = $("disklist");
  list.innerHTML = "";
  if (!disks.length) {
    list.innerHTML = `<div class="empty">No USB drives found — plug one in and refresh.</div>`;
    state.disk = null;
    $("btn-flash").disabled = true;
    return;
  }
  for (const d of disks) {
    const el = document.createElement("div");
    el.className = "disk";
    el.tabIndex = 0;
    el.setAttribute("role", "button");
    el.innerHTML = `<span class="d-icon">🖴</span>
      <span><div class="d-name"></div><div class="d-sub">Disk ${d.number} · USB</div></span>
      <span class="d-size"></span>`;
    el.querySelector(".d-name").textContent = d.friendlyName || "USB drive";
    el.querySelector(".d-size").textContent = d.sizeGB.toFixed(1) + " GB";
    el.addEventListener("keydown", (e) => {
      if (e.key === "Enter" || e.key === " ") { e.preventDefault(); el.click(); }
    });
    el.addEventListener("click", () => {
      state.disk = d.number;
      disarmConfirm();
      document.querySelectorAll(".disk").forEach(x => x.classList.remove("selected"));
      el.classList.add("selected");
      $("btn-flash").disabled = false;
    });
    list.appendChild(el);
  }
  state.disk = null;
  $("btn-flash").disabled = true;
}

$("btn-refresh-disks").addEventListener("click", refreshDisks);

$("btn-flash").addEventListener("click", (e) => {
  if (state.disk === null) return;
  armConfirm(e.target, "⚠ Click again to ERASE and flash", async () => {
    $("btn-flash").disabled = true;
    $("btn-refresh-disks").disabled = true;
    $("btn-flash-cancel").classList.remove("hidden");
    const err = await App().StartFlash(state.disk);
    if (err) {
      appendLog("flash-log", err, "err");
      $("btn-flash").disabled = false;
      $("btn-refresh-disks").disabled = false;
      $("btn-flash-cancel").classList.add("hidden");
    }
  });
});

$("btn-flash-cancel").addEventListener("click", (e) =>
  armConfirm(e.target, "Really abort mid-write?", () => App().Cancel()));

$("btn-flash-another").addEventListener("click", () => show("flash"));

/* ---------------------------------------------------- remove builder */

$("btn-remove-builder").addEventListener("click", (e) =>
  armConfirm(e.target, "Click again to delete (~20 GB)", async () => {
    try {
      const msg = await App().RemoveBuilder();
      appendLog("setup-log", msg || "Builder removed.");
      recheck();
    } catch (err) { alertLine(String(err)); }
  }));

function alertLine(msg) {
  const ch = { check: "wsl", builder: "setup", build: "build", flash: "flash" }[state.step];
  if (ch) appendLog(ch + "-log", msg, "err");
}

/* ------------------------------------------------------------- events */

function onEvent(e) {
  const routes = { wsl: "wsl-log", setup: "setup-log", build: "build-log", flash: "flash-log" };
  const logId = routes[e.chan];
  switch (e.type) {
    case "log":
      if (logId) appendLog(logId, e.msg);
      break;
    case "progress":
      if (e.chan === "setup") setProgress("setup", e.cur, e.total);
      if (e.chan === "build") setProgress("build", e.cur, e.total);
      if (e.chan === "flash") setProgress("flash", e.cur, e.total);
      break;
    case "done":
      if (e.chan === "wsl") {
        appendLog("wsl-log", e.msg, "okl");
        recheck();
      } else if (e.chan === "setup") {
        appendLog("setup-log", e.msg, "okl");
        $("setup-progresswrap").classList.add("hidden");
        $("btn-setup-cancel").classList.add("hidden");
        $("btn-builder-next").disabled = false;
        setTimeout(() => show("image"), 900);
      } else if (e.chan === "build") {
        appendLog("build-log", "Build complete: " + e.msg, "okl");
        $("btn-build-cancel").classList.add("hidden");
        $("btn-build").disabled = false;
        $("btn-build-next").disabled = false;
        setTimeout(() => show("flash"), 900);
      } else if (e.chan === "flash") {
        appendLog("flash-log", e.msg, "okl");
        $("btn-flash-cancel").classList.add("hidden");
        $("btn-refresh-disks").disabled = false;
        show("done");
      }
      break;
    case "error":
      if (logId) appendLog(logId, e.msg, "err");
      if (e.chan === "setup") {
        $("btn-setup").disabled = false;
        $("btn-setup-cancel").classList.add("hidden");
      } else if (e.chan === "build") {
        $("btn-build").disabled = false;
        $("btn-build-cancel").classList.add("hidden");
      } else if (e.chan === "flash") {
        $("btn-flash").disabled = false;
        $("btn-refresh-disks").disabled = false;
        $("btn-flash-cancel").classList.add("hidden");
      } else if (e.chan === "wsl") {
        $("btn-install-wsl").disabled = false;
      }
      break;
  }
}

/* --------------------------------------------------------------- init */

function ready() {
  window.runtime.EventsOn("evt", onEvent);

  // Native drag-and-drop of the recovery image (Wails file drop).
  if (window.runtime.OnFileDrop) {
    window.runtime.OnFileDrop((x, y, paths) => {
      if (state.step !== "image" || !paths || !paths.length) return;
      acceptImage(App().InspectImage(paths[0]));
    }, true);
  }

  show("check");
  recheck().then(sys => {
    if (sys.builderReady) state.furthest = Math.max(state.furthest, ORDER.indexOf("image"));
  });
}

(function waitForBackend(tries) {
  if (window.go && window.go.main && window.go.main.App && window.runtime) {
    ready();
  } else if (tries > 200) {
    document.body.innerHTML = "<p style='padding:40px'>Failed to connect to the app backend.</p>";
  } else {
    setTimeout(() => waitForBackend(tries + 1), 50);
  }
})(0);
