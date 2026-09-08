export type Reflex =
  | "progressing"
  | "uncertain"
  | "stalled"
  | "looping"
  | "regressing"
  | "complete";

export type Phase = "splash" | "prompt" | "zoom" | "run";

export interface HeaderState {
  runId: string;
  state: Reflex;
  agent: string;
  provider: string;
  model: string;
}

export interface EventRow {
  at: string;
  type: string;
  detail: string;
  state?: Reflex;
}

export interface RunTick {
  delay: number;
  event?: EventRow;
  header?: Partial<HeaderState>;
  progress?: number;
  tokens?: number;
}

export const RUN_ID = "run_7f3a91c2";

export const INITIAL_HEADER: HeaderState = {
  runId: RUN_ID,
  state: "progressing",
  agent: "opencode",
  provider: "omlx",
  model: "qwen2.5-coder",
};

export const COMPLETED_HEADER: HeaderState = {
  runId: RUN_ID,
  state: "complete",
  agent: "codex",
  provider: "openai",
  model: "gpt-5",
};

export const PROMPT = 'temper run "fix the auth tests"';

export const ASCII = `
████████╗███████╗███╗   ███╗██████╗ ███████╗██████╗
╚══██╔══╝██╔════╝████╗ ████║██╔══██╗██╔════╝██╔══██╗
   ██║   █████╗  ██╔████╔██║██████╔╝█████╗  ██████╔╝
   ██║   ██╔══╝  ██║╚██╔╝██║██╔═══╝ ██╔══╝  ██╔══██╗
   ██║   ███████╗██║ ╚═╝ ██║██║     ███████╗██║  ██║
   ╚═╝   ╚══════╝╚═╝     ╚═╝╚═╝     ╚══════╝╚═╝  ╚═╝`.trim();

export const SPIN = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

