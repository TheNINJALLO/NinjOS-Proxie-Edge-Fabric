// Copyright (C) 2026 Ninj-OS contributors.
// SPDX-License-Identifier: AGPL-3.0-only

'use strict'

const fs = require('node:fs')
const path = require('node:path')
const crypto = require('node:crypto')
const { NinjOSRelay } = require('./ninjos-relay')
const { signedHeaders } = require('./hmac')
const { ProtocolWeave } = require('./protocol-weave')

const configPath = path.resolve(process.env.SESSION_CORE_CONFIG || 'runtime/session-core.json')
let active = new Map()
let players = new Map()
let currentConfig = null
let reloading = false
let stateTimer = null
let protocolWeave = null
let lastInspectionWarning = 0

function log (...args) { console.log('[Ninj-OS Session Core]', ...args) }
function warn (...args) { console.warn('[Ninj-OS Session Core]', ...args) }

function writeRuntimeState () {
  if (!currentConfig) return
  const stateFile = path.resolve(currentConfig.stateFile || path.join(path.dirname(configPath), 'session-core-state.json'))
  const state = {
    timestamp: Date.now(),
    engine: 'session-core',
    version: '7.3.9',
    protocolPacks: protocolWeave?.catalog() || [],
    protocolInspection: protocolWeave?.inspection() || { enabled: false },
    backends: [...active.values()].map(({ backend, relay, codecProtocol }) => ({
      name: backend.id,
      displayName: backend.displayName,
      enabled: true,
      healthy: Boolean(relay),
      latencyMs: 0,
      activeSessions: [...players.values()].filter((player) => player.backendId === backend.id).length,
      activeClientProtocols: [...new Set([...players.values()]
        .filter((player) => player.backendId === backend.id)
        .map((player) => player.protocol))].sort((a, b) => a - b),
      publicPort: Number(backend.publicPort),
      backendHost: backend.host,
      backendPort: Number(backend.backendPort),
      connectionMode: 'full_proxy',
      adapter: backend.adapter,
      status: relay ? 'listening' : 'offline',
      protocolCompatibility: protocolWeave?.summary(codecProtocol)
    }))
  }
  const temporary = `${stateFile}.tmp`
  try {
    fs.mkdirSync(path.dirname(stateFile), { recursive: true })
    fs.writeFileSync(temporary, `${JSON.stringify(state, null, 2)}\n`, { mode: 0o600 })
    fs.renameSync(temporary, stateFile)
  } catch (error) {
    warn('Could not write Full Proxy health state:', error.message)
  }
}

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
    player.queue('text', { type: 'system', needs_translation: false, source_name: '', xuid: '', platform_chat_id: '', filtered_message: '', message: '§bNinj-OS Proxie v7.3.9 · Full Proxy Session Core' })
  }
  return true
}

function inspectPacketSafely (pack, backendId, direction, name, params, context) {
  try {
    return protocolWeave?.process(pack, backendId, direction, name, params, context) === true
  } catch (error) {
    const now = Date.now()
    if (now - lastInspectionWarning >= 5000) {
      warn(`Protocol inspection error (${backendId} ${direction} ${name}); forwarding unchanged:`, error.message)
      lastInspectionWarning = now
    }
    return false
  }
}

