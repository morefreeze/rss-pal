import { fireEvent, render, screen, cleanup } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SourceDirectory from '../src/components/SourceDirectory'

afterEach(cleanup)
const items = [
  { id: 1, title: 'The GitHub Blog', url: 'https://github.blog/feed/', category: '编程', description: '工程博客', sort_order: 1 },
  { id: 2, title: 'OpenAI News', url: 'https://openai.com/news/rss.xml', category: 'AI', description: '官方消息', sort_order: 2 },
]


describe('source directory', () => {
  it('opens a category, previews a concrete feed, and returns to the directory', () => {
    const onPreview = vi.fn()
    render(<SourceDirectory items={items} onPreview={onPreview} />)
    fireEvent.click(screen.getByRole('button', { name: /编程.*个源/ }))
    expect(screen.getByRole('heading', { name: '编程' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '预览 The GitHub Blog' }))
    expect(onPreview).toHaveBeenCalledWith('https://github.blog/feed/')
    fireEvent.click(screen.getByRole('button', { name: '返回分类目录' }))
    expect(screen.getByRole('button', { name: /金融.*个源/ })).toBeTruthy()
  })

  it('searches all categories and deduplicates sources also featured in popular', () => {
    render(<SourceDirectory items={items} onPreview={vi.fn()} />)
    fireEvent.change(screen.getByRole('searchbox', { name: '搜索推荐源' }), { target: { value: 'OpenAI' } })
    expect(screen.getAllByRole('button', { name: '预览 OpenAI News' })).toHaveLength(1)
    fireEvent.change(screen.getByRole('searchbox', { name: '搜索推荐源' }), { target: { value: '无匹配源123' } })
    expect(screen.getByText('没有找到推荐源，试试其他关键词。')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '清除搜索' }))
    expect(screen.getByRole('button', { name: /热门.*个源/ })).toBeTruthy()
  })

  it('exposes every Folo category and retains AI, podcasts and health', () => {
    render(<SourceDirectory items={items} onPreview={vi.fn()} />)
    for (const name of ['社交媒体', '新媒体', '传统媒体', '论坛', '博客', '编程', '设计', '直播', '音视频', '图片', '二次元', '程序更新', '购物', '游戏', '阅读', '科学期刊', '金融', 'AI', '播客', '健康']) {
      expect(screen.getByRole('button', { name: new RegExp(`^${name}.*个源$`) })).toBeTruthy()
    }
  })
})

it('retains legacy category aliases, custom admin categories and empty categories', () => {
  const onPreview = vi.fn()
  render(<SourceDirectory items={[
    ...items,
    { id: 3, title: '旧科技源', url: 'https://example.com/tech', category: '科技', description: '', sort_order: 3 },
    { id: 4, title: '自定义源', url: 'https://example.com/custom', category: '城市生活', description: '', sort_order: 4 },
  ]} onPreview={onPreview} />)
  fireEvent.click(screen.getByRole('button', { name: /^新媒体.*1 个源$/ }))
  expect(screen.getByRole('button', { name: '预览 旧科技源' })).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '返回分类目录' }))
  fireEvent.click(screen.getByRole('button', { name: /^城市生活.*1 个源$/ }))
  expect(screen.getByRole('button', { name: '预览 自定义源' })).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '返回分类目录' }))
  fireEvent.click(screen.getByRole('button', { name: /^直播.*0 个源$/ }))
  expect(screen.getByText('这个分类暂无已上架的推荐源。')).toBeTruthy()
})
