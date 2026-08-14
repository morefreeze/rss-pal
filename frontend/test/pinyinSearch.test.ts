import { describe, expect, it } from 'vitest'
import { pinyinSearchIncludes } from '../src/utils/pinyinSearch'

describe('pinyinSearchIncludes', () => {
  it('matches Chinese text by full pinyin', () => {
    expect(pinyinSearchIncludes('科技周刊', 'keji')).toBe(true)
  })

  it('matches Chinese text by initials', () => {
    expect(pinyinSearchIncludes('阮一峰的网络日志', 'ryf')).toBe(true)
  })
})