function makeRelay (backend) {
  const relay = new NinjOSRelay({
    version: currentConfig.version,
    host: currentConfig.listenHost || '0.0.0.0',
    port: Number(backend.publicPort),
    // Bedrock 26.33 sends the OIDC Token identity form without the legacy
    // Certificate/chain container. bedrock-protocol 3.57 only parses that
    // token form on its offline server path; encryption and Full Proxy packet
    // termination remain enabled, while strict legacy-chain verification is
    // unavailable for this hotfix login format.
    offline: true,
    maxPlayers: Number(backend.capacity || 50),
    motd: {
      motd: currentConfig.motd,
      levelName: `${currentConfig.subMotd} · ${backend.displayName}`,
      // Bedrock 26.31-26.33 retain the 26.30 packet protocol (1001), but
      // clients expect the current hotfix version in the server advertisement.
      version: currentConfig.advertisedVersion || '1.26.33'
    },
    destination: { host: backend.host, port: Number(backend.backendPort), offline: true },
    profilesFolder: currentConfig.profilesFolder,
    raknetBackend: 'raknet-native',
    enableChunkCaching: false,
    backendId: backend.id,
    logging: false
  }, {
    fallbackOrDisconnect: (player, reason) => fallbackOrDisconnect(backend, player, reason),
    onBackendError: (_player, error) => warn(`${backend.id}:`, error.message)
  })
  const codecProtocol = Number(relay.options.protocolVersion)

  const parsePacketBuffer = relay.deserializer.parsePacketBuffer.bind(relay.deserializer)
  relay.deserializer.parsePacketBuffer = (packet) => {
    try {
      return parsePacketBuffer(packet)
    } catch (error) {
      const context = relay.__ninjosParseContext
      if (context) {
        protocolWeave.observeDecodeFailure(
          context.player.__ninjosProtocolPack,
          backend.id,
          context.direction,
          packet,
          error
        )
      }
      throw error
    }
  }

  relay.on('connect', (player) => {
    const wrapPacketReader = (method, direction) => {
      const original = player[method].bind(player)
      player[method] = (packet) => {
        relay.__ninjosParseContext = { player, direction }
        try { return original(packet) } finally { relay.__ninjosParseContext = null }
      }
    }
    wrapPacketReader('readPacket', 'serverbound')
    wrapPacketReader('readUpstream', 'clientbound')
    const expectedProtocol = codecProtocol
    const originalDecodeLoginJWT = player.decodeLoginJWT.bind(player)
    player.decodeLoginJWT = (...args) => {
      try {
        return originalDecodeLoginJWT(...args)
      } catch (error) {
        warn(
          `${backend.id}: downstream authentication verification failed:`,
          error?.stack || error?.message || String(error)
        )
        throw error
      }
    }
    const originalProtocolCheck = player.handleClientProtocolVersion.bind(player)
    player.handleClientProtocolVersion = (clientProtocol) => {
      const numericProtocol = Number(clientProtocol)
      const pack = protocolWeave.resolve(numericProtocol)
      player.__ninjosProtocolPack = pack
      log(
        `${backend.id}: client network protocol=${numericProtocol}; ` +
        `expected=${expectedProtocol}; advertised=${relay.advertisement.version}`
      )

      if (protocolWeave.accepts(numericProtocol, expectedProtocol)) {
        log(`${backend.id}: protocol pack ${pack.name} selected (${pack.mode})`)
        return true
      }
      warn(`${backend.id}: no reviewed protocol pack can map ${numericProtocol} to codec ${expectedProtocol}`)
      return originalProtocolCheck(clientProtocol)
    }

    player.on('loggingIn', (body) => {
      log(
        `${backend.id}: client login protocol=${body.params.protocol_version}; ` +
        `expected=${relay.options.protocolVersion}; advertised=${relay.advertisement.version}`
      )
    })

    player.on('login', async () => {
      log(`${backend.id}: downstream identity accepted for ${player.profile?.name || 'Unknown'}`)
      const key = String(player.profile?.xuid || player.profile?.uuid || crypto.randomUUID())
      players.set(key, { key, name: player.profile?.name || 'Unknown', xuid: String(player.profile?.xuid || ''), backendId: backend.id, protocol: Number(player.version || 0), connectedAt: Date.now(), player })
      try {
        await createIdentityGrant(backend, player)
        await reportPresence(backend, player, 'proxy.player_authenticated')
      } catch (error) {
        warn(`Identity grant rejected for ${player.profile?.name || 'Unknown'}:`, error.message)
        if (backend.requireProxyIdentity) player.disconnect(`Ninj-OS identity verification failed: ${error.message}`)
      }
    })
    player.on('join', () => {
      log(`${backend.id}: downstream encrypted handshake completed for ${player.profile?.name || 'Unknown'}`)
    })
    player.on('error', (error) => {
      warn(`${backend.id}: downstream error:`, error?.stack || error?.message || String(error))
    })
    player.on('serverbound', ({ name, params }, descriptor) => {
      descriptor.ninjosReencode = inspectPacketSafely(player.__ninjosProtocolPack, backend.id, 'serverbound', name, params, {
        rawBuffer: descriptor?.fullBuffer,
        serialize: (packetName, packetParams) => relay.serializer.createPacketBuffer({ name: packetName, params: packetParams })
      })
      if (name === 'command_request') handleProxyCommand(backend, player, params, descriptor)
    })
    player.on('clientbound', ({ name, params }, descriptor) => {
      descriptor.ninjosReencode = inspectPacketSafely(player.__ninjosProtocolPack, backend.id, 'clientbound', name, params, {
        rawBuffer: descriptor?.fullBuffer,
        serialize: (packetName, packetParams) => relay.serializer.createPacketBuffer({ name: packetName, params: packetParams })
      })
    })
    player.on('close', (reason) => {
      warn(`${backend.id}: downstream connection closed:`, reason || 'no reason supplied')
      const key = String(player.profile?.xuid || player.profile?.uuid || '')
      if (key) players.delete(key)
      reportPresence(backend, player, 'proxy.player_disconnected')
    })
  })
  relay.on('join', (downstream) => {
    log(`${backend.id}: upstream Endstone session established for ${downstream.profile?.name || 'Unknown'}`)
  })
  relay.on('error', (error) => warn(`${backend.id} relay error:`, error.message))
  return { relay, codecProtocol }
}

