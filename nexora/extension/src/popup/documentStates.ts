// The two whole-document popup states (packages/schema/presentation.json#/parity/rows): the permission
// denial and the not-controllable proxy. Both become §4 states rendered in the same document — a
// location change hides the list the user was trying to read, which is exactly what the legacy popup's
// redirects did. The snapshot names which whole-document state is in force; the popup renders it over
// everything else.

export type DocumentState = 'permission' | 'not-controllable';

export interface DocumentStateCopy {
  title: string;
  body: string;
}

const COPY: Record<DocumentState, DocumentStateCopy> = {
  permission: {
    title: 'Нет разрешения на прокси',
    body: 'Расширение не получило разрешение управлять прокси-настройками браузера. Предоставьте доступ в разрешениях расширения — список профилей появится без перезагрузки страницы.',
  },
  'not-controllable': {
    title: 'Прокси управляется другой программой',
    body: 'Другое расширение или политика удерживает прокси-настройки. Пока это так, NEXORA не может ни применить профиль, ни восстановить прежние настройки.',
  },
};

export function documentStateCopy(state: DocumentState): DocumentStateCopy {
  return COPY[state];
}
