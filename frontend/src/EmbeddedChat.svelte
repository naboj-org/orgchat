<script lang="ts">
  import { onMount, tick } from "svelte";
  import ConversationHistory from "./lib/ConversationHistory.svelte";
  import { createChat } from "./lib/chat.svelte";
  import { createEmbedAuth } from "./lib/embed-auth";
  import { request } from "./lib/api";

  let visible = $state(false);
  let expanded = $state(false);
  let openingTab = $state(false);
  let popoutError = $state("");
  let launcher: HTMLButtonElement;
  let closeButton: HTMLButtonElement;
  let parentOrigin: string | undefined;
  const framed = window.parent !== window;

  const auth = createEmbedAuth((available) => {
    visible = available;
    if (framed && parentOrigin) window.parent.postMessage(
      { type: "orgchat:visibility", visible: available }, parentOrigin,
    );
  });
  const selectedChannelKey = "orgchat:widget:selected-channel";

  function restoreSelectedChannel(): string[] {
    try {
      const id = localStorage.getItem(selectedChannelKey);
      return id ? [id] : [];
    } catch {
      // Storage can be unavailable in third-party embeds.
      return [];
    }
  }

  const chat = createChat({
    beforeConnect: auth.checkCookies,
    authenticate: auth.authenticate,
    initialOpenChannelIds: restoreSelectedChannel(),
  });

  function selectChannel(id: string) {
    chat.setOpenChannels([id]);
    const selected = chat.openChannelIds[0];
    if (!selected) return;
    try {
      localStorage.setItem(selectedChannelKey, selected);
    } catch {
      // Keep channel switching usable even when storage is blocked.
    }
  }
  const unreadCount = $derived(chat.unreadChannels.length);
  const activeChannel = $derived(chat.openChannelIds[0] ?? "");
  const conversation = $derived(chat.conversation(activeChannel));

  async function openInNewTab() {
    if (openingTab) return;
    popoutError = "";
    // Navigate immediately, while the click still grants popup permission.
    // Do not access a blank popup's document or navigate it after an await:
    // browser isolation rules can prevent that from a cross-site iframe.
    const tab = window.open(new URL("/popout.html", location.origin).href, "_blank");
    if (!tab) {
      popoutError = "Your browser blocked the new tab. Allow popups for chat and try again.";
      return;
    }
    openingTab = true;
    let handleMessage: (event: MessageEvent) => void = () => {};
    let timer: ReturnType<typeof setTimeout> | undefined;
    try {
      const ready = new Promise<void>((resolve, reject) => {
        handleMessage = (event) => {
          if (event.source === tab && event.origin === location.origin &&
              event.data?.type === "orgchat:popout-ready") resolve();
        };
        window.addEventListener("message", handleMessage);
        timer = setTimeout(() => reject(new Error("The chat tab did not finish loading. Please try again.")), 20000);
      });
      const [{ token }] = await Promise.all([
        request<{ token: string }>("/auth/popout", { method: "POST" }),
        ready,
      ]);
      if (tab.closed) return;
      tab.postMessage({ type: "orgchat:popout-token", token }, location.origin);
    } catch (cause) {
      tab.close();
      popoutError = `Could not open chat in a new tab: ${cause instanceof Error ? cause.message : String(cause)}`;
    } finally {
      clearTimeout(timer);
      window.removeEventListener("message", handleMessage);
      openingTab = false;
    }
  }

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
    auth.setParentOrigin(parentOrigin);
    function handleMessage(event: MessageEvent) {
      auth.handleMessage(event);
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
      auth.dispose();
      window.removeEventListener("message", handleMessage);
    };
  });
</script>

<svelte:window onkeydown={handleKeydown} />

{#if visible}
<div class="widget" class:expanded class:framed>
  {#if expanded}
    <div class="widget-panel" role="dialog" aria-label="orgchat" tabindex="-1">
      <header class="widget-header">
        <div class="widget-heading">
          <strong>orgchat</strong>
          {#if unreadCount}
            <span class="widget-badge" role="status" aria-label={`${unreadCount} unread channels`}>
              {unreadCount}
            </span>
          {/if}
        </div>
        <button
          class="widget-open"
          disabled={openingTab}
          onclick={openInNewTab}
          aria-label="Open orgchat in a new tab"
          title="Open orgchat in a new tab">
          ↗
        </button>
        <button
          bind:this={closeButton}
          aria-label="Close chat"
          onclick={() => setExpanded(false)}>
          ×
        </button>
      </header>
      {#if popoutError}
        <div class="connection-status error" role="alert">
          <span>{popoutError}</span>
          <button disabled={openingTab} onclick={openInNewTab}>Try again</button>
        </div>
      {/if}
      <div class="widget-channels">
        <label for="widget-channel">Channel</label>
        <select
          id="widget-channel"
          value={activeChannel}
          disabled={!chat.connected || chat.syncing}
          onchange={(event) => selectChannel(event.currentTarget.value)}>
          {#if !chat.channels.length}<option value="">Loading…</option>{/if}
          {#each chat.channels as channel (channel.id)}
            <option value={channel.id}>
              #{channel.name}{channel.unread ? " • unread" : ""}
            </option>
          {/each}
        </select>
      </div>
      {#if !chat.connected || chat.error || conversation.error}
        <div class="connection-status" class:error={!!chat.error || !!conversation.error} role="status">
          <span>
            {chat.error || conversation.error ||
              (chat.syncing ? "Connecting…" : "Disconnected. Reconnecting…")}
          </span>
          <button onclick={() => chat.connected && !chat.error ? chat.loadChannel(activeChannel) : chat.retry()}>Retry</button>
        </div>
      {/if}
      {#if activeChannel}
        {#key activeChannel}
          <ConversationHistory
            channel={chat.channels.find(
              (channel) => channel.id === activeChannel,
            )?.name ?? activeChannel}
            description={chat.channels.find(channel => channel.id === activeChannel)?.description ?? ""}
            compactDescription
            messages={conversation.messages}
            users={chat.users}
            disabled={!chat.connected || chat.syncing || conversation.loading}
            loading={conversation.loading}
            hasOlder={conversation.hasOlder}
            showHeading={false}
            onLoadOlder={() => chat.loadOlder(activeChannel)}
            onSend={(text, clientMessageId) => chat.send(activeChannel, text, clientMessageId)}
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
      title="Open chat"
      aria-expanded="false"
      aria-haspopup="dialog"
      onclick={() => setExpanded(true)}>
      <img src="/favicon.svg" width="32" height="32" alt="" aria-hidden="true" />
      {#if unreadCount}<span class="widget-badge" aria-hidden="true">
          {unreadCount}
        </span>{/if}
    </button>
  {/if}
</div>
{/if}
