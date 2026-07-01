const jackett = require('./jackett')

async function searchAll(config, meta) {
  try {
    const torrents = await jackett.searchTorrents(config.jackettUrl, config.jackettApiKey, meta)
    if (torrents.length === 0) {
      console.warn('[scrapers] Jackett returned 0 torrents — check indexers at http://localhost:9117')
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
