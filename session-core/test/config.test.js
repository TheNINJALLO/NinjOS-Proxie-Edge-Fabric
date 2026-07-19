'use strict'
const assert = require('node:assert')
const fs = require('node:fs')
const path = require('node:path')
const packageJson = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'package.json'), 'utf8'))
assert.strictEqual(packageJson.dependencies['bedrock-protocol'], '3.56.1')
assert.ok(Number(process.versions.node.split('.')[0]) >= 22)
console.log('session-core package metadata validated')
