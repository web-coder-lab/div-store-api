const API = window.DIV_API || 'https://div-store-api.onrender.com';

async function api(path, opts = {}) {
  const url = API + path;
  const headers = { ...(opts.headers || {}) };
  let body = opts.body;
  if (body && !(body instanceof FormData)) {
    headers['Content-Type'] = 'application/json';
    body = JSON.stringify(body);
  }
  const r = await fetch(url, { method: opts.method || 'GET', headers, body });
  const t = await r.text();
  let d = null;
  try { d = t ? JSON.parse(t) : null; } catch { throw new Error(t || r.statusText); }
  if (!r.ok) throw new Error((d && d.error) || t || ('HTTP ' + r.status));
  return d;
}

function studioEmail() {
  return localStorage.getItem('studio_email') || '';
}
function setStudioEmail(e) {
  localStorage.setItem('studio_email', e);
}

window.DivAPI = { api, API, studioEmail, setStudioEmail };
