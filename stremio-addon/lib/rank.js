const VIDEO_EXT = /\.(mp4|mkv|avi|webm|mov|m4v|wmv|flv|ts|m2ts)$/i

function parseQuality(title) {
  const t = title.toLowerCase()
  if (/2160p|4k/u.test(t)) return { resolution: 2160, label: '4K' }
  if (/1080p/u.test(t)) return { resolution: 1080, label: '1080p' }
  if (/720p/u.test(t)) return { resolution: 720, label: '720p' }
  if (/480p/u.test(t)) return { resolution: 480, label: '480p' }
  return { resolution: 0, label: 'SD' }
}

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

function scoreTorrent(torrent, meta) {
  if (!looksLikeVideoRelease(torrent.title)) {
    return -1
  }
  if (!matchesEpisode(torrent.title, meta)) {
    return -1
  }

  const quality = parseQuality(torrent.title)
  const source = parseSource(torrent.title)
  let score = 0

  score += Math.min(torrent.seeders || 0, 500)
  score += quality.resolution * 2

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

function rankTorrents(torrents, meta, maxResults = 5) {
  return torrents
    .map((torrent) => ({
      torrent,
      score: scoreTorrent(torrent, meta),
      quality: parseQuality(torrent.title),
      source: parseSource(torrent.title),
    }))
    .filter((entry) => entry.score >= 0)
    .sort((a, b) => b.score - a.score)
    .slice(0, maxResults)
}

function formatStreamLabel(entry, cached = false) {
  const { torrent, quality, source } = entry
  const seeders = torrent.seeders ? ` (${torrent.seeders} seeders)` : ''
  const prefix = cached ? 'DebridNest ⚡' : 'DebridNest'
  return `${prefix} ${quality.label} ${source}${cached ? ' (cached)' : ''}${seeders}`
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
