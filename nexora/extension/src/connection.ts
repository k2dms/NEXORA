import type { Json } from '../../../packages/protocol/src/contract.ts';
import { ConnectorClient, type Port } from '../../../packages/state/src/client.ts';
import settings from '../../../packages/schema/examples/settings.default.json' with { type: 'json' };
import failurePolicy from '../../../packages/schema/examples/failure-policy.default.json' with { type: 'json' };
import type { Action, ConnectionView, SavedProfile, SavedServer, SavedSubscription, ServerCheck, ServerForm, SubscriptionResult } from './model.ts';

export type Selection = { server: SavedServer; profile: SavedProfile };
export type ProxyValue = chrome.proxy.ProxyConfig;
export type RuntimeRecord = { previous: ProxyValue; proxy: ProxyValue | null; active: boolean; configHash: string; profileId: string };
export type Environment = {
  connectNative(): Port;
  version: string;
  load(): Promise<Selection | null>;
  save(selection: Selection): Promise<void>;
  loadSubscription(): Promise<SavedSubscription | null>;
  loadServers(ids: string[]): Promise<SavedServer[]>;
  saveSubscription(source: SavedSubscription, servers: SavedServer[], selection: Selection | null): Promise<void>;
  readRuntime(): Promise<RuntimeRecord | null>;
  writeRuntime(record: RuntimeRecord | null): Promise<void>;
  getProxy(): Promise<{ value: ProxyValue; levelOfControl: string }>;
  setProxy(value: ProxyValue): Promise<void>;
  restoreProxy(value: ProxyValue): Promise<void>;
  openSite(url: string): Promise<void>;
};

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const address = /^[A-Za-z0-9.\-:\[\]]{1,253}$/;
const hostname = /^[A-Za-z0-9.-]{1,253}$/;
const configHash = /^[0-9a-f]{64}$/i;

function loopbackProxy(proxy: ProxyValue | null): boolean {
  const rules = proxy?.rules;
  const single = rules?.singleProxy;
  return proxy?.mode === 'fixed_servers' && single?.scheme === 'socks5' && single.host === '127.0.0.1'
    && Number.isInteger(single.port) && single.port! >= 1 && single.port! <= 65535
    && !rules?.proxyForHttp && !rules?.proxyForHttps && !rules?.proxyForFtp && !rules?.fallbackProxy
    && rules?.bypassList?.length === 1 && rules.bypassList[0] === '<-loopback>';
}

function runtimeProof(status: unknown, record: RuntimeRecord, selection: Selection | null): string | null {
  if (typeof record.configHash !== 'string' || !configHash.test(record.configHash) || typeof record.profileId !== 'string' || !uuid.test(record.profileId)) return 'В сохранённом сеансе нет доказательства конфига и профиля. Прокси не сброшен; отключитесь явно.';
  if (!status || typeof status !== 'object') return 'Connector не подтвердил состояние Core. Прокси не сброшен.';
  const state = status as { running?: unknown; configHash?: unknown; selectedProfileId?: unknown; inboundPorts?: unknown };
  if (state.running !== true) return 'Core остановился или не подтвердил запуск. Прокси не сброшен; автоматический запуск отключён.';
  if (state.configHash !== record.configHash) return 'Конфиг Core не совпадает с сохранённым сеансом. Прокси не сброшен.';
  if (state.selectedProfileId !== record.profileId || selection?.profile.id !== record.profileId) return 'Профиль Core не совпадает с сохранённым и выбранным профилем. Прокси не сброшен.';
  if (!loopbackProxy(record.proxy) || !selection || typeof selection.server.id !== 'string' || !uuid.test(selection.server.id)) return 'Сохранённый локальный SOCKS5-прокси не подтверждён. Прокси не сброшен.';
  const tag = `in-srv-${selection.server.id.replaceAll('-', '').slice(0, 8)}`;
  const ports = Array.isArray(state.inboundPorts) ? state.inboundPorts.filter((item) => item && typeof item === 'object' && item.tag === tag) : [];
  if (ports.length !== 1 || ports[0].listen !== '127.0.0.1' || ports[0].port !== record.proxy!.rules!.singleProxy!.port) return 'Входящий порт Core не совпадает с сохранённым прокси выбранного сервера. Прокси не сброшен.';
  return null;
}

