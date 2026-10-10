// Окна поверх панели: уход с несохранённым, сохранение пресета, применение, импорт.
import type { KeyboardEvent, ReactNode } from 'react';
import type App from '../App';
import { END, INTEG } from '../meta';
import { fmtMs, frameAt, stepsText, stripStyle, thumb, total } from '../seq';
import { Ic, ThumbRow } from '../ui';

export function Modals({ app }: { app: App }) {
  const s = app.state;
  if (!s.modal) return null;
  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'Escape') app.closeModal();
  };

  if (s.modal === 'leave') {
    const ev = app.ev(s.editId);
    return (
      <Dialog titleId="m-leave-t" onKey={onKey}>
        <h2 className="modal-title" id="m-leave-t">
          Сохранить изменения?
        </h2>
        <p className="modal-text">
          В «{ev?.name}» есть несохранённые изменения. Если уйти без сохранения, свет останется прежним.
        </p>
        <div className="modal-actions">
          <button className="btn ghost" onClick={() => app.closeModal()}>
            Остаться
          </button>
          <button className="btn" onClick={() => app.leaveDiscard()}>
            Не сохранять
          </button>
          <button className="btn primary" autoFocus onClick={() => void app.leaveSave()}>
            Сохранить
          </button>
        </div>
      </Dialog>
    );
  }

  if (s.modal === 'savePreset' && s.draft) {
    const t = thumb(s.draft);
    const empty = !s.presetName.trim();
    return (
      <Dialog titleId="m-save-t" onKey={onKey}>
        <h2 className="modal-title" id="m-save-t">
          Сохранить как пресет
        </h2>
        <p className="modal-text">Пресет появится в библиотеке. Его можно применить к любому событию или отправить файлом.</p>
        <div className="thumb">
          <ThumbRow t={t} height={18} loopIcon={false} />
          <div className="thumb-meta">
            <span className="mono">{t.totalText}</span>
            <span>·</span>
            <span>{t.endShort}</span>
          </div>
        </div>
        <div className="field">
          <label className="field-label" htmlFor="preset-name">
            Название
          </label>
          <input
            id="preset-name"
            className="inp"
            type="text"
            maxLength={60}
            autoFocus
            value={s.presetName}
            onChange={(e) => app.setState({ presetName: e.target.value })}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void app.confirmSavePreset();
            }}
          />
        </div>
        <div className="modal-actions">
          <button className="btn ghost" onClick={() => app.closeModal()}>
            Отмена
          </button>
          <button className="btn primary" disabled={empty} onClick={() => void app.confirmSavePreset()}>
            Сохранить пресет
          </button>
        </div>
      </Dialog>
    );
  }

  if (s.modal === 'apply') {
    const p = s.presets.find((x) => x.id === s.applyId);
    return (
      <Dialog titleId="m-apply-t" wide onKey={onKey}>
        <h2 className="modal-title" id="m-apply-t">
          Применить «{p?.name}»
        </h2>
        <p className="modal-text">Выбери событие. Его текущий свет заменится этим пресетом.</p>
        <div className="radio-list" role="radiogroup" aria-labelledby="m-apply-t">
          <Targets app={app} name="apply-target" current={s.applyTarget} pick={(id) => app.setState({ applyTarget: id })} />
        </div>
        <div className="modal-actions">
          <button className="btn ghost" onClick={() => app.closeModal()}>
            Отмена
          </button>
          <button className="btn primary" disabled={!s.applyTarget} onClick={() => void app.confirmApply()}>
            Применить
          </button>
        </div>
      </Dialog>
    );
  }

  if (s.modal === 'import' && s.imp) {
    const imp = s.imp;
    if (imp.status === 'broken') {
      return (
        <Dialog titleId="m-imp-t" wide onKey={onKey}>
          <div className="broken">
            <div className="broken-ic">
              <Ic n="fileX" size="lg" />
            </div>
            <h2 className="modal-title" id="m-imp-t">
              Этот файл не получится открыть
            </h2>
            <div className="imp-file">
              <span className="mono">{imp.file}</span>
            </div>
            <div className="broken-reason">{imp.reason}</div>
            <div className="broken-hint">{imp.hint}</div>
          </div>
          <div className="modal-actions">
            <button className="btn ghost" autoFocus onClick={() => app.closeModal()}>
              Закрыть
            </button>
            <label className="btn primary" htmlFor="import-file-2">
              Выбрать другой файл
            </label>
            <input id="import-file-2" className="visually-hidden" type="file" accept=".hhbpreset,.json" onChange={app.onImportFile} />
          </div>
        </Dialog>
      );
    }
    const fr = s.play?.key === 'import' ? frameAt(imp.seq, s.now - s.play.start) : null;
    const t = thumb(imp.seq, fr ? fr.pos : null);
    return (
      <Dialog titleId="m-imp-t" wide onKey={onKey}>
        <div>
          <h2 className="modal-title" id="m-imp-t">
            Пресет из файла
          </h2>
          <div className="imp-file" style={{ marginTop: 6 }}>
            <Ic n="file" size="sm" />
            <span>{imp.file}</span>
          </div>
        </div>
        <div className="imp-stage" role="img" aria-label="Предпросмотр пресета">
          <div className="pr-strip" style={stripStyle(fr || { kind: 'ambient', idx: -1, phase: 'after', pos: 0 })} />
        </div>
        <div className="thumb">
          <ThumbRow t={t} height={18} loopIcon={false} />
          <div className="thumb-meta">
            <span className="mono">{t.totalText}</span>
            <span>·</span>
            <span>{stepsText(imp.seq.steps.length)}</span>
            <span>·</span>
            <span>{t.endShort}</span>
          </div>
        </div>
        <div className="field">
          <label className="field-label" htmlFor="imp-name">
            Название
          </label>
          <input
            id="imp-name"
            className="inp"
            type="text"
            maxLength={60}
            autoFocus
            value={imp.name}
            onChange={(e) => app.setImp({ name: e.target.value })}
          />
        </div>
        <div className="field">
          <div className="field-label" id="imp-target-l">
            Применить к событию
          </div>
          <div className="radio-list" role="radiogroup" aria-labelledby="imp-target-l">
            <Targets app={app} name="imp-target" current={imp.target} pick={(id) => app.setImp({ target: id })} withNone />
          </div>
        </div>
        <label className="check-row">
          <input type="checkbox" checked={imp.keep} onChange={() => app.setImp({ keep: !imp.keep })} />
          <span>Добавить в мои пресеты</span>
        </label>
        <div className="modal-actions">
          <button className="btn ghost" onClick={() => app.closeModal()}>
            Отмена
          </button>
          <button className="btn primary" disabled={!imp.target && !imp.keep} onClick={() => void app.confirmImport()}>
            {imp.target ? 'Применить' : 'Сохранить в пресеты'}
          </button>
        </div>
      </Dialog>
    );
  }
  return null;
}

