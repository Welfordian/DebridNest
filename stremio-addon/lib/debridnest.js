const VIDEO_EXT = /\.(mp4|mkv|avi|webm|mov|m4v|wmv|flv|ts|m2ts)$/i

function normalizeBaseUrl(url) {
  return String(url || '').replace(/\/+$/, '')
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

async function apiRequest(baseUrl, token, method, path, body) {
  const url = `${normalizeBaseUrl(baseUrl)}${path.startsWith('/') ? path : `/${path}`}`
  const options = {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
    },
  }

  if (body) {
    options.headers['Content-Type'] = 'application/x-www-form-urlencoded'
    options.body = new URLSearchParams(body).toString()
  }

  const res = await fetch(url, options)
  const text = await res.text()
  let data = null

  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = text
    }
  }

  if (!res.ok) {
    const message = data && data.error ? data.error : `DebridNest API ${res.status}`
    const err = new Error(message)
    err.status = res.status
    err.data = data
    throw err
  }

  return data
}

async function getUser(baseUrl, token) {
  return apiRequest(baseUrl, token, 'GET', '/user')
}

async function addMagnet(baseUrl, token, magnet) {
  return apiRequest(baseUrl, token, 'POST', '/torrents/addMagnet', { magnet })
}

async function getTorrentInfo(baseUrl, token, id) {
  return apiRequest(baseUrl, token, 'GET', `/torrents/info/${encodeURIComponent(id)}`)
}

async function selectFiles(baseUrl, token, id, files) {
  const filesParam = Array.isArray(files) ? files.join(',') : String(files)
  await apiRequest(baseUrl, token, 'POST', `/torrents/selectFiles/${encodeURIComponent(id)}`, {
    files: filesParam,
  })
}

async function unrestrictLink(baseUrl, token, link) {
  return apiRequest(baseUrl, token, 'POST', '/unrestrict/link', { link })
}

async function checkInstantAvailability(baseUrl, token, infoHashes) {
  const hashes = infoHashes.filter(Boolean)
  if (!hashes.length) {
    return {}
  }
  const path = `/torrents/instantAvailability/${hashes.join('/')}`
  return apiRequest(baseUrl, token, 'GET', path)
}

function isCached(availability, infoHash) {
  if (!infoHash || !availability) {
    return false
  }
  const entry = availability[infoHash.toLowerCase()]
  if (!entry) {
    return false
  }
  return Object.values(entry).some((variants) => Array.isArray(variants) && variants.length > 0)
}

function pickVideoFileIds(files) {
  const videos = files.filter((file) => VIDEO_EXT.test(file.path))
  const candidates = videos.length ? videos : files
  const largest = candidates.reduce((best, file) => (file.bytes > best.bytes ? file : best))
  return String(largest.id)
}

function pickHostLink(info) {
  if (!info.links || !info.links.length) {
    return null
  }

  if (info.files && info.files.length) {
    const selected = info.files
      .filter((file) => file.selected === 1)
      .sort((a, b) => a.id - b.id)
    const video = selected.find((file) => VIDEO_EXT.test(file.path)) || selected[0]
    if (video) {
      const linkIndex = selected.findIndex((file) => file.id === video.id)
      if (linkIndex >= 0 && info.links[linkIndex]) {
        return info.links[linkIndex]
      }
    }
  }

  return info.links[0]
}

async function prepareTorrent(baseUrl, token, torrentId) {
  const info = await getTorrentInfo(baseUrl, token, torrentId)
  if (isFailedStatus(info.status)) {
    throw new Error(`Torrent failed: ${info.status}`)
  }
  if (info.status === 'waiting_files_selection' && info.files && info.files.length) {
    await selectFiles(baseUrl, token, torrentId, pickVideoFileIds(info.files))
    return prepareTorrent(baseUrl, token, torrentId)
  }
  return info
}

function isFailedStatus(status) {
  return ['error', 'magnet_error', 'dead', 'virus'].includes(status)
}

async function resolveStreamUrl(baseUrl, token, torrentId) {
  const info = await prepareTorrent(baseUrl, token, torrentId)
  if (!info.links || !info.links.length) {
    return null
  }
  const hostLink = pickHostLink(info)
  if (!hostLink) {
    return null
  }
  const unrestricted = await unrestrictLink(baseUrl, token, hostLink)
  return {
    hostLink,
    download: unrestricted.download,
    filename: unrestricted.filename,
    filesize: unrestricted.filesize,
    mimeType: unrestricted.mimeType,
    info,
  }
}