export function createSelection(value: unknown): { selection: Selection; userId: string } {
  if (!value || typeof value !== 'object') throw new Error('Введите параметры сервера.');
  const f = value as ServerForm;
  if (typeof f.displayName !== 'string' || !f.displayName.trim() || [...f.displayName.trim()].length > 80) throw new Error('Название должно содержать от 1 до 80 символов.');
  if (typeof f.address !== 'string' || !address.test(f.address) || !Number.isInteger(f.port) || f.port < 1 || f.port > 65535) throw new Error('Проверьте адрес и порт сервера.');
  if (typeof f.userId !== 'string' || !uuid.test(f.userId)) throw new Error('Введите UUID доступа к VLESS.');
  if (typeof f.serverName !== 'string' || !hostname.test(f.serverName) || typeof f.publicKey !== 'string' || !/^[A-Za-z0-9_-]{43}$/.test(f.publicKey)) throw new Error('Проверьте имя SNI и публичный ключ REALITY (43 символа).');
  if (typeof f.shortId !== 'string' || !/^([0-9a-f]{2}){0,8}$/.test(f.shortId) || typeof f.flow !== 'boolean') throw new Error('Short ID должен содержать 0–16 шестнадцатеричных символов, попарно.');
  const id = crypto.randomUUID();
  const profileId = crypto.randomUUID();
  const server: SavedServer = {
    id, displayName: f.displayName.trim(), protocol: 'vless', address: f.address, port: f.port,
    userIdRef: `keystore://server/${id}/user-id`, network: 'tcp', security: 'reality',
    flow: f.flow ? 'xtls-rprx-vision' : '',
    reality: { serverName: f.serverName, publicKey: f.publicKey, shortId: f.shortId, fingerprint: 'chrome' }, source: 'manual',
  };
  const profile: SavedProfile = {
    id: profileId, displayName: server.displayName, kind: 'connector', serverIds: [id], defaultServerId: id,
    selection: { mode: 'fixed' }, failover: { enabled: false }, failurePolicy: { coreDeath: { action: 'block' } },
  };
  return { selection: { server, profile }, userId: f.userId };
}

export function siteURL(value: unknown): string {
  if (!value || typeof value !== 'object' || typeof (value as { url?: unknown }).url !== 'string') throw new Error('Введите HTTPS-адрес сайта.');
  let url: URL;
  try { url = new URL((value as { url: string }).url); } catch { throw new Error('Введите полный HTTPS-адрес сайта.'); }
  if (url.protocol !== 'https:' || url.username || url.password) throw new Error('Разрешён только HTTPS-адрес без логина и пароля.');
  return url.href;
}

export function createSubscription(value: unknown): { source: SavedSubscription; url: string } {
  let url: string;
  try { url = siteURL(value); }
  catch { throw new Error('Введите полный HTTPS-адрес подписки без логина и пароля.'); }
  const parsed = new URL(url);
  const id = crypto.randomUUID();
  return {
    url,
    source: {
      id, displayName: parsed.hostname, url: parsed.origin + '/', urlRef: `keystore://subscription/${id}/url`,
      format: 'auto', limits: { maxBytes: 2097152, maxRedirects: 3, timeoutMs: 20000, maxServers: 512 },
      keepLastGood: true, schedule: { intervalSeconds: 3600 }, status: { consecutiveFailures: 0 }, serverIds: [],
    },
  };
}

function subscriptionStatusCode(code: string): string {
  if (code === 'HandshakeFailed') return 'NetworkUnreachable';
  return ['NetworkUnreachable', 'Timeout', 'AuthFailed', 'SchemaMismatch', 'QuotaExceeded', 'IntegrityMismatch', 'DiskFull', 'Internal'].includes(code) ? code : 'Internal';
}

function subscriptionWarning(code: string, partial: boolean): string {
  if (partial) return 'Часть записей подписки не поддерживается или превышает лимит. Доступны только разобранные TCP- и WebSocket-серверы.';
  if (code === 'AuthFailed') return 'Подписка недоступна: отказ в доступе или адрес больше не существует (в том числе HTTP 404). Прежний список сохранён.';
  if (code === 'SchemaMismatch') return 'Подписка содержит неподдерживаемые параметры. Прежний список и выбранный сервер сохранены.';
  if (code === 'QuotaExceeded') return 'Подписка превышает допустимый лимит. Прежний список сохранён.';
  return 'Не удалось обновить подписку. Прежний список и выбранный сервер сохранены. Повторите обновление позже.';
}

