import { hostName } from '../../../packages/protocol/src/contract.ts';
import { Connection, type RuntimeRecord, type Selection } from './connection.ts';
import { channel, selectedKey, runtimeKey, type Action, type SavedSubscription, type SavedServer } from './model.ts';
import { TempRules } from './background/tempRules.ts';
import { bypassWith, bypassWithout, nextVerdict, type ProxyRules } from './background/apply.ts';

const connection = new Connection({
  version: chrome.runtime.getManifest().version,
  connectNative() {
    const port = chrome.runtime.connectNative(hostName);
    port.onDisconnect.addListener(() => { void chrome.runtime.lastError; });
    return port;
  },
  async load() {
    const selected = (await chrome.storage.local.get(selectedKey))[selectedKey] as string | undefined;
    if (!selected) return null;
    const profileKey = `-nexora.profile.${selected}`;
    const profile = (await chrome.storage.local.get(profileKey))[profileKey] as Selection['profile'] | undefined;
    if (!profile) return null;
    const serverKey = `-nexora.server.${profile.defaultServerId}`;
    const server = (await chrome.storage.local.get(serverKey))[serverKey] as Selection['server'] | undefined;
    return server ? { profile, server } : null;
  },
  async save({ server, profile }) {
    await chrome.storage.local.set({
      [`-nexora.server.${server.id}`]: server,
      [`-nexora.profile.${profile.id}`]: profile,
      [selectedKey]: profile.id,
    });
  },
  async loadSubscription() {
    const selection = await this.load();
    const id = selection?.server.source === 'subscription' ? selection.server.subscriptionId : null;
    if (!id) return null;
    const key = `-nexora.subscription.${id}`;
    return (await chrome.storage.local.get(key))[key] as SavedSubscription | undefined ?? null;
  },
  async loadServers(ids) {
    const keys = ids.map((id) => `-nexora.server.${id}`);
    const stored = await chrome.storage.local.get(keys);
    return keys.map((key) => stored[key] as SavedServer | undefined).filter((server): server is SavedServer => !!server);
  },
  async saveSubscription(source, servers, selection) {
    // Auto is a transient parser declaration, never a stored source.
    if (source.format === 'auto' || source.url !== new URL(source.url).origin + '/') throw new Error('Подписка ещё не готова к сохранению.');
    const values: Record<string, unknown> = { [`-nexora.subscription.${source.id}`]: source, [selectedKey]: selection?.profile.id ?? null };
    for (const server of servers) values[`-nexora.server.${server.id}`] = server;
    if (selection) {
      values[`-nexora.server.${selection.server.id}`] = selection.server;
      values[`-nexora.profile.${selection.profile.id}`] = selection.profile;
    }
    await chrome.storage.local.set(values);
  },
  async readRuntime() { return (await chrome.storage.local.get(runtimeKey))[runtimeKey] as RuntimeRecord | undefined ?? null; },
  async writeRuntime(record) {
    if (record) await chrome.storage.local.set({ [runtimeKey]: record });
    else await chrome.storage.local.remove(runtimeKey);
  },
  async getProxy() { return await chrome.proxy.settings.get({ incognito: false }); },
  async setProxy(value) {
    await chrome.proxy.settings.set({ value, scope: 'regular' });
    const actual = await chrome.proxy.settings.get({ incognito: false });
    if (actual.levelOfControl !== 'controlled_by_this_extension' || actual.value.mode !== 'fixed_servers' || actual.value.rules?.singleProxy?.host !== '127.0.0.1' || actual.value.rules.singleProxy.port !== value.rules?.singleProxy?.port || actual.value.rules.singleProxy.scheme !== 'socks5' || actual.value.rules.bypassList?.length !== 1 || actual.value.rules.bypassList[0] !== '<-loopback>') throw new Error('Браузер не подтвердил локальный прокси. Не открывайте сайты до восстановления защиты.');
  },
  async restoreProxy(value) {
    await chrome.proxy.settings.set({ value, scope: 'regular' });
    const actual = await chrome.proxy.settings.get({ incognito: false });
    if (JSON.stringify(actual.value) !== JSON.stringify(value)) throw new Error('Браузер не подтвердил восстановление прежнего прокси. Закройте вкладки и восстановите управление настройками.');
  },
  async openSite(url) { await chrome.tabs.create({ url }); },
});

