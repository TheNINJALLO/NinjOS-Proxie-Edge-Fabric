'use strict'

// Applies the narrowly scoped Bedrock 1.26.33 compatibility fixes that are
// not yet present in the pinned upstream packages. Every edit is idempotent
// and fails closed when an upstream file no longer matches the reviewed form.

const fs = require('node:fs')
const path = require('node:path')

function packageFile (packageName, relativePath) {
  const manifest = require.resolve(`${packageName}/package.json`)
  return path.join(path.dirname(manifest), relativePath)
}

function replaceOnce (file, before, after, marker) {
  let content = fs.readFileSync(file, 'utf8')
  if (content.includes(marker)) return
  if (!content.includes(before)) throw new Error(`Compatibility patch no longer matches ${file}`)
  content = content.replace(before, after)
  fs.writeFileSync(file, content)
}

const login = packageFile('bedrock-protocol', 'src/handshake/login.js')
replaceOnce(
  login,
  "        }, privateKey, { algorithm, notBefore: 0, issuer: 'self', expiresIn: 60 * 60, header: { x5u: client.clientX509, typ: undefined } })",
  `        }, privateKey, {
          algorithm,
          notBefore: 0,
          issuer: 'self',
          expiresIn: 60 * 60,
          audience: 'api://auth-minecraft-services/multiplayer',
          header: { x5u: client.clientX509, typ: undefined }
        })`,
  "audience: 'api://auth-minecraft-services/multiplayer'"
)

const loginVerify = packageFile('bedrock-protocol', 'src/handshake/loginVerify.js')
replaceOnce(
  loginVerify,
  "const crypto = require('crypto')",
  `const crypto = require('crypto')
const https = require('https')

const OIDC_ISSUER = 'https://authorization.franchise.minecraft-services.net/'
const OIDC_AUDIENCE = 'api://auth-minecraft-services/multiplayer'
const OIDC_JWKS_URL = 'https://authorization.franchise.minecraft-services.net/.well-known/keys'
const oidcKeys = new Map()

function refreshOidcKeys () {
  https.get(OIDC_JWKS_URL, {
    headers: { Accept: 'application/json', 'User-Agent': 'NinjOS-Session-Core/7.3.14' },
    timeout: 5000
  }, (response) => {
    let body = ''
    response.setEncoding('utf8')
    response.on('data', (chunk) => { body += chunk })
    response.on('end', () => {
      try {
        if (response.statusCode !== 200) throw new Error(\`HTTP \${response.statusCode}\`)
        const parsed = JSON.parse(body)
        const next = new Map()
        for (const jwk of parsed.keys || []) {
          if (jwk.kid && jwk.kty === 'RSA') next.set(jwk.kid, jwk)
        }
        if (next.size === 0) throw new Error('JWKS response contained no RSA signing keys')
        oidcKeys.clear()
        for (const [kid, jwk] of next) oidcKeys.set(kid, jwk)
        console.log(\`[Ninj-OS Session Core] Loaded \${oidcKeys.size} Minecraft OIDC signing keys\`)
      } catch (error) {
        console.warn('[Ninj-OS Session Core] Minecraft OIDC key refresh failed:', error.message)
      }
    })
  }).on('error', (error) => {
    console.warn('[Ninj-OS Session Core] Minecraft OIDC key refresh failed:', error.message)
  })
}

refreshOidcKeys()
const oidcRefreshTimer = setInterval(refreshOidcKeys, 30 * 60 * 1000)
oidcRefreshTimer.unref()`,
  'const OIDC_JWKS_URL'
)
replaceOnce(
  loginVerify,
  `    const normalized = normalizeToken(token)
    const x5u = getX5U(normalized)
    const decoded = JWT.verify(normalized, getDER(x5u), { algorithms: ['ES384', 'RS256'] })`,
  `    const normalized = normalizeToken(token)
    const complete = JWT.decode(normalized, { complete: true })
    const header = complete?.header || {}
    const unverified = complete?.payload || {}
    const algorithm = header.alg
    const allowedAlgorithms = ['ES256', 'ES384', 'ES512', 'RS256', 'RS384', 'RS512']
    if (!allowedAlgorithms.includes(algorithm)) {
      throw new Error(\`Unsupported OIDC login algorithm \${algorithm || 'missing'}\`)
    }

    const x5u = getX5U(normalized)
    let verificationKey
    if (header.jwk) {
      verificationKey = crypto.createPublicKey({ key: header.jwk, format: 'jwk' })
    } else if (x5u) {
      verificationKey = getDER(x5u)
    } else if (algorithm.startsWith('RS') && header.kid && oidcKeys.has(header.kid)) {
      verificationKey = crypto.createPublicKey({ key: oidcKeys.get(header.kid), format: 'jwk' })
    } else if (algorithm.startsWith('ES')) {
      const clientKey = unverified.cpk || unverified.clientPublicKey
      if (clientKey) verificationKey = getDER(clientKey)
    }
    if (!verificationKey) {
      throw new Error(
        \`OIDC login token has alg=\${algorithm} kid=\${header.kid || 'missing'} but no trusted verification key; \` +
        \`cachedKeys=\${oidcKeys.size}\`
      )
    }
    const verifyOptions = algorithm.startsWith('RS')
      ? { algorithms: [algorithm], issuer: OIDC_ISSUER, audience: OIDC_AUDIENCE }
      : { algorithms: [algorithm] }
    const decoded = JWT.verify(normalized, verificationKey, verifyOptions)`,
  'const allowedAlgorithms'
)

