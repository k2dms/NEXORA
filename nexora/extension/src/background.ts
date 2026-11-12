import { hostName } from '../../../packages/protocol/src/contract.ts';
import { Connection, type RuntimeRecord, type Selection } from './connection.ts';
import { channel, selectedKey, runtimeKey, type Action, type SavedSubscription, type SavedServer } from './model.ts';

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

chrome.proxy.settings.onChange.addListener((details) => {
  if (connection.snapshot().phase !== 'connected' && connection.snapshot().phase !== 'blocked') return;
  void connection.preserveProxy(details.value, details.levelOfControl);
});

void connection.action('status');
