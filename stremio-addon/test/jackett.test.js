const test = require('node:test')
const assert = require('node:assert/strict')

const jackett = require('../lib/jackett')

function itemXml({ title, hash, seeders = 10, indexer = '1337x', size = 1024 }) {
  return `<item>
    <title>${title}</title>
    <link>http://jackett/dl/${hash}</link>
    <jackettindexer id="${indexer.toLowerCase()}">${indexer}</jackettindexer>
    <size>${size}</size>
    <torznab:attr name="seeders" value="${seeders}" />
    <torznab:attr name="jackettindexer" value="${indexer}" />
    <torznab:attr name="magneturl" value="magnet:?xt=urn:btih:${hash}&amp;dn=${encodeURIComponent(title)}" />
  </item>`
}

function rss(items) {
  return `<?xml version="1.0" encoding="UTF-8"?>
  <rss xmlns:torznab="http://torznab.com/schemas/2015/feed">
    <channel>${items.map(itemXml).join('')}</channel>
  </rss>`
}

test('movie search expands sparse IMDb results and dedupes variants', async (t) => {
  const originalFetch = global.fetch
  t.after(() => {
    global.fetch = originalFetch
  })

  const calls = []
  global.fetch = async (url) => {
    const parsed = new URL(url)
    const q = parsed.searchParams.get('q')
    const imdbid = parsed.searchParams.get('imdbid')
    calls.push({ q, imdbid })

    let items = []
    if (imdbid && q === 'Backrooms 2026') {
      items = [
        {
          title: 'Backrooms 2026 1080p WEB-DL',
          hash: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
          seeders: 10,
        },
        {
          title: 'Backrooms 2026 720p WEBRip',
          hash: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
          seeders: 8,
        },
      ]
    } else if (imdbid && !q) {
      items = [
        {
          title: 'Backrooms 2026 1080p WEB-DL',
          hash: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
          seeders: 99,
        },
      ]
    } else if (!imdbid && q === 'Backrooms 2026') {
      items = [
        {
          title: 'Backrooms 2026 480p WEBRip',
          hash: 'cccccccccccccccccccccccccccccccccccccccc',
          seeders: 5,
        },
      ]
    }

    return {
      ok: true,
      status: 200,
      text: async () => rss(items),
    }
  }

  const result = await jackett.searchTorrentsDetailed(
    'http://jackett:9117',
    'test-key',
    {
      type: 'movie',
      imdbId: 'tt1234567',
      title: 'Backrooms',
      year: 2026,
    },
    { minMovieResults: 3 },
  )

  assert.equal(result.torrents.length, 3)
  assert.deepEqual(
    result.diagnostics.queries.map((query) => query.label),
    ['imdb-title-year', 'imdb-only', 'title-year', 'title-only'],
  )
  assert.equal(result.diagnostics.rawCount, 4)
  assert.equal(result.torrents[0].seeders, 99)
  assert.equal(result.torrents[0].indexer, '1337x')
  assert.deepEqual(calls.map((call) => call.q), ['Backrooms 2026', null, 'Backrooms 2026', 'Backrooms'])
})
