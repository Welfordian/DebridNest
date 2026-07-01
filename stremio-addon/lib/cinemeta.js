const CINEMETA_BASE = 'https://v3-cinemeta.strem.io'

function parseStremioId(id) {
  if (!id || !id.startsWith('tt')) {
    return null
  }
  const parts = id.split(':')
  const imdbId = parts[0]
  const season = parts.length > 1 ? Number.parseInt(parts[1], 10) : null
  const episode = parts.length > 2 ? Number.parseInt(parts[2], 10) : null
  return {
    imdbId,
    season: Number.isFinite(season) ? season : null,
    episode: Number.isFinite(episode) ? episode : null,
  }
}

async function fetchMeta(type, imdbId) {
  const path = type === 'series'
    ? `/meta/series/${imdbId}.json`
    : `/meta/movie/${imdbId}.json`
  const res = await fetch(`${CINEMETA_BASE}${path}`)
  if (!res.ok) {
    throw new Error(`Cinemeta ${res.status}`)
  }
  const data = await res.json()
  return data.meta
}

async function resolveMetadata(type, id) {
  const parsed = parseStremioId(id)
  if (!parsed) {
    return null
  }

  const meta = await fetchMeta(type, parsed.imdbId)
  if (!meta) {
    return null
  }

  return {
    imdbId: parsed.imdbId,
    type,
    title: meta.name,
    year: meta.year,
    season: parsed.season,
    episode: parsed.episode,
  }
}

module.exports = {
  parseStremioId,
  resolveMetadata,
}
