const jackett = require('./jackett')
const dedupe = require('./dedupe')
const seasonPacks = require('./seasonPacks')

async function searchAll(config, meta) {
  try {
    let torrents = await jackett.searchTorrents(config.jackettUrl, config.jackettApiKey, meta)
    if (torrents.length === 0) {
      console.warn('[scrapers] Jackett returned 0 torrents — check indexers at http://localhost:9117')
    }

    if (config.preferSeasonPacks) {
      torrents = await seasonPacks.enrichWithSeasonPacks(
        config.jackettUrl,
        config.jackettApiKey,
        meta,
        torrents,
      )
    }

    if (config.dedupeStreams) {
      const before = torrents.length
      torrents = dedupe.collapseDuplicates(torrents)
      if (before !== torrents.length) {
        console.warn(`[scrapers] Deduped ${before} torrents to ${torrents.length}`)
      }
    }

    return torrents
  } catch (err) {
    console.error('[scrapers] Jackett/Prowlarr search failed:', err?.message || err)
    return []
  }
}

module.exports = {
  searchAll,
}
