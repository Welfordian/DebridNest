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

const TITLE_STOPWORDS = new Set([
  'a',
  'an',
  'and',
  'de',
  'la',
  'le',
  'les',
  'of',
  'the',
  'to',
])

function normalizeTitleWords(value) {
  return String(value || '')
    .toLowerCase()
    .replace(/&/g, ' and ')
    .replace(/\[.*?\]/g, ' ')
    .replace(/[^a-z0-9]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
}

function meaningfulTitleTokens(title) {
  return normalizeTitleWords(title)
    .split(' ')
    .filter(Boolean)
    .filter((token) => !TITLE_STOPWORDS.has(token))
    .filter((token) => !/^\d{4}$/u.test(token))
}

function matchesMovieTitle(title, meta) {
  if (meta.type !== 'movie' || !meta.title) {
    return true
  }

  const expected = normalizeTitleWords(meta.title)
  const actual = normalizeTitleWords(title)
  if (!expected || !actual) {
    return true
  }
  if (actual.includes(expected)) {
    return true
  }

  const tokens = meaningfulTitleTokens(meta.title)
  if (tokens.length === 0) {
    return true
  }

  // Very short one-word titles are too ambiguous to safely hard-filter.
  if (tokens.length === 1 && tokens[0].length <= 2) {
    return true
  }

  const actualWords = new Set(actual.split(' ').filter(Boolean))
  const matched = tokens.filter((token) => actualWords.has(token)).length
  if (matched === tokens.length) {
    return true
  }
  return tokens.length >= 3 && matched / tokens.length >= 0.7
}

function torrentRejectionReason(torrent, meta, qualityConfig = {}) {
  const isSeasonPack = torrent.seasonPack
    || seasonPacks.isSeasonPackForMeta(torrent.title, meta)

  if (!quality.passesQualityFilters(torrent, qualityConfig)) {
    return 'quality'
  }

  if (!isSeasonPack && !looksLikeVideoRelease(torrent.title)) {
    return 'notVideo'
  }

  if (!matchesMovieTitle(torrent.title, meta)) {
    return 'movieTitleMismatch'
  }

  const rankMeta = isSeasonPack
    ? { ...meta, allowSeasonPack: true }
    : meta

  if (!matchesEpisode(torrent.title, rankMeta)) {
    return 'episodeMismatch'
  }

  return null
}

function computeTorrentScore(torrent, meta, qualityConfig = {}) {
  const isSeasonPack = torrent.seasonPack
    || seasonPacks.isSeasonPackForMeta(torrent.title, meta)
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

function scoreTorrent(torrent, meta, qualityConfig = {}) {
  if (torrentRejectionReason(torrent, meta, qualityConfig)) {
    return -1
  }
  return computeTorrentScore(torrent, meta, qualityConfig)
}

function rankTorrents(torrents, meta, maxResults = 5, qualityConfig = {}) {
  return rankTorrentsDetailed(torrents, meta, maxResults, qualityConfig).entries
}

function rankTorrentsDetailed(torrents, meta, maxResults = 5, qualityConfig = {}) {
  const input = Array.isArray(torrents) ? torrents : []
  const rejected = {
    quality: 0,
    notVideo: 0,
    episodeMismatch: 0,
    movieTitleMismatch: 0,
  }
  const entries = []

  for (const torrent of input) {
    const reason = torrentRejectionReason(torrent, meta, qualityConfig)
    if (reason) {
      rejected[reason] = (rejected[reason] || 0) + 1
      continue
    }
    entries.push({
      torrent,
      score: computeTorrentScore(torrent, meta, qualityConfig),
      quality: quality.parseQuality(torrent.title),
      source: parseSource(torrent.title),
    })
  }

  entries.sort((a, b) => b.score - a.score)
  const limited = entries.slice(0, maxResults)

  return {
    entries: limited,
    counts: {
      input: input.length,
      scored: entries.length,
      ranked: entries.length,
      returned: limited.length,
    },
    rejected,
  }
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
  const parts = [`Seeders: ${Number(torrent.seeders || 0)}`]
  const sizeLabel = formatFileSize(torrent.size)
  if (sizeLabel) {
    parts.push(`Size: ${sizeLabel}`)
  }
  const provider = torrent.indexer || null
  if (provider) {
    parts.push(`Provider: ${formatProviderName(provider)}`)
  }
  return parts.join(' | ')
}

function formatStreamName(entry, cached = false) {
  const tags = quality.formatQualityTags(entry.torrent.title, entry.source)
  const prefix = cached ? 'Ready' : 'Starts download'
  return [prefix, tags].filter(Boolean).join('\n')
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

function hasTorrentLink(entry) {
  const link = entry?.torrent?.torrentLink || entry?.torrent?.link
  if (!link || typeof link !== 'string') {
    return false
  }
  return !link.trim().toLowerCase().startsWith('magnet:')
}

function compareKnownLowerSize(a, b) {
  const aSize = Number(a.torrent.size || 0)
  const bSize = Number(b.torrent.size || 0)
  const aKnown = aSize > 0
  const bKnown = bSize > 0
  if (aKnown !== bKnown) {
    return aKnown ? -1 : 1
  }
  if (aKnown && aSize !== bSize) {
    return aSize - bSize
  }
  return 0
}

function compareFreshPlaceholderPriority(a, b) {
  const aHasTorrentLink = hasTorrentLink(a)
  const bHasTorrentLink = hasTorrentLink(b)
  if (aHasTorrentLink !== bHasTorrentLink) {
    return aHasTorrentLink ? -1 : 1
  }

  const seedersDelta = Number(b.torrent.seeders || 0) - Number(a.torrent.seeders || 0)
  if (seedersDelta !== 0) {
    return seedersDelta
  }

  const sizeDelta = compareKnownLowerSize(a, b)
  if (sizeDelta !== 0) {
    return sizeDelta
  }

  return b.score - a.score
}

function applyStreamListingPriority(ranked, availability) {
  return ranked.slice().sort((a, b) => {
    const aCached = isEntryCached(a, availability)
    const bCached = isEntryCached(b, availability)
    if (aCached !== bCached) {
      return aCached ? -1 : 1
    }
    if (!aCached && !bCached) {
      return compareFreshPlaceholderPriority(a, b)
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
  const prefix = cached ? 'Ready' : 'Starts download'
  const provider = formatProviderName(entry.torrent.indexer)
  const parts = [prefix, qualityLabel]
  if (provider) {
    parts.push(provider)
  }
  return ['DebridNest', ...parts].join('\n')
}

function formatStremioStreamDescription(entry, cached = false) {
  const display = formatStreamDisplay(entry, { cached })
  const status = cached ? 'Status: ready' : 'Status: starts download'
  return [display.title, status, display.description].filter(Boolean).join('\n')
}

function formatProviderName(provider) {
  const text = String(provider || '')
    .replace(/^\[[^\]]+\]\s*/, '')
    .trim()
  if (!text) {
    return ''
  }
  return text.length > 28 ? `${text.slice(0, 25)}...` : text
}

module.exports = {
  rankTorrents,
  rankTorrentsDetailed,
  countEpisodeMatches,
  formatStreamDisplay,
  formatStreamLabel,
  formatPlaceholderLabel,
  applyCachePriority,
  applyStreamListingPriority,
  isEntryCached,
  formatStreamFilename,
  formatBingeGroup,
  formatStremioStreamName,
  formatStremioStreamDescription,
  matchesMovieTitle,
  looksLikeVideoRelease,
}
