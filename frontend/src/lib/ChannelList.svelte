<script lang="ts" generics="Channel extends string">
  let {
    channels,
    activeChannel,
    unreadChannels = [],
    disabled = false,
    openChannels = [],
    allowMultiple = false,
    onSelect,
  }: {
    channels: Channel[];
    activeChannel: Channel;
    unreadChannels?: Channel[];
    disabled?: boolean;
    openChannels?: Channel[];
    allowMultiple?: boolean;
    onSelect: (channel: Channel, event: MouseEvent) => void;
  } = $props();
</script>

<nav aria-label="Channels">
  <h1>orgchat</h1>
  <ul>
    {#each channels as channel}
      <li>
        <button
          {disabled}
          class:open={openChannels.includes(channel)}
          title={allowMultiple ? "Click to replace the focused pane; Ctrl/Cmd+click to open alongside (max 4)" : undefined}
          class:active={activeChannel === channel}
          aria-pressed={activeChannel === channel}
          aria-label={unreadChannels.includes(channel)
            ? `#${channel}, unread messages`
            : `#${channel}`}
          onclick={(event) => onSelect(channel, event)}>
          <span>#{channel}</span>
          {#if unreadChannels.includes(channel)}
            <span class="unread-dot" aria-hidden="true"></span>
          {/if}
        </button>
      </li>
    {/each}
  </ul>
</nav>

<style>
  button {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 8px 10px;
  }

  button.open,
  button.active {
    font-weight: 600;
  }

  li {
    margin-bottom: 4px;
  }

  .unread-dot {
    width: 10px;
    height: 10px;
    flex-shrink: 0;
    border-radius: 50%;
    background: var(--unread);
  }
</style>
