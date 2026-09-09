<script lang="ts">
  const cmd = "curl -fsSL https://raw.githubusercontent.com/james-see/temper/main/install.sh | bash";
  const go = "go install github.com/james-see/temper/cmd/temper@latest";
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

<div class="install">
  <button type="button" onclick={copy}>
    <span class="lines">
      <span><span class="ps">$</span> curl -fsSL https://raw.githubusercontent.com/james-see/temper/main/install.sh | bash</span>
    </span>
    <span class="hint">{copied ? "copied" : "copy"}</span>
  </button>
  <p class="alt">
    or:<br>
    <span class="ps">$</span> brew tap james-see/tap && brew install temper<br>
    <span class="ps">$</span> go install github.com/james-see/temper/cmd/temper@latest
  </p>
</div>

<style>
  .install {
    display: grid;
    gap: 8px;
  }

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
    border-color: var(--ink);
  }

  .lines {
    display: grid;
    gap: 4px;
    flex: 1;
    font-size: clamp(12px, 2.4vw, 14px);
  }

  .lines > span {
    display: flex;
    align-items: baseline;
    gap: 12px;
  }

  .ps {
    color: var(--ink);
    flex: none;
  }

  .hint {
    flex: none;
    color: var(--dim);
    font-size: 11px;
    letter-spacing: 0.08em;
    text-transform: lowercase;
  }

  .alt {
    margin: 0 2px;
    color: var(--dim);
    font-size: 11px;
    overflow-wrap: anywhere;
  }
</style>
