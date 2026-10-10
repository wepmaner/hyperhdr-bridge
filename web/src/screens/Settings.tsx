// Экран «Настройки»: общие настройки приложения.
import type App from '../App';
import { Ic, Switch } from '../ui';

export function SettingsScreen({ app }: { app: App }) {
  const auto = app.state.panel!.settings.autostart;
  return (
    <section className="content">
      <header className="page-head">
        <div>
          <h1 className="h1">Настройки</h1>
          <p className="sub">Общие настройки приложения.</p>
        </div>
      </header>
      <div className="settings">
        <section className="card set-group">
          <h2 className="set-title">Запуск</h2>
          <div className="set-row first">
            <div>
              <div className="set-name" id="autostart-l">
                Запускать при входе в Windows
              </div>
              <div className="set-desc">
                Приложение само появится в трее после включения компьютера, и свет сразу начнёт реагировать на события. Окно
                панели при этом не откроется.
              </div>
              {!auto.supported && <div className="set-desc set-warn">На этой системе автозапуск не поддерживается.</div>}
              {auto.stale && (
                <div className="set-desc set-warn">
                  Сейчас автозапуск ведёт на другую копию программы. Выключи и включи его заново, чтобы запускалась эта.
                </div>
              )}
              {auto.error && <div className="set-desc set-warn">{auto.error}</div>}
            </div>
            <Switch
              on={auto.enabled}
              labelledBy="autostart-l"
              disabled={!auto.supported}
              onToggle={() => void app.setSettings({ autostart: !auto.enabled })}
            />
          </div>
        </section>
        <div className="slot">
          <div className="slot-ic" aria-hidden="true">
            <Ic n="plus" size="sm" />
          </div>
          <div>Здесь появятся другие общие настройки.</div>
        </div>
      </div>
    </section>
  );
}
