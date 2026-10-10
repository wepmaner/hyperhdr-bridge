// Чистая логика световых последовательностей: подсчёты, цвета, кадр предпросмотра.
import type { CSSProperties } from 'react';
import { END, PALETTE } from './meta';
import type { ApiSeq, ApiStep, Seq, Step } from './types';

export const MIN_MS = 10;
export const MAX_MS = 60000;
/** Короче этого шаг на настоящей ленте почти не виден. */
export const SHORT_MS = 100;

export const uid = () => 's' + Math.random().toString(36).slice(2, 9);
export const msOf = (s: { ms: number | '' }) => Number(s.ms) || 0;

/** Последовательность моста → черновик редактора. */
export function toDraft(seq: ApiSeq): Seq {
  return {
    end: seq.end,
    steps: seq.steps.map((s) =>
      s.type === 'color'
        ? { id: uid(), type: 'color', ms: s.ms, color: (s.color || '#FF7A00').toUpperCase(), bright: s.brightness || 100 }
        : { id: uid(), type: 'pause', ms: s.ms },
    ),
  };
}

/** Черновик редактора → то, что понимает мост. */
export function toApi(seq: Seq | ApiSeq): ApiSeq {
  return {
    end: seq.end,
    steps: (seq.steps as Array<Step | ApiStep>).map((s) => {
      const ms = Math.max(MIN_MS, Math.min(MAX_MS, Math.round(msOf(s))));
      if (s.type !== 'color') return { type: 'pause', ms };
      const bright = 'bright' in s ? s.bright : (s as ApiStep).brightness;
      return { type: 'color', ms, color: (s.color || '#FF7A00').toUpperCase(), brightness: bright || 100 };
    }),
  };
}

export const sameSeq = (a: Seq | ApiSeq, b: Seq | ApiSeq) => JSON.stringify(toApi(a)) === JSON.stringify(toApi(b));

export const total = (seq: { steps: Array<{ ms: number | '' }> }) => seq.steps.reduce((a, s) => a + msOf(s), 0);

export function lastColor(seq: Seq): Step | null {
  for (let i = seq.steps.length - 1; i >= 0; i--) if (seq.steps[i].type === 'color') return seq.steps[i];
  return null;
}

export function rgb(hex?: string): [number, number, number] {
  const h = String(hex || '#000000').replace('#', '');
  return [parseInt(h.slice(0, 2), 16) || 0, parseInt(h.slice(2, 4), 16) || 0, parseInt(h.slice(4, 6), 16) || 0];
}
export function rgba(hex: string | undefined, a: number) {
  const v = rgb(hex);
  return `rgba(${v[0]}, ${v[1]}, ${v[2]}, ${a.toFixed(3)})`;
}
/** Как яркость шага выглядит на экране: совсем тусклый шаг всё равно должен быть виден. */
export const alpha = (bright?: number) => 0.22 + 0.78 * ((bright == null ? 100 : bright) / 100);

/** Цвет текста, читаемый поверх шага. */
export function ink(hex?: string, bright?: number) {
  const v = rgb(hex);
  const l = ((0.2126 * v[0] + 0.7152 * v[1] + 0.0722 * v[2]) / 255) * alpha(bright);
  return l > 0.4 ? '#0B0F14' : '#F4F7FA';
}

export function fmtMs(ms: number | '') {
  const v = Number(ms) || 0;
  if (v < 1000) return v + ' мс';
  return String(Math.round(v / 10) / 100).replace('.', ',') + ' с';
}

export function plural(n: number, forms: [string, string, string]) {
  const a = Math.abs(n) % 100;
  const b = a % 10;
  if (a > 10 && a < 20) return forms[2];
  if (b > 1 && b < 5) return forms[1];
  if (b === 1) return forms[0];
  return forms[2];
}
export const stepsText = (n: number) => n + ' ' + plural(n, ['шаг', 'шага', 'шагов']);

export function colorName(hex?: string) {
  const p = PALETTE.find((x) => x.hex.toUpperCase() === String(hex).toUpperCase());
  return p ? p.name : 'Свой цвет';
}

/** Что горит в момент t (мс от срабатывания события). */
export interface Frame {
  kind: 'color' | 'ambient';
  color?: string;
  bright?: number;
  idx: number;
  phase: 'steps' | 'hold' | 'after';
  pos: number;
  ms?: number;
  type?: Step['type'];
}

