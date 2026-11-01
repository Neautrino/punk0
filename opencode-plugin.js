// punk0's OpenCode v2 plugin: tells the punk0 tab an OpenCode runs in what the
// tab's session is doing, by POSTing punk0's hook body to PUNK0_HOOK_URL.
// Without PUNK0_HOOK_URL it does nothing. It never throws and never writes to
// stdout or stderr: anything written there shows up in the agent's terminal.
//
// A v2 plugin is a directory (a configured file path is skipped), so punk0
// writes this one file as both <dir>/server.js and <dir>/tui.js
// (agentshell.go) and loads it for one process without touching the user's
// config:
//   full-screen TUI:  OPENCODE_CLI_CONFIG_CONTENT='{"plugins":["<dir>"]}',
//                     which a tab's opencode function sets (the TUI half runs
//                     in the tab's own process, so it works with the shared
//                     background service)
//   run / mini:       OPENCODE_CONFIG_CONTENT='{"plugins":["<dir>"]}' plus
//                     --standalone (the server half runs in the private server,
//                     which inherits the tab's environment); punk0 does not
//                     set this up, so `opencode run` in a tab goes unreported
//
// Status: SessionStart binds the tab's session (idle), UserPromptSubmit =
// working, PermissionRequest = waiting (a permission or question is open),
// Stop = idle, SessionEnd = the session is gone or OpenCode exited.

import { execFileSync } from "node:child_process"
import { readFileSync } from "node:fs"

const AGENT = "opencode"
const POST_TIMEOUT_MS = 2000
const TICK_MS = 250
const MAX_DEPTH = 32
const EVENT = { idle: "Stop", working: "UserPromptSubmit", waiting: "PermissionRequest" }

function hookTarget() {
  const raw = process.env.PUNK0_HOOK_URL
  if (!raw) return undefined
  try {
    const url = new URL(raw)
    if (url.protocol !== "http:" && url.protocol !== "https:") return undefined
    // punk0 checks Origin on every write: the scheme and host of its own URL.
    return { href: url.href, origin: url.protocol + "//" + url.host }
  } catch {
    return undefined
  }
}

// Posts in order, one at a time; a body identical to the last one is dropped.
function makeReporter(hook) {
  let last = ""
  let queue = Promise.resolve()
  const post = async (body) => {
    try {
      const res = await fetch(hook.href, {
        method: "POST",
        headers: { "Content-Type": "application/json", Origin: hook.origin },
        body,
        signal: AbortSignal.timeout(POST_TIMEOUT_MS),
      })
      await res.arrayBuffer()
    } catch {}
  }
  return (name, session, title, cwd) => {
    const body = JSON.stringify({
      punk0_agent: AGENT,
      hook_event_name: name,
      session_id: session,
      title: title || "",
      cwd: cwd || "",
    })
    if (body === last) return queue
    last = body
    queue = queue.then(() => post(body)).catch(() => {})
    return queue
  }
}

// The tab's one session and its last reported status. A status is sent only
// when it or the title changes.
function makeBinding(report) {
  let bound = ""
  let status = ""
  let title = ""
  let cwd = ""
  return {
    get bound() {
      return bound
    },
    bind(session, info) {
      if (!session || session === bound) return false
      bound = session
      status = "idle"
      title = info.title
      cwd = info.cwd
      report("SessionStart", bound, title, cwd)
      return true
    },
    set(next, info) {
      if (!bound) return
      const nextTitle = info.title || title
      if (next === status && nextTitle === title) return
      status = next
      title = nextTitle
      report(EVENT[next], bound, title, cwd)
    },
    end() {
      if (!bound) return Promise.resolve()
      const session = bound
      bound = ""
      status = ""
      return report("SessionEnd", session, title, cwd)
    },
  }
}

const quiet = (fn, fallback) => {
  try {
    return fn() ?? fallback
  } catch {
    return fallback
  }
}

// The TUI half runs in the TUI's own process, with the tab's environment. The
// TUI hears every session of the server it is attached to (the shared service
// serves every terminal), so the tab's session is the root of the one this
// TUI shows; its subagents count towards it. It derives the status from the
// TUI's session data, on every event and on a tick (a route change has no
// event).
function setupTui(ctx, hook) {
  const sessions = ctx.data.session
  const router = ctx.ui.router
  if (typeof sessions?.status !== "function" || typeof sessions.get !== "function") return
  const binding = makeBinding(makeReporter(hook))
  const deleted = new Set()
  let disposed = false
  const info = (id) => {
    const session = quiet(() => sessions.get(id))
    return { title: session?.title || "", cwd: session?.location?.directory || process.cwd() }
  }
  const rootOf = (id) => quiet(() => sessions.root(id), id) || id
  const open = (id, list) => (quiet(() => list?.(id), []) || []).length > 0
  const derive = () => {
    if (disposed) return
    const route = quiet(() => router.current())
    if (route?.type === "session" && typeof route.sessionID === "string") {
      const root = rootOf(route.sessionID)
      if (!deleted.has(root)) binding.bind(root, info(root))
    }
    const root = binding.bound
    if (!root) return
    const family = [root, ...(quiet(() => sessions.family(root), []) || [])]
    const running = family.some((id) => quiet(() => sessions.status(id)) === "running")
    const waiting =
      running &&
      family.some((id) => open(id, sessions.permission?.list) || open(id, sessions.form?.list))
    binding.set(waiting ? "waiting" : running ? "working" : "idle", info(root))
  }
  const unlisten = ctx.data.listen((event) => {
    try {
      const details = event?.details
      if (details?.type === "session.deleted") {
        const id = details.data?.sessionID
        if (id) deleted.add(id)
        if (id && id === binding.bound) binding.end()
      }
      derive()
    } catch {}
  })
  const tick = setInterval(() => {
    try {
      derive()
    } catch {}
  }, TICK_MS)
  tick.unref?.()
  derive()
  return async () => {
    disposed = true
    clearInterval(tick)
    try {
      if (typeof unlisten === "function") unlisten()
      await binding.end()
    } catch {}
  }
}

