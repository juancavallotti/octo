// eip-go landing site — no framework, no build. Wires up diagrams, the samples
// gallery, YAML highlighting, and the release-please changelog feed.

import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.esm.min.mjs';

const GH_OWNER = 'juancavallotti';
const GH_REPO = 'eip-go';

/* ---------- Mermaid ---------- */
mermaid.initialize({
  startOnLoad: false,
  securityLevel: 'loose',
  theme: 'base',
  fontFamily: 'Inter, system-ui, sans-serif',
  themeVariables: {
    background: '#0b0f14',
    primaryColor: '#141b24',
    primaryBorderColor: '#243140',
    primaryTextColor: '#e6edf3',
    lineColor: '#4a5d70',
    secondaryColor: '#11161d',
    tertiaryColor: '#11161d',
    fontSize: '14px',
  },
  flowchart: { htmlLabels: true, curve: 'basis', padding: 14 },
});

/* ---------- YAML syntax highlighting ---------- */
function highlightYaml(codeEl, text) {
  codeEl.textContent = text;
  if (window.hljs) {
    codeEl.classList.add('language-yaml');
    window.hljs.highlightElement(codeEl);
  }
}

// Hero snippet.
const heroSrc = document.querySelector('[data-sample-src="hero"]');
const heroCode = document.querySelector('code[data-sample="hero"]');
if (heroSrc && heroCode) highlightYaml(heroCode, heroSrc.textContent.trim());

/* ---------- Samples gallery ---------- */
function buildSamples() {
  const tabsEl = document.getElementById('sample-tabs');
  const panelsEl = document.getElementById('sample-panels');
  if (!tabsEl || !panelsEl) return;

  // Each sample = a meta <script> + a matching src <script>, keyed by id.
  const metas = [...document.querySelectorAll('#sample-data [data-sample-meta]')];

  metas.forEach((meta, i) => {
    const id = meta.getAttribute('data-sample-meta');
    const src = document.querySelector(`[data-sample-src="${id}"]`);
    if (!src) return;

    const title = meta.getAttribute('data-title') || id;
    const blurb = meta.getAttribute('data-blurb') || '';
    const run = meta.getAttribute('data-run') || '';
    const pills = (meta.getAttribute('data-pills') || '').split('|').filter(Boolean);

    const tab = document.createElement('button');
    tab.className = 'tab' + (i === 0 ? ' active' : '');
    tab.textContent = title;
    tab.dataset.target = id;
    tabsEl.appendChild(tab);

    const panel = document.createElement('div');
    panel.className = 'sample-panel' + (i === 0 ? ' active' : '');
    panel.dataset.panel = id;
    panel.innerHTML = `
      <div class="sample-grid">
        <div>
          <div class="hero-card">
            <div class="titlebar">
              <span class="tdot"></span><span class="tdot"></span><span class="tdot"></span>
              <span class="fname">samples/${title}.yaml</span>
            </div>
            <pre><code class="language-yaml"></code></pre>
          </div>
        </div>
        <div class="sample-meta">
          <h3>${title}</h3>
          <p>${blurb}</p>
          <div class="pill-row">${pills.map((p) => `<span class="pill">${p}</span>`).join('')}</div>
          <div class="run-label">Run it</div>
          <div class="cmd"><span class="p">$</span> ${run}</div>
        </div>
      </div>`;
    panelsEl.appendChild(panel);

    highlightYaml(panel.querySelector('code'), src.textContent.replace(/^\n/, '').trimEnd());
  });

  tabsEl.addEventListener('click', (e) => {
    const tab = e.target.closest('.tab');
    if (!tab) return;
    const id = tab.dataset.target;
    tabsEl.querySelectorAll('.tab').forEach((t) => t.classList.toggle('active', t === tab));
    panelsEl.querySelectorAll('.sample-panel').forEach((p) => p.classList.toggle('active', p.dataset.panel === id));
  });
}
buildSamples();

/* ---------- Changelog feed (connects the docs to release-please output) ---------- */
async function loadChangelog() {
  const box = document.getElementById('changelog');
  const status = document.getElementById('changelog-status');
  if (!box) return;
  try {
    const res = await fetch(`https://raw.githubusercontent.com/${GH_OWNER}/${GH_REPO}/main/CHANGELOG.md`, { cache: 'no-store' });
    if (!res.ok) throw new Error(String(res.status));
    const md = await res.text();
    box.innerHTML = renderChangelog(md);
  } catch {
    // No release yet (or offline): degrade gracefully.
    if (status) {
      status.innerHTML =
        'No published changelog yet — release-please opens a release PR once Conventional Commits land on <code>main</code>. ' +
        'Until then, follow along on <a href="https://github.com/' + GH_OWNER + '/' + GH_REPO + '/commits/main" target="_blank" rel="noopener">the commit history ↗</a>.';
    }
  }
}

// Render only the most recent few releases of the CHANGELOG, lightly. Keeps the
// page dependency-free (no markdown library) while staying readable.
function renderChangelog(md) {
  const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  const lines = md.split('\n');
  const out = [];
  let releases = 0;
  for (const raw of lines) {
    const line = raw.replace(/\r$/, '');
    if (/^##\s+/.test(line)) {
      releases++;
      if (releases > 3) break;
      out.push(`<h3 style="color:var(--text);margin-top:${releases === 1 ? 0 : 22}px">${esc(line.replace(/^##\s+/, ''))}</h3>`);
    } else if (/^###\s+/.test(line)) {
      out.push(`<div style="font-family:var(--mono);font-size:0.78rem;letter-spacing:0.06em;text-transform:uppercase;color:var(--accent);margin:14px 0 6px">${esc(line.replace(/^###\s+/, ''))}</div>`);
    } else if (/^\*\s+/.test(line) || /^-\s+/.test(line)) {
      out.push(`<div style="color:var(--text-dim);font-size:0.93rem;padding-left:14px">• ${esc(line.replace(/^[\*-]\s+/, ''))}</div>`);
    }
  }
  return out.length ? out.join('\n') : '<p class="muted">Changelog is empty.</p>';
}
loadChangelog();

/* ---------- Render diagrams (explicit run is more reliable than startOnLoad) ---------- */
mermaid.run({ querySelector: '.mermaid' }).catch((e) => console.error('mermaid render failed', e));
