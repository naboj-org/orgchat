<script lang="ts">
  let { description, editable = false, disabled = false, inline = false, compact = false, onSave }: {
    description: string;
    editable?: boolean;
    disabled?: boolean;
    inline?: boolean;
    compact?: boolean;
    onSave?: (description: string) => Promise<void>;
  } = $props();

  let editing = $state(false);
  let draft = $state("");
  let saving = $state(false);
  let error = $state("");

  function startEditing() {
    draft = description;
    error = "";
    editing = true;
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    if (!onSave || !editable || disabled || saving) return;
    saving = true;
    error = "";
    try {
      await onSave(draft);
      editing = false;
    } catch (cause) {
      error = `Could not save description: ${String(cause)}`;
    } finally {
      saving = false;
    }
  }
</script>

{#if description || editable}
  <div class="channel-description" class:inline class:compact>
    {#if editing && editable}
      <form onsubmit={save}>
        <label>
          Channel description
          <textarea bind:value={draft} maxlength="5000" rows="3" disabled={disabled || saving}></textarea>
        </label>
        <div class="actions">
          <button type="submit" disabled={disabled || saving} aria-label={saving ? "Saving description" : "Save description"} title="Save description">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m5 12 4 4L19 6" /></svg>
          </button>
          <button type="button" disabled={saving} aria-label="Cancel editing description" title="Cancel" onclick={() => { editing = false; error = ""; }}>
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6" /></svg>
          </button>
        </div>
        {#if error}<p class="error" role="alert">{error}</p>{/if}
      </form>
    {:else}
      {#if description}<p class="description">{description}</p>{/if}
      {#if editable}
        <button type="button" disabled={disabled} onclick={startEditing}
          aria-label={description ? "Edit channel description" : "Add channel description"}
          title={description ? "Edit channel description" : "Add channel description"}>
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m16 3 5 5L9 20l-6 1 1-6L16 3ZM13 6l5 5" /></svg>
        </button>
      {/if}
    {/if}
  </div>
{/if}

<style>
  .channel-description {
    flex-shrink: 0;
    max-height: 30vh;
    overflow-y: auto;
    margin-bottom: 12px;
    padding-bottom: 12px;
    border-bottom: 1px solid var(--border);
    font-size: 14px;
  }

  .channel-description.compact {
    margin-bottom: 4px;
    padding-bottom: 4px;
    font-size: 12px;
    line-height: 1.35;
  }

  .channel-description.inline {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0;
    padding: 0;
    border: 0;
  }

  .inline .description {
    flex: 0 1 auto;
    min-width: 0;
  }

  .inline form {
    flex: 1;
    min-width: 0;
  }

  .description {
    margin: 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    color: var(--muted);
  }

  label {
    display: block;
  }

  textarea {
    display: block;
    box-sizing: border-box;
    width: 100%;
    margin-top: 4px;
    padding: 8px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--background);
    color: var(--text);
    font: inherit;
    resize: vertical;
  }

  textarea:focus-visible {
    outline: 2px solid var(--muted);
    outline-offset: 2px;
  }

  .actions {
    display: flex;
    gap: 8px;
    margin-top: 4px;
  }

  button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 28px;
    height: 28px;
    padding: 5px;
    color: var(--muted);
  }

  svg {
    width: 18px;
    height: 18px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  .error {
    color: var(--error-text);
    margin: 4px 0 0;
  }
</style>
