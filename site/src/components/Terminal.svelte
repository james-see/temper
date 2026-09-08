<script lang="ts">
  import {
    ASCII,
    COMPLETED_HEADER,
    INITIAL_HEADER,
    PROMPT,
    SCRIPT,
    SPIN,
    STATIC_EVENTS,
    bar,
    delay,
    eventTone,
    type EventRow,
    type HeaderState,
    type Phase,
  } from "../lib/demo";

  let phase = $state<Phase>("splash");
  let prompt = $state("");
  let zoomText = $state("");
  let events = $state<EventRow[]>([]);
  let header = $state<HeaderState>({ ...INITIAL_HEADER });
  let progress = $state(0);
  let tokens = $state(0);
  let spinIdx = $state(0);
  let splashMsg = $state("starting runtime...");

  const spinChar = $derived(SPIN[spinIdx % SPIN.length]);
  const tokenLabel = $derived(
    tokens >= 1000 ? `${(tokens / 1000).toFixed(1)}k tok` : `${tokens} tok`,
  );

  function applyStatic() {
    phase = "run";
    prompt = PROMPT;
    zoomText = "";
    events = [...STATIC_EVENTS];
    header = { ...COMPLETED_HEADER };
    progress = 100;
    tokens = 6410;
    splashMsg = "runtime ready";
  }

  function resetLive() {
    phase = "splash";
    prompt = "";
    zoomText = "";
    events = [];
    header = { ...INITIAL_HEADER };
    progress = 0;
    tokens = 0;
    splashMsg = "starting runtime...";
  }

  async function typeInto(
    write: (s: string) => void,
    text: string,
    signal: AbortSignal,
    cps: number,
  ) {
    let out = "";
    for (const ch of text) {
      out += ch;
      write(out);
      await delay(1000 / cps, signal);
    }
  }

  async function play(signal: AbortSignal) {
    const spin = setInterval(() => {
      spinIdx += 1;
    }, 80);
    signal.addEventListener("abort", () => clearInterval(spin), { once: true });

    try {
      while (!signal.aborted) {
        resetLive();
        await delay(2200, signal);
        splashMsg = "runtime ready";
        await delay(380, signal);

        phase = "prompt";
        await typeInto(
          (s) => {
            prompt = s;
          },
          PROMPT,
          signal,
          26,
        );
        await delay(420, signal);

        phase = "zoom";
        zoomText = "";
        const flood = "temper ".repeat(28).trimEnd();
        await typeInto(
          (s) => {
            zoomText = s;
          },
          flood,
          signal,
          42,
        );
        await delay(520, signal);

        phase = "run";
        zoomText = "";
        for (const tick of SCRIPT) {
          await delay(tick.delay, signal);
          if (tick.event) events = [...events, tick.event].slice(-12);
          if (tick.header) header = { ...header, ...tick.header };
          if (tick.progress !== undefined) progress = tick.progress;
          if (tick.tokens !== undefined) tokens = tick.tokens;
        }

        await delay(3600, signal);
      }
    } catch (err) {
      if (!(err instanceof DOMException && err.name === "AbortError")) throw err;
    } finally {
      clearInterval(spin);
    }
  }

  $effect(() => {
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (reduced) {
      applyStatic();
      return;
    }
    const ac = new AbortController();
    void play(ac.signal);
    return () => ac.abort();
  });
</script>

<section
  class="bezel"
  aria-label="Temper runtime demonstration"
  aria-live="off"
