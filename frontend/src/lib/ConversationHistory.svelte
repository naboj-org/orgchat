<script lang="ts">
  import { onMount, tick } from "svelte";
  import Username from "./Username.svelte";
  import type { Message, User } from "./api";

  let {
    channel,
    messages,
    users,
    onSend,
    onRead,
    onLoadOlder,
    hasOlder = false,
    disabled = false,
    loading = false,
    showHeading = true,
    autoFocus = false,
  }: {
    channel: string;
    messages: Message[];
    users: Record<string, User>;
    onSend: (text: string, clientMessageId: string) => Promise<void>;
    onRead: (channelId: string, messageId: string) => void;
    onLoadOlder: () => Promise<void>;
    hasOlder?: boolean;
    disabled?: boolean;
    loading?: boolean;
    showHeading?: boolean;
    autoFocus?: boolean;
  } = $props();

  let draft = $state("");
  let sending = $state(false);
  let olderLoading = $state(false);
  let error = $state("");
  let list: HTMLDivElement;
  let messageInput: HTMLInputElement;
  let focusedInitially = false;
  let atBottom = true;
  let restoring = false;
  let pending: { text: string; id: string } | undefined;
  const timeFormat = new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
  });

  const dateFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "long" });

  function localDay(timestamp: string) {
    return new Date(timestamp).toDateString();
  }

  function updateReadPosition() {
    if (!list?.isConnected) return;
    atBottom = list.scrollHeight - list.scrollTop - list.clientHeight < 24;
    const latest = messages.at(-1);
    if (
      latest &&
      atBottom &&
      !disabled &&
      !restoring &&
      document.visibilityState === "visible"
    ) {
      onRead(latest.channelId, latest.id);
    }
  }

  $effect(() => {
    const latestId = messages.at(-1)?.id;
    const blocked = disabled;
    if (!latestId || blocked || restoring) return;
    let cancelled = false;
    tick().then(() => {
      if (cancelled || !list) return;
      if (atBottom) list.scrollTop = list.scrollHeight;
      updateReadPosition();
    });
    return () => {
      cancelled = true;
    };
  });

  $effect(() => {
    if (
      !autoFocus ||
      disabled ||
      focusedInitially ||
      !window.matchMedia("(hover: hover) and (pointer: fine)").matches
    )
      return;
    let cancelled = false;
    tick().then(() => {
      if (cancelled || !messageInput?.isConnected) return;
      messageInput.focus({ preventScroll: true });
      focusedInitially = true;
    });
    return () => {
      cancelled = true;
    };
  });

  onMount(() => {
    document.addEventListener("visibilitychange", updateReadPosition);
    window.addEventListener("focus", updateReadPosition);
    return () => {
      document.removeEventListener("visibilitychange", updateReadPosition);
      window.removeEventListener("focus", updateReadPosition);
    };
  });

  async function send(event: SubmitEvent) {
    event.preventDefault();
    const text = draft.trim();
    if (!text || sending || disabled) return;
    messageInput.focus({ preventScroll: true });
    // Preserve the key on a failed send so retrying cannot create a duplicate.
    if (pending?.text !== text) pending = { text, id: crypto.randomUUID() };
    sending = true;
    error = "";
    try {
      await onSend(text, pending.id);
      if (draft.trim() === text) draft = "";
      pending = undefined;
      await tick();
      list.scrollTop = list.scrollHeight;
      updateReadPosition();
    } catch (cause) {
      error = `Could not send: ${String(cause)}`;
    } finally {
      sending = false;
    }
  }

  async function loadOlder() {
    if (olderLoading || disabled) return;
    olderLoading = true;
    restoring = true;
    error = "";
    const height = list.scrollHeight;
    const top = list.scrollTop;
    try {
      await onLoadOlder();
      await tick();
      list.scrollTop = top + list.scrollHeight - height;
    } catch (cause) {
      error = `Could not load older messages: ${String(cause)}`;
    } finally {
      restoring = false;
      olderLoading = false;
      updateReadPosition();
    }
  }
