// Copyright (C) 2026 Ninj-OS contributors.
// SPDX-License-Identifier: AGPL-3.0-only

'use strict'

const fs = require('node:fs')
const path = require('node:path')
const crypto = require('node:crypto')
const { NinjOSRelay } = require('./ninjos-relay')
const { signedHeaders } = require('./hmac')

const configPath = path.resolve(process.env.SESSION_CORE_CONFIG || 'runtime/session-core.json')
let active = new Map()
let players = new Map()
let currentConfig = null
let reloading = false

function log (...args) { console.log('[Ninj-OS Session Core]', ...args) }
function warn (...args) { console.warn('[Ninj-OS Session Core]', ...args) }

function readConfig () {
  const config = JSON.parse(fs.readFileSync(configPath, 'utf8'))
  if (!config.internalToken) throw new Error('session_core.internal_token is empty')
  if (!Array.isArray(config.backends)) config.backends = []
  return config
}

function publicAddress (backend) {
  return { host: currentConfig.publicHost, port: Number(backend.publicPort) }
}

function transferPlayer (player, backendId, message) {
  const target = currentConfig.backends.find((item) => item.id === backendId && item.enabled)
  if (!target) {
    player.queue('text', {
      type: 'system', needs_translation: false, source_name: '', xuid: '', platform_chat_id: '', filtered_message: '',
      message: `§cServer ${backendId} is unavailable.`
    })
    return false
  }
  const address = publicAddress(target)
  if (message) {
    player.queue('text', {
      type: 'system', needs_translation: false, source_name: '', xuid: '', platform_chat_id: '', filtered_message: '',
      message
    })
  }
  player.queue('transfer', {
    server_address: address.host,
    port: address.port,
    reload_world: false
  })
  return true
}

function fallbackFor (backend) {
  return backend.fallbackBackend || currentConfig.primaryBackend
}

function fallbackOrDisconnect (backend, player, reason) {
  if (player.__ninjosClosing) return
  player.__ninjosClosing = true
  const fallback = fallbackFor(backend)
  if (fallback && fallback !== backend.id && transferPlayer(player, fallback, `§e${reason}. Moving you to ${fallback}.`)) {
    setTimeout(() => player.close('fallback transfer'), 500)
    return
  }
  player.disconnect(reason)
}

async function postJson (url, body, headers = {}) {
  const response = await fetch(url, { method: 'POST', headers: { 'content-type': 'application/json', ...headers }, body })
  const text = await response.text()
  let payload = {}
  try { payload = text ? JSON.parse(text) : {} } catch (_) { payload = { raw: text } }
  if (!response.ok) throw new Error(payload.error || `HTTP ${response.status}`)
  return payload
}

async function createIdentityGrant (backend, player) {
  const profile = player.profile || {}
  const sessionId = crypto.randomUUID()
  player.__ninjosSessionId = sessionId
  const body = JSON.stringify({
    sessionId,
    serverId: backend.id,
    username: profile.name || 'Unknown',
    xuid: String(profile.xuid || ''),
    uuid: String(profile.uuid || ''),
    originalIp: player.connection?.address?.address || player.connection?.address?.toString?.() || '',
    clientVersion: currentConfig.version,
    protocolVersion: Number(player.version || 0),
    expiresAt: Date.now() + 30000
  })
  return postJson(`${currentConfig.dashboardUrl}/api/session-core/v1/grants`, body, {
    authorization: `Bearer ${currentConfig.internalToken}`
  })
}

async function reportPresence (backend, player, eventType) {
  if (!backend.companionSecret) return
  const profile = player.profile || {}
  const body = JSON.stringify({
    serverId: backend.id,
    records: [{
      type: 'event', eventType, timestamp: Date.now(), severity: 'info',
      player: profile.name || '', playerName: profile.name || '', xuid: String(profile.xuid || ''),
      address: player.connection?.address?.address || '', message: `${profile.name || 'Player'} ${eventType}`,
      source: 'session-core', sessionId: player.__ninjosSessionId || ''
    }]
  })
  try {
    await postJson(`${currentConfig.dashboardUrl}/ingest`, body, signedHeaders(backend.id, backend.companionSecret, body))
  } catch (error) {
    warn(`Presence report failed for ${backend.id}:`, error.message)
  }
}

function sendServerList (player) {
  const list = currentConfig.backends
    .filter((item) => item.enabled)
    .map((item) => `${item.id}${active.get(item.id)?.relay?.clientCount >= item.capacity ? ' (full)' : ''}`)
    .join(', ')
  player.queue('text', {
    type: 'system', needs_translation: false, source_name: '', xuid: '', platform_chat_id: '', filtered_message: '',
    message: `§bAvailable servers: §f${list || 'none'}`
  })
}

