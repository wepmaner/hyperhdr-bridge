// Экран «События»: список по интеграциям, порядок сверху вниз — это приоритет.
import type App from '../App';
import type { Ev } from '../App';
import { INTEG, type Source } from '../meta';
import { thumb } from '../seq';
import type { Panel, Status } from '../types';
import { Alert, Ic, Live, Pill, Switch, ThumbRow } from '../ui';

/** Почему события интеграции сейчас не сработают — заголовок и пояснение. */
export function integProblem(key: Source, st: Status, panel: Panel): { title: string; text: string } {
  if (key === 'discord') {
    if (st === 'off') {
      return { title: 'Discord не подключён', text: 'Автоподключение выключено — подключись вручную в интеграции.' };
    }
    return { title: 'Discord не отвечает', text: 'Пока связи нет, эти события не сработают.' };
  }
  const c = panel.cs2;
  if (st === 'off') return { title: 'CS2 выключена', text: 'Интеграция выключена в файле настроек.' };
  if (st === 'err') return { title: 'Панель не слышит CS2', text: c.error || 'Не получается принять события от игры.' };
  if (!c.dir) return { title: 'Не найдена папка CS2', text: 'Укажи папку игры в интеграции.' };
  if (!c.installed) return { title: 'CS2 пока не присылает события', text: 'Нужно один раз положить файл в папку игры.' };
  if (c.stale) return { title: 'Файл в папке CS2 устарел', text: 'Установи его заново в интеграции.' };
  if (c.needs_restart) return { title: 'Перезапусти CS2', text: 'Игра начнёт присылать события после перезапуска.' };
  return { title: 'CS2 требует действия', text: c.error || 'Открой интеграцию, чтобы посмотреть подробности.' };
}

export function EventsScreen({ app }: { app: App }) {
  const s = app.state;
  const panel = s.panel!;
  const order = app.groupOrder();

  return (
    <section className="content">
      <header className="page-head">
        <div>
          <h1 className="h1">События</h1>
          <p className="sub">
            Что делает свет, когда что-то происходит. Если сработают сразу два события, лента покажет то, что выше в
            списке — перетаскивай, чтобы поменять порядок.
          </p>
        </div>
      </header>

      {order.map((gk, gi) => {
        const st = panel.status[gk];
        const meta = INTEG[gk];
        const evs = s.events.filter((e) => e.integ === gk);
        const problem = st !== 'ok' ? integProblem(gk, st, panel) : null;
        return (
          <section className="group" aria-label={meta.name} key={gk}>
            <div className="group-head">
              <div className="mono-tile" aria-hidden="true">
                {meta.mono}
              </div>
              <div className="group-name">{meta.name}</div>
              <Pill status={st} />
              <span className="grow" />
              <div className="group-order">
                <span>Группа</span>
                <button
                  className="ord-btn"
                  aria-label={'Поднять группу ' + meta.name}
                  disabled={gi === 0}
                  onClick={() => app.moveGroup(gk, -1)}
                >
                  <Ic n="up" size="sm" />
                </button>
                <button
                  className="ord-btn"
                  aria-label={'Опустить группу ' + meta.name}
                  disabled={gi === order.length - 1}
                  onClick={() => app.moveGroup(gk, 1)}
                >
                  <Ic n="down" size="sm" />
                </button>
              </div>
            </div>
            {problem && (
              <Alert
                kind={st === 'err' ? 'err' : st === 'off' ? 'info' : 'warn'}
                title={problem.title}
                action={
                  <button className="btn sm" onClick={() => app.go('integrations', { integSel: gk })}>
                    Открыть интеграцию
                  </button>
                }
              >
                {problem.text}
              </Alert>
            )}
            <div className="ev-list">
              {evs.map((e, i) => (
                <EventRow key={e.id} app={app} e={e} live={e.active && e.enabled && st === 'ok'} first={i === 0} last={i === evs.length - 1} />
              ))}
            </div>
          </section>
        );
      })}
    </section>
  );
}

function EventRow({ app, e, live, first, last }: { app: App; e: Ev; live: boolean; first: boolean; last: boolean }) {
  const s = app.state;
  const t = thumb(e.seq);
  const cls = [
    'ev-row',
    e.enabled ? '' : 'is-off',
    s.evDrop === e.id && s.evDrag !== e.id ? 'drop' : '',
    s.evDrag === e.id ? 'dragging' : '',
  ].join(' ');
  return (
    <div
      className={cls}
      draggable
      onDragStart={(ev) => {
        ev.dataTransfer.effectAllowed = 'move';
        ev.dataTransfer.setData('text/plain', e.id);
        app.evDragId = e.id;
        app.setState({ evDrag: e.id });
      }}
      onDragOver={(ev) => {
        // Перетаскивать можно только внутри своей интеграции.
        const dragged = app.ev(app.evDragId);
        if (!dragged || dragged.integ !== e.integ) return;
        ev.preventDefault();
        if (s.evDrop !== e.id) app.setState({ evDrop: e.id });
      }}
      onDrop={(ev) => {
        ev.preventDefault();
        app.dropEvent(e.id);
      }}
      onDragEnd={() => {
        app.evDragId = null;
        app.setState({ evDrag: null, evDrop: null });
      }}
    >
      <div className="ev-order">
        <span className="grip" title="Перетащи, чтобы поменять порядок" aria-hidden="true">
          <Ic n="grip" size="sm" />
        </span>
        <div className="ord-btns">
          <button className="ord-btn" aria-label={'Поднять «' + e.name + '»'} disabled={first} onClick={() => app.moveEvent(e.id, -1)}>
            <Ic n="up" size="sm" />
          </button>
          <button className="ord-btn" aria-label={'Опустить «' + e.name + '»'} disabled={last} onClick={() => app.moveEvent(e.id, 1)}>
            <Ic n="down" size="sm" />
          </button>
        </div>
      </div>
      <div className="ev-main">
        <div className="ev-name">
          <span>{e.name}</span>
          {live && <Live />}
          {!e.enabled && <span className="pill">Выключено</span>}
        </div>
        <div className="ev-desc">{e.desc}</div>
      </div>
      <div className="thumb">
        {t.has ? (
          <>
            <ThumbRow t={t} />
            <div className="thumb-meta">
              <span className="mono">{t.totalText}</span>
              <span>·</span>
              <span>{t.endShort}</span>
            </div>
          </>
        ) : (
          <>
            <div className="thumb-empty" />
            <div className="thumb-meta">Свет не настроен</div>
          </>
        )}
      </div>
      <Switch on={e.enabled} label={e.name + ': ' + (e.enabled ? 'включено' : 'выключено')} onToggle={() => app.toggleEvent(e.id)} />
      <button className="btn sm" onClick={() => app.openEditor(e.id)}>
        Настроить
      </button>
    </div>
  );
}
