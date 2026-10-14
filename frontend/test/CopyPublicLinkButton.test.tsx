import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import CopyPublicLinkButton from '../src/components/CopyPublicLinkButton'
const api = vi.hoisted(() => ({listArticleShares: vi.fn(), createArticleShare: vi.fn()}))
vi.mock('../src/api/client', () => api)
const url = 'https://r.morefreeze.top/Aa0000000000'
const row = {id:'a',url,status:'active',legacy:false,expires_at:null,created_at:'2026-10-14T00:00:00Z'}
beforeEach(() => {
  api.listArticleShares.mockReset().mockResolvedValue([row])
  api.createArticleShare.mockReset().mockResolvedValue(row)
  Object.defineProperty(navigator, 'clipboard', {configurable:true,value:{writeText:vi.fn().mockResolvedValue(undefined)}})
})
it('reuses an active public link and copies only its URL without a dialog', async () => {
  render(<CopyPublicLinkButton articleId={42} />)
  fireEvent.click(screen.getByRole('button', {name:'复制链接'}))
  await screen.findByRole('status')
  expect(navigator.clipboard.writeText).toHaveBeenCalledWith(url)
  expect(api.listArticleShares).toHaveBeenCalledWith(42)
  expect(api.createArticleShare).not.toHaveBeenCalled()
  expect(screen.queryByRole('dialog')).toBeNull()
})
it('creates a permanent public link only when no reusable link exists', async () => {
  api.listArticleShares.mockResolvedValue([{...row,legacy:true},{...row,status:'revoked'},{...row,status:'expired'}])
  render(<CopyPublicLinkButton articleId={42} />)
  fireEvent.click(screen.getByRole('button', {name:'复制链接'}))
  await screen.findByRole('status')
  expect(api.createArticleShare).toHaveBeenCalledWith(42,null)
  expect(navigator.clipboard.writeText).toHaveBeenCalledWith(url)
})
it('does not create a public link after list failure', async () => {
  api.listArticleShares.mockRejectedValue(new Error('offline'))
  render(<CopyPublicLinkButton articleId={42} />)
  fireEvent.click(screen.getByRole('button', {name:'复制链接'}))
  await screen.findByRole('alert')
  expect(api.createArticleShare).not.toHaveBeenCalled()
})
it('guards repeated clicks and offers the URL inline after clipboard denial', async () => {
  let resolve!: (value: typeof row[]) => void
  api.listArticleShares.mockReturnValue(new Promise(r=>{resolve=r}))
  vi.mocked(navigator.clipboard.writeText).mockRejectedValue(new Error('denied'))
  render(<CopyPublicLinkButton articleId={42} />)
  const button=screen.getByRole('button',{name:'复制链接'})
  fireEvent.click(button); fireEvent.click(button)
  expect(api.listArticleShares).toHaveBeenCalledTimes(1)
  await act(async()=>resolve([row]))
  const input=await screen.findByRole('textbox',{name:'公开分享链接'}) as HTMLInputElement
  expect(input.value).toBe(url)
  expect(document.activeElement).toBe(input)
  expect(screen.queryByRole('dialog')).toBeNull()
})
it('does not create a link when navigating away before list completes', async () => {
  let resolve!: (value: typeof row[]) => void
  api.listArticleShares.mockReturnValue(new Promise(r=>{resolve=r}))
  const view=render(<CopyPublicLinkButton articleId={42} />)
  fireEvent.click(screen.getByRole('button',{name:'复制链接'}))
  view.unmount()
  await act(async()=>resolve([]))
  expect(api.createArticleShare).not.toHaveBeenCalled()
  expect(navigator.clipboard.writeText).not.toHaveBeenCalled()
})
it('starts promise-backed clipboard writing during the click for Safari', async () => {
  const item = vi.fn(function(this: {data: Record<string,Promise<Blob>>}, data: Record<string,Promise<Blob>>) {this.data=data})
  vi.stubGlobal('ClipboardItem',item)
  const write=vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator,'clipboard',{configurable:true,value:{write}})
  render(<CopyPublicLinkButton articleId={42} />)
  fireEvent.click(screen.getByRole('button',{name:'复制链接'}))
  expect(write).toHaveBeenCalledTimes(1)
  const blob = await item.mock.calls[0][0]['text/plain']
  expect(blob.type).toBe('text/plain')
  const actual = await new Promise(resolve => { const reader = new FileReader(); reader.onload = () => resolve(reader.result); reader.readAsText(blob) })
  expect(actual).toBe(url)
  await waitFor(()=>expect(screen.getByRole('status')).toBeTruthy())
  vi.unstubAllGlobals()
})

it('reports a body-not-ready error without copying a private article URL', async () => {
  api.listArticleShares.mockResolvedValue([])
  api.createArticleShare.mockRejectedValue({response:{status:409}})
  render(<CopyPublicLinkButton articleId={42} />)
  fireEvent.click(screen.getByRole('button',{name:'复制链接'}))
  expect((await screen.findByRole('alert')).textContent).toContain('文章正文尚未准备完成')
  expect(navigator.clipboard.writeText).not.toHaveBeenCalled()
})
