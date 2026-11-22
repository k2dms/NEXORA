// "Apply, dirty and discard as one model" (packages/schema/presentation.json#/parity/rows, surface
// settings): editing is not applying. `config.apply` is a replace-class command, so a dirty buffer and
// an applied state must both be visible at all times — the applied profile keeps serving while the form
// carries edits, the dirty fact is rendered, and discard returns the buffer to the applied values.
//
// The comparison is field-by-field against the saved server's own form projection, not against a
// reference of the input object: a field the form normalises (the port's number-ness) must compare as
// the user's value would apply, which is why the port comparison is numeric.

import type { SavedServer, ServerForm } from './model.ts';

export function formOfServer(server: SavedServer): ServerForm {
  return {
    displayName: server.displayName,
    address: server.address,
    port: server.port,
    userId: server.userIdRef,
    serverName: server.reality?.serverName ?? server.tls?.serverName ?? '',
    publicKey: server.reality?.publicKey ?? '',
    shortId: server.reality?.shortId ?? '',
    flow: server.flow === 'xtls-rprx-vision',
  };
}

export function isDirty(form: ServerForm, applied: ServerForm | null): boolean {
  if (!applied) return false;
  if (form.displayName !== applied.displayName) return true;
  if (form.address.trim() !== applied.address) return true;
  if (Number(form.port) !== applied.port) return true;
  if (form.userId !== applied.userId) return true;
  if (form.serverName !== applied.serverName) return true;
  if (form.publicKey !== applied.publicKey) return true;
  if ((form.shortId ?? '') !== (applied.shortId ?? '')) return true;
  if (Boolean(form.flow) !== Boolean(applied.flow)) return true;
  return false;
}
