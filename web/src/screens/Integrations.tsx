// Экран «Интеграции»: HyperHDR (куда уходит свет) и источники событий.
import { useState, type ReactNode } from 'react';
import type App from '../App';
import { INTEG, type Source } from '../meta';
import type { IntegKey } from '../types';
import { Alert, Ic, Pill, Switch } from '../ui';
import { integProblem } from './Events';

export function IntegrationsScreen({ app }: { app: App }) {
  const s = app.state;
  const panel = s.panel!;
  const item = (key: IntegKey, tile: ReactNode) => (
    <button
      key={key}
      className={s.integSel === key ? 'integ-item cur' : 'integ-item'}
      aria-pressed={s.integSel === key}
      onClick={() => app.setState({ integSel: key })}
    >
      {tile}
      <div className="grow">
        <div className="integ-name">{INTEG[key].name}</div>
        <div className="integ-sub">{INTEG[key].sub}</div>
      </div>
      <Pill status={panel.status[key]} />
    </button>
  );
  const mono = (key: Source) => (
    <div className="mono-tile" aria-hidden="true">
      {INTEG[key].mono}
    </div>
  );

  return (
    <section className="content">
      <header className="page-head">
        <div>
          <h1 className="h1">Интеграции</h1>
          <p className="sub">Откуда приходят события и куда уходит свет. У каждой интеграции свои настройки.</p>
        </div>
      </header>
      <div className="integ-layout">
        <div className="integ-list">
          <div className="list-label">Подсветка</div>
          {item(
            'hyperhdr',
            <div className="mono-tile strip" aria-hidden="true">
              <span />
            </div>,
          )}
          <div className="list-label">Источники событий</div>
          {item('discord', mono('discord'))}
          {item('cs2', mono('cs2'))}
          <div className="slot">
            <div className="slot-ic" aria-hidden="true">
              <Ic n="plus" size="sm" />
            </div>
            <div>Здесь появятся новые интеграции — другие игры и приложения.</div>
          </div>
        </div>

        <div className="card integ-detail">
          {s.integSel === 'hyperhdr' && <HyperHDR app={app} />}
          {s.integSel === 'discord' && <Discord app={app} />}
          {s.integSel === 'cs2' && <CS2 app={app} />}
        </div>
      </div>
    </section>
  );
}

function EventChips({ app, integ }: { app: App; integ: Source }) {
  return (
    <>
      <div className="det-section">События</div>
      <div className="ev-chips">
        {app.state.events
          .filter((e) => e.integ === integ)
          .map((e) => (
            <button className="ev-chip" key={e.id} onClick={() => app.openEditor(e.id)}>
              <span className={e.enabled ? 'dot on' : 'dot'} />
              {e.name}
            </button>
          ))}
      </div>
    </>
  );
}

const isLocal = (host: string) => ['127.0.0.1', 'localhost', '::1'].includes(host);

