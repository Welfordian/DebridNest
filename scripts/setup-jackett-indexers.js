#!/usr/bin/env node

const fs = require('fs')

const JACKETT_URL = (process.env.JACKETT_URL || 'http://jackett:9117').replace(/\/+$/, '')
const JACKETT_CONFIG_FILE = process.env.JACKETT_CONFIG_FILE || '/jackett-config/Jackett/ServerConfig.json'
const DEFAULT_INDEXERS = '1337x,knaben,limetorrents,magnetz,nyaasi,rutracker-ru,thepiratebay,therarbg,yts'
const INDEXERS = (process.env.JACKETT_INDEXERS || DEFAULT_INDEXERS)
  .split(',')
  .map((name) => name.trim())
  .filter(Boolean)
const MAX_WAIT_MS = Number(process.env.JACKETT_SETUP_TIMEOUT_MS || 120000)

function readApiKey() {
  if (process.env.JACKETT_API_KEY && process.env.JACKETT_API_KEY.trim()) {
    return process.env.JACKETT_API_KEY.trim()
  }
  try {
    const config = JSON.parse(fs.readFileSync(JACKETT_CONFIG_FILE, 'utf8'))
    return config.APIKey || ''
  } catch {
    return ''
  }
}

function parseSetCookie(setCookieHeader) {
  const headers = Array.isArray(setCookieHeader) ? setCookieHeader : [setCookieHeader]
  return headers
    .filter(Boolean)
    .map((entry) => entry.split(';')[0])
    .join('; ')
}

class CookieClient {
  constructor() {
    this.cookie = ''
  }

  async fetch(path, options = {}) {
    const url = path.startsWith('http') ? path : `${JACKETT_URL}${path.startsWith('/') ? path : `/${path}`}`
    const headers = { ...(options.headers || {}) }
    if (this.cookie) {
      headers.Cookie = this.cookie
    }

    const res = await fetch(url, { ...options, headers, redirect: 'manual' })
    const setCookie = res.headers.getSetCookie?.() || res.headers.raw?.()['set-cookie']
    if (setCookie?.length) {
      const next = parseSetCookie(setCookie)
      this.cookie = this.cookie ? `${this.cookie}; ${next}` : next
    }

    if (res.status >= 300 && res.status < 400) {
      const location = res.headers.get('location')
      if (location) {
        const nextPath = new URL(location, url).toString()
        return this.fetch(nextPath, { ...options, headers: undefined })
      }
    }

    return res
  }
}

async function waitForJackett(apiKey) {
  const probe = `${JACKETT_URL}/api/v2.0/indexers/all/results/torznab/api?apikey=${encodeURIComponent(apiKey)}&t=indexers`
  const started = Date.now()
  while (Date.now() - started < MAX_WAIT_MS) {
    try {
      const res = await fetch(probe)
      if (res.ok) {
        return
      }
    } catch {
      // retry
    }
    await new Promise((resolve) => setTimeout(resolve, 2000))
  }
  throw new Error(`Jackett did not become ready within ${MAX_WAIT_MS}ms`)
}

async function login(client) {
  await client.fetch('/UI/Login')
  const res = await client.fetch('/UI/Login?cookiesChecked=1')
  if (!res.ok && res.status !== 302) {
    throw new Error(`Jackett login failed with HTTP ${res.status}`)
  }
}

async function listConfigured(client, apiKey) {
  const res = await client.fetch(`/api/v2.0/indexers?configured=true&apikey=${encodeURIComponent(apiKey)}`)
  if (!res.ok) {
    throw new Error(`Failed to list configured indexers: HTTP ${res.status}`)
  }
  return res.json()
}

async function configureIndexer(client, apiKey, indexerId) {
  const configRes = await client.fetch(`/api/v2.0/indexers/${indexerId}/Config?apikey=${encodeURIComponent(apiKey)}`)
  if (!configRes.ok) {
    throw new Error(`${indexerId}: config schema HTTP ${configRes.status}`)
  }

  const schema = await configRes.json()
  const body = schema.map((item) => ({
    id: item.id,
    name: item.name,
    value: item.value ?? '',
  }))

  const saveRes = await client.fetch(`/api/v2.0/indexers/${indexerId}/Config?apikey=${encodeURIComponent(apiKey)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })

  if (!saveRes.ok && saveRes.status !== 204) {
    const text = await saveRes.text()
    throw new Error(`${indexerId}: save HTTP ${saveRes.status} ${text.slice(0, 120)}`)
  }

  const testRes = await client.fetch(`/api/v2.0/indexers/${indexerId}/test?apikey=${encodeURIComponent(apiKey)}`, {
    method: 'POST',
  })
  if (!testRes.ok && testRes.status !== 204) {
    const text = await testRes.text()
    console.warn(`[jackett-setup] ${indexerId}: test warning HTTP ${testRes.status} ${text.slice(0, 120)}`)
  }
}

async function main() {
  const apiKey = readApiKey()
  if (!apiKey) {
    throw new Error('Jackett API key not found (set JACKETT_API_KEY or mount ServerConfig.json)')
  }

  const client = new CookieClient()
  await waitForJackett(apiKey)
  await login(client)

  const configured = await listConfigured(client, apiKey)
  const configuredIds = new Set(configured.map((entry) => entry.id))

  for (const indexerId of INDEXERS) {
    if (configuredIds.has(indexerId)) {
      console.log(`[jackett-setup] ${indexerId}: already configured`)
      continue
    }

    try {
      await configureIndexer(client, apiKey, indexerId)
      console.log(`[jackett-setup] ${indexerId}: configured`)
    } catch (err) {
      console.warn(`[jackett-setup] ${indexerId}: ${err.message}`)
    }
  }

  const final = await listConfigured(client, apiKey)
  console.log(`[jackett-setup] configured indexers: ${final.map((entry) => entry.name).join(', ') || '(none)'}`)
  if (!final.length) {
    process.exitCode = 1
  }
}

main().catch((err) => {
  console.error('[jackett-setup] failed:', err.message)
  process.exit(1)
})
