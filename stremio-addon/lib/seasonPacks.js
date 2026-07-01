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
    new RegExp(`\\bs${season}\\b(?!e)`, 'i'),
    new RegExp(`season\\s*${season}\\b`, 'i'),
    /\bcomplete\b/i,
    /\bseason pack\b/i,
  ]
  return packPatterns.some((p) => p.test(t))
}

function specifiesDifferentEpisode(title, meta) {
  if (meta.type !== 'series' || meta.season == null || meta.episode == null) {
    return false
  }
  const t = String(title || '').toLowerCase()
  const s = meta.season
  const e = meta.episode
  const sPad = pad2(s)
  const epPatterns = [
    new RegExp(`\\bs${sPad}e(\\d{2})\\b`, 'i'),
    new RegExp(`\\bs${s}e(\\d+)\\b`, 'i'),
    new RegExp(`\\b${s}x(\\d+)\\b`, 'i'),
    new RegExp(`\\b${sPad}x(\\d{2})\\b`, 'i'),
  ]
  for (const pattern of epPatterns) {
    const match = t.match(pattern)
    if (match && Number(match[1]) !== e) {
      return true
    }
  }
  const episodeWord = t.match(/episode\s*(\d+)/i)
  if (episodeWord && Number(episodeWord[1]) !== e) {
    return true
  }
  return false
}

function isSeasonPackForMeta(title, meta) {
  if (meta.type !== 'series' || meta.season == null) {
    return false
  }
  if (specifiesDifferentEpisode(title, meta)) {
    return false
  }
  return isSeasonPackTitle(title, meta.season)
}

function needsSeasonPackFallback(matchCount, meta, minResults = MIN_EPISODE_RESULTS) {
  if (meta.type !== 'series' || meta.season == null || meta.episode == null) {
    return false
  }
  return Number(matchCount || 0) < minResults
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

async function enrichWithSeasonPacks(jackettUrl, jackettApiKey, meta, episodeTorrents, matchCount) {
  if (!needsSeasonPackFallback(matchCount, meta)) {
    return episodeTorrents
  }

  try {
    const seasonTorrents = await searchSeasonPacks(jackettUrl, jackettApiKey, meta)
    if (seasonTorrents.length === 0) {
      return episodeTorrents
    }
    console.warn(
      `[seasonPacks] Only ${matchCount} episode match(es) in ${episodeTorrents.length} Jackett result(s); adding ${seasonTorrents.length} season pack(s) for S${pad2(meta.season)}`,
    )
    return mergeEpisodeAndSeasonResults(episodeTorrents, seasonTorrents.map((t) => ({
      ...t,
      seasonPack: true,
    })))
  } catch (err) {
    console.error('[seasonPacks] Season pack search failed:', err?.message || err)
    return episodeTorrents
  }
}

module.exports = {
  MIN_EPISODE_RESULTS,
  isSeasonPackTitle,
  specifiesDifferentEpisode,
  isSeasonPackForMeta,
  needsSeasonPackFallback,
  searchSeasonPacks,
  mergeEpisodeAndSeasonResults,
  enrichWithSeasonPacks,
}