function failure(error: unknown): string {
  const code = error && typeof error === 'object' && 'code' in error ? String(error.code) : '';
  switch (code) {
    case 'IntegrityMismatch': return 'Xray не установлен или не прошёл проверку receipt. Нужна доверенная установка Core.';
    case 'KeystoreUnavailable': return 'Хранилище секретов Windows недоступно. Проверьте учётную запись Local Connector.';
    case 'HandshakeFailed': case 'NetworkUnreachable': case 'Timeout': return 'Local Connector не ответил. Проверьте установку Native Messaging и повторите действие.';
    case 'Busy': return 'Connector занят. Подождите и повторите действие.';
    case 'InvalidProfile': case 'SchemaMismatch': case 'BadRequest': return 'Connector отказался сохранять параметры сервера. Проверьте введённые значения.';
    case 'UnauthorizedExtension': return 'Этот идентификатор расширения не зарегистрирован в Local Connector.';
    default: return 'Действие не выполнено. Проверьте установку Connector и Xray.';
  }
}

export class Connection {
  readonly client: ConnectorClient;
  private selection: Selection | null = null;
  private subscription: SavedSubscription | null = null;
  private servers: SavedServer[] = [];
  private runtime: RuntimeRecord | null = null;
  private initialized: Promise<void> | null = null;
  private busy = false;
  private revision = 0;
  private proxyWork: Promise<void> = Promise.resolve();
  private view: ConnectionView = { phase: 'disconnected', connector: 'idle', server: null, servers: [], subscription: null, warning: null, pending: false, check: null, error: null, coreVersion: null };

  private readonly env: Environment;

  constructor(env: Environment) {
    this.env = env;
    this.client = new ConnectorClient({
      connect: () => env.connectNative(), extensionVersion: env.version,
      onState: (state) => {
        this.revision++;
        this.view.check = null;
        this.view.connector = state;
        if ((state === 'waiting' || state === 'incompatible' || state === 'stopped') && this.runtime?.active) {
          this.view.phase = 'blocked';
          this.view.error = 'Связь с Connector потеряна. Прокси не сброшен; отключитесь явно, чтобы восстановить прежние настройки.';
        }
        if (state === 'ready' && this.runtime?.active && !this.busy) void this.refresh();
      },
    });
  }

  private async initialize(): Promise<void> {
    this.selection = await this.env.load();
    this.subscription = this.selection?.server.source === 'subscription' ? await this.env.loadSubscription() : null;
    this.servers = this.subscription ? await this.env.loadServers(this.subscription.serverIds) : this.selection ? [this.selection.server] : [];
    if (this.subscription?.status.lastError) this.view.warning = subscriptionWarning(this.subscription.status.lastError.code, false);
    this.runtime = await this.env.readRuntime();
    this.revision++;
    if (this.runtime?.active) {
      this.view.phase = 'blocked';
      this.view.error = 'Сеанс Connector прерван. Прежний прокси сохранён; ожидается проверка Core.';
      if (typeof this.runtime.configHash === 'string' && configHash.test(this.runtime.configHash)) this.client.recordAppliedConfigHash(this.runtime.configHash);
      if (loopbackProxy(this.runtime.proxy)) {
        try { await this.env.setProxy(this.runtime.proxy!); }
        catch { this.view.error = 'Локальный прокси не подтверждён. Закройте вкладки и восстановите управление прокси.'; }
      }
    } else if (this.runtime) {
      this.view.phase = 'blocked';
      this.view.error = 'Отключение было прервано. Завершите отключение, чтобы восстановить прежний прокси.';
    } else if (!this.runtime) {
      const actual = await this.env.getProxy();
      if (actual.value.mode === 'fixed_servers' && actual.value.rules?.singleProxy?.host === '127.0.0.1' && actual.value.rules.singleProxy.scheme === 'socks5') {
        this.view.phase = 'blocked';
        this.view.error = 'Нет сохранённого доказательства сеанса и прежнего прокси. Прямое подключение не включено.';
      }
    }
    this.client.start();
  }

