// The ConnectorClient of docs/architecture/22-component-contracts.md §1: the single connection ExtensionShell owns,
// with the deadline, the request table and the reconnect loop the framing contract describes.
//
// The port is injected rather than reached for, because `chrome.runtime` does not exist in a test runner and this
// package is framework-agnostic by docs/architecture/20-target-repository-layout.md §3. In the worker the caller
// passes `() => chrome.runtime.connectNative(hostName)`; here a fake port drives the same code.
//
// No frame codec is used on this hop. The browser writes and reads the four-byte length prefix itself
// (docs/architecture/25-native-messaging-protocol.md §3), so `postMessage` takes the envelope object and a prefix
// written here would be four bytes of payload the host would try to parse as JSON.
import {
  type ErrorCode,
  type Json,
  type MethodName,
  type RawEnvelope,
  extensionToHostBytes,
  frameFormatVersion,
  maxConcurrentRequests,
  maxInFlightBytes,
  methodCapabilities,
  methodTimeoutMs,
  minCompatibleProtocolVersion,
  protocolVersion,
  reconnectBackoffSeconds,
  reconnectJitterPercent,
  retryableErrors,
} from '../../protocol/src/contract.ts';

export type Port = {
  postMessage(message: unknown): void;
  onMessage: { addListener(listener: (message: unknown) => void): void };
  onDisconnect: { addListener(listener: () => void): void };
  disconnect(): void;
};

export type Clock = {
  now(): number;
  setTimeout(fn: () => void, ms: number): unknown;
  clearTimeout(handle: unknown): void;
};

export type ClientState = 'idle' | 'connecting' | 'ready' | 'waiting' | 'incompatible' | 'stopped';

export type Reconcile = { expected: string; applied: string | null; matches: boolean };

export type Options = {
  connect: () => Port;
  extensionVersion: string;
  clock?: Clock;
  random?: () => number;
  onEvent?: (frame: RawEnvelope) => void;
  onState?: (state: ClientState, reason: string) => void;
  onReconcile?: (result: Reconcile) => void;
};

export class ClientError extends Error {
  code: ErrorCode;
  retryable: boolean;

  constructor(code: ErrorCode, message: string) {
    super(message);
    this.name = 'ClientError';
    this.code = code;
    this.retryable = retryableErrors[code];
  }
}

type Pending = {
  resolve: (result: Json) => void;
  reject: (error: ClientError) => void;
  timer: unknown;
  bytes: number;
};

const systemClock: Clock = {
  now: () => Date.now(),
  setTimeout: (fn, ms) => setTimeout(fn, ms),
  clearTimeout: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
};

export class ConnectorClient {
  private readonly options: Options;
  private readonly clock: Clock;
  private readonly random: () => number;
  private readonly sessionTag: string;

  private port: Port | null = null;
  private current: ClientState = 'idle';
  private reason = '';
  private readonly pending = new Map<string, Pending>();
  private inFlightBytes = 0;
  private seq = 0;
  private idCounter = 0;
  private attempt = 0;
  private reconnectTimer: unknown = null;
  private capabilities = new Set<string>();
  private appliedConfigHash: string | null = null;

  constructor(options: Options) {
    this.options = options;
    this.clock = options.clock ?? systemClock;
    this.random = options.random ?? Math.random;
    this.sessionTag = Math.floor(this.random() * 0xffffffff).toString(36).padStart(7, '0');
  }

  get state(): ClientState {
    return this.current;
  }

  get stateReason(): string {
    return this.reason;
  }

  // The hash the extension believes is applied. Recording it is what makes a reconnect a reconcile rather than a
  // blind reconfigure (docs/architecture/22-component-contracts.md §1 invariant 3).
  recordAppliedConfigHash(hash: string | null): void {
    this.appliedConfigHash = hash;
  }

  start(): void {
    if (this.current !== 'idle' && this.current !== 'stopped') return;
    this.attempt = 0;
    this.open();
  }

  stop(): void {
    this.setState('stopped', 'the client was stopped');
    this.cancelReconnect();
    this.teardown(new ClientError('NetworkUnreachable', 'the client stopped before this request was answered'));
  }

  async request(method: MethodName, args: Json = null): Promise<Json> {
    if (this.current !== 'ready') {
      throw new ClientError('HandshakeFailed', `${method} needs a negotiation in force and the client is ${this.current}: ${this.reason}`);
    }
    for (const need of methodCapabilities[method]) {
      if (!this.capabilities.has(need)) {
        throw new ClientError('HandshakeFailed', `${method} needs the ${need} capability, which this hello did not announce`);
      }
    }
    return this.send(method, args);
  }

  private open(): void {
    this.setState('connecting', '');
    const port = this.options.connect();
    this.port = port;
    port.onMessage.addListener((message) => this.onMessage(message));
    port.onDisconnect.addListener(() => this.onDisconnect());
    void this.negotiate();
  }

  private async negotiate(): Promise<void> {
    // The port this hello rode. A rejection or a late answer that arrives after a teardown belongs to a session
    // that is already over, and acting on it would close the port the reconnect just opened.
    const port = this.port;
    let result: Json;
    try {
      result = await this.send('connector.hello', {
        extensionVersion: this.options.extensionVersion,
        protocolVersion,
        minCompatibleProtocolVersion,
      });
    } catch {
      if (this.port === port) this.onDisconnect();
      return;
    }
    if (this.port !== port) return;
    const hello = result as { protocolVersion: number; minCompatibleProtocolVersion: number; capabilities: string[] };
    if (minCompatibleProtocolVersion > hello.protocolVersion || hello.minCompatibleProtocolVersion > protocolVersion) {
      this.setState('incompatible', `VersionMismatch: the connector speaks ${hello.protocolVersion} (min ${hello.minCompatibleProtocolVersion}), this extension speaks ${protocolVersion} (min ${minCompatibleProtocolVersion})`);
      this.teardown(new ClientError('VersionMismatch', this.reason));
      return;
    }
    this.capabilities = new Set(hello.capabilities);
    this.attempt = 0;
    this.setState('ready', '');
    await this.reconcile();
  }

