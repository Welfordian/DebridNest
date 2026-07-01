#!/usr/bin/env node

const express = require('express')
const { addonBuilder, getRouter } = require('stremio-addon-sdk')
const landingTemplate = require('./lib/landing')
const cinemeta = require('./lib/cinemeta')
const scrapers = require('./lib/scrapers')
const rank = require('./lib/rank')
const debridnest = require('./lib/debridnest')
const progress = require('./lib/progress')
const progressHandler = require('./lib/progressHandler')
const jackettConfig = require('./lib/jackettConfig')
const jackett = require('./lib/jackett')
const quality = require('./lib/quality')
const externalPlayer = require('./lib/externalPlayer')
const playHandler = require('./lib/playHandler')

const PORT = Number(process.env.PORT || 7000)
const ADDON_BASE_URL = process.env.ADDON_BASE_URL || `http://127.0.0.1:${PORT}`

const DEFAULT_API_URL = process.env.DEBRIDNEST_API_URL || 'http://localhost:8080/rest/1.0'
const DEFAULT_API_TOKEN = process.env.DEBRIDNEST_API_TOKEN || ''
const DEFAULT_JACKETT_URL = process.env.JACKETT_URL || ''
const DEFAULT_JACKETT_API_KEY = jackettConfig.resolveDefaultApiKey()
const DEFAULT_MAX_RESULTS = Number(process.env.MAX_RESULTS || 5)
const DEFAULT_PREFER_SDR = process.env.PREFER_SDR === '1'
const DEFAULT_MAX_RESOLUTION = process.env.MAX_RESOLUTION || '0'
const DEFAULT_MAX_FILE_SIZE_GB = process.env.MAX_FILE_SIZE_GB || '0'
const DEFAULT_DEDUPE_STREAMS = process.env.DEDUPE_STREAMS !== '0'
const DEFAULT_PREFER_SEASON_PACKS = process.env.PREFER_SEASON_PACKS === '1'
const PLACEHOLDER_COUNT = Number(process.env.PLACEHOLDER_COUNT || 10)
const LIST_RESOLVE_COUNT = Number(process.env.LIST_RESOLVE_COUNT || 0)
const CACHED_RESOLVE_WAIT_MS = Number(process.env.CACHED_RESOLVE_WAIT_MS || 8000)
const DEFAULT_PREWARM_COUNT = process.env.PRELOAD_TOP_RESULT === '1' ? 1 : 0
const PREWARM_COUNT = Math.max(
  0,
  Number(process.env.PREWARM_COUNT ?? DEFAULT_PREWARM_COUNT) || 0,
)
const PROGRESS_POLL_MS = Number(process.env.PROGRESS_POLL_MS || 500)
const JACKETT_TORRENT_PREFETCH_COUNT = Math.max(
  0,
  Number(process.env.JACKETT_TORRENT_PREFETCH_COUNT || 5) || 0,
)
const ENABLE_MAGNET_TEST = process.env.ENABLE_MAGNET_TEST === '1'
const ADMIN_TOKEN = process.env.ADMIN_TOKEN || ''

const LOCAL_IPS = new Set(['127.0.0.1', '::1', '::ffff:127.0.0.1'])

function normalizeRemoteIp(value) {
  const ip = String(value || '')
  return ip.startsWith('::ffff:') ? ip.slice('::ffff:'.length) : ip
}

function isLocalRequest(req) {
  const ip = normalizeRemoteIp(req.ip || req.socket?.remoteAddress || '')
  return LOCAL_IPS.has(ip)
}

function requireLocalOrAdmin(req, res, next) {
  if (isLocalRequest(req)) {
    next()
    return
  }
  const adminHeader = req.get('X-Admin-Token') || ''
  if (ADMIN_TOKEN && adminHeader === ADMIN_TOKEN) {
    next()
    return
  }
  res.status(403).json({ error: 'Forbidden' })
}

function isDockerBridgeIp(ip) {
  const parts = String(ip || '').split('.').map((part) => Number(part))
  if (parts.length !== 4 || parts.some((part) => Number.isNaN(part))) {
    return false
  }
  if (parts[0] === 172 && parts[1] >= 16 && parts[1] <= 31) {
    return true
  }
  return parts[0] === 192 && parts[1] === 168 && (parts[2] === 64 || parts[2] === 65)
}

function isLocalhostHostHeader(req) {
  const rawHost = String(req.get('host') || '').toLowerCase()
  if (rawHost.startsWith('[::1]')) {
    return true
  }
  const host = rawHost.split(':')[0]
  return host === 'localhost' || host === '127.0.0.1' || host === '[::1]' || host === '::1'
}