  private async ready(): Promise<void> {
    this.client.start();
    const deadline = Date.now() + 10000;
    while (this.client.state !== 'ready' && Date.now() < deadline) {
      if (this.client.state === 'incompatible') break;
      await new Promise((done) => setTimeout(done, 50));
    }
    if (this.client.state !== 'ready') throw { code: 'HandshakeFailed' };
  }

  async action(action: Action, payload?: unknown): Promise<ConnectionView> {
    this.initialized ??= this.initialize();
    try { await this.initialized; } catch (error) { this.view.error = failure(error); return this.snapshot(); }
    if (action === 'status') {
      if (!this.busy && this.client.state === 'ready') await this.refresh();
      if (this.client.state === 'waiting' && this.view.phase === 'disconnected') this.view.error = 'Local Connector недоступен. Установите Native Messaging host и зарегистрируйте идентификатор этого расширения.';
      return this.snapshot();
    }
    if (this.busy) return { ...this.snapshot(), error: 'Дождитесь завершения текущего действия.' };
    this.busy = true;
    this.view.error = null;
    this.view.check = null;
    try {
      switch (action) {
        case 'save': {
          if (this.view.phase !== 'disconnected') throw new Error('Сначала отключитесь от сервера.');
          const { selection, userId } = createSelection(payload);
          await this.ready();
          await this.client.request('profile.import', {
            profiles: [selection.profile], servers: [selection.server], mode: 'merge',
            credentials: [{ entity: 'server', entityId: selection.server.id, field: 'user-id', value: userId }],
          } as Json);
          await this.env.save(selection);
          this.selection = selection;
          this.subscription = null;
          this.servers = [selection.server];
          this.view.warning = null;
          break;
        }
        case 'subscribe': await this.subscribe(payload); break;
        case 'refresh':
          this.requireEditable();
          if (!this.subscription) throw new Error('Сначала добавьте подписку.');
          await this.refreshSubscription(this.subscription);
          break;
        case 'select': await this.select(payload); break;
        case 'connect': await this.connect(); break;
        case 'disconnect': await this.disconnect(); break;
        case 'test-server': await this.testServer(); break;
        case 'open-site':
          if (this.view.phase !== 'connected') throw new Error('Подключитесь перед открытием сайта.');
          await this.refresh();
          if (this.view.phase !== 'connected') throw new Error('Core больше не работает. Сайт не открыт.');
          await this.env.openSite(siteURL(payload));
          break;
      }
    } catch (error) {
      this.view.error = error instanceof Error && !('code' in error) ? error.message : failure(error);
      if (this.view.phase === 'connecting') this.view.phase = this.runtime?.active ? 'blocked' : 'disconnected';
      if (this.view.phase === 'disconnecting') this.view.phase = this.runtime?.active ? 'blocked' : 'disconnected';
    } finally { this.busy = false; }
    return this.snapshot();
  }

  private requireEditable(): void {
    if (this.view.phase !== 'disconnected') throw new Error('Сначала отключитесь от сервера.');
  }

  private async subscribe(payload: unknown): Promise<void> {
    this.requireEditable();
    const request = createSubscription(payload);
    try {
      await this.ready();
      await this.client.request('profile.import', {
        profiles: [], subscriptions: [request.source],
        credentials: [{ entity: 'subscription', entityId: request.source.id, field: 'url', value: request.url }],
      } as Json);
    } finally { request.url = ''; }
    await this.refreshSubscription(request.source);
  }

  private selectedServer(server: SavedServer): Selection {
    const id = this.selection?.profile.id ?? crypto.randomUUID();
    return {
      server,
      profile: {
        id, displayName: server.displayName, kind: 'connector', serverIds: [server.id], defaultServerId: server.id,
        selection: { mode: 'fixed' }, failover: { enabled: false }, failurePolicy: { coreDeath: { action: 'block' } },
      },
    };
  }

  private async select(payload: unknown): Promise<void> {
    this.requireEditable();
    const id = payload && typeof payload === 'object' && 'serverId' in payload ? payload.serverId : null;
    const server = this.servers.find((item) => item.id === id);
    if (!server || !this.subscription || server.subscriptionId !== this.subscription.id) throw new Error('Выберите сервер из доступного списка подписки.');
    if (this.selection?.server.id === server.id) return;
    const selection = this.selectedServer(server);
    if (this.subscription.format !== 'auto') await this.env.saveSubscription(this.subscription, this.servers, selection);
    this.selection = selection;
  }

