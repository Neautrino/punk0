// punk0's Oh My Pi extension: tells the punk0 tab an omp runs in what the
// tab's chat is doing, by POSTing punk0's hook body to PUNK0_HOOK_URL.
// Without PUNK0_HOOK_URL it does nothing. It never throws and never writes to
// stdout or stderr: anything written there shows up in the agent's terminal.
//
// punk0 writes it to its own state folder and loads it for one run with
// `omp -e <file>` (agentshell.go); the user's own extensions load beside it
// and nothing of the user's is edited.
//
// Status: session_start and session_switch (/new, /resume) = SessionStart
// (binds the tab's chat, idle), before_agent_start = UserPromptSubmit
// (working), tool_approval_requested = PermissionRequest (needs you),
// tool_approval_resolved = PostToolUse (working), agent_end = Stop (idle)
// unless omp continues the turn, session_shutdown = SessionEnd. A subagent's
// events are not the tab's, except its approval prompts (approval). omp
// titles a chat about 2 s after its first turn ends, with no event, so the
// last status is re-sent with the title once 3 s after each turn and on
// session_stop, when the title changed. punk0 reads a title from
// SessionStart, UserPromptSubmit and Stop only (hook.go, refreshTitle), so a
// re-sent PermissionRequest or PostToolUse leaves the tab's title to the
// next of those, which carries it.

const AGENT = "omp"
const POST_TIMEOUT_MS = 1000
const TITLE_WAIT_MS = 3000

type Header = { title?: string; parentSession?: string }
type Session = {
  getSessionId?: () => string
  getSessionFile?: () => string | undefined
  getHeader?: () => Header | undefined
}
type Ctx = { sessionManager?: Session; cwd?: string; agentKind?: string }
type Handler = (event: unknown, ctx: Ctx) => unknown
type Omp = {
  on: (name: string, handler: Handler) => void
  getSessionName?: () => string | undefined
}

// A body as punk0's hook reads it (hook.go hookEvent).
type Body = {
  hook_event_name: string
  punk0_agent: string
  session_id: string
  transcript_path: string
  title: string
  cwd: string
  prompt?: string
  agent_id?: string // a subagent's approval prompt: the subagent's session
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

// field is event[key] when the event has it.
function field(event: unknown, key: string): unknown {
  return event && typeof event === "object" && key in event ? (event as Record<string, unknown>)[key] : undefined
}

// The process's one reporter. omp may run the factory again for a chat it
// switches to, so the reporter lives on globalThis: posts stay in order
// across the switch, and only the newest run's handlers report whether or
// not omp dropped the earlier ones.
function reporter(): Reporter {
  const g = globalThis as Record<symbol, Reporter | undefined>
  const key = Symbol.for("punk0.omp.reporter")
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

// omp awaits a handler, so the one that waits for its post waits no longer
// than a post may take.
function bounded(p: Promise<void>): Promise<void> {
  const { promise, resolve } = Promise.withResolvers<void>()
  setTimeout(resolve, POST_TIMEOUT_MS).unref?.()
  return Promise.race([p, promise])
}

// A task tool's subagent runs in the same process and reaches the same
// handlers; its chat is not the tab's. The filter is Orca's extension's.
function subagent(ctx: Ctx): boolean {
  if (ctx?.agentKind === "sub") return true
  return typeof quiet(() => ctx?.sessionManager?.getHeader?.()?.parentSession, undefined) === "string"
}

export default function (omp: Omp): void {
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
      title: quiet(() => omp.getSessionName?.() || s?.getHeader?.()?.title, ""),
      cwd: ctx?.cwd || process.cwd(),
    }
  }
  const status = (name: string, ctx: Ctx, prompt?: string) => {
    const b = body(name, ctx)
    if (prompt !== undefined) b.prompt = prompt
    r.status = b
    void post(r, hook, b)
  }
  // The last status again, with the chat's title, if omp titled it since.
  const retitle = (ctx: Ctx) => {
    const last = r.status
    if (!last) return
    const now = body("", ctx)
    if (now.session_id !== last.session_id || now.title === last.title) return
    r.status = { ...last, title: now.title }
    void post(r, hook, r.status)
  }
  // A subagent's approval prompt waits for the user like the chat's own, so
  // it reaches the tab as the chat's last status renamed, with agent_id the
  // subagent's session: punk0 shows needs-you and keeps the tab's chat
  // (handleHook binds no session from an event with agent_id). Every other
  // subagent event is dropped.
  const approval = (name: string, ctx: Ctx) => {
    if (!subagent(ctx)) return status(name, ctx)
    const last = r.status
    if (!last) return
    const agent_id = quiet(() => ctx?.sessionManager?.getSessionId?.(), "") || "subagent"
    void post(r, hook, { ...last, hook_event_name: name, prompt: undefined, agent_id })
  }
  const on = (name: string, handler: Handler, subagents = false) =>
    omp.on(name, (event, ctx) => {
      if (gen !== r.gen) return
      try {
        if (!subagents && subagent(ctx)) return
        return handler(event, ctx)
      } catch {}
    })

  on("session_start", (_event, ctx) => status("SessionStart", ctx))
  on("session_switch", (_event, ctx) => status("SessionStart", ctx))
  on("before_agent_start", (event, ctx) => {
    const prompt = field(event, "prompt")
    status("UserPromptSubmit", ctx, typeof prompt === "string" ? prompt : "")
  })
  on("tool_approval_requested", (_event, ctx) => approval("PermissionRequest", ctx), true)
  on("tool_approval_resolved", (_event, ctx) => approval("PostToolUse", ctx), true)
  on("agent_end", (event, ctx) => {
    if (field(event, "willContinue") === true) return
    status("Stop", ctx)
    // A global timer, deliberately not ctx.setTimeout (the spike never saw
    // one on omp's ctx): the try keeps its callback from throwing, which
    // would end the omp session, the reason omp's docs give for theirs.
    setTimeout(() => {
      if (gen !== r.gen) return
      try {
        retitle(ctx)
      } catch {}
    }, TITLE_WAIT_MS).unref?.()
  })
  on("session_stop", (_event, ctx) => retitle(ctx))
  on("session_shutdown", (_event, ctx) => {
    r.status = undefined
    return bounded(post(r, hook, body("SessionEnd", ctx)))
  })
}
