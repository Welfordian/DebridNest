const { Readable } = require('stream')

function debridnestApiBase(apiUrl) {
  return String(apiUrl || '').replace(/\/rest\/1\.0\/?$/, '')
}

function internalDownloadUrl(publicUrl, apiUrl) {
  try {
    const target = new URL(publicUrl)
    const base = new URL(debridnestApiBase(apiUrl))
    target.protocol = base.protocol
    target.host = base.host
    return target.toString()
  } catch {
    return publicUrl
  }
}

const HOP_BY_HOP = new Set([
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailers',
  'transfer-encoding',
  'upgrade',
])

const PASS_HEADERS = [
  'content-type',
  'content-length',
  'content-range',
  'accept-ranges',
  'content-disposition',
  'last-modified',
  'etag',
]

async function fetchUpstream(req, downloadUrl, apiUrl) {
  const url = internalDownloadUrl(downloadUrl, apiUrl)
  const headers = {}
  if (req.headers.range) {
    headers.Range = req.headers.range
  }
  if (req.headers['if-range']) {
    headers['If-Range'] = req.headers['if-range']
  }
  if (req.headers['if-modified-since']) {
    headers['If-Modified-Since'] = req.headers['if-modified-since']
  }

  const controller = new AbortController()
  const onClose = () => controller.abort()
  req.on('close', onClose)

  try {
    const upstream = await fetch(url, {
      method: req.method === 'HEAD' ? 'HEAD' : 'GET',
      headers,
      redirect: 'follow',
      signal: controller.signal,
    })
    return { upstream, controller, onClose }
  } catch (err) {
    req.off('close', onClose)
    throw err
  }
}

function copyHeaders(res, upstream) {
  for (const name of PASS_HEADERS) {
    const value = upstream.headers.get(name)
    if (value) {
      res.setHeader(name, value)
    }
  }
}

async function proxyStream(req, res, downloadUrl, apiUrl, options = {}) {
  const { refreshUrl } = options
  let url = downloadUrl

  for (let attempt = 0; attempt < 3; attempt++) {
    let upstream
    let controller
    let onClose
    try {
      ;({ upstream, controller, onClose } = await fetchUpstream(req, url, apiUrl))
    } catch (err) {
      if (err.name === 'AbortError') {
        return
      }
      if (attempt < 2 && refreshUrl) {
        try {
          url = await refreshUrl()
          continue
        } catch {
          // fall through
        }
      }
      res.status(502).send(`Upstream stream failed: ${err.message}`)
      return
    }

    if (upstream.status === 503) {
      req.off('close', onClose)
      res.set('Retry-After', upstream.headers.get('retry-after') || '2')
      res.status(503).send('Stream not ready at this position')
      return
    }

    if (!upstream.ok && upstream.status !== 206) {
      req.off('close', onClose)
      if ((upstream.status === 403 || upstream.status === 404) && refreshUrl && attempt < 2) {
        try {
          url = await refreshUrl()
          continue
        } catch {
          // fall through
        }
      }
      res.status(502).send(`Upstream stream failed (${upstream.status})`)
      return
    }

    res.status(upstream.status)
    copyHeaders(res, upstream)

    if (req.method === 'HEAD' || !upstream.body) {
      req.off('close', onClose)
      res.end()
      return
    }

    const body = Readable.fromWeb(upstream.body)
    body.on('error', () => {
      if (!res.headersSent) {
        res.status(502).end()
      } else {
        res.destroy()
      }
    })
    req.on('close', () => {
      body.destroy()
      controller.abort()
    })
    body.pipe(res)
    return
  }

  res.status(502).send('Upstream stream failed after retries')
}

module.exports = {
  internalDownloadUrl,
  proxyStream,
}