function HyperHDR({ app }: { app: App }) {
  const s = app.state;
  const panel = s.panel!;
  const down = panel.status.hyperhdr !== 'ok';
  const h = panel.hyperhdr;
  const [edit, setEdit] = useState<{ host: string; port: string } | null>(null);
  const port = edit ? parseInt(edit.port, 10) : 0;
  const valid = !!edit && edit.host.trim() !== '' && port >= 1 && port <= 65535;
  const retryLabel = s.checking === 'hyperhdr' ? 'Проверяем…' : 'Повторить';

  return (
    <>
      <div className="det-head">
        <div className="mono-tile lg strip" aria-hidden="true">
          <span />
        </div>
        <div className="grow">
          <div className="det-name">
            HyperHDR <Pill status={panel.status.hyperhdr} />
          </div>
          <div className="det-desc">Программа, которая управляет лентой. Через неё панель включает цвета событий.</div>
        </div>
      </div>
      {down && (
        <Alert
          kind="err"
          title="Не получается связаться с HyperHDR"
          action={
            <button className="btn sm" onClick={() => app.retryHyper()}>
              {retryLabel}
            </button>
          }
        >
          Проверь, что HyperHDR запущен. Как только связь появится, свет снова начнёт реагировать на события.
        </Alert>
      )}
      <div className="det-section">Настройки</div>
      {edit ? (
        <form
          className="set-row first set-edit"
          onSubmit={(e) => {
            e.preventDefault();
            if (!valid) return;
            void app.setSettings({ hyperhdr: { host: edit.host.trim(), port } }).then((ok) => {
              if (!ok) return;
              setEdit(null);
              app.toast(app.status('hyperhdr') === 'ok' ? 'Связь с HyperHDR есть' : 'По этому адресу HyperHDR не отвечает', {
                err: app.status('hyperhdr') !== 'ok',
              });
            });
          }}
        >
          <div className="grow">
            <div className="set-name">Где запущен HyperHDR</div>
            <div className="edit-row">
              <label className="edit-field grow">
                <span className="set-desc">Адрес компьютера</span>
                <input className="inp mono" autoFocus value={edit.host} onChange={(e) => setEdit({ ...edit, host: e.target.value })} />
              </label>
              <label className="edit-field">
                <span className="set-desc">Порт</span>
                <input
                  className="inp mono"
                  type="number"
                  min={1}
                  max={65535}
                  value={edit.port}
                  onChange={(e) => setEdit({ ...edit, port: e.target.value })}
                />
              </label>
            </div>
            <div className="set-desc">Если HyperHDR на этом же компьютере, адрес — 127.0.0.1. Порт JSON-сервера обычно 19444.</div>
          </div>
          <div className="row-gap">
            <button type="button" className="btn sm ghost" onClick={() => setEdit(null)}>
              Отмена
            </button>
            <button type="submit" className="btn sm primary" disabled={!valid}>
              Сохранить
            </button>
          </div>
        </form>
      ) : (
        <div className="set-row first">
          <div>
            <div className="set-name">Где запущен HyperHDR</div>
            <div className="set-desc">
              {isLocal(h.host) ? 'На этом компьютере' : <span className="mono">{h.host}</span>} · порт{' '}
              <span className="mono">{h.port}</span>
            </div>
          </div>
          <button className="btn sm" onClick={() => setEdit({ host: h.host, port: String(h.port) })}>
            Изменить
          </button>
        </div>
      )}
      <div className="set-row">
        <div>
          <div className="set-name">Проверить ленту</div>
          <div className="set-desc">Лента мигнёт белым — так видно, что всё на связи.</div>
        </div>
        <button className="btn sm" disabled={down} onClick={() => void app.blinkStrip()}>
          Мигнуть
        </button>
      </div>
    </>
  );
}

function Discord({ app }: { app: App }) {
  const s = app.state;
  const panel = s.panel!;
  const st = panel.status.discord;
  const d = panel.discord;
  const busy = s.checking === 'discord';
  const connecting = d.state === 'connecting' || d.state === 'handshaked';

  return (
    <>
      <div className="det-head">
        <div className="mono-tile lg" aria-hidden="true">
          D
        </div>
        <div className="grow">
          <div className="det-name">
            Discord <Pill status={st} />
          </div>
          <div className="det-desc">Микрофон, звук и голосовые каналы в приложении Discord на этом компьютере.</div>
        </div>
      </div>
      {st === 'err' && (
        <Alert
          kind="err"
          title={connecting ? 'Подключаемся к Discord…' : 'Discord не отвечает'}
          action={
            <button className="btn sm" onClick={() => app.reconnectDiscord()}>
              {busy ? 'Подключаемся…' : 'Повторить'}
            </button>
          }
        >
          Открой Discord на этом компьютере — панель подключится сама. Если Discord спросит разрешение, нажми «Авторизовать».
          Пока связи нет, события Discord не сработают.
        </Alert>
      )}
      {st === 'off' && (
        <Alert
          kind="info"
          icon="off"
          title="Discord не подключён"
          action={
            <button className="btn sm primary" onClick={() => app.reconnectDiscord()}>
              {busy ? 'Подключаемся…' : 'Подключить'}
            </button>
          }
        >
          Автоподключение выключено. Подключись вручную или включи его ниже.
        </Alert>
      )}
      <div className="det-section">Настройки</div>
      <div className="set-row first">
        <div>
          <div className="set-name">Аккаунт</div>
          <div className="set-desc">{st === 'ok' && d.user ? d.user : 'Не подключён'}</div>
        </div>
        <button className="btn sm" onClick={() => app.reconnectDiscord()}>
          Переподключить
        </button>
      </div>
      <div className="set-row">
        <div>
          <div className="set-name" id="discord-auto-l">
            Подключаться автоматически
          </div>
          <div className="set-desc">Когда Discord открыт, панель подключается к нему без вопросов.</div>
        </div>
        <Switch on={d.auto} labelledBy="discord-auto-l" onToggle={() => void app.setSettings({ discordAuto: !d.auto })} />
      </div>
      <EventChips app={app} integ="discord" />
    </>
  );
}