export const SCRIPT: RunTick[] = [
  {
    delay: 280,
    event: { at: "00:00.0", type: "run.started", detail: RUN_ID },
    header: { state: "progressing" },
    progress: 6,
    tokens: 180,
  },
  {
    delay: 420,
    event: { at: "00:00.4", type: "task.classified", detail: "bugfix · go · tests" },
    progress: 9,
    tokens: 410,
  },
  {
    delay: 380,
    event: { at: "00:00.8", type: "agent.selected", detail: "opencode" },
    tokens: 620,
  },
  {
    delay: 360,
    event: { at: "00:01.1", type: "model.called", detail: "omlx / qwen2.5-coder" },
    tokens: 980,
  },
  {
    delay: 480,
    event: { at: "00:01.6", type: "tool.requested", detail: "read internal/auth/session.go" },
    tokens: 1210,
  },
  {
    delay: 520,
    event: { at: "00:02.1", type: "tool.completed", detail: "session.go  184 lines" },
    progress: 18,
    tokens: 1540,
  },
  {
    delay: 400,
    event: { at: "00:02.5", type: "tool.requested", detail: "read internal/auth/session_test.go" },
    tokens: 1710,
  },
  {
    delay: 540,
    event: { at: "00:03.0", type: "tool.completed", detail: "session_test.go  4 failing" },
    progress: 24,
    tokens: 1980,
  },
  {
    delay: 460,
    event: { at: "00:03.5", type: "tool.requested", detail: "edit internal/auth/session.go" },
    tokens: 2340,
  },
  {
    delay: 580,
    event: { at: "00:04.1", type: "tool.completed", detail: "+18 −6" },
    progress: 33,
    tokens: 2710,
  },
  {
    delay: 500,
    event: {
      at: "00:04.6",
      type: "progress.evaluated",
      detail: "progressing  0.41",
      state: "progressing",
    },
    header: { state: "progressing" },
    progress: 41,
    tokens: 2890,
  },
  {
    delay: 440,
    event: { at: "00:05.0", type: "tool.requested", detail: "test ./internal/auth" },
    tokens: 3120,
  },
  {
    delay: 640,
    event: { at: "00:05.7", type: "test.executed", detail: "FAIL  2/4" },
    progress: 44,
    tokens: 3480,
  },
  {
    delay: 420,
    event: { at: "00:06.1", type: "tool.requested", detail: "edit internal/auth/session.go" },
    tokens: 3710,
  },
  {
    delay: 500,
    event: { at: "00:06.6", type: "tool.completed", detail: "+4 −4" },
    tokens: 3990,
  },
  {
    delay: 480,
    event: {
      at: "00:07.1",
      type: "progress.evaluated",
      detail: "uncertain  0.38",
      state: "uncertain",
    },
    header: { state: "uncertain" },
    progress: 38,
    tokens: 4180,
  },
  {
    delay: 420,
    event: { at: "00:07.5", type: "tool.requested", detail: "edit internal/auth/session.go" },
    tokens: 4410,
  },
  {
    delay: 480,
    event: { at: "00:08.0", type: "tool.completed", detail: "+3 −3" },
    tokens: 4680,
  },
  {
    delay: 520,
    event: {
      at: "00:08.5",
      type: "progress.evaluated",
      detail: "stalled  0.36",
      state: "stalled",
    },
    header: { state: "stalled" },
    progress: 36,
    tokens: 4890,
  },
  {
    delay: 380,
    event: { at: "00:08.9", type: "loop.detected", detail: "edit→test→edit  ×3", state: "looping" },
    header: { state: "looping" },
    progress: 34,
    tokens: 5020,
  },
  {
    delay: 560,
    event: {
      at: "00:09.4",
      type: "recovery.started",
      detail: "reflex · switch_agent",
      state: "looping",
    },
    tokens: 5210,
  },
  {
    delay: 500,
    event: { at: "00:09.9", type: "agent.switched", detail: "opencode → codex" },
    header: { agent: "codex", provider: "openai", model: "gpt-5", state: "progressing" },
    progress: 48,
    tokens: 5480,
  },
  {
    delay: 440,
    event: { at: "00:10.4", type: "model.called", detail: "openai / gpt-5" },
    tokens: 5810,
  },
  {
    delay: 420,
    event: { at: "00:10.8", type: "tool.requested", detail: "test ./internal/auth" },
    tokens: 5990,
  },
  {
    delay: 700,
    event: { at: "00:11.5", type: "test.executed", detail: "PASS  4/4" },
    progress: 86,
    tokens: 6280,
  },
  {
    delay: 480,
    event: {
      at: "00:12.0",
      type: "progress.evaluated",
      detail: "complete  1.00",
      state: "complete",
    },
    header: { state: "complete" },
    progress: 100,
    tokens: 6410,
  },
  {
    delay: 420,
    event: { at: "00:12.4", type: "run.completed", detail: "22.4s  ·  6.4k tok" },
    tokens: 6410,
  },
];

export const STATIC_EVENTS: EventRow[] = SCRIPT.filter((t) => t.event).map(
  (t) => t.event as EventRow,
);

export function bar(pct: number, width = 22): string {
  const clamped = Math.max(0, Math.min(100, pct));
  const filled = Math.round((clamped / 100) * width);
  return "█".repeat(filled) + "░".repeat(width - filled);
}

export function reflexColor(state: Reflex | undefined): string {
  switch (state) {
    case "progressing":
    case "complete":
      return "phosphor";
    case "uncertain":
    case "stalled":
      return "amber";
    case "looping":
    case "regressing":
      return "alert";
    default:
      return "paper";
  }
}

export function eventTone(type: string, state?: Reflex): string {
  if (state) return reflexColor(state);
  if (type === "loop.detected" || type === "recovery.started") return "alert";
  if (type.startsWith("progress.") || type === "run.completed") return "phosphor";
  if (type === "test.executed") return "paper";
  return "dim";
}

export function delay(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const t = setTimeout(resolve, ms);
    const onAbort = () => {
      clearTimeout(t);
      reject(new DOMException("aborted", "AbortError"));
    };
    if (signal.aborted) {
      onAbort();
      return;
    }
    signal.addEventListener("abort", onAbort, { once: true });
  });
}