  // The reconcile step of framing.json#/reconnect/afterReconnect. The comparison is local because
  // config.snapshot#/args carries no expectation field, so its matchesExtensionHash is null by its own description;
  // a hash equality has no clock in it, so comparing here is not the clock-skewed comparison that field warns about.
  private async reconcile(): Promise<void> {
    const expected = this.appliedConfigHash;
    if (expected === null) return;
    let applied: string | null = null;
    try {
      const result = await this.request('config.snapshot', {});
      applied = (result as { appliedConfigHash?: string }).appliedConfigHash ?? null;
    } catch {
      // Not learning what is applied is a mismatch, never an assumption that nothing changed.
      applied = null;
    }
    this.options.onReconcile?.({ expected, applied, matches: applied === expected });
  }

  private send(method: MethodName, args: Json): Promise<Json> {
    const port = this.port;
    if (port === null) {
      throw new ClientError('NetworkUnreachable', `${method} has no port to travel on`);
    }
    if (this.pending.size >= maxConcurrentRequests) {
      throw new ClientError('Busy', `${method} would be request ${this.pending.size + 1} of a ${maxConcurrentRequests}-request window`);
    }
    const id = this.nextId();
    const envelope = {
      v: frameFormatVersion,
      id,
      type: 'request' as const,
      method,
      args,
      sentAt: new Date(this.clock.now()).toISOString(),
      seq: this.seq,
    };
    const bytes = new TextEncoder().encode(JSON.stringify(envelope)).byteLength;
    if (bytes > extensionToHostBytes) {
      // §5's chunked envelope is the mechanism for a body this size and this client does not implement it, so the
      // refusal happens before the write rather than as a port the browser closes.
      throw new ClientError('BadRequest', `${method} is ${bytes} B, over the ${extensionToHostBytes} B extension-to-host ceiling; it needs the chunked envelope`);
    }
    if (this.inFlightBytes + bytes > maxInFlightBytes) {
      throw new ClientError('Busy', `${method} would put ${this.inFlightBytes + bytes} B in flight, over the ${maxInFlightBytes} B window`);
    }

    return new Promise<Json>((resolve, reject) => {
      const timer = this.clock.setTimeout(() => this.expire(id, method), methodTimeoutMs[method]);
      this.pending.set(id, { resolve, reject, timer, bytes });
      this.inFlightBytes += bytes;
      this.seq += 1;
      port.postMessage(envelope);
    });
  }

  private nextId(): string {
    this.idCounter += 1;
    return `req-${this.idCounter.toString(36).padStart(6, '0')}-${this.sessionTag}`;
  }

  private expire(id: string, method: MethodName): void {
    const entry = this.take(id);
    if (entry === null) return;
    entry.reject(new ClientError('Timeout', `${method} was not answered within its ${methodTimeoutMs[method]} ms deadline; a late answer is discarded by id`));
  }

  private take(id: string): Pending | null {
    const entry = this.pending.get(id);
    if (entry === undefined) return null;
    this.pending.delete(id);
    this.inFlightBytes -= entry.bytes;
    this.clock.clearTimeout(entry.timer);
    return entry;
  }

  private onMessage(message: unknown): void {
    if (message === null || typeof message !== 'object') return;
    const frame = message as RawEnvelope;
    if (frame.type === 'response') {
      // A response whose id left the table — timed out, or answered twice — is discarded, and its side effect is
      // not re-applied (docs/architecture/25-native-messaging-protocol.md §9).
      const entry = this.take(frame.inReplyTo ?? '');
      if (entry === null) return;
      if (frame.ok) entry.resolve(frame.result ?? null);
      else entry.reject(new ClientError(frame.error?.code ?? 'Internal', frame.error?.message ?? 'the connector refused without a code'));
      return;
    }
    if (frame.type === 'event' || frame.type === 'progress') this.options.onEvent?.(frame);
  }

  private onDisconnect(): void {
    if (this.current === 'stopped' || this.current === 'incompatible') return;
    this.teardown(new ClientError('NetworkUnreachable', 'the port closed; the connector and Core keep running (ADR-0005)'));
    const index = Math.min(this.attempt, reconnectBackoffSeconds.length - 1);
    this.attempt += 1;
    const jitter = 1 + (this.random() * 2 - 1) * (reconnectJitterPercent / 100);
    const delay = Math.round(reconnectBackoffSeconds[index] * 1000 * jitter);
    this.setState('waiting', `reconnecting in ${delay} ms (attempt ${this.attempt})`);
    this.reconnectTimer = this.clock.setTimeout(() => {
      this.reconnectTimer = null;
      this.open();
    }, delay);
  }

  private teardown(error: ClientError): void {
    const port = this.port;
    this.port = null;
    this.capabilities = new Set();
    for (const id of [...this.pending.keys()]) this.take(id)?.reject(error);
    port?.disconnect();
  }

  private cancelReconnect(): void {
    if (this.reconnectTimer === null) return;
    this.clock.clearTimeout(this.reconnectTimer);
    this.reconnectTimer = null;
  }

  private setState(state: ClientState, reason: string): void {
    this.current = state;
    this.reason = reason;
    this.options.onState?.(state, reason);
  }
}
