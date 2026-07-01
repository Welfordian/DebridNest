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

function buildIinaUrl(streamUrl) {
  return `iina://weblink?url=${encodeURIComponent(streamUrl)}`
}

function registerStream(streamUrl, label = '') {
  const id = crypto.randomBytes(12).toString('hex')
  streams.set(id, {
    url: streamUrl,
    label,
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

function buildOpenPageHtml({ streamUrl, label, iinaUrl, copyPageUrl }) {
  const safeLabel = escapeHtml(label || 'DebridNest stream')
  const safeStreamUrl = escapeHtml(streamUrl)
  const safeIinaUrl = escapeHtml(iinaUrl)
  const safeCopyPageUrl = escapeHtml(copyPageUrl)

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
    input { width: 100%; font-size: 0.85rem; padding: 0.5rem; margin-top: 0.5rem; }
    .hint { color: #666; font-size: 0.9rem; }
    code { word-break: break-all; }
  </style>
</head>
<body>
  <h1>${safeLabel}</h1>
  <p class="hint">Use IINA on macOS when Stremio playback fails or for HDR/codec issues.</p>
  <div class="actions">
    <a class="primary" href="${safeIinaUrl}">Open in IINA</a>
    <button type="button" class="secondary" id="copyIina">Copy IINA URL</button>
    <button type="button" class="secondary" id="copyStream">Copy stream URL</button>
    <button type="button" class="secondary" id="copyPage">Copy this page URL</button>
  </div>
  <p class="hint">Stream URL (for VLC or other players):</p>
  <input id="streamUrl" readonly value="${safeStreamUrl}" />
  <p class="hint">Share link: <code>${safeCopyPageUrl}</code></p>
  <script>
    const iinaUrl = ${JSON.stringify(iinaUrl)};
    const streamUrl = ${JSON.stringify(streamUrl)};
    const pageUrl = ${JSON.stringify(copyPageUrl)};
    async function copy(text, btn, label) {
      try {
        await navigator.clipboard.writeText(text);
        btn.textContent = 'Copied!';
        setTimeout(() => { btn.textContent = label; }, 1500);
      } catch {
        window.prompt('Copy:', text);
      }
    }
    document.getElementById('copyIina').onclick = (e) => copy(iinaUrl, e.target, 'Copy IINA URL');
    document.getElementById('copyStream').onclick = (e) => copy(streamUrl, e.target, 'Copy stream URL');
    document.getElementById('copyPage').onclick = (e) => copy(pageUrl, e.target, 'Copy this page URL');
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
  registerStream,
  getStream,
  buildOpenPageHtml,
}
