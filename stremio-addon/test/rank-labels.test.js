const test = require('node:test')
const assert = require('node:assert/strict')

const rank = require('../lib/rank')

test('Stremio labels include readiness, quality, and provider', () => {
  const entry = {
    torrent: {
      title: '[1337x] Backrooms.2026.1080p.WEB-DL.mkv',
      indexer: '1337x',
      seeders: 123,
      size: 2 * 1024 ** 3,
    },
    source: 'WEB-DL',
    score: 100,
  }

  const readyName = rank.formatStremioStreamName(entry, true)
  assert.equal(readyName, 'DebridNest\nReady\n1080p WEB-DL\n1337x')

  const freshName = rank.formatStremioStreamName(entry, false)
  assert.equal(freshName, 'DebridNest\nStarts download\n1080p WEB-DL\n1337x')

  const description = rank.formatStremioStreamDescription(entry, true)
  assert.match(description, /Status: ready/)
  assert.match(description, /Provider: 1337x/)
  assert.match(description, /Seeders: 123/)
  assert.match(description, /Size: 2.00 GB/)
  assert.equal(description.split('\n').length, 3)
})
