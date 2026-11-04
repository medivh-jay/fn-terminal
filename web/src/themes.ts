import type { ITheme } from '@xterm/xterm';

export interface Theme {
  id: string;
  label: string;
  scheme: 'dark' | 'light';
  terminal: ITheme & {
    background: string;
    foreground: string;
    cursor: string;
    selectionBackground: string;
  };
  ui: Record<
    | 'bar'
    | 'active'
    | 'muted'
    | 'accent'
    | 'border'
    | 'hover'
    | 'subtle'
    | 'warning'
    | 'error'
    | 'success'
    | 'notice-bg'
    | 'notice-border',
    string
  >;
}

export const themeStorageKey = 'fnterminal.theme';
export const themes: readonly Theme[] = [
  {
    id: 'pine',
    label: '松绿',
    scheme: 'dark',
    terminal: {
      background: '#101516',
      foreground: '#d2ddd9',
      cursor: '#9ae6be',
      selectionBackground: '#37594b',
      black: '#24302e',
      red: '#ed9696',
      green: '#99d6ae',
      yellow: '#e5cc91',
      blue: '#8fb9dd',
      magenta: '#c2a3df',
      cyan: '#8dd4cd',
      white: '#dce8e2',
      brightBlack: '#788b82',
      brightRed: '#f7abab',
      brightGreen: '#b2e8c4',
      brightYellow: '#f1dba5',
      brightBlue: '#accfec',
      brightMagenta: '#d7bbef',
      brightCyan: '#abe4dd',
      brightWhite: '#f2f6f3',
    },
    ui: {
      bar: '#1b2321',
      active: '#131c18',
      muted: '#95a198',
      accent: '#a2dfb9',
      border: '#2a3431',
      hover: '#293b30',
      subtle: '#7c9083',
      warning: '#d0b680',
      error: '#e6a099',
      success: '#a6ddb7',
      'notice-bg': '#294335',
      'notice-border': '#5c8467',
    },
  },
  {
    id: 'graphite',
    label: '石墨',
    scheme: 'dark',
    terminal: {
      background: '#181818',
      foreground: '#dddddd',
      cursor: '#eeeeee',
      selectionBackground: '#414141',
      black: '#303030',
      red: '#e58a8a',
      green: '#97ca9b',
      yellow: '#ddc483',
      blue: '#90b8e0',
      magenta: '#c4a0d9',
      cyan: '#86c7cc',
      white: '#d4d4d4',
      brightBlack: '#858585',
      brightRed: '#f4a5a5',
      brightGreen: '#b0dfb2',
      brightYellow: '#f2dda4',
      brightBlue: '#b2d5f9',
      brightMagenta: '#dfbdf1',
      brightCyan: '#a7e1e6',
      brightWhite: '#ffffff',
    },
    ui: {
      bar: '#242424',
      active: '#1b1b1b',
      muted: '#ababab',
      accent: '#d0d8df',
      border: '#383838',
      hover: '#343434',
      subtle: '#929292',
      warning: '#ddc483',
      error: '#e58a8a',
      success: '#97ca9b',
      'notice-bg': '#303630',
      'notice-border': '#718a7b',
    },
  },
  {
    id: 'midnight',
    label: '午夜蓝',
    scheme: 'dark',
    terminal: {
      background: '#0f172a',
      foreground: '#dbeafe',
      cursor: '#93c5fd',
      selectionBackground: '#30476d',
      black: '#25334d',
      red: '#f5a0aa',
      green: '#8ed8b0',
      yellow: '#ead29b',
      blue: '#8fbcf5',
      magenta: '#c9adf0',
      cyan: '#85d6e5',
      white: '#d5e2f5',
      brightBlack: '#8498b4',
      brightRed: '#ffc0c7',
      brightGreen: '#b3f1ce',
      brightYellow: '#f7e6ba',
      brightBlue: '#bddcff',
      brightMagenta: '#e3cdff',
      brightCyan: '#b3eff7',
      brightWhite: '#f3f7ff',
    },
    ui: {
      bar: '#17243b',
      active: '#111d32',
      muted: '#9caec7',
      accent: '#93c5fd',
      border: '#2b3d59',
      hover: '#253a58',
      subtle: '#8ba1c0',
      warning: '#ead29b',
      error: '#f5a0aa',
      success: '#8ed8b0',
      'notice-bg': '#263e5c',
      'notice-border': '#6c94bd',
    },
  },
  {
    id: 'light',
    label: '浅色',
    scheme: 'light',
    terminal: {
      background: '#f6f8fa',
      foreground: '#24292f',
      cursor: '#0550ae',
      cursorAccent: '#f6f8fa',
      selectionBackground: '#c9def5',
      selectionForeground: '#24292f',
      black: '#24292f',
      red: '#a40e26',
      green: '#116329',
      yellow: '#7d4e00',
      blue: '#0550ae',
      magenta: '#6639ba',
      cyan: '#096b72',
      white: '#57606a',
      brightBlack: '#6e7781',
      brightRed: '#cf222e',
      brightGreen: '#1a7f37',
      brightYellow: '#9a6700',
      brightBlue: '#0969da',
      brightMagenta: '#8250df',
      brightCyan: '#0a7b83',
      brightWhite: '#1f2328',
    },
    ui: {
      bar: '#eaeef2',
      active: '#f6f8fa',
      muted: '#57606a',
      accent: '#0550ae',
      border: '#c8d0d9',
      hover: '#dbe3ec',
      subtle: '#636e7b',
      warning: '#9a6700',
      error: '#cf222e',
      success: '#1a7f37',
      'notice-bg': '#e0ecdf',
      'notice-border': '#77a17d',
    },
  },
];

export function resolveTheme(id: string | null): Theme {
  return themes.find((theme) => theme.id === id) ?? themes[0];
}

export function loadTheme(): Theme {
  try {
    return resolveTheme(localStorage.getItem(themeStorageKey));
  } catch {
    return themes[0];
  }
}

export function saveTheme(theme: Theme): void {
  try {
    localStorage.setItem(themeStorageKey, theme.id);
  } catch {
    /* A blocked preference store must not prevent a live theme change. */
  }
}

export function applyTheme(theme: Theme): void {
  const root = document.documentElement;
  root.dataset.theme = theme.id;
  root.style.colorScheme = theme.scheme;
  root.style.setProperty('--bg', theme.terminal.background);
  root.style.setProperty('--fg', theme.terminal.foreground);
  for (const [key, value] of Object.entries(theme.ui)) root.style.setProperty(`--${key}`, value);
  document
    .querySelector<HTMLMetaElement>('meta[name="theme-color"]')
    ?.setAttribute('content', theme.terminal.background);
  document
    .querySelector<HTMLMetaElement>('meta[name="color-scheme"]')
    ?.setAttribute('content', theme.scheme);
}
