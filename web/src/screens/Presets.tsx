// Экран «Пресеты»: библиотека готовых последовательностей, импорт и экспорт файлом.
import type App from '../App';
import { frameAt, stepsText, stripStyle, thumb } from '../seq';
import { Alert, Ic, ThumbRow } from '../ui';

export function PresetsScreen({ app }: { app: App }) {
  const s = app.state;
  const ownCount = s.presets.filter((p) => p.own).length;
  const filters = [
    { key: 'all', label: 'Все · ' + s.presets.length },
    { key: 'builtin', label: 'Встроенные · ' + (s.presets.length - ownCount) },
    { key: 'own', label: 'Мои · ' + ownCount },
  ] as const;
  const list = s.presets.filter((p) => s.presetFilter === 'all' || (s.presetFilter === 'own' ? p.own : !p.own));

  return (
    <section className="content">
      <header className="page-head">
        <div>
          <h1 className="h1">Пресеты</h1>
          <p className="sub">Готовые последовательности света. Примени любой к любому событию или передай файлом другу.</p>
        </div>
        <div className="head-actions">
          <label className="btn" htmlFor="import-file">
            <Ic n="upload" size="sm" />
            Открыть файл пресета
          </label>
          <input id="import-file" className="visually-hidden" type="file" accept=".hhbpreset,.json" onChange={app.onImportFile} />
        </div>
      </header>

      <div className="filter" role="tablist" aria-label="Какие пресеты показать">
        {filters.map((f) => (
          <button
            key={f.key}
            className={s.presetFilter === f.key ? 'chip cur' : 'chip'}
            role="tab"
            aria-selected={s.presetFilter === f.key}
            onClick={() => app.setState({ presetFilter: f.key })}
          >
            {f.label}
          </button>
        ))}
      </div>

      {list.length === 0 && (
        <div className="empty">
          <div className="empty-title">Своих пресетов пока нет</div>
          <div className="empty-text">
            Собери свет в редакторе события и нажми «Сохранить как пресет». Или открой файл, который тебе прислали.
          </div>
        </div>
      )}

      <div className="preset-grid">
        {list.map((p) => {
          const playing = s.play?.key === p.id;
          const fr = playing ? frameAt(p.seq, s.now - s.play!.start) : null;
          const t = thumb(p.seq, fr ? fr.pos : null);
          return (
            <article className="card preset" key={p.id}>
              <div className="pr-stage">
                <div className="pr-strip" style={stripStyle(fr)} />
              </div>
              <div className="pr-body">
                <div className="pr-top">
                  <h3 className="pr-name">{p.name}</h3>
                  <span className={p.own ? 'tag own' : 'tag'}>{p.own ? 'Свой' : 'Встроенный'}</span>
                </div>
                <div className="thumb">
                  <ThumbRow t={t} />
                  <div className="thumb-meta">
                    <span className="mono">{t.totalText}</span>
                    <span>·</span>
                    <span>{stepsText(p.seq.steps.length)}</span>
                    <span>·</span>
                    <span>{t.endShort}</span>
                  </div>
                </div>
                {p.from && <div className="pr-from">Сделан из «{p.from}»</div>}
              </div>
              <div className="pr-actions">
                <button
                  className="btn sm icon"
                  aria-label={(playing ? 'Остановить «' : 'Проиграть «') + p.name + '»'}
                  onClick={() => (playing ? app.stopPlay() : app.playPreset(p.id))}
                >
                  <Ic n={playing ? 'stop' : 'play'} size="sm" />
                </button>
                <button className="btn sm" onClick={() => app.openApply(p.id)}>
                  Применить к событию
                </button>
                <button className="btn sm ghost" onClick={() => app.exportPreset(p.id)}>
                  <Ic n="download" size="sm" />
                  Экспорт
                </button>
                {p.own && (
                  <button className="btn sm ghost icon" aria-label={'Удалить «' + p.name + '»'} onClick={() => void app.deletePreset(p.id)}>
                    <Ic n="trash" size="sm" />
                  </button>
                )}
              </div>
            </article>
          );
        })}
      </div>

      <Alert kind="info" icon="file" className="share-note" title="Как поделиться светом">
        «Экспорт» сохраняет пресет в файл .hhbpreset. Отправь его другу — он откроет файл здесь же, посмотрит, как светится, и
        выберет событие.
      </Alert>
    </section>
  );
}