const serverPlayer = packageFile('bedrock-protocol', 'src/serverPlayer.js')
replaceOnce(
  serverPlayer,
  `    } catch (e) {
      debug(this.address, e)`,
  `    } catch (e) {
      console.error(
        '[Ninj-OS Session Core] Downstream authentication exception:',
        e?.stack || e?.message || String(e)
      )
      debug(this.address, e)`,
  'Downstream authentication exception:'
)

// Relay terminates encryption at both sides, but native gameplay packets do not
// need to be decoded and serialized again. Preserve their original decrypted
// bytes unless an explicit Protocol Weave translator changed the decoded fields.
// This avoids corrupting hotfix packet layouts that share protocol 1001 while
// the upstream schema package catches up.
const relay = packageFile('bedrock-protocol', 'src/relay.js')
replaceOnce(
  relay,
  `    for (const packet of this.downQ) {
      const des = this.server.deserializer.parsePacketBuffer(packet)
      this.write(des.data.name, des.data.params)
    }`,
  `    for (const packet of this.downQ) {
      this.sendBuffer(packet)
    }`,
  'this.sendBuffer(packet) // Ninj-OS lossless downstream queue'
)
replaceOnce(
  relay,
  '      this.sendBuffer(packet)',
  '      this.sendBuffer(packet) // Ninj-OS lossless downstream queue',
  'this.sendBuffer(packet) // Ninj-OS lossless downstream queue'
)
replaceOnce(
  relay,
  `      const des = this.server.deserializer.parsePacketBuffer(e)
      if (des.data.name === 'client_cache_status') {
        // Currently not working, force off the chunk cache
      } else {
        this.upstream.write(des.data.name, des.data.params)
      }`,
  `      const des = this.server.deserializer.parsePacketBuffer(e)
      if (des.data.name === 'client_cache_status') {
        // Currently not working, force off the chunk cache
      } else {
        this.upstream.sendBuffer(e)
      }`,
  'this.upstream.sendBuffer(e)'
)
replaceOnce(
  relay,
  `      const des = this.server.deserializer.parsePacketBuffer(e)
      if (des.data.name === 'client_cache_status') {
        // Currently not working, force off the chunk cache
      } else {
        this.upstream.sendBuffer(e)
      }`,
  `      // Preserve packets collected during the short upstream handshake.
      this.upstream.sendBuffer(e) // Ninj-OS lossless upstream queue`,
  'this.upstream.sendBuffer(e) // Ninj-OS lossless upstream queue'
)
replaceOnce(
  relay,
  `    } catch (e) {
      this.server.deserializer.dumpFailedBuffer(packet, this.connection.address)
      console.error(this.connection.address, e)

      if (!this.options.omitParseErrors) {
        this.disconnect('Server packet parse error')
      }

      return
    }`,
  `    } catch (e) {
      this.server.deserializer.dumpFailedBuffer(packet, this.connection.address)
      console.error(this.connection.address, e)
      // Inspection is best-effort. A schema lag must not break gameplay.
      this.sendBuffer(packet)
      return
    }`,
  'A schema lag must not break gameplay.'
)
replaceOnce(
  relay,
  `    let des
    try {
      des = this.server.deserializer.parsePacketBuffer(packet)`,
  `    // CraftingData and VoxelShapes are large, schema-volatile login packets.
    // Reading them with a lagging schema can block the event loop or misread a
    // recipe length. Inspect only their envelope and preserve the wire payload.
    let header = 0
    let shift = 0
    let packetId = null
    for (let index = 0; index < Math.min(packet.length, 5); index++) {
      const value = packet[index]
      header |= (value & 0x7f) << shift
      if ((value & 0x80) === 0) {
        packetId = header & 0x3ff
        break
      }
      shift += 7
    }
    if (packetId === 0x34 || packetId === 0xd1) {
      this.emit('clientbound_raw', { packetId, packet })
      this.sendBuffer(packet)
      return
    }

    let des
    try {
      des = this.server.deserializer.parsePacketBuffer(packet)`,
  "this.emit('clientbound_raw', { packetId, packet })"
)
replaceOnce(
  relay,
  `      if (!this.upstream) {
        const des = this.server.deserializer.parsePacketBuffer(packet)
        this.downInLog('Got downstream connected packet but upstream is not connected yet, added to q', des)
        this.upQ.push(packet) // Put into a queue
        return
      }`,
  `      if (!this.upstream) {
        this.downInLog('Got downstream connected packet but upstream is not connected yet, added raw packet to q')
        this.upQ.push(packet) // Put into a queue
        return
      }`,
  'added raw packet to q'
)
replaceOnce(
  relay,
  `      // TODO: If we fail to parse a packet, proxy it raw and log an error
      const des = this.server.deserializer.parsePacketBuffer(packet)`,
  `      let des
      try {
        des = this.server.deserializer.parsePacketBuffer(packet)
      } catch (error) {
        // The decoder wrapper already recorded the failure for Protocol Weave.
        // Forward the original bytes so schema lag remains non-disruptive.
        this.upstream.sendBuffer(packet)
        return
      }`,
  'schema lag remains non-disruptive.'
)
replaceOnce(
  relay,
  `      this.queue(name, params)
    }

    if (this.chunkSendCache.length > 0 && this.sentStartGame) {
      for (const entry of this.chunkSendCache) {
        this.queue('level_chunk', entry)
      }`,
  `      if (des.ninjosReencode) this.queue(name, params)
      else this.sendBuffer(packet)
    }

    if (this.chunkSendCache.length > 0 && this.sentStartGame) {
      for (const entry of this.chunkSendCache) {
        if (entry.reencode) this.queue('level_chunk', entry.params)
        else this.sendBuffer(entry.packet)
      }`,
  'if (des.ninjosReencode) this.queue(name, params)'
)
replaceOnce(
  relay,
  '        this.chunkSendCache.push(params)',
  '        this.chunkSendCache.push({ packet, params, reencode: des.ninjosReencode === true })',
  'reencode: des.ninjosReencode === true'
)
replaceOnce(
  relay,
  `          // Emit the packet as-is back to the upstream server
          this.downInLog('Relaying', des.data)
          this.upstream.queue(des.data.name, des.data.params)`,
  `          // Preserve the original decrypted bytes unless an explicit
          // translator changed the decoded packet.
          this.downInLog('Relaying', des.data)
          if (des.ninjosReencode) this.upstream.queue(des.data.name, des.data.params)
          else this.upstream.sendBuffer(packet)`,
  'if (des.ninjosReencode) this.upstream.queue'
)

const protocol = packageFile('minecraft-data', 'minecraft-data/data/bedrock/1.26.30/protocol.json')
const protocolData = JSON.parse(fs.readFileSync(protocol, 'utf8'))
const voxelCells = protocolData.types?.VoxelShape?.[1]?.find((field) => field.name === 'cells')
if (!voxelCells) throw new Error('VoxelShape.cells is missing from the 1.26.30 protocol schema')
voxelCells.type = 'VoxelCells'
fs.writeFileSync(protocol, `${JSON.stringify(protocolData, null, 2)}\n`)

const raknetBinding = packageFile('raknet-native', 'binding.js')
replaceOnce(
  raknetBinding,
  'const pathsToSearch = [helper.getPath()]',
  'const pathsToSearch = [helper.getPath(), helper.getFallbackPath()].filter(Boolean)',
  'helper.getFallbackPath()].filter(Boolean)'
)

console.log('[Ninj-OS Session Core] Bedrock 1.26.33 compatibility patches verified')
