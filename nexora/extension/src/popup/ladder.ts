// The RPC retry ladder a UI action rides on, ported verbatim from
// omega-target-chromium-extension/src/coffee/omega_target_web.coffee:50-77 — ADR-0016 rule 6 and
// packages/schema/presentation.json#/popup/coldStart both pin it, including the branch on the exact
// error string. Simplifying it reintroduces regression #276: a popup that renders only the toolbar
// icon while the worker refuses to wake.
//
// The constants and the two formulas are the ledger's own numbers (coldStart.maxAttempts 5,
// maxWorkerDownAttempts 9, quickDelay 100 * attempt, workerDownDelay 2000 * 2^(attempt - maxAttempts - 1),
// ≈15 s total), and the worker-down detection matches the exact string "Receiving end does not exist".

export const MAX_ATTEMPTS = 5;
export const MAX_WORKER_DOWN_ATTEMPTS = 9;
export const WORKER_DOWN_MATCH = 'Receiving end does not exist';

export function isWorkerDown(message: string | undefined): boolean {
  return typeof message === 'string' && message.indexOf(WORKER_DOWN_MATCH) >= 0;
}

export function ladderDelay(workerDown: boolean, attempt: number): number {
  if (workerDown && attempt > MAX_ATTEMPTS) {
    return 2000 * Math.pow(2, attempt - MAX_ATTEMPTS - 1);
  }
  return 100 * attempt;
}

export function ladderMax(workerDown: boolean): number {
  return workerDown ? MAX_WORKER_DOWN_ATTEMPTS : MAX_ATTEMPTS;
}

/**
 * sendWithLadder rides one UI action up the ladder. `transmit` is the attempt body — it resolves with
 * the answer or rejects with the error; a worker-down rejection keeps retrying with the long backoff,
 * every other rejection retries the short ladder, and exhausting either rejects with the last error.
 * The timers come from `schedule` so a test can run the ladder on a fake clock.
 */
export function sendWithLadder<T>(
  transmit: () => Promise<T>,
  schedule: (fn: () => void, ms: number) => void = (fn, ms) => setTimeout(fn, ms),
): Promise<T> {
  const attemptOnce = (attempt: number): Promise<T> =>
    transmit().catch((error) => {
      const workerDown = isWorkerDown(error instanceof Error ? error.message : String(error));
      const max = ladderMax(workerDown);
      if (attempt >= max) throw error;
      return new Promise<T>((resolveLadder, rejectLadder) => {
        schedule(() => {
          attemptOnce(attempt + 1).then(resolveLadder, rejectLadder);
        }, ladderDelay(workerDown, attempt));
      });
    });
  return attemptOnce(1);
}
