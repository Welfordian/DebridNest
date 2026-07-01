const crypto = require('crypto')

const streams = new Map()
const TTL_MS = 6 * 60 * 60 * 1000

function cleanup() {
  const now = Date.now()
  for (const [id, entry] of streams.entries()) {
    if (entry.expiresAt <= now) {
      streams.delete(id)
    }
  }
}

setInterval(cleanup, 60_000).unref()

function buildIinaUrl(targetUrl) {
  return `iina://open?url=${encodeURIComponent(targetUrl)}`
}

function buildPlayUrl(streamId, addonBaseUrl) {
  return `${addonBaseUrl.replace(/\/+$/, '')}/play/${streamId}`
}

function registerStream(meta = {}) {
  const id = crypto.randomBytes(12).toString('hex')
  streams.set(id, {
    directUrl: meta.directUrl || null,
    progressToken: meta.progressToken || null,
    label: meta.label || '',
    expiresAt: Date.now() + TTL_MS,
  })
  return id
}

function getStream(id) {
  const entry = streams.get(id)
  if (!entry) {
    return null
  }
  if (entry.expiresAt <= Date.now()) {
    streams.delete(id)
    return null
  }
  entry.expiresAt = Date.now() + TTL_MS
  return entry
}

function buildOpenPageHtml({ streamUrl, label, readyUrl, copyPageUrl }) {
  const safeLabel = escapeHtml(label || 'DebridNest stream')
  const safeStreamUrl = escapeHtml(streamUrl)
  const safeCopyPageUrl = escapeHtml(copyPageUrl)
  const iinaLaunchUrl = `${copyPageUrl}?format=iina`

  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Open in IINA</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; max-width: 640px; margin: 2rem auto; padding: 0 1rem; line-height: 1.5; }
    h1 { font-size: 1.25rem; }
    .actions { display: flex; flex-direction: column; gap: 0.75rem; margin: 1.5rem 0; }
    a, button { font-size: 1rem; padding: 0.75rem 1rem; border-radius: 8px; cursor: pointer; text-align: center; text-decoration: none; }
    .primary { background: #8A5AAB; color: white; border: 0; }
    .secondary { background: #f0f0f0; color: #222; border: 1px solid #ccc; }
    .disabled { opacity: 0.5; pointer-events: none; }
    input { width: 100%; font-size: 0.85rem; padding: 0.5rem; margin-top: 0.5rem; }
    .hint { color: #666; font-size: 0.9rem; }
    #status { margin: 1rem 0; }
    code { word-break: break-all; }
  </style>
</head>
<body>
  <h1>${safeLabel}</h1>
  <p id="status" class="hint">Waiting for DebridNest to buffer the stream…</p>
  <div class="actions">
    <a class="primary disabled" id="openIina" href="${escapeHtml(iinaLaunchUrl)}">Open in IINA</a>
    <button type="button" class="secondary" id="copyIina" disabled>Copy IINA URL</button>
    <button type="button" class="secondary" id="copyStream" disabled>Copy stream URL</button>
    <button type="button" class="secondary" id="copyPage">Copy this page URL</button>
  </div>
  <p class="hint">Direct stream URL (for VLC or other players):</p>
  <input id="streamUrl" readonly value="" />
  <p class="hint">Share link: <code>${safeCopyPageUrl}</code></p>
  <script>
    const readyUrl = ${JSON.stringify(readyUrl)};
    const fallbackPlayUrl = ${JSON.stringify(streamUrl)};
    const iinaLaunchUrl = ${JSON.stringify(iinaLaunchUrl)};
    const pageUrl = ${JSON.stringify(copyPageUrl)};
    const openIina = document.getElementById('openIina');
    const status = document.getElementById('status');
    const streamInput = document.getElementById('streamUrl');
    let directUrl = '';
    let iinaUrl = '';

    async function copy(text, btn, label) {
      try {
        await navigator.clipboard.writeText(text);
        btn.textContent = 'Copied!';
        setTimeout(() => { btn.textContent = label; }, 1500);
      } catch {
        window.prompt('Copy:', text);
      }
    }

    function markReady(url) {
      directUrl = url;
      iinaUrl = 'iina://open?url=' + encodeURIComponent(url);
      status.textContent = 'Stream ready — open in IINA or copy the direct URL below.';
      openIina.classList.remove('disabled');
      openIina.href = iinaLaunchUrl;
      streamInput.value = url;
      document.getElementById('copyIina').disabled = false;
      document.getElementById('copyStream').disabled = false;
      document.getElementById('copyIina').onclick = (e) => copy(iinaUrl, e.target, 'Copy IINA URL');
      document.getElementById('copyStream').onclick = (e) => copy(url, e.target, 'Copy stream URL');
    }

    document.getElementById('copyPage').onclick = (e) => copy(pageUrl, e.target, 'Copy this page URL');

    async function pollReady() {
      try {
        const res = await fetch(readyUrl);
        if (res.ok) {
          const data = await res.json();
          if (data.ready && data.url) {
            markReady(data.url);
            return;
          }
        }
      } catch {}
      setTimeout(pollReady, 2000);
    }

    pollReady();
  </script>
</body>
</html>`
}

function escapeHtml(value) {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

module.exports = {
  buildIinaUrl,
  buildPlayUrl,
  registerStream,
  getStream,
  buildOpenPageHtml,
}
