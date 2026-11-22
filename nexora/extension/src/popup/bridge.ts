// The popup's browser-plane bridge (doc 22 §13 invariant 4: the shell owns the state, the popup is a
// view). The keyboard map's named actions — add-rule, temp-rule, select-builtin — travel to the shell
// as runtime messages on the same channel the options page uses; the shell answers with the verdict
// the popup announces. The site the rule names is the active tab's own domain, read here — reading a
// tab's URL is not a side effect (invariant 2) — and a tab without a host refuses rather than guessing.

export interface BridgeResponse {
  result?: { outcome: string; reason?: string; bypassList?: string[] };
  error?: string;
}

export async function activeSiteDomain(): Promise<string> {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  const url = tab?.url;
  if (!url) throw new Error('Активная вкладка не отдала свой адрес; временное правило не знает, для какого сайта.');
  let host: string;
  try {
    host = new URL(url).hostname;
  } catch {
    throw new Error('Активная вкладка показывает не URL; временное правило не знает, для какого сайта.');
  }
  if (!host || host === 'localhost' || host === '127.0.0.1' || host === '::1') {
    throw new Error('Локальные адреса не требуют прокси — временное правило для них не имеет смысла.');
  }
  return host;
}

/** Send one browser-plane action to the shell. The channel and the id shape are the options page's own
 * (`nexora.connection.v1`, a UUID), so the shell routes both surfaces through one listener. */
export function sendBrowserPlane(action: 'add-rule' | 'temp-rule' | 'select-builtin', payload: Record<string, unknown> = {}): Promise<BridgeResponse> {
  return new Promise((resolve, reject) => {
    chrome.runtime.sendMessage(
      { channel: 'nexora.connection.v1', id: crypto.randomUUID?.() ?? 'br-' + Math.random().toString(36).slice(2, 14), action, payload },
      (response: BridgeResponse) => {
        void chrome.runtime.lastError;
        if (!response) reject(new Error('Фоновый процесс не ответил на browser-plane действие.'));
        else resolve(response);
      },
    );
  });
}

/** The one call the keyboard actions make: read the active site and ask the shell to toggle its temp
 * rule. Errors are the popup's to announce — a rule that names no site is not a rule. */
export async function toggleSiteRule(profileName = 'direct'): Promise<string> {
  const domain = await activeSiteDomain();
  const response = await sendBrowserPlane('temp-rule', { domain, profileName });
  if (response.error) throw new Error(response.error);
  return response.result?.outcome ?? 'unchanged';
}
