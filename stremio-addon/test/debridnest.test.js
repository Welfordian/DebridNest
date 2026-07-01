const test = require('node:test')
const assert = require('node:assert/strict')

const debridnest = require('../lib/debridnest')
const progress = require('../lib/progress')

const baseFiles = [
  { id: 1, path: '/Show/Season 1/Show.S01E01.1080p.mkv', bytes: 1_000 },
  { id: 2, path: '/Show/Season 1/Show.S01E02.1080p.mkv', bytes: 2_000 },
  { id: 3, path: '/Show/Season 1/Show.S01E08.1080p.mkv', bytes: 9_000 },
]

test('episode target selects matching SxxEyy file instead of largest season-pack file', () => {
  assert.equal(
    debridnest.pickVideoFileIds(baseFiles, { season: 1, episode: 2 }),
    '2',
  )
})

test('episode target supports 1x02 and season-folder episode-number paths', () => {
  assert.equal(
    debridnest.pickVideoFileIds([
      { id: 1, path: '/Show/Show.1x01.mkv', bytes: 5_000 },
      { id: 2, path: '/Show/Show.1x02.mkv', bytes: 1_000 },
    ], { season: 1, episode: 2 }),
    '2',
  )
  assert.equal(
    debridnest.pickVideoFileIds([
      { id: 1, path: '/Show/Season 1/01 - Pilot.mkv', bytes: 5_000 },
      { id: 2, path: '/Show/Season 1/02 - Next.mkv', bytes: 1_000 },
    ], { season: 1, episode: 2 }),
    '2',
  )
})

test('movie/no-target selection keeps largest video fallback', () => {
  assert.equal(debridnest.pickVideoFileIds(baseFiles), '3')
})

test('multi-video target with no match does not select arbitrary largest file', () => {
  assert.equal(
    debridnest.pickVideoFileIds(baseFiles, { season: 1, episode: 9 }),
    '',
  )
})

test('host link aligns with selected target episode file', () => {
  const info = {
    files: baseFiles.map((file) => ({ ...file, selected: 1 })),
    links: ['link-1', 'link-2', 'link-3'],
  }
  assert.equal(debridnest.pickHostLink(info, { season: 1, episode: 2 }), 'link-2')
  assert.equal(debridnest.pickHostLink(info, { season: 1, episode: 9 }), null)
})

test('magnet tracker appending preserves magnets that already have trackers', () => {
  const bare = 'magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
  const withTrackers = debridnest.appendDefaultTrackers(bare)
  assert.match(withTrackers, /[?&]tr=/)
  assert.equal(debridnest.appendDefaultTrackers(`${bare}&tr=udp%3A%2F%2Fexisting`), `${bare}&tr=udp%3A%2F%2Fexisting`)
})

test('progress jobs preserve episode targeting', () => {
  const token = progress.createJob({
    magnet: 'magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    apiUrl: 'http://debridnest',
    apiToken: 'token',
    label: 'Show S01E02',
    season: 1,
    episode: 2,
  })
  const job = progress.getJob(token)
  assert.equal(job.season, 1)
  assert.equal(job.episode, 2)
})