  private async refreshSubscription(source: SavedSubscription): Promise<void> {
    const at = new Date().toISOString();
    let result: SubscriptionResult;
    try {
      await this.ready();
      result = await this.client.request('subscription.refresh', { sources: [source], force: true } as Json) as SubscriptionResult;
    } catch (error) {
      const code = error && typeof error === 'object' && 'code' in error ? String(error.code) : 'Internal';
      result = { refreshed: [], unchanged: [], failed: [{ subscriptionId: source.id, code, retryable: true, consecutiveFailures: source.status.consecutiveFailures + 1 }] };
    }
    const refreshed = result.refreshed.find((item) => item.subscriptionId === source.id);
    if (refreshed) {
      if (!['vless-base64', 'vless-uri-list', 'v2ray-json', 'clash-yaml'].includes(refreshed.format)) throw new Error('Connector не подтвердил формат подписки. Список не изменён.');
      const servers = this.subscriptionServers(refreshed.servers, source.id);
      const server = servers.find((item) => item.id === this.selection?.server.id) ?? servers[0];
      const selection = server ? this.selectedServer(server) : null;
      const updated: SavedSubscription = {
        ...source, format: refreshed.format, serverIds: servers.map((item) => item.id),
        status: { lastAttemptAt: at, lastSuccessAt: at, lastSha256: refreshed.sha256, serverCount: servers.length, consecutiveFailures: 0, lastError: null },
      };
      await this.env.saveSubscription(updated, servers, selection);
      this.subscription = updated;
      this.servers = servers;
      this.selection = selection;
      this.view.warning = refreshed.lossyFields?.length ? 'При импорте часть полей не перенесена. Проверьте параметры выбранного сервера перед подключением.' : null;
      if (!server) this.view.error = 'В подписке нет доступных TCP- или WebSocket-серверов.';
      return;
    }
    if (result.unchanged.includes(source.id)) {
      const updated = { ...source, status: { ...source.status, lastAttemptAt: at } };
      if (updated.format !== 'auto') await this.env.saveSubscription(updated, this.servers, this.selection);
      this.subscription = updated;
      return;
    }
    const failed = result.failed.find((item) => item.subscriptionId === source.id);
    if (failed) {
      // Never replace an existing subscription list or selection with partial data.
      const initialPartial = !source.serverIds.length && !!failed.partialServers?.length;
      const servers = initialPartial ? this.subscriptionServers(failed.partialServers!, source.id) : this.servers;
      const selection = initialPartial ? this.selectedServer(servers[0]!) : this.selection;
      const status = { ...source.status, lastAttemptAt: at, consecutiveFailures: failed.consecutiveFailures, lastError: { code: subscriptionStatusCode(failed.code), at } };
      delete status.nextAttemptAt;
      if (failed.nextAttemptAt) status.nextAttemptAt = failed.nextAttemptAt;
      const format = source.format === 'auto' && failed.format ? failed.format : source.format;
      if (format !== 'auto' && !['vless-base64', 'vless-uri-list', 'v2ray-json', 'clash-yaml'].includes(format)) throw new Error('Connector не подтвердил формат подписки. Список не изменён.');
      const updated: SavedSubscription = { ...source, format, status, serverIds: initialPartial ? servers.map((server) => server.id) : source.serverIds };
      this.view.warning = subscriptionWarning(failed.code, !!failed.partialServers?.length);
      if (failed.rejectedCount) this.view.warning += ` Неподдерживаемых записей: ${failed.rejectedCount}.`;
      if (!initialPartial && failed.partialServers?.length) this.view.warning += ' Прежний список и выбранный сервер сохранены без изменений.';
      if (updated.format === 'auto') {
        // A partial result may lack the optional format verdict. Never invent or persist it.
        this.view.warning += ' Формат ещё не подтверждён: новая подписка и её список временные до успешного обновления; после перезапуска добавьте ссылку заново.';
      } else await this.env.saveSubscription(updated, servers, selection);
      if (initialPartial || !this.subscription || this.subscription.id === source.id) {
        this.subscription = updated;
        this.servers = servers;
        this.selection = selection;
      }
      this.view.error = initialPartial ? null : subscriptionWarning(failed.code, false);
      return;
    }
    if (result.skipped?.includes(source.id)) throw new Error('Обновление подписки отложено Connector. Повторите позже.');
    throw new Error('Connector не вернул результат обновления подписки. Список не изменён.');
  }