// Value-taking flags that can come before the subcommand on opencode's command line.
const VALUE_FLAGS = new Set(["--server", "--session", "-s", "--prompt", "--log-level", "--completions"])

function parentArgs() {
  const ppid = process.ppid
  if (!ppid || ppid === 1) return []
  try {
    return readFileSync(`/proc/${ppid}/cmdline`, "utf8").split("\0").filter(Boolean)
  } catch {}
  try {
    const out = execFileSync("ps", ["-o", "args=", "-p", String(ppid)], {
      encoding: "utf8",
      timeout: 1000,
      stdio: ["ignore", "pipe", "ignore"],
    })
    return out.trim().split(/\s+/)
  } catch {}
  return []
}

// Whether this server is the private one (`--standalone`) of `opencode run` or
// `opencode mini`, which load no TUI plugins. The shared background service
// (`serve --service`) carries the environment of whichever terminal started
// it, so it must never report; a private server of the full-screen TUI stays
// quiet because that TUI reports through its TUI half.
function servesRunOrMini() {
  if (process.argv.includes("--service")) return false
  const args = parentArgs()
  for (let i = 1; i < args.length; i++) {
    const arg = args[i]
    if (arg === "--") return false
    if (arg.startsWith("-")) {
      if (!arg.includes("=") && VALUE_FLAGS.has(arg)) i++
      continue
    }
    if (arg.includes("/")) continue // a launcher script's path
    return arg === "run" || arg === "mini"
  }
  return false
}

// The server half follows the server's event stream. Plugins start with the
// first request for a directory, which can be the one creating the session,
// so a session can be first heard of mid-turn: its parent and title are
// looked up then.
function setupServer(ctx, hook) {
  if (!servesRunOrMini()) return
  const binding = makeBinding(makeReporter(hook))
  const controller = new AbortController()
  const parents = new Map()
  const titles = new Map()
  const pending = new Set()
  let running = false
  const cwd = ctx.location?.directory || process.cwd()
  const info = (id) => ({ title: titles.get(id) || "", cwd })
  const parentOf = async (id) => {
    if (parents.has(id)) return parents.get(id)
    let parent = ""
    try {
      const session = await ctx.session.get({ sessionID: id })
      const record = session?.data ?? session
      parent = record?.parentID || ""
      if (record?.title && !titles.has(id)) titles.set(id, record.title)
    } catch {}
    parents.set(id, parent)
    return parent
  }
  const rootOf = async (id) => {
    let current = id
    for (let depth = 0; depth < MAX_DEPTH; depth++) {
      const parent = await parentOf(current)
      if (!parent) return current
      current = parent
    }
    return current
  }
  const update = () => binding.set(pending.size ? "waiting" : running ? "working" : "idle", info(binding.bound))
  // The root a session belongs to, bound as the tab's session.
  const take = async (id) => {
    if (!id) return ""
    const root = await rootOf(id)
    if (binding.bind(root, info(root))) {
      running = false
      pending.clear()
    }
    return root
  }
  const handle = async (event) => {
    const data = event?.data || {}
    switch (event?.type) {
      case "session.created":
        parents.set(data.sessionID, data.parentID || "")
        if (data.title) titles.set(data.sessionID, data.title)
        if (!data.parentID) await take(data.sessionID)
        return
      case "session.renamed":
        if (data.sessionID && data.title) titles.set(data.sessionID, data.title)
        if (data.sessionID === binding.bound) update()
        return
      case "session.execution.started":
      case "session.execution.succeeded":
      case "session.execution.failed":
      case "session.execution.interrupted": {
        // On shutdown the turn resumes with the next server; dispose ends the binding.
        if (event.type === "session.execution.interrupted" && data.reason === "shutdown") return
        // A subagent's turn says nothing about the tab's.
        if (!data.sessionID || (await rootOf(data.sessionID)) !== data.sessionID) return
        await take(data.sessionID)
        running = event.type === "session.execution.started"
        if (!running) pending.clear()
        update()
        return
      }
      case "permission.asked":
      case "form.created": {
        const request = event.type === "form.created" ? data.form : data
        if (!request?.id || !request.sessionID) return
        if (!(await take(request.sessionID))) return
        pending.add(request.id)
        running = true
        update()
        return
      }
      case "permission.replied":
      case "form.replied":
      case "form.cancelled": {
        const id = event.type === "permission.replied" ? data.requestID : data.id
        if (pending.delete(id)) update()
        return
      }
      case "session.deleted":
        if (data.sessionID && data.sessionID === binding.bound) {
          running = false
          pending.clear()
          await binding.end()
        }
        return
    }
  }
  ;(async () => {
    try {
      for await (const event of ctx.event.subscribe({ signal: controller.signal })) {
        if (controller.signal.aborted) break
        try {
          await handle(event)
        } catch {}
      }
    } catch {}
  })()
  return async () => {
    controller.abort()
    try {
      await binding.end()
    } catch {}
  }
}

export default {
  id: "punk0",
  setup(ctx) {
    try {
      const hook = hookTarget()
      if (!hook || !ctx) return
      if (typeof ctx.data?.listen === "function" && typeof ctx.ui?.router?.current === "function")
        return setupTui(ctx, hook)
      if (typeof ctx.event?.subscribe === "function") return setupServer(ctx, hook)
    } catch {}
  },
}
