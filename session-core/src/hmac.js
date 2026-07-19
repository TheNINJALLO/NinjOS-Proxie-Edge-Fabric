// Copyright (C) 2026 Ninj-OS contributors.
// SPDX-License-Identifier: AGPL-3.0-only

const crypto = require('node:crypto')

function signedHeaders (serverId, secret, body) {
  const timestamp = String(Date.now())
  const signature = crypto
    .createHmac('sha256', secret)
    .update(timestamp + '\n')
    .update(body)
    .digest('hex')
  return {
    'content-type': 'application/json',
    'x-ninjos-server': serverId,
    'x-ninjos-timestamp': timestamp,
    'x-ninjos-signature': signature
  }
}

module.exports = { signedHeaders }
