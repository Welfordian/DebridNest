const test = require('node:test')
const assert = require('node:assert/strict')

const rank = require('../lib/rank')

test('detailed ranking reports quality, title, and non-video rejections', () => {
  const result = rank.rankTorrentsDetailed(
    [
      {
        title: 'Backrooms.2026.1080p.WEB-DL.mkv',
        seeders: 50,
        size: 2 * 1024 ** 3,
      },
      {
        title: 'Backrooms.2026.2160p.WEB-DL.mkv',
        seeders: 100,
        size: 4 * 1024 ** 3,
      },
      {
        title: 'Different.Movie.2026.1080p.WEB-DL.mkv',
        seeders: 100,
        size: 2 * 1024 ** 3,
      },
      {
        title: 'Backrooms soundtrack',
        seeders: 100,
        size: 100 * 1024 ** 2,
      },
    ],
    {
      type: 'movie',
      title: 'Backrooms',
      year: 2026,
    },
    10,
    {
      maxResolution: 1080,
      maxFileSizeGb: 0,
      preferSdr: false,
    },
  )

  assert.equal(result.entries.length, 1)
  assert.equal(result.entries[0].torrent.title, 'Backrooms.2026.1080p.WEB-DL.mkv')
  assert.deepEqual(result.rejected, {
    quality: 1,
    notVideo: 1,
    episodeMismatch: 0,
    movieTitleMismatch: 1,
  })
  assert.equal(result.counts.input, 4)
  assert.equal(result.counts.scored, 1)
  assert.equal(result.counts.returned, 1)
})
