import { APIPrefix, Version } from './protocol.gen'
import styles from './app.module.css'

/**
 * Placeholder shell. TODO(frontend, frontend.md §1-§2): replace with the
 * router (routes.tsx), AppShell and the screens.
 */
export function App() {
  return (
    <div className={styles.shell}>
      <header className={styles.topbar}>
        <span className={styles.brand}>
          <i className={styles.mark} aria-hidden="true">
            e
          </i>
          Emacs Playground
        </span>
        <span className={styles.chip}>
          protocol {Version} · {APIPrefix}
        </span>
      </header>
      <main className={styles.main}>
        <h1>Emacs Playground</h1>
        <p>
          The web app is being built. Press <kbd>C-x</kbd> <kbd>C-f</kbd> once it is ready.
        </p>
      </main>
    </div>
  )
}
