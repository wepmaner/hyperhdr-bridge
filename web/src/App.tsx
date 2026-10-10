// Состояние и действия панели. Экраны — в screens/, они только рисуют и зовут методы отсюда.
import { Component, type ChangeEvent } from 'react';
import { api, stream, type SettingsPatch } from './api';
import { eventMeta, type Source } from './meta';
import { EditorScreen } from './screens/Editor';
import { EventsScreen } from './screens/Events';
import { IntegrationsScreen } from './screens/Integrations';
import { Modals } from './screens/Modals';
import { PresetsScreen } from './screens/Presets';
import { SettingsScreen } from './screens/Settings';
import { lastColor, MAX_MS, MIN_MS, msOf, sameSeq, toApi, toDraft, total, uid } from './seq';
import type { End, IntegKey, Panel, Screen, Seq, Status, Step, StepType } from './types';
import { Ic } from './ui';

export interface Ev {
  id: string;
  integ: Source;
  name: string;
  desc: string;
  enabled: boolean;
  active: boolean;
  seq: Seq;
}
export interface Pr {
  id: string;
  name: string;
  own: boolean;
  from?: string;
  seq: Seq;
}
export type Imp =
  | { status: 'ok'; file: string; name: string; seq: Seq; target: string | null; keep: boolean }
  | { status: 'broken'; file: string; reason: string; hint: string };
type Modal = 'leave' | 'savePreset' | 'apply' | 'import' | null;
interface Toast {
  msg: string;
  err?: boolean;
  label?: string;
  act?: () => void;
}
/** Что сейчас играет в предпросмотре: редактор, окно импорта или пресет по id. */
interface Play {
  key: string;
  start: number;
  seq: Seq | null;
}
type Go = Partial<Pick<State, 'integSel' | 'presetFilter'>>;