function handleProxyCommand (backend, player, packet, descriptor) {
  const raw = String(packet.command || '').trim()
  const [command, ...args] = raw.replace(/^\//, '').split(/\s+/)
  const name = command.toLowerCase()
  if (!['server', 'hub', 'lobby', 'glist', 'find', 'proxie'].includes(name)) return false
  descriptor.canceled = true

  if (name === 'server') {
    if (!args[0]) sendServerList(player)
    else transferPlayer(player, args[0].toLowerCase(), `§7Connecting to ${args[0]}...`)
  } else if (name === 'hub' || name === 'lobby') {
    const target = currentConfig.backends.find((item) => item.id === 'lobby')?.id || currentConfig.primaryBackend
    transferPlayer(player, target, '§7Returning to the network lobby...')
  } else if (name === 'glist') {
    const grouped = {}
    for (const entry of players.values()) {
      grouped[entry.backendId] = (grouped[entry.backendId] || 0) + 1
    }
    const message = Object.entries(grouped).map(([id, count]) => `${id}: ${count}`).join(' · ') || 'No players tracked'
    player.queue('text', { type: 'system', needs_translation: false, source_name: '', xuid: '', platform_chat_id: '', filtered_message: '', message: `§bNetwork players (${players.size}): §f${message}` })
  } else if (name === 'find') {
    const needle = (args[0] || '').toLowerCase()
    const found = [...players.values()].find((entry) => entry.name.toLowerCase() === needle)
    const message = found ? `${found.name} is on ${found.backendId}` : `${args[0] || 'Player'} is not online`
    player.queue('text', { type: 'system', needs_translation: false, source_name: '', xuid: '', platform_chat_id: '', filtered_message: '', message: `§b${message}` })
  } else {
    player.queue('text', { type: 'system', needs_translation: false, source_name: '', xuid: '', platform_chat_id: '', filtered_message: '', message: '§bNinj-OS Proxie v7.3.0 · Full Proxy Session Core' })
  }
  return true
}

function makeRelay (backend) {
  const relay = new NinjOSRelay({
    version: currentConfig.version,
    host: currentConfig.listenHost || '0.0.0.0',
    port: Number(backend.publicPort),
    offline: false,
    maxPlayers: Number(backend.capacity || 50),
    motd: { motd: currentConfig.motd, levelName: `${currentConfig.subMotd} · ${backend.displayName}` },
    destination: { host: backend.host, port: Number(backend.backendPort), offline: true },
    profilesFolder: currentConfig.profilesFolder,
    raknetBackend: 'jsp-raknet',
    enableChunkCaching: false,
    backendId: backend.id,
    logging: false
  }, {
    fallbackOrDisconnect: (player, reason) => fallbackOrDisconnect(backend, player, reason),
    onBackendError: (_player, error) => warn(`${backend.id}:`, error.message)
  })

  relay.on('connect', (player) => {
    player.on('login', async () => {
      const key = String(player.profile?.xuid || player.profile?.uuid || crypto.randomUUID())
      players.set(key, { key, name: player.profile?.name || 'Unknown', xuid: String(player.profile?.xuid || ''), backendId: backend.id, connectedAt: Date.now(), player })
      try {
        await createIdentityGrant(backend, player)
        await reportPresence(backend, player, 'proxy.player_authenticated')
      } catch (error) {
        warn(`Identity grant rejected for ${player.profile?.name || 'Unknown'}:`, error.message)
        if (backend.requireProxyIdentity) player.disconnect(`Ninj-OS identity verification failed: ${error.message}`)
      }
    })
    player.on('serverbound', ({ name, params }, descriptor) => {
      if (name === 'command_request') handleProxyCommand(backend, player, params, descriptor)
    })
    player.on('close', () => {
      const key = String(player.profile?.xuid || player.profile?.uuid || '')
      if (key) players.delete(key)
      reportPresence(backend, player, 'proxy.player_disconnected')
    })
  })
  relay.on('error', (error) => warn(`${backend.id} relay error:`, error.message))
  return relay
}

async function stopAll () {
  for (const { relay } of active.values()) {
    try { relay.close('Configuration reload') } catch (_) {}
  }
  active.clear()
  players.clear()
}

async function loadAll () {
  if (reloading) return
  reloading = true
  try {
    const next = readConfig()
    await stopAll()
    currentConfig = next
    if (!next.enabled) {
      log('Disabled by configuration.')
      return
    }
    for (const backend of next.backends.filter((item) => item.enabled)) {
      const relay = makeRelay(backend)
      relay.listen()
      active.set(backend.id, { backend, relay })
      log(`Full proxy listener ${next.listenHost}:${backend.publicPort}/UDP -> ${backend.host}:${backend.backendPort} (${backend.adapter})`)
    }
    if (active.size === 0) log('No full_proxy backends are enabled. Transparent gateway mode can continue independently.')
  } finally {
    reloading = false
  }
}

process.on('SIGINT', async () => { await stopAll(); process.exit(0) })
process.on('SIGTERM', async () => { await stopAll(); process.exit(0) })
process.on('uncaughtException', (error) => warn('Uncaught exception:', error.stack || error))
process.on('unhandledRejection', (error) => warn('Unhandled rejection:', error?.stack || error))

loadAll().catch((error) => { console.error(error); process.exit(1) })
fs.watchFile(configPath, { interval: 2000 }, () => {
  log('Configuration changed; rebuilding full-proxy listeners.')
  loadAll().catch((error) => warn('Reload failed:', error.message))
})
