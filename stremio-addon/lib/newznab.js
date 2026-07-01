const { XMLParser } = require('fast-xml-parser')

const NEWZNAB_TIMEOUT_MS = Number(process.env.NEWZNAB_TIMEOUT_MS || process.env.JACKETT_TIMEOUT_MS || 12000)

function normalizeBaseUrl(url) {
  return String(url || '').replace(/\/+$/, '')
}

function buildSearchUrl(baseUrl, apiKey, meta) {
  const url = new URL(normalizeBaseUrl(baseUrl))
  url.searchParams.set('apikey', apiKey)
  url.searchParams.set('limit', '100')

  if (meta.type === 'series' && meta.imdbId && meta.season != null) {
    url.searchParams.set('t', 'tvsearch')
    url.searchParams.set('imdbid', meta.imdbId.replace(/^tt/i, ''))
    url.searchParams.set('season', String(meta.season))
    if (meta.episode != null) {
      url.searchParams.set('ep', String(meta.episode))
    }
    return url
  }

  url.searchParams.set('t', 'search')
  const query = meta.type === 'movie' && meta.imdbId
    ? meta.imdbId
    : [meta.title, meta.year].filter(Boolean).join(' ')
  url.searchParams.set('q', query || meta.imdbId || 'unknown')
  return url
}

function parseNewznabItem(item) {
  const attrs = item || {}
  const title = attrs.title || ''
  const link = attrs.link || attrs.guid || ''
  const enclosure = attrs.enclosure || {}
  const enclosureUrl = typeof enclosure === 'object' ? enclosure['@_url'] || enclosure.url : null
  const nzbUrl = enclosureUrl || (String(link).includes('.nzb') ? link : null) || link
  const size = Number(
    attrs.size
      || attrs['@_size']
      || enclosure['@_length']
      || findAttr(attrs, 'size')
      || 0,
  )
  const guid = findAttr(attrs, 'guid') || attrs.guid || nzbUrl

  return {
    title: title.replace(/^\[[^\]]+\]\s*/, ''),
    nzbUrl,
    guid,
    size,
    seeders: 0,
    leechers: 0,
    indexer: findAttr(attrs, 'indexer') || 'usenet',
    source: 'usenet',
  }
}

function findAttr(attrs, name) {
  const newznabAttrs = attrs['newznab:attr'] || attrs['torznab:attr']
  if (!newznabAttrs) {
    return null
  }
  const list = Array.isArray(newznabAttrs) ? newznabAttrs : [newznabAttrs]
  for (const attr of list) {
    if (attr['@_name'] === name) {
      return attr['@_value']
    }
  }
  return null
}

async function searchNewznab(baseUrl, apiKey, meta) {
  if (!baseUrl || !apiKey) {
    return []
  }

  const searchUrl = buildSearchUrl(baseUrl, apiKey, meta)
  const res = await fetch(searchUrl, { signal: AbortSignal.timeout(NEWZNAB_TIMEOUT_MS) })
  if (!res.ok) {
    throw new Error(`Newznab search failed: HTTP ${res.status}`)
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
    .map(parseNewznabItem)
    .filter((item) => item.nzbUrl && String(item.nzbUrl).startsWith('http'))
}

module.exports = {
  searchNewznab,
}
