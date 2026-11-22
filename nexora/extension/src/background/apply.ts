// The apply half of the temp-rule decision (PROJECT-STATE §4.143): the browser plane keeps the rule.
//
// What the legacy temp rule did — "this site goes where I say, temporarily" — has a NEXORA-native subset
// that needs no legacy runtime: while NEXORA's proxy is in force (fixed_servers to the Connector), a
// temp rule "this site goes direct" is an entry in the applied config's own bypass list. The base
// bypass list is the applied config's, and `<-loopback>` is never removed from it — the fail-closed
// bypass the extension applies must keep the loopback coverage, or a dead Connector stops being a
// block (doc 30's invariant).
//
// What this subset does not do, stated as the remaining half: a temp rule routing a site to another
// *profile* needs the profile's own proxy chain, which is the legacy apply path's to compile. Those
// rules are stored (the temp-rule store) and recorded as stored-not-applied in the verdict.

export interface ProxyRules {
  mode: string;
  rules?: {
    singleProxy?: { scheme: string; host: string; port?: number };
    bypassList?: string[];
  };
}

const LOOPBACK_GUARD = '<-loopback>';

/** The applied bypass list for one temp rule: the base list plus the host, with `<-loopback>` always
 * first. A rule the base list already bypasses produces the same list — a no-op is a no-op. */
export function bypassWith(base: string[], domain: string): string[] {
  const host = domainToBypass(domain);
  if (base.includes(host)) return [...base];
  // `<-loopback>` is the fail-closed coverage; it stays first so a review sees it without reading.
  const withoutGuard = base.filter((entry) => entry !== LOOPBACK_GUARD);
  return [LOOPBACK_GUARD, host, ...withoutGuard];
}

/** The applied bypass list without the domain's entry — restoring the base list exactly. */
export function bypassWithout(base: string[], domain: string): string[] {
  const host = domainToBypass(domain);
  const out = base.filter((entry) => entry !== host);
  if (!out.includes(LOOPBACK_GUARD)) return [LOOPBACK_GUARD, ...out];
  return out;
}

function domainToBypass(domain: string): string {
  if (!/^[A-Za-z0-9.-]+$/.test(domain)) {
    throw new Error('apply: the domain is not a hostname a bypass entry can carry');
  }
  return "*." + domain;
}

/** The verdict the background answers the popup with: what happened to the rule on the applied plane. */
export type ApplyVerdict = 'applied' | 'restored' | 'stored' | 'unchanged';

export function nextVerdict(applied: boolean, before: 'added' | 'removed' | 'moved' | 'unchanged'): ApplyVerdict {
  if (before === 'unchanged') return 'unchanged';
  return applied ? (before === 'removed' ? 'restored' : 'applied') : 'stored';
}