</script>

<section aria-label="Conversation history">
  {#if showHeading}<h1>#{channel}</h1>{/if}
  <div class="messages" bind:this={list} onscroll={updateReadPosition}>
    {#if hasOlder}
      <button
        class="load-older"
        disabled={disabled || olderLoading}
        onclick={loadOlder}>
        {olderLoading ? "Loading…" : "Load older messages"}
      </button>
    {/if}
    {#if loading}<p class="notice" role="status">Loading messages…</p>{/if}
    {#each messages as message, index (message.id)}
      {#if index === 0 || localDay(message.createdAt) !== localDay(messages[index - 1].createdAt)}
        <div class="date-divider">
          <time datetime={message.createdAt}>{dateFormat.format(new Date(message.createdAt))}</time>
        </div>
      {/if}
      <article class="message">
        <span class="message-text">
          <Username
            name={users[message.authorId]?.username ?? message.authorId}
            affiliation={users[message.authorId]?.affiliation} />: {message.text}
        </span>
        <time
          datetime={message.createdAt}
          title={new Date(message.createdAt).toLocaleString()}>
          ({timeFormat.format(new Date(message.createdAt))})
        </time>
      </article>
    {/each}
  </div>
  {#if error}<p class="notice error-notice" role="alert">{error}</p>{/if}
  <form onsubmit={send}>
    <input
      bind:this={messageInput}
      aria-label={`Message #${channel}`}
      placeholder={`Message #${channel}`}
      bind:value={draft}
      maxlength="10000"
      disabled={disabled} />
    <button type="submit" disabled={disabled || sending || !draft.trim()}>
      {sending ? "Sending…" : "Send"}
    </button>
  </form>
</section>

<style>
  section {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    padding: var(--conversation-padding, 24px);
    container-type: inline-size;
    container-name: conversation;
  }

  h1,
  form {
    flex-shrink: 0;
  }

  .messages {
    flex: 1;
    min-height: 0;
    margin-inline: calc(-1 * var(--conversation-padding, 24px));
    overflow-y: auto;
  }

  .notice {
    color: var(--muted);
    padding-inline: var(--conversation-padding, 24px);
    font-size: 14px;
  }

  .error-notice {
    color: var(--error-text);
  }

  .load-older {
    width: auto;
    display: block;
    margin: 8px auto;
    color: var(--muted);
    font-size: 14px;
  }

  form {
    display: flex;
    gap: 8px;
    margin-top: var(--conversation-padding, 24px);
  }

  input {
    flex: 1;
    min-width: 0;
    padding: 10px 12px;
    border: 1px solid var(--border);
    border-radius: 6px;
    font: inherit;
    color: var(--text);
    background: var(--background);
  }

  input::placeholder {
    color: var(--muted);
  }

  form button {
    width: auto;
    padding: 10px 16px;
    border: 1px solid var(--border);
    font-weight: 500;
  }

  .message {
    display: flex;
    align-items: baseline;
    gap: 12px;
    padding: 4px var(--conversation-padding, 24px);
  }

  .message:hover {
    background: var(--hover);
  }

  .message-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .date-divider {
    display: flex;
    align-items: center;
    gap: 12px;
    margin: 16px var(--conversation-padding, 24px);
    color: var(--muted);
    font-size: 12px;
  }

  .date-divider::before,
  .date-divider::after {
    content: "";
    flex: 1;
    border-top: 1px solid var(--border);
  }

  .message time {
    margin-left: auto;
    color: var(--muted);
    white-space: nowrap;
    font-size: 14px;
    font-variant-numeric: tabular-nums;
  }

  @container conversation (max-width: 560px) {
    .message {
      display: block;
    }

    .message time {
      margin-left: 8px;
      font-size: 12px;
    }
  }
</style>
