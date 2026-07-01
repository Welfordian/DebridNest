const jackett = require('./jackett')
const dedupe = require('./dedupe')
const seasonPacks = require('./seasonPacks')

const JACKETT_CACHE_TTL_MS = Number(process.env.JACKETT_CACHE_TTL_MS || 600000)
const searchCache = new Map()

function searchCacheKey(meta) {
  return `${meta.imdbId || ''}:${meta.season ?? ''}:${meta.episode ?? ''}:${meta.type || ''}`
}

function getCachedSearch(meta) {
  if (JACKETT_CACHE_TTL_MS <= 0) {
    return null
  }
  const entry = searchCache.get(searchCacheKey(meta))
  if (!entry || Date.now() > entry.expiresAt) {
    if (entry) {
      searchCache.delete(searchCacheKey(meta))
    }
    return null
  }
  return entry.torrents
}

function setCachedSearch(meta, torrents) {
  if (JACKETT_CACHE_TTL_MS <= 0) {
    return
  }
  searchCache.set(searchCacheKey(meta), {
    torrents,
    expiresAt: Date.now() + JACKETT_CACHE_TTL_MS,
  })
}

async function searchAll(config, meta) {
  try {
    const cached = getCachedSearch(meta)
    if (cached) {
      return cached
    }

    let torrents = await jackett.searchTorrents(config.jackettUrl, config.jackettApiKey, meta)
    if (torrents.length === 0) {
      console.warn('[scrapers] Jackett returned 0 torrents — check indexers at http://localhost:9117')
    }

    if (config.preferSeasonPacks) {
      torrents = await seasonPacks.enrichWithSeasonPacks(
        config.jackettUrl,
        config.jackettApiKey,
        meta,
        torrents,
      )
    }

    if (config.dedupeStreams) {
      const before = torrents.length
      torrents = dedupe.collapseDuplicates(torrents)
      if (before !== torrents.length) {
        console.warn(`[scrapers] Deduped ${before} torrents to ${torrents.length}`)
      }
    }

    setCachedSearch(meta, torrents)
    return torrents
  } catch (err) {
    console.error('[scrapers] Jackett/Prowlarr search failed:', err?.message || err)
    return []
  }
}

module.exports = {
  searchAll,
}
