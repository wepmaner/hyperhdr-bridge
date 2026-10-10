// Тексты и справочники панели: названия событий, интеграций, концовок, палитра.
import type { End, IntegKey } from './types';

export const END: Record<End, { short: string; title: string; desc: string }> = {
  hold: { short: 'горит, пока активно', title: 'Оставить гореть', desc: 'Последний цвет горит, пока событие активно' },
  return: { short: 'потом подсветка экрана', title: 'Вернуть подсветку', desc: 'Лента снова показывает картинку с экрана' },
  loop: { short: 'по кругу', title: 'Повторять', desc: 'Шаги идут по кругу, пока событие активно' },
};
export const ENDS: End[] = ['hold', 'return', 'loop'];

export const INTEG: Record<IntegKey, { name: string; mono: string; sub: string }> = {
  discord: { name: 'Discord', mono: 'D', sub: 'Микрофон, звук, каналы' },
  cs2: { name: 'Counter-Strike 2', mono: 'CS', sub: 'Смерть, флешки, бомба' },
  hyperhdr: { name: 'HyperHDR', mono: '', sub: 'Лента за монитором' },
};

/** Источник события. Идентификаторы — имена состояний моста (internal/state). */
export type Source = 'discord' | 'cs2';

const EVENTS: Record<string, { integ: Source; name: string; desc: string }> = {
  deafened: { integ: 'discord', name: 'Звук выключен', desc: 'Ты отключил звук в Discord' },
  muted: { integ: 'discord', name: 'Микрофон выключен', desc: 'Тебя не слышно в голосовом канале' },
  connected: { integ: 'discord', name: 'В голосовом канале', desc: 'Ты зашёл в голосовой канал' },
  speaking: { integ: 'discord', name: 'Говоришь', desc: 'Микрофон ловит твой голос' },
  cs2_dead: { integ: 'cs2', name: 'Смерть', desc: 'Тебя убили в раунде' },
  cs2_bomb_exploded: { integ: 'cs2', name: 'Бомба взорвалась', desc: 'Раунд закончился взрывом' },
  cs2_flashed: { integ: 'cs2', name: 'Флешка', desc: 'Тебя ослепило' },
  cs2_bomb: { integ: 'cs2', name: 'Бомба заложена', desc: 'Таймер бомбы пошёл' },
  cs2_bomb_hurry: { integ: 'cs2', name: 'Бомба: времени мало', desc: 'До взрыва осталось немного' },
  cs2_bomb_panic: { integ: 'cs2', name: 'Бомба: последние секунды', desc: 'Взрыв вот-вот' },
};

/** Событие, о котором панель не знает (добавлено в конфиг руками), показываем по имени. */
export function eventMeta(id: string) {
  return EVENTS[id] || { integ: (id.startsWith('cs2_') ? 'cs2' : 'discord') as Source, name: id, desc: '' };
}

export const PALETTE = [
  { hex: '#FF2A2A', name: 'Красный' },
  { hex: '#FF7A00', name: 'Оранжевый' },
  { hex: '#FFC400', name: 'Жёлтый' },
  { hex: '#FFFFFF', name: 'Белый' },
  { hex: '#2BE36B', name: 'Зелёный' },
  { hex: '#00C2FF', name: 'Голубой' },
  { hex: '#2F5BFF', name: 'Синий' },
  { hex: '#A64BFF', name: 'Фиолетовый' },
  { hex: '#FF3DA8', name: 'Розовый' },
];