>
  <div class="chrome">
    <span class="dot" aria-hidden="true"></span>
    <span class="chrome-title">temper · tui</span>
    <span class="chrome-meta">local-first</span>
  </div>

  <div class="screen">
    <div class="scan" aria-hidden="true"></div>

    {#if phase === "splash"}
      <div class="splash">
        <pre class="ascii">{ASCII}</pre>
        <p class="splash-line">
          <span class="spin">{spinChar}</span>
          {splashMsg}
        </p>
      </div>
    {:else if phase === "prompt"}
      <div class="prompt-pane">
        <p class="prompt">
          <span class="ps">$</span> {prompt}<span class="caret">█</span>
        </p>
      </div>
    {:else if phase === "zoom"}
      <div class="zoom" aria-hidden="true">
        {zoomText}
      </div>
    {:else}
      <header class="hdr">
        <span class="brand">TEMPER</span>
        <span class="sep">·</span>
        <span class="dim">{header.runId}</span>
        <span class="sep">·</span>
        <span class={header.state}>{header.state}</span>
        <span class="sep">·</span>
        <span>{header.agent}</span>
        <span class="sep">·</span>
        <span class="dim">{header.provider}</span>
        <span class="sep">·</span>
        <span class="dim">{header.model}</span>
      </header>

      <ol class="log">
        {#each events as ev, i (i + ev.at + ev.type)}
          <li class="row">
            <span class="at">{ev.at}</span>
            <span class={"typ " + eventTone(ev.type, ev.state)}>{ev.type}</span>
            <span class="det">{ev.detail}</span>
          </li>
        {/each}
      </ol>

      <footer class="ftr">
        <span class="spin">{header.state === "complete" ? "✓" : spinChar}</span>
        <span class="pbar" aria-hidden="true">{bar(progress)}</span>
        <span class="pct">{progress}%</span>
        <span class={header.state}>{header.state}</span>
        <span class="dim">{tokenLabel}</span>
      </footer>
    {/if}
  </div>
</section>

<style>
  .bezel {
    --r: 14px;
    position: relative;
    background: var(--bezel);
    border: 1px solid var(--line);
    border-radius: 18px;
    box-shadow:
      0 0 0 1px #000,
      0 24px 80px rgba(0, 0, 0, 0.55),
      inset 0 1px 0 rgba(229, 229, 229, 0.06);
    overflow: hidden;
  }

  .chrome {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px 8px;
    font-size: 11px;
    letter-spacing: 0.08em;
    text-transform: lowercase;
    color: var(--dim);
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--ink);
  }

  .chrome-title {
    color: var(--paper);
  }

  .chrome-meta {
    margin-left: auto;
  }

  .screen {
    position: relative;
    min-height: min(62vh, 560px);
    margin: 0 10px 10px;
    padding: 16px 16px 14px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--r);
    overflow: hidden;
  }

  .scan {
    pointer-events: none;
    position: absolute;
    inset: 0;
    background:
      repeating-linear-gradient(
        to bottom,
        transparent 0,
        transparent 2px,
        rgba(0, 0, 0, 0.12) 2px,
        rgba(0, 0, 0, 0.12) 3px
      );
    mix-blend-mode: multiply;
    opacity: 0.45;
  }

  .splash,
  .prompt-pane,
  .zoom {
    position: relative;
    z-index: 1;
    min-height: inherit;
    display: grid;
    place-items: center;
    text-align: center;
  }

  .ascii {
    margin: 0;
    color: var(--paper);
    font-family: var(--mono);
    font-size: clamp(4.4px, 1.55vw, 12px);
    line-height: 1.12;
    letter-spacing: 0;
    overflow: auto;
    max-width: 100%;
  }

  .splash-line {
    margin: 22px 0 0;
    color: var(--dim);
    font-size: 13px;
  }

  .prompt-pane {
    place-items: start center;
    padding-top: 22%;
  }

  .prompt {
    margin: 0;
    font-size: clamp(15px, 2.4vw, 22px);
    color: var(--paper);
  }

  .ps {
    color: var(--ink);
  }

  .caret {
    color: var(--paper);
    animation: blink 0.9s steps(1) infinite;
  }

  .zoom {
    align-content: start;
    padding: 28px 18px;
    font-size: clamp(28px, 7vw, 72px);
    font-family: var(--display);
    font-weight: 800;
    line-height: 0.92;
    letter-spacing: -0.03em;
    text-transform: lowercase;
    color: var(--paper);
    text-align: left;
    word-break: break-word;
    animation: flood 0.18s steps(2) infinite;
  }

  .hdr,
  .ftr,
  .log {
    position: relative;
    z-index: 1;
  }

  .hdr,
  .ftr {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px 10px;
    font-size: 12px;
  }

  .hdr {
    padding-bottom: 10px;
    border-bottom: 1px solid var(--line);
    color: var(--paper);
  }

  .brand {
    font-family: var(--display);
    font-weight: 800;
    font-size: 18px;
    letter-spacing: 0.08em;
    color: var(--paper);
  }

  .sep,
  .dim,
  .at {
    color: var(--dim);
  }

  .log {
    list-style: none;
    margin: 0;
    padding: 12px 0 14px;
    min-height: calc(min(62vh, 560px) - 118px);
    display: flex;
    flex-direction: column;
    justify-content: flex-end;
    gap: 5px;
    mask-image: linear-gradient(to bottom, transparent, #000 16%);
  }

  .row {
    display: grid;
    grid-template-columns: 64px minmax(132px, 0.9fr) 1fr;
    gap: 10px;
    font-size: clamp(11px, 1.5vw, 13px);
  }

  .typ {
    font-weight: 600;
  }

  .det {
    color: var(--paper);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .ftr {
    padding-top: 10px;
    border-top: 1px solid var(--line);
    font-size: 12px;
  }

  .pbar {
    font-variant-ligatures: none;
    letter-spacing: 0;
    color: var(--paper);
  }

  .pct {
    min-width: 3ch;
  }

  .spin {
    color: var(--ink);
    width: 1.2em;
  }

  .progressing,
  .complete {
    color: var(--paper);
  }

  .uncertain,
  .stalled {
    color: var(--amber);
  }

  .looping,
  .regressing {
    color: var(--alert);
  }

  .paper {
    color: var(--paper);
  }

  .phosphor {
    color: var(--paper);
  }

  .amber {
    color: var(--amber);
  }

  .alert {
    color: var(--alert);
  }

  @keyframes blink {
    50% {
      opacity: 0;
    }
  }

  @keyframes flood {
    50% {
      filter: brightness(1.15);
    }
  }

  @media (max-width: 720px) {
    .screen {
      min-height: 68vh;
      padding: 12px;
    }

    .row {
      grid-template-columns: 54px 1fr;
    }

    .det {
      grid-column: 1 / -1;
      padding-left: 54px;
    }

    .log {
      min-height: calc(68vh - 128px);
    }

    .chrome-meta {
      display: none;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .caret,
    .zoom,
    .scan {
      animation: none;
    }
  }
</style>