function requireDiagnosticsAccess(req, res, next) {
  if (isLocalRequest(req)) {
    next()
    return
  }
  const adminHeader = req.get('X-Admin-Token') || ''
  if (ADMIN_TOKEN && adminHeader === ADMIN_TOKEN) {
    next()
    return
  }
  const ip = normalizeRemoteIp(req.ip || req.socket?.remoteAddress || '')
  if (isLocalhostHostHeader(req) && isDockerBridgeIp(ip)) {
    next()
    return
  }
  res.status(403).json({ error: 'Forbidden' })
}

function extractBearerToken(req) {
  const auth = req.get('Authorization') || ''
  const match = auth.match(/^Bearer\s+(.+)$/i)
  return match ? match[1].trim() : ''
}

function getServerSideConfig(req) {
  const bearerToken = extractBearerToken(req)
  return {
    apiUrl: DEFAULT_API_URL,
    apiToken: resolveApiToken(bearerToken || DEFAULT_API_TOKEN),
    jackettUrl: DEFAULT_JACKETT_URL,
    jackettApiKey: DEFAULT_JACKETT_API_KEY,
  }
}

function configValue(userValue, defaultValue) {
  if (userValue === undefined || userValue === null) {
    return defaultValue
  }
  if (typeof userValue === 'string' && userValue.trim() === '') {
    return defaultValue
  }
  return userValue
}

function collapseDuplicatedToken(value) {
  const token = String(value || '').trim()
  if (token.length < 64 || token.length % 2 !== 0 || !/^[a-f0-9]+$/i.test(token)) {
    return token
  }
  const half = token.length / 2
  const left = token.slice(0, half)
  const right = token.slice(half)
  return left === right ? left : token
}

function resolveApiToken(userValue) {
  return collapseDuplicatedToken(configValue(userValue, DEFAULT_API_TOKEN))
}

function resolveJackettApiKey(userValue) {
  if (jackettConfig.isPlaceholderKey(userValue)) {
    return DEFAULT_JACKETT_API_KEY
  }
  return configValue(userValue, DEFAULT_JACKETT_API_KEY)
}

function normalizeUserConfig(userConfig) {
  if (!userConfig || typeof userConfig !== 'object') {
    return {}
  }
  return userConfig
}

function resolveApiUrl(userValue) {
  const configured = configValue(userValue, DEFAULT_API_URL)
  const normalized = String(configured || '').trim()
  if (!normalized) {
    return DEFAULT_API_URL
  }
  // Public IP / localhost URLs fail from inside the addon container (no hairpin NAT).
  if (/^https?:\/\/(localhost|127\.0\.0\.1|\d{1,3}(?:\.\d{1,3}){3})(?::\d+)?\//i.test(normalized)) {
    if (normalized !== DEFAULT_API_URL) {
      console.warn(`[config] apiUrl ${normalized} is not reachable from the addon container; using ${DEFAULT_API_URL}`)
    }
    return DEFAULT_API_URL
  }
  return normalized
}

function getConfig(userConfig = {}) {
  userConfig = normalizeUserConfig(userConfig)
  const qualityConfig = quality.resolveQualityConfig(userConfig, {
    preferSdr: DEFAULT_PREFER_SDR,
    maxResolution: DEFAULT_MAX_RESOLUTION,
    maxFileSizeGb: DEFAULT_MAX_FILE_SIZE_GB,
  })
  return {
    apiUrl: resolveApiUrl(userConfig.apiUrl),
    apiToken: resolveApiToken(userConfig.apiToken),
    jackettUrl: configValue(userConfig.jackettUrl, DEFAULT_JACKETT_URL),
    jackettApiKey: resolveJackettApiKey(userConfig.jackettApiKey),
    maxResults: Number(configValue(userConfig.maxResults, DEFAULT_MAX_RESULTS)) || 5,
    preferSdr: qualityConfig.preferSdr,
    maxResolution: String(qualityConfig.maxResolution || '0'),
    maxFileSizeGb: String(qualityConfig.maxFileSizeGb || '0'),
    dedupeStreams: DEFAULT_DEDUPE_STREAMS,
    preferSeasonPacks: DEFAULT_PREFER_SEASON_PACKS,
  }
}

function getQualityConfig(config) {
  return quality.resolveQualityConfig(config)
}

