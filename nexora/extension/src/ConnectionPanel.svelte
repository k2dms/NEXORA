<svelte:options customElement="nexora-connection-panel" />

<script lang="ts">
  import { onMount } from 'svelte';
  import type { ConnectionView, ServerForm } from './model';
  import { request } from './rpc';

  type Action = 'save' | 'connect' | 'disconnect' | 'open-site';
  type FieldErrors = Record<string, string | undefined>;

  const phaseLabels: Record<ConnectionView['phase'], string> = {
    disconnected: 'Отключено',
    connecting: 'Подключение…',
    connected: 'Подключено',
    blocked: 'Соединение прервано',
    disconnecting: 'Отключение…',
  };
  const connectorLabels: Record<ConnectionView['connector'], string> = {
    idle: 'Не запущен',
    connecting: 'Запускается',
    ready: 'Готов',
    waiting: 'Ожидает Connector',
    incompatible: 'Несовместимая версия',
    stopped: 'Остановлен',
  };
  const uuidPattern = '[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}';
  const publicKeyPattern = '[A-Za-z0-9_\\-]{43}';
  const shortIdPattern = '([0-9a-f]{2}){0,4}';
  const addressPattern = '[^\\s\\x2f?#@]+';
  const sniPattern = '[^\\s:\\x2f?#@]+';
  const validationMessages: Record<string, string> = {
    displayName: 'Укажите название сервера.',
    address: 'Укажите адрес без пробелов, схемы https:// и пути.',
    port: 'Порт — целое число от 1 до 65535.',
    userId: 'Введите UUID: 32 шестнадцатеричных символа в формате 8-4-4-4-12.',
    serverName: 'Укажите SNI — имя хоста без схемы, порта и пути.',
    publicKey: 'Публичный ключ REALITY: ровно 43 символа A–Z, a–z, 0–9, _ или -.',
    shortId: 'Short ID: 0, 2, 4, 6 или 8 символов (0–9, a–f).',
    url: 'Введите полный адрес сайта, начинающийся с https://.',
  };

  let view = $state<ConnectionView | null>(null);
  let pending = $state<Action | null>(null);
  let replacing = $state(false);
  let fieldErrors = $state<FieldErrors>({});
  let actionError = $state<string | null>(null);
  let statusError = $state<string | null>(null);
  let notice = $state('');
  let uuidInput = $state<HTMLInputElement>();

  // A status response issued before an action must never replace its result.
  let revision = 0;
  let active = false;
  let polling = false;

  const transitioning = $derived(view?.phase === 'connecting' || view?.phase === 'disconnecting');
  const editable = $derived(view?.phase === 'disconnected' && !pending && !statusError);
  const showForm = $derived(view?.phase === 'disconnected' && (!view.server || replacing));
  const canConnect = $derived(editable && !!view?.server && !replacing);
  const canDisconnect = $derived(!pending && (view?.phase === 'connected' || view?.phase === 'blocked'));
  const canOpen = $derived(view?.phase === 'connected' && !pending && !statusError);
  const phaseLabel = $derived(view ? phaseLabels[view.phase] : 'Состояние неизвестно');

  function acceptView(next: ConnectionView): void {
    view = next;
    statusError = null;
    if (next.phase !== 'disconnected') replacing = false;
  }

  function errorText(error: unknown): string {
    return error instanceof Error && error.message
      ? error.message
      : 'Запрос не выполнен. Проверьте расширение и Connector.';
  }

  async function pollStatus(): Promise<void> {
    if (!active || pending || polling) return;
    polling = true;
    const ticket = revision;
    try {
      const next = await request('status');
      if (active && !pending && ticket === revision) acceptView(next);
    } catch (error: unknown) {
      if (active && !pending && ticket === revision) statusError = errorText(error);
    } finally {
      polling = false;
    }
  }

  onMount(() => {
    active = true;
    void pollStatus();
    const interval = window.setInterval(() => void pollStatus(), 2000);
    return () => {
      active = false;
      revision += 1;
      window.clearInterval(interval);
      if (uuidInput) uuidInput.value = '';
    };
  });

  async function perform(action: Action, payload?: unknown): Promise<void> {
    if (!active || pending || transitioning) return;
    if (action === 'save' && !editable) return;
    if (action === 'connect' && !canConnect) return;
    if (action === 'disconnect' && !canDisconnect) return;
    if (action === 'open-site' && !canOpen) return;

    const ticket = ++revision;
    const secretInput = uuidInput;
    pending = action;
    actionError = null;
    notice = '';
    try {
      const next = await request(action, payload);
      if (!active || ticket !== revision) return;
      acceptView(next);
      if (action === 'save' && next.server && !next.error) {
        replacing = false;
        notice = 'Сервер сохранён.';
      } else if (action === 'open-site' && !next.error) {
        notice = 'Запрос на открытие сайта отправлен. Доступность сайта не проверялась.';
      }
    } catch (error: unknown) {
      if (active && ticket === revision) actionError = errorText(error);
    } finally {
      // UUID lives only in the input and this transient request, never in UI state/storage.
      if (secretInput) secretInput.value = '';
      if (active && ticket === revision) pending = null;
    }
  }

  function clearFieldError(event: Event): void {
    const input = event.currentTarget as HTMLInputElement;
    input.setCustomValidity('');
    if (fieldErrors[input.name]) {
      const next = { ...fieldErrors };
      delete next[input.name];
      fieldErrors = next;
    }
  }

  function validate(form: HTMLFormElement): boolean {
    const next = { ...fieldErrors };
    let firstInvalid: HTMLInputElement | undefined;
    for (const input of Array.from(form.querySelectorAll<HTMLInputElement>('input'))) {
      input.setCustomValidity('');
      delete next[input.name];
      if (!input.willValidate) continue;
      let message = '';
      if (!input.validity.valid || (input.required && !input.value.trim())) {
        message = validationMessages[input.name] ?? 'Проверьте значение поля.';
      } else if (input.name === 'url') {
        try {
          const url = new URL(input.value.trim());
          if (url.protocol !== 'https:' || !url.hostname) message = validationMessages.url;
          else if (url.username || url.password) message = 'Адрес сайта не должен содержать логин или пароль.';
        } catch {
          message = validationMessages.url;
        }
      }
      if (message) {
        input.setCustomValidity(message);
        next[input.name] = message;
        firstInvalid ??= input;
      }
    }
    fieldErrors = next;
    if (!form.checkValidity()) {
      firstInvalid?.focus();
      firstInvalid?.reportValidity();
      return false;
    }
    return true;
  }

  function saveServer(event: SubmitEvent): void {
    event.preventDefault();
    if (!editable) return;
    const form = event.currentTarget as HTMLFormElement;
    if (!validate(form)) return;
    const data = new FormData(form);
    const text = (name: string): string => String(data.get(name) ?? '').trim();
    const payload: ServerForm = {
      displayName: text('displayName'),
      address: text('address'),
      port: Number(text('port')),
      userId: text('userId'),
      serverName: text('serverName'),
      publicKey: text('publicKey'),
      shortId: text('shortId'),
      flow: data.get('flow') === 'on',
    };
    void perform('save', payload);
  }

  function openSite(event: SubmitEvent): void {
    event.preventDefault();
    if (!canOpen) return;
    const form = event.currentTarget as HTMLFormElement;
    if (!validate(form)) return;
    const url = String(new FormData(form).get('url') ?? '').trim();
    void perform('open-site', { url });
  }

  function toggleReplacement(): void {
    if (!editable) return;
    if (uuidInput) uuidInput.value = '';
    replacing = !replacing;
    fieldErrors = {};
    actionError = null;
    notice = '';
  }
