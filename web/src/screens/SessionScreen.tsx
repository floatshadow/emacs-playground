import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { APIPrefix } from '../protocol.gen'
import { api } from '../api/http'
import { useCurriculum, useEnsureSession, useMe, useSessionInfo, useStatus } from '../api/queries'
import { disconnectSession } from '../api/session/connection'
import { rpc } from '../api/session/rpc'
import { Button } from '../components/common/Button'
import { ConfirmDialog } from '../components/common/Dialog'
import { AppShell } from '../components/layout/AppShell'
import { SessionChip, TopBar } from '../components/layout/TopBar'
import { FrameSwitch } from '../components/screen/FrameSwitch'
import { LOCALES } from '../i18n'
import { clock, uptime } from '../lib/time'
import { useSessionStore } from '../store/session'
import { toast, useUiStore } from '../store/ui'
import styles from './Screens.module.css'

/**
 * Configuration modules (emacs.md §3) in load order, with the part that
 * introduces each; the curriculum index (parts[].modules) overrides the part
 * when it is loaded. Slice 1 ships the first twelve, the rest are stubs.
 */
const MODULES: { id: string; part: string }[] = [
  { id: 'base', part: '0' },
  { id: 'agent', part: '0' },
  { id: 'editing', part: '1' },
  { id: 'files', part: '2' },
  { id: 'windows', part: '3' },
  { id: 'completion', part: '4' },
  { id: 'git', part: '5' },
  { id: 'terminals', part: '6' },
  { id: 'lsp', part: '7' },
  { id: 'lang-rust', part: '7' },
  { id: 'lang-python', part: '7' },
  { id: 'lang-ocaml', part: '7' },
  { id: 'ui', part: '13' },
  { id: 'writing', part: '8' },
  { id: 'input', part: '8' },
  { id: 'org', part: '9' },
  { id: 'power', part: '10' },
  { id: 'remote', part: '12' },
]

/**
 * /session (frontend.md §2): session status, frame mode, modules, restart,
 * export and bring-your-own-terminal instructions, plus preferences.
 */
