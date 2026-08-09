const { api, API, studioEmail, setStudioEmail } = window.DivAPI;
const view = document.getElementById('view');
const hdr = document.getElementById('hdrTitle');
const GAME_HINTS = ['game', 'games', 'action', 'arcade'];

let tab = 'apps';
let stack = []; // {name, data}
let cats = [];
let allApps = [];

function isGame(a) {
  const n = [a.categoryName, a.name, ...(a.categoryNames || [])].join(' ').toLowerCase();
  return GAME_HINTS.some((g) => n.includes(g));
}
function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
function icon(url) {
  return url
    ? `<img src="${esc(url)}" alt="" onerror="this.style.opacity=.3"/>`
    : `<div style="aspect-ratio:1;border-radius:12px;background:var(--s2);display:grid;place-items:center;color:var(--p)">App</div>`;
}
function appCard(a) {
  const id = a.apkId || a.id;
  return `<article class="card" data-open-app="${esc(id)}">
    ${icon(a.iconUrl)}
    <div class="n">${esc(a.name)}</div>
    <div class="c">${esc(a.categoryName || 'App')}</div>
    <div class="meta"><span>★ ${a.rating || '—'}</span><span>↓ ${a.downloads || 0}</span></div>
  </article>`;
}

async function loadCatalog() {
  const [apps, categories] = await Promise.all([
    api('/api/apps'),
    api('/api/categories'),
  ]);
  allApps = Array.isArray(apps) ? apps : [];
  cats = Array.isArray(categories) ? categories : [];
}

function renderApps(filterCat) {
  hdr.textContent = 'DIV STORE';
  const list = allApps.filter((a) => !isGame(a));
  const filtered = filterCat
    ? list.filter((a) => (a.categoryName || '') === filterCat || (a.categoryNames || []).includes(filterCat))
    : list;
  const nonGameCats = cats.filter((c) => !GAME_HINTS.some((g) => (c.name || '').toLowerCase().includes(g)));
  view.innerHTML = `
    <div class="chips">
      <button type="button" class="chip ${!filterCat ? 'on' : ''}" data-cat="">All</button>
      ${nonGameCats.map((c) => `<button type="button" class="chip ${filterCat === c.name ? 'on' : ''}" data-cat="${esc(c.name)}">${esc(c.name)}</button>`).join('')}
    </div>
    ${filtered.length ? `<div class="grid">${filtered.map(appCard).join('')}</div>` : `<div class="empty">No apps yet</div>`}
  `;
  view.querySelectorAll('[data-cat]').forEach((b) =>
    b.addEventListener('click', () => renderApps(b.getAttribute('data-cat') || null))
  );
  bindAppCards();
}

function renderGames() {
  hdr.textContent = 'Games';
  const list = allApps.filter(isGame);
  view.innerHTML = list.length
    ? `<h1 class="page">Games</h1><div class="grid">${list.map(appCard).join('')}</div>`
    : `<h1 class="page">Games</h1><div class="empty">No games yet</div>`;
  bindAppCards();
}

async function renderResearch() {
  hdr.textContent = 'Research';
  const email = studioEmail();
  let dev = null;
  if (email) {
    try {
      const r = await api('/api/developers/by-email?email=' + encodeURIComponent(email));
      dev = r.developer;
    } catch (_) {}
  }
  view.innerHTML = `
    <h1 class="page">Research</h1>
    <p class="dim">Developer Studio & resources</p>
    <div style="height:12px"></div>
    ${
      dev
        ? `<div class="panel">
            <h2>${esc(dev.name || dev.companyName)}</h2>
            <p class="dim">${esc(dev.email || email)}</p>
            <button type="button" class="btn" id="goSubmit" style="margin-top:12px">Submit APK</button>
            <button type="button" class="btn ghost" id="goDev">Public profile</button>
          </div>`
        : `<div class="panel">
            <h2>Developer Studio</h2>
            <p class="dim">Create a publisher account to submit APKs. Upload stays locked until you register.</p>
            <button type="button" class="btn" id="goReg" style="margin-top:12px">Create account</button>
          </div>`
    }
    <a class="legal" href="${API}/terms" target="_blank" rel="noopener">Terms of Service</a>
    <a class="legal" href="${API}/privacy" target="_blank" rel="noopener">Privacy Policy</a>
    <a class="legal" href="${API}/api/health" target="_blank" rel="noopener">API Health</a>
  `;
  document.getElementById('goReg')?.addEventListener('click', renderRegister);
  document.getElementById('goSubmit')?.addEventListener('click', renderSubmit);
  document.getElementById('goDev')?.addEventListener('click', () => renderDeveloper(dev.slug));
}

