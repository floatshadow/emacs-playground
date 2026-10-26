import styles from './Preview.module.css'

/**
 * HTML preview in a sandboxed iframe (frontend.md §9): scripts may run, but
 * without allow-same-origin the page cannot reach the app's cookies or DOM
 * (sessiond also sends a sandboxing CSP, backend.md §3.10). Reloads in place
 * when the version changes.
 */
export function HtmlView({ url, version, name }: { url: string; version: number; name: string }) {
  const src = `${url}${url.includes('?') ? '&' : '?'}v=${version}`
  return (
    <iframe
      className={styles.frame}
      src={src}
      sandbox="allow-scripts"
      title={name}
      referrerPolicy="no-referrer"
    />
  )
}
