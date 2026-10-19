<script lang="ts">
  import { onMount, tick } from "svelte";
  import ConversationHistory from "./lib/ConversationHistory.svelte";
  import { createChat } from "./lib/chat.svelte";

  const chat = createChat();
  let expanded = $state(false);
  let launcher: HTMLButtonElement;
  let closeButton: HTMLButtonElement;
  let parentOrigin: string | undefined;
  const framed = window.parent !== window;
  const unreadCount = $derived(chat.unreadChannels.length);

  function notifyParent() {
    if (framed && parentOrigin) {
      window.parent.postMessage(
        { type: "orgchat:resize", expanded },
        parentOrigin,
      );
    }
  }

  async function setExpanded(value: boolean) {
    expanded = value;
    notifyParent();
    await tick();
    focusWidget();
  }

  function focusWidget() {
    if (!expanded) {
      launcher?.focus({ preventScroll: true });
      return;
    }
    closeButton?.focus({ preventScroll: true });
  }

  function handleKeydown(event: KeyboardEvent) {
    if (expanded && event.key === "Escape") {
      event.preventDefault();
      void setExpanded(false);
    }
  }

  onMount(() => {
    const origin = new URLSearchParams(location.search).get("parentOrigin");
    if (origin) {
      try {
        const url = new URL(origin);
        if (["http:", "https:"].includes(url.protocol))
          parentOrigin = url.origin;
      } catch {
        /* Standalone mode needs no parent messaging. */
      }
    }
    function handleMessage(event: MessageEvent) {
      if (event.source !== window.parent || event.origin !== parentOrigin)
        return;
      if (event.data?.type === "orgchat:focus") {
        focusWidget();
      }
    }
    window.addEventListener("message", handleMessage);
    notifyParent();
    const disconnect = chat.connect();
    return () => {
      disconnect();
      window.removeEventListener("message", handleMessage);
    };
  });
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="widget" class:expanded class:framed>
  {#if expanded}
    <div class="widget-panel" role="dialog" aria-label="orgchat" tabindex="-1">
      <header class="widget-header">
        <strong>orgchat</strong>
        <a
          class="widget-open"
          href="./"
          target="_blank"
          rel="noopener noreferrer"
          aria-label="Open orgchat in a new tab"
          title="Open orgchat in a new tab">
          ↗
        </a>
        <button
          bind:this={closeButton}
          aria-label="Close chat"
          onclick={() => setExpanded(false)}>
          ×
        </button>
      </header>
      <div class="widget-channels">
        <label for="widget-channel">Channel</label>
        <select
          id="widget-channel"
          value={chat.activeChannel}
          disabled={!chat.connected || chat.syncing}
          onchange={(event) => chat.selectChannel(event.currentTarget.value)}>
          {#if !chat.channels.length}<option value="">Loading…</option>{/if}
          {#each chat.channels as channel (channel.id)}
            <option value={channel.id}>
              #{channel.name}{channel.unread ? " • unread" : ""}
            </option>
          {/each}
        </select>
      </div>
      {#if !chat.connected || chat.error}
        <div class="connection-status" class:error={!!chat.error} role="status">
          <span>
            {chat.error ||
              (chat.syncing ? "Connecting…" : "Disconnected. Reconnecting…")}
          </span>
          <button onclick={chat.retry}>Retry</button>
        </div>
      {/if}
      {#if chat.activeChannel}
        {#key chat.activeChannel}
          <ConversationHistory
            channel={chat.channels.find(
              (channel) => channel.id === chat.activeChannel,
            )?.name ?? chat.activeChannel}
            messages={chat.messages}
            users={chat.users}
            disabled={!chat.connected || chat.syncing || chat.loading}
            loading={chat.loading}
            hasOlder={chat.hasOlder}
            showHeading={false}
            onLoadOlder={chat.loadOlder}
            onSend={chat.send}
            onRead={chat.markRead} />
        {/key}
      {/if}
    </div>
  {:else}
    <button
      class="widget-launcher"
      bind:this={launcher}
      aria-label={unreadCount
        ? `Open chat, ${unreadCount} unread channels`
        : "Open chat"}
      aria-expanded="false"
      aria-haspopup="dialog"
      onclick={() => setExpanded(true)}>
      orgchat
      {#if unreadCount}<span class="widget-badge" aria-hidden="true">
          {unreadCount}
        </span>{/if}
    </button>
  {/if}
</div>
