function normalizeTitle(title) {
  return String(title || '')
    .toLowerCase()
    .replace(/\[.*?\]/g, ' ')
    .replace(/[^a-z0-9]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
}

function pickBetter(a, b) {
  const aSeeders = Number(a.seeders || 0)
  const bSeeders = Number(b.seeders || 0)
  if (aSeeders !== bSeeders) {
    return aSeeders > bSeeders ? a : b
  }
  const aSize = Number(a.size || 0)
  const bSize = Number(b.size || 0)
  return aSize >= bSize ? a : b
}

function collapseDuplicates(torrents) {
  if (!Array.isArray(torrents) || torrents.length === 0) {
    return []
  }

  const byHash = new Map()
  const noHash = []

  for (const torrent of torrents) {
    const hash = torrent.infoHash ? torrent.infoHash.toLowerCase() : null
    if (hash) {
      const existing = byHash.get(hash)
      byHash.set(hash, existing ? pickBetter(existing, torrent) : torrent)
      continue
    }
    noHash.push(torrent)
  }

  const byTitle = new Map()
  for (const torrent of [...byHash.values(), ...noHash]) {
    const key = normalizeTitle(torrent.title)
    if (!key) {
      byTitle.set(`__empty:${byTitle.size}`, torrent)
      continue
    }
    const existing = byTitle.get(key)
    byTitle.set(key, existing ? pickBetter(existing, torrent) : torrent)
  }

  return [...byTitle.values()]
}

module.exports = {
  normalizeTitle,
  collapseDuplicates,
}
