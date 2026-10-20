<script lang="ts">
  import { onMount } from "svelte";
  import ChannelList from "./lib/ChannelList.svelte";
  import ConversationHistory from "./lib/ConversationHistory.svelte";
  import OnlineUsers from "./lib/OnlineUsers.svelte";
  import { createChat } from "./lib/chat.svelte";

  const chat = createChat();
  let mobilePanel = $state<"channels" | "users" | null>(null);

  onMount(() => chat.connect());
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
      activeChannel={chat.activeChannel}
      unreadChannels={chat.unreadChannels}
      disabled={!chat.connected || chat.syncing}
      onSelect={(id) => {
        mobilePanel = null;
        void chat.selectChannel(id);
      }} />
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
          autoFocus
          hasOlder={chat.hasOlder}
          onLoadOlder={chat.loadOlder}
          onSend={chat.send}
          onRead={chat.markRead} />
      {/key}
    {/if}
  </div>

  <div id="user-panel" class="sidebar" class:open={mobilePanel === "users"}>
    <OnlineUsers users={chat.onlineUsers} />
  </div>
</main>
