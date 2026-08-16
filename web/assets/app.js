/** DIV STORE web client — same-origin /api */
const API = '';

async function api(path) {
  const res = await fetch(API + path, {
    headers: { 'Accept': 'application/json', 'User-Agent': 'DivStoreWeb/1.0' },
  });
  const text = await res.text();
  let data;
  try { data = JSON.parse(text); } catch { data = text; }
  if (!res.ok) {
    const msg = (data && data.error) || text || res.statusText;
    throw new Error(typeof msg === 'string' ? msg : JSON.stringify(msg));
  }
  return data;
}

function appsFrom(data) {
  if (Array.isArray(data)) return data;
  if (data && Array.isArray(data.apps)) return data.apps;
  return [];
}

function catsFrom(data) {
  if (Array.isArray(data)) return data;
  if (data && Array.isArray(data.categories)) return data.categories;
  return [];
}

function letterIcon(name) {
  const l = (name || '?').trim().charAt(0).toUpperCase() || '?';
  return `<div class="app-icon letter" aria-hidden="true">${l}</div>`;
}

function iconHtml(app, cls = 'app-icon') {
  if (app.iconUrl) {
    return `<img class="${cls}" src="${escapeAttr(app.iconUrl)}" alt="" loading="lazy" onerror="this.outerHTML='${letterIcon(app.name).replace(/'/g, "\\'")}'" />`;
  }
  return letterIcon(app.name).replace('app-icon', cls);
}

function escapeAttr(s) {
  return String(s || '').replace(/&/g,'&amp;').replace(/"/g,'&quot;').replace(/</g,'&lt;');
}
function escapeHtml(s) {
  return String(s || '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

function appCard(app) {
  const id = app.id || app.apkId || '';
  const cat = app.categoryName || (app.categoryNames && app.categoryNames[0]) || '';
  const ver = app.version ? ` · v${escapeHtml(app.version)}` : '';
  return `<a class="app-card" href="/app.html?id=${encodeURIComponent(id)}">
    ${iconHtml(app)}
    <div class="app-meta">
      <h3>${escapeHtml(app.name || 'App')}</h3>
      <p>${escapeHtml(cat)}${ver}</p>
    </div>
  </a>`;
}

function featCard(app) {
  const id = app.id || app.apkId || '';
  const cat = app.categoryName || (app.categoryNames && app.categoryNames[0]) || '';
  return `<a class="feat-card" href="/app.html?id=${encodeURIComponent(id)}">
    ${iconHtml(app, 'letter')}
    <strong>${escapeHtml(app.name || 'App')}</strong>
    <p style="font-size:12px;color:var(--muted);margin-top:4px">${escapeHtml(cat)}</p>
  </a>`;
}

function setActiveNav() {
  const path = location.pathname.replace(/\/$/, '') || '/';
  document.querySelectorAll('.nav-links a[href]').forEach(a => {
    const href = a.getAttribute('href').replace(/\/$/, '') || '/';
    if (href === path || (path === '/index.html' && href === '/')) a.classList.add('active');
  });
}

document.addEventListener('DOMContentLoaded', setActiveNav);

window.DivStore = { api, appsFrom, catsFrom, appCard, featCard, escapeHtml, escapeAttr, iconHtml, letterIcon };
