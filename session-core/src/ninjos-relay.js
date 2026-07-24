// Copyright (C) 2026 Ninj-OS contributors.
// SPDX-License-Identifier: AGPL-3.0-only
//
// The forwarding implementation builds on the MIT-licensed Relay API from
// PrismarineJS bedrock-protocol. Ninj-OS supplies the routing, identity,
// command, fallback, and control-plane integration around that transport.

const { Relay } = require('bedrock-protocol/src/relay')
const { Client } = require('bedrock-protocol/src/client')

class ForwardedIdentityClient extends Client {
  constructor (options, forwardedProfile) {
    super(options)
    this.forwardedProfile = {
      name: forwardedProfile.name,
      uuid: forwardedProfile.uuid,
      xuid: String(forwardedProfile.xuid || '0')
    }
  }

  connect () {
    if (!this.connection) throw new Error('Connect not currently allowed')
    this.on('session', this._connect)
    if (this.options.offline) {
      this.profile = this.forwardedProfile
      this.username = this.forwardedProfile.name
      this.accessToken = []
      this.multiplayerToken = ''
      this.emit('session', this.forwardedProfile)
    } else {
      super.connect()
      return
    }
    this.startQueue()
  }
}

class NinjOSRelay extends Relay {
  constructor (options, hooks) {
    super(options)
    this.hooks = hooks
  }

  async openUpstreamConnection (downstream, clientAddr) {
    const destination = this.options.destination
    // Current OIDC identity tokens may omit the legacy profile UUID. The
    // signed client-data JWT still carries SelfSignedId, which is the stable
    // per-login UUID Bedrock uses for the upstream player identity.
    const forwardedProfile = {
      ...downstream.profile,
      uuid: downstream.profile?.uuid || downstream.skinData?.SelfSignedId
    }
    const options = {
      offline: true,
      username: downstream.profile.name,
      version: this.options.version,
      host: destination.host,
      port: destination.port,
      batchingInterval: this.options.batchingInterval,
      connectTimeout: destination.connectTimeout || 9000,
      profilesFolder: this.options.profilesFolder,
      raknetBackend: this.options.raknetBackend || 'jsp-raknet',
      autoInitPlayer: false,
      conLog: null
    }

    const client = new ForwardedIdentityClient(options, forwardedProfile)
    client.options.skinData = {
      ...downstream.skinData,
      NinjOSProxyIdentityVersion: 1,
      NinjOSProxySessionId: String(downstream.__ninjosSessionId || ''),
      NinjOSProxyXuid: String(downstream.profile?.xuid || ''),
      NinjOSProxyUuid: String(forwardedProfile.uuid || ''),
      NinjOSProxyName: String(downstream.profile?.name || '')
    }
    client.ping().then(() => client.connect()).catch((error) => {
      this.hooks.onBackendError?.(downstream, error)
      this.hooks.fallbackOrDisconnect?.(downstream, `Unable to reach ${this.options.backendId}`)
    })

    client.outLog = downstream.upOutLog
    client.inLog = downstream.upInLog
    client.once('join', () => {
      client.write('client_cache_status', { enabled: this.enableChunkCaching })
      downstream.upstream = client
      downstream.flushUpQueue()
      client.readPacket = (packet) => downstream.readUpstream(packet)
      this.emit('join', downstream, client)
      this.hooks.onBackendJoin?.(downstream, client)
    })
    client.on('error', (error) => {
      this.hooks.onBackendError?.(downstream, error)
      this.hooks.fallbackOrDisconnect?.(downstream, `Backend error: ${error.message}`)
      this.upstreams.delete(clientAddr.hash)
    })
    client.on('close', (reason) => {
      this.hooks.onBackendClose?.(downstream, reason)
      this.hooks.fallbackOrDisconnect?.(downstream, 'Backend server closed the connection')
      this.upstreams.delete(clientAddr.hash)
    })
    this.upstreams.set(clientAddr.hash, client)
  }
}

module.exports = { NinjOSRelay }