function Dialog(props: { titleId: string; wide?: boolean; onKey: (e: KeyboardEvent) => void; children: ReactNode }) {
  return (
    <div className="overlay">
      <div
        className={props.wide ? 'card modal wide' : 'card modal'}
        role="dialog"
        aria-modal="true"
        aria-labelledby={props.titleId}
        onKeyDown={props.onKey}
      >
        {props.children}
      </div>
    </div>
  );
}

/** Список событий для выбора, куда применить пресет. */
function Targets(props: { app: App; name: string; current: string | null; pick: (id: string | null) => void; withNone?: boolean }) {
  const { app, current } = props;
  const radio = (key: string, on: boolean, pick: () => void, name: string, sub: string, segs: ReactNode) => (
    <label className={on ? 'radio-item cur' : 'radio-item'} key={key}>
      <input type="radio" name={props.name} checked={on} onChange={pick} />
      <span>
        <span className="radio-name" style={{ display: 'block' }}>
          {name}
        </span>
        <span className="radio-sub" style={{ display: 'block' }}>
          {sub}
        </span>
      </span>
      <span className="thumb">{segs}</span>
    </label>
  );
  return (
    <>
      {props.withNone && radio('none', !current, () => props.pick(null), 'Не применять', 'Только сохранить в мои пресеты', null)}
      {app.groupOrder().map((gk) => (
        <div key={gk} style={{ display: 'contents' }}>
          <div className="radio-group-label">{INTEG[gk].name}</div>
          {app.state.events
            .filter((e) => e.integ === gk)
            .map((e) =>
              radio(
                e.id,
                current === e.id,
                () => props.pick(e.id),
                e.name,
                e.seq.steps.length ? 'Сейчас: ' + fmtMs(total(e.seq)) + ', ' + END[e.seq.end].short : 'Свет не настроен',
                <ThumbRow t={thumb(e.seq)} cap={false} />,
              ),
            )}
        </div>
      ))}
    </>
  );
}
