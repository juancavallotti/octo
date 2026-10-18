/**
 * The Container window's script. A separate file for the same reason as Settings':
 * the page's CSP allows `script-src 'self'` only.
 *
 * Everything goes through window.octoContainer (see src/preload/container.ts). The
 * main process re-checks every value this page sends, so nothing here is trusted to
 * have validated anything — it only keeps the buttons honest.
 */
const api = window.octoContainer;

const el = (id) => document.getElementById(id);

let docker = "missing";
let busy = null;

const DOCKER_TEXT = {
  running: (d) => `Docker ${d.server} is running.`,
  stopped: () => "Docker is not running. Start it to build, run and push; you can still export a Dockerfile.",
  missing: () => "Docker is not installed, or Octo cannot find it (see Settings). You can still export a Dockerfile.",
};

function drawDocker(d) {
  docker = d.state;
  const line = el("docker");
  line.className = d.state === "running" ? "hint ok" : "hint warn";
  line.textContent = DOCKER_TEXT[d.state](d);
  drawButtons();
  if (docker === "running") void refreshList();
  else drawList(null);
}

/** Docker buttons need docker and an idle window; Export needs only the idle window. */
function drawButtons() {
  for (const b of document.querySelectorAll("button[data-docker]")) {
    b.disabled = docker !== "running" || busy !== null;
  }
  el("export").disabled = busy !== null;
  el("cancel").disabled = busy === null;
}

function print(line) {
  const out = el("output");
  const stick = out.scrollTop + out.clientHeight >= out.scrollHeight - 4;
  out.textContent += `${line}\n`;
  if (stick) out.scrollTop = out.scrollHeight;
}

function drawVersions(v) {
  const select = el("version");
  const options = v.tags.map((t) => {
    const o = document.createElement("option");
    o.value = t.tag;
    const notes = [];
    if (t.tag === v.matching) notes.push("matches the editor");
    if (t.local && !t.remote) notes.push("local only");
    else if (t.local) notes.push("pulled");
    o.textContent = notes.length ? `${t.tag}  (${notes.join(", ")})` : t.tag;
    return o;
  });
  select.replaceChildren(...options);
  select.value = v.matching ?? v.tags[0]?.tag ?? "";
  select.onchange = () => drawVersionHint(v);
  drawVersionHint(v);
}

function drawVersionHint(v) {
  const hint = el("versionHint");
  const notes = [];
  if (v.offline) notes.push("Docker Hub did not answer, so only pulled versions are listed.");
  if (v.tags.length === 0) notes.push("No runtime versions are available.");
  if (v.matching && el("version").value !== v.matching) {
    notes.push(`The editor runs ${v.matching}; another version may not accept everything it lets you write.`);
  }
  hint.textContent = notes.join(" ");
  hint.className = notes.length ? "hint warn" : "hint";
}

function drawList(items) {
  const table = el("list");
  if (!items || items.length === 0) {
    const row = document.createElement("tr");
    const cell = document.createElement("td");
    cell.className = "empty";
    cell.textContent = items ? "Nothing yet." : "Docker is needed to list containers.";
    row.append(cell);
    table.replaceChildren(row);
    return;
  }
  table.replaceChildren(
    ...items.map((c) => {
      const row = document.createElement("tr");
      const name = document.createElement("td");
      const strong = document.createElement("div");
      strong.textContent = c.name;
      const image = document.createElement("div");
      image.className = "mono hint";
      image.textContent = c.image;
      name.append(strong, image);

      const status = document.createElement("td");
      status.textContent = c.status;
      status.className = c.state === "running" ? "ok" : "hint";

      const ports = document.createElement("td");
      ports.className = "mono";
      ports.textContent = c.httpPort ? `:${c.httpPort}` : "";
      ports.title = c.adminPort ? `Health: http://127.0.0.1:${c.adminPort}/healthz` : "";

      const buttons = document.createElement("td");
      buttons.className = "buttons";
      const open = button("Open", () => api.open(c.id));
      open.disabled = c.state !== "running" || !c.httpPort;
      const logs = button("Logs", () => api.follow(c.id));
      const remove = button("Remove", async () => {
        await api.remove(c.id);
        await refreshList();
      });
      buttons.append(open, logs, remove);

      row.append(name, status, ports, buttons);
      return row;
    }),
  );
}

function button(label, onclick) {
  const b = document.createElement("button");
  b.textContent = label;
  b.onclick = () => attempt(onclick);
  return b;
}

/** Run a bridge call, printing its failure rather than dropping it. */
async function attempt(fn) {
  try {
    return await fn();
  } catch (err) {
    // Electron prefixes a handler's error with the channel; the reason is what is left.
    const message = String(err?.message ?? err).replace(/^Error invoking remote method '[^']+': (Error: )?/, "");
    print(`— ${message}`);
    return undefined;
  }
}

async function refreshList() {
  if (docker !== "running") return drawList(null);
  drawList(await attempt(() => api.list()) ?? []);
}

const ref = () => el("ref").value.trim();
const version = () => el("version").value;

el("buildOnly").onclick = () => attempt(() => api.build(ref(), version()));
el("buildRun").onclick = () =>
  attempt(async () => {
    if (!(await api.build(ref(), version()))) return;
    const c = await api.run(ref());
    await refreshList();
    await api.follow(c.id);
  });
el("export").onclick = () => attempt(() => api.exportContext(version()));
el("pushBtn").onclick = () =>
  attempt(async () => {
    const target = el("target").value.trim() || ref();
    if (!(await api.build(ref(), version()))) return;
    await api.push(ref(), target);
    drawTargets([target, ...lastTargets.filter((t) => t !== target)]);
  });
el("cancel").onclick = () => api.cancel();
el("clear").onclick = () => {
  el("output").textContent = "";
  void api.unfollow();
};

let lastTargets = [];
function drawTargets(targets) {
  lastTargets = targets;
  el("targets").replaceChildren(
    ...targets.map((t) => {
      const o = document.createElement("option");
      o.value = t;
      return o;
    }),
  );
  if (!el("target").value && targets[0]) el("target").value = targets[0];
}

/** Bring the part a menu item named into view, and mark it for a moment. */
function focusOn(part) {
  const section = el(part === "export" ? "build" : part);
  if (!section) return;
  section.scrollIntoView({ block: "nearest" });
  section.classList.add("flash");
  setTimeout(() => section.classList.remove("flash"), 900);
  if (part === "push") el("target").focus();
  if (part === "export") el("export").focus();
}

api.onOutput(print);
api.onBusy((label) => {
  busy = label;
  drawButtons();
});
api.onDocker(drawDocker);
api.onFocus(focusOn);

/** Drawn on open and again on every return to the window: the folder may have changed. */
async function load() {
  const o = await api.overview();
  el("title").textContent = o.folder ? `Containers — ${o.folder.name}` : "Containers";
  if (!el("ref").value || el("ref").dataset.folder !== o.folder?.path) {
    el("ref").value = o.ref;
    el("ref").dataset.folder = o.folder?.path ?? "";
  }
  drawTargets(o.pushTargets);
  drawDocker(o.docker);
}

window.addEventListener("focus", () => void attempt(load));
void attempt(async () => {
  await load();
  focusOn(new URLSearchParams(location.search).get("focus") ?? "build");
  drawVersions(await api.versions());
});
