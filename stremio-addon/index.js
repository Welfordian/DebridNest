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
const DEFAULT_DEDUPE_STREAMS = process.env.DEDUPE_STREAMS === '1'
const DEFAULT_PREFER_SEASON_PACKS = process.env.PREFER_SEASON_PACKS === '1'
const PLACEHOLDER_COUNT = Number(process.env.PLACEHOLDER_COUNT || 2)
const PROGRESS_POLL_MS = Number(process.env.PROGRESS_POLL_MS || 2000)
const ENABLE_MAGNET_TEST = process.env.ENABLE_MAGNET_TEST === '1'

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

function getConfig(userConfig = {}) {
  const qualityConfig = quality.resolveQualityConfig(userConfig, {
    preferSdr: DEFAULT_PREFER_SDR,
    maxResolution: DEFAULT_MAX_RESOLUTION,
    maxFileSizeGb: DEFAULT_MAX_FILE_SIZE_GB,
  })
  return {
    apiUrl: configValue(userConfig.apiUrl, DEFAULT_API_URL),
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

function buildStreamObject(entry, streamUrl, options = {}) {
  const { cached = false, placeholder = false } = options
  const label = placeholder
    ? rank.formatPlaceholderLabel(entry)
    : rank.formatStreamLabel(entry, cached)
  const title = entry?.torrent?.title || label

  const stream = {
    name: label,
    title,
    url: streamUrl,
  }

  if (placeholder) {
    stream.behaviorHints = { notWebReady: true }
    return stream
  }

  const streamId = externalPlayer.registerStream(streamUrl, title)
  const openUrl = `${ADDON_BASE_URL}/open/${streamId}`
  stream.title = `${title}\nIINA: ${openUrl}`
  stream.behaviorHints = {
    playerUrl: externalPlayer.buildIinaUrl(streamUrl),
    openInExternal: openUrl,
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
  version: '3.1.0',
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
      title: 'DebridNest API URL',
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
        { torrent: { title: resolved.filename || 'DebridNest' }, quality: { label: '' }, source: '' },
        resolved.download,
      )],
    }
  }

  if (!['movie', 'series'].includes(args.type)) {
    return { streams: [] }
  }

  const meta = await cinemeta.resolveMetadata(args.type, args.id)
  if (!meta) {
    return { streams: [] }
  }

  const qualityConfig = getQualityConfig(config)
  const torrents = await scrapers.searchAll(config, meta)
  const ranked = rank.rankTorrents(torrents, meta, config.maxResults * 2, qualityConfig)
  if (!ranked.length) {
    return { streams: [] }
  }

  const hashes = ranked.map((e) => e.torrent.infoHash).filter(Boolean)
  const availability = await debridnest.checkInstantAvailability(config.apiUrl, config.apiToken, hashes)
  const ordered = rank.applyCachePriority(ranked, availability).slice(0, config.maxResults)

  const streams = []
  let placeholders = 0

  for (const entry of ordered) {
    const cached = rank.isEntryCached(entry, availability)

    if (cached) {
      try {
        const resolved = await debridnest.resolveCachedOnly(
          config.apiUrl,
          config.apiToken,
          entry.torrent.magnet,
        )
        if (resolved) {
          streams.push(buildStreamObject(entry, resolved.download, { cached: true }))
          continue
        }
      } catch {
        // fall through to placeholder
      }
    }

    if (placeholders >= PLACEHOLDER_COUNT) {
      continue
    }

    try {
      const torrentId = await debridnest.startDownload(
        config.apiUrl,
        config.apiToken,
        entry.torrent.magnet,
      )
      const token = progress.createJob({
        torrentId,
        apiUrl: config.apiUrl,
        apiToken: config.apiToken,
        label: entry.torrent.title,
      })
      streams.push(buildStreamObject(
        entry,
        `${ADDON_BASE_URL}/progress/${token}`,
        { placeholder: true },
      ))
      placeholders++
    } catch {
      // skip failed starts
    }
  }

  return { streams }
})

async function resolveFromQuery(req) {
  const magnet = req.query.magnet
  if (!isMagnetUri(magnet)) {
    const err = new Error('Query parameter "magnet" must be a magnet URI.')
    err.status = 400
    throw err
  }
  const config = getConfig({
    apiUrl: req.query.apiUrl,
    apiToken: req.query.apiToken,
  })
  requireDebridNestConfig(config)
  return debridnest.resolveMagnet(config.apiUrl, config.apiToken, magnet.trim())
}

const addonInterface = builder.getInterface()
const app = express()
const hasConfig = !!(addonInterface.manifest.config || []).length
const landingHTML = landingTemplate(addonInterface.manifest)

app.get('/resolve', async (req, res) => {
  try {
    const result = await resolveFromQuery(req)
    res.json(result)
  } catch (err) {
    res.status(err.status || 500).json({ error: err.message })
  }
})

app.get('/open/:streamId', (req, res) => {
  const entry = externalPlayer.getStream(req.params.streamId)
  if (!entry) {
    res.status(404).send('Stream link expired or not found')
    return
  }

  const iinaUrl = externalPlayer.buildIinaUrl(entry.url)
  const pageUrl = `${ADDON_BASE_URL}/open/${req.params.streamId}`

  if (req.query.format === 'iina') {
    res.redirect(302, iinaUrl)
    return
  }

  res.setHeader('content-type', 'text/html')
  res.end(externalPlayer.buildOpenPageHtml({
    streamUrl: entry.url,
    label: entry.label,
    iinaUrl,
    copyPageUrl: pageUrl,
  }))
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

app.get('/diagnostics', async (req, res) => {
  const config = getConfig({
    apiUrl: req.query.apiUrl,
    apiToken: req.query.apiToken,
    jackettUrl: req.query.jackettUrl,
    jackettApiKey: req.query.jackettApiKey,
  })

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

  if (jackettConfig.isPlaceholderKey(req.query.jackettApiKey) && !jackettConfig.isPlaceholderKey(config.jackettApiKey)) {
    result.hints.push('Manifest uses a placeholder Jackett key; empty field or reinstall recommended')
  }

  res.json(result)
})

app.get('/health', async (req, res) => {
  try {
    const config = getConfig({
      apiUrl: req.query.apiUrl,
      apiToken: req.query.apiToken,
    })
    requireDebridNestConfig(config)
    const user = await debridnest.getUser(config.apiUrl, config.apiToken)
    res.json({ ok: true, user: { username: user.username, type: user.type } })
  } catch (err) {
    res.status(err.status || 500).json({ ok: false, error: err.message })
  }
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
