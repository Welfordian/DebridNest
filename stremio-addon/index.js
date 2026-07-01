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
const PROGRESS_POLL_MS = Number(process.env.PROGRESS_POLL_MS || 2000)
const ENABLE_MAGNET_TEST = process.env.ENABLE_MAGNET_TEST === '1'
const ADMIN_TOKEN = process.env.ADMIN_TOKEN || ''

const LOCAL_IPS = new Set(['127.0.0.1', '::1', '::ffff:127.0.0.1'])

function isLocalRequest(req) {
  return LOCAL_IPS.has(req.ip || req.socket?.remoteAddress || '')
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

function extractBearerToken(req) {
  const auth = req.get('Authorization') || ''
  const match = auth.match(/^Bearer\s+(.+)$/i)
  return match ? match[1].trim() : ''
}

function getServerSideConfig(req) {
  const bearerToken = extractBearerToken(req)
  return {
    apiUrl: DEFAULT_API_URL,
    apiToken: bearerToken || DEFAULT_API_TOKEN,
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
    apiToken: configValue(userConfig.apiToken, DEFAULT_API_TOKEN),
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
  if (!config.jackettUrl || !config.jackettApiKey) {
    throw new Error('Jackett/Prowlarr URL and API key are required.')
  }
}

const manifest = {
  id: 'com.debridnest.streams',
  version: '3.1.12',
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
    const resolved = await debridnest.resolveMagnet(config.apiUrl, config.apiToken, magnet)
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
  const torrents = await scrapers.searchAll(config, meta)
  if (!torrents.length) {
    console.warn(
      `[streams] No Jackett results for ${meta.title || meta.imdbId}`
      + (meta.season != null ? ` S${meta.season}E${meta.episode}` : ''),
    )
  }
  const ranked = rank.rankTorrents(torrents, meta, config.maxResults * 2, qualityConfig)
  if (!ranked.length) {
    if (torrents.length) {
      console.warn(
        `[streams] ${torrents.length} Jackett results filtered out for ${meta.title} S${meta.season}E${meta.episode}`,
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
  const ordered = rank.applyCachePriority(ranked, availability).slice(0, config.maxResults)

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

    if (cached && resolvedCount < LIST_RESOLVE_COUNT && (entry.torrent.magnet || entry.torrent.link)) {
      const candidateOpts = {
        maxWaitMs: CACHED_RESOLVE_WAIT_MS,
        torrentLink: entry.torrent.link,
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

  let placeholders = 0
  for (const { entry, cached } of placeholderCandidates) {
    if (streams.length >= maxStreams || placeholders >= maxPlaceholders) {
      break
    }

    try {
      const progressToken = progress.createJob({
        magnet: entry.torrent.magnet,
        torrentLink: entry.torrent.link,
        apiUrl: config.apiUrl,
        apiToken: config.apiToken,
        label: entry.torrent.title,
      })
      streams.push(buildStreamObject(entry, {
        progressToken,
        cached,
      }))
      placeholders++
    } catch {
      // skip failed starts
    }
  }

  console.log(
    `[streams] ${args.type}/${args.id} → ${streams.length} streams`
    + ` (${resolvedCount} direct, ${placeholders} placeholder)`,
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
  return debridnest.resolveMagnet(config.apiUrl, config.apiToken, magnet.trim())
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
      res.status(504).send(`Still buffering: ${entry.label || 'stream'}. Open ${pageUrl} in your browser and retry.`)
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
    res.status(504).send(`Timed out buffering: ${entry.label || 'stream'}. Try another stream source.`)
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

// /diagnostics — localhost or X-Admin-Token only; backend URLs/tokens from env; DebridNest token via Authorization Bearer.
app.get('/diagnostics', requireLocalOrAdmin, async (req, res) => {
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
    console.log(`[http] ${req.method} ${req.path}`)
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
