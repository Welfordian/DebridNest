const VIDEO_EXT = /\.(mp4|mkv|avi|webm|mov|m4v|wmv|flv|ts|m2ts)$/i

const FETCH_TIMEOUT_MS = Number(process.env.DEBRIDNEST_FETCH_TIMEOUT_MS || 30000)
const DEFAULT_TRACKERS = [
  'udp://tracker.opentrackr.org:1337/announce',
  'udp://open.stealth.si:80/announce',
  'udp://tracker.torrent.eu.org:451/announce',
  'udp://tracker.bittor.pw:1337/announce',
  'udp://tracker.dler.org:6969/announce',
  'udp://open.demonii.com:1337/announce',
]

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
    signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
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
  return apiRequest(baseUrl, token, 'POST', '/torrents/addMagnet', {
    magnet: appendDefaultTrackers(magnet),
  })
}

async function addTorrentFile(baseUrl, token, data, filename = 'file.torrent') {
  const url = `${normalizeBaseUrl(baseUrl)}/torrents/addTorrent`
  const form = new FormData()
  form.append('torrent', new Blob([data]), filename)
  const res = await fetch(url, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
    },
    body: form,
    signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
  })
  const text = await res.text()
  let parsed = null
  if (text) {
    try {
      parsed = JSON.parse(text)
    } catch {
      parsed = text
    }
  }
  if (!res.ok) {
    const message = parsed && parsed.error ? parsed.error : `DebridNest API ${res.status}`
    const err = new Error(message)
    err.status = res.status
    err.data = parsed
    throw err
  }
  return parsed
}

async function addTorrentCandidate(baseUrl, token, { magnet, torrentLink, torrentData }) {
  if (torrentData?.length) {
    return addTorrentFile(baseUrl, token, torrentData)
  }
  if (torrentLink) {
    const jackett = require('./jackett')
    const data = await jackett.downloadTorrentFile(torrentLink)
    if (data?.length) {
      return addTorrentFile(baseUrl, token, data)
    }
  }
  if (!magnet) {
    throw new Error('Torrent has no magnet link or downloadable .torrent file')
  }
  return addMagnet(baseUrl, token, magnet)
}