function maxResolutionDefaultOption() {
  const parsed = quality.parseMaxResolution(DEFAULT_MAX_RESOLUTION)
  if (!parsed) {
    return 'Any'
  }
  return String(parsed)
}

function needsNotWebReady(url) {
  if (!url || typeof url !== 'string') {
    return true
  }
  try {
    const parsed = new URL(url)
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
      return false
    }
    if (parsed.protocol === 'http:') {
      return true
    }
    return !/\.mp4(\?|$)/i.test(parsed.pathname)
  } catch {
    return true
  }
}

function buildStreamObject(entry, options = {}) {
  const { directUrl = null, progressToken = null, cached = false } = options
  const label = entry?.torrent?.title || 'DebridNest stream'
  let streamUrl = directUrl

  if (!streamUrl) {
    const streamId = externalPlayer.registerStream({
      directUrl,
      progressToken,
      label,
    })
    streamUrl = externalPlayer.buildPlayUrl(streamId, ADDON_BASE_URL)
  }

  const description = rank.formatStremioStreamDescription(entry, cached)
  const stream = {
    name: rank.formatStremioStreamName(entry, cached),
    description,
    url: streamUrl,
    behaviorHints: {
      bingeGroup: rank.formatBingeGroup(entry),
      filename: rank.formatStreamFilename(entry.torrent.title),
    },
  }

  if (needsNotWebReady(streamUrl)) {
    stream.behaviorHints.notWebReady = true
  }

  return stream
}

function encodeMagnetId(magnet) {
  return `magnet:${Buffer.from(magnet, 'utf8').toString('base64url')}`
}

function decodeMagnetId(id) {
  if (!id || !id.startsWith('magnet:')) {
    return null
  }
  try {
    return Buffer.from(id.slice('magnet:'.length), 'base64url').toString('utf8')
  } catch {
    return null
  }
}

function isMagnetUri(value) {
  return typeof value === 'string' && value.trim().toLowerCase().startsWith('magnet:?')
}

function requireDebridNestConfig(config) {
  if (!config.apiToken) {
    throw new Error('DebridNest API token is required.')
  }
}

function requireConfig(config) {
  requireDebridNestConfig(config)
  const hasTorrentSearch = config.jackettUrl && config.jackettApiKey
  if (!hasTorrentSearch) {
    throw new Error('Configure Jackett/Prowlarr.')
  }
}

function isAuthError(err) {
  return err?.status === 401 || err?.message === 'bad_token'
}

function sendAuthError(res) {
  res.status(401).send('DebridNest API token rejected. Reinstall this addon with the current DebridNest API token.')
}

function sendPlayError(res, entry, err) {
  if (isAuthError(err)) {
    sendAuthError(res)
    return
  }
  res.set('Retry-After', String(Math.max(1, Math.ceil(PROGRESS_POLL_MS / 1000))))
  res.status(503).send(`Still buffering: ${entry.label || 'stream'}. Try again shortly or choose another stream source.`)
}

function safeRequestPath(req) {
  const marker = req.path.indexOf('/stream/')
  if (marker < 0) {
    return req.path
  }
  return `{config}/stream/${req.path.slice(marker + '/stream/'.length)}`
}

function redactLogText(value) {
  return String(value || '')
    .replace(/(Bearer\s+)[^\s]+/gi, '$1[redacted]')
    .replace(/([?&](?:api_?key|token|auth|password|pass)=)[^&\s]+/gi, '$1[redacted]')
    .replace(/(magnet:\?)[^\s]+/gi, '$1[redacted]')
    .replace(/\b[a-f0-9]{64,}\b/gi, '[redacted]')
}

function safeLogText(value, fallback) {
  const cleaned = redactLogText(value || fallback || '')
    .replace(/[\r\n\t]+/g, ' ')
    .trim()
  return cleaned.length > 160 ? `${cleaned.slice(0, 157)}...` : cleaned
}

function logPrewarmError(entry, err) {
  const label = safeLogText(entry?.torrent?.title, 'stream')
  const message = safeLogText(err?.message || err, 'failed')
  console.warn(`[prewarm] ${label}: ${message}`)
}

function prewarmProgressJob(progressToken, entry) {
  const job = progress.getJob(progressToken)
  if (!job) {
    return false
  }
  progressHandler.prewarmJob(job).catch((err) => logPrewarmError(entry, err))
  return true
}

function hasDownloadSource(entry) {
  return !!(entry?.torrent?.magnet || entry?.torrent?.link)
}

