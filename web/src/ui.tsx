// Мелкие общие детали интерфейса: иконки, выключатель, статус, миниатюра.
import type { ReactNode } from 'react';
import type { ThumbData } from './seq';
import type { Status } from './types';

const ICONS = {
  list: <path d="M9 6h11M9 12h11M9 18h11M4.5 6h.01M4.5 12h.01M4.5 18h.01" />,
  layers: (
    <>
      <path d="M12 3l9 5-9 5-9-5 9-5z" />
      <path d="M3 13l9 5 9-5" />
    </>
  ),
  plug: <path d="M9 3v5M15 3v5M6 8h12v3a6 6 0 0 1-12 0V8zM12 17v4" />,
  sliders: (
    <>
      <path d="M4 6h9M17 6h3M4 12h3M11 12h9M4 18h11M19 18h1" />
      <circle cx="15" cy="6" r="2" />
      <circle cx="9" cy="12" r="2" />
      <circle cx="17" cy="18" r="2" />
    </>
  ),
  warn: <path d="M12 4l9 16H3L12 4zM12 10v4M12 17h.01" />,
  bang: <path d="M12 7v6M12 17h.01" />,
  up: <path d="M6 15l6-6 6 6" />,
  down: <path d="M6 9l6 6 6-6" />,
  left: <path d="M15 6l-6 6 6 6" />,
  right: <path d="M9 6l6 6-6 6" />,
  back: <path d="M19 12H5M11 6l-6 6 6 6" />,
  check: <path d="M5 12.5l4.5 4.5L19 7.5" />,
  plus: <path d="M12 5v14M5 12h14" />,
  grip: (
    <>
      <circle className="f" cx="9" cy="6" r="1.4" />
      <circle className="f" cx="15" cy="6" r="1.4" />
      <circle className="f" cx="9" cy="12" r="1.4" />
      <circle className="f" cx="15" cy="12" r="1.4" />
      <circle className="f" cx="9" cy="18" r="1.4" />
      <circle className="f" cx="15" cy="18" r="1.4" />
    </>
  ),
  off: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M8 16L16 8" />
    </>
  ),
  stop: <rect className="f" x="6.5" y="6.5" width="11" height="11" rx="1.5" />,
  play: <path className="f" d="M8 5.5v13l10.5-6.5z" />,
  bulb: <path d="M9 18h6M10 21h4M12 3a6 6 0 0 0-3.8 10.6c.6.6.8 1.3.8 2.4h6c0-1.1.2-1.8.8-2.4A6 6 0 0 0 12 3z" />,
  monitor: (
    <>
      <rect x="3" y="4" width="18" height="12" rx="2" />
      <path d="M8 20h8M12 16v4" />
    </>
  ),
  hold: <rect className="f" x="3" y="9" width="18" height="6" rx="2" />,
  ret: <path d="M9 14L4 9l5-5M4 9h10.5a5.5 5.5 0 0 1 0 11H11" />,
  loop: <path d="M17 2l4 4-4 4M3 11V9a3 3 0 0 1 3-3h15M7 22l-4-4 4-4M21 13v2a3 3 0 0 1-3 3H3" />,
  dup: (
    <>
      <rect x="9" y="9" width="11" height="11" rx="2" />
      <path d="M15 9V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h3" />
    </>
  ),
  trash: <path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3" />,
  upload: <path d="M12 16V5M7 10l5-5 5 5M5 20h14" />,
  download: <path d="M12 4v11M7 10l5 5 5-5M5 20h14" />,
  file: (
    <>
      <path d="M14 3H6a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8l-5-5z" />
      <path d="M14 3v5h5" />
    </>
  ),
  fileX: (
    <>
      <path d="M14 3H6a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8l-5-5z" />
      <path d="M14 3v5h5M10 12l4 4M14 12l-4 4" />
    </>
  ),
} satisfies Record<string, ReactNode>;

export type IconName = keyof typeof ICONS;

/** Декоративная иконка: смысл всегда несёт текст или aria-label рядом. */
export function Ic({ n, size, className }: { n: IconName; size?: 'sm' | 'lg'; className?: string }) {
  return (
    <svg className={['ic', size, className].filter(Boolean).join(' ')} viewBox="0 0 24 24" aria-hidden="true">
      {ICONS[n]}
    </svg>
  );
}

export function Switch(props: { on: boolean; onToggle: () => void; label?: string; labelledBy?: string; disabled?: boolean }) {
  return (
    <button
      className={props.on ? 'switch on' : 'switch'}
      role="switch"
      aria-checked={props.on}
      aria-label={props.label}
      aria-labelledby={props.labelledBy}
      disabled={props.disabled}
      onClick={props.onToggle}
    />
  );
}

const PILLS: Record<Status, { cls: string; text: string }> = {
  ok: { cls: 'pill ok', text: 'Подключено' },
  action: { cls: 'pill warn', text: 'Требует действия' },
  err: { cls: 'pill err', text: 'Нет связи' },
  off: { cls: 'pill', text: 'Выключено' },
};

export function Pill({ status }: { status: Status }) {
  const p = PILLS[status];
  return (
    <span className={p.cls}>
      <span className="dot" />
      {p.text}
    </span>
  );
}

export function Live() {
  return (
    <span className="live">
      <span className="dot" />
      Сейчас активно
    </span>
  );
}

/** Полоска-миниатюра последовательности: сегменты шагов и «хвост»-концовка. */
export function ThumbRow({ t, height, loopIcon = true, cap = true }: { t: ThumbData; height?: number; loopIcon?: boolean; cap?: boolean }) {
  return (
    <div className="thumb-row" style={height ? { height } : undefined}>
      <div className="segs">
        {t.segs.map((sg, i) => (
          <span key={i} className={sg.cls} style={sg.style} />
        ))}
        {t.headStyle && <span className="thumb-head" style={t.headStyle} />}
      </div>
      {cap && (
        <span className={t.cap.cls} style={t.cap.style}>
          {loopIcon && t.isLoop && <Ic n="loop" size="sm" />}
        </span>
      )}
    </div>
  );
}

export function Alert(props: { kind: 'err' | 'warn' | 'info'; icon?: IconName; title?: string; children: ReactNode; action?: ReactNode; className?: string }) {
  return (
    <div className={['alert', props.kind, props.className].filter(Boolean).join(' ')}>
      <Ic n={props.icon || 'warn'} className="alert-ic" />
      <div className="grow">
        {props.title && <div className="alert-title">{props.title}</div>}
        <div className="alert-text" style={props.title ? undefined : { margin: 0 }}>
          {props.children}
        </div>
      </div>
      {props.action}
    </div>
  );
}