  private subscriptionServers(servers: SavedServer[], subscriptionId: string): SavedServer[] {
    if (servers.length > 512 || new Set(servers.map((server) => server.id)).size !== servers.length) throw new Error('Connector вернул некорректный список серверов.');
    return servers.map((server) => {
      if (!uuid.test(server.id) || server.protocol !== 'vless' || !['tcp', 'ws'].includes(server.network) || server.source !== 'subscription' || server.subscriptionId !== subscriptionId || server.userIdRef !== `keystore://server/${server.id}/user-id`) throw new Error('Connector вернул неподдерживаемый сервер. Список не изменён.');
      if (server.network === 'ws' && (server.security === 'reality' || server.flow !== '' || !server.ws || 'headersRef' in server.ws || 'headersKeys' in server.ws)) throw new Error('Connector вернул неподдерживаемый сервер. Список не изменён.');
      // Whitelist public fields; do not retain any inline credential or raw feed field.
      const saved: SavedServer = {
        id: server.id, displayName: server.displayName, protocol: server.protocol, address: server.address, port: server.port,
        userIdRef: server.userIdRef, network: server.network, security: server.security, flow: server.flow,
        source: server.source, subscriptionId,
      };
      if (server.network === 'ws') saved.ws = { ...(server.ws!.path !== undefined ? { path: server.ws!.path } : {}), ...(server.ws!.host !== undefined ? { host: server.ws!.host } : {}) };
      if (server.tls) saved.tls = { serverName: server.tls.serverName, ...(server.tls.alpn ? { alpn: [...server.tls.alpn] } : {}), ...(server.tls.fingerprint ? { fingerprint: server.tls.fingerprint } : {}), ...(server.tls.insecure === false ? { insecure: false } : {}) };
      if (server.reality) saved.reality = { serverName: server.reality.serverName, publicKey: server.reality.publicKey, ...(server.reality.shortId !== undefined ? { shortId: server.reality.shortId } : {}), ...(server.reality.fingerprint ? { fingerprint: server.reality.fingerprint } : {}), ...(server.reality.spiderX !== undefined ? { spiderX: server.reality.spiderX } : {}) };
      return saved;
    });
  }

  private async connect(): Promise<void> {
    if (!this.selection) throw new Error('Сначала добавьте сервер.');
    if (this.view.phase !== 'disconnected') throw new Error('Сначала отключите предыдущий сеанс.');
    this.revision++;
    this.view.phase = 'connecting';
    const previous = await this.env.getProxy();
    if (!['controllable_by_this_extension', 'controlled_by_this_extension'].includes(previous.levelOfControl)) throw new Error('Прокси управляется политикой браузера или другим расширением. Подключение не начато.');
    await this.ready();
    const { profile, server } = this.selection;
    const revision = this.revision;
    const current = () => this.revision === revision && this.client.state === 'ready';
    const result = await this.client.request('config.apply', {
      profileIds: [profile.id], intent: { profiles: [profile], servers: [server], settings, failurePolicy },
    } as Json) as { applied?: boolean; configHash?: string; inboundPorts?: { listen: string; port: number; tag: string }[] };
    try {
      const inbound = Array.isArray(result.inboundPorts) ? result.inboundPorts.find((item) => item?.tag === `in-srv-${server.id.replaceAll('-', '').slice(0, 8)}`) : undefined;
      if (result.applied !== true || typeof result.configHash !== 'string' || !configHash.test(result.configHash) || !inbound || inbound.listen !== '127.0.0.1' || !Number.isInteger(inbound.port) || inbound.port < 1 || inbound.port > 65535) throw new Error('Connector не сообщил безопасный входящий порт и хеш конфига. Прокси браузера не изменён.');
      if (!current()) throw new Error('Связь с Connector изменилась. Подключение не подтверждено.');
      const state = await this.client.request('core.status', {});
      const proxy: ProxyValue = { mode: 'fixed_servers', rules: { singleProxy: { scheme: 'socks5', host: '127.0.0.1', port: inbound.port }, bypassList: ['<-loopback>'] } };
      const record: RuntimeRecord = { previous: previous.value, proxy, active: true, configHash: result.configHash, profileId: profile.id };
      const error = runtimeProof(state, record, this.selection);
      if (error) throw new Error(error);
      if (!current()) throw new Error('Связь с Connector изменилась. Подключение не подтверждено.');
      await this.env.writeRuntime(record);
      this.runtime = record;
      this.view.phase = 'blocked';
      this.client.recordAppliedConfigHash(record.configHash);
      if (!current()) throw new Error('Связь с Connector потеряна. Прокси не сброшен.');
      await this.proxyOperation(() => this.env.setProxy(proxy));
      if (!current()) throw new Error('Связь с Connector потеряна. Прокси не сброшен.');
      this.view.phase = 'connected';
      await this.refresh();
    } catch (error) {
      if (result.applied === true && !this.runtime?.active) {
        if (current() && typeof result.configHash === 'string' && configHash.test(result.configHash)) {
          try { await this.client.request('core.stop', { graceful: true, reason: 'connect-failed', expectedConfigHash: result.configHash }); }
          catch { this.view.phase = 'blocked'; }
        } else this.view.phase = 'blocked';
      }
      throw error;
    }
  }

