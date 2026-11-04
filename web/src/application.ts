import { TrimApp } from '@trimjs/web-app';

const sdk = new TrimApp();

/** Close this application page only; the NAS service and other windows stay alive. */
export async function closeApplication(
  stillEmpty: () => boolean,
  fallback: () => void,
): Promise<void> {
  if (!stillEmpty()) return;
  if (sdk.isStandaloneWeb) {
    window.close();
    // User-opened browser tabs may refuse window.close().
    if (!window.closed && stillEmpty()) fallback();
    return;
  }
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    // An unsupported iframe may never finish the SDK's host handshake.
    await Promise.race([
      sdk.ready(),
      new Promise<never>((_, reject) => {
        timer = setTimeout(() => reject(new Error('Host bridge unavailable')), 8000);
      }),
    ]);
    if (stillEmpty()) await sdk.close();
  } catch {
    if (stillEmpty()) fallback();
  } finally {
    clearTimeout(timer);
  }
}
