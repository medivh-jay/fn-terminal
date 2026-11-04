export type ServerMessage =
  | { type: 'ready'; uid: number; username: string }
  | { type: 'exit'; message: string; reason: 'shell_exit' | 'session_end' };

export function chunkInput(text: string): Uint8Array<ArrayBuffer>[] {
  const bytes = new TextEncoder().encode(text);
  const chunks: Uint8Array<ArrayBuffer>[] = [];
  for (let offset = 0; offset < bytes.length; offset += 32768)
    chunks.push(bytes.subarray(offset, offset + 32768));
  return chunks;
}

export function parseServerMessage(text: string): ServerMessage | null {
  try {
    const value = JSON.parse(text);
    if (
      value?.type === 'ready' &&
      Number.isSafeInteger(value.uid) &&
      value.uid > 0 &&
      typeof value.username === 'string' &&
      value.username.length > 0
    )
      return { type: 'ready', uid: value.uid, username: value.username };
    if (value?.type === 'exit' && typeof value.message === 'string')
      return {
        type: 'exit',
        message: value.message,
        reason: value.reason === 'shell_exit' ? 'shell_exit' : 'session_end',
      };
  } catch {
    /* An invalid control message never changes connection state. */
  }
  return null;
}
