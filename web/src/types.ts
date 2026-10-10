// Типы данных панели. Форма ответа сервера — internal/web/server.go (panel).

export type StepType = 'color' | 'pause';
export type End = 'hold' | 'return' | 'loop';

/** Шаг, как его хранит и отдаёт мост. */
export interface ApiStep {
  type: StepType;
  ms: number;
  color?: string;
  brightness?: number;
}
export interface ApiSeq {
  end: End;
  steps: ApiStep[];
}

/** Шаг в редакторе: у него есть id, а длительность может быть пустой, пока её набирают. */
export interface Step {
  id: string;
  type: StepType;
  ms: number | '';
  color?: string;
  bright?: number;
}
export interface Seq {
  end: End;
  steps: Step[];
}

/** ok — работает, err — нет связи, action — нужно что-то сделать, off — выключено. */
export type Status = 'ok' | 'err' | 'action' | 'off';
export type IntegKey = 'hyperhdr' | 'discord' | 'cs2';

export interface PanelEvent {
  id: string;
  enabled: boolean;
  active: boolean;
  seq: ApiSeq;
}
export interface Preset {
  id: string;
  name: string;
  own: boolean;
  from?: string;
  seq: ApiSeq;
}

export interface Panel {
  status: Record<IntegKey, Status>;
  hyperhdr: { host: string; port: number; error?: string };
  discord: { user: string; state: string; error?: string; auto: boolean };
  cs2: {
    supported: boolean;
    dir?: string;
    path?: string;
    installed: boolean;
    stale?: boolean;
    running: boolean;
    needs_restart: boolean;
    addr?: string;
    listening: boolean;
    seen_data: boolean;
    error?: string;
    enabled: boolean;
    folder: string;
    spectate: boolean;
  };
  events: PanelEvent[];
  presets: Preset[];
  settings: {
    autostart: { supported: boolean; enabled: boolean; stale?: boolean; error?: string };
  };
}

export type Screen = 'events' | 'editor' | 'presets' | 'integrations' | 'settings';