function formatStreamRequestLabel(meta) {
  if (!meta) {
    return 'unknown'
  }
  const episode = meta.season != null && meta.episode != null
    ? ` S${meta.season}E${meta.episode}`
    : ''
  return `${meta.title || meta.imdbId || 'unknown'}${episode}`
}

const manifest = {
  id: 'com.debridnest.streams',
  version: '3.1.13',
  name: 'DebridNest Streams',
  description: 'Stream movies and series via Jackett/Prowlarr and your self-hosted DebridNest debrid server.',
  resources: [
    {
      name: 'stream',
      types: ['movie', 'series'],
      idPrefixes: ['tt'],
    },
  ],
  types: ['movie', 'series'],
  catalogs: [],
  behaviorHints: {
    configurable: true,
    configurationRequired: true,
    p2p: false,
  },
  config: [
    {
      key: 'apiUrl',
      type: 'text',
      title: 'DebridNest API URL (Docker VPN: http://gluetun:8080/rest/1.0)',
      default: DEFAULT_API_URL,
    },
    {
      key: 'apiToken',
      type: 'password',
      title: 'DebridNest API Token',
      required: true,
    },
    {
      key: 'jackettUrl',
      type: 'text',
      title: 'Jackett/Prowlarr URL',
      default: DEFAULT_JACKETT_URL,
    },
    {
      key: 'jackettApiKey',
      type: 'password',
      title: 'Jackett/Prowlarr API Key',
      required: true,
    },
    {
      key: 'maxResults',
      type: 'text',
      title: 'Max streams to resolve',
      default: String(DEFAULT_MAX_RESULTS),
    },
    {
      key: 'preferSdr',
      type: 'checkbox',
      title: 'Prefer SDR over HDR/Dolby Vision (Mac Stremio)',
      default: DEFAULT_PREFER_SDR ? 'true' : 'false',
    },
    {
      key: 'maxResolution',
      type: 'select',
      title: 'Max resolution',
      default: maxResolutionDefaultOption(),
      options: ['Any', '720', '1080', '2160'],
    },
    {
      key: 'maxFileSizeGb',
      type: 'text',
      title: 'Max file size (GB, 0 = no limit)',
      default: String(DEFAULT_MAX_FILE_SIZE_GB),
    },
  ],
}

if (ENABLE_MAGNET_TEST) {
  manifest.resources.unshift('catalog')
  manifest.catalogs.push({
    type: 'movie',
    id: 'magnet-test',
    name: 'Magnet Test',
    extra: [{ name: 'search', isRequired: true }],
  })
}

const builder = new addonBuilder(manifest)

if (ENABLE_MAGNET_TEST) {
  builder.defineCatalogHandler(async (args) => {
    if (args.type !== 'movie' || args.id !== 'magnet-test') {
      return { metas: [] }
    }
    const query = args.extra && args.extra.search
    if (!isMagnetUri(query)) {
      return { metas: [] }
    }
    const magnet = query.trim()
    const label = magnet.length > 72 ? `${magnet.slice(0, 72)}...` : magnet
    return {
      metas: [{
        id: encodeMagnetId(magnet),
        type: 'movie',
        name: `Magnet: ${label}`,
        description: 'Play this magnet through DebridNest.',
      }],
    }
  })
}

