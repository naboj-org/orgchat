<script lang="ts">
  import { tick } from 'svelte'
  import ConversationHistory from './ConversationHistory.svelte'
  import type { Chat } from './chat.svelte'

  let { chat, channelId, focused, visible, closable, onFocus, onClose }: {
    chat: Chat
    channelId: string
    focused: boolean
    visible: boolean
    closable: boolean
    onFocus: () => void
    onClose: () => void
  } = $props()

  const conversation = $derived(chat.conversation(channelId))
  const channelName = $derived(chat.channels.find(channel => channel.id === channelId)?.name ?? channelId)
  let pane: HTMLDivElement

  $effect(() => {
    if (!focused || !visible) return
    let cancelled = false
    tick().then(() => {
      if (!cancelled && pane?.isConnected) pane.scrollIntoView({ block: 'nearest', inline: 'nearest' })
    })
    return () => { cancelled = true }
  })
</script>

<div
  class="conversation-pane"
  class:focused
  hidden={!visible}
  bind:this={pane}
  role="group"
  aria-label={`Conversation #${channelName}`}
  onpointerdown={onFocus}
  onfocusin={onFocus}
>
  <header class="pane-header">
    <h1>#{channelName}</h1>
    {#if closable}
      <button
        aria-label={`Close #${channelName}`}
        onclick={(event) => { event.stopPropagation(); onClose() }}
      >×</button>
    {/if}
  </header>
  {#if conversation.error}
    <div class="connection-status error" role="alert">
      <span>{conversation.error}</span>
      <button onclick={() => chat.loadChannel(channelId)}>Retry</button>
    </div>
  {/if}
  <ConversationHistory
    channel={channelName}
    messages={conversation.messages}
    users={chat.users}
    disabled={!chat.connected || chat.syncing || conversation.loading}
    loading={conversation.loading}
    hasOlder={conversation.hasOlder}
    showHeading={false}
    autoFocus={focused && visible}
    readEnabled={visible}
    onLoadOlder={() => chat.loadOlder(channelId)}
    onSend={(text, clientMessageId) => chat.send(channelId, text, clientMessageId)}
    onRead={chat.markRead}
  />
</div>
