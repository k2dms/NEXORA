// The browser-plane temp-rule store, shaped after the legacy Options#addTempRule
// (omega-target/src/options.coffee:920-960): one rule per domain, routed to a profile, toggled. The
// decision (PROJECT-STATE §4.143) puts the temp rule in the legacy engine's own path — the legacy
// engine stays the browser-plane truth until the cut-over (ADR-0013) — so the document this store
// persists is the legacy `_tempProfile` shape: a SwitchProfile whose rules are HostWildcardCondition
// entries for each domain. The apply path that compiles it (omega_pac, shipped) is the remaining half
// of the slice; the store is what the apply path will read.
//
// Persistence is chrome.storage.session: temp rules are ephemeral by name — the legacy profile was
// in-memory (Options#_tempProfile) — and session storage keeps them out of the envelope, out of sync
// and out of the key registry, because a temporary rule is not user intent to migrate.

export interface TempRule {
  condition: { conditionType: 'HostWildcardCondition'; pattern: string };
  profileName: string;
}

/** The legacy `_tempProfile` shape: a SwitchProfile the legacy path can compile as-is. */
export interface TempProfile {
  profileType: 'SwitchProfile';
  defaultProfileName: string;
  color?: string;
  rules: TempRule[];
}

export interface TempRulesStorage {
  get(keys?: string[] | null): Promise<Record<string, unknown>>;
  set(items: Record<string, unknown>): Promise<void>;
}

const STORE_KEY = 'tempRulesByDomain';

export function ruleFor(domain: string, profileName: string): TempRule {
  return { condition: { conditionType: 'HostWildcardCondition', pattern: '*.' + domain }, profileName };
}

export class TempRules {
  private readonly storage: TempRulesStorage;

  constructor(storage: TempRulesStorage) {
    this.storage = storage;
  }

  private async byDomain(): Promise<Map<string, string>> {
    const data = await this.storage.get([STORE_KEY]);
    const raw = data[STORE_KEY];
    return new Map(Object.entries((typeof raw === 'object' && raw !== null ? raw : {}) as Record<string, string>));
  }

  private async save(byDomain: Map<string, string>): Promise<void> {
    await this.storage.set({ [STORE_KEY]: Object.fromEntries(byDomain) });
  }

  /**
   * The legacy semantics, verbatim: `toggle` 1 forces the rule on, -1 forces it off, 0/null/undefined
   * toggles it. Adding a rule for a domain that is already routed to another profile moves the domain
   * to the new profile. A no-op reports so.
   */
  async toggle(domain: string, profileName: string, toggle?: 1 | -1 | 0): Promise<'added' | 'removed' | 'moved' | 'unchanged'> {
    if (!/^[A-Za-z0-9.-]+$/.test(domain)) {
      throw new Error('tempRules: the domain is not a hostname the pattern can carry');
    }
    const byDomain = await this.byDomain();
    const existing = byDomain.get(domain);
    if (toggle === 1 && existing === profileName) return 'unchanged';
    if (toggle === -1 && !existing) return 'unchanged';
    if (existing && existing === profileName && (toggle === undefined || toggle === 0)) {
      byDomain.delete(domain);
      await this.save(byDomain);
      return 'removed';
    }
    byDomain.set(domain, profileName);
    await this.save(byDomain);
    return existing && existing !== profileName ? 'moved' : 'added';
  }

  /** The `_tempProfile` document the legacy apply path compiles, built from the store. */
  async asProfile(defaultProfileName: string, color?: string): Promise<TempProfile> {
    const byDomain = await this.byDomain();
    const domains = [...byDomain.keys()].sort();
    return {
      profileType: 'SwitchProfile',
      defaultProfileName,
      color,
      rules: domains.map((domain) => ruleFor(domain, byDomain.get(domain)!)),
    };
  }
}
