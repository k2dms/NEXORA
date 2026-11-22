import { test } from 'node:test';
import { strict as assert } from 'node:assert';
import { bypassWith, bypassWithout, nextVerdict } from './apply.ts';

// The base bypass list is the applied config's own: `<-loopback>` plus whatever the failure policy
// bypassed. The loopback guard is the fail-closed coverage — it must survive every temp rule.

const base = ['<-loopback>'];

test('a temp rule adds the host to the bypass list with the loopback guard first', () => {
  assert.deepEqual(bypassWith(base, 'example.com'), ['<-loopback>', '*.example.com']);
});

test('a second rule keeps the guard first and appends after', () => {
  const withOne = bypassWith(base, 'a.test');
  assert.deepEqual(bypassWith(withOne, 'b.test'), ['<-loopback>', '*.b.test', '*.a.test']);
});

test('a rule the base already bypasses is a no-op', () => {
  assert.deepEqual(bypassWith([...base, '*.example.com'], 'example.com'), [...base, '*.example.com']);
});

test('removing a rule restores the base list exactly', () => {
  assert.deepEqual(bypassWithout(['<-loopback>', '*.example.com', '*.other.test'], 'example.com'), ['<-loopback>', '*.other.test']);
  assert.deepEqual(bypassWithout(base, 'example.com'), base);
});

test('a non-hostname is refused before the list is built', () => {
  assert.throws(() => bypassWith(base, 'not a host;el'), /not a hostname/);
});

test('the verdict says what happened on the applied plane', () => {
  assert.equal(nextVerdict(true, 'added'), 'applied');
  assert.equal(nextVerdict(true, 'removed'), 'restored');
  assert.equal(nextVerdict(false, 'added'), 'stored');
  assert.equal(nextVerdict(false, 'unchanged'), 'unchanged');
});