function renderRegister() {
  hdr.textContent = 'Studio';
  view.innerHTML = `
    <button type="button" class="back" id="bk">← Back</button>
    <h1 class="page">Developer Studio</h1>
    <label>Company name</label>
    <input id="rName" placeholder="Husdain Web"/>
    <label>Description</label>
    <textarea id="rDesc" rows="3" placeholder="About your studio"></textarea>
    <label>Email</label>
    <input id="rEmail" type="email" placeholder="you@email.com"/>
    <label>Icon URL (optional)</label>
    <input id="rIcon" placeholder="https://…"/>
    <button type="button" class="btn" id="rGo">Create account</button>
    <p class="err" id="rErr"></p>
  `;
  document.getElementById('bk').onclick = () => showTab('research');
  document.getElementById('rGo').onclick = async () => {
    const err = document.getElementById('rErr');
    err.textContent = '';
    try {
      await api('/api/developers/register', {
        method: 'POST',
        body: {
          companyName: document.getElementById('rName').value.trim(),
          description: document.getElementById('rDesc').value.trim(),
          email: document.getElementById('rEmail').value.trim(),
          iconUrl: document.getElementById('rIcon').value.trim(),
        },
      });
      setStudioEmail(document.getElementById('rEmail').value.trim());
      showTab('research');
    } catch (e) {
      err.textContent = e.message;
    }
  };
}

function renderSubmit() {
  if (!studioEmail()) {
    alert('Create Developer Studio account first');
    return renderRegister();
  }
  hdr.textContent = 'Submit';
  let step = 1;
  const state = { iconUrl: '', apkUrl: '', appName: '', packageName: '', description: '', categories: [] };

  function paint() {
    if (step === 1) {
      view.innerHTML = `
        <button type="button" class="back" id="bk">← Back</button>
        <p class="dim">Step 1 / 4 · Media</p>
        <label>Icon URL</label>
        <input id="sIcon" value="${esc(state.iconUrl)}" placeholder="https://…/icon.png"/>
        <label>APK URL (or host file on GitHub / CDN ending in .apk)</label>
        <input id="sApk" value="${esc(state.apkUrl)}" placeholder="https://…/app.apk"/>
        <p class="dim">Web build: use APK URL. Native app also supports direct .apk file upload.</p>
        <button type="button" class="btn" id="next">Continue</button>
      `;
      document.getElementById('bk').onclick = () => showTab('research');
      document.getElementById('next').onclick = () => {
        state.iconUrl = document.getElementById('sIcon').value.trim();
        state.apkUrl = document.getElementById('sApk').value.trim();
        if (!state.iconUrl || !state.apkUrl) return alert('Icon URL and APK URL required');
        if (!state.apkUrl.toLowerCase().includes('.apk') && !state.apkUrl.startsWith('http')) {
          return alert('APK URL should be http(s) and preferably end with .apk');
        }
        step = 2;
        paint();
      };
    } else if (step === 2) {
      view.innerHTML = `
        <button type="button" class="back" id="bk">← Back</button>
        <p class="dim">Step 2 / 4 · Identity</p>
        <label>App name</label>
        <input id="sName" value="${esc(state.appName)}"/>
        <label>Package name</label>
        <input id="sPkg" value="${esc(state.packageName)}" placeholder="com.example.app"/>
        <label>Description</label>
        <textarea id="sDesc" rows="4">${esc(state.description)}</textarea>
        <button type="button" class="btn" id="next">Continue</button>
      `;
      document.getElementById('bk').onclick = () => { step = 1; paint(); };
      document.getElementById('next').onclick = () => {
        state.appName = document.getElementById('sName').value.trim();
        state.packageName = document.getElementById('sPkg').value.trim();
        state.description = document.getElementById('sDesc').value.trim();
        if (!state.appName || !state.packageName || !state.description) return alert('Fill all fields');
        step = 3;
        paint();
      };
    } else if (step === 3) {
      view.innerHTML = `
        <button type="button" class="back" id="bk">← Back</button>
        <p class="dim">Step 3 / 4 · Categories (max 4)</p>
        <div class="filters" id="catBox">
          ${cats.map((c) => `<button type="button" class="filter ${state.categories.includes(c.name) ? 'on' : ''}" data-n="${esc(c.name)}">${esc(c.name)}</button>`).join('')}
        </div>
        <button type="button" class="btn" id="next">Submit</button>
        <p class="err" id="sErr"></p>
      `;
      document.getElementById('bk').onclick = () => { step = 2; paint(); };
      document.getElementById('catBox').onclick = (e) => {
        const b = e.target.closest('[data-n]');
        if (!b) return;
        const n = b.getAttribute('data-n');
        if (state.categories.includes(n)) state.categories = state.categories.filter((x) => x !== n);
        else {
          if (state.categories.length >= 4) return alert('Maximum 4 categories');
          state.categories.push(n);
        }
        paint();
      };
      document.getElementById('next').onclick = async () => {
        const err = document.getElementById('sErr');
        err.textContent = '';
        if (!state.categories.length) return alert('Pick at least 1 category');
        try {
          await api('/api/submit', {
            method: 'POST',
            body: {
              appName: state.appName,
              packageName: state.packageName,
              description: state.description,
              iconUrl: state.iconUrl,
              apkUrl: state.apkUrl,
              developerEmail: studioEmail(),
              categories: state.categories,
            },
          });
          step = 4;
          paint();
        } catch (e) {
          err.textContent = e.message;
        }
      };
    } else {
      view.innerHTML = `
        <div style="text-align:center;padding:40px 12px">
          <div style="font-size:48px;color:var(--ok)">✓</div>
          <h1 class="page">Thanks for submit your APK</h1>
          <p class="dim">Please wait approval · up to 24 hours</p>
          <button type="button" class="btn" id="home" style="margin-top:20px">Go back store</button>
        </div>`;
      document.getElementById('home').onclick = () => showTab('apps');
    }
  }
  paint();
}

