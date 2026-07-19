#!/usr/bin/env python3
from __future__ import annotations

import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
APP = ROOT / "dashboard" / "public" / "app.js"

HARNESS = r'''
const fs = require("fs");
const vm = require("vm");

const appPath = process.argv[1];
const source = fs.readFileSync(appPath, "utf8");
const elements = new Map();
const intervalCalls = [];
const clearedIntervals = [];
const timeoutCalls = [];
const storage = new Map();
let fetchImpl = async (path) => {
  if (path === "/api/setup/status") {
    return { status: 200, ok: true, json: async () => ({ setupRequired: false }) };
  }
  throw new Error("unexpected fetch");
};

function element(selector) {
  if (elements.has(selector)) return elements.get(selector);
  const node = {
    selector,
    value: "",
    textContent: "",
    innerHTML: "",
    className: "",
    open: false,
    disabled: false,
    checked: false,
    style: {},
    dataset: {},
    events: {},
    classList: {
      add() {}, remove() {}, toggle() {}, contains() { return false; },
    },
    addEventListener(name, callback) { this.events[name] = callback; },
    showModal() { this.open = true; },
    close() { this.open = false; },
    focus() { this.focused = true; },
    getContext() {
      return {
        clearRect() {}, beginPath() {}, moveTo() {}, lineTo() {}, stroke() {},
        fillText() {}, arc() {}, fill() {}, save() {}, restore() {},
        set lineWidth(_) {}, set strokeStyle(_) {}, set fillStyle(_) {},
        set font(_) {}, set textAlign(_) {},
      };
    },
  };
  elements.set(selector, node);
  return node;
}

const context = {
  console,
  JSON,
  Math,
  Number,
  String,
  Date,
  Promise,
  URLSearchParams,
  document: {
    querySelector: element,
    querySelectorAll: () => [],
  },
  window: {
    addEventListener() {},
    prompt() {},
    location: { hostname: "localhost" },
    devicePixelRatio: 1,
  },
  navigator: { clipboard: { writeText: async () => {} } },
  sessionStorage: {
    getItem(key) { return storage.get(key) || null; },
    setItem(key, value) { storage.set(key, String(value)); },
    removeItem(key) { storage.delete(key); },
  },
  fetch: (...args) => fetchImpl(...args),
  setInterval(callback, delay) {
    const id = intervalCalls.length + 1;
    intervalCalls.push({ id, callback, delay });
    return id;
  },
  clearInterval(id) { clearedIntervals.push(id); },
  setTimeout(callback, delay) {
    timeoutCalls.push({ callback, delay });
    return timeoutCalls.length;
  },
  clearTimeout() {},
};
context.globalThis = context;
vm.createContext(context);
vm.runInContext(source, context, { filename: appPath });

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

(async () => {
  await new Promise((resolve) => setImmediate(resolve));
  const token = element("#tokenInput");
  const totp = element("#totpInput");
  const dialog = element("#tokenDialog");

  assert(dialog.open, "login dialog should open when no session exists");
  assert(intervalCalls.length === 0, "polling must not start before login");

  token.value = "typing-token";
  totp.value = "123456";
  vm.runInContext("openTokenDialog({ reset: false, focus: false })", context);
  assert(token.value === "typing-token", "opening an existing login dialog erased the token");
  assert(totp.value === "123456", "opening an existing login dialog erased the TOTP code");

  vm.runInContext('state.token = "expired-session"; state.pollTimers = [44, 45]', context);
  fetchImpl = async () => ({ status: 401, ok: false });
  await vm.runInContext('api("/api/state").catch(() => null)', context);
  assert(token.value === "typing-token", "401 handling erased the token being typed");
  assert(totp.value === "123456", "401 handling erased the TOTP code being typed");
  assert(vm.runInContext("state.token", context) === "", "expired session was not cleared");
  assert(vm.runInContext("state.pollTimers.length", context) === 0, "polling did not stop after 401");
  assert(clearedIntervals.includes(44) && clearedIntervals.includes(45), "active poll timers were not cancelled");

  token.value = "valid-dashboard-token";
  totp.value = "654321";
  element("#usernameInput").value = "owner";
  fetchImpl = async (path, options) => {
    assert(path === "/api/login", "unexpected login endpoint");
    const body = JSON.parse(options.body);
    assert(body.password === "valid-dashboard-token", "submitted password changed before login request");
    assert(body.totp === "654321", "submitted TOTP changed before login request");
    return {
      status: 200,
      ok: true,
      json: async () => ({ token: "browser-session", principal: { username: "owner" } }),
    };
  };
  const submit = element("#loginForm").events.submit;
  assert(typeof submit === "function", "login submit handler was not registered");
  await submit({ preventDefault() {} });

  assert(storage.get("ninjos_dashboard_token") === "browser-session", "browser session was not saved");
  assert(vm.runInContext("state.pollTimers.length", context) === 7, "polling did not start after login");
  assert(token.value === "" && totp.value === "", "credentials were not cleared after successful login");
  assert(!dialog.open, "login dialog did not close after successful login");

  assert(typeof element("#setupForm").events.submit === "function", "setup submit handler was not registered");

  console.log("dashboard-login-input: PASS");
})().catch((error) => {
  console.error(error.stack || error.message || error);
  process.exit(1);
});
'''

result = subprocess.run(
    ["node", "-e", HARNESS, str(APP)],
    check=True,
    text=True,
    capture_output=True,
)
print(result.stdout.strip())