async function stopAll () {
  for (const { relay } of active.values()) {
    try { relay.close('Configuration reload') } catch (_) {}
  }
  active.clear()
  players.clear()
  protocolWeave?.close()
  protocolWeave = null
  writeRuntimeState()
}

function resolveProtocolPackDirectory (configured) {
  const bundled = path.join(__dirname, '..', 'protocol-packs')
  if (!configured) return bundled
  if (path.isAbsolute(configured)) return fs.existsSync(configured) ? configured : bundled

  // Generated paths are rooted at the installation directory, not whichever
  // working directory happened to launch Node.js.
  const installRoot = path.dirname(path.dirname(configPath))
  const fromInstallRoot = path.resolve(installRoot, configured)
  if (fs.existsSync(fromInstallRoot)) return fromInstallRoot
  return bundled
}

async function loadAll () {
  if (reloading) return
  reloading = true
  try {
    const next = readConfig()
    await stopAll()
    currentConfig = next
    protocolWeave = new ProtocolWeave({
      packDirectory: resolveProtocolPackDirectory(next.protocolPackDirectory),
      observationDirectory: next.protocolObservationDirectory || path.join(path.dirname(configPath), 'protocol-observations'),
      captureEnabled: next.protocolCaptureEnabled,
      captureMode: next.protocolCaptureMode,
      maxObservationBytes: next.protocolObservationMaxBytes,
      maxPacketBytes: next.protocolCaptureMaxPacketBytes,
      selectedPacketIds: next.protocolCapturePacketIds,
      captureDecodeFailures: next.protocolCaptureDecodeFailures
    })
    log(`Protocol Weave loaded ${protocolWeave.catalog().length} reviewed pack(s)`)
    if (!next.enabled) {
      log('Disabled by configuration.')
      writeRuntimeState()
      return
    }
    for (const backend of next.backends.filter((item) => item.enabled)) {
      const { relay, codecProtocol } = makeRelay(backend)
      relay.listen()
      active.set(backend.id, { backend, relay, codecProtocol })
      log(`Full proxy listener ${next.listenHost}:${backend.publicPort}/UDP -> ${backend.host}:${backend.backendPort} (${backend.adapter})`)
    }
    if (active.size === 0) log('No full_proxy backends are enabled. Transparent gateway mode can continue independently.')
    writeRuntimeState()
    if (!stateTimer) stateTimer = setInterval(writeRuntimeState, 1000)
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
