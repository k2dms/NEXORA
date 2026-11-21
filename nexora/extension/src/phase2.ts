// Constraint 11 of the brief: "a button in the interface does not mean a finished feature". The controls
// here exist on a screen as disabled-with-a-reason, which is the compliant pattern VER-39 asks to prove:
// a control whose backend is Phase 2 renders disabled and says why, instead of being absent (which reads
// as "does not exist") or enabled (which trains users not to trust the UI). The reasons are the corpus's
// own, not invented copy.

export interface Phase2Control {
  id: string;
  label: string;
  reason: string;
}

export const PHASE2_CONTROLS: Phase2Control[] = [
  {
    id: 'diagnostics-export',
    label: 'Экспорт диагностики',
    reason: 'Файл diagnostics.get ещё не входит в состав сборки Connector — выгрузка появится вместе с командой (docs/architecture/36-observability.md §11).',
  },
  {
    id: 'sync-backend',
    label: 'Синхронизация через Gist',
    reason: 'Форма синхронизации переносится в Svelte по parity-матрице (packages/schema/presentation.json#/parity/rows); legacy-страница пока отвечает за неё.',
  },
];
