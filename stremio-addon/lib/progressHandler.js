const progress = require('./progress')
const debridnest = require('./debridnest')
const proxy = require('./proxy')

const PROGRESS_POLL_MS = Number(process.env.PROGRESS_POLL_MS || 2000)
const PROGRESS_MAX_WAIT_MS = Number(process.env.PROGRESS_MAX_WAIT_MS || 120000)

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function isFailedStatus(status) {
  return ['error', 'magnet_error', 'dead', 'virus'].includes(status)
}

async function ensureTorrentStarted(job) {
  if (job.torrentId) {
    return job.torrentId
  }
  if (!job.magnet) {
    throw new Error('missing magnet link')
  }
  if (!job.starting) {
    job.starting = debridnest.startDownload(job.apiUrl, job.apiToken, job.magnet)
      .then((torrentId) => {
        job.torrentId = torrentId
        delete job.starting
        return torrentId
      })
      .catch((err) => {
        delete job.starting
        throw err
      })
  }
  await job.starting
  return job.torrentId
}

async function refreshStreamUrl(job) {
  const resolved = await debridnest.resolveStreamUrl(job.apiUrl, job.apiToken, job.torrentId)
  if (!resolved) {
    throw new Error('Stream not ready')
  }
  job.hostLink = resolved.hostLink
  job.downloadUrl = resolved.download
  return resolved.download
}

async function waitUntilStreamable(job) {
  const deadline = Date.now() + PROGRESS_MAX_WAIT_MS
  while (Date.now() < deadline) {
    const resolved = await debridnest.resolveStreamUrl(job.apiUrl, job.apiToken, job.torrentId)
    if (resolved) {
      job.hostLink = resolved.hostLink
      job.downloadUrl = resolved.download
      return resolved
    }

    const info = await debridnest.getTorrentInfo(job.apiUrl, job.apiToken, job.torrentId)
    if (isFailedStatus(info.status)) {
      throw new Error(`Torrent failed: ${info.status}`)
    }

    await sleep(PROGRESS_POLL_MS)
  }
  throw new Error('Timed out waiting for stream to buffer')
}

async function handleProgressRequest(req, res, job) {
  await progress.withJobLock(job, () => ensureTorrentStarted(job))

  const hasRange = Boolean(req.headers.range)

  if (!job.downloadUrl) {
    if (hasRange) {
      const resolved = await debridnest.resolveStreamUrl(job.apiUrl, job.apiToken, job.torrentId)
      if (!resolved) {
        const info = await debridnest.getTorrentInfo(job.apiUrl, job.apiToken, job.torrentId)
        if (isFailedStatus(info.status)) {
          throw new Error(`Torrent failed: ${info.status}`)
        }
        const pct = info.progress ?? 0
        res.set('Retry-After', String(Math.max(1, Math.ceil(PROGRESS_POLL_MS / 1000))))
        res.status(503).send(`Buffering (${pct}%): ${job.label || 'torrent'}`)
        return
      }
      job.hostLink = resolved.hostLink
      job.downloadUrl = resolved.download
    } else {
      await waitUntilStreamable(job)
    }
  }

  await proxy.proxyStream(req, res, job.downloadUrl, job.apiUrl, {
    refreshUrl: () => progress.withJobLock(job, () => refreshStreamUrl(job)),
  })
}

module.exports = {
  handleProgressRequest,
}
