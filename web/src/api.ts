// Локальный API моста. Каждый запрос, меняющий состояние, возвращает панель целиком.
import type { ApiSeq, Panel } from './types';

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new Error('Приложение не отвечает. Проверь, что HyperHDR Bridge запущен.');
  }
  const data = await res.json().catch(() => null);
  if (!res.ok) throw new Error((data && data.error) || `Ошибка ${res.status}`);
  return data as T;
}

export interface SettingsPatch {
  autostart?: boolean;
  discordAuto?: boolean;
  cs2Spectate?: boolean;
  hyperhdr?: { host: string; port: number };
  cs2Folder?: string;
}

export const api = {
  panel: () => call<Panel>('GET', '/api/panel'),
  setEvent: (id: string, patch: { enabled?: boolean; seq?: ApiSeq }) =>
    call<Panel>('PUT', '/api/events/' + encodeURIComponent(id), patch),
  setOrder: (order: string[]) => call<Panel>('PUT', '/api/order', { order }),
  /** Проиграть на настоящей ленте. ms == 0 — играет, пока не остановят. */
  test: (seq: ApiSeq) => call<{ ms: number }>('POST', '/api/test', { seq }),
  stopTest: () => call<{ ok: boolean }>('POST', '/api/test/stop'),
  addPreset: (name: string, from: string, seq: ApiSeq) => call<Panel>('POST', '/api/presets', { name, from, seq }),
  deletePreset: (id: string) => call<Panel>('DELETE', '/api/presets/' + encodeURIComponent(id)),
  setSettings: (patch: SettingsPatch) => call<Panel>('PUT', '/api/settings', patch),
  checkHyperHDR: () => call<Panel>('POST', '/api/hyperhdr/check'),
  blinkStrip: () => call<{ ms: number }>('POST', '/api/hyperhdr/blink'),
  reconnectDiscord: () => call<Panel>('POST', '/api/discord/reconnect'),
  installCS2: () => call<Panel>('POST', '/api/cs2/install'),
};

/** Поток состояния панели. onDown сообщает, что мост перестал отвечать. */
export function stream(onPanel: (p: Panel) => void, onDown: () => void): () => void {
  const es = new EventSource('/api/stream');
  es.onmessage = (e) => {
    try {
      onPanel(JSON.parse(e.data));
    } catch {
      /* битый кадр — ждём следующий */
    }
  };
  // EventSource переподключается сам; нам остаётся только показать, что связи нет.
  es.onerror = onDown;
  return () => es.close();
}
