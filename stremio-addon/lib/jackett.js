const { XMLParser } = require('fast-xml-parser')

const JACKETT_TIMEOUT_MS = Number(process.env.JACKETT_TIMEOUT_MS || 12000)

function normalizeBaseUrl(url) {
  return String(url || '').replace(/\/+$/, '')
}

function extractIndexerFromTitle(title) {
  const match = String(title || '').match(/^\[([^\]]+)\]/)
  return match ? match[1].trim() : null
}

function parseIndexer(attrs) {
  return findAttr(attrs, 'jackett indexer')
    || findAttr(attrs, 'indexer')
    || extractIndexerFromTitle(attrs.title)
    || null
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
  for (const attr of list) {
    if (attr['@_name'] === name) {
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

function pad2(n) {
  return String(n).padStart(2, '0')
}

function buildSearchQuery(meta) {
  if (meta.type === 'series' && meta.season != null && meta.episode != null) {
    const title = meta.title ? `${meta.title} ` : ''
    return `${title}S${pad2(meta.season)}E${pad2(meta.episode)}`.trim()
  }
  if (meta.type === 'series' && meta.season != null) {
    const title = meta.title ? `${meta.title} ` : ''
    return `${title}S${pad2(meta.season)}`.trim()
  }
  if (meta.title && meta.year) {
    return `${meta.title} ${meta.year}`
  }
  return meta.title || meta.imdbId || ''
}

function torznabUrl(baseUrl, apiKey, meta, options = {}) {
  const { useImdb = true, useTextQuery = true } = options
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
    const query = buildSearchQuery(meta)
    if (query) {
      params.set('q', query)
    }
  }

  if (base.includes('/api/v1/indexer') || /\/\d+\/api$/u.test(base)) {
    return `${base}?${params.toString()}`
  }
  return `${base}/api/v2.0/indexers/all/results/torznab/api?${params.toString()}`
}

async function fetchTorznab(jackettUrl, jackettApiKey, meta, options) {
  const url = torznabUrl(jackettUrl, jackettApiKey, meta, options)
  const res = await fetch(url, {
    signal: AbortSignal.timeout(JACKETT_TIMEOUT_MS),
  })
  const body = await res.text()

  if (!res.ok) {
    if (meta.type === 'series' && (res.status === 400 || res.status === 404)) {
      return []
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
    return []
  }
  const rawItems = channel?.item
  if (!rawItems) {
    return []
  }
  const items = Array.isArray(rawItems) ? rawItems : [rawItems]

  return items
    .map(parseTorznabItem)
    .filter((item) => item.magnet || item.infoHash)
}

async function searchTorrents(jackettUrl, jackettApiKey, meta) {
  if (!jackettUrl || !jackettApiKey) {
    throw new Error('Jackett/Prowlarr URL and API key are required')
  }

  if (meta.type === 'series') {
    // Public indexers rarely support imdbid on tvsearch; text query is reliable.
    let results = await fetchTorznab(jackettUrl, jackettApiKey, meta, {
      useImdb: false,
      useTextQuery: true,
    })

    if (results.length === 0) {
      results = await fetchTorznab(jackettUrl, jackettApiKey, meta, {
        useImdb: false,
        useTextQuery: false,
      })
    }

    if (results.length === 0 && meta.imdbId) {
      results = await fetchTorznab(jackettUrl, jackettApiKey, meta, {
        useImdb: true,
        useTextQuery: false,
      })
    }

    return results
  }

  let results = await fetchTorznab(jackettUrl, jackettApiKey, meta, {
    useImdb: true,
    useTextQuery: true,
  })

  if (results.length === 0 && meta.title) {
    results = await fetchTorznab(jackettUrl, jackettApiKey, meta, {
      useImdb: false,
      useTextQuery: true,
    })
  }

  return results
}

module.exports = {
  searchTorrents,
  buildSearchQuery,
  torznabUrl,
}
