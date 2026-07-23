'use strict'

const fs = require('node:fs')
const path = require('node:path')
const crypto = require('node:crypto')

const SAFE_MODES = new Set(['native', 'alias', 'translated', 'observe'])
const SAFE_OPERATIONS = new Set(['rename_field', 'add_default', 'drop_field'])
const REDACTED_KEYS = /token|secret|certificate|chain|key|xuid|identity|uuid|address|device|skin/i
const SENSITIVE_PACKET_NAMES = /login|handshake|network_settings|resource_pack|client_cache_blob/i
const SENSITIVE_PACKET_IDS = new Set([1, 3, 4])
const BUILTIN_BASELINE_PACK = Object.freeze({
  schemaVersion: 1,
  name: 'Bedrock 1.26.30-1.26.33 (built-in baseline)',
  protocol: 1001,
  minecraftVersions: ['1.26.30', '1.26.31', '1.26.32', '1.26.33'],
  codecVersion: '1.26.30',
  mode: 'native',
  translators: []
})

function parsePacketId (buffer) {
  if (!Buffer.isBuffer(buffer) || buffer.length === 0) return null
  let header = 0
  let shift = 0
  for (let index = 0; index < Math.min(buffer.length, 5); index++) {
    const value = buffer[index]
    header |= (value & 0x7f) << shift
    if ((value & 0x80) === 0) return header & 0x3ff
    shift += 7
  }
  return null
}

function bufferHash (buffer) {
  return crypto.createHash('sha256').update(buffer).digest('hex')
}

function firstMismatch (left, right) {
  const length = Math.min(left.length, right.length)
  for (let index = 0; index < length; index++) if (left[index] !== right[index]) return index
  return left.length === right.length ? -1 : length
}

function parsePacketIdList (value) {
  if (value instanceof Set) return value
  const items = Array.isArray(value) ? value : String(value || '').split(',')
  return new Set(items.map((item) => Number(String(item).trim())).filter((item) => Number.isInteger(item) && item >= 0 && item <= 1023))
}

function cloneValue (value) {
  if (value === undefined) return undefined
  if (typeof structuredClone === 'function') return structuredClone(value)
  return JSON.parse(JSON.stringify(value, (_key, item) => typeof item === 'bigint' ? item.toString() : item))
}

function redactValue (value, depth = 0) {
  if (depth > 8) return '[DEPTH_LIMIT]'
  if (Buffer.isBuffer(value) || ArrayBuffer.isView(value)) return `[BINARY:${value.byteLength} bytes]`
  if (typeof value === 'string') return value.length > 512 ? `${value.slice(0, 512)}[TRUNCATED]` : value
  if (Array.isArray(value)) return value.slice(0, 64).map((item) => redactValue(item, depth + 1))
  if (typeof value === 'bigint') return value.toString()
  if (!value || typeof value !== 'object') return value
  const output = {}
  for (const [key, item] of Object.entries(value).slice(0, 128)) {
    output[key] = REDACTED_KEYS.test(key) ? '[REDACTED]' : redactValue(item, depth + 1)
  }
  return output
}