async function renderDetail(id) {
  hdr.textContent = 'App';
  view.innerHTML = `<div class="empty">Loading…</div>`;
  try {
    const a = await api('/api/apps/' + encodeURIComponent(id));
    view.innerHTML = `
      <button type="button" class="back" id="bk">← Back</button>
      <div class="detail-hero">
        ${icon(a.iconUrl)}
        <div>
          <div style="font-size:20px;font-weight:800">${esc(a.name)}</div>
          <div class="dim" style="color:var(--p);cursor:pointer" id="devLink">${esc(a.developer || a.developerSlug || '')}</div>
          <div class="dim" style="font-size:11px">${esc(a.packageName)}</div>
        </div>
      </div>
      <div class="stats">
        <div class="stat"><b>${esc(a.downloads || 0)}</b><span>Downloads</span></div>
        <div class="stat"><b>${a.rating || '—'}</b><span>Rating</span></div>
        <div class="stat"><b>${esc(a.size || '—')}</b><span>Size</span></div>
        <div class="stat"><b>${esc(a.version || '—')}</b><span>Ver</span></div>
      </div>
      <div class="pills">${(a.categoryNames || [a.categoryName]).filter(Boolean).map((c) => `<span class="pill">${esc(c)}</span>`).join('')}</div>
      <h3 style="margin:16px 0 6px">About</h3>
      <p class="dim" style="line-height:1.55">${esc(a.description)}</p>
      <button type="button" class="btn" id="ins" style="margin-top:20px">Install</button>
    `;
    document.getElementById('bk').onclick = () => showTab(tab);
    document.getElementById('devLink').onclick = () => {
      if (a.developerSlug) renderDeveloper(a.developerSlug);
    };
    document.getElementById('ins').onclick = () => startInstall(a);
  } catch (e) {
    view.innerHTML = `<button type="button" class="back" id="bk">← Back</button><div class="err">${esc(e.message)}</div>`;
    document.getElementById('bk').onclick = () => showTab(tab);
  }
}

async function renderDeveloper(slug) {
  hdr.textContent = 'Developer';
  view.innerHTML = `<div class="empty">Loading…</div>`;
  try {
    const d = await api('/api/developers/' + encodeURIComponent(slug));
    const apps = d.apps || [];
    view.innerHTML = `
      <button type="button" class="back" id="bk">← Back</button>
      <div class="detail-hero">
        ${icon(d.logoUrl)}
        <div>
          <div style="font-size:18px;font-weight:800">${esc(d.name)}</div>
          <div class="dim" style="color:var(--p)">Verified Developer</div>
        </div>
      </div>
      <p class="dim" style="line-height:1.5">${esc(d.description || '')}</p>
      <h3 style="margin:18px 0 10px">All Apps</h3>
      ${apps.length ? `<div class="grid">${apps.map(appCard).join('')}</div>` : `<div class="empty">No apps</div>`}
    `;
    document.getElementById('bk').onclick = () => showTab(tab);
    bindAppCards();
  } catch (e) {
    view.innerHTML = `<button type="button" class="back" id="bk">← Back</button><div class="err">${esc(e.message)}</div>`;
    document.getElementById('bk').onclick = () => showTab(tab);
  }
}

