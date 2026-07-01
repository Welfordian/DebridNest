const progress = require('./progress')
const debridnest = require('./debridnest')

const PROGRESS_POLL_MS = Number(process.env.PROGRESS_POLL_MS || 2000)

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
      .then(async (torrentId) => {
        job.torrentId = torrentId
        delete job.starting
        try {
          await debridnest.prepareTorrent(job.apiUrl, job.apiToken, torrentId)
        } catch {
          // magnet may still be resolving metadata
        }
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

async function resolveJobStream(job) {
  if (job.downloadUrl) {
    return job.downloadUrl
  }

  const resolved = await debridnest.resolveStreamUrl(job.apiUrl, job.apiToken, job.torrentId)
  if (resolved) {
    job.hostLink = resolved.hostLink
    job.downloadUrl = resolved.download
    return job.downloadUrl
  }

  return null
}

async function waitForJobDownloadUrl(job, maxWaitMs = Number(process.env.PROGRESS_MAX_WAIT_MS || 120000)) {
  await ensureTorrentStarted(job)
  const deadline = Date.now() + maxWaitMs
  while (Date.now() < deadline) {
    const url = await resolveJobStream(job)
    if (url) {
      return url
    }
    await new Promise((resolve) => setTimeout(resolve, PROGRESS_POLL_MS))
  }
  throw new Error('Timed out waiting for stream to buffer')
}

async function sendBufferingResponse(res, job) {
  const info = await debridnest.getTorrentInfo(job.apiUrl, job.apiToken, job.torrentId)
  if (isFailedStatus(info.status)) {
    throw new Error(`Torrent failed: ${info.status}`)
  }
  const pct = info.progress ?? 0
  res.set('Retry-After', String(Math.max(1, Math.ceil(PROGRESS_POLL_MS / 1000))))
  res.status(503).send(`Buffering (${pct}%): ${job.label || 'torrent'}`)
}

async function handleProgressRequest(req, res, job) {
  await progress.withJobLock(job, () => ensureTorrentStarted(job))

  const downloadUrl = await resolveJobStream(job)
  if (!downloadUrl) {
    await sendBufferingResponse(res, job)
    return
  }

  // Redirect to DebridNest directly for fast seeking.
  res.redirect(302, downloadUrl)
}

module.exports = {
  handleProgressRequest,
  ensureTorrentStarted,
  resolveJobStream,
  waitForJobDownloadUrl,
}
