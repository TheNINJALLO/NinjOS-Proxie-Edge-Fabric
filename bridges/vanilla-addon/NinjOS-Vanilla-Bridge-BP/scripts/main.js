// Copyright (C) 2026 Ninj-OS contributors.
// SPDX-License-Identifier: AGPL-3.0-only

import { world, system, CommandPermissionLevel } from "@minecraft/server"
import { http, HttpHeader, HttpRequest, HttpRequestMethod } from "@minecraft/server-net"
import { bridgeConfig } from "./config.js"

const verified = new Map()

function log(message) {
  console.warn(`[Ninj-OS Vanilla Bridge] ${message}`)
}

function clean(value) {
  return String(value ?? "").trim()
}

function addRoleTags(player, role) {
  for (const tag of player.getTags()) {
    if (tag.startsWith("ninjos.role.")) player.removeTag(tag)
  }
  player.addTag("ninjos.proxy.verified")
  player.addTag(`ninjos.role.${clean(role || "member").toLowerCase().replace(/[^a-z0-9_.-]/g, "_")}`)
}

async function consumeIdentity(player) {
  const body = JSON.stringify({
    serverId: bridgeConfig.serverId,
    username: player.name,
    sessionId: ""
  })
  const request = new HttpRequest(`${bridgeConfig.dashboardUrl.replace(/\/$/, "")}/api/bridge/v1/join/consume`)
  request.method = HttpRequestMethod.Post
  request.timeout = 8
  request.body = body
  request.headers = [
    new HttpHeader("Content-Type", "application/json"),
    new HttpHeader("X-NinjOS-Server", bridgeConfig.serverId),
    new HttpHeader("X-NinjOS-Bridge-Token", bridgeConfig.sharedSecret)
  ]
  return http.request(request)
}

function applyIdentity(player, grant) {
  const operator = Boolean(grant.operator)
  player.commandPermissionLevel = operator ? CommandPermissionLevel.Admin : CommandPermissionLevel.Any
  addRoleTags(player, grant.role)
  player.setDynamicProperty("ninjos:xuid", clean(grant.xuid))
  player.setDynamicProperty("ninjos:session_id", clean(grant.sessionId))
  player.setDynamicProperty("ninjos:role", clean(grant.role || "member"))
  player.setDynamicProperty("ninjos:operator", operator)
  verified.set(player.id, { grant, verifiedAt: Date.now() })
  if (bridgeConfig.showWelcomeMessage) {
    player.sendMessage(`§8[Ninj-OS] §7Identity verified as §f${grant.role || "member"}§7.`)
  }
  // Repeat after initialization so the Bedrock command list receives the final level.
  system.runTimeout(() => {
    if (player.isValid) player.commandPermissionLevel = operator ? CommandPermissionLevel.Admin : CommandPermissionLevel.Any
  }, 2)
}

async function verifyPlayer(player) {
  try {
    const response = await consumeIdentity(player)
    if (response.status !== 200) throw new Error(`dashboard returned HTTP ${response.status}`)
    const grant = JSON.parse(response.body)
    if (!grant || clean(grant.username).toLowerCase() !== player.name.toLowerCase()) {
      throw new Error("identity response did not match the joining player")
    }
    applyIdentity(player, grant)
  } catch (error) {
    log(`Identity verification failed for ${player.name}: ${error}`)
    if (!bridgeConfig.requireProxyIdentity) {
      player.sendMessage("§e[Ninj-OS] Proxy identity was unavailable; vanilla authentication remains authoritative.")
      return
    }
    system.runTimeout(() => {
      try {
        player.runCommand('kick @s This backend requires a verified Ninj-OS Proxie connection.')
      } catch (kickError) {
        log(`Could not remove unverified player ${player.name}: ${kickError}`)
      }
    }, bridgeConfig.rejectDelayTicks)
  }
}

world.afterEvents.playerSpawn.subscribe((event) => {
  if (!event.initialSpawn) return
  system.run(() => verifyPlayer(event.player))
})

world.afterEvents.playerLeave.subscribe((event) => verified.delete(event.playerId))

system.runInterval(() => {
  for (const player of world.getAllPlayers()) {
    const state = verified.get(player.id)
    if (!state) continue
    const operator = Boolean(state.grant.operator)
    if (player.commandPermissionLevel !== (operator ? CommandPermissionLevel.Admin : CommandPermissionLevel.Any)) {
      player.commandPermissionLevel = operator ? CommandPermissionLevel.Admin : CommandPermissionLevel.Any
    }
  }
}, 100)

log(`Loaded for backend ${bridgeConfig.serverId}.`)