  private async testServer(): Promise<void> {
    if (this.view.phase !== 'connected') throw new Error('Подключитесь перед проверкой сервера.');
    await this.refresh();
    if (this.view.phase !== 'connected' || !this.selection || !this.runtime?.active) throw new Error('Сеанс Core не подтверждён. Проверка не начата.');
    const selection = this.selection;
    const record = this.runtime;
    const revision = this.revision;
    const layers = ['dns', 'tcp', 'core-outbound', 'proxied-http'] as const;
    const result = await this.client.request('server.test', { serverId: selection.server.id, layers: [...layers], repeat: 1 }) as { serverId?: unknown; layers?: unknown } | null;
    if (this.revision !== revision || this.view.phase !== 'connected' || this.runtime !== record || this.client.state !== 'ready') throw new Error('Сеанс изменился. Результат проверки отброшен.');
    if (!result || result.serverId !== selection.server.id || !Array.isArray(result.layers) || result.layers.length !== layers.length) throw new Error('Connector вернул некорректный результат проверки.');
    const measures = result.layers.map((value, index) => {
      const row = value as Record<string, unknown> | null;
      if (!row || row.layer !== layers[index] || !['pass', 'fail', 'skipped', 'timeout'].includes(row.state as string) ||
        (row.rttMs !== undefined && row.rttMs !== null && (typeof row.rttMs !== 'number' || !Number.isFinite(row.rttMs) || row.rttMs < 0))) throw new Error('Connector вернул некорректный результат проверки.');
      return { layer: layers[index]!, state: row.state as ServerCheck['layers'][number]['state'], rttMs: typeof row.rttMs === 'number' ? row.rttMs : null };
    });
    const egressIp = typeof (result as Record<string, unknown>).egressIp === 'string' ? (result as Record<string, unknown>).egressIp as string : null;
    const matches = typeof (result as Record<string, unknown>).matchesExpectedEgress === 'boolean' ? (result as Record<string, unknown>).matchesExpectedEgress as boolean : null;
    await this.refresh();
    if (this.revision !== revision + 1 || this.view.phase !== 'connected' || this.runtime !== record || this.selection !== selection || this.client.state !== 'ready') throw new Error('Сеанс изменился. Результат проверки отброшен.');
    this.view.check = { serverId: selection.server.id, layers: measures, egressIp, matchesExpectedEgress: matches };
  }

