<script lang="ts">
  import { onMount } from "svelte";
  import ChannelList from "./lib/ChannelList.svelte";
  import ConversationPane from "./lib/ConversationPane.svelte";
  import OnlineUsers from "./lib/OnlineUsers.svelte";
  import { createChat } from "./lib/chat.svelte";

  const chat = createChat();
  let mobilePanel = $state<"channels" | "users" | null>(null);

  let largeScreen = $state(false);
  let focusedChannelId = $state("");
  let paneNotice = $state("");

  $effect(() => {
    if (!chat.openChannelIds.includes(focusedChannelId)) {
      focusedChannelId = chat.openChannelIds[0] ?? "";
    }
  });

  function selectChannel(id: string, event: MouseEvent) {
    mobilePanel = null;
    paneNotice = "";
    if (chat.openChannelIds.includes(id)) {
      focusedChannelId = id;
      return;
    }
    if (largeScreen && (event.ctrlKey || event.metaKey)) {
      if (chat.openChannelIds.length >= 4) {
        paneNotice = "Up to 4 conversations can be open. Close a pane to open another.";
        return;
      }
      chat.setOpenChannels([...chat.openChannelIds, id]);
    } else {
      const target = chat.openChannelIds.includes(focusedChannelId)
        ? focusedChannelId : chat.openChannelIds[0];
      chat.setOpenChannels(target
        ? chat.openChannelIds.map(open => open === target ? id : open) : [id]);
    }
    focusedChannelId = id;
  }

  function closePane(id: string) {
    if (chat.openChannelIds.length <= 1) return;
    const index = chat.openChannelIds.indexOf(id);
    const remaining = chat.openChannelIds.filter(open => open !== id);
    chat.setOpenChannels(remaining);
    if (focusedChannelId === id) focusedChannelId = remaining[Math.min(index, remaining.length - 1)]!;
    paneNotice = "";
  }

  onMount(() => {
    const media = window.matchMedia("(min-width: 901px)");
    const update = () => { largeScreen = media.matches };
    update();
    media.addEventListener("change", update);
    const disconnect = chat.connect();
    return () => {
      disconnect();
      media.removeEventListener("change", update);
    };
  });
</script>

<svelte:head>
  <title>{chat.unreadChannels.length ? "(*) orgchat" : "orgchat"}</title>
  <link rel="icon" type="image/svg+xml" href={chat.unreadChannels.length ? "/favicon-unread.svg" : "/favicon.svg"} />
</svelte:head>

<main class="full-chat">
  <header class="mobile-toolbar">
    <strong class="mobile-title">orgchat</strong>
    <button
      aria-expanded={mobilePanel === "channels"}
      aria-controls="channel-panel"
      onclick={() =>
        (mobilePanel = mobilePanel === "channels" ? null : "channels")}>
      Channels
    </button>
    <button
      aria-expanded={mobilePanel === "users"}
      aria-controls="user-panel"
      onclick={() => (mobilePanel = mobilePanel === "users" ? null : "users")}>
      Online ({chat.onlineUsers.length})
    </button>
  </header>

  <div
    id="channel-panel"
    class="sidebar"
    class:open={mobilePanel === "channels"}>
    <ChannelList
      channels={chat.channels.map((channel) => channel.id)}
      activeChannel={focusedChannelId}
      openChannels={chat.openChannelIds}
      allowMultiple={largeScreen}
      unreadChannels={chat.unreadChannels}
      disabled={!chat.connected || chat.syncing}
      onSelect={selectChannel} />
  </div>

  <div class="conversation">
    {#if !chat.connected || chat.error}
      <div class="connection-status" class:error={!!chat.error} role="status">
        <span>
          {chat.error ||
            (chat.syncing ? "Connecting…" : "Disconnected. Reconnecting…")}
        </span>
        <button onclick={chat.retry}>Retry</button>
      </div>
    {/if}
    {#if paneNotice}
      <div class="pane-notice" role="status">{paneNotice}</div>
    {/if}
    <div class="conversation-panes" style:--pane-count={largeScreen ? chat.openChannelIds.length || 1 : 1}>
      {#each chat.openChannelIds as channelId (channelId)}
        <ConversationPane
          {chat}
          {channelId}
          focused={focusedChannelId === channelId}
          visible={largeScreen || focusedChannelId === channelId}
          closable={largeScreen && chat.openChannelIds.length > 1}
          onFocus={() => { focusedChannelId = channelId }}
          onClose={() => closePane(channelId)}
        />
      {/each}
    </div>
  </div>

  <div id="user-panel" class="sidebar" class:open={mobilePanel === "users"}>
    <OnlineUsers users={chat.onlineUsers} />
  </div>
</main>
