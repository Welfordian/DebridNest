const crypto = require('crypto')

const jobs = new Map()
const TTL_MS = 6 * 60 * 60 * 1000

function cleanup() {
  const now = Date.now()
  for (const [token, job] of jobs.entries()) {
    if (job.expiresAt <= now) {
      jobs.delete(token)
    }
  }
}

setInterval(cleanup, 60_000).unref()

function touchJob(job) {
  job.expiresAt = Date.now() + TTL_MS
}

function createJob({ torrentId, magnet, torrentLink, nzbUrl, apiUrl, apiToken, label }) {
  const token = crypto.randomBytes(16).toString('hex')
  jobs.set(token, {
    torrentId: torrentId || null,
    magnet: magnet || null,
    torrentLink: torrentLink || null,
    nzbUrl: nzbUrl || null,
    apiUrl,
    apiToken,
    label,
    hostLink: null,
    downloadUrl: null,
    expiresAt: Date.now() + TTL_MS,
  })
  return token
}

function getJob(token) {
  const job = jobs.get(token)
  if (!job) {
    return null
  }
  if (job.expiresAt <= Date.now()) {
    jobs.delete(token)
    return null
  }
  touchJob(job)
  return job
}

async function withJobLock(job, fn) {
  job._lock = (job._lock || Promise.resolve())
    .catch(() => {})
    .then(fn)
  return job._lock
}

module.exports = {
  createJob,
  getJob,
  touchJob,
  withJobLock,
}
