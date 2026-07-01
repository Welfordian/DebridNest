const { XMLParser } = require('fast-xml-parser')

const JACKETT_TIMEOUT_MS = Number(process.env.JACKETT_TIMEOUT_MS || 12000)
const DEFAULT_MOVIE_EXPAND_MIN_RESULTS = Number(process.env.JACKETT_MOVIE_EXPAND_MIN_RESULTS || 20)
const TORRENT_FILE_CACHE_TTL_MS = Number(process.env.JACKETT_TORRENT_FILE_CACHE_TTL_MS || 600000)
const TORRENT_FILE_CACHE_MAX = Number(process.env.JACKETT_TORRENT_FILE_CACHE_MAX || 64)

const torrentFileCache = new Map()

function normalizeBaseUrl(url) {
  return String(url || '').replace(/\/+$/, '')
}

function extractIndexerFromTitle(title) {
  const match = String(title || '').match(/^\[([^\]]+)\]/)
  return match ? match[1].trim() : null
}

function normalizeIndexerValue(value) {
  if (!value) {
    return null
  }
  if (typeof value === 'object') {
    return normalizeIndexerValue(
      value['#text']
      || value['@_name']
      || value['@_id']
      || value.name
      || value.id,
    )
  }
  const text = String(value).trim()
  return text || null
}

function parseIndexer(attrs) {
  const candidates = [
    attrs.jackettindexer,
    attrs.indexer,
    findAttr(attrs, 'jackettindexer'),
    findAttr(attrs, 'jackett indexer'),
    findAttr(attrs, 'indexer'),
    findAttr(attrs, 'tracker'),
    extractIndexerFromTitle(attrs.title),
  ]
  for (const candidate of candidates) {
    const normalized = normalizeIndexerValue(candidate)
    if (normalized) {
      return normalized
    }
  }
  return null
}

function parseTorznabItem(item) {
  const attrs = item || {}
  const title = attrs.title || ''
  const link = attrs.link || attrs.guid || ''
  const size = Number(attrs.size || attrs['@_size'] || findAttr(attrs, 'size') || 0)
  const seeders = Number(
    attrs.seeders || attrs['@_seeders'] || findAttr(attrs, 'seeders') || 0,
  )
  const peers = Number(attrs.peers || attrs['@_peers'] || findAttr(attrs, 'peers') || 0)
  const leechers = Number(attrs.leechers || attrs['@_leechers'] || findAttr(attrs, 'leechers') || 0)
  const magnet = attrs.magneturl || attrs['torznab:attr']?.find?.((a) => a['@_name'] === 'magneturl')?.['@_value']
    || extractMagnetFromAttrs(attrs)
    || (String(link).startsWith('magnet:') ? link : null)
  const infoHash = attrs.infohash
    || findAttr(attrs, 'infohash')
    || magnetInfoHash(magnet)

  return {
    title,
    magnet,
    infoHash: infoHash ? infoHash.toLowerCase() : null,
    size,
    seeders: seeders || Math.max(0, peers - leechers),
    leechers,
    indexer: parseIndexer(attrs),
    link,
  }
}

function extractMagnetFromAttrs(attrs) {
  const torznabAttrs = attrs['torznab:attr']
  if (!torznabAttrs) {
    return null
  }
  const list = Array.isArray(torznabAttrs) ? torznabAttrs : [torznabAttrs]
  for (const attr of list) {
    if (attr['@_name'] === 'magneturl') {
      return attr['@_value']
    }
  }
  return null
}

function findAttr(attrs, name) {
  const torznabAttrs = attrs['torznab:attr']
  if (!torznabAttrs) {
    return null
  }
  const list = Array.isArray(torznabAttrs) ? torznabAttrs : [torznabAttrs]
  const expected = String(name || '').toLowerCase()
  for (const attr of list) {
    if (String(attr['@_name'] || '').toLowerCase() === expected) {
      return attr['@_value']
    }
  }
  return null
}

function magnetInfoHash(magnet) {
  if (!magnet) {
    return null
  }
  const match = magnet.match(/btih:([a-fA-F0-9]{40})/i)
  return match ? match[1].toLowerCase() : null
}

function isJackettDownloadLink(link) {
  if (!link || typeof link !== 'string') {
    return false
  }
  try {
    return new URL(link).pathname.includes('/dl/')
  } catch {
    return false
  }
}

