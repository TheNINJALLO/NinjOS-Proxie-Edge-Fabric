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
    headers: { Accept: 'application/json', 'User-Agent': 'NinjOS-Session-Core/7.3.7' },
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
