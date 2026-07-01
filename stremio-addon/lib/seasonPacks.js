const jackett = require('./jackett')

const MIN_EPISODE_RESULTS = 3

function pad2(n) {
  return String(n).padStart(2, '0')
}

function isSeasonPackTitle(title, season) {
  const t = String(title || '').toLowerCase()
  const s = pad2(season)
  if (new RegExp(`\\bs${s}e\\d{2}\\b`, 'i').test(t)) {
    return false
  }
  const packPatterns = [
    new RegExp(`\\bs${s}\\b(?!e)`, 'i'),
    new RegExp(`season\\s*${season}\\b`, 'i'),
    /\bcomplete\b/i,
    /\bseason pack\b/i,
  ]
  return packPatterns.some((p) => p.test(t))
}

function needsSeasonPackFallback(torrents, meta, minResults = MIN_EPISODE_RESULTS) {
  if (meta.type !== 'series' || meta.season == null || meta.episode == null) {
    return false
  }
  return !Array.isArray(torrents) || torrents.length < minResults
}

async function searchSeasonPacks(jackettUrl, jackettApiKey, meta) {
  const seasonMeta = {
    ...meta,
    episode: null,
  }
  const torrents = await jackett.searchTorrents(jackettUrl, jackettApiKey, seasonMeta)
  return torrents.filter((t) => isSeasonPackTitle(t.title, meta.season))
}

function mergeEpisodeAndSeasonResults(episodeTorrents, seasonTorrents) {
  const seen = new Set()
  const merged = []

  for (const torrent of [...episodeTorrents, ...seasonTorrents]) {
    const key = torrent.infoHash
      ? torrent.infoHash.toLowerCase()
      : String(torrent.title || '').toLowerCase()
    if (seen.has(key)) {
      continue
    }
    seen.add(key)
    merged.push(torrent)
  }

  return merged
}

async function enrichWithSeasonPacks(jackettUrl, jackettApiKey, meta, episodeTorrents) {
  if (!needsSeasonPackFallback(episodeTorrents, meta)) {
    return episodeTorrents
  }

  try {
    const seasonTorrents = await searchSeasonPacks(jackettUrl, jackettApiKey, meta)
    if (seasonTorrents.length === 0) {
      return episodeTorrents
    }
    console.warn(
      `[seasonPacks] Episode search returned ${episodeTorrents.length} result(s); adding ${seasonTorrents.length} season pack(s) for S${pad2(meta.season)}`,
    )
    return mergeEpisodeAndSeasonResults(episodeTorrents, seasonTorrents)
  } catch (err) {
    console.error('[seasonPacks] Season pack search failed:', err?.message || err)
    return episodeTorrents
  }
}

module.exports = {
  MIN_EPISODE_RESULTS,
  isSeasonPackTitle,
  needsSeasonPackFallback,
  searchSeasonPacks,
  mergeEpisodeAndSeasonResults,
  enrichWithSeasonPacks,
}
