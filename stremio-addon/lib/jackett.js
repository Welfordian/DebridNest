const { XMLParser } = require('fast-xml-parser')

function normalizeBaseUrl(url) {
  return String(url || '').replace(/\/+$/, '')
}

function parseTorznabItem(item) {
  const attrs = item || {}
  const title = attrs.title || ''
  const link = attrs.link || attrs.guid || ''
  const size = Number(attrs.size || attrs['@_size'] || 0)
  const seeders = Number(attrs.seeders || attrs['@_seeders'] || 0)
  const peers = Number(attrs.peers || attrs['@_peers'] || 0)
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
    seeders: seeders || Math.max(0, peers - Number(attrs.leechers || 0)),
    leechers: Number(attrs.leechers || attrs['@_leechers'] || 0),
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

function buildSearchQuery(meta) {
  if (meta.type === 'series' && meta.season != null && meta.episode != null) {
    return `${meta.imdbId} S${pad2(meta.season)}E${pad2(meta.episode)}`
  }
  if (meta.type === 'series' && meta.season != null) {
    return `${meta.imdbId} S${pad2(meta.season)}`
  }
  return meta.imdbId
}

function pad2(n) {
  return String(n).padStart(2, '0')
}

function torznabUrl(baseUrl, apiKey, query, type, imdbId) {
  const base = normalizeBaseUrl(baseUrl)
  const params = new URLSearchParams({
    apikey: apiKey,
    t: type === 'series' ? 'tvsearch' : 'movie',
    cat: '2000,5000',
    limit: '100',
    o: 'seeders',
  })
  if (imdbId) {
    params.set('imdbid', imdbId.replace(/^tt/, ''))
  } else {
    params.set('q', query)
  }
  if (type === 'series') {
    params.set('extended', '1')
    if (query.includes('S') && query.includes('E')) {
      params.set('q', query)
    }
  }
  if (base.includes('/api/v1/indexer') || /\/\d+\/api$/u.test(base)) {
    return `${base}?${params.toString()}`
  }
  return `${base}/api/v2.0/indexers/all/results/torznab/api?${params.toString()}`
}

async function searchTorrents(jackettUrl, jackettApiKey, meta) {
  if (!jackettUrl || !jackettApiKey) {
    throw new Error('Jackett/Prowlarr URL and API key are required')
  }

  const query = buildSearchQuery(meta)
  const url = torznabUrl(jackettUrl, jackettApiKey, query, meta.type, meta.imdbId)
  const res = await fetch(url)
  if (!res.ok) {
    throw new Error(`Jackett search failed: ${res.status}`)
  }

  const xml = await res.text()
  const parser = new XMLParser({
    ignoreAttributes: false,
    attributeNamePrefix: '@_',
  })
  const doc = parser.parse(xml)
  const channel = doc?.rss?.channel || doc?.channel
  const rawItems = channel?.item
  if (!rawItems) {
    return []
  }
  const items = Array.isArray(rawItems) ? rawItems : [rawItems]

  return items
    .map(parseTorznabItem)
    .filter((item) => item.magnet || item.infoHash)
}

module.exports = {
  searchTorrents,
  buildSearchQuery,
}
