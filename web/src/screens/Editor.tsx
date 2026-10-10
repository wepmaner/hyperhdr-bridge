// Редактор подсветки одного события: предпросмотр, таймлайн, настройки шага.
import type { CSSProperties } from 'react';
import type App from '../App';
import { END, ENDS, INTEG, PALETTE } from '../meta';
import {
  alpha,
  colorName,
  fmtMs,
  frameAt,
  glowStyle,
  ink,
  lastColor,
  MAX_MS,
  MIN_MS,
  msOf,
  plural,
  rgba,
  SHORT_MS,
  stepsText,
  thumb,
  total,
} from '../seq';
import type { Step } from '../types';
import { Alert, Ic, Live, Switch, ThumbRow } from '../ui';
import { integProblem } from './Events';

const GAP = 4; // зазор между шагами на таймлайне, px
const END_ICON = { hold: 'hold', return: 'ret', loop: 'loop' } as const;

export function EditorScreen({ app }: { app: App }) {
  const s = app.state;
  const ev = app.ev(s.editId);
  const d = s.draft;
  if (!ev || !d || !s.panel) return null;

  const n = d.steps.length;
  const sum = total(d);
  const dirty = app.isDirty();
  const playing = s.play?.key === 'editor';
  const fr = playing ? frameAt(d, s.now - s.play!.start) : null;
  const last = lastColor(d);
  const stInteg = s.panel.status[ev.integ];
  const hdrDown = s.panel.status.hyperhdr !== 'ok';

  // Геометрия таймлайна. Масштаб подбирается так, чтобы вся последовательность
  // помещалась в ширину; на время перетаскивания края он заморожен.
  const avail = Math.max(300, (s.trackW || 760) - 150);
  const scale = s.dragScale || Math.min(0.9, Math.max(0.03, avail / Math.max(sum, 300)));
  app.curScale = scale;
  const widths = d.steps.map((st) => Math.max(14, msOf(st) * scale));
  const lefts: number[] = [];
  let acc = 0;
  widths.forEach((w) => {
    lefts.push(acc);
    acc += w + GAP;
  });
  const trackLen = n ? acc - GAP : 0;
  /** Время от начала → координата на таймлайне. */
  const tpx = (t: number) => {
    let a = 0;
    for (let i = 0; i < n; i++) {
      const ms = msOf(d.steps[i]);
      if (t <= a + ms) return lefts[i] + (ms ? (t - a) / ms : 0) * widths[i];
      a += ms;
    }
    return trackLen;
  };
  const iv = [50, 100, 200, 250, 500, 1000, 2000, 5000].find((v) => v * scale >= 64) || 5000;
  const ticks: Array<{ left: number; label: string }> = [];
  for (let t = 0; t <= sum; t += iv) ticks.push({ left: tpx(t), label: t === 0 ? '0' : fmtMs(t) });

  const warnIds = d.steps.filter((st) => msOf(st) < SHORT_MS).map((st) => st.id);

  let capCls = 'endcap';
  let capStyle: CSSProperties = {};
  let capLabel = '';
  let capSub = '';
  if (d.end === 'hold') {
    if (last) {
      capCls = 'endcap hold';
      capStyle = {
        background: `linear-gradient(90deg, ${rgba(last.color, alpha(last.bright))}, ${rgba(last.color, 0.14)})`,
        color: ink(last.color, last.bright),
      };
      capLabel = 'Горит';
      capSub = 'пока активно';
    } else {
      capLabel = 'Нечему гореть';
      capSub = 'нет цвета';
    }
  }
  if (d.end === 'return') {
    capCls = 'endcap ret';
    capLabel = 'Подсветка';
    capSub = 'экрана';
  }
  if (d.end === 'loop') {
    capCls = 'endcap loop';
    capLabel = 'Сначала';
    capSub = 'по кругу';
  }

  let phaseText = 'Событие не активно';
  let phaseCls = 'pill';
  let statusText = n
    ? d.end === 'return'
      ? 'Нажми «Проиграть» — увидишь, как сработает событие.'
      : 'Нажми «Проиграть» — свет будет идти, пока не остановишь.'
    : 'Свет не настроен — лента показывает подсветку экрана.';
  if (fr) {
    if (fr.phase === 'steps') {
      phaseText = 'Событие сработало';
      phaseCls = 'live';
      statusText =
        `Шаг ${fr.idx + 1} из ${n} · ` +
        (fr.type === 'color' ? colorName(fr.color) : 'Пауза, подсветка экрана') +
        ' · ' +
        fmtMs(fr.ms || 0) +
        (d.end === 'loop' ? ' · по кругу' : '');
    } else if (fr.phase === 'hold') {
      phaseText = 'Событие активно';
      phaseCls = 'live';
      statusText = last ? 'Цвет горит, пока событие активно.' : 'Цвета нет — лента показывает подсветку экрана.';
    } else {
      phaseText = 'Событие закончилось';
      statusText = 'Лента вернулась к подсветке экрана.';
    }
  }

  const stripNote = hdrDown
    ? 'Нет связи с HyperHDR — играет только здесь'
    : s.testing
      ? 'Играет на ленте — посмотри за монитор'
      : n === 0
        ? ''
        : 'Играет здесь и на ленте';
  const canEnd = playing && !!fr && (fr.phase === 'hold' || d.end === 'loop');
  const problem = stInteg !== 'ok' ? integProblem(ev.integ, stInteg, s.panel) : null;
  const sel = d.steps.find((x) => x.id === s.selStep) || null;

  return (
    <section className="content wide">
      <div className="ed-head">
        <div>
          <button className="btn ghost sm ed-back" onClick={() => app.go('events')}>
            <Ic n="back" size="sm" />
            Все события
          </button>
          <div className="ed-name-row">
            <span className="chip-integ">{INTEG[ev.integ].name}</span>
            <h1 className="h1">{ev.name}</h1>
            {ev.active && ev.enabled && stInteg === 'ok' && <Live />}
          </div>
          <p className="sub">{ev.desc}</p>
        </div>
        <div className="ed-actions">
          <div className="sw-row">
            <Switch on={ev.enabled} labelledBy="ed-sw-label" onToggle={() => app.toggleEvent(ev.id)} />
            <span id="ed-sw-label">{ev.enabled ? 'Включено' : 'Выключено'}</span>
          </div>
          {dirty && (
            <span className="dirty" role="status">
              <span className="dot" />
              Есть несохранённые изменения
            </span>
          )}
          <button className="btn" disabled={!dirty} onClick={() => app.revert()}>
            Отменить
          </button>
          <button className="btn primary" disabled={!dirty} onClick={() => void app.save()}>
            <Ic n="check" size="sm" />
            Сохранить
          </button>
        </div>
      </div>

      {!ev.enabled && (
        <Alert
          kind="info"
          icon="off"
          className="ed-alert"
          title="Событие выключено"
          action={
            <button className="btn sm" onClick={() => app.toggleEvent(ev.id)}>
              Включить
            </button>
          }
        >
          На ленте оно не сработает. Настраивать, смотреть и проверять свет всё равно можно.
        </Alert>
      )}
      {problem && (
        <Alert
          kind="warn"
          className="ed-alert"
          title={problem.title}
          action={
            <button className="btn sm" onClick={() => app.go('integrations', { integSel: ev.integ })}>
              Открыть интеграцию
            </button>
          }
        >
          Пока это так, событие не сработает. Настроить свет и проверить его на ленте можно уже сейчас.
        </Alert>
      )}

      <div className="ed-grid">
        <div className="ed-left">
          {/* Предпросмотр */}
          <div className="card pv">
            <div className="pv-stage">
              <div className="pv-label">Предпросмотр</div>
              <div className="pv-phase">
                <span className={phaseCls}>{phaseText}</span>
              </div>
              <div
                className="pv-wall"
                role="img"
                aria-label={
                  fr && fr.kind === 'color'
                    ? 'Лента горит: ' + colorName(fr.color).toLowerCase()
                    : 'Лента показывает подсветку экрана'
                }
              >
                <div className="pv-glow" style={glowStyle(fr)} />
                <div className="pv-monitor">
                  <div className="pv-screen" />
                </div>
                <div className="pv-stand" />
              </div>
            </div>
            <div className="pv-bar">
              <button className="btn" disabled={n === 0} onClick={() => (playing ? app.stopPlay() : app.playDraft())}>
                <Ic n={playing ? 'stop' : 'play'} size="sm" />
                {playing ? 'Остановить' : 'Проиграть'}
              </button>
              {canEnd && (
                <button className="btn" onClick={() => app.stopPlay()}>
                  Событие закончилось
                </button>
              )}
              <div className="pv-status" aria-live="polite">
                {statusText}
              </div>
              <div className="pv-test">
                <span className="pv-note">
                  {stripNote && <Ic n="bulb" size="sm" />} {stripNote}
                </span>
              </div>
            </div>
          </div>

          {/* Таймлайн */}
          <div className="card tl">
            <div className="tl-head">
              <div>
                <h2 className="h2">Последовательность</h2>
                <div className="tl-sum">
                  {n > 0 ? (
                    <span>
                      Всего <span className="mono">{fmtMs(sum)}</span> · {stepsText(n)} · {END[d.end].short}
                    </span>
                  ) : (
                    <span>Пока нет ни одного шага</span>
                  )}
                  {warnIds.length > 0 && (
                    <button className="warn-chip" onClick={() => app.setState({ selStep: warnIds[0], sideTab: 'step' })}>
                      <Ic n="warn" size="sm" />
                      {warnIds.length}{' '}
                      {plural(warnIds.length, ['шаг слишком короткий', 'шага слишком короткие', 'шагов слишком короткие'])}
                    </button>
                  )}
                </div>
              </div>
              <div className="tl-tools">
                <button className="btn sm" onClick={() => app.addStep('color')}>
                  <Ic n="plus" size="sm" />
                  Цвет
                </button>
                <button className="btn sm" onClick={() => app.addStep('pause')}>
                  <Ic n="plus" size="sm" />
                  Пауза
                </button>
                <button className="btn sm ghost" disabled={n === 0} onClick={() => app.openSavePreset()}>
                  <Ic n="layers" size="sm" />
                  Сохранить как пресет
                </button>
              </div>
            </div>

            {n === 0 ? (
              <div className="empty">
                <div className="empty-ic">
                  <Ic n="bulb" size="lg" />
                </div>
                <div className="empty-title">Свет для этого события ещё не настроен</div>
                <div className="empty-text">
                  Пока лента просто показывает подсветку экрана. Добавь первый шаг или возьми готовый пресет.
                </div>
                <div className="row-gap">
                  <button className="btn primary" onClick={() => app.addStep('color')}>
                    <Ic n="plus" size="sm" />
                    Добавить цвет
                  </button>
                  <button className="btn" onClick={() => app.addStep('pause')}>
                    Начать с паузы
                  </button>
                  <button className="btn ghost" onClick={() => app.setState({ sideTab: 'presets' })}>
                    Выбрать пресет
                  </button>
                </div>
              </div>
            ) : (
              <>
                <div className="tl-scroll" ref={app.trackRef}>
                  <div className="tl-inner">
                    <div className="tl-ruler" style={{ width: Math.round(trackLen) }} aria-hidden="true">
                      {ticks.map((t, i) => (
                        <span key={i} className="tick" style={{ left: t.left }}>
                          {t.label}
                        </span>
                      ))}
                    </div>
                    <div className="tl-track">
                      {d.steps.map((st, i) => (
                        <Block
                          key={st.id}
                          app={app}
                          st={st}
                          index={i}
                          width={widths[i]}
                          selected={s.selStep === st.id}
                          on={!!fr && fr.phase === 'steps' && fr.idx === i}
                        />
                      ))}
                      <div className={capCls} style={capStyle}>
                        <Ic n={END_ICON[d.end]} size="sm" />
                        <span>{capLabel}</span>
                        <span style={{ fontWeight: 400, opacity: 0.85 }}>{capSub}</span>
                      </div>
                      {fr && <div className="playhead" style={{ left: fr.phase === 'steps' ? tpx(fr.pos) : trackLen + 10 + 64 }} />}
                    </div>
                  </div>
                </div>
                <div className="tl-foot">
                  <span>Потяни правый край шага — изменится длительность</span>
                  <span>Перетащи шаг — поменяется порядок</span>
                  <span>
                    <span className="kbd">←</span> <span className="kbd">→</span> выбор, <span className="kbd">Alt</span> + стрелка
                    — сдвиг, <span className="kbd">Del</span> — удалить
                  </span>
                </div>

                <div className="ending">
                  <h2 className="h2">Чем закончить</h2>
                  <div className="ending-opts" role="radiogroup" aria-label="Чем закончить">
                    {ENDS.map((k) => (
                      <button
                        key={k}
                        className={d.end === k ? 'end-opt sel' : 'end-opt'}
                        role="radio"
                        aria-checked={d.end === k}
                        onClick={() => app.setEnd(k)}
                      >
                        <span className="end-ic">
                          <Ic n={END_ICON[k]} />
                        </span>
                        <span>
                          <span className="end-title" style={{ display: 'block' }}>
                            {END[k].title}
                          </span>
                          <span className="end-desc" style={{ display: 'block' }}>
                            {END[k].desc}
                          </span>
                        </span>
                      </button>
                    ))}
                  </div>
                  {d.end === 'hold' && !last && (
                    <Alert kind="warn">
                      В последовательности нет ни одного цвета — гореть будет нечему. Добавь цвет или выбери другой конец.
                    </Alert>
                  )}
                </div>
              </>
            )}
          </div>
        </div>

        {/* Боковая панель: шаг и пресеты */}
        <aside className="card ed-side" aria-label="Настройки шага и пресеты">
          <div className="tabs" role="tablist">
            <button
              className={s.sideTab === 'step' ? 'tab cur' : 'tab'}
              role="tab"
              aria-selected={s.sideTab === 'step'}
              onClick={() => app.setState({ sideTab: 'step' })}
            >
              Шаг
            </button>
            <button
              className={s.sideTab === 'presets' ? 'tab cur' : 'tab'}
              role="tab"
              aria-selected={s.sideTab === 'presets'}
              onClick={() => app.setState({ sideTab: 'presets' })}
            >
              Пресеты
            </button>
          </div>

          {s.sideTab === 'step' &&
            (sel ? (
              <Inspector app={app} st={sel} index={d.steps.indexOf(sel)} count={n} />
            ) : (
              <div className="side-empty">
                Выбери шаг на таймлайне или добавь новый — здесь появятся его цвет, яркость и длительность.
              </div>
            ))}

          {s.sideTab === 'presets' && (
            <>
              <div className="side-list">
                {s.presets.map((p) => {
                  const t = thumb(p.seq);
                  return (
                    <div className="side-item" key={p.id}>
                      <div className="side-item-top">
                        <span className="side-item-name">{p.name}</span>
                        <button
                          className="btn sm"
                          aria-label={'Подставить «' + p.name + '» в редактор'}
                          onClick={() => app.applyPresetToDraft(p.id)}
                        >
                          Подставить
                        </button>
                      </div>
                      <div className="thumb">
                        <ThumbRow t={t} />
                        <div className="thumb-meta">
                          <span className={p.own ? 'tag own' : 'tag'}>{p.own ? 'Свой' : 'Встроенный'}</span>
                          <span className="mono">{t.totalText}</span>
                          <span>·</span>
                          <span>{t.endShort}</span>
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>
              <div className="side-foot2">
                <button className="btn sm" disabled={n === 0} onClick={() => app.openSavePreset()}>
                  Сохранить текущий как пресет
                </button>
                <button className="btn sm ghost" onClick={() => app.go('presets')}>
                  Все пресеты
                </button>
              </div>
            </>
          )}
        </aside>
      </div>
    </section>
  );
}

/** Один шаг на таймлайне. */
function Block(props: { app: App; st: Step; index: number; width: number; selected: boolean; on: boolean }) {
  const { app, st, width: w } = props;
  const isColor = st.type === 'color';
  const ms = msOf(st);
  const short = ms < SHORT_MS;
  const cls = [
    'blk',
    isColor ? 'color' : 'pause',
    props.selected ? 'sel' : '',
    props.on ? 'on' : '',
    app.state.stepDrop === st.id ? 'drop' : '',
  ].join(' ');
  const style: CSSProperties = { width: w };
  if (isColor) style.background = rgba(st.color, alpha(st.bright));
  return (
    <div
      className={cls}
      style={style}
      draggable
      onDragStart={(e) => {
        // Перетаскивание края меняет длительность, а не порядок.
        if (app.isResizing()) {
          e.preventDefault();
          return;
        }
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('text/plain', st.id);
        app.stepDragId = st.id;
      }}
      onDragOver={(e) => {
        if (!app.stepDragId) return;
        e.preventDefault();
        if (app.state.stepDrop !== st.id) app.setState({ stepDrop: st.id });
      }}
      onDrop={(e) => {
        e.preventDefault();
        app.dropStep(st.id);
      }}
      onDragEnd={() => {
        app.stepDragId = null;
        app.setState({ stepDrop: null });
      }}
    >
      <button
        className="blk-hit"
        style={{ color: isColor ? ink(st.color, st.bright) : '#C9D3E0' }}
        aria-label={`Шаг ${props.index + 1}: ${isColor ? 'цвет, ' + colorName(st.color).toLowerCase() : 'пауза'}, ${ms} мс`}
        onClick={() => app.setState({ selStep: st.id, sideTab: 'step' })}
        onKeyDown={(e) => app.stepKey(e, st.id)}
      >
        {w >= 60 && (
          <span className="blk-type">
            {!isColor && <Ic n="monitor" size="sm" />}
            {isColor ? 'Цвет' : 'Экран'}
          </span>
        )}
        <span className="blk-ms mono">{w >= 34 ? fmtMs(ms) : ''}</span>
      </button>
      {short && (
        <span
          className="blk-warn"
          title={
            isColor
              ? `Короче ${SHORT_MS} мс — на ленте вспышку почти не видно`
              : `Пауза короче ${SHORT_MS} мс — соседние вспышки сольются`
          }
        >
          <Ic n="bang" size="sm" />
        </span>
      )}
      <span
        className="blk-handle"
        title="Потяни, чтобы изменить длительность"
        aria-hidden="true"
        onPointerDown={(e) => app.onHandleDown(e, st.id)}
      />
    </div>
  );
}

/** Настройки выбранного шага: тип, длительность, цвет, яркость. */
function Inspector({ app, st, index, count }: { app: App; st: Step; index: number; count: number }) {
  const isColor = st.type === 'color';
  const ms = msOf(st);
  const color = (st.color || '#FF7A00').toUpperCase();
  const bright = st.bright == null ? 100 : st.bright;
  return (
    <div className="insp">
      <div className="insp-head">
        <div className="insp-title">
          Шаг {index + 1} из {count}
        </div>
        <div className="segctl" role="group" aria-label="Тип шага">
          <button className={isColor ? 'cur' : ''} aria-pressed={isColor} onClick={() => app.setType(st.id, 'color')}>
            Цвет
          </button>
          <button className={isColor ? '' : 'cur'} aria-pressed={!isColor} onClick={() => app.setType(st.id, 'pause')}>
            Пауза
          </button>
        </div>
      </div>

      <div className="field">
        <label className="field-label" htmlFor="step-ms">
          Длительность
        </label>
        <div className="ms-row">
          <input
            id="step-ms"
            className="inp mono"
            type="number"
            min={MIN_MS}
            max={MAX_MS}
            step={10}
            value={st.ms}
            onChange={(e) => {
              const v = parseInt(e.target.value, 10);
              app.setStep(st.id, { ms: isNaN(v) ? '' : Math.min(MAX_MS, v) });
            }}
            onBlur={() => {
              // Пустое или слишком маленькое значение поправляем, когда поле отпустили.
              if (ms < MIN_MS) app.setStep(st.id, { ms: MIN_MS });
            }}
          />
          <span className="unit">мс</span>
        </div>
        <div className="chips">
          {[100, 150, 250, 500, 1000].map((q) => (
            <button key={q} className={ms === q ? 'chip cur' : 'chip'} onClick={() => app.setStep(st.id, { ms: q })}>
              {fmtMs(q)}
            </button>
          ))}
        </div>
        {ms < SHORT_MS && (
          <div className="field-warn">
            <Ic n="warn" size="sm" />
            <span>
              {isColor
                ? `Короче ${SHORT_MS} мс — на настоящей ленте такую вспышку почти не видно.`
                : `Пауза короче ${SHORT_MS} мс — соседние вспышки сольются в одну.`}
            </span>
          </div>
        )}
      </div>

      {isColor ? (
        <>
          <div className="field">
            <div className="field-label">Цвет</div>
            <div className="swatches">
              {PALETTE.map((p) => (
                <button
                  key={p.hex}
                  className={p.hex === color ? 'sw cur' : 'sw'}
                  style={{ background: p.hex }}
                  aria-label={p.name}
                  aria-pressed={p.hex === color}
                  onClick={() => app.setStep(st.id, { color: p.hex })}
                />
              ))}
              <label className="sw sw-custom" title="Свой цвет">
                <input
                  type="color"
                  value={color.toLowerCase()}
                  onChange={(e) => app.setStep(st.id, { color: e.target.value.toUpperCase() })}
                  aria-label="Свой цвет"
                />
                <span>
                  <Ic n="plus" size="sm" />
                </span>
              </label>
            </div>
            <div className="color-now">
              <span className="color-dot" style={{ background: color }} />
              <span>{colorName(color)}</span>
              <span className="mono">{color}</span>
            </div>
          </div>
          <div className="field">
            <label className="field-label" htmlFor="step-bright">
              <span>Яркость</span>
              <span className="mono">{bright}%</span>
            </label>
            <input
              id="step-bright"
              className="range"
              type="range"
              min={5}
              max={100}
              step={5}
              value={bright}
              onChange={(e) => app.setStep(st.id, { bright: parseInt(e.target.value, 10) || 100 })}
            />
          </div>
        </>
      ) : (
        <div className="note">
          <Ic n="monitor" size="sm" />
          <span>Во время паузы лента показывает обычную подсветку — цвета с экрана.</span>
        </div>
      )}

      <div className="insp-actions">
        <button className="btn sm icon" aria-label="Сдвинуть шаг раньше" disabled={index === 0} onClick={() => app.moveStep(st.id, -1)}>
          <Ic n="left" size="sm" />
        </button>
        <button
          className="btn sm icon"
          aria-label="Сдвинуть шаг позже"
          disabled={index === count - 1}
          onClick={() => app.moveStep(st.id, 1)}
        >
          <Ic n="right" size="sm" />
        </button>
        <button className="btn sm" onClick={() => app.dupStep(st.id)}>
          <Ic n="dup" size="sm" />
          Дублировать
        </button>
        <span className="grow" />
        <button className="btn sm ghost danger" onClick={() => app.delStep(st.id)}>
          <Ic n="trash" size="sm" />
          Удалить
        </button>
      </div>
    </div>
  );
}
