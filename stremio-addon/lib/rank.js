const quality = require('./quality')
const seasonPacks = require('./seasonPacks')

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
  if (meta.allowSeasonPack) {
    return true
  }
  const t = title.toLowerCase()
  const s = meta.season
  const e = meta.episode
  const sPad = pad2(s)
  const ePad = pad2(e)
  const patterns = [
    new RegExp(`\\bs${sPad}e${ePad}\\b`, 'i'),
    new RegExp(`\\bs${s}e${e}\\b`, 'i'),
    new RegExp(`\\b${s}x${e}\\b`, 'i'),
    new RegExp(`\\b${s}x${ePad}\\b`, 'i'),
    new RegExp(`\\b${sPad}x${ePad}\\b`, 'i'),
    new RegExp(`season\\s*${s}.*episode\\s*${e}`, 'i'),
    new RegExp(`\\b${ePad}\\b.*\\bs${sPad}\\b`, 'i'),
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
  const isSeasonPack = torrent.seasonPack
    || seasonPacks.isSeasonPackForMeta(torrent.title, meta)

  if (!isSeasonPack && !looksLikeVideoRelease(torrent.title)) {
    return -1
  }

  const rankMeta = isSeasonPack
    ? { ...meta, allowSeasonPack: true }
    : meta

  if (!matchesEpisode(torrent.title, rankMeta)) {
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

function countEpisodeMatches(torrents, meta, qualityConfig = {}) {
  return rankTorrents(torrents, meta, torrents.length, qualityConfig).length
}

const DISPLAY_TITLE_MAX = 80

function formatFileSize(bytes) {
  const size = Number(bytes || 0)
  if (size <= 0) {
    return null
  }
  const gb = size / (1024 ** 3)
  if (gb >= 100) {
    return `${Math.round(gb)} GB`
  }
  if (gb >= 10) {
    return `${gb.toFixed(1)} GB`
  }
  return `${gb.toFixed(2)} GB`
}

function formatDisplayTitle(title) {
  let text = String(title || 'Unknown').trim()
  text = text.replace(/^\[[^\]]+\]\s*/, '')
  if (text.length <= DISPLAY_TITLE_MAX) {
    return text
  }
  return `${text.slice(0, DISPLAY_TITLE_MAX - 3)}...`
}

function formatStreamMetadata(entry) {
  const { torrent } = entry
  const parts = [`👤 ${Number(torrent.seeders || 0)}`]
  const sizeLabel = formatFileSize(torrent.size)
  if (sizeLabel) {
    parts.push(`💾 ${sizeLabel}`)
  }
  const provider = torrent.indexer || null
  if (provider) {
    parts.push(`⚙️ ${provider}`)
  }
  return parts.join('  ')
}

function formatStreamName(entry, cached = false) {
  const tags = quality.formatQualityTags(entry.torrent.title, entry.source)
  const prefix = cached ? '⚡' : '⏳'
  return tags ? `${prefix} ${tags}` : prefix
}

function formatStreamDisplay(entry, options = {}) {
  const { cached = false } = options
  return {
    name: formatStreamName(entry, cached),
    title: formatDisplayTitle(entry.torrent.title),
    description: formatStreamMetadata(entry),
  }
}

function formatStreamLabel(entry, cached = false) {
  const display = formatStreamDisplay(entry, { cached })
  return display.name
}

function formatPlaceholderLabel(entry) {
  return formatStreamName(entry, false)
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

function formatStreamFilename(title) {
  const cleaned = String(title || '').replace(/^\[[^\]]+\]\s*/, '').trim()
  const match = cleaned.match(/[^\s/\\]+\.(mkv|mp4|avi|webm|mov|m4v|ts|m2ts)/i)
  if (match) {
    return match[0]
  }
  return cleaned.slice(0, 120) || 'video.mkv'
}

function formatBingeGroup(entry) {
  const tags = quality.formatQualityTags(entry.torrent.title, entry.source)
  const normalized = String(tags || entry.source || 'unknown')
    .toLowerCase()
    .replace(/\s+/g, '-')
  return `debridnest|${normalized}`
}

function formatStremioStreamName(entry, cached = false) {
  const tags = quality.formatQualityTags(entry.torrent.title, entry.source)
  const qualityLabel = tags || entry.source || 'Stream'
  const prefix = cached ? '⚡' : '⏳'
  return `DebridNest\n${prefix} ${qualityLabel}`
}

function formatStremioStreamDescription(entry, cached = false) {
  const display = formatStreamDisplay(entry, { cached })
  return `${display.title}\n${display.description}`
}

module.exports = {
  rankTorrents,
  countEpisodeMatches,
  formatStreamDisplay,
  formatStreamLabel,
  formatPlaceholderLabel,
  applyCachePriority,
  isEntryCached,
  formatStreamFilename,
  formatBingeGroup,
  formatStremioStreamName,
  formatStremioStreamDescription,
}