export default function SessionScreen() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const qc = useQueryClient()
  useEnsureSession()
  const me = useMe()
  const sessionId = useSessionStore((s) => s.sessionId)
  const modules = useSessionStore((s) => s.modules)
  const modulesRestarting = useSessionStore((s) => s.modulesRestarting)
  const info = useSessionInfo(sessionId, true)
  const status = useStatus(sessionId, true)
  const prefs = useUiStore()
  const [selected, setSelected] = useState<string[] | null>(null)
  const [confirmRestart, setConfirmRestart] = useState(false)
  const [confirmStop, setConfirmStop] = useState(false)
  const [busy, setBusy] = useState(false)
  const enabled = selected ?? status.data?.modules ?? modules
  const curriculum = useCurriculum()
  const partOf = useMemo(() => {
    const m = new Map<string, string>()
    for (const part of curriculum.data?.parts ?? [])
      for (const id of part.modules ?? []) if (!m.has(id)) m.set(id, String(part.n))
    return m
  }, [curriculum.data])
  const local = me.data?.mode !== 'server'

  useEffect(() => {
    document.title = `${t('session.title')} — Emacs Playground`
  }, [t])

  const toggle = (id: string) =>
    setSelected((cur) => {
      const base = cur ?? enabled
      return base.includes(id) ? base.filter((m) => m !== id) : [...base, id]
    })

  const applyModules = async () => {
    setBusy(true)
    try {
      const ordered = MODULES.map((m) => m.id).filter((id) => enabled.includes(id))
      const r = await rpc('modules', { enabled: ordered })
      toast(r.restarting ? t('session.modulesRestarting') : t('session.modulesApplied'))
      setSelected(null)
    } catch {
      // toasted
    } finally {
      setBusy(false)
    }
  }

  const restartEmacs = async () => {
    if (!sessionId) return
    setBusy(true)
    try {
      await api.post(`${APIPrefix}/sessions/${encodeURIComponent(sessionId)}/restart-emacs`, {})
      toast(t('session.restarting'))
      setConfirmRestart(false)
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'error')
    } finally {
      setBusy(false)
    }
  }

  const stopSession = async () => {
    if (!sessionId) return
    setBusy(true)
    try {
      await api.del(`${APIPrefix}/sessions/${encodeURIComponent(sessionId)}`)
      disconnectSession()
      useSessionStore.getState().reset()
      await qc.invalidateQueries()
      toast(t('session.stopped'))
      setConfirmStop(false)
      void navigate('/')
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'error')
    } finally {
      setBusy(false)
    }
  }

  const logout = async () => {
    try {
      await api.post(`${APIPrefix}/auth/logout`, {})
    } catch {
      // the cookie may already be gone
    }
    disconnectSession()
    useSessionStore.getState().reset()
    qc.clear()
    void navigate('/login')
  }

  const st = status.data
  return (
    <AppShell
      scroll
      topbar={
        <TopBar crumbs={[{ label: t('topbar.course'), to: '/' }, { label: t('session.title') }]}>
          <SessionChip />
        </TopBar>
      }
    >
      <main className={styles.page}>
        <h1>{t('session.title')}</h1>
        <p className={styles.lede}>{local ? t('session.ledeLocal') : t('session.ledeServer')}</p>

        <section className={styles.card} aria-labelledby="s-emacs">
          <h2 id="s-emacs">{t('session.emacs')}</h2>
          <dl className={styles.dl}>
            <dt>{t('session.state')}</dt>
            <dd>{info.data?.state ?? (sessionId ? '…' : t('session.none'))}</dd>
            <dt>{t('session.version')}</dt>
            <dd>{st?.emacs?.version ?? '…'}</dd>
            <dt>{t('session.uptime')}</dt>
            <dd>{st?.emacs ? uptime(st.emacs.uptimeMs) : '…'}</dd>
            <dt>{t('session.agent')}</dt>
            <dd>
              {st?.emacs
                ? st.emacs.agent
                  ? t('session.connected')
                  : t('session.disconnected')
                : '…'}
            </dd>
            <dt>{t('session.responsive')}</dt>
            <dd>{st?.emacs ? (st.emacs.responsive ? t('common.yes') : t('common.no')) : '…'}</dd>
            <dt>{t('session.step')}</dt>
            <dd>{st?.step ? `${st.step.lessonId} · ${st.step.stepId}` : t('session.none')}</dd>
            {info.data?.createdAt ? (
              <>
                <dt>{t('session.created')}</dt>
                <dd>{clock(info.data.createdAt)}</dd>
              </>
            ) : null}
            {info.data?.image ? (
              <>
                <dt>{t('session.image')}</dt>
                <dd>
                  <code>{info.data.image}</code>
                </dd>
              </>
            ) : null}
          </dl>
          <div className={styles.row} style={{ marginTop: 12 }}>
            <Button onClick={() => setConfirmRestart(true)} disabled={!sessionId}>
              {t('session.restart')}
            </Button>
            {!local && (
              <Button onClick={() => setConfirmStop(true)} disabled={!sessionId}>
                {t('session.stop')}
              </Button>
            )}
          </div>
        </section>

        <section className={styles.card} aria-labelledby="s-frame">
          <h2 id="s-frame">{t('session.frame')}</h2>
          <p>{t('session.frameBody')}</p>
          <FrameSwitch />
        </section>

        <section className={styles.card} aria-labelledby="s-modules">
          <h2 id="s-modules">{t('session.modules')}</h2>
          <p>{t('session.modulesBody')}</p>
          <div className={styles.modules}>
            {MODULES.map((m) => (
              <label key={m.id}>
                <input
                  type="checkbox"
                  checked={enabled.includes(m.id)}
                  disabled={m.id === 'base' || m.id === 'agent'}
                  onChange={() => toggle(m.id)}
                />
                <code>{m.id}</code>{' '}
                <small>{t('session.part', { n: partOf.get(m.id) ?? m.part })}</small>
              </label>
            ))}
          </div>
          <Button
            variant="primary"
            onClick={() => void applyModules()}
            disabled={busy || !selected || modulesRestarting}
          >
            {modulesRestarting ? t('session.applying') : t('session.apply')}
          </Button>
        </section>

        <section className={styles.card} aria-labelledby="s-byo">
          <h2 id="s-byo">{t('session.byo')}</h2>
          <p>{t('session.byoBody')}</p>
          {local ? (
            <pre className={styles.pre}>
              {'make shell\n# or\ndocker compose exec session tmux -L pg attach -t emacs'}
            </pre>
          ) : (
            <p className={styles.dim}>{t('session.byoServer')}</p>
          )}
        </section>

        <section className={styles.card} aria-labelledby="s-export">
          <h2 id="s-export">{t('session.export')}</h2>
          {/* TODO(frontend, api.md §2.3): a download endpoint for the workspace does not exist in v1;
              on a server the operator exports with `playctl export-user` (backend.md §4). */}
          <p>{local ? t('session.exportLocal') : t('session.exportServer')}</p>
          {local && <pre className={styles.pre}>{'cp -r workspace/emacs.d ~/.config/emacs'}</pre>}
        </section>

        {st?.versions && Object.keys(st.versions).length > 0 && (
          <section className={styles.card} aria-labelledby="s-versions">
            <h2 id="s-versions">{t('session.versions')}</h2>
            <dl className={styles.dl}>
              {Object.entries(st.versions).map(([k, v]) => (
                <div key={k} style={{ display: 'contents' }}>
                  <dt>{k}</dt>
                  <dd>{v}</dd>
                </div>
              ))}
            </dl>
          </section>
        )}

        <section className={`${styles.card} ${styles.prefs}`} aria-labelledby="s-prefs">
          <h2 id="s-prefs">{t('session.prefs')}</h2>
          <label>
            {t('session.language')}
            <select
              value={prefs.locale}
              onChange={(e) => prefs.setPrefs({ locale: e.target.value })}
            >
              {LOCALES.map((l) => (
                <option key={l.id} value={l.id}>
                  {l.label}
                </option>
              ))}
            </select>
          </label>
          <label>
            <input
              type="checkbox"
              checked={prefs.screenReaderMode}
              onChange={(e) => prefs.setPrefs({ screenReaderMode: e.target.checked })}
            />
            {t('session.screenReader')}
          </label>
          <label>
            <input
              type="checkbox"
              checked={prefs.reducedMotion}
              onChange={(e) => prefs.setPrefs({ reducedMotion: e.target.checked })}
            />
            {t('session.reducedMotion')}
          </label>
          {me.data && me.data.authMode !== 'none' && (
            <div className={styles.row}>
              <span className={styles.dim}>
                {t('session.signedInAs', { name: me.data.user.displayName })}
              </span>
              <Button size="small" onClick={() => void logout()}>
                {t('session.logout')}
              </Button>
            </div>
          )}
        </section>
      </main>
      <ConfirmDialog
        open={confirmRestart}
        title={t('session.restartTitle')}
        confirmLabel={t('session.restart')}
        busy={busy}
        onClose={() => setConfirmRestart(false)}
        onConfirm={() => void restartEmacs()}
      >
        <p>{t('session.restartBody')}</p>
      </ConfirmDialog>
      <ConfirmDialog
        open={confirmStop}
        title={t('session.stopTitle')}
        confirmLabel={t('session.stop')}
        busy={busy}
        onClose={() => setConfirmStop(false)}
        onConfirm={() => void stopSession()}
      >
        <p>{t('session.stopBody')}</p>
      </ConfirmDialog>
    </AppShell>
  )
}