async function resolveMagnetFromJackettLink(link) {
  if (!isJackettDownloadLink(link)) {
    return null
  }
  try {
    const res = await fetch(link, {
      redirect: 'manual',
      signal: AbortSignal.timeout(JACKETT_TIMEOUT_MS),
    })
    const location = res.headers.get('location')
    if (location && location.toLowerCase().startsWith('magnet:')) {
      return location
    }
  } catch {
    // Jackett proxy links expire or may be unreachable; skip quietly.
  }
  return null
}

function getTorrentFileCacheEntry(link) {
  if (TORRENT_FILE_CACHE_TTL_MS <= 0) {
    return null
  }
  const entry = torrentFileCache.get(link)
  if (!entry) {
    return null
  }
  if (entry.expiresAt <= Date.now()) {
    torrentFileCache.delete(link)
    return null
  }
  torrentFileCache.delete(link)
  torrentFileCache.set(link, entry)
  return entry
}

function setTorrentFileCacheEntry(link, entry) {
  if (TORRENT_FILE_CACHE_TTL_MS <= 0) {
    return
  }
  torrentFileCache.set(link, {
    ...entry,
    expiresAt: Date.now() + TORRENT_FILE_CACHE_TTL_MS,
  })
  while (torrentFileCache.size > TORRENT_FILE_CACHE_MAX) {
    const oldest = torrentFileCache.keys().next().value
    torrentFileCache.delete(oldest)
  }
}

async function fetchTorrentFile(link) {
  if (!isJackettDownloadLink(link)) {
    return null
  }
  try {
    const res = await fetch(link, {
      redirect: 'follow',
      signal: AbortSignal.timeout(JACKETT_TIMEOUT_MS),
    })
    if (!res.ok) {
      return null
    }
    const data = Buffer.from(await res.arrayBuffer())
    if (data.length < 20 || data[0] !== 0x64) {
      return null
    }
    return data
  } catch {
    return null
  }
}

async function downloadTorrentFile(link) {
  if (!isJackettDownloadLink(link)) {
    return null
  }
  const cached = getTorrentFileCacheEntry(link)
  if (cached?.data) {
    return cached.data
  }
  if (cached?.promise) {
    return cached.promise
  }

  const promise = fetchTorrentFile(link)
    .then((data) => {
      if (data?.length) {
        setTorrentFileCacheEntry(link, { data })
      } else {
        torrentFileCache.delete(link)
      }
      return data
    })
    .catch((err) => {
      torrentFileCache.delete(link)
      throw err
    })

  setTorrentFileCacheEntry(link, { promise })
  return promise
}

function warmTorrentFileCache(links, limit = 5) {
  const list = Array.isArray(links) ? links : [links]
  let warmed = 0
  for (const link of list) {
    if (warmed >= limit) {
      break
    }
    if (!isJackettDownloadLink(link)) {
      continue
    }
    if (getTorrentFileCacheEntry(link)) {
      warmed++
      continue
    }
    downloadTorrentFile(link).catch(() => {})
    warmed++
  }
}

async function enrichTorrentMagnets(items) {
  const pending = items.filter(
    (item) => !item.magnet && !item.infoHash && isJackettDownloadLink(item.link),
  )
  if (!pending.length) {
    return items
  }

  await Promise.all(pending.map(async (item) => {
    const magnet = await resolveMagnetFromJackettLink(item.link)
    if (!magnet) {
      return
    }
    item.magnet = magnet
    item.infoHash = magnetInfoHash(magnet) || item.infoHash
  }))

  return items
}

function pad2(n) {
  return String(n).padStart(2, '0')
}

function buildSearchQuery(meta, options = {}) {
  const includeYear = options.includeYear !== false
  if (meta.type === 'series' && meta.season != null && meta.episode != null) {
    const title = meta.title ? `${meta.title} ` : ''
    return `${title}S${pad2(meta.season)}E${pad2(meta.episode)}`.trim()
  }
  if (meta.type === 'series' && meta.season != null) {
    const title = meta.title ? `${meta.title} ` : ''
    return `${title}S${pad2(meta.season)}`.trim()
  }
  if (includeYear && meta.title && meta.year) {
    return `${meta.title} ${meta.year}`
  }
  return meta.title || meta.imdbId || ''
}