export function frameAt(seq: Seq, t: number): Frame {
  const st = seq.steps;
  const sum = total(seq);
  if (!st.length || sum <= 0) return { kind: 'ambient', idx: -1, phase: 'after', pos: 0 };
  const tt = seq.end === 'loop' ? t % sum : t;
  if (tt < sum) {
    let acc = 0;
    for (let i = 0; i < st.length; i++) {
      const ms = msOf(st[i]);
      if (tt < acc + ms) {
        return {
          kind: st[i].type === 'color' ? 'color' : 'ambient',
          color: st[i].color,
          bright: st[i].bright,
          idx: i,
          phase: 'steps',
          pos: tt,
          ms,
          type: st[i].type,
        };
      }
      acc += ms;
    }
  }
  if (seq.end === 'hold') {
    const last = lastColor(seq);
    if (last) return { kind: 'color', color: last.color, bright: last.bright, idx: -1, phase: 'hold', pos: sum };
    return { kind: 'ambient', idx: -1, phase: 'hold', pos: sum };
  }
  return { kind: 'ambient', idx: -1, phase: 'after', pos: sum };
}

/** Свечение за монитором в предпросмотре редактора. */
export function glowStyle(fr: Frame | null): CSSProperties {
  if (fr && fr.kind === 'color') {
    const a = alpha(fr.bright);
    return {
      background: rgba(fr.color, a),
      boxShadow: `0 0 70px 26px ${rgba(fr.color, a * 0.9)}, 0 0 150px 60px ${rgba(fr.color, a * 0.45)}`,
    };
  }
  return {
    background: '#22304A',
    boxShadow:
      '-70px 0 90px 14px rgba(62, 110, 230, 0.5), 70px 0 90px 14px rgba(150, 70, 200, 0.45), 0 50px 90px 6px rgba(30, 160, 150, 0.38), 0 -40px 90px 6px rgba(80, 90, 210, 0.3)',
  };
}

const AMBIENT = 'linear-gradient(90deg, #3E6EE6, #8A4FD0 50%, #1EA096)';

/** Полоска-лента на карточке пресета. fr == null — пресет не играет. */
export function stripStyle(fr: Frame | null): CSSProperties {
  if (fr && fr.kind === 'color') {
    const b = fr.bright == null ? 100 : fr.bright;
    return {
      background: fr.color,
      opacity: Number((0.35 + 0.65 * (b / 100)).toFixed(2)),
      boxShadow: `0 0 22px 6px ${rgba(fr.color, alpha(fr.bright) * 0.8)}`,
    };
  }
  if (!fr) return { background: AMBIENT, opacity: 0.4 };
  return { background: AMBIENT, opacity: 0.85, boxShadow: '0 0 18px 3px rgba(110, 90, 220, 0.45)' };
}

/** Миниатюра последовательности: сегменты, «хвост»-концовка и подписи. */
export interface ThumbData {
  has: boolean;
  segs: Array<{ cls: string; style: CSSProperties }>;
  cap: { cls: string; style: CSSProperties };
  isLoop: boolean;
  totalText: string;
  endShort: string;
  headStyle: CSSProperties | null;
}

export function thumb(seq: Seq, pos?: number | null): ThumbData {
  const sum = total(seq);
  const segs = seq.steps.map((s) => {
    const flex = `${Math.max(msOf(s), 1)} 1 0px`;
    return s.type === 'color'
      ? { cls: 'seg', style: { flex, background: rgba(s.color, alpha(s.bright)) } }
      : { cls: 'seg seg-pause', style: { flex } };
  });
  const last = lastColor(seq);
  let cap: ThumbData['cap'] = { cls: 'seg-cap', style: {} };
  if (seq.end === 'hold' && last) {
    cap.style = { background: `linear-gradient(90deg, ${rgba(last.color, alpha(last.bright))}, ${rgba(last.color, 0.08)})` };
  }
  if (seq.end === 'return') cap = { cls: 'seg-cap amb', style: {} };
  if (seq.end === 'loop') cap = { cls: 'seg-cap loop', style: {} };
  const headShow = pos != null && sum > 0;
  return {
    has: seq.steps.length > 0,
    segs,
    cap,
    isLoop: seq.end === 'loop',
    totalText: fmtMs(sum),
    endShort: END[seq.end].short,
    headStyle: headShow ? { left: Math.min(100, (pos / sum) * 100).toFixed(2) + '%' } : null,
  };
}