  private async disconnect(): Promise<void> {
    this.revision++;
    if (!this.runtime || typeof this.runtime.configHash !== 'string' || !configHash.test(this.runtime.configHash)) throw new Error('Нет подтверждённой записи сеанса. Прокси не сброшен; восстановите настройки вручную после остановки Core.');
    this.view.phase = 'disconnecting';
    await this.ready();
    await this.client.request('core.stop', { graceful: true, reason: 'user-disconnect', expectedConfigHash: this.runtime.configHash });
    if (this.runtime) {
      await this.env.writeRuntime({ ...this.runtime, active: false });
      this.runtime.active = false;
      try { await this.proxyOperation(() => this.env.restoreProxy(this.runtime!.previous)); }
      catch (error) {
        this.runtime.active = true;
        try {
          if (loopbackProxy(this.runtime.proxy)) await this.proxyOperation(() => this.env.setProxy(this.runtime!.proxy!));
        } finally { await this.env.writeRuntime(this.runtime); }
        throw error;
      }
    }
    await this.env.writeRuntime(null);
    this.runtime = null;
    this.client.recordAppliedConfigHash(null);
    this.view.phase = 'disconnected';
  }

  // Serialize writes so a late recovery/onChange write cannot overwrite an explicit restoration.
  private proxyOperation(operation: () => Promise<void>): Promise<void> {
    const work = this.proxyWork.then(operation);
    this.proxyWork = work.catch(() => {});
    return work;
  }

  private async refresh(): Promise<void> {
    const record = this.runtime;
    const selection = this.selection;
    const revision = ++this.revision;
    const current = () => this.revision === revision && this.runtime === record && this.selection === selection && this.client.state === 'ready' && this.view.phase !== 'disconnecting';
    try {
      const status = await this.client.request('core.status', {});
      if (!current()) return;
      this.view.coreVersion = status && typeof status === 'object' && 'coreVersion' in status && typeof status.coreVersion === 'string' ? status.coreVersion : null;
      if (!record?.active) return;
      const error = runtimeProof(status, record, selection);
      if (error) {
        this.view.phase = 'blocked';
        this.view.error = error;
        this.view.check = null;
        return;
      }
      await this.proxyOperation(async () => {
        if (!current() || !record.active) return;
        if (this.view.phase === 'blocked') await this.env.setProxy(record.proxy!);
        else {
          const actual = await this.env.getProxy();
          if (!current()) return;
          if (actual.levelOfControl !== 'controlled_by_this_extension' || !loopbackProxy(actual.value) || actual.value.rules!.singleProxy!.port !== record.proxy!.rules!.singleProxy!.port) throw new Error('proxy readback failed');
        }
        if (!current() || !record.active) return;
        this.view.phase = 'connected';
        this.view.error = null;
      });
    } catch {
      if (!current() || !record?.active) return;
      this.view.phase = 'blocked';
      this.view.error = 'Состояние Core или управление локальным прокси не подтверждено. Закройте вкладки; прокси не сброшен.';
      this.view.check = null;
    }
  }

  async preserveProxy(actual: ProxyValue, levelOfControl?: string): Promise<void> {
    try { await this.initialized; } catch { return; }
    const record = this.runtime;
    if (!record?.active || !loopbackProxy(record.proxy) || this.view.phase === 'disconnecting') return;
    if (loopbackProxy(actual) && actual.rules!.singleProxy!.port === record.proxy!.rules!.singleProxy!.port && (levelOfControl === undefined || levelOfControl === 'controlled_by_this_extension')) return;
    const revision = ++this.revision;
    const current = () => this.revision === revision && this.runtime === record && record.active && this.view.phase !== 'disconnecting';
    try {
      await this.proxyOperation(async () => {
        if (current()) await this.env.setProxy(record.proxy!);
      });
    } catch {
      if (!current()) return;
      this.view.phase = 'blocked';
      this.view.error = 'Настройки прокси изменены извне. Защита не подтверждена: закройте вкладки и восстановите управление прокси.';
      this.view.check = null;
    }
  }

  snapshot(): ConnectionView {
    const server = this.selection?.server;
    const source = this.subscription;
    return {
      ...this.view, pending: this.busy, check: structuredClone(this.view.check), servers: structuredClone(this.servers),
      subscription: source ? {
        id: source.id, displayName: source.displayName, serverCount: this.servers.filter((item) => item.subscriptionId === source.id).length,
        consecutiveFailures: source.status.consecutiveFailures, lastAttemptAt: source.status.lastAttemptAt,
        lastSuccessAt: source.status.lastSuccessAt, temporary: source.format === 'auto',
      } : null,
      server: server ? { id: server.id, displayName: server.displayName, address: server.address, port: server.port } : null,
    };
  }
}
