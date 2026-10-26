import styles from './Preview.module.css'

/** Image preview, reloaded in place when the file changes. */
export function ImageView({ url, version, name }: { url: string; version: number; name: string }) {
  const src = `${url}${url.includes('?') ? '&' : '?'}v=${version}`
  return (
    <div className={styles.imageWrap}>
      <img className={styles.image} src={src} alt={name} />
    </div>
  )
}