function CS2({ app }: { app: App }) {
  const s = app.state;
  const panel = s.panel!;
  const st = panel.status.cs2;
  const c = panel.cs2;
  const [folder, setFolder] = useState<string | null>(null);
  const busy = s.checking === 'cs2';
  const fileOk = c.installed && !c.stale;
  const problem = st !== 'ok' ? integProblem('cs2', st, panel) : null;
  const canInstall = st === 'action' && !!c.dir && (!c.installed || !!c.stale);

  let fileText = 'Не установлен';
  if (c.installed) {
    fileText = c.stale
      ? 'Устарел — установи заново'
      : c.seen_data
        ? 'Установлен — игра сообщает о событиях'
        : 'Установлен — события пойдут, когда запустишь игру';
  }

  return (
    <>
      <div className="det-head">
        <div className="mono-tile lg" aria-hidden="true">
          CS
        </div>
        <div className="grow">
          <div className="det-name">
            Counter-Strike 2 <Pill status={st} />
          </div>
          <div className="det-desc">Смерть, флешки и всё, что происходит с бомбой в раунде.</div>
        </div>
      </div>
      {problem && (
        <Alert
          kind={st === 'err' ? 'err' : st === 'off' ? 'info' : 'warn'}
          title={canInstall ? 'Нужен файл в папке игры' : problem.title}
          action={
            canInstall && (
              <button className="btn sm primary" onClick={() => app.installCs2()}>
                {busy ? 'Устанавливаем…' : 'Установить файл'}
              </button>
            )
          }
        >
          {canInstall
            ? 'Чтобы игра рассказывала о событиях, в её папку нужно один раз положить небольшой файл. После этого перезапусти CS2.'
            : problem.text}
        </Alert>
      )}
      <div className="det-section">Настройки</div>
      {folder !== null ? (
        <form
          className="set-row first set-edit"
          onSubmit={(e) => {
            e.preventDefault();
            void app.setSettings({ cs2Folder: folder.trim() }, 'Папка игры сохранена').then((ok) => ok && setFolder(null));
          }}
        >
          <div className="grow">
            <div className="set-name">Папка игры</div>
            <div className="edit-row">
              <input
                className="inp mono grow"
                autoFocus
                aria-label="Путь к папке cfg игры"
                placeholder="…\Counter-Strike Global Offensive\game\csgo\cfg"
                value={folder}
                onChange={(e) => setFolder(e.target.value)}
              />
            </div>
            <div className="set-desc">Путь к папке cfg внутри игры. Оставь поле пустым — панель найдёт игру через Steam сама.</div>
          </div>
          <div className="row-gap">
            <button type="button" className="btn sm ghost" onClick={() => setFolder(null)}>
              Отмена
            </button>
            <button type="submit" className="btn sm primary">
              Сохранить
            </button>
          </div>
        </form>
      ) : (
        <div className="set-row first">
          <div className="grow">
            <div className="set-name">Папка игры</div>
            <div className="path mono">{c.dir || 'Не найдена'}</div>
          </div>
          <button className="btn sm" onClick={() => setFolder(c.folder)}>
            Изменить
          </button>
        </div>
      )}
      <div className="set-row">
        <div>
          <div className="set-name">Файл для игры</div>
          <div className="set-desc">{fileText}</div>
        </div>
        {fileOk && !!c.dir && (
          <button className="btn sm ghost" onClick={() => app.installCs2()}>
            {busy ? 'Устанавливаем…' : 'Переустановить'}
          </button>
        )}
      </div>
      <div className="set-row">
        <div>
          <div className="set-name" id="cs2-spec-l">
            Реагировать, когда наблюдаешь
          </div>
          <div className="set-desc">После смерти свет продолжит реагировать на бомбу, пока ты смотришь за командой.</div>
        </div>
        <Switch on={c.spectate} labelledBy="cs2-spec-l" onToggle={() => void app.setSettings({ cs2Spectate: !c.spectate })} />
      </div>
      <EventChips app={app} integ="cs2" />
    </>
  );
}