function torznabUrl(baseUrl, apiKey, meta, options = {}) {
  const { useImdb = true, useTextQuery = true, includeYear = true } = options
  const base = normalizeBaseUrl(baseUrl)
  const isSeries = meta.type === 'series'
  const params = new URLSearchParams({
    apikey: apiKey,
    t: isSeries ? 'tvsearch' : 'movie',
    cat: isSeries ? '5000,5040,5045' : '2000,2070',
    limit: '100',
    o: 'seeders',
    extended: '1',
  })

  if (isSeries) {
    if (useImdb && meta.imdbId) {
      params.set('imdbid', meta.imdbId.replace(/^tt/i, ''))
    }
    if (meta.season != null && !Number.isNaN(meta.season)) {
      params.set('season', String(meta.season))
    }
    if (meta.episode != null && !Number.isNaN(meta.episode)) {
      params.set('ep', String(meta.episode))
    }
  } else if (useImdb && meta.imdbId) {
    params.set('imdbid', meta.imdbId.replace(/^tt/i, ''))
  }

  if (useTextQuery) {
    const query = buildSearchQuery(meta, { includeYear })
    if (query) {
      params.set('q', query)
    }
  }

  if (base.includes('/api/v1/indexer') || /\/\d+\/api$/u.test(base)) {
    return `${base}?${params.toString()}`
  }
  return `${base}/api/v2.0/indexers/all/results/torznab/api?${params.toString()}`
}

async function fetchTorznabDetailed(jackettUrl, jackettApiKey, meta, options) {
  const url = torznabUrl(jackettUrl, jackettApiKey, meta, options)
  const res = await fetch(url, {
    signal: AbortSignal.timeout(JACKETT_TIMEOUT_MS),
  })
  const body = await res.text()

  if (!res.ok) {
    if (meta.type === 'series' && (res.status === 400 || res.status === 404)) {
      return { torrents: [], rawCount: 0 }
    }
    throw new Error(`Jackett search failed: ${res.status}`)
  }

  const parser = new XMLParser({
    ignoreAttributes: false,
    attributeNamePrefix: '@_',
  })
  const doc = parser.parse(body)
  const channel = doc?.rss?.channel || doc?.channel
  if (channel?.error) {
    return { torrents: [], rawCount: 0 }
  }
  const rawItems = channel?.item
  if (!rawItems) {
    return { torrents: [], rawCount: 0 }
  }
  const items = Array.isArray(rawItems) ? rawItems : [rawItems]

  const parsed = items.map(parseTorznabItem)
  await enrichTorrentMagnets(parsed)
  return {
    torrents: parsed.filter((item) => item.magnet || item.infoHash),
    rawCount: items.length,
  }
}

async function fetchTorznab(jackettUrl, jackettApiKey, meta, options) {
  const result = await fetchTorznabDetailed(jackettUrl, jackettApiKey, meta, options)
  return result.torrents
}

