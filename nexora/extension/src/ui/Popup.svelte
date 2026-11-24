<svelte:options customElement="nexora-popup" />

<!-- The PopupShell island (doc 22 §13): current profile, quick switch, health dot, one tap to options.
     Non-goals are the contract's own: no editing, no diagnostics, no subscription management, no secret
     entry. Invariant 2: opening performs no side-effecting command on mount — the snapshot arrives as a
     property, and nothing here starts a probe or a refresh. -->

<script lang="ts">
  import { handleKey, tabbables, type KeyboardHandlers, type KeyboardState } from '../popup/keyboard';
  import { autoResize } from '../popup/resize';
  import { documentStateCopy, type DocumentState } from '../popup/documentStates';
  import { toggleSiteRule } from '../popup/bridge';
  import { tokensFor, type ThemeEntry } from '../themes';
  import type { PopupProfile, PopupSnapshot } from '../popup/model';

  let { snapshot, legacyProfiles = [], unavailableReason = '', documentState = null as DocumentState | null, theme = null as ThemeEntry | null }: {
    snapshot?: PopupSnapshot;
    legacyProfiles?: string[];
    unavailableReason?: string;
    documentState?: DocumentState | null;
    theme?: ThemeEntry | null;
  } = $props();
  const hostEl = $host<HTMLElement>();
  // Custom properties inherit into the shadow root without injecting a legacy stylesheet.
  $effect(() => {
    if (!theme) return;
    for (const [token, value] of Object.entries(tokensFor(theme))) {
      hostEl.style.setProperty('--' + token, value);
    }
  });
  const wholeDocument = $derived(documentState ? documentStateCopy(documentState) : null);

  // Invariant 1: with no Connector the popup still renders the legacy list and says why the NEXORA half
  // is unavailable — never silently absent.
  const view = $derived(snapshot ?? {
    profiles: legacyProfiles.map((name, index) => ({
      displayName: name,
      plane: 'legacy' as const,
      profileType: undefined as PopupProfile['profileType'],
      color: undefined as PopupProfile['color'],
      selected: index === 0,
    })),
    applied: undefined as PopupSnapshot['applied'],
    health: 'unknown' as const,
    nexoraUnavailableReason: unavailableReason,
    phase: 'disconnected' as const,
  });

  let helpOpen = $state(false);
  let focusedIndex = $state(0);
  let announce = $state('');

  const healthLabels: Record<PopupSnapshot['health'], string> = {
    unknown: 'Состояние неизвестно',
    healthy: 'Connector и Core работают',
    degraded: 'Работает с ограничениями',
    down: 'Connector недоступен',
  };
  const ICON_GLYPH: Record<string, string> = {
    FixedProfile: '●',
    PacProfile: '▲',
    RuleListProfile: '◈',
    SwitchProfile: '◆',
    legacy: '◇',
  };

  const phaseLabels: Record<PopupSnapshot['phase'], string> = {
    disconnected: 'Отключено',
    connecting: 'Подключение…',
    connected: 'Подключено',
    blocked: 'Заблокировано',
    disconnecting: 'Отключение…',
  };

  function onFocusNext(delta: number): void {
    const list = tabbables(rootEl);
    if (!list.length) return;
    const current = list.indexOf(document.activeElement as HTMLElement);
    const next = Math.min(Math.max(current + delta, 0), list.length - 1);
    list[next]?.focus();
  }

  // js-addrule / js-temprule / js-direct / js-system / js-external: browser-plane quick actions. They
  // travel to the shell over the runtime message bridge (the shell owns the state; the popup is a view)
  // and keep working with no Connector — the parity row's own requirement. A verdict is announced.
  function browserPlane(action: 'add-rule' | 'temp-rule' | 'select-builtin'): void {
    announce = action;
    void toggleSiteRule().then((outcome) => {
      announce = action + ': ' + outcome;
    }).catch((error: unknown) => {
      announce = action + ': ' + (error instanceof Error ? error.message : 'не выполнено');
    });
  }

  const handlers: KeyboardHandlers = {
    selectBySlot(slot: number) {
      const profile = view.profiles[slot - 1];
      if (profile) select(profile);
    },
    openOptions() {
      window.open('nexora-options.html', '_blank');
    },
    announceSelection() {
      announce = selected?.displayName ?? phaseLabel;
    },
    selectBuiltin(which) {
      browserPlane('select-builtin');
    },
    addRule() {
      browserPlane('add-rule');
    },
    tempRule() {
      browserPlane('temp-rule');
    },
    toggleHelp() {
      helpOpen = !helpOpen;
    },
    moveFocus(delta: number) {
      onFocusNext(delta);
    },
    activateFocused() {
      const profile = view.profiles[focusedIndex];
      if (profile) select(profile);
    },
  };

  // Invariant 3: converge-to-desired-state — selecting the selected profile is a no-op, and the selection
  // request carries the profile's own id so a double tap cannot queue two switches.
  function select(profile: PopupProfile): void {
    if (profile.selected || profile.plane !== 'nexora' || !profile.nexoraId) return;
    window.dispatchEvent(new CustomEvent('nexora-popup-select', { detail: { profileId: profile.nexoraId } }));
  }

  const selected = $derived(view.profiles.find((profile) => profile.selected));
  const phaseLabel = $derived(phaseLabels[view.phase]);

  let rootEl: HTMLElement;
  function autoResizeAction(node: HTMLElement): { destroy(): void } {
    const disconnect = autoResize(node);
    return { destroy: disconnect };
  }

  function onKeydown(event: KeyboardEvent): void {
    const next = handleKey(event.key, { helpOpen, focusedIndex, announce }, view.profiles, handlers);
    if (next !== undefined) {
      helpOpen = next.helpOpen;
      focusedIndex = next.focusedIndex;
      announce = next.announce;
    }
    if (event.key === '/' || event.key === '?' || /^F?[0-9]$/.test(event.key) || ['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(event.key)) {
      event.preventDefault();
    }
  }
</script>

<main bind:this={rootEl} onkeydown={onKeydown} tabindex="-1" use:autoResizeAction>
  {#if wholeDocument}
    <div class="whole-document" role="alert">
      <h1>{wholeDocument.title}</h1>
      <p>{wholeDocument.body}</p>
    </div>
  {/if}
  <header>
    <span class="health health-{view.health}" role="status" aria-label={healthLabels[view.health]}></span>
    <span class="phase">{phaseLabel}</span>
  </header>

  {#if view.nexoraUnavailableReason}
    <p class="unavailable" role="note">{view.nexoraUnavailableReason}</p>
  {/if}

  {#if view.applied}
    <p class="applied">{view.applied.serverName}</p>
  {:else if selected}
    <p class="applied">{selected.displayName}</p>
  {/if}

  <ul class="profiles" aria-label="Профили">
    {#each view.profiles as profile, index (profile.displayName + index)}
      <li>
        <button
          type="button"
          class:selected={profile.selected}
          disabled={profile.plane !== 'nexora'}
          aria-current={profile.selected ? 'true' : undefined}
          onclick={() => select(profile)}
        >
          <span class="slot">{index + 1}</span>
          <span class="icon icon-{profile.profileType ?? 'legacy'}" style={profile.color ? 'outline-color: ' + profile.color : ''} aria-hidden="true">{ICON_GLYPH[profile.profileType ?? 'FixedProfile']}</span>
          {profile.displayName}
          {#if profile.plane === 'legacy'}<span class="plane">legacy</span>{/if}
        </button>
      </li>
    {/each}
  </ul>

  <p class="announce" role="status" aria-live="polite">{announce}</p>

  <button id="open-options" class="options" type="button" onclick={handlers.openOptions}>Настройки</button>

  {#if helpOpen}
    <div class="help" role="dialog" aria-label="Горячие клавиши">
      <ul>
        <li>1–9 — выбор профиля</li>
        <li>↑/↓ или j/k — навигация</li>
        <li>Enter — применить выбранный</li>
        <li>o — настройки</li>
        <li>i — объявить выбор</li>
        <li>? — эта справка, Esc — закрыть</li>
      </ul>
    </div>
  {/if}
</main>

<style>
  :host { display: block; min-width: 320px; background: #142235; color: #e5edf5; font: 13px/1.4 system-ui, sans-serif; }
  main { padding: 8px 10px; outline: none; }
  header { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; }
  .health { width: 10px; height: 10px; border-radius: 50%; background: #829bb1; }
  .health-healthy { background: #59c98f; }
  .health-degraded { background: #e0b25a; }
  .health-down { background: #d76a5c; }
  .phase { color: #829bb1; }
  .unavailable { margin: 6px 0; padding: 6px 8px; background: #1c2e43; border-left: 3px solid #e0b25a; font-size: 12px; }
  .applied { margin: 6px 0; font-weight: 600; }
  .profiles { list-style: none; margin: 6px 0; padding: 0; }
  .profiles button { display: flex; align-items: center; gap: 8px; width: 100%; padding: 6px 8px; background: transparent; color: inherit; border: 1px solid transparent; border-radius: 6px; cursor: pointer; text-align: left; font: inherit; }
  .profiles button:focus-visible, .profiles button:hover { background: #1c2e43; border-color: #3b536c; }
  .profiles button.selected { border-color: #79d2de; }
  .slot { color: #829bb1; font-variant-numeric: tabular-nums; }
  .plane { margin-left: auto; font-size: 10px; color: #829bb1; text-transform: uppercase; }
  .announce { height: 1em; margin: 4px 0; color: #829bb1; font-size: 11px; }
  .options { width: 100%; padding: 6px; background: #1c2e43; color: inherit; border: 1px solid #3b536c; border-radius: 6px; cursor: pointer; font: inherit; }
  .help { position: fixed; inset: 0; background: rgba(10, 20, 32, 0.92); padding: 16px; }
  .help ul { list-style: none; margin: 0; padding: 0; }
  .help li { margin: 6px 0; }
  .whole-document { padding: 12px; }
  .whole-document h1 { margin: 0 0 8px; font-size: 15px; }
  .icon { color: #829bb1; }
  .icon-FixedProfile { color: #79d2de; }
  .icon-PacProfile { color: #e0b25a; }
  .icon-RuleListProfile { color: #d76a5c; }
  .icon-SwitchProfile { color: #59c98f; }
  .icon-legacy { color: #829bb1; }
</style>
