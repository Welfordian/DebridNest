const progress = require('./progress')
const progressHandler = require('./progressHandler')

const PLAY_WAIT_MS = Number(process.env.PLAY_WAIT_MS || 30000)
const PLAY_POLL_MS = Number(process.env.PLAY_POLL_MS || 500)

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

async function resolvePlayUrl(entry, options = {}) {
  if (entry.directUrl) {
    return entry.directUrl
  }

  if (entry.progressToken) {
    const job = progress.getJob(entry.progressToken)
    if (!job) {
      throw new Error('Download job not found or expired')
    }
    await progressHandler.ensureTorrentStarted(job)
    const url = await progressHandler.resolveJobStream(job, {
      infoWait: options.infoWait,
    })
    if (!url) {
      throw new Error('Stream not ready')
    }
    return url
  }

  throw new Error('Stream entry has no playback source')
}

async function waitForPlayUrl(entry, options = {}) {
  const maxWaitMs = options.maxWaitMs || PLAY_WAIT_MS
  const pollMs = options.pollMs || PLAY_POLL_MS
  const deadline = Date.now() + maxWaitMs

  while (Date.now() < deadline) {
    try {
      const remaining = deadline - Date.now()
      const waitSeconds = Math.max(1, Math.min(25, Math.ceil(remaining / 1000)))
      return await resolvePlayUrl(entry, { infoWait: `${waitSeconds}s` })
    } catch (err) {
      if (err.message !== 'Stream not ready') {
        throw err
      }
    }
    await sleep(pollMs)
  }

  throw new Error('Timed out waiting for stream to buffer')
}

module.exports = {
  resolvePlayUrl,
  waitForPlayUrl,
}
