<script lang="ts">
  import Username from "./Username.svelte";
  import type { User } from "./api";

  let { users }: { users: User[] } = $props();
</script>

<aside aria-label="Online users">
  <h2>Online — {users.length}</h2>
  <ul>
    {#each users as user (user.id)}
      <li><Username name={user.username} affiliation={user.affiliation} /></li>
    {/each}
  </ul>
</aside>

<style>
  li {
    display: flex;
    align-items: baseline;
    gap: 6px;
    margin-bottom: 2px;
    padding: 3px 0;
    border-radius: 6px;
  }
</style>
