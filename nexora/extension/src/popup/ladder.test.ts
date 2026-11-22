import { test } from 'node:test';
import { strict as assert } from 'node:assert';
import { readFileSync } from 'node:fs';
import { ladderDelay, ladderMax, sendWithLadder, MAX_ATTEMPTS, MAX_WORKER_DOWN_ATTEMPTS, WORKER_DOWN_MATCH } from './ladder.ts';

// The ladder queues its next timer from a rejection microtask, so the drain is asynchronous too.
async function drain(timers: Array<() => void>, rounds = 40): Promise<void> {
  for (let i = 0; i < rounds; i++) {
    while (timers.length) timers.shift()!();
    await new Promise((done) => setTimeout(done, 0));
  }
}

// The ladder is ported verbatim (ADR-0016 rule 6; presentation.json#/popup/coldStart): the constants,
// the two formulas, and the branch on the exact error string. Simplifying it reintroduces regression
// #276 — a popup that renders only the toolbar icon while the worker refuses to wake.

test('the constants match the ledger data', () => {
  const pres = JSON.parse(readFileSync(new URL('../../../../packages/schema/presentation.json', import.meta.url), 'utf8')).popup.coldStart;
  assert.equal(MAX_ATTEMPTS, pres.maxAttempts);
  assert.equal(MAX_WORKER_DOWN_ATTEMPTS, pres.maxWorkerDownAttempts);
  assert.equal(WORKER_DOWN_MATCH, pres.errorMatch);
  assert.equal(ladderDelay(false, 3), 300);
  assert.equal(ladderDelay(true, 6), 2000 * Math.pow(2, 6 - 5 - 1));
});

test('a worker-down rejection rides the long ladder to nine attempts', async () => {
  const delays: number[] = [];
  const timers: Array<() => void> = [];
  const schedule = (fn: () => void, ms: number) => {
    delays.push(ms);
    timers.push(fn);
  };
  let attempts = 0;
  const result = sendWithLadder(async () => {
    attempts++;
    throw new Error('Receiving end does not exist');
  }, schedule);
  const settled: 'pending' | 'rejected' = 'pending';
  void result.catch(() => {});
  // Drive the ladder by hand: fire every queued timer until none is left.
  await drain(timers);
  assert.equal(attempts, MAX_WORKER_DOWN_ATTEMPTS, 'the worker-down ladder runs to nine attempts');
  assert.equal(delays.length, MAX_WORKER_DOWN_ATTEMPTS - 1);
  assert.equal(ladderDelay(true, MAX_WORKER_DOWN_ATTEMPTS), 2000 * Math.pow(2, 3), 'the ≈15 s ceiling shapes the last delay');
  void settled;
  await assert.rejects(result);
});

test('a non-worker-down rejection stops at five attempts with the short delays', async () => {
  let attempts = 0;
  const delays: number[] = [];
  const timers: Array<() => void> = [];
  const result = sendWithLadder(async () => {
    attempts++;
    throw new Error('BadRequest: something else');
  }, (fn, ms) => {
    delays.push(ms);
    timers.push(fn);
  });
  void result.catch(() => {});
  await drain(timers);
  assert.equal(attempts, MAX_ATTEMPTS);
  assert.deepEqual(delays, [100, 200, 300, 400]);
  await assert.rejects(result);
});

test('a transmission that succeeds on a retry resolves', async () => {
  const timers: Array<() => void> = [];
  let attempts = 0;
  const result = sendWithLadder(async () => {
    attempts++;
    if (attempts < 3) throw new Error('Receiving end does not exist');
    return 'answer';
  }, (fn) => timers.push(fn));
  await drain(timers);
  assert.equal(await result, 'answer');
});