async function resolveMagnet(baseUrl, token, magnet, options = {}) {
  const pollIntervalMs = options.pollIntervalMs || 2000
  const maxWaitMs = options.maxWaitMs || 300000
  const startedAt = Date.now()

  await getUser(baseUrl, token)

  const added = await addMagnet(baseUrl, token, magnet)
  const torrentId = added.id

  while (Date.now() - startedAt < maxWaitMs) {
    const info = await getTorrentInfo(baseUrl, token, torrentId)

    if (['error', 'magnet_error', 'dead', 'virus'].includes(info.status)) {
      throw new Error(`Torrent failed with status: ${info.status}`)
    }

    if (info.status === 'waiting_files_selection' && info.files && info.files.length) {
      await selectFiles(baseUrl, token, torrentId, pickVideoFileIds(info.files))
    }

    if ((info.status === 'downloaded' || info.status === 'dead') && info.links && info.links.length) {
      const hostLink = pickHostLink(info)
      if (!hostLink) {
        throw new Error('Torrent downloaded but no host link was returned')
      }

      const unrestricted = await unrestrictLink(baseUrl, token, hostLink)
      return {
        torrentId,
        download: unrestricted.download,
        filename: unrestricted.filename,
        filesize: unrestricted.filesize,
        mimeType: unrestricted.mimeType,
        streamable: unrestricted.streamable,
      }
    }

    await sleep(pollIntervalMs)
  }

  throw new Error('Timed out waiting for torrent to finish downloading')
}

async function resolveCachedOnly(baseUrl, token, magnet, options = {}) {
  const pollIntervalMs = options.pollIntervalMs || 1000
  const maxWaitMs = options.maxWaitMs || 15000
  const startedAt = Date.now()

  const added = await addMagnet(baseUrl, token, magnet)
  const torrentId = added.id

  while (Date.now() - startedAt < maxWaitMs) {
    const info = await getTorrentInfo(baseUrl, token, torrentId)
    if (info.status === 'downloaded' && info.links && info.links.length) {
      const hostLink = pickHostLink(info)
      if (!hostLink) {
        return null
      }
      const unrestricted = await unrestrictLink(baseUrl, token, hostLink)
      return {
        torrentId,
        download: unrestricted.download,
        filename: unrestricted.filename,
      }
    }
    if (['error', 'magnet_error', 'dead', 'virus'].includes(info.status)) {
      return null
    }
    await sleep(pollIntervalMs)
  }
  return null
}

async function startDownload(baseUrl, token, magnet, options = {}) {
  const pollIntervalMs = options.pollIntervalMs || 1000
  const maxWaitMs = options.maxWaitMs || 120000
  const startedAt = Date.now()

  await getUser(baseUrl, token)
  const added = await addMagnet(baseUrl, token, magnet)
  const torrentId = added.id

  while (Date.now() - startedAt < maxWaitMs) {
    const info = await getTorrentInfo(baseUrl, token, torrentId)
    if (isFailedStatus(info.status)) {
      throw new Error(`Torrent failed: ${info.status}`)
    }
    if (info.status === 'waiting_files_selection' && info.files && info.files.length) {
      await selectFiles(baseUrl, token, torrentId, pickVideoFileIds(info.files))
    }
    if (info.status !== 'magnet_conversion' && info.status !== 'waiting_files_selection') {
      return torrentId
    }
    await sleep(pollIntervalMs)
  }

  return torrentId
}

async function checkDownloadReady(baseUrl, token, torrentId) {
  const resolved = await resolveStreamUrl(baseUrl, token, torrentId)
  if (resolved) {
    return {
      ready: true,
      unrestricted: resolved,
      info: resolved.info,
      streaming: resolved.info.status === 'downloading',
    }
  }

  const info = await getTorrentInfo(baseUrl, token, torrentId)
  if (isFailedStatus(info.status)) {
    throw new Error(`Torrent failed: ${info.status}`)
  }

  return { ready: false, info }
}

async function waitForDownload(baseUrl, token, torrentId, options = {}) {
  const pollIntervalMs = options.pollIntervalMs || 2000
  const maxWaitMs = options.maxWaitMs || 600000
  const startedAt = Date.now()

  while (Date.now() - startedAt < maxWaitMs) {
    const info = await getTorrentInfo(baseUrl, token, torrentId)
    if (['error', 'magnet_error', 'dead', 'virus'].includes(info.status)) {
      throw new Error(`Torrent failed: ${info.status}`)
    }
    if (info.status === 'waiting_files_selection' && info.files && info.files.length) {
      await selectFiles(baseUrl, token, torrentId, pickVideoFileIds(info.files))
    }
    if (info.links && info.links.length) {
      const hostLink = pickHostLink(info)
      if (!hostLink) {
        throw new Error('No host link')
      }
      return unrestrictLink(baseUrl, token, hostLink)
    }
    await sleep(pollIntervalMs)
  }
  throw new Error('Timed out waiting for download')
}

async function resolveTorrentCandidate(baseUrl, token, torrent, options = {}) {
  if (!torrent.magnet) {
    throw new Error('Torrent has no magnet link')
  }

  if (torrent.infoHash) {
    const availability = await checkInstantAvailability(baseUrl, token, [torrent.infoHash])
    if (isCached(availability, torrent.infoHash)) {
      const cached = await resolveCachedOnly(baseUrl, token, torrent.magnet, options)
      if (cached) {
        return cached
      }
    }
  }

  return resolveMagnet(baseUrl, token, torrent.magnet, options)
}

module.exports = {
  getUser,
  addMagnet,
  getTorrentInfo,
  selectFiles,
  unrestrictLink,
  checkInstantAvailability,
  isCached,
  resolveMagnet,
  resolveCachedOnly,
  startDownload,
  resolveStreamUrl,
  prepareTorrent,
  checkDownloadReady,
  waitForDownload,
  resolveTorrentCandidate,
}
