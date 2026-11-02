// punk0's Pi extension: tells the punk0 tab a Pi runs in what the tab's chat
// is doing, by POSTing punk0's hook body to PUNK0_HOOK_URL. Without
// PUNK0_HOOK_URL it does nothing. It never throws and never writes to stdout
// or stderr: anything written there shows up in the agent's terminal.
//
// punk0 writes it to its own state folder and loads it for one run with
// `pi -e <file>` (agentshell.go); the user's own extensions load beside it
// and nothing of the user's is edited.
//
// Status: session_start = SessionStart (binds the tab's chat, idle),
// before_agent_start = UserPromptSubmit (working), agent_settled = Stop
// (idle; agent_end fires once per provider retry), session_shutdown =
// SessionEnd. Pi has no permission prompt, so it never needs you. The chat's
// title is the name the user gives it (/name, --name): every body carries
// it, and a /name re-sends the last status with the new one.

const AGENT = "pi"
const POST_TIMEOUT_MS = 1000

type Session = {
  getSessionId?: () => string
  getSessionFile?: () => string | undefined
  getSessionName?: () => string | undefined
}
type Ctx = { sessionManager?: Session; cwd?: string }
type Handler = (event: unknown, ctx: Ctx) => unknown
type Pi = { on: (name: string, handler: Handler) => void }

// A body as punk0's hook reads it (hook.go hookEvent).
type Body = {
  hook_event_name: string
  punk0_agent: string
  session_id: string
  transcript_path: string
  title: string
  cwd: string
  prompt?: string
}

type Hook = { href: string; origin: string }

type Reporter = {
  gen: number // the factory run whose handlers report
  sent: string // the last body posted
  queue: Promise<void> // the posts, in order
  status?: Body // the chat's last status, re-sent when its title changes
}

function hookTarget(): Hook | undefined {
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

const quiet = <T>(fn: () => T | undefined, fallback: T): T => {
  try {
    return fn() ?? fallback
  } catch {
    return fallback
  }
}

// field is event[key] when the event has it as a string.
function field(event: unknown, key: string): string | undefined {
  if (event && typeof event === "object" && key in event) {
    const v = (event as Record<string, unknown>)[key]
    return typeof v === "string" ? v : undefined
  }
  return undefined
}

// The process's one reporter. Pi runs the factory again for every chat it
// switches to (/new, /resume), so the reporter lives on globalThis: posts
// stay in order across the switch, and only the newest run's handlers report
// whether or not Pi dropped the earlier ones.
function reporter(): Reporter {
  const g = globalThis as Record<symbol, Reporter | undefined>
  const key = Symbol.for("punk0.pi.reporter")
  return (g[key] ??= { gen: 0, sent: "", queue: Promise.resolve() })
}

// Posts in order, one at a time; a body identical to the last one is dropped
// on purpose: it says nothing new (a title change makes the body differ).
function post(r: Reporter, hook: Hook, body: Body): Promise<void> {
  const text = JSON.stringify(body)
  if (text === r.sent) return r.queue
  r.sent = text
  r.queue = r.queue.then(async () => {
    try {
      const res = await fetch(hook.href, {
        method: "POST",
        headers: { "Content-Type": "application/json", Origin: hook.origin },
        body: text,
        signal: AbortSignal.timeout(POST_TIMEOUT_MS),
      })
      await res.arrayBuffer()
    } catch {}
  })
  return r.queue
}

// Pi awaits a handler, so the one that waits for its post waits no longer
// than a post may take.
function bounded(p: Promise<void>): Promise<void> {
  const { promise, resolve } = Promise.withResolvers<void>()
  setTimeout(resolve, POST_TIMEOUT_MS).unref?.()
  return Promise.race([p, promise])
}

export default function (pi: Pi): void {
  const hook = hookTarget()
  if (!hook) return
  const r = reporter()
  const gen = ++r.gen
  const body = (name: string, ctx: Ctx): Body => {
    const s = ctx?.sessionManager
    return {
      hook_event_name: name,
      punk0_agent: AGENT,
      session_id: quiet(() => s?.getSessionId?.(), ""),
      transcript_path: quiet(() => s?.getSessionFile?.(), ""),
      title: quiet(() => s?.getSessionName?.(), ""),
      cwd: ctx?.cwd || process.cwd(),
    }
  }
  const status = (name: string, ctx: Ctx, prompt?: string) => {
    const b = body(name, ctx)
    if (prompt !== undefined) b.prompt = prompt
    r.status = b
    void post(r, hook, b)
  }
  const on = (name: string, handler: Handler) =>
    pi.on(name, (event, ctx) => {
      if (gen !== r.gen) return
      try {
        return handler(event, ctx)
      } catch {}
    })

  on("session_start", (_event, ctx) => status("SessionStart", ctx))
  on("before_agent_start", (event, ctx) => status("UserPromptSubmit", ctx, field(event, "prompt") ?? ""))
  on("agent_settled", (_event, ctx) => status("Stop", ctx))
  on("session_info_changed", (event, ctx) => {
    const last = r.status
    const title = field(event, "name") ?? body("", ctx).title
    if (!last || last.session_id !== body("", ctx).session_id || title === last.title) return
    r.status = { ...last, title }
    void post(r, hook, r.status)
  })
  on("session_shutdown", (_event, ctx) => {
    r.status = undefined
    return bounded(post(r, hook, body("SessionEnd", ctx)))
  })
}