builder.defineStreamHandler(async (args) => {
  const config = getConfig(args.config)
  requireConfig(config)

  if (ENABLE_MAGNET_TEST && args.type === 'movie' && args.id.startsWith('magnet:')) {
    const magnet = decodeMagnetId(args.id)
    if (!magnet) {
      return { streams: [] }
    }
    const resolved = await debridnest.resolveMagnet(config.apiUrl, config.apiToken, magnet, {
      pollIntervalMs: PROGRESS_POLL_MS,
    })
    return {
      streams: [buildStreamObject(
        { torrent: { title: resolved.filename || 'DebridNest' } },
        { directUrl: resolved.download },
      )],
    }
  }

  if (!['movie', 'series'].includes(args.type)) {
    return { streams: [] }
  }

  const meta = await cinemeta.resolveMetadata(args.type, args.id)
  if (!meta) {
    console.warn(`[streams] Could not resolve metadata for ${args.type} id=${args.id}`)
    return { streams: [] }
  }

  if (args.type === 'series' && (meta.season == null || meta.episode == null)) {
    console.warn(`[streams] Series request missing season/episode in id=${args.id}`)
    return { streams: [] }
  }

  const qualityConfig = getQualityConfig(config)
  const searchResult = await scrapers.searchAllDetailed(config, meta)
  const torrents = searchResult.torrents
  if (!torrents.length) {
    console.warn(
      `[streams] No Jackett results for ${meta.title || meta.imdbId}`
      + (meta.season != null ? ` S${meta.season}E${meta.episode}` : ''),
    )
  }
  const ranking = rank.rankTorrentsDetailed(torrents, meta, config.maxResults * 2, qualityConfig)
  const ranked = ranking.entries
  if (!ranked.length) {
    if (torrents.length) {
      console.warn(
        `[streams] ${torrents.length} Jackett results filtered out for ${formatStreamRequestLabel(meta)}`
        + ` (${JSON.stringify(ranking.rejected)})`,
      )
    }
    return { streams: [] }
  }

  const hashes = ranked.map((e) => e.torrent.infoHash).filter(Boolean)
  let availability = {}
  try {
    availability = await debridnest.checkInstantAvailability(config.apiUrl, config.apiToken, hashes)
  } catch (err) {
    console.warn(`[streams] instantAvailability failed (${err.message}); listing without cache hints`)
  }
  const ordered = rank.applyStreamListingPriority(ranked, availability).slice(0, config.maxResults)
  jackett.warmTorrentFileCache(
    ordered.map((entry) => entry.torrent.link).filter(Boolean),
    JACKETT_TORRENT_PREFETCH_COUNT,
  )

  const streams = []
  const placeholderCandidates = []
  let resolvedCount = 0
  const seenHashes = new Set()

  for (const entry of ordered) {
    const hash = entry.torrent.infoHash?.toLowerCase()
    if (hash) {
      if (seenHashes.has(hash)) {
        continue
      }
      seenHashes.add(hash)
    }

    const cached = rank.isEntryCached(entry, availability)
    let resolved = null

    if (cached && resolvedCount < LIST_RESOLVE_COUNT && hasDownloadSource(entry)) {
      const candidateOpts = {
        maxWaitMs: CACHED_RESOLVE_WAIT_MS,
        torrentLink: entry.torrent.link,
        season: meta.season,
        episode: meta.episode,
      }
      try {
        resolved = await debridnest.resolveCachedOnly(
          config.apiUrl,
          config.apiToken,
          entry.torrent.magnet,
          candidateOpts,
        )
      } catch {
        resolved = null
      }
    }

    if (resolved?.download) {
      streams.push(buildStreamObject(entry, {
        directUrl: resolved.download,
        cached: cached || resolved.info?.status === 'downloaded',
      }))
      resolvedCount++
      continue
    }

    placeholderCandidates.push({ entry, cached })
  }

  const maxStreams = config.maxResults
  const maxPlaceholders = Math.min(maxStreams, PLACEHOLDER_COUNT)
  const maxPrewarm = Math.min(maxPlaceholders, PREWARM_COUNT)

  let placeholders = 0
  let prewarmed = 0
  for (const { entry, cached } of placeholderCandidates) {
    if (streams.length >= maxStreams || placeholders >= maxPlaceholders) {
      break
    }
    if (!hasDownloadSource(entry)) {
      continue
    }

    try {
      const progressToken = progress.createJob({
        magnet: entry.torrent.magnet,
        torrentLink: entry.torrent.link,
        apiUrl: config.apiUrl,
        apiToken: config.apiToken,
        label: entry.torrent.title,
        season: meta.season,
        episode: meta.episode,
      })
      streams.push(buildStreamObject(entry, {
        progressToken,
        cached,
      }))
      if (prewarmed < maxPrewarm && prewarmProgressJob(progressToken, entry)) {
        prewarmed++
      }
      placeholders++
    } catch {
      // skip failed starts
    }
  }

  console.log(
    `[streams] ${args.type}/${args.id} → ${streams.length} streams`
    + ` (${resolvedCount} direct, ${placeholders} placeholder, ${prewarmed} prewarm)`,
  )

  return { streams }
})

async function resolveFromQuery(req) {
  const magnet = req.query.magnet
  if (!isMagnetUri(magnet)) {
    const err = new Error('Query parameter "magnet" must be a magnet URI.')
    err.status = 400
    throw err
  }
  const config = getServerSideConfig(req)
  requireDebridNestConfig(config)
  return debridnest.resolveMagnet(config.apiUrl, config.apiToken, magnet.trim(), {
    pollIntervalMs: PROGRESS_POLL_MS,
  })
}

