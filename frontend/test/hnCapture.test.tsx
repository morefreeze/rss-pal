import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { vi, it, expect } from 'vitest'
import FeedListPage from '../src/pages/FeedListPage'
import * as api from '../src/api/client'
vi.mock('../src/api/client', () => ({
 getFeeds: vi.fn().mockResolvedValue([]), previewFeed: vi.fn(),
 createOneoffLinkSet: vi.fn().mockResolvedValue({feed_id:1,parent_article_id:42}),
 addFeed:vi.fn(),deleteFeed:vi.fn(),fetchFeedNow:vi.fn(),toggleFeedActive:vi.fn(),exportOPML:vi.fn(),capturePDFURL:vi.fn(),getMyBookmarkletToken:vi.fn(),
}))
vi.mock('../src/utils/toast',()=>({toast:{success:vi.fn(),error:vi.fn(),info:vi.fn()}}))
it('saves HN input as one clip without RSS preview or link expansion',async()=>{
 render(<MemoryRouter><FeedListPage /></MemoryRouter>)
 fireEvent.change(await screen.findByRole('textbox'),{target:{value:'https://news.ycombinator.com/item?id=49985502&utm_source=x'}})
 fireEvent.click(screen.getByRole('button',{name:'抓取 HN 帖子'}))
 await waitFor(()=>expect(api.createOneoffLinkSet).toHaveBeenCalledWith('https://news.ycombinator.com/item?id=49985502',false))
 expect(api.previewFeed).not.toHaveBeenCalled()
 expect((await screen.findByText('查看帖子')).getAttribute('href')).toBe('/articles/42')
})
