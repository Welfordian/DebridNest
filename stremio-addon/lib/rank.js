const quality = require('./quality')

const VIDEO_EXT = /\.(mp4|mkv|avi|webm|mov|m4v|wmv|flv|ts|m2ts)$/i

function parseSource(title) {
  const t = title.toLowerCase()
  if (/remux/u.test(t)) return 'Remux'
  if (/blu-?ray|bdrip|brrip/u.test(t)) return 'BluRay'
  if (/web-?dl/u.test(t)) return 'WEB-DL'
  if (/webrip/u.test(t)) return 'WEBRip'
  if (/hdtv/u.test(t)) return 'HDTV'
  return 'Unknown'
}

function matchesEpisode(title, meta) {
  if (meta.type !== 'series' || meta.season == null || meta.episode == null) {
    return true
  }
  const t = title.toLowerCase()
  const s = pad2(meta.season)
  const e = pad2(meta.episode)
  const patterns = [
    new RegExp(`s${s}e${e}\\b`, 'i'),
    new RegExp(`${meta.season}x${meta.episode}\\b`, 'i'),
    new RegExp(`season ${meta.season}.*episode ${meta.episode}`, 'i'),
  ]
  return patterns.some((p) => p.test(t))
}

function pad2(n) {
  return String(n).padStart(2, '0')
}

function looksLikeVideoRelease(title) {
  const t = String(title || '').toLowerCase()
  if (VIDEO_EXT.test(t) || /\.(mp4|mkv|avi|webm|mov|m4v)/i.test(t)) {
    return true
  }
  return /2160p|4k|1080p|720p|480p|bluray|web-?dl|webrip|hdtv|remux|x264|x265|hevc|dvdrip/u.test(t)
}

function scoreTorrent(torrent, meta, qualityConfig = {}) {
  if (!looksLikeVideoRelease(torrent.title)) {
    return -1
  }
  if (!matchesEpisode(torrent.title, meta)) {
    return -1
  }

  const parsedQuality = quality.parseQuality(torrent.title)
  const source = parseSource(torrent.title)
  let score = 0

  score += Math.min(torrent.seeders || 0, 500)
  score += parsedQuality.resolution * 2
  score += quality.hdrScoreAdjustment(torrent.title, qualityConfig.preferSdr)

  switch (source) {
    case 'Remux': score += 120; break
    case 'BluRay': score += 100; break
    case 'WEB-DL': score += 80; break
    case 'WEBRip': score += 60; break
    case 'HDTV': score += 40; break
    default: score += 20
  }

  if (meta.type === 'series' && /complete|season pack|s\d{2}\b(?!e)/i.test(torrent.title)) {
    if (meta.episode == null) {
      score += 50
    } else {
      score -= 30
    }
  }

  return score
}

function rankTorrents(torrents, meta, maxResults = 5, qualityConfig = {}) {
  return torrents
    .filter((torrent) => quality.passesQualityFilters(torrent, qualityConfig))
    .map((torrent) => ({
      torrent,
      score: scoreTorrent(torrent, meta, qualityConfig),
      quality: quality.parseQuality(torrent.title),
      source: parseSource(torrent.title),
    }))
    .filter((entry) => entry.score >= 0)
    .sort((a, b) => b.score - a.score)
    .slice(0, maxResults)
}

function formatStreamLabel(entry, cached = false) {
  const { torrent, quality: parsedQuality, source } = entry
  const seeders = torrent.seeders ? ` (${torrent.seeders} seeders)` : ''
  const hdr = quality.formatHdrLabel(torrent.title)
  const prefix = cached ? 'DebridNest ⚡' : 'DebridNest'
  return `${prefix} ${parsedQuality.label}${hdr} ${source}${cached ? ' (cached)' : ''}${seeders}`
}

function formatPlaceholderLabel(entry) {
  const { quality, source } = entry
  return `DebridNest ⏳ ${quality.label} ${source} (stream)`
}

function applyCachePriority(ranked, availability) {
  return ranked.slice().sort((a, b) => {
    const aCached = isEntryCached(a, availability)
    const bCached = isEntryCached(b, availability)
    if (aCached !== bCached) {
      return aCached ? -1 : 1
    }
    return b.score - a.score
  })
}

function isEntryCached(entry, availability) {
  if (!entry.torrent.infoHash || !availability) {
    return false
  }
  const hash = entry.torrent.infoHash.toLowerCase()
  const item = availability[hash]
  if (!item) {
    return false
  }
  return Object.values(item).some((v) => Array.isArray(v) && v.length > 0)
}

module.exports = {
  rankTorrents,
  formatStreamLabel,
  formatPlaceholderLabel,
  applyCachePriority,
  isEntryCached,
}