</script>

<main class="panel" lang="ru" aria-labelledby="panel-title">
  <header class="header">
    <div>
      <p class="wordmark">NEXORA</p>
      <h1 id="panel-title">Соединение</h1>
    </div>
    <span class="platform">Windows<br />Chromium</span>
  </header>

  <section class="connection" aria-label="Состояние соединения" data-phase={view?.phase ?? 'unknown'}>
    <div class="status-heading" role="status" aria-live="polite" aria-atomic="true">
      <span class="status-dot" aria-hidden="true"></span>
      <h2>{phaseLabel}</h2>
      {#if statusError}<span class="stale">Последнее известное</span>{/if}
    </div>
    <div class="route" aria-hidden="true">
      <svg viewBox="0 0 320 30" fill="none" preserveAspectRatio="none">
        <path class="route-line" d="M8 9H128L144 21H312" />
        <circle cx="8" cy="9" r="3" />
        <circle cx="160" cy="21" r="3" />
        <circle cx="312" cy="21" r="3" />
      </svg>
      <div class="route-labels"><span>Браузер</span><span>Connector</span><span>Сервер</span></div>
    </div>
    <dl class="diagnostics">
      <div><dt>Connector</dt><dd>{view ? connectorLabels[view.connector] : 'Нет данных'}</dd></div>
      <div><dt>Ядро</dt><dd class="technical">{view?.coreVersion ?? 'Нет данных'}</dd></div>
    </dl>
    {#if view?.phase === 'blocked'}
      <p class="blocked-note">Прокси не сбрасывается автоматически. Проверьте сообщение об ошибке; «Отключить» восстанавливает предыдущие настройки после остановки Core.</p>
    {:else if view?.phase === 'connecting'}
      <p class="phase-note">Ожидаем результат Connector. Состояние обновляется автоматически.</p>
    {:else if view?.phase === 'disconnecting'}
      <p class="phase-note">Восстанавливаем предыдущие настройки прокси.</p>
    {/if}
  </section>

  {#if statusError || actionError || view?.error}
    <div class="request-errors" role="alert" aria-atomic="true">
      {#if statusError}<p><strong>Не удалось обновить состояние.</strong> {statusError}</p>{/if}
      {#if actionError}<p>{actionError}</p>{/if}
      {#if view?.error}<p>{view.error}</p>{/if}
    </div>
  {/if}
  <p class="notice" role="status" aria-live="polite" hidden={!notice}>{notice}</p>

  {#if view?.server}
    <section class="server-card" aria-labelledby="server-title">
      <div class="section-heading">
        <h2 id="server-title">Сервер</h2>
        <span class="protocol">VLESS / REALITY · TCP</span>
      </div>
      <p class="server-name">{view.server.displayName}</p>
      <p class="server-address technical">{view.server.address}<span class="port"> · {view.server.port}</span></p>
      <div class="actions">
        <button class="primary" type="button" disabled={!canConnect} onclick={() => void perform('connect')}>
          {pending === 'connect' || view.phase === 'connecting' ? 'Подключение…' : 'Подключить'}
        </button>
        <button class:recovery={view.phase === 'blocked'} class="secondary" type="button" disabled={!canDisconnect} onclick={() => void perform('disconnect')}>
          {pending === 'disconnect' || view.phase === 'disconnecting' ? 'Отключение…' : 'Отключить'}
        </button>
      </div>
      <button class="text-button" type="button" disabled={!editable} aria-expanded={replacing} aria-controls="server-form" onclick={toggleReplacement}>
        {replacing ? 'Отменить замену' : 'Заменить сервер'}
      </button>
    </section>
  {:else if view?.phase === 'blocked' || view?.phase === 'connected'}
    <section class="recovery-card" aria-label="Восстановление прокси">
      <p>Данные сервера недоступны. Можно восстановить предыдущий прокси.</p>
      <button class="secondary recovery" type="button" disabled={!canDisconnect} onclick={() => void perform('disconnect')}>
        {pending === 'disconnect' ? 'Отключение…' : 'Отключить'}
      </button>
    </section>
  {/if}

  {#if showForm}
    <section class="form-section" aria-labelledby="form-title">
      <div class="section-heading">
        <h2 id="form-title">{replacing ? 'Заменить сервер' : 'Добавить сервер'}</h2>
        <span class="protocol">Вручную</span>
      </div>
      <p class="section-note">VLESS / REALITY по TCP. Введите параметры, выданные владельцем сервера.</p>
      <form id="server-form" novalidate autocomplete="off" onsubmit={saveServer}>
        <fieldset disabled={!editable}>
          <legend class="visually-hidden">Параметры сервера VLESS / REALITY</legend>
          <div class="field">
            <label for="display-name">Название</label>
            <input id="display-name" name="displayName" required value={replacing ? view?.server?.displayName ?? '' : ''} placeholder="Название для списка" aria-invalid={!!fieldErrors.displayName} aria-describedby="name-error" oninput={clearFieldError} />
            <p class="field-error" id="name-error" hidden={!fieldErrors.displayName}>{fieldErrors.displayName ?? ''}</p>
          </div>
          <div class="address-grid">
            <div class="field">
              <label for="server-address">Адрес сервера</label>
              <input class="technical" id="server-address" name="address" required pattern={addressPattern} value={replacing ? view?.server?.address ?? '' : ''} placeholder="Домен или IP" spellcheck="false" autocapitalize="none" aria-invalid={!!fieldErrors.address} aria-describedby="address-error" oninput={clearFieldError} />
              <p class="field-error" id="address-error" hidden={!fieldErrors.address}>{fieldErrors.address ?? ''}</p>
            </div>
            <div class="field">
              <label for="server-port">Порт</label>
              <input class="technical" id="server-port" name="port" type="number" min="1" max="65535" step="1" required value={replacing ? view?.server?.port ?? 443 : 443} inputmode="numeric" aria-invalid={!!fieldErrors.port} aria-describedby="port-error" oninput={clearFieldError} />
              <p class="field-error" id="port-error" hidden={!fieldErrors.port}>{fieldErrors.port ?? ''}</p>
            </div>
          </div>
          <div class="field">
            <label for="user-id">UUID пользователя</label>
            <input class="technical" bind:this={uuidInput} id="user-id" name="userId" type="password" required pattern={uuidPattern} autocomplete="off" spellcheck="false" autocapitalize="none" aria-invalid={!!fieldErrors.userId} aria-describedby="uuid-hint uuid-error" oninput={clearFieldError} />
            <p class="field-hint" id="uuid-hint">Очищается после каждой попытки сохранения. Для замены введите заново.</p>
            <p class="field-error" id="uuid-error" hidden={!fieldErrors.userId}>{fieldErrors.userId ?? ''}</p>
          </div>
          <div class="field">
            <label for="server-name">Имя сервера (SNI)</label>
            <input class="technical" id="server-name" name="serverName" required pattern={sniPattern} spellcheck="false" autocapitalize="none" aria-invalid={!!fieldErrors.serverName} aria-describedby="sni-error" oninput={clearFieldError} />
            <p class="field-error" id="sni-error" hidden={!fieldErrors.serverName}>{fieldErrors.serverName ?? ''}</p>
          </div>
          <div class="field">
            <label for="public-key">Публичный ключ REALITY</label>
            <input class="technical" id="public-key" name="publicKey" required pattern={publicKeyPattern} minlength="43" maxlength="43" spellcheck="false" autocapitalize="none" aria-invalid={!!fieldErrors.publicKey} aria-describedby="key-hint key-error" oninput={clearFieldError} />
            <p class="field-hint" id="key-hint">43 символа, формат base64url.</p>
            <p class="field-error" id="key-error" hidden={!fieldErrors.publicKey}>{fieldErrors.publicKey ?? ''}</p>
          </div>
          <div class="field">
            <label for="short-id">Short ID <span class="optional">необязательно</span></label>
            <input class="technical" id="short-id" name="shortId" pattern={shortIdPattern} maxlength="8" spellcheck="false" autocapitalize="none" aria-invalid={!!fieldErrors.shortId} aria-describedby="short-hint short-error" oninput={clearFieldError} />
            <p class="field-hint" id="short-hint">До 8 hex-символов. Оставьте пустым, если ID не выдан.</p>
            <p class="field-error" id="short-error" hidden={!fieldErrors.shortId}>{fieldErrors.shortId ?? ''}</p>
          </div>
          <label class="checkbox" for="vision"><input id="vision" name="flow" type="checkbox" checked /><span>Vision <span class="technical">xtls-rprx-vision</span></span></label>
          <button class="primary save-button" type="submit">{pending === 'save' ? 'Сохранение…' : replacing ? 'Сохранить замену' : 'Сохранить сервер'}</button>
        </fieldset>
      </form>
    </section>
  {:else if !view}
    <p class="empty-note">{statusError ? 'Параметры недоступны до получения состояния.' : 'Получаем состояние расширения…'}</p>
  {/if}

  <section class="site-section" aria-labelledby="site-title">
    <h2 id="site-title">Открыть сайт</h2>
    <form novalidate onsubmit={openSite}>
      <label for="site-url">HTTPS-адрес</label>
      <div class="site-row">
        <input class="technical" id="site-url" name="url" type="url" required placeholder="Вставьте HTTPS-ссылку" autocomplete="off" spellcheck="false" autocapitalize="none" disabled={!canOpen} aria-invalid={!!fieldErrors.url} aria-describedby="site-hint url-error" oninput={clearFieldError} />
        <button class="secondary" type="submit" disabled={!canOpen}>{pending === 'open-site' ? 'Открытие…' : 'Открыть'}</button>
      </div>
      <p class="field-error" id="url-error" hidden={!fieldErrors.url}>{fieldErrors.url ?? ''}</p>
      <p class="field-hint" id="site-hint">Доступно только при подключении. Расширение меняет прокси браузера целиком, не отдельной вкладки.</p>
    </form>
  </section>

  <footer>
    <p>Состояние сообщает Connector. Доступность сайтов, внешний IP и задержка здесь не проверяются. Это не системный VPN.</p>
    <p class="legacy-note">Прежние настройки сохранены. <a href="options.html" target="_blank" rel="noopener">Открыть настройки</a></p>
  </footer>
</main>

<style>
  :host {
    display: block;
    color-scheme: dark;
    color: #e5edf5;
    background: #142235;
    font: 14px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif;
    --navy: #142235;
    --slate: #1c2e43;
    --steel: #829bb1;
    --line: #3b536c;
    --cyan: #79d2de;
    --paper: #e5edf5;
  }
  *, *::before, *::after { box-sizing: border-box; }
  [hidden] { display: none !important; }
  .panel { width: 100%; max-width: 540px; min-width: 0; margin: 0 auto; padding: 26px 24px 20px; }
  h1, h2, p, dl, dd { margin: 0; }
  h1 { font-size: 24px; font-weight: 600; line-height: 1.25; letter-spacing: -0.6px; }
  h2 { font-size: 15px; font-weight: 600; line-height: 1.4; }
  .header { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 24px; }
  .wordmark { color: var(--cyan); font-size: 11px; font-weight: 650; letter-spacing: 3px; margin-bottom: 6px; }
  .platform { color: #a9bacb; font-size: 11px; text-align: right; line-height: 1.5; }
  .connection { background: var(--slate); border: 1px solid var(--line); border-radius: 10px; padding: 17px 18px; }
  .status-heading { display: flex; align-items: center; gap: 9px; flex-wrap: wrap; }
  .status-dot { width: 7px; height: 7px; background: var(--steel); border-radius: 50%; flex: 0 0 auto; }
  [data-phase="connected"] .status-dot { background: var(--cyan); }
  [data-phase="blocked"] .status-dot { background: #e8bc82; }
  .stale { color: #e8bc82; font-size: 11px; width: 100%; padding-left: 16px; }
  .route { margin: 19px 0 18px; }
  .route svg { display: block; width: 100%; height: 30px; overflow: visible; }
  .route-line { stroke: var(--steel); stroke-width: 1.5; vector-effect: non-scaling-stroke; }
  .route circle { fill: var(--slate); stroke: var(--steel); stroke-width: 1.5; vector-effect: non-scaling-stroke; }
  [data-phase="connected"] .route-line { stroke: var(--cyan); }
  [data-phase="blocked"] .route-line { stroke: #e8bc82; stroke-dasharray: 4 5; }
  .route-labels { display: grid; grid-template-columns: 1fr 1fr 1fr; color: #a9bacb; font-size: 10px; letter-spacing: 0.2px; }
  .route-labels span:nth-child(2) { text-align: center; }
  .route-labels span:last-child { text-align: right; }
  .diagnostics { display: grid; gap: 5px; font-size: 12px; }
  .diagnostics div { display: flex; justify-content: space-between; gap: 16px; }
  dt { color: #a9bacb; }
  dd { text-align: right; overflow-wrap: anywhere; min-width: 0; }
  .technical, .protocol { font-family: ui-monospace, "Cascadia Mono", Consolas, monospace; }
  .phase-note, .blocked-note { margin-top: 14px; padding-top: 12px; border-top: 1px solid var(--line); font-size: 12px; color: #c0cfdd; }
  .blocked-note { color: #f1cfa4; }
  .request-errors { margin-top: 14px; padding: 12px 14px; border: 1px solid #986c68; border-radius: 6px; background: #352b35; color: #f4c1bb; font-size: 12px; overflow-wrap: anywhere; white-space: pre-wrap; }
  .request-errors p + p { margin-top: 8px; }
  .notice { margin-top: 12px; color: var(--cyan); font-size: 12px; }
  .server-card, .form-section, .recovery-card { margin-top: 24px; }
  .section-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; flex-wrap: wrap; margin-bottom: 10px; }
  .protocol { color: #a9bacb; font-size: 10px; }
  .server-name { font-size: 18px; font-weight: 550; overflow-wrap: anywhere; }
  .server-address { margin-top: 4px; color: #b6c8d8; font-size: 12px; overflow-wrap: anywhere; }
  .port { color: #a9bacb; }
  .actions { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin-top: 17px; }
  button, input { font: inherit; }
  button { min-height: 40px; border: 1px solid transparent; border-radius: 6px; padding: 9px 14px; cursor: pointer; font-size: 13px; font-weight: 600; line-height: 1.4; transition: background-color 120ms ease, border-color 120ms ease; }
  button.primary { background: var(--cyan); color: var(--navy); }
  button.primary:hover:not(:disabled) { background: #a0e0e8; }
  button.secondary { background: var(--slate); color: var(--paper); border-color: #587089; }
  button.secondary:hover:not(:disabled) { background: #29425b; border-color: #a9bacb; }
  button.recovery { border-color: #c9a06d; color: #f1cfa4; }
  button:disabled { cursor: not-allowed; opacity: 0.48; }
  button.text-button { background: transparent; color: #c0cfdd; min-height: 36px; padding: 8px 0; margin-top: 5px; font-size: 12px; font-weight: 400; text-decoration: underline; text-underline-offset: 4px; }
  button.text-button:hover:not(:disabled) { color: var(--cyan); }
  .section-note, .empty-note, .recovery-card p { color: #b6c8d8; font-size: 12px; margin-bottom: 16px; }
  .empty-note { margin-top: 20px; }
  fieldset { margin: 0; padding: 0; border: 0; min-width: 0; }
  .field { min-width: 0; margin-bottom: 14px; }
  label { display: block; font-size: 12px; color: #d6e1ec; margin-bottom: 6px; }
  input:not([type="checkbox"]) { display: block; width: 100%; min-width: 0; min-height: 40px; padding: 9px 10px; background: #18293d; border: 1px solid #587089; border-radius: 5px; color: var(--paper); font-size: 13px; line-height: 1.5; }
  input::placeholder { color: #97aabe; opacity: 1; }
  input:not([type="checkbox"]):disabled { opacity: 0.55; cursor: not-allowed; }
  input[aria-invalid="true"] { border-color: #eba9a2; }
  input.technical { font-size: 12px; }
  .address-grid { display: grid; grid-template-columns: minmax(0, 1fr) 90px; gap: 10px; }
  .field-hint { font-size: 11px; color: #a9bacb; margin-top: 6px; line-height: 1.5; }
  .field-error { font-size: 12px; color: #f4c1bb; margin-top: 6px; }
  .optional { color: #a9bacb; font-size: 11px; margin-left: 6px; font-weight: 400; }
  .checkbox { display: flex; align-items: center; gap: 9px; margin: 4px 0 18px; min-height: 30px; cursor: pointer; }
  .checkbox input { width: 16px; height: 16px; flex: 0 0 auto; margin: 0; accent-color: var(--cyan); }
  .checkbox .technical { color: #a9bacb; font-size: 10px; margin-left: 5px; }
  .save-button { width: 100%; }
  .site-section { margin-top: 24px; padding-top: 20px; border-top: 1px solid var(--line); }
  .site-section h2 { margin-bottom: 12px; }
  .site-row { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px; }
  footer { margin-top: 20px; color: #a9bacb; font-size: 11px; line-height: 1.6; }
  .legacy-note { margin-top: 10px; }
  a { color: #c0e6ed; text-decoration: underline; text-underline-offset: 3px; }
  :where(button, input, a):focus-visible { outline: 2px solid var(--cyan); outline-offset: 3px; }
  .visually-hidden { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; border: 0; }
  @media (max-width: 360px) {
    .panel { padding: 20px 16px 18px; }
    .connection { padding: 15px 13px; }
    .header { margin-bottom: 20px; }
    .address-grid { grid-template-columns: minmax(0, 1fr) 78px; gap: 8px; }
    .site-row { grid-template-columns: minmax(0, 1fr); }
    .site-row button { width: 100%; }
    .actions { gap: 8px; }
    button { padding-left: 10px; padding-right: 10px; }
  }
  @media (prefers-reduced-motion: reduce) {
    button { transition: none; }
  }
</style>