const actions = new Set<Action>(['status', 'save', 'subscribe', 'refresh', 'select', 'connect', 'disconnect', 'open-site', 'test-server']);
const answers = new Map<string, Promise<unknown>>();
// The browser-plane temp rules the decision of PROJECT-STATE §4.143 places in the legacy path: the
// store is the legacy _tempProfile shape, persisted ephemerally in session storage — temp rules are
// temporary, so they stay out of the envelope, out of sync and out of the key registry. The apply half
// (compiling the document through the shipped omega_pac) is the remaining slice.
const tempRules = new TempRules(chrome.storage.session);
const POPUP_URL = chrome.runtime.getURL('nexora-popup.html');
const browserPlaneActions = new Set(['add-rule', 'temp-rule', 'select-builtin']);

chrome.runtime.onMessage.addListener((message: unknown, sender, respond) => {
  if (!message || typeof message !== 'object' || !('channel' in message) || message.channel !== channel) return;
  if (sender.id !== chrome.runtime.id || sender.url !== chrome.runtime.getURL('nexora-options.html')) return;
  const request = message as { channel: unknown; action: Action; payload?: unknown; id: string };
  if (!actions.has(request.action) || typeof request.id !== 'string' || !/^[0-9a-f-]{36}$/.test(request.id)) {
    respond({ error: 'Неизвестный запрос NEXORA.' });
    return;
  }
  let answer = answers.get(request.id);
  if (!answer) {
    answer = connection.action(request.action, request.payload).then((view) => ({ view }), () => ({ error: 'Фоновый процесс не выполнил запрос.' }));
    answers.set(request.id, answer);
    if (answers.size > 64) answers.delete(answers.keys().next().value!);
  }
  void answer.then(respond);
  return true;
});

// The popup's browser-plane actions (doc 22 §13: the popup is a view; the shell owns the state). The
// sender is the popup document, the actions are the keyboard map's named ones, and the answer carries
// the store's own verdict so the popup can announce it. The apply half is the remaining slice.
chrome.runtime.onMessage.addListener((message: unknown, sender, respond) => {
  if (!message || typeof message !== 'object' || !('channel' in message) || message.channel !== channel) return;
  if (sender.id !== chrome.runtime.id || sender.url !== POPUP_URL) return;
  const request = message as { channel: unknown; action: string; payload?: { domain?: string; which?: string; profileName?: string }; id: string };
  if (!browserPlaneActions.has(request.action) || typeof request.id !== 'string' || !/^[0-9a-f-]{36}$/.test(request.id)) {
    respond({ error: 'Неизвестный запрос NEXORA.' });
    return;
  }
  const domain = typeof request.payload?.domain === 'string' ? request.payload.domain : '';
  if (request.action === 'select-builtin') {
    // The legacy builtins are the legacy engine's to apply; the popup announces the intent and the
    // shell-side apply is the remaining slice, recorded in PROJECT-STATE §4.143.
    respond({ result: { outcome: 'deferred', reason: 'select-builtin needs the legacy apply path (§4.143)' } });
    return true;
  }
  void tempRules.toggle(domain, request.payload?.profileName ?? 'direct', request.action === 'add-rule' ? 1 : undefined)
    .then(async (outcome) => {
      // The apply half: while NEXORA's proxy is in force, a "site goes direct" rule is an entry in the
      // applied fixed_servers' own bypass list, with `<-loopback>` never removed (doc 30's invariant).
      // With no Connector in force, the rule is stored and the verdict says so.
      const applied = await chrome.proxy.settings.get({ incognito: false });
      const inForce = applied.levelOfControl === 'controlled_by_this_extension' && applied.value.mode === 'fixed_servers';
      if (!inForce) {
        respond({ result: { outcome: nextVerdict(false, outcome), profileName: request.payload?.profileName ?? 'direct' } });
        return;
      }
      const current = applied.value as ProxyRules;
      const base = current.rules?.bypassList ?? [];
      const list = outcome === 'removed' ? bypassWithout(base, domain) : bypassWith(base, domain);
      const next = { ...current, mode: 'fixed_servers' as const, rules: { ...current.rules, bypassList: list } };
      await chrome.proxy.settings.set({ value: next as typeof applied.value, scope: 'regular' });
      const check = await chrome.proxy.settings.get({ incognito: false });
      const rules = (check.value as ProxyRules).rules?.bypassList ?? [];
      if (JSON.stringify(rules) !== JSON.stringify(list)) throw new Error('Браузер не подтвердил список обхода. Временное правило не применено.');
      respond({ result: { outcome: nextVerdict(true, outcome), bypassList: list } });
    })
    .catch((error: unknown) => respond({ error: error instanceof Error ? error.message : 'Неизвестный запрос NEXORA.' }));
  return true;
});

chrome.proxy.settings.onChange.addListener((details) => {
  if (connection.snapshot().phase !== 'connected' && connection.snapshot().phase !== 'blocked') return;
  void connection.preserveProxy(details.value, details.levelOfControl);
});

void connection.action('status');
