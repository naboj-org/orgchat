export type User = { id: string; username: string; affiliation?: string };
export type Channel = {
  id: string;
  name: string;
  unread: boolean;
  lastReadMessageId: string | null;
};
export type Message = {
  id: string;
  channelId: string;
  authorId: string;
  text: string;
  createdAt: string;
};
export type Bootstrap = { currentUser: User; channels: Channel[] };
export type MessagePage = {
  messages: Message[];
  users: User[];
  nextBefore: string | null;
};
export type MessageResult = { message: Message; author: User };
export type ServerEvent =
  | { type: "message.created"; data: MessageResult }
  | { type: "presence.snapshot"; data: { users: User[] } }
  | {
      type: "presence.changed";
      data: { user: User; online: true } | { userId: string; online: false };
    }
  | {
      type: "channel.read";
      data: { channelId: string; lastReadMessageId: string };
    };

export async function request<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const response = await fetch(`/api${path}`, {
    ...options,
    signal: AbortSignal.timeout(15000),
    headers: { "Content-Type": "application/json", ...options.headers },
  });
  if (!response.ok) {
    const body = await response.json().catch(() => null);
    throw new Error(
      body?.error?.message ?? `Request failed (${response.status})`,
    );
  }
  return response.json();
}

export const channelPath = (id: string) =>
  `/channels/${encodeURIComponent(id)}`;

export function mergeMessages(...groups: Message[][]): Message[] {
  return [
    ...new Map(groups.flat().map((message) => [message.id, message])).values(),
  ].sort((a, b) => a.createdAt.localeCompare(b.createdAt));
}
