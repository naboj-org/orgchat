<script lang="ts">
  let { name, affiliation }: { name: string; affiliation?: string } = $props();
  const hueFor = (value: string) => {
    let hash = 0;
    for (const character of value) {
      hash = (hash * 31 + character.codePointAt(0)!) | 0;
    }
    return ((hash % 360) + 360) % 360;
  };
  let hue = $derived(hueFor(name));
</script>

<span
  class="username"
  style:color={`hsl(${hue} 85% var(--username-lightness))`}>
  {name}
</span>
{#if affiliation}
  <span class="affiliation">[{affiliation}]</span>{/if}

<style>
  .affiliation {
    margin-left: 4px;
    color: var(--muted);
    font-size: 12px;
    font-weight: 400;
    white-space: nowrap;
  }

  .username {
    font-weight: 600;
  }
</style>