function getDiagnosticsConfig(req) {
  return {
    ...getConfig(),
    ...getServerSideConfig(req),
  }
}

function wantsDiagnosticsHtml(req) {
  if (req.query.format === 'json') {
    return false
  }
  if (req.query.format === 'html') {
    return true
  }
  return String(req.get('accept') || '').includes('text/html')
}

function escapeHtml(value) {
  return String(value ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

function formatDiagnosticSize(bytes) {
  const size = Number(bytes || 0)
  if (size <= 0) {
    return ''
  }
  const gb = size / (1024 ** 3)
  return `${gb >= 10 ? gb.toFixed(1) : gb.toFixed(2)} GB`
}

function compactStreamName(entry, cached) {
  return rank.formatStremioStreamName(entry, cached).replace(/\n/g, ' / ')
}

async function buildStreamDiagnostics(config, type, id) {
  if (!['movie', 'series'].includes(type)) {
    const err = new Error('type must be "movie" or "series"')
    err.status = 400
    throw err
  }
  if (!id) {
    const err = new Error('id is required')
    err.status = 400
    throw err
  }

  requireConfig(config)
  const meta = await cinemeta.resolveMetadata(type, id)
  if (!meta) {
    const err = new Error(`Could not resolve metadata for ${type}/${id}`)
    err.status = 404
    throw err
  }

  const qualityConfig = getQualityConfig(config)
  const search = await scrapers.searchAllDetailed(config, meta)
  const ranking = rank.rankTorrentsDetailed(
    search.torrents,
    meta,
    config.maxResults * 2,
    qualityConfig,
  )

  const hashes = ranking.entries.map((entry) => entry.torrent.infoHash).filter(Boolean)
  let availability = {}
  let availabilityError = null
  if (hashes.length) {
    try {
      availability = await debridnest.checkInstantAvailability(config.apiUrl, config.apiToken, hashes)
    } catch (err) {
      availabilityError = err.message
    }
  }

  const ordered = rank
    .applyStreamListingPriority(ranking.entries, availability)
    .slice(0, config.maxResults)
  const finalEntries = ordered.slice(0, Math.min(config.maxResults, PLACEHOLDER_COUNT))

  return {
    ok: true,
    meta: {
      type,
      id,
      title: meta.title || '',
      year: meta.year || null,
      season: meta.season ?? null,
      episode: meta.episode ?? null,
    },
    counts: {
      raw: search.counts.raw,
      search: search.counts.search,
      afterSeasonPackEnrich: search.counts.afterSeasonPackEnrich,
      afterDedupe: search.counts.afterDedupe,
      filtered: ranking.counts.scored,
      ranked: ranking.counts.ranked,
      rankReturned: ranking.counts.returned,
      ordered: ordered.length,
      final: finalEntries.length,
      deduped: search.counts.deduped,
    },
    rejected: ranking.rejected,
    queries: search.jackettQueries || search.queries || [],
    errors: search.errors || [],
    availability: {
      checked: hashes.length,
      error: availabilityError,
    },
    cacheHit: search.cacheHit,
    streams: finalEntries.map((entry) => {
      const cached = rank.isEntryCached(entry, availability)
      return {
        name: compactStreamName(entry, cached),
        title: entry.torrent.title,
        provider: entry.torrent.indexer || '',
        seeders: Number(entry.torrent.seeders || 0),
        size: formatDiagnosticSize(entry.torrent.size),
        cached,
        score: entry.score,
        quality: entry.quality?.label || '',
        source: entry.source || '',
      }
    }),
  }
}

function renderDiagnosticsHtml(result) {
  const countRows = Object.entries(result.counts)
    .map(([key, value]) => `<tr><th>${escapeHtml(key)}</th><td>${escapeHtml(value)}</td></tr>`)
    .join('')
  const rejectedRows = Object.entries(result.rejected)
    .map(([key, value]) => `<tr><th>${escapeHtml(key)}</th><td>${escapeHtml(value)}</td></tr>`)
    .join('')
  const queryRows = result.queries.length
    ? result.queries.map((query) => (
      `<tr><td>${escapeHtml(query.label)}</td><td>${escapeHtml(query.rawCount)}</td><td>${escapeHtml(query.usableCount)}</td></tr>`
    )).join('')
    : '<tr><td colspan="3">No Jackett queries recorded.</td></tr>'
  const streamRows = result.streams.length
    ? result.streams.map((stream) => (
      `<tr><td>${escapeHtml(stream.name)}</td><td>${escapeHtml(stream.title)}</td><td>${escapeHtml(stream.provider)}</td><td>${escapeHtml(stream.seeders)}</td><td>${escapeHtml(stream.size)}</td></tr>`
    )).join('')
    : '<tr><td colspan="5">No final streams.</td></tr>'

  return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>DebridNest Stream Diagnostics</title>
  <style>
    body { background: #0f1420; color: #eef2ff; font-family: -apple-system, BlinkMacSystemFont, sans-serif; margin: 0; padding: 24px; }
    main { max-width: 1120px; margin: 0 auto; }
    h1 { font-size: 24px; margin: 0 0 8px; }
    h2 { font-size: 18px; margin: 28px 0 10px; }
    .muted { color: #9aa4b8; }
    table { width: 100%; border-collapse: collapse; background: #151b28; border: 1px solid #2b3448; margin-bottom: 16px; }
    th, td { border-bottom: 1px solid #2b3448; padding: 9px 10px; text-align: left; vertical-align: top; }
    th { color: #c8d1e6; width: 220px; }
    code { color: #d6e4ff; }
  </style>
</head>
<body>
  <main>
    <h1>Stream Diagnostics</h1>
    <p class="muted">${escapeHtml(result.meta.type)}/${escapeHtml(result.meta.id)} - ${escapeHtml(result.meta.title || 'Unknown')}</p>
    <h2>Counts</h2>
    <table>${countRows}</table>
    <h2>Rejected</h2>
    <table>${rejectedRows}</table>
    <h2>Jackett Queries</h2>
    <table><tr><th>Query</th><th>Raw</th><th>Usable</th></tr>${queryRows}</table>
    <h2>Final Streams</h2>
    <table><tr><th>Name</th><th>Release</th><th>Provider</th><th>Seeders</th><th>Size</th></tr>${streamRows}</table>
    ${result.errors.length ? `<p class="muted">Errors: <code>${escapeHtml(result.errors.join('; '))}</code></p>` : ''}
    ${result.availability.error ? `<p class="muted">Availability check failed: <code>${escapeHtml(result.availability.error)}</code></p>` : ''}
  </main>
</body>
</html>`
}

async function handleStreamDiagnostics(req, res) {
  const type = req.params.type || req.query.type
  const id = req.params.id || req.query.id
  try {
    const result = await buildStreamDiagnostics(getDiagnosticsConfig(req), type, id)
    if (wantsDiagnosticsHtml(req)) {
      res.setHeader('content-type', 'text/html')
      res.end(renderDiagnosticsHtml(result))
      return
    }
    res.json(result)
  } catch (err) {
    const status = err.status || 500
    if (wantsDiagnosticsHtml(req)) {
      res.status(status).setHeader('content-type', 'text/html')
      res.end(`<pre>${escapeHtml(err.message)}</pre>`)
      return
    }
    res.status(status).json({ ok: false, error: err.message })
  }
}

const addonInterface = builder.getInterface()
const app = express()
const hasConfig = !!(addonInterface.manifest.config || []).length
const landingHTML = landingTemplate(addonInterface.manifest)

// /resolve — localhost or X-Admin-Token only; DebridNest URL from env; token via Authorization Bearer.
app.get('/resolve', requireLocalOrAdmin, async (req, res) => {
  try {
    const result = await resolveFromQuery(req)
    res.json(result)
  } catch (err) {
    res.status(err.status || 500).json({ error: err.message })
  }
})

app.get('/ready/:streamId', async (req, res) => {
  const entry = externalPlayer.getStream(req.params.streamId)
  if (!entry) {
    res.status(404).json({ ready: false, error: 'Stream link expired or not found' })
    return
  }

  try {
    const url = await playHandler.resolvePlayUrl(entry)
    res.json({ ready: true, url })
  } catch (err) {
    if (err.message === 'Stream not ready') {
      res.status(503).json({ ready: false })
      return
    }
    res.status(502).json({ ready: false, error: err.message })
  }
})

app.get('/open/:streamId', async (req, res) => {
  const entry = externalPlayer.getStream(req.params.streamId)
  if (!entry) {
    res.status(404).send('Stream link expired or not found')
    return
  }

  const playUrl = externalPlayer.buildPlayUrl(req.params.streamId, ADDON_BASE_URL)
  const pageUrl = `${ADDON_BASE_URL}/open/${req.params.streamId}`
  const readyUrl = `${ADDON_BASE_URL}/ready/${req.params.streamId}`

  if (req.query.format === 'iina') {
    try {
      const downloadUrl = await playHandler.waitForPlayUrl(entry)
      res.redirect(302, externalPlayer.buildIinaUrl(downloadUrl))
    } catch (err) {
      console.error(`[open/iina] ${req.params.streamId}: ${err.message}`)
      if (isAuthError(err)) {
        sendAuthError(res)
      } else {
        res.status(504).send(`Still buffering: ${entry.label || 'stream'}. Open ${pageUrl} in your browser and retry.`)
      }
    }
    return
  }

  res.setHeader('content-type', 'text/html')
  res.end(externalPlayer.buildOpenPageHtml({
    streamUrl: playUrl,
    label: entry.label,
    readyUrl,
    copyPageUrl: pageUrl,
  }))
})

app.get('/play/:streamId', async (req, res) => {
  const entry = externalPlayer.getStream(req.params.streamId)
  if (!entry) {
    res.status(404).send('Stream link expired or not found')
    return
  }

  try {
    const downloadUrl = await playHandler.waitForPlayUrl(entry)
    res.redirect(302, downloadUrl)
  } catch (err) {
    console.error(`[play] ${req.params.streamId}: ${err.message}`)
    sendPlayError(res, entry, err)
  }
})

app.get('/progress/:token', async (req, res) => {
  const job = progress.getJob(req.params.token)
  if (!job) {
    res.status(404).send('Download job not found or expired')
    return
  }

  try {
    await progressHandler.handleProgressRequest(req, res, job)
  } catch (err) {
    console.error(`[progress] ${req.params.token}: ${err.message}`)
    res.status(502).send(`Download failed: ${err.message}`)
  }
})

app.get('/diagnostics/streams', requireDiagnosticsAccess, handleStreamDiagnostics)
app.get('/diagnostics/streams/:type/:id', requireDiagnosticsAccess, handleStreamDiagnostics)

// /diagnostics — localhost or X-Admin-Token only; backend URLs/tokens from env; DebridNest token via Authorization Bearer.
app.get('/diagnostics', requireDiagnosticsAccess, async (req, res) => {
  const config = getServerSideConfig(req)

  const result = {
    ok: true,
    debridnest: { ok: false },
    jackett: { ok: false },
    hints: [],
  }

  try {
    requireDebridNestConfig(config)
    const user = await debridnest.getUser(config.apiUrl, config.apiToken)
    result.debridnest = { ok: true, username: user.username }
  } catch (err) {
    result.ok = false
    result.debridnest = { ok: false, error: err.message }
    result.hints.push('Set DebridNest API token to match DEBRIDNEST_API_TOKEN in .env')
  }

  const indexerStats = await jackettConfig.fetchIndexerStats(config.jackettUrl, config.jackettApiKey)
  if (indexerStats.error) {
    result.ok = false
    result.jackett = { ok: false, error: indexerStats.error }
    result.hints.push('Check Jackett URL (Docker: http://jackett:9117) and API key')
  } else {
    result.jackett = {
      ok: indexerStats.configured > 0,
      configuredIndexers: indexerStats.configured,
      totalIndexers: indexerStats.total,
    }
    if (indexerStats.configured === 0) {
      result.ok = false
      result.hints.push('Open http://localhost:9117 and add at least one indexer')
    }
  }

  res.json(result)
})

// /health — localhost or X-Admin-Token only; DebridNest URL from env; token via Authorization Bearer.
app.get('/health', requireLocalOrAdmin, async (req, res) => {
  try {
    const config = getServerSideConfig(req)
    requireDebridNestConfig(config)
    const user = await debridnest.getUser(config.apiUrl, config.apiToken)
    res.json({ ok: true, user: { username: user.username, type: user.type } })
  } catch (err) {
    res.status(err.status || 500).json({ ok: false, error: err.message })
  }
})

app.use((req, res, next) => {
  if (req.path.includes('/stream/')) {
    console.log(`[http] ${req.method} ${safeRequestPath(req)}`)
  }
  next()
})

app.use(getRouter(addonInterface))

app.get('/', (_, res) => {
  if (hasConfig) {
    res.redirect('/configure')
    return
  }
  res.setHeader('content-type', 'text/html')
  res.end(landingHTML)
})

if (hasConfig) {
  app.get('/configure', (_, res) => {
    res.setHeader('content-type', 'text/html')
    res.end(landingHTML)
  })
}

app.listen(PORT, () => {
  console.log(`DebridNest Stremio addon listening on http://127.0.0.1:${PORT}/manifest.json`)
})