function summarizePacket (name, params, context = {}) {
  if (!params || typeof params !== 'object') return null
  if (name === 'command_request') {
    const commandName = String(params.command || '').trim().replace(/^\//, '').split(/\s+/, 1)[0].toLowerCase()
    return {
      category: 'command',
      commandName,
      originType: String(params.origin?.type || 'unknown'),
      internal: params.internal === true,
      proxyIntercepted: context.proxyIntercepted === true
    }
  }
  if (name !== 'player_auth_input') return null
  const flags = params.input_data && typeof params.input_data === 'object'
    ? Object.entries(params.input_data).filter(([, enabled]) => enabled === true).map(([flag]) => flag)
    : []
  const actions = Array.isArray(params.block_action)
    ? params.block_action.map((entry) => String(entry?.action || 'unknown'))
    : []
  return {
    category: 'player_input',
    clientTick: typeof params.tick === 'bigint' ? params.tick.toString() : params.tick,
    inputFlags: flags,
    blockActionCount: actions.length,
    blockActions: actions,
    hasItemInteraction: params.transaction != null,
    hasItemStackRequest: params.item_stack_request != null
  }
}

function validatePack (pack, source) {
  if (pack.schemaVersion !== 1) throw new Error(`${source}: unsupported schemaVersion`)
  if (!Number.isInteger(pack.protocol) || pack.protocol <= 0) throw new Error(`${source}: protocol must be a positive integer`)
  if (!SAFE_MODES.has(pack.mode)) throw new Error(`${source}: unsupported mode ${pack.mode}`)
  if (!Array.isArray(pack.minecraftVersions) || pack.minecraftVersions.length === 0) throw new Error(`${source}: minecraftVersions is required`)
  if (!pack.codecVersion || typeof pack.codecVersion !== 'string') throw new Error(`${source}: codecVersion is required`)
  if (pack.mode !== 'native' && !Number.isInteger(pack.baseProtocol)) throw new Error(`${source}: non-native packs require baseProtocol`)
  pack.translators = Array.isArray(pack.translators) ? pack.translators : []
  for (const translator of pack.translators) {
    if (!['serverbound', 'clientbound'].includes(translator.direction)) throw new Error(`${source}: invalid translator direction`)
    if (!translator.packet || !Array.isArray(translator.operations)) throw new Error(`${source}: invalid translator definition`)
    for (const operation of translator.operations) {
      if (!SAFE_OPERATIONS.has(operation.op)) throw new Error(`${source}: unsupported operation ${operation.op}`)
      if (!operation.field && operation.op !== 'rename_field') throw new Error(`${source}: operation field is required`)
      if (operation.op === 'rename_field' && (!operation.from || !operation.to)) throw new Error(`${source}: rename_field requires from and to`)
    }
  }
  return pack
}

class ProtocolWeave {
  constructor (options = {}) {
    this.packDirectory = path.resolve(options.packDirectory || 'protocol-packs')
    this.observationDirectory = path.resolve(options.observationDirectory || 'runtime/protocol-observations')
    const requestedMode = options.captureMode === 'redacted_payload' ? 'decoded' : options.captureMode
    this.captureMode = ['metadata', 'decoded', 'wire', 'full'].includes(requestedMode) ? requestedMode : 'metadata'
    this.captureEnabled = options.captureEnabled !== false
    this.maxObservationBytes = Math.max(65536, Number(options.maxObservationBytes || 10 * 1024 * 1024))
    this.maxPacketBytes = Math.max(64, Math.min(1024 * 1024, Number(options.maxPacketBytes || 65536)))
    this.selectedPacketIds = parsePacketIdList(options.selectedPacketIds)
    this.captureDecodeFailures = options.captureDecodeFailures !== false
    this.maxPendingBytes = Math.min(this.maxObservationBytes, 8 * 1024 * 1024)
    this.pendingObservationBytes = 0
    this.packs = new Map()
    this.stats = new Map()
    this.pendingObservations = []
    this.droppedObservations = 0
    this.load()
    this.flushTimer = setInterval(() => this.flush(), 250)
    this.flushTimer.unref()
  }

  load () {
    this.packs.clear()
    if (fs.existsSync(this.packDirectory)) {
      for (const entry of fs.readdirSync(this.packDirectory, { withFileTypes: true })) {
        if (!entry.isDirectory()) continue
        const source = path.join(this.packDirectory, entry.name, 'pack.json')
        if (!fs.existsSync(source)) continue
        const pack = validatePack(JSON.parse(fs.readFileSync(source, 'utf8')), source)
        if (this.packs.has(pack.protocol)) throw new Error(`Duplicate protocol pack ${pack.protocol}`)
        this.packs.set(pack.protocol, Object.freeze(pack))
      }
    }
    if (this.packs.size === 0) this.packs.set(BUILTIN_BASELINE_PACK.protocol, BUILTIN_BASELINE_PACK)
    for (const pack of this.packs.values()) {
      this.stats.set(pack.protocol, { observedPackets: 0, translatedPackets: 0, lastPacketAt: 0, packetNames: new Set() })
      if (pack.mode !== 'native' && !this.packs.has(pack.baseProtocol)) {
        throw new Error(`Protocol pack ${pack.protocol} references missing base protocol ${pack.baseProtocol}`)
      }
      if (pack.wireCompatibleWith !== undefined && !Number.isInteger(pack.wireCompatibleWith)) {
        throw new Error(`Protocol pack ${pack.protocol} has an invalid wireCompatibleWith value`)
      }
    }
  }

  resolve (protocol) {
    return this.packs.get(Number(protocol)) || null
  }

  accepts (protocol, codecProtocol) {
    const pack = this.resolve(protocol)
    if (!pack) return false
    return pack.protocol === Number(codecProtocol) || pack.wireCompatibleWith === Number(codecProtocol)
  }

  applyOperations (params, operations) {
    let changed = false
    for (const operation of operations) {
      if (operation.op === 'rename_field' && Object.hasOwn(params, operation.from)) {
        params[operation.to] = params[operation.from]
        delete params[operation.from]
        changed = true
      } else if (operation.op === 'add_default' && !Object.hasOwn(params, operation.field)) {
        params[operation.field] = cloneValue(operation.value)
        changed = true
      } else if (operation.op === 'drop_field' && Object.hasOwn(params, operation.field)) {
        delete params[operation.field]
        changed = true
      }
    }
    return changed
  }

  process (pack, backendId, direction, name, params, context = {}) {
    if (!pack) return false
    const rawBuffer = Buffer.isBuffer(context.rawBuffer) ? context.rawBuffer : null
    const packetId = parsePacketId(rawBuffer)
    const sensitive = SENSITIVE_PACKET_IDS.has(packetId) || SENSITIVE_PACKET_NAMES.test(String(name || ''))
    const decodedBefore = !sensitive && ['decoded', 'full'].includes(this.captureMode) ? redactValue(params) : null
    let roundTrip = null
    if (this.captureMode === 'full' && rawBuffer && this.selectedPacketIds.has(packetId) && !sensitive && typeof context.serialize === 'function') {
      try {
        const encoded = context.serialize(name, params)
        const mismatch = firstMismatch(rawBuffer, encoded)
        roundTrip = {
          attempted: true,
          exact: mismatch === -1,
          mismatchOffset: mismatch,
          originalBytes: rawBuffer.length,
          encodedBytes: encoded.length,
          originalSha256: bufferHash(rawBuffer),
          encodedSha256: bufferHash(encoded)
        }
      } catch (error) {
        roundTrip = { attempted: true, exact: false, error: String(error?.message || error).slice(0, 512) }
      }
    }
    let translated = false
    for (const translator of pack.translators) {
      if (translator.direction === direction && translator.packet === name) {
        translated = this.applyOperations(params, translator.operations) || translated
      }
    }
    const stats = this.stats.get(pack.protocol)
    if (stats) {
      stats.observedPackets++
      if (translated) stats.translatedPackets++
      stats.lastPacketAt = Date.now()
      stats.packetNames.add(`${direction}:${name}`)
    }
    if (this.captureEnabled) this.observe(pack, backendId, direction, name, params, translated, {
      decodedBefore, rawBuffer, packetId, sensitive, roundTrip,
      proxyIntercepted: context.proxyIntercepted === true
    })
    return translated
  }

  observe (pack, backendId, direction, name, params, translated, context = {}) {
    const directory = path.join(this.observationDirectory, String(pack.protocol))
    fs.mkdirSync(directory, { recursive: true })
    const safeBackend = String(backendId || 'unknown').toLowerCase().replace(/[^a-z0-9_-]/g, '') || 'unknown'
    const file = path.join(directory, `${safeBackend}.jsonl`)
    const record = {
      recordId: crypto.randomUUID(), timestamp: Date.now(), type: 'protocol_packet', layer: 'protocol', protocol: pack.protocol, pack: pack.name,
      backend: safeBackend, direction, packetName: name, packetId: context.packetId,
      action: translated ? 'translated' : 'forward', translated,
      bytes: context.rawBuffer?.length || 0,
      fields: params && typeof params === 'object' ? Object.keys(params).sort() : [],
      captureTiers: ['metadata'], sensitive: context.sensitive === true
    }
    const semantic = summarizePacket(name, params, context)
    if (semantic) record.semantic = semantic
    if (!context.sensitive && ['decoded', 'full'].includes(this.captureMode)) {
      record.decoded = context.decodedBefore
      if (translated) record.translatedDecoded = redactValue(params)
      record.captureTiers.push('decoded')
    }
    if (!context.sensitive && ['wire', 'full'].includes(this.captureMode) && context.rawBuffer && this.selectedPacketIds.has(context.packetId)) {
      const selected = context.rawBuffer.subarray(0, this.maxPacketBytes)
      record.wire = {
        encoding: 'hex', data: selected.toString('hex'), capturedBytes: selected.length,
        originalBytes: context.rawBuffer.length, truncated: selected.length !== context.rawBuffer.length,
        sha256: bufferHash(context.rawBuffer)
      }
      record.captureTiers.push('wire')
    }
    if (context.roundTrip) {
      record.roundTrip = context.roundTrip
      record.captureTiers.push('round_trip')
    }
    this.queueObservation(file, record)
  }

  observeDecodeFailure (pack, backendId, direction, rawBuffer, error) {
    if (!this.captureEnabled || !this.captureDecodeFailures || !Buffer.isBuffer(rawBuffer)) return
    const packetId = parsePacketId(rawBuffer)
    const sensitive = SENSITIVE_PACKET_IDS.has(packetId)
    const safeBackend = String(backendId || 'unknown').toLowerCase().replace(/[^a-z0-9_-]/g, '') || 'unknown'
    const protocol = pack?.protocol || 0
    const directory = path.join(this.observationDirectory, String(protocol || 'unknown'))
    fs.mkdirSync(directory, { recursive: true })
    const record = {
      recordId: crypto.randomUUID(), timestamp: Date.now(), type: 'protocol_decode_failure', layer: 'protocol', protocol,
      pack: pack?.name || 'unresolved', backend: safeBackend, direction, packetId,
      packetName: `Unknown packet #${packetId ?? '?'}`, action: 'decode_failed', bytes: rawBuffer.length,
      captureTiers: ['metadata', 'decode_failure'], sensitive,
      decodeError: String(error?.stack || error?.message || error).slice(0, 2048)
    }
    if (!sensitive && ['wire', 'full'].includes(this.captureMode) && this.selectedPacketIds.has(packetId)) {
      const selected = rawBuffer.subarray(0, this.maxPacketBytes)
      record.wire = {
        encoding: 'hex', data: selected.toString('hex'), capturedBytes: selected.length,
        originalBytes: rawBuffer.length, truncated: selected.length !== rawBuffer.length,
        sha256: bufferHash(rawBuffer)
      }
      record.captureTiers.push('wire')
    }
    this.queueObservation(path.join(directory, `${safeBackend}.jsonl`), record)
  }

  observeLosslessPassthrough (pack, backendId, direction, rawBuffer, packetId) {
    if (!this.captureEnabled || !Buffer.isBuffer(rawBuffer)) return
    const safeBackend = String(backendId || 'unknown').toLowerCase().replace(/[^a-z0-9_-]/g, '') || 'unknown'
    const protocol = pack?.protocol || 0
    const directory = path.join(this.observationDirectory, String(protocol || 'unknown'))
    fs.mkdirSync(directory, { recursive: true })
    const packetNames = { 0x34: 'CraftingData', 0xd1: 'VoxelShapes' }
    this.queueObservation(path.join(directory, `${safeBackend}.jsonl`), {
      recordId: crypto.randomUUID(), timestamp: Date.now(), type: 'protocol_passthrough', layer: 'protocol', protocol,
      pack: pack?.name || 'unresolved', backend: safeBackend, direction, packetId,
      packetName: packetNames[packetId] || `Unknown packet #${packetId ?? '?'}`,
      action: 'lossless_passthrough', bytes: rawBuffer.length, captureTiers: ['metadata'], sensitive: false
    })
  }

  queueObservation (file, record) {
    const line = `${JSON.stringify(record)}\n`
    const bytes = Buffer.byteLength(line)
    if (this.pendingObservations.length >= 10000 || this.pendingObservationBytes + bytes > this.maxPendingBytes) {
      this.droppedObservations++
      return
    }
    this.pendingObservations.push({ file, line })
    this.pendingObservationBytes += bytes
  }

  flush () {
    if (this.pendingObservations.length === 0) return
    const pending = this.pendingObservations.splice(0)
    this.pendingObservationBytes = 0
    const grouped = new Map()
    for (const item of pending) grouped.set(item.file, (grouped.get(item.file) || '') + item.line)
    for (const [file, content] of grouped) {
      try {
        if (fs.existsSync(file) && fs.statSync(file).size + Buffer.byteLength(content) >= this.maxObservationBytes) {
          const previous = `${file}.1`
          fs.rmSync(previous, { force: true })
          fs.renameSync(file, previous)
        }
        fs.appendFileSync(file, content, { mode: 0o600 })
      } catch (_) {
        this.droppedObservations += content.split('\n').length - 1
      }
    }
  }

  close () {
    clearInterval(this.flushTimer)
    this.flush()
  }

  summary (protocol) {
    const pack = this.resolve(protocol)
    if (!pack) return { protocol: Number(protocol), supported: false, mode: 'unsupported' }
    const stats = this.stats.get(pack.protocol)
    return {
      protocol: pack.protocol,
      supported: true,
      name: pack.name,
      mode: pack.mode,
      codecVersion: pack.codecVersion,
      minecraftVersions: pack.minecraftVersions,
      baseProtocol: pack.baseProtocol || pack.protocol,
      observedPackets: stats?.observedPackets || 0,
      translatedPackets: stats?.translatedPackets || 0,
      distinctPackets: stats?.packetNames.size || 0,
      lastPacketAt: stats?.lastPacketAt || 0,
      droppedObservations: this.droppedObservations
    }
  }

  catalog () {
    return [...this.packs.keys()].sort((a, b) => a - b).map((protocol) => this.summary(protocol))
  }

  inspection () {
    return {
      enabled: this.captureEnabled,
      mode: this.captureMode,
      selectedPacketIds: [...this.selectedPacketIds].sort((a, b) => a - b),
      maxPacketBytes: this.maxPacketBytes,
      maxObservationBytes: this.maxObservationBytes,
      captureDecodeFailures: this.captureDecodeFailures,
      sensitivePacketIds: [...SENSITIVE_PACKET_IDS].sort((a, b) => a - b),
      pendingRecords: this.pendingObservations.length,
      droppedRecords: this.droppedObservations
    }
  }
}

module.exports = { ProtocolWeave, redactValue, validatePack, parsePacketId, firstMismatch, summarizePacket }
