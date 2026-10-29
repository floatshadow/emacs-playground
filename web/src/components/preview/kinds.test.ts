import { describe, expect, it } from 'vitest'
import { followSource } from './kinds'

describe('followSource', () => {
  const follow = {
    'site/pages/03-usage.md': 'site/_site/usage.html',
    'site/pages/usage.md': 'site/_site/usage.html',
    'lab/src/main.rs': 'lab/README.md',
  }
  it('prefers the file being edited among several sources', () => {
    expect(followSource('site/_site/usage.html', follow, 'site/pages/usage.md', {})).toBe(
      'site/pages/usage.md',
    )
  })
  it('else the source changed last and not deleted', () => {
    const files = {
      'site/pages/usage.md': { event: 'deleted', ts: 3 },
      'site/pages/03-usage.md': { event: 'created', ts: 2 },
    }
    expect(followSource('site/_site/usage.html', follow, null, files)).toBe(
      'site/pages/03-usage.md',
    )
  })
  it('a followed file nobody builds has no source', () => {
    expect(followSource('lab/README.md', follow, 'lab/src/main.rs', {})).toBeUndefined()
  })
})