async function getTorrentInfo(baseUrl, token, id, options = {}) {
  const params = new URLSearchParams()
  if (options.wait) {
    params.set('wait', options.wait)
  }
  const query = params.toString()
  return apiRequest(
    baseUrl,
    token,
    'GET',
    `/torrents/info/${encodeURIComponent(id)}${query ? `?${query}` : ''}`,
  )
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

function pad2(n) {
  return String(n).padStart(2, '0')
}

function episodePatterns(target = {}) {
  const season = Number(target.season)
  const episode = Number(target.episode)
  if (!Number.isFinite(season) || !Number.isFinite(episode)) {
    return []
  }
  const s = pad2(season)
  const e = pad2(episode)
  return [
    new RegExp(`(?:^|[^a-z0-9])s0*${season}e0*${episode}(?:[^a-z0-9]|$)`, 'i'),
    new RegExp(`(?:^|[^a-z0-9])s${s}e${e}(?:[^a-z0-9]|$)`, 'i'),
    new RegExp(`(?:^|[^a-z0-9])0*${season}x0*${episode}(?:[^a-z0-9]|$)`, 'i'),
    new RegExp(`(?:^|[^a-z0-9])${s}x${e}(?:[^a-z0-9]|$)`, 'i'),
    new RegExp(`season[^a-z0-9]*0*${season}[^a-z0-9]+(?:episode[^a-z0-9]*)?0*${episode}(?:[^a-z0-9]|$)`, 'i'),
    new RegExp(`(?:^|[^a-z0-9])s${s}[^a-z0-9]+(?:e|ep|episode)?0*${episode}(?:[^a-z0-9]|$)`, 'i'),
  ]
}

function hasEpisodeTarget(target = {}) {
  const season = Number(target.season)
  const episode = Number(target.episode)
  return Number.isFinite(season) && Number.isFinite(episode)
}

function matchesEpisodePath(file, target = {}) {
  const path = String(file?.path || '')
  const patterns = episodePatterns(target)
  return patterns.length > 0 && patterns.some((pattern) => pattern.test(path))
}

function pickVideoFile(files, target = {}) {
  const videos = files.filter((file) => VIDEO_EXT.test(file.path))
  const targetVideos = videos.filter((file) => matchesEpisodePath(file, target))
  if (targetVideos.length) {
    return targetVideos.reduce((best, file) => (file.bytes > best.bytes ? file : best))
  }
  if (hasEpisodeTarget(target) && videos.length > 1) {
    return null
  }
  const candidates = videos.length ? videos : files
  if (!candidates.length) {
    return null
  }
  return candidates.reduce((best, file) => (file.bytes > best.bytes ? file : best))
}

function pickVideoFileIds(files, target = {}) {
  const file = pickVideoFile(files, target)
  return file ? String(file.id) : ''
}

function selectedVideoFile(info, target = {}) {
  const selected = (info.files || [])
    .filter((file) => file.selected === 1)
    .sort((a, b) => a.id - b.id)
  if (!selected.length) {
    return null
  }
  const picked = pickVideoFile(selected, target)
  if (picked || hasEpisodeTarget(target)) {
    return picked
  }
  return selected[0]
}

function pickHostLink(info, target = {}) {
  if (!info.links || !info.links.length) {
    return null
  }

  if (info.files && info.files.length) {
    const selected = info.files
      .filter((file) => file.selected === 1)
      .sort((a, b) => a.id - b.id)
    const video = selectedVideoFile(info, target)
    if (video) {
      const linkIndex = selected.findIndex((file) => file.id === video.id)
      if (linkIndex >= 0 && info.links[linkIndex]) {
        return info.links[linkIndex]
      }
    }
    if (hasEpisodeTarget(target)) {
      return null
    }
  }

  return info.links[0]
}

async function prepareTorrent(baseUrl, token, torrentId, options = {}) {
  const info = await getTorrentInfo(baseUrl, token, torrentId, { wait: options.infoWait })
  if (isFailedStatus(info.status)) {
    throw new Error(`Torrent failed: ${info.status}`)
  }
  if (info.status === 'waiting_files_selection' && info.files && info.files.length) {
    const fileIds = pickVideoFileIds(info.files, options)
    if (!fileIds) {
      throw new Error('No matching episode file found in torrent')
    }
    await selectFiles(baseUrl, token, torrentId, fileIds)
    return prepareTorrent(baseUrl, token, torrentId, options)
  }
  if (hasEpisodeTarget(options) && info.files && info.files.length) {
    const targetFile = pickVideoFile(info.files, options)
    if (!targetFile) {
      throw new Error('No matching episode file found in torrent')
    }
    if (targetFile.selected !== 1) {
      await selectFiles(baseUrl, token, torrentId, String(targetFile.id))
      return prepareTorrent(baseUrl, token, torrentId, options)
    }
  }
  return info
}

function isFailedStatus(status) {
  return ['error', 'magnet_error', 'dead', 'virus'].includes(status)
}

async function resolveStreamUrl(baseUrl, token, torrentId, options = {}) {
  const info = await prepareTorrent(baseUrl, token, torrentId, options)
  if (!info.links || !info.links.length) {
    return null
  }
  const hostLink = pickHostLink(info, options)
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
  const pollIntervalMs = options.pollIntervalMs || 500
  const maxWaitMs = options.maxWaitMs || 300000
  const startedAt = Date.now()

  const added = await addTorrentCandidate(baseUrl, token, { magnet, torrentLink: options.torrentLink, torrentData: options.torrentData })
  const torrentId = added.id

  while (Date.now() - startedAt < maxWaitMs) {
    const info = await getTorrentInfo(baseUrl, token, torrentId)

    if (['error', 'magnet_error', 'dead', 'virus'].includes(info.status)) {
      throw new Error(`Torrent failed with status: ${info.status}`)
    }

    if (info.status === 'waiting_files_selection' && info.files && info.files.length) {
      const fileIds = pickVideoFileIds(info.files, options)
      if (!fileIds) {
        throw new Error('No matching episode file found in torrent')
      }
      await selectFiles(baseUrl, token, torrentId, fileIds)
    }

    if (info.links && info.links.length) {
      const hostLink = pickHostLink(info, options)
      if (!hostLink) {
        throw new Error('Torrent is streamable but no host link was returned')
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
  const pollIntervalMs = options.pollIntervalMs || 500
  const maxWaitMs = options.maxWaitMs || 15000
  const startedAt = Date.now()

  const added = await addTorrentCandidate(baseUrl, token, { magnet, torrentLink: options.torrentLink, torrentData: options.torrentData })
  const torrentId = added.id

  while (Date.now() - startedAt < maxWaitMs) {
    try {
      const resolved = await resolveStreamUrl(baseUrl, token, torrentId, options)
      if (resolved) {
        return resolved
      }
    } catch {
      // metadata or file selection still in progress
    }
    const info = await getTorrentInfo(baseUrl, token, torrentId)
    if (isFailedStatus(info.status)) {
      return null
    }
    await sleep(pollIntervalMs)
  }
  return null
}

async function resolveStreamableQuick(baseUrl, token, magnet, options = {}) {
  const pollIntervalMs = options.pollIntervalMs || 500
  const maxWaitMs = options.maxWaitMs || 20000
  const startedAt = Date.now()

  const added = await addTorrentCandidate(baseUrl, token, { magnet, torrentLink: options.torrentLink, torrentData: options.torrentData })
  const torrentId = added.id

  while (Date.now() - startedAt < maxWaitMs) {
    try {
      const resolved = await resolveStreamUrl(baseUrl, token, torrentId, options)
      if (resolved) {
        return resolved
      }
    } catch {
      // metadata or file selection still in progress
    }
    const info = await getTorrentInfo(baseUrl, token, torrentId)
    if (isFailedStatus(info.status)) {
      return null
    }
    await sleep(pollIntervalMs)
  }
  return null
}

async function startDownload(baseUrl, token, magnet, options = {}) {
  const added = await addTorrentCandidate(baseUrl, token, {
    magnet,
    torrentLink: options.torrentLink,
    torrentData: options.torrentData,
  })
  return added.id
}

async function checkDownloadReady(baseUrl, token, torrentId, options = {}) {
  const resolved = await resolveStreamUrl(baseUrl, token, torrentId, options)
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
  const pollIntervalMs = options.pollIntervalMs || 500
  const maxWaitMs = options.maxWaitMs || 600000
  const startedAt = Date.now()

  while (Date.now() - startedAt < maxWaitMs) {
    const info = await getTorrentInfo(baseUrl, token, torrentId)
    if (['error', 'magnet_error', 'dead', 'virus'].includes(info.status)) {
      throw new Error(`Torrent failed: ${info.status}`)
    }
    if (info.status === 'waiting_files_selection' && info.files && info.files.length) {
      const fileIds = pickVideoFileIds(info.files, options)
      if (!fileIds) {
        throw new Error('No matching episode file found in torrent')
      }
      await selectFiles(baseUrl, token, torrentId, fileIds)
    }
    if (info.links && info.links.length) {
      const hostLink = pickHostLink(info, options)
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
  const candidateOpts = {
    ...options,
    torrentLink: options.torrentLink || torrent.link,
  }
  if (!torrent.magnet && !candidateOpts.torrentLink) {
    throw new Error('Torrent has no magnet link or Jackett download link')
  }

  if (torrent.infoHash) {
    const availability = await checkInstantAvailability(baseUrl, token, [torrent.infoHash])
    if (isCached(availability, torrent.infoHash)) {
      const cached = await resolveCachedOnly(baseUrl, token, torrent.magnet, candidateOpts)
      if (cached) {
        return cached
      }
    }
  }

  return resolveMagnet(baseUrl, token, torrent.magnet, candidateOpts)
}

function appendDefaultTrackers(magnet) {
  const value = String(magnet || '')
  if (!value.toLowerCase().startsWith('magnet:?')) {
    return magnet
  }
  if (/[?&]tr=/i.test(value)) {
    return value
  }
  return `${value}${DEFAULT_TRACKERS.map((tracker) => `&tr=${encodeURIComponent(tracker)}`).join('')}`
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
  resolveStreamableQuick,
  startDownload,
  resolveStreamUrl,
  prepareTorrent,
  checkDownloadReady,
  waitForDownload,
  resolveTorrentCandidate,
  appendDefaultTrackers,
  pickVideoFileIds,
  pickHostLink,
}
