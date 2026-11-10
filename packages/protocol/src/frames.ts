// The frame codec — the one artefact docs/architecture/25-native-messaging-protocol.md §2 says both sides of the
// pipe must agree on, because "a bug in the codec is a bug on both sides". It is the TypeScript mirror of
// connector/internal/frames, and packages/protocol/testdata/framed.jsonl is the fixture the Go test reads to prove
// the two implementations produce the same bytes.
import { extensionToHostBytes, hostToExtensionBytes, prefixBytes } from './contract.ts';

export type Direction = 'extension-to-host' | 'host-to-extension';

export type FrameFailureKind = 'closed' | 'empty' | 'truncated' | 'too-large' | 'not-an-object';

export class FrameFailure extends Error {
  kind: FrameFailureKind;
  limit: number;

  constructor(kind: FrameFailureKind, message: string, limit = 0) {
    super(message);
    this.name = 'FrameFailure';
    this.kind = kind;
    this.limit = limit;
  }
}

// The direction decides the number, and the numbers are not symmetric: 1 MiB out of the extension against 1 GiB
// into it, so a codec that reads and writes the same ceiling is wrong on both halves of the contract.
export function limitFor(direction: Direction): number {
  return direction === 'extension-to-host' ? extensionToHostBytes : hostToExtensionBytes;
}

// A frame is the 4-byte unsigned little-endian length of the JSON payload, then the payload.
export function encodeFrame(payload: Uint8Array, direction: Direction): Uint8Array {
  const limit = limitFor(direction);
  if (payload.byteLength === 0) {
    throw new FrameFailure('empty', 'frames: an empty payload is not a frame', limit);
  }
  if (payload.byteLength > limit) {
    // The sender refuses before writing; a receiver that gets one anyway is a transport error, not a truncation.
    throw new FrameFailure('too-large', `frames: payload of ${payload.byteLength} B exceeds the ${direction} limit of ${limit} B`, limit);
  }
  const frame = new Uint8Array(prefixBytes + payload.byteLength);
  new DataView(frame.buffer, frame.byteOffset, prefixBytes).setUint32(0, payload.byteLength, true);
  frame.set(payload, prefixBytes);
  return frame;
}

// One frame is one object, never an array and never null (protocol/framing.json `encoding.payload`).
export function encodeEnvelope(envelope: unknown, direction: Direction): Uint8Array {
  if (envelope === null || typeof envelope !== 'object' || Array.isArray(envelope)) {
    throw new FrameFailure('not-an-object', 'frames: a frame carries a single JSON object', limitFor(direction));
  }
  return encodeFrame(new TextEncoder().encode(JSON.stringify(envelope)), direction);
}

export type FrameDecoder = {
  push(chunk: Uint8Array): Uint8Array[];
  end(): void;
};

// The streaming half, because the pipe hands bytes in pieces: a frame that has not arrived in full is not an error,
// and a frame whose declared length is over the limit is an error the moment the prefix is known.
export function createFrameDecoder(limit: number): FrameDecoder {
  let buffer = new Uint8Array(0);
  let sawBytes = false;
  let broken = false;

  const fail = (failure: FrameFailure): never => {
    broken = true;
    throw failure;
  };

  return {
    push(chunk: Uint8Array): Uint8Array[] {
      if (broken) {
        throw new FrameFailure('closed', 'frames: the pipe is already broken; reconnect rather than push again', limit);
      }
      if (chunk.byteLength === 0) return [];
      sawBytes = true;
      const joined = new Uint8Array(buffer.byteLength + chunk.byteLength);
      joined.set(buffer);
      joined.set(chunk, buffer.byteLength);
      buffer = joined;

      const frames: Uint8Array[] = [];
      for (;;) {
        if (buffer.byteLength < prefixBytes) return frames;
        const length = new DataView(buffer.buffer, buffer.byteOffset, prefixBytes).getUint32(0, true);
        if (length > limit) {
          return fail(new FrameFailure('too-large', `frames: peer declared a ${length} B payload over the ${limit} B limit`, limit));
        }
        if (length === 0) {
          return fail(new FrameFailure('empty', 'frames: peer declared an empty payload', limit));
        }
        if (buffer.byteLength < prefixBytes + length) return frames;
        frames.push(buffer.slice(prefixBytes, prefixBytes + length));
        buffer = buffer.slice(prefixBytes + length);
      }
    },
    end(): void {
      if (broken) return;
      if (buffer.byteLength === 0) {
        if (!sawBytes) {
          throw new FrameFailure('closed', 'frames: the peer closed the pipe without a frame', limit);
        }
        return;
      }
      throw new FrameFailure('truncated', `frames: pipe ended ${buffer.byteLength} B into a frame`, limit);
    },
  };
}

// A payload off the pipe as an object, refusing anything that is not the single object a frame promises.
export function decodeEnvelope(payload: Uint8Array): unknown {
  const value: unknown = JSON.parse(new TextDecoder().decode(payload));
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    throw new FrameFailure('not-an-object', 'frames: a frame carries a single JSON object', 0);
  }
  return value;
}