function renderSearch() {
  hdr.textContent = 'Search';
  view.innerHTML = `
    <button type="button" class="back" id="bk">← Back</button>
    <input id="q" placeholder="Search apps…" autofocus/>
    <div id="res" class="grid" style="margin-top:12px"></div>
  `;
  document.getElementById('bk').onclick = () => showTab(tab);
  const res = document.getElementById('res');
  const paint = (q) => {
    const t = q.toLowerCase();
    const list = allApps.filter(
      (a) =>
        (a.name || '').toLowerCase().includes(t) ||
        (a.packageName || '').toLowerCase().includes(t) ||
        (a.categoryName || '').toLowerCase().includes(t)
    );
    res.innerHTML = list.map(appCard).join('') || '<div class="empty">No results</div>';
    bindAppCards();
  };
  document.getElementById('q').oninput = (e) => paint(e.target.value);
  paint('');
}

function bindAppCards() {
  view.querySelectorAll('[data-open-app]').forEach((el) => {
    el.addEventListener('click', () => renderDetail(el.getAttribute('data-open-app')));
  });
}

async function startInstall(a) {
  const mask = document.getElementById('installMask');
  const ring = document.getElementById('insRing');
  const pct = document.getElementById('insPct');
  const status = document.getElementById('insStatus');
  document.getElementById('insName').textContent = a.name || 'App';
  mask.classList.remove('hidden');
  let p = 0;
  const circ = 2 * Math.PI * 40;
  ring.style.strokeDasharray = String(circ);
  const tick = setInterval(() => {
    p = Math.min(0.95, p + 0.04);
    ring.style.strokeDashoffset = String(circ * (1 - p));
    pct.textContent = Math.round(p * 100) + '%';
    status.textContent = 'Downloading… ' + Math.round(p * 100) + '%';
  }, 120);
  try {
    const id = a.apkId || a.id;
    const r = await api('/api/apps/' + encodeURIComponent(id) + '/download', { method: 'POST' });
    clearInterval(tick);
    ring.style.strokeDashoffset = '0';
    pct.textContent = '100%';
    status.textContent = 'Opening download…';
    const url = r.downloadUrl;
    if (!url) throw new Error('No download URL');
    // Web: trigger browser download / open APK URL (Android Chrome may prompt install)
    const w = window.open(url, '_blank');
    if (!w) {
      const link = document.createElement('a');
      link.href = url;
      link.download = (a.packageName || 'app') + '.apk';
      link.click();
    }
    status.textContent = 'If download finished, open the APK to install (allow unknown apps).';
  } catch (e) {
    clearInterval(tick);
    status.textContent = e.message;
    status.style.color = 'var(--bad)';
  }
}
document.getElementById('insClose').onclick = () => {
  document.getElementById('installMask').classList.add('hidden');
  document.getElementById('insStatus').style.color = '';
};

async function showTab(name) {
  tab = name;
  document.querySelectorAll('.tab').forEach((t) => t.classList.toggle('on', t.dataset.tab === name));
  view.innerHTML = `<div class="empty">Loading…</div>`;
  try {
    if (!allApps.length) await loadCatalog();
    if (name === 'apps') renderApps(null);
    else if (name === 'games') renderGames();
    else renderResearch();
  } catch (e) {
    view.innerHTML = `<div class="err">${esc(e.message)}</div><button type="button" class="btn" id="retry">Retry</button>`;
    document.getElementById('retry').onclick = () => {
      allApps = [];
      showTab(name);
    };
  }
}

document.querySelectorAll('.tab').forEach((t) =>
  t.addEventListener('click', () => showTab(t.dataset.tab))
);
document.getElementById('btnSearch').onclick = () => {
  if (!allApps.length) loadCatalog().then(renderSearch).catch((e) => alert(e.message));
  else renderSearch();
};

showTab('apps');
