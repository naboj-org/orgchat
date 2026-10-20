import { ApiError, channelPath, mergeMessages, request, unauthorizedMessage } from "./api";
import type {
  Bootstrap,
  Channel,
  Message,
  MessagePage,
  MessageResult,
  ServerEvent,
  User,
} from "./api";

// One independent client per call; creating it does not open a connection.
export function createChat() {
  let userMap = $state<Record<string, User>>({});
  let channels = $state<Channel[]>([]);
  let onlineUserIds = $state<string[]>([]);
  let currentUserId = $state("");
  let history = $state<Record<string, Message[]>>({});
  let cursors = $state<Record<string, string | null>>({});
  let activeChannel = $state("");
  let connected = $state(false);
  let syncing = $state(true);
  let loading = $state(false);
  let error = $state("");
  let retryConnection: () => void = () => {};
  let generation = 0;
  let selection = 0;
  const reading = new Set<string>();
  const pendingReads = new Map<string, string>();

  const onlineUsers = $derived(
    onlineUserIds
      .flatMap((id) => (userMap[id] ? [userMap[id]] : []))
      .sort((a, b) =>
        a.username.localeCompare(b.username, undefined, {
          sensitivity: "base",
        }),
      ),
  );
  const unreadChannels = $derived(
    channels.filter((channel) => channel.unread).map((channel) => channel.id),
  );

  function cacheUsers(incoming: User[]) {
    for (const user of incoming) userMap[user.id] = user;
  }

  function applyEvent(event: ServerEvent) {
    if (event.type === "message.created") {
      const { message, author } = event.data;
      cacheUsers([author]);
      history[message.channelId] = mergeMessages(
        history[message.channelId] ?? [],
        [message],
      );
      if (message.authorId !== currentUserId) {
        const cached = history[message.channelId]!;
        const messageIndex = cached.findIndex((item) => item.id === message.id);
        channels = channels.map((channel) => {
          if (channel.id !== message.channelId) return channel;
          const readIndex = cached.findIndex(
            (item) => item.id === channel.lastReadMessageId,
          );
          return readIndex >= messageIndex
            ? channel
            : { ...channel, unread: true };
        });
      }
    } else if (event.type === "presence.snapshot") {
      cacheUsers(event.data.users);
      onlineUserIds = event.data.users.map((user) => user.id);
    } else if (event.type === "presence.changed") {
      if (event.data.online === true) {
        cacheUsers([event.data.user]);
        onlineUserIds = [...new Set([...onlineUserIds, event.data.user.id])];
      } else {
        const { userId } = event.data;
        onlineUserIds = onlineUserIds.filter((id) => id !== userId);
      }
    } else if (event.type === "channel.read") {
      const { channelId, lastReadMessageId } = event.data;
      const cached = history[channelId] ?? [];
      const position = cached.findIndex(
        (message) => message.id === lastReadMessageId,
      );
      const previousId = channels.find(
        (channel) => channel.id === channelId,
      )?.lastReadMessageId;
      if (
        position >= 0 &&
        cached.findIndex((message) => message.id === previousId) > position
      )
        return;
      channels = channels.map((channel) =>
        channel.id === channelId
          ? {
              ...channel,
              lastReadMessageId,
              unread:
                position >= 0
                  ? cached
                      .slice(position + 1)
                      .some((message) => message.authorId !== currentUserId)
                  : cached.length === 0
                    ? false
                    : channel.unread,
            }
          : channel,
      );
    }
  }

  async function selectChannel(id: string) {
    activeChannel = id;
    const version = ++selection;
    const session = generation;
    loading = true;
    error = "";
    try {
      const page = await request<MessagePage>(`${channelPath(id)}/messages`);
      if (session !== generation) return;
      cacheUsers(page.users);
      history[id] = mergeMessages(history[id] ?? [], page.messages);
      cursors[id] = page.nextBefore;
    } catch (cause) {
      if (session === generation && version === selection)
        error = String(cause);
    } finally {
      if (session === generation && version === selection) loading = false;
    }
  }

  async function loadOlder() {
    const id = activeChannel;
    const cursor = cursors[id];
    const session = generation;
    if (!cursor) return;
    const page = await request<MessagePage>(
      `${channelPath(id)}/messages?before=${encodeURIComponent(cursor)}`,
    );
    if (session !== generation) return;
    cacheUsers(page.users);
    history[id] = mergeMessages(page.messages, history[id] ?? []);
    cursors[id] = page.nextBefore;
  }

  async function send(text: string, clientMessageId: string) {
    const id = activeChannel;
    const session = generation;
    const { message, author } = await request<MessageResult>(
      `${channelPath(id)}/messages`,
      {
        method: "POST",
        body: JSON.stringify({ text, clientMessageId }),
      },
    );
    if (session === generation) {
      cacheUsers([author]);
      history[id] = mergeMessages(history[id] ?? [], [message]);
    }
  }

  async function markRead(id: string, messageId: string) {
    const channel = channels.find((channel) => channel.id === id);
    if (
      !connected ||
      syncing ||
      loading ||
      !channel ||
      channel.lastReadMessageId === messageId
    )
      return;
    if (reading.has(id)) {
      pendingReads.set(id, messageId);
      return;
    }
    const session = generation;
    reading.add(id);
    try {
      const data = await request<{
        channelId: string;
        lastReadMessageId: string;
      }>(`${channelPath(id)}/read`, {
        method: "PUT",
        body: JSON.stringify({ lastReadMessageId: messageId }),
      });
      if (session === generation) applyEvent({ type: "channel.read", data });
    } catch (cause) {
      if (session === generation)
        error = `Could not mark messages read: ${String(cause)}`;
    } finally {
      reading.delete(id);
      const pending = pendingReads.get(id);
      pendingReads.delete(id);
      if (session === generation && pending && pending !== messageId)
        void markRead(id, pending);
    }
  }

  let stopConnection: (() => void) | undefined;

  function connect() {
    if (stopConnection) return stopConnection;
    let stopped = false;
    let socket: WebSocket | undefined;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let attempt = 0;

    function scheduleReconnect(session: number) {
      if (stopped || session !== generation) return;
      connected = false;
      syncing = false;
      // Invalidate requests from the disconnected session.
      generation++;
      timer = setTimeout(
        () => void openConnection(),
        Math.min(1000 * 2 ** attempt++, 15000),
      );
    }

    async function openConnection() {
      if (stopped) return;
      clearTimeout(timer);
      const session = ++generation;
      connected = false;
      syncing = true;
      loading = false;
      // A browser cannot expose the HTTP status of a rejected WS upgrade.
      // Check authorization over HTTP before attempting the connection.
      try {
        await request<Bootstrap>("/bootstrap");
      } catch (cause) {
        if (stopped || session !== generation) return;
        error = cause instanceof ApiError && cause.status === 401
          ? unauthorizedMessage : `Could not load chat: ${String(cause)}`;
        syncing = false;
        if (!(cause instanceof ApiError && cause.status === 401)) scheduleReconnect(session);
        return;
      }
      if (stopped || session !== generation) return;
      let unauthorized = false;
      const ws = new WebSocket(
        `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/api/ws`,
      );
      socket = ws;
      let ready = false;
      const buffered: ServerEvent[] = [];
      const isCurrent = () =>
        !stopped && session === generation && ws.readyState === WebSocket.OPEN;

      ws.onmessage = (event) => {
        if (stopped || session !== generation) return;
        try {
          const parsed = JSON.parse(event.data) as ServerEvent;
          if (!ready) buffered.push(parsed);
          else applyEvent(parsed);
        } catch {
          error = "Received an invalid server event.";
        }
      };
      ws.onopen = async () => {
        try {
          // Refresh after subscribing, then merge buffered events so the
          // preflight-to-socket interval cannot leave a gap in state.
          const bootstrap = await request<Bootstrap>("/bootstrap");
          if (!isCurrent()) return;
          userMap = {};
          cacheUsers([bootstrap.currentUser]);
          currentUserId = bootstrap.currentUser.id;
          channels = bootstrap.channels;
          onlineUserIds = [];
          if (!channels.some((channel) => channel.id === activeChannel))
            activeChannel = channels[0]?.id ?? "";
          // Drop stale caches on reconnect: there is no server event replay.
          history = {};
          cursors = {};
          if (activeChannel) {
            const id = activeChannel;
            const page = await request<MessagePage>(
              `${channelPath(id)}/messages`,
            );
            if (!isCurrent()) return;
            cacheUsers(page.users);
            history[id] = page.messages;
            cursors[id] = page.nextBefore;
          }
          for (const event of buffered) applyEvent(event);
          ready = true;
          connected = true;
          syncing = false;
          error = "";
          attempt = 0;
        } catch (cause) {
          if (!isCurrent()) return;
          unauthorized = cause instanceof ApiError && cause.status === 401;
          error = unauthorized ? unauthorizedMessage : `Could not load chat: ${String(cause)}`;
          ws.close();
        }
      };
      ws.onerror = () => {
        if (!stopped && session === generation && !unauthorized)
          error = "Cannot connect to the chat server.";
      };
      ws.onclose = (event) => {
        if (stopped || session !== generation) return;
        connected = false;
        syncing = false;
        if (unauthorized || event.code === 4401) {
          error = unauthorizedMessage;
          generation++;
          return;
        }
        // Failed handshakes are opaque; the next attempt checks HTTP auth again.
        scheduleReconnect(session);
      };
    }

    retryConnection = () => {
      generation++;
      socket?.close();
      void openConnection();
    };
    void openConnection();
    stopConnection = () => {
      stopped = true;
      generation++;
      clearTimeout(timer);
      socket?.close();
      connected = false;
      syncing = false;
      retryConnection = () => {};
      stopConnection = undefined;
    };
    return stopConnection;
  }

  return {
    get channels() {
      return channels;
    },
    get activeChannel() {
      return activeChannel;
    },
    get users() {
      return userMap;
    },
    get onlineUsers() {
      return onlineUsers;
    },
    get unreadChannels() {
      return unreadChannels;
    },
    get messages() {
      return history[activeChannel] ?? [];
    },
    get hasOlder() {
      return !!cursors[activeChannel];
    },
    get connected() {
      return connected;
    },
    get syncing() {
      return syncing;
    },
    get loading() {
      return loading;
    },
    get error() {
      return error;
    },
    connect,
    selectChannel,
    loadOlder,
    send,
    markRead,
    retry: () => retryConnection(),
  };
}