interface State {
  panel: Panel | null;
  /** Мост перестал отвечать: показываем баннер, данные на экране могли устареть. */
  down: boolean;
  events: Ev[];
  presets: Pr[];
  screen: Screen;
  editId: string | null;
  draft: Seq | null;
  selStep: string | null;
  modal: Modal;
  imp: Imp | null;
  integSel: IntegKey;
  checking: string | null;
  sideTab: 'step' | 'presets';
  presetFilter: 'all' | 'builtin' | 'own';
  presetName: string;
  applyId: string | null;
  applyTarget: string | null;
  pending: { screen: Screen; extra: Go } | null;
  toast: Toast | null;
  play: Play | null;
  now: number;
  testing: boolean;
  trackW: number;
  dragScale: number | null;
  evDrag: string | null;
  evDrop: string | null;
  stepDrop: string | null;
}

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** Адрес экрана в строке браузера — чтобы обновление страницы не выбрасывало из редактора. */
function parseHash(): { screen: Screen; arg: string } {
  const [name, arg = ''] = decodeURIComponent(location.hash.replace(/^#\/?/, '')).split('/');
  const screens: Screen[] = ['events', 'editor', 'presets', 'integrations', 'settings'];
  return { screen: screens.includes(name as Screen) ? (name as Screen) : 'events', arg };
}

export default class App extends Component<object, State> {
  private closeStream: (() => void) | null = null;
  private timer: number | null = null;
  private toastT = 0;
  private testT = 0;
  /** Номер последнего запуска на ленте — чтобы запоздавший ответ не трогал чужой предпросмотр. */
  private testN = 0;
  private ro: ResizeObserver | null = null;
  private resizing: { id: string; x0: number; ms0: number; scale: number } | null = null;
  /** Событие, редактор которого нужно открыть, как только придут данные (из адреса страницы). */
  private wantEdit: string | null = null;
  /** Масштаб таймлайна (px на мс) с последней отрисовки — нужен перетаскиванию края шага. */
  curScale = 0.5;
  evDragId: string | null = null;
  stepDragId: string | null = null;

  constructor(props: object) {
    super(props);
    const route = parseHash();
    if (route.screen === 'editor') this.wantEdit = route.arg || null;
    const integs: IntegKey[] = ['hyperhdr', 'discord', 'cs2'];
    this.state = {
      panel: null,
      down: false,
      events: [],
      presets: [],
      screen: route.screen === 'editor' ? 'events' : route.screen,
      editId: null,
      draft: null,
      selStep: null,
      modal: null,
      imp: null,
      integSel: integs.includes(route.arg as IntegKey) ? (route.arg as IntegKey) : 'discord',
      checking: null,
      sideTab: 'step',
      presetFilter: 'all',
      presetName: '',
      applyId: null,
      applyTarget: null,
      pending: null,
      toast: null,
      play: null,
      now: 0,
      testing: false,
      trackW: 760,
      dragScale: null,
      evDrag: null,
      evDrop: null,
      stepDrop: null,
    };
  }

  componentDidMount() {
    this.closeStream = stream(
      (p) => this.setPanel(p),
      () => this.setState({ down: true }),
    );
    window.addEventListener('beforeunload', this.onBeforeUnload);
  }

  componentDidUpdate() {
    const s = this.state;
    let hash = '#/' + s.screen;
    if (s.screen === 'editor' && s.editId) hash += '/' + encodeURIComponent(s.editId);
    if (s.screen === 'integrations') hash += '/' + s.integSel;
    if (location.hash !== hash) history.replaceState(null, '', hash);
  }

  componentWillUnmount() {
    this.closeStream?.();
    this.stopTimer();
    clearTimeout(this.toastT);
    clearTimeout(this.testT);
    window.removeEventListener('beforeunload', this.onBeforeUnload);
    window.removeEventListener('pointermove', this.onMove);
    window.removeEventListener('pointerup', this.onUp);
    this.ro?.disconnect();
  }

  private onBeforeUnload = (e: BeforeUnloadEvent) => {
    if (this.isDirty()) e.preventDefault();
  };

  // ── Данные моста ──
  setPanel(panel: Panel) {
    const events: Ev[] = panel.events.map((e) => ({
      ...eventMeta(e.id),
      id: e.id,
      enabled: e.enabled,
      active: e.active,
      seq: toDraft(e.seq),
    }));
    const presets: Pr[] = panel.presets.map((p) => ({ ...p, seq: toDraft(p.seq) }));
    this.setState({ panel, events, presets, down: false }, () => {
      const want = this.wantEdit;
      this.wantEdit = null;
      if (want && this.ev(want)) this.openEditor(want);
      // Событие, которое редактировали, могли убрать из конфига.
      if (this.state.editId && !this.ev(this.state.editId)) this.doGo('events');
    });
  }

  /** Выполняет запрос, который возвращает панель. Ошибку показывает уведомлением. */
  async run(fn: () => Promise<Panel>, ok?: string | (() => void)): Promise<boolean> {
    try {
      this.setPanel(await fn());
    } catch (e) {
      this.toast(errText(e), { err: true });
      return false;
    }
    if (typeof ok === 'string') this.toast(ok);
    else ok?.();
    return true;
  }

  // ── Общее ──
  ev(id: string | null) {
    return this.state.events.find((e) => e.id === id);
  }
  status(key: IntegKey): Status {
    return this.state.panel ? this.state.panel.status[key] : 'ok';
  }
  /** Порядок групп — по первому событию каждой интеграции в общем списке приоритетов. */
  groupOrder(): Source[] {
    const out: Source[] = [];
    this.state.events.forEach((e) => {
      if (!out.includes(e.integ)) out.push(e.integ);
    });
    return out;
  }
  isDirty() {
    const s = this.state;
    const ev = this.ev(s.editId);
    return !!(ev && s.draft && !sameSeq(s.draft, ev.seq));
  }
  toast(msg: string, opts: Omit<Toast, 'msg'> = {}) {
    clearTimeout(this.toastT);
    this.setState({ toast: { msg, ...opts } });
    this.toastT = window.setTimeout(() => this.setState({ toast: null }), opts.act || opts.err ? 5200 : 2800);
  }
  go(screen: Screen, extra: Go = {}) {
    if (this.state.screen === 'editor' && screen !== 'editor' && this.isDirty()) {
      this.setState({ modal: 'leave', pending: { screen, extra } });
      return;
    }
    this.doGo(screen, extra);
  }
  doGo(screen: Screen, extra: Go = {}) {
    this.stopPlay();
    this.setState({ screen, modal: null, pending: null, editId: null, draft: null, ...extra } as State);
  }
  openEditor(id: string) {
    const ev = this.ev(id);
    if (!ev) return;
    const draft = toDraft(toApi(ev.seq));
    this.stopPlay();
    this.setState({
      screen: 'editor',
      editId: id,
      draft,
      selStep: draft.steps[0] ? draft.steps[0].id : null,
      sideTab: 'step',
      modal: null,
      toast: null,
    });
  }

  // ── Список событий ──
  toggleEvent(id: string) {
    const e = this.ev(id);
    if (!e) return;
    const enabled = !e.enabled;
    // Выключатель отзывается сразу; ответ моста потом подтвердит или вернёт как было.
    this.setState({ events: this.state.events.map((x) => (x.id === id ? { ...x, enabled } : x)) });
    void this.run(() => api.setEvent(id, { enabled }));
  }
  private setOrder(events: Ev[]) {
    this.setState({ events, evDrag: null, evDrop: null });
    void this.run(() => api.setOrder(events.map((e) => e.id)));
  }
  /** Собирает общий список приоритетов: группы в заданном порядке, события внутри — как были. */
  private grouped(order: Source[], events = this.state.events) {
    return order.flatMap((k) => events.filter((e) => e.integ === k));
  }
  moveEvent(id: string, dir: number) {
    const evs = this.grouped(this.groupOrder());
    const e = evs.find((x) => x.id === id);
    if (!e) return;
    const from = evs.indexOf(e);
    const to = from + dir;
    if (!evs[to] || evs[to].integ !== e.integ) return;
    [evs[from], evs[to]] = [evs[to], evs[from]];
    this.setOrder(evs);
  }
  dropEvent(target: string) {
    const d = this.evDragId;
    this.evDragId = null;
    if (!d || d === target) {
      this.setState({ evDrag: null, evDrop: null });
      return;
    }
    const evs = this.grouped(this.groupOrder());
    const from = evs.findIndex((x) => x.id === d);
    const to0 = evs.findIndex((x) => x.id === target);
    const [item] = evs.splice(from, 1);
    let to = evs.findIndex((x) => x.id === target);
    if (from < to0) to += 1;
    evs.splice(to, 0, item);
    this.setOrder(evs);
  }
  moveGroup(key: Source, dir: number) {
    const g = this.groupOrder();
    const i = g.indexOf(key);
    const j = i + dir;
    if (j < 0 || j >= g.length) return;
    [g[i], g[j]] = [g[j], g[i]];
    this.setOrder(this.grouped(g));
  }

  // ── Редактор ──
  updateDraft(fn: (d: Seq) => void, extra: Partial<State> = {}) {
    if (!this.state.draft) return;
    const d: Seq = JSON.parse(JSON.stringify(this.state.draft));
    fn(d);
    this.setState({ draft: d, ...extra } as State);
  }
  addStep(type: StepType) {
    const sel = this.state.selStep;
    this.updateDraft((d) => {
      const last = lastColor(d);
      const st: Step =
        type === 'color'
          ? { id: uid(), type: 'color', ms: 200, color: last?.color || '#FF7A00', bright: last?.bright ?? 100 }
          : { id: uid(), type: 'pause', ms: 150 };
      let idx = d.steps.findIndex((x) => x.id === sel);
      if (idx < 0) idx = d.steps.length - 1;
      d.steps.splice(idx + 1, 0, st);
      this.setState({ selStep: st.id, sideTab: 'step' });
    });
  }
  setStep(id: string, patch: Partial<Step>) {
    this.updateDraft((d) => {
      const st = d.steps.find((x) => x.id === id);
      if (st) Object.assign(st, patch);
    });
  }
  setType(id: string, type: StepType) {
    this.updateDraft((d) => {
      const st = d.steps.find((x) => x.id === id);
      if (!st || st.type === type) return;
      st.type = type;
      if (type === 'color') {
        const others = d.steps.filter((x) => x.type === 'color' && x.id !== id);
        if (!st.color) st.color = others.length ? others[others.length - 1].color : '#FF7A00';
        if (st.bright == null) st.bright = 100;
      }
    });
  }
  setEnd(end: End) {
    this.updateDraft((d) => {
      d.end = end;
    });
  }
  dupStep(id: string) {
    this.updateDraft((d) => {
      const i = d.steps.findIndex((x) => x.id === id);
      if (i < 0) return;
      const copy = { ...d.steps[i], id: uid() };
      d.steps.splice(i + 1, 0, copy);
      this.setState({ selStep: copy.id });
    });
  }
  delStep(id: string) {
    this.updateDraft((d) => {
      const i = d.steps.findIndex((x) => x.id === id);
      if (i < 0) return;
      d.steps.splice(i, 1);
      const next = d.steps[i] || d.steps[i - 1];
      this.setState({ selStep: next ? next.id : null });
    });
  }
  moveStep(id: string, dir: number) {
    this.updateDraft((d) => {
      const i = d.steps.findIndex((x) => x.id === id);
      const j = i + dir;
      if (i < 0 || j < 0 || j >= d.steps.length) return;
      [d.steps[i], d.steps[j]] = [d.steps[j], d.steps[i]];
    });
  }
  dropStep(target: string) {
    const dId = this.stepDragId;
    this.stepDragId = null;
    if (!dId || dId === target) {
      this.setState({ stepDrop: null });
      return;
    }
    this.updateDraft(
      (d) => {
        const from = d.steps.findIndex((x) => x.id === dId);
        const to0 = d.steps.findIndex((x) => x.id === target);
        const [item] = d.steps.splice(from, 1);
        let to = d.steps.findIndex((x) => x.id === target);
        if (from < to0) to += 1;
        d.steps.splice(to, 0, item);
      },
      { stepDrop: null, selStep: dId },
    );
  }
  stepKey(e: React.KeyboardEvent, id: string) {
    const steps = this.state.draft?.steps || [];
    const i = steps.findIndex((x) => x.id === id);
    if ((e.key === 'ArrowRight' || e.key === 'ArrowLeft') && e.altKey) {
      e.preventDefault();
      this.moveStep(id, e.key === 'ArrowRight' ? 1 : -1);
      return;
    }
    if (e.key === 'ArrowRight' && steps[i + 1]) {
      e.preventDefault();
      this.setState({ selStep: steps[i + 1].id });
      return;
    }
    if (e.key === 'ArrowLeft' && steps[i - 1]) {
      e.preventDefault();
      this.setState({ selStep: steps[i - 1].id });
      return;
    }
    if (e.key === 'Delete' || e.key === 'Backspace') {
      e.preventDefault();
      this.delStep(id);
    }
  }
  /** Начало перетаскивания правого края шага — меняет длительность. */
  onHandleDown(e: React.PointerEvent, id: string) {
    e.preventDefault();
    e.stopPropagation();
    const st = this.state.draft?.steps.find((x) => x.id === id);
    if (!st) return;
    // Масштаб фиксируется на время перетаскивания, иначе шаг «убегал» бы из-под курсора.
    this.resizing = { id, x0: e.clientX, ms0: msOf(st), scale: this.curScale || 0.5 };
    this.setState({ selStep: id, dragScale: this.resizing.scale, sideTab: 'step' });
    window.addEventListener('pointermove', this.onMove);
    window.addEventListener('pointerup', this.onUp);
  }
  private onMove = (e: PointerEvent) => {
    const r = this.resizing;
    if (!r) return;
    let ms = Math.round((r.ms0 + (e.clientX - r.x0) / r.scale) / 10) * 10;
    ms = Math.max(MIN_MS, Math.min(20000, ms));
    this.setStep(r.id, { ms });
  };
  private onUp = () => {
    this.resizing = null;
    window.removeEventListener('pointermove', this.onMove);
    window.removeEventListener('pointerup', this.onUp);
    this.setState({ dragScale: null });
  };
  isResizing() {
    return !!this.resizing;
  }
  trackRef = (el: HTMLDivElement | null) => {
    this.ro?.disconnect();
    this.ro = null;
    if (el && typeof ResizeObserver !== 'undefined') {
      this.ro = new ResizeObserver(() => {
        const w = el.clientWidth;
        if (w && Math.abs(w - this.state.trackW) > 4) this.setState({ trackW: w });
      });
      this.ro.observe(el);
    }
  };
  async save(): Promise<boolean> {
    const s = this.state;
    const ev = this.ev(s.editId);
    if (!ev || !s.draft) return false;
    return this.run(
      () => api.setEvent(ev.id, { seq: toApi(s.draft!) }),
      'Сохранено. Свет для «' + ev.name + '» обновлён',
    );
  }
  revert() {
    const ev = this.ev(this.state.editId);
    if (!ev) return;
    const d = toDraft(toApi(ev.seq));
    this.setState({ draft: d, selStep: d.steps[0] ? d.steps[0].id : null });
  }

  // ── Предпросмотр ──
  startPlay(key: string, seq: Seq | null = null) {
    this.stopTimer();
    const now = performance.now();
    this.setState({ play: { key, start: now, seq }, now });
    this.timer = window.setInterval(this.tick, 33);
  }
  private tick = () => {
    const p = this.state.play;
    if (!p) {
      this.stopTimer();
      return;
    }
    const seq = p.key === 'editor' ? this.state.draft : p.seq;
    if (!seq) {
      this.stopPlay();
      return;
    }
    const sum = total(seq);
    const now = performance.now();
    const t = now - p.start;
    if (sum <= 0) {
      this.stopPlay();
      return;
    }
    // Окно импорта крутит предпросмотр по кругу, остальные останавливаются сами.
    if (p.key === 'import' && seq.end !== 'loop' && t > sum + 1600) {
      this.setState({ play: { ...p, start: now }, now });
      return;
    }
    if (p.key !== 'import' && seq.end === 'return' && t > sum + 900) {
      this.stopPlay();
      return;
    }
    if (p.key !== 'import' && p.key !== 'editor' && t > Math.max(sum + 1600, 6000)) {
      this.stopPlay();
      return;
    }
    this.setState({ now });
  };
  private stopTimer() {
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = null;
    }
  }
  stopPlay() {
    this.stopTimer();
    if (this.state.play) this.setState({ play: null });
    if (this.state.testing) {
      clearTimeout(this.testT);
      this.setState({ testing: false });
      // Лента возвращается к тому, что положено текущему состоянию.
      api.stopTest().catch(() => {});
    }
  }
  /**
   * «Проиграть» в редакторе: предпросмотр в браузере и, если лента на связи, то же
   * самое на ней. Удержание и повтор идут, пока не нажмут «Остановить».
   */
  playDraft() {
    const draft = this.state.draft;
    if (!draft) return;
    this.startPlay('editor');
    void this.playOnStrip('editor', draft);
  }
  /** «Проиграть» на карточке пресета: то же, что в редакторе, — в браузере и на ленте. */
  playPreset(pid: string) {
    const p = this.state.presets.find((x) => x.id === pid);
    if (!p) return;
    this.startPlay(pid, p.seq);
    void this.playOnStrip(pid, p.seq);
  }
  /** Повторяет на ленте предпросмотр с ключом key, если лента на связи. */
  private async playOnStrip(key: string, seq: Seq) {
    if (this.status('hyperhdr') !== 'ok') return;
    const n = ++this.testN;
    try {
      const { ms } = await api.test(toApi(seq));
      // Пока шёл запрос, запустили другой предпросмотр — лента уже его.
      if (n !== this.testN) return;
      // Пока шёл запрос, предпросмотр могли остановить — тогда гасим и ленту.
      if (this.state.play?.key !== key) {
        api.stopTest().catch(() => {});
        return;
      }
      clearTimeout(this.testT);
      this.setState({ testing: true });
      if (ms > 0) this.testT = window.setTimeout(() => this.setState({ testing: false }), ms + 200);
    } catch (e) {
      this.toast(errText(e), { err: true });
    }
  }

  // ── Пресеты ──
  openSavePreset() {
    const ev = this.ev(this.state.editId);
    this.setState({ modal: 'savePreset', presetName: ev ? ev.name : '' });
  }
  async confirmSavePreset() {
    const s = this.state;
    const name = s.presetName.trim();
    if (!name || !s.draft) return;
    const ev = this.ev(s.editId);
    const ok = await this.run(() => api.addPreset(name, ev ? ev.name : '', toApi(s.draft!)));
    if (!ok) return;
    this.setState({ modal: null });
    this.toast('Пресет «' + name + '» сохранён', {
      label: 'К пресетам',
      act: () => this.go('presets', { presetFilter: 'own' }),
    });
  }
  applyPresetToDraft(pid: string) {
    const p = this.state.presets.find((x) => x.id === pid);
    if (!p) return;
    const d = toDraft(toApi(p.seq));
    this.setState({ draft: d, selStep: d.steps[0] ? d.steps[0].id : null });
    this.toast('«' + p.name + '» подставлен. Сохрани, чтобы применить');
  }
  openApply(pid: string) {
    this.setState({ modal: 'apply', applyId: pid, applyTarget: null });
  }
  async confirmApply() {
    const s = this.state;
    const p = s.presets.find((x) => x.id === s.applyId);
    const ev = this.ev(s.applyTarget);
    if (!p || !ev) return;
    const ok = await this.run(() => api.setEvent(ev.id, { seq: toApi(p.seq) }));
    if (!ok) return;
    this.setState({ modal: null });
    this.toast('«' + p.name + '» применён к «' + ev.name + '»', { label: 'Открыть', act: () => this.openEditor(ev.id) });
  }
  /** Сохраняет пресет файлом .hhbpreset — его можно отправить другому человеку. */
  exportPreset(pid: string) {
    const p = this.state.presets.find((x) => x.id === pid);
    if (!p) return;
    const seq = toApi(p.seq);
    const data = { kind: 'hyperhdr-bridge-preset', version: 1, name: p.name, end: seq.end, steps: seq.steps };
    const file = (p.name.replace(/[\\/:*?"<>|]+/g, ' ').trim() || 'пресет') + '.hhbpreset';
    const url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }));
    const a = document.createElement('a');
    a.href = url;
    a.download = file;
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    this.toast('Файл «' + file + '» сохранён в загрузки');
  }
  async deletePreset(pid: string) {
    const p = this.state.presets.find((x) => x.id === pid);
    if (!p) return;
    if (this.state.play?.key === pid) this.stopPlay();
    const ok = await this.run(() => api.deletePreset(pid));
    if (!ok) return;
    this.toast('Пресет «' + p.name + '» удалён', {
      label: 'Вернуть',
      act: () => void this.run(() => api.addPreset(p.name, p.from || '', toApi(p.seq))),
    });
  }

  // ── Импорт ──
  onImportFile = (e: ChangeEvent<HTMLInputElement>) => {
    const input = e.target;
    const f = input.files && input.files[0];
    if (!f) return;
    const r = new FileReader();
    r.onload = () => {
      this.parseImport(f.name, String(r.result || ''));
      input.value = '';
    };
    r.onerror = () => {
      this.importBroken(f.name, 'Файл не получается прочитать.', 'Попробуй сохранить его заново и открыть ещё раз.');
      input.value = '';
    };
    r.readAsText(f);
  };
  private importBroken(file: string, reason: string, hint: string) {
    this.stopPlay();
    this.setState({ modal: 'import', imp: { status: 'broken', file, reason, hint } });
  }
  private parseImport(file: string, text: string) {
    // Файл приходит от другого человека — проверяем каждое поле, прежде чем показать.
    let data: any;
    try {
      data = JSON.parse(text);
    } catch {
      return this.importBroken(
        file,
        'Файл повреждён — его не получается прочитать.',
        'Скорее всего, он не до конца скачался. Попроси прислать его ещё раз.',
      );
    }
    if (!data || data.kind !== 'hyperhdr-bridge-preset') {
      return this.importBroken(
        file,
        'Это не пресет HyperHDR Bridge.',
        'Пресеты сохраняются здесь же кнопкой «Экспорт». Попроси прислать такой файл.',
      );
    }
    if ((Number(data.version) || 0) > 1) {
      return this.importBroken(
        file,
        'Пресет сделан в более новой версии приложения.',
        'Обнови HyperHDR Bridge и открой файл снова.',
      );
    }
    const steps: any[] | null = Array.isArray(data.steps) ? data.steps : null;
    const okEnd = ['hold', 'return', 'loop'].includes(data.end);
    const valid =
      okEnd &&
      steps &&
      steps.length > 0 &&
      steps.length <= 200 &&
      steps.every(
        (st) =>
          st &&
          (st.type === 'pause' || (st.type === 'color' && /^#[0-9a-fA-F]{6}$/.test(st.color))) &&
          Number.isFinite(st.ms) &&
          st.ms >= MIN_MS &&
          st.ms <= MAX_MS,
      );
    if (!valid || !steps) {
      return this.importBroken(
        file,
        'В файле есть шаги, которые панель не понимает.',
        'Возможно, файл правили вручную. Попроси прислать его заново.',
      );
    }
    const seq: Seq = {
      end: data.end,
      steps: steps.map((st) =>
        st.type === 'color'
          ? {
              id: uid(),
              type: 'color',
              ms: Math.round(st.ms),
              color: String(st.color).toUpperCase(),
              bright: Math.max(5, Math.min(100, Math.round(Number(st.brightness == null ? 100 : st.brightness)) || 100)),
            }
          : { id: uid(), type: 'pause', ms: Math.round(st.ms) },
      ),
    };
    const name = String(data.name || 'Пресет из файла').slice(0, 60);
    this.setState({ modal: 'import', imp: { status: 'ok', file, name, seq, target: null, keep: true } });
    this.startPlay('import', seq);
  }
  setImp(patch: Partial<Extract<Imp, { status: 'ok' }>>) {
    const imp = this.state.imp;
    if (imp && imp.status === 'ok') this.setState({ imp: { ...imp, ...patch } });
  }
  async confirmImport() {
    const imp = this.state.imp;
    if (!imp || imp.status !== 'ok') return;
    const name = imp.name.trim() || 'Пресет из файла';
    const ev = this.ev(imp.target);
    const seq = toApi(imp.seq);
    if (imp.keep && !(await this.run(() => api.addPreset(name, 'файл ' + imp.file, seq)))) return;
    if (ev && !(await this.run(() => api.setEvent(ev.id, { seq })))) return;
    this.stopPlay();
    this.setState({ modal: null, imp: null });
    if (ev) {
      this.toast('«' + name + '» применён к «' + ev.name + '»', { label: 'Открыть', act: () => this.openEditor(ev.id) });
    } else {
      this.toast('«' + name + '» добавлен в мои пресеты');
    }
  }
  closeModal() {
    if (this.state.play?.key === 'import') this.stopPlay();
    this.setState({ modal: null, pending: null });
  }
  leaveDiscard() {
    const p = this.state.pending || { screen: 'events' as Screen, extra: {} };
    this.doGo(p.screen, p.extra);
  }
  async leaveSave() {
    const p = this.state.pending || { screen: 'events' as Screen, extra: {} };
    if (await this.save()) this.doGo(p.screen, p.extra);
  }

  // ── Интеграции и настройки ──
  /** Действие с индикатором «идёт проверка» на кнопке. */
  async checked(key: string, fn: () => Promise<Panel>, done: (p: Panel) => void) {
    this.setState({ checking: key });
    const ok = await this.run(fn);
    this.setState({ checking: null });
    if (ok && this.state.panel) done(this.state.panel);
  }
  retryHyper() {
    void this.checked('hyperhdr', api.checkHyperHDR, (p) =>
      p.status.hyperhdr === 'ok'
        ? this.toast('Связь с HyperHDR есть')
        : this.toast('HyperHDR по-прежнему не отвечает', { err: true }),
    );
  }
  async blinkStrip() {
    try {
      await api.blinkStrip();
      this.toast('Лента мигнула белым');
    } catch (e) {
      this.toast(errText(e), { err: true });
    }
  }
  reconnectDiscord() {
    void this.checked('discord', api.reconnectDiscord, () => this.toast('Подключаемся к Discord…'));
  }
  installCs2() {
    void this.checked('cs2', api.installCS2, (p) =>
      this.toast(p.cs2.needs_restart ? 'Файл установлен. Перезапусти CS2' : 'Файл для игры установлен'),
    );
  }
  setSettings(patch: SettingsPatch, ok?: string) {
    return this.run(() => api.setSettings(patch), ok);
  }

  render() {
    const s = this.state;
    if (!s.panel) {
      return (
        <div className="app">
          <div className="boot">
            {s.down ? 'Приложение не отвечает. Проверь, что HyperHDR Bridge запущен.' : 'Загрузка…'}
          </div>
        </div>
      );
    }
    const stH = this.status('hyperhdr');
    const problems = (['hyperhdr', 'discord', 'cs2'] as IntegKey[]).filter((k) => {
      const st = this.status(k);
      return st === 'err' || st === 'action';
    }).length;
    const navKey = s.screen === 'editor' ? 'events' : s.screen;
    const nav = (key: Screen, label: string, icon: Parameters<typeof Ic>[0]['n'], badge?: number) => (
      <button
        className={'nav-btn' + (navKey === key ? ' cur' : '')}
        aria-current={navKey === key ? 'page' : undefined}
        onClick={() => this.go(key)}
      >
        <Ic n={icon} />
        <span className="grow">{label}</span>
        {!!badge && (
          <span className="nav-badge" aria-label="Требуют внимания">
            {badge}
          </span>
        )}
      </button>
    );

    return (
      <div className="app">
        <aside className="side">
          <div className="brand">
            <div className="brand-mark" aria-hidden="true">
              <span />
            </div>
            <div>
              <div className="brand-name">HyperHDR Bridge</div>
              <div className="brand-sub">Свет по событиям</div>
            </div>
          </div>
          <nav className="nav" aria-label="Разделы">
            {nav('events', 'События', 'list')}
            {nav('presets', 'Пресеты', 'layers')}
            {nav('integrations', 'Интеграции', 'plug', problems)}
            {nav('settings', 'Настройки', 'sliders')}
          </nav>
          <div className="side-foot">
            <div className={'side-foot-row ' + (stH === 'ok' ? 'ok' : 'err')}>
              <span className="dot" />
              {stH === 'ok' ? 'Лента на связи' : 'Нет связи с лентой'}
            </div>
            <div className="side-foot-sub">{stH === 'ok' ? 'HyperHDR отвечает' : 'Проверь, что HyperHDR запущен'}</div>
          </div>
        </aside>

        <main className="main">
          {s.down && (
            <div className="banner" role="alert">
              <Ic n="warn" />
              <span className="grow">Приложение не отвечает — данные на экране могли устареть. Проверь, что HyperHDR Bridge запущен.</span>
            </div>
          )}
          {!s.down && stH !== 'ok' && (
            <div className="banner" role="alert">
              <Ic n="warn" />
              <span className="grow">Нет связи с HyperHDR — сейчас события не меняют свет на ленте.</span>
              <button className="btn sm" onClick={() => this.retryHyper()}>
                {s.checking === 'hyperhdr' ? 'Проверяем…' : 'Повторить'}
              </button>
              <button className="btn sm ghost" onClick={() => this.go('integrations', { integSel: 'hyperhdr' })}>
                Подробнее
              </button>
            </div>
          )}
          {s.screen === 'events' && <EventsScreen app={this} />}
          {s.screen === 'editor' && <EditorScreen app={this} />}
          {s.screen === 'presets' && <PresetsScreen app={this} />}
          {s.screen === 'integrations' && <IntegrationsScreen app={this} />}
          {s.screen === 'settings' && <SettingsScreen app={this} />}
        </main>

        <Modals app={this} />

        {s.toast && (
          <div className={'toast' + (s.toast.err ? ' err' : '')} role="status">
            <Ic n={s.toast.err ? 'warn' : 'check'} size="sm" />
            <span>{s.toast.msg}</span>
            {s.toast.act && (
              <button
                className="toast-btn"
                onClick={() => {
                  const act = s.toast?.act;
                  this.setState({ toast: null });
                  act?.();
                }}
              >
                {s.toast.label}
              </button>
            )}
          </div>
        )}
      </div>
    );
  }
}
