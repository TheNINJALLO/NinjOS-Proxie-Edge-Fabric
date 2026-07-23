'use strict'

const assert = require('node:assert')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')
const { ProtocolWeave, redactValue, validatePack, parsePacketId, firstMismatch, summarizePacket } = require('../src/protocol-weave')

const root = fs.mkdtempSync(path.join(os.tmpdir(), 'ninjos-protocol-weave-'))
const packs = path.join(root, 'packs')
const observations = path.join(root, 'observations')
fs.mkdirSync(path.join(packs, '1001'), { recursive: true })
fs.mkdirSync(path.join(packs, '1005'), { recursive: true })
fs.writeFileSync(path.join(packs, '1001', 'pack.json'), JSON.stringify({
  schemaVersion: 1,
  name: 'Baseline',
  protocol: 1001,
  minecraftVersions: ['1.26.33'],
  codecVersion: '1.26.30',
  mode: 'native',
  translators: []
}))
fs.writeFileSync(path.join(packs, '1005', 'pack.json'), JSON.stringify({
  schemaVersion: 1,
  name: 'Controlled future fixture',
  protocol: 1005,
  minecraftVersions: ['1.26.40-test'],
  codecVersion: '1.26.30',
  baseProtocol: 1001,
  wireCompatibleWith: 1001,
  mode: 'translated',
  translators: [{
    direction: 'serverbound',
    packet: 'fixture_packet',
    operations: [
      { op: 'rename_field', from: 'future_name', to: 'known_name' },
      { op: 'add_default', field: 'enabled', value: true },
      { op: 'drop_field', field: 'future_only' }
    ]
  }]
}))

const weave = new ProtocolWeave({
  packDirectory: packs,
  observationDirectory: observations,
  captureMode: 'full',
  selectedPacketIds: '5',
  maxPacketBytes: 64
})
assert.strictEqual(weave.catalog().length, 2)
assert.strictEqual(weave.accepts(1001, 1001), true)
assert.strictEqual(weave.accepts(1005, 1001), true)
assert.strictEqual(weave.accepts(1006, 1001), false)

const packet = { future_name: 'hello', future_only: 9, authToken: 'private', nested: { deviceId: 'private' } }
const raw = Buffer.from([5, 1, 2])
assert.strictEqual(weave.process(weave.resolve(1005), 'Zoo Server', 'serverbound', 'fixture_packet', packet, {
  rawBuffer: raw,
  serialize: () => Buffer.from(raw)
}), true)
assert.deepStrictEqual(packet, { known_name: 'hello', authToken: 'private', nested: { deviceId: 'private' }, enabled: true })
weave.process(weave.resolve(1005), 'Zoo Server', 'serverbound', 'login', { token: 'private' }, { rawBuffer: Buffer.from([1, 9]) })
weave.observeDecodeFailure(weave.resolve(1005), 'Zoo Server', 'clientbound', Buffer.from([6, 10, 11]), new Error('fixture decode failed'))
weave.flush()
const records = fs.readFileSync(path.join(observations, '1005', 'zooserver.jsonl'), 'utf8').trim().split('\n').map(JSON.parse)
const record = records.find((item) => item.packetName === 'fixture_packet')
assert.strictEqual(record.decoded.authToken, '[REDACTED]')
assert.strictEqual(record.decoded.nested.deviceId, '[REDACTED]')
assert.strictEqual(record.wire.data, raw.toString('hex'))
assert.strictEqual(record.roundTrip.exact, true)
assert.strictEqual(records.find((item) => item.packetName === 'login').wire, undefined)
assert.strictEqual(records.find((item) => item.packetName === 'login').decoded, undefined)
assert.strictEqual(records.find((item) => item.type === 'protocol_decode_failure').decodeError.includes('fixture decode failed'), true)
assert.strictEqual(weave.summary(1005).translatedPackets, 1)
assert.strictEqual(redactValue({ shared_secret: 'x' }).shared_secret, '[REDACTED]')
assert.strictEqual(redactValue({ payload: Buffer.alloc(20) }).payload, '[BINARY:20 bytes]')
assert.throws(() => validatePack({ protocol: 5, mode: 'magic', minecraftVersions: ['x'], codecVersion: 'x' }, 'fixture'))
assert.strictEqual(parsePacketId(Buffer.from([0x85, 0x08])), 5)
assert.strictEqual(firstMismatch(Buffer.from([1, 2]), Buffer.from([1, 3])), 1)
assert.deepStrictEqual(summarizePacket('player_auth_input', {
  tick: 42n,
  input_data: { block_action: true, jumping: false, item_interact: true },
  block_action: [{ action: 'start_break' }, { action: 'continue_break' }],
  transaction: { type: 'use_item' }
}), {
  category: 'player_input', clientTick: '42', inputFlags: ['block_action', 'item_interact'],
  blockActionCount: 2, blockActions: ['start_break', 'continue_break'],
  hasItemInteraction: true, hasItemStackRequest: false
})
assert.deepStrictEqual(summarizePacket('command_request', {
  command: '/example secret-argument', origin: { type: 'player' }, internal: false
}, { proxyIntercepted: false }), {
  category: 'command', commandName: 'example', originType: 'player',
  internal: false, proxyIntercepted: false
})

const fallbackWeave = new ProtocolWeave({
  packDirectory: path.join(root, 'missing-packs'),
  observationDirectory: path.join(root, 'fallback-observations')
})
assert.strictEqual(fallbackWeave.accepts(1001, 1001), true)
assert.strictEqual(fallbackWeave.summary(1001).supported, true)
fallbackWeave.close()

weave.close()
fs.rmSync(root, { recursive: true, force: true })
console.log('protocol-weave: PASS')
