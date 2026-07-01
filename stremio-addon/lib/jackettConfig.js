const fs = require('fs')
const path = require('path')

const PLACEHOLDER_KEYS = new Set([
  '',
  'lol',
  'change-me',
  'your-jackett-key',
  'your_jackett_key',
  'your-jackett-api-key',
])

function isPlaceholderKey(value) {
  if (value === undefined || value === null) {
    return true
  }
  const normalized = String(value).trim().toLowerCase()
  return PLACEHOLDER_KEYS.has(normalized)
}

function readApiKeyFromFile(filePath) {
  if (!filePath) {
    return ''
  }
  try {
    const config = JSON.parse(fs.readFileSync(filePath, 'utf8'))
    return config.APIKey || ''
  } catch {
    return ''
  }
}

function countConfiguredIndexers(configFile) {
  if (!configFile) {
    return 0
  }
  const indexersDir = path.join(path.dirname(configFile), 'Indexers')
  try {
    return fs.readdirSync(indexersDir).filter((name) => name.endsWith('.json')).length
  } catch {
    return 0
  }
}

function resolveDefaultApiKey() {
  const fromEnv = process.env.JACKETT_API_KEY || ''
  if (!isPlaceholderKey(fromEnv)) {
    return fromEnv
  }
  return readApiKeyFromFile(process.env.JACKETT_CONFIG_FILE) || fromEnv
}

async function fetchIndexerStats(jackettUrl, apiKey) {
  const configured = countConfiguredIndexers(process.env.JACKETT_CONFIG_FILE)
  if (configured > 0) {
    return { configured, total: configured }
  }

  const base = String(jackettUrl || '').replace(/\/+$/, '')
  if (!base || isPlaceholderKey(apiKey)) {
    return { configured: 0, error: 'Jackett API key not configured' }
  }

  const url = `${base}/api/v2.0/indexers/all/results/torznab/api?apikey=${encodeURIComponent(apiKey)}&t=movie&cat=2000,5000&limit=1&imdbid=0111161`
  try {
    const res = await fetch(url)
    if (res.status === 401) {
      return { configured: 0, error: 'Jackett API key rejected (401)' }
    }
    if (!res.ok) {
      return { configured: 0, error: `Jackett search probe HTTP ${res.status}` }
    }
    return { configured: 0, total: 0 }
  } catch (err) {
    return { configured: 0, error: err.message || 'Jackett unreachable' }
  }
}

module.exports = {
  isPlaceholderKey,
  resolveDefaultApiKey,
  fetchIndexerStats,
}