function normalizeIdentityText(value) {
  return String(value || '')
    .toLowerCase()
    .replace(/\[.*?\]/g, ' ')
    .replace(/[^a-z0-9]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
}

function torrentIdentity(torrent) {
  const hash = torrent.infoHash || magnetInfoHash(torrent.magnet)
  if (hash) {
    return `hash:${String(hash).toLowerCase()}`
  }
  const link = String(torrent.link || '').trim()
  if (link && !link.toLowerCase().startsWith('magnet:')) {
    return `link:${link}`
  }
  const title = normalizeIdentityText(torrent.title)
  if (title) {
    return `title:${title}:${Number(torrent.size || 0)}`
  }
  return null
}

function pickBetterTorrent(a, b) {
  const aSeeders = Number(a.seeders || 0)
  const bSeeders = Number(b.seeders || 0)
  if (aSeeders !== bSeeders) {
    return aSeeders > bSeeders ? a : b
  }
  const aSize = Number(a.size || 0)
  const bSize = Number(b.size || 0)
  return aSize >= bSize ? a : b
}

function mergeTorrentResults(existing, additions) {
  const merged = existing.slice()
  const byIdentity = new Map()

  for (let i = 0; i < merged.length; i++) {
    const key = torrentIdentity(merged[i])
    if (key) {
      byIdentity.set(key, i)
    }
  }

  for (const torrent of additions) {
    const key = torrentIdentity(torrent)
    if (!key) {
      merged.push(torrent)
      continue
    }
    const existingIndex = byIdentity.get(key)
    if (existingIndex == null) {
      byIdentity.set(key, merged.length)
      merged.push(torrent)
      continue
    }
    merged[existingIndex] = pickBetterTorrent(merged[existingIndex], torrent)
  }

  return merged
}

async function searchTorrentsDetailed(jackettUrl, jackettApiKey, meta, options = {}) {
  if (!jackettUrl || !jackettApiKey) {
    throw new Error('Jackett/Prowlarr URL and API key are required')
  }

  const diagnostics = {
    rawCount: 0,
    usableCount: 0,
    queries: [],
    errors: [],
  }
  let merged = []

  function recordVariant(label, result) {
    diagnostics.rawCount += result.rawCount
    diagnostics.queries.push({
      label,
      rawCount: result.rawCount,
      usableCount: result.torrents.length,
    })
    merged = mergeTorrentResults(merged, result.torrents)
    diagnostics.usableCount = merged.length
  }

  async function fetchVariant(label, variantOptions) {
    try {
      const result = await fetchTorznabDetailed(jackettUrl, jackettApiKey, meta, variantOptions)
      return { label, result }
    } catch (err) {
      const message = err?.message || String(err)
      return { label, error: err, message }
    }
  }

  function recordVariantOutcome(outcome) {
    if (!outcome.error) {
      recordVariant(outcome.label, outcome.result)
      return
    }
    diagnostics.errors.push(`${outcome.label}: ${outcome.message}`)
    diagnostics.queries.push({
      label: outcome.label,
      rawCount: 0,
      usableCount: 0,
      error: outcome.message,
    })
  }

  async function runVariant(label, variantOptions) {
    const outcome = await fetchVariant(label, variantOptions)
    recordVariantOutcome(outcome)
    if (outcome.error && merged.length === 0) {
      throw outcome.error
    }
    return outcome.result || { torrents: [], rawCount: 0 }
  }

  async function runVariantsConcurrently(variants, options = {}) {
    const enabled = variants.filter((variant) => variant.enabled !== false)
    const outcomes = await Promise.all(enabled.map((variant) => fetchVariant(variant.label, variant.options)))
    if (options.firstNonEmpty) {
      for (const outcome of outcomes) {
        if (outcome.error) {
          recordVariantOutcome(outcome)
          continue
        }
        recordVariantOutcome(outcome)
        if (outcome.result.torrents.length > 0) {
          return
        }
      }
    } else {
      for (const outcome of outcomes) {
        recordVariantOutcome(outcome)
      }
    }

    const firstError = outcomes.find((outcome) => outcome.error)
    if (merged.length === 0 && firstError) {
      throw firstError.error
    }
  }

  if (meta.type === 'series') {
    // Public indexers rarely support imdbid on tvsearch; text query is reliable.
    await runVariant('series-title-episode', {
      useImdb: false,
      useTextQuery: true,
    })

    if (merged.length === 0) {
      await runVariantsConcurrently([
        {
          label: 'series-season-episode',
          options: {
            useImdb: false,
            useTextQuery: false,
          },
        },
        {
          label: 'series-imdb-only',
          enabled: !!meta.imdbId,
          options: {
            useImdb: true,
            useTextQuery: false,
          },
        },
      ], { firstNonEmpty: true })
    }

    return { torrents: merged, diagnostics }
  }

  const minMovieResults = Math.max(
    0,
    Number(options.minMovieResults ?? DEFAULT_MOVIE_EXPAND_MIN_RESULTS) || 0,
  )
  const variants = [
    {
      label: 'imdb-title-year',
      enabled: true,
      options: { useImdb: true, useTextQuery: true, includeYear: true },
    },
    {
      label: 'imdb-only',
      enabled: !!meta.imdbId,
      options: { useImdb: true, useTextQuery: false },
    },
    {
      label: 'title-year',
      enabled: !!meta.title,
      options: { useImdb: false, useTextQuery: true, includeYear: true },
    },
    {
      label: 'title-only',
      enabled: !!meta.title && !!meta.year,
      options: { useImdb: false, useTextQuery: true, includeYear: false },
    },
  ]

  const primary = variants.find((variant) => variant.enabled)
  if (primary) {
    await runVariant(primary.label, primary.options)
  }

  if (merged.length < minMovieResults) {
    await runVariantsConcurrently(variants.slice(1))
  }

  if (!primary) {
    await runVariant('fallback', {
      useImdb: false,
      useTextQuery: true,
    })
  }

  return { torrents: merged, diagnostics }
}

async function searchTorrents(jackettUrl, jackettApiKey, meta, options = {}) {
  const result = await searchTorrentsDetailed(jackettUrl, jackettApiKey, meta, options)
  return result.torrents
}

module.exports = {
  searchTorrents,
  searchTorrentsDetailed,
  buildSearchQuery,
  torznabUrl,
  downloadTorrentFile,
  warmTorrentFileCache,
  isJackettDownloadLink,
}
