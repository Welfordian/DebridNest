function parseQuality(title) {
  const t = String(title || '').toLowerCase()
  if (/2160p|4k/u.test(t)) return { resolution: 2160, label: '4K' }
  if (/1080p/u.test(t)) return { resolution: 1080, label: '1080p' }
  if (/720p/u.test(t)) return { resolution: 720, label: '720p' }
  if (/480p/u.test(t)) return { resolution: 480, label: '480p' }
  return { resolution: 0, label: 'SD' }
}

function parseHdrProfile(title) {
  const t = String(title || '').toLowerCase()
  if (/dolby\s*vision|\bdv\b|dovi/u.test(t)) return 'dv'
  if (/hdr10\+|hdr10plus/u.test(t)) return 'hdr10plus'
  if (/\bhdr\b|hdr10/u.test(t)) return 'hdr'
  return 'sdr'
}

function parseMaxResolution(value) {
  if (value === undefined || value === null || value === '') {
    return 0
  }
  const normalized = String(value).toLowerCase().replace(/\s+/g, '')
  if (normalized === '0' || normalized === 'any' || normalized === 'none') {
    return 0
  }
  if (normalized === '4k' || normalized === '2160' || normalized === '2160p') {
    return 2160
  }
  if (normalized === '1080' || normalized === '1080p') {
    return 1080
  }
  if (normalized === '720' || normalized === '720p') {
    return 720
  }
  const num = Number(normalized)
  return Number.isFinite(num) && num > 0 ? num : 0
}

function parseMaxFileSizeGb(value) {
  if (value === undefined || value === null || value === '') {
    return 0
  }
  const num = Number(String(value).trim())
  return Number.isFinite(num) && num > 0 ? num : 0
}

function parsePreferSdr(value) {
  if (value === undefined || value === null || value === '') {
    return false
  }
  if (typeof value === 'boolean') {
    return value
  }
  const normalized = String(value).trim().toLowerCase()
  return normalized === '1' || normalized === 'true' || normalized === 'yes' || normalized === 'on'
}

function resolveQualityConfig(userConfig = {}, defaults = {}) {
  return {
    preferSdr: parsePreferSdr(
      userConfig.preferSdr !== undefined ? userConfig.preferSdr : defaults.preferSdr,
    ),
    maxResolution: parseMaxResolution(
      userConfig.maxResolution !== undefined ? userConfig.maxResolution : defaults.maxResolution,
    ),
    maxFileSizeGb: parseMaxFileSizeGb(
      userConfig.maxFileSizeGb !== undefined ? userConfig.maxFileSizeGb : defaults.maxFileSizeGb,
    ),
  }
}

function passesQualityFilters(torrent, qualityConfig) {
  const { maxResolution, maxFileSizeGb } = qualityConfig
  const quality = parseQuality(torrent.title)

  if (maxResolution > 0) {
    const effective = quality.resolution || 480
    if (effective > maxResolution) {
      return false
    }
  }

  if (maxFileSizeGb > 0 && torrent.size > 0) {
    const limitBytes = maxFileSizeGb * 1024 ** 3
    if (torrent.size > limitBytes) {
      return false
    }
  }

  return true
}

function hdrScoreAdjustment(title, preferSdr) {
  if (!preferSdr) {
    return 0
  }
  switch (parseHdrProfile(title)) {
    case 'dv': return -200
    case 'hdr10plus': return -150
    case 'hdr': return -100
    default: return 40
  }
}

function formatHdrLabel(title) {
  const profile = parseHdrProfile(title)
  if (profile === 'sdr') {
    return ''
  }
  if (profile === 'dv') {
    return ' DV'
  }
  if (profile === 'hdr10plus') {
    return ' HDR10+'
  }
  return ' HDR'
}

function formatHdrTag(title) {
  const profile = parseHdrProfile(title)
  if (profile === 'sdr') {
    return ''
  }
  if (profile === 'dv') {
    return 'DV'
  }
  if (profile === 'hdr10plus') {
    return 'HDR10+'
  }
  return 'HDR'
}

function formatQualityTags(title, source) {
  const q = parseQuality(title)
  const hdr = formatHdrTag(title)
  const parts = [q.label]
  if (hdr) {
    parts.push(hdr)
  }
  if (source && source !== 'Unknown') {
    parts.push(source)
  }
  return parts.join(' ')
}

module.exports = {
  parseQuality,
  parseHdrProfile,
  parseMaxResolution,
  parseMaxFileSizeGb,
  parsePreferSdr,
  resolveQualityConfig,
  passesQualityFilters,
  hdrScoreAdjustment,
  formatHdrLabel,
  formatHdrTag,
  formatQualityTags,
}
