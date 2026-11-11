import { channel, type Action, type ConnectionView } from './model';

export function request(action: Action, payload?: unknown): Promise<ConnectionView> {
  if (!globalThis.chrome?.runtime?.id) {
    return Promise.resolve({ phase: 'disconnected', connector: 'stopped', server: null, servers: [], subscription: null, warning: null, pending: false, coreVersion: null, error: 'Откройте этот экран из установленного расширения NEXORA. Обычная вкладка не имеет доступа к Local Connector.' });
  }
  const id = crypto.randomUUID();
  return new Promise((resolve, reject) => {
    const send = (attempt: number) => {
      chrome.runtime.sendMessage({ channel, action, payload, id }, (response: { view?: ConnectionView; error?: string } | undefined) => {
        const error = chrome.runtime.lastError;
        if (error) {
          const workerDown = error.message?.includes('Receiving end does not exist');
          const max = workerDown ? 9 : 5;
          if (attempt < max) {
            const delay = workerDown && attempt > 5 ? 2000 * Math.pow(2, attempt - 5 - 1) : 100 * attempt;
            setTimeout(() => send(attempt + 1), delay);
          } else reject(new Error('Фоновый процесс расширения недоступен. Перезагрузите расширение и повторите действие.'));
          return;
        }
        if (response?.view) resolve(response.view);
        else reject(new Error(response?.error ?? 'Расширение не ответило на запрос.'));
      });
    };
    send(1);
  });
}
