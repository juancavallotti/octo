/**
 * The Settings window's script.
 *
 * A separate file rather than inline, because the page's CSP allows `script-src
 * 'self'` and not 'unsafe-inline' — a settings page that can execute a string is a
 * settings page one XSS away from repointing the runtime binary.
 *
 * Everything it does goes through window.octoSettings (see src/preload/settings.ts);
 * it holds no state of its own and re-renders from whatever the main process last
 * returned, so the file on disk is always what is on screen.
 */
const api = window.octoSettings;

const el = (id) => document.getElementById(id);

function binaryRow(b) {
  const row = document.createElement("div");
  row.className = "row";

  const name = document.createElement("div");
  name.className = "name";
  name.textContent = b.name;

  const detail = document.createElement("div");
  detail.className = "detail";

  const version = document.createElement("div");
  if (b.missing) {
    version.className = "warn";
    version.textContent = "Chosen file is missing — using the bundled one";
  } else if (b.version) {
    version.className = "version";
    version.textContent = b.version;
  } else {
    version.className = "warn";
    version.textContent = "Did not answer when asked its version";
  }

  const p = document.createElement("span");
  p.className = "path";
  p.textContent = b.override ?? b.bundled;
  p.title = b.override ?? b.bundled;

  detail.append(version, p);

  const choose = document.createElement("button");
  choose.textContent = "Choose…";
  choose.onclick = () => render(api.pickBinary(b.name));

  const reset = document.createElement("button");
  reset.textContent = "Use Bundled";
  reset.disabled = !b.override;
  reset.onclick = () => render(api.setBinary(b.name, null));

  row.append(name, detail, choose, reset);
  return row;
}

function draw(view) {
  const list = el("binaries");
  list.replaceChildren(...view.binaries.map(binaryRow));
  el("auto").checked = view.autoUpdateCheck;
  el("appVersion").textContent = `Octo ${view.appVersion}`;
  el("check").disabled = !view.canUpdate;
  el("check").title = view.canUpdate ? "" : "Updates are only available in a packaged build.";
}

/** Draw whatever a bridge call resolves to. Every mutation returns the new view. */
async function render(promise) {
  draw(await promise);
}

el("auto").onchange = (e) => render(api.setAutoUpdateCheck(e.target.checked));
el("check").onclick = () => api.checkForUpdates();
el("close").onclick = () => api.close();

void render(api.get());
