<script lang="ts">
  const cmd = "go install github.com/james-see/temper/cmd/temper@latest";
  let copied = $state(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(cmd);
      copied = true;
      setTimeout(() => {
        copied = false;
      }, 1400);
    } catch {
      copied = false;
    }
  }
</script>

<button type="button" onclick={copy}>
  <span class="ps">$</span>
  <code>{cmd}</code>
  <span class="hint">{copied ? "copied" : "copy"}</span>
</button>

<style>
  button {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    margin: 0;
    padding: 14px 16px;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--panel);
    color: var(--paper);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  button:hover {
    border-color: var(--phosphor-dim);
  }

  .ps {
    color: var(--phosphor);
    flex: none;
  }

  code {
    flex: 1;
    font-family: inherit;
    font-size: clamp(12px, 2.4vw, 14px);
    overflow-wrap: anywhere;
  }

  .hint {
    flex: none;
    color: var(--dim);
    font-size: 11px;
    letter-spacing: 0.08em;
    text-transform: lowercase;
  }
</style>
