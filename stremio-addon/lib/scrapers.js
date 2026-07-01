const jackett = require('./jackett')
const dedupe = require('./dedupe')
const seasonPacks = require('./seasonPacks')
const rank = require('./rank')
const quality = require('./quality')

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
  return cloneSearchResult(entry.result, true)
}

function setCachedSearch(meta, result) {
  if (JACKETT_CACHE_TTL_MS <= 0) {
    return
  }
  searchCache.set(searchCacheKey(meta), {
    result: cloneSearchResult(result, false),
    expiresAt: Date.now() + JACKETT_CACHE_TTL_MS,
  })
}

async function searchAll(config, meta) {
  const result = await searchAllDetailed(config, meta)
  return result.torrents
}

function emptySearchResult(error = null) {
  return {
    torrents: [],
    counts: {
      raw: 0,
      search: 0,
      afterSeasonPackMark: 0,
      afterSeasonPackEnrich: 0,
      beforeDedupe: 0,
      afterDedupe: 0,
      deduped: 0,
    },
    queries: [],
    jackettQueries: [],
    errors: error ? [error] : [],
    cacheHit: false,
  }
}

function cloneSearchResult(result, cacheHit) {
  return {
    torrents: Array.isArray(result?.torrents) ? result.torrents.map((torrent) => ({ ...torrent })) : [],
    counts: { ...(result?.counts || emptySearchResult().counts) },
    queries: Array.isArray(result?.queries) ? result.queries.map((query) => ({ ...query })) : [],
    jackettQueries: Array.isArray(result?.jackettQueries)
      ? result.jackettQueries.map((query) => ({ ...query }))
      : Array.isArray(result?.queries)
        ? result.queries.map((query) => ({ ...query }))
        : [],
    errors: Array.isArray(result?.errors) ? result.errors.slice() : [],
    cacheHit,
  }
}

async function searchAllDetailed(config, meta) {
  try {
    const cached = getCachedSearch(meta)
    if (cached) {
      return cached
    }

    const searches = []
    if (config.jackettUrl && config.jackettApiKey) {
      searches.push(jackett.searchTorrentsDetailed(config.jackettUrl, config.jackettApiKey, meta))
    }

    const parts = await Promise.all(searches.map((p) => p.catch((err) => {
      console.error('[scrapers] search failed:', err?.message || err)
      return {
        torrents: [],
        diagnostics: {
          rawCount: 0,
          usableCount: 0,
          queries: [],
          error: err?.message || String(err),
        },
      }
    })))
    let torrents = parts.flatMap((part) => part.torrents || [])
    const queries = parts.flatMap((part) => part.diagnostics?.queries || [])
    const errors = parts.flatMap((part) => {
      const diagnostics = part.diagnostics || {}
      return [
        ...(Array.isArray(diagnostics.errors) ? diagnostics.errors : []),
        diagnostics.error,
      ].filter(Boolean)
    })
    const counts = {
      raw: parts.reduce((sum, part) => sum + Number(part.diagnostics?.rawCount || 0), 0),
      search: torrents.length,
      afterSeasonPackMark: torrents.length,
      afterSeasonPackEnrich: torrents.length,
      beforeDedupe: torrents.length,
      afterDedupe: torrents.length,
      deduped: 0,
    }

    if (torrents.length === 0) {
      console.warn('[scrapers] search returned 0 results — check Jackett/Prowlarr indexers')
    }

    if (meta.type === 'series' && meta.season != null) {
      torrents = torrents.map((torrent) => ({
        ...torrent,
        seasonPack: torrent.seasonPack || seasonPacks.isSeasonPackForMeta(torrent.title, meta),
      }))
      counts.afterSeasonPackMark = torrents.length
    }

    const qualityConfig = quality.resolveQualityConfig(config)
    const episodeMatchCount = rank.countEpisodeMatches(torrents, meta, qualityConfig)

    if (config.preferSeasonPacks) {
      torrents = await seasonPacks.enrichWithSeasonPacks(
        config.jackettUrl,
        config.jackettApiKey,
        meta,
        torrents,
        episodeMatchCount,
      )
    }
    counts.afterSeasonPackEnrich = torrents.length
    counts.beforeDedupe = torrents.length

    if (config.dedupeStreams) {
      const before = torrents.length
      torrents = dedupe.collapseDuplicates(torrents)
      if (before !== torrents.length) {
        console.warn(`[scrapers] Deduped ${before} torrents to ${torrents.length}`)
      }
      counts.afterDedupe = torrents.length
      counts.deduped = before - torrents.length
    } else {
      counts.afterDedupe = torrents.length
    }

    const result = {
      torrents,
      counts,
      queries,
      jackettQueries: queries,
      errors,
      cacheHit: false,
    }
    setCachedSearch(meta, result)
    return result
  } catch (err) {
    console.error('[scrapers] Jackett/Prowlarr search failed:', err?.message || err)
    return emptySearchResult(err?.message || String(err))
  }
}

module.exports = {
  searchAll,
  searchAllDetailed,
}
