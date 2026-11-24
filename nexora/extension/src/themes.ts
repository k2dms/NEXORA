export const PALETTE_KEYS = ['base00', 'base01', 'base02', 'base03', 'base04', 'base05', 'base06', 'base07', 'base08', 'base09', 'base0A', 'base0B', 'base0C', 'base0D', 'base0E', 'base0F'] as const;

export const SEMANTIC_TOKENS = ['defaultBackground', 'lighterBackground', 'selectionBackground', 'highlightBackground', 'darkForeground', 'defaultForeground', 'lightForeground', 'lightBackground', 'primaryColor', 'secondaryColor', 'positiveColor', 'negativeColor', 'infoColor', 'warningColor', 'whiteColor'] as const;

export const TOKEN_COUNT = PALETTE_KEYS.length + SEMANTIC_TOKENS.length;

const HEX = /^#[0-9a-fA-F]{6}$/;

export interface ThemeEntry {
  key: string;
  name: string;
  dark: boolean;
  colors: Record<string, string>;
}

// Trim the inherited whitespace defects before refusing an invalid property value.
export function readThemeColors(colors: unknown): Record<string, string> | null {
  if (typeof colors !== 'object' || colors === null) return null;
  const out: Record<string, string> = {};
  for (const token of PALETTE_KEYS) {
    const raw = (colors as Record<string, unknown>)[token];
    if (typeof raw !== 'string') return null;
    const value = raw.trim();
    if (!HEX.test(value)) return null;
    out[token] = value;
  }
  return out;
}

export function readThemeCatalogue(themesJson: string): ThemeEntry[] {
  const parsed: Record<string, { name?: string; dark?: unknown; colors?: unknown }> = JSON.parse(themesJson);
  const entries: ThemeEntry[] = [];
  for (const [key, value] of Object.entries(parsed)) {
    if (typeof value !== 'object' || value === null) continue;
    const colors = readThemeColors(value.colors);
    if (!colors) continue;
    entries.push({
      key,
      name: typeof value.name === 'string' && value.name.length > 0 ? value.name : key,
      dark: value.dark === true,
      colors,
    });
  }
  return entries;
}

export function semanticFrom(base16: Record<string, string>): Record<string, string> {
  return {
    defaultBackground: base16.base00,
    lighterBackground: base16.base01,
    selectionBackground: base16.base02,
    highlightBackground: base16.base03,
    darkForeground: base16.base04,
    defaultForeground: base16.base05,
    lightForeground: base16.base06,
    lightBackground: base16.base07,
    primaryColor: base16.base09,
    secondaryColor: base16.base0D,
    positiveColor: base16.base0B,
    negativeColor: base16.base08,
    infoColor: base16.base0A,
    warningColor: base16.base0E,
    whiteColor: '#fff',
  };
}

export function tokensFor(entry: ThemeEntry): Record<string, string> {
  return { ...entry.colors, ...semanticFrom(entry.colors) };
}
