import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import RedditExploreSources from '../src/components/RedditExploreSources'
import * as api from '../src/api/client'
afterEach(()=>{vi.restoreAllMocks();localStorage.clear()})
it('shows server list and removes a subreddit without clearing other entries',async()=>{
 localStorage.setItem('user',JSON.stringify({is_admin:true}))
 vi.spyOn(api,'getRedditSubreddits').mockResolvedValue([{name:'golang',enabled:true,last_success_at:null},{name:'programming',enabled:true,last_success_at:null}])
 const remove=vi.spyOn(api,'removeRedditSubreddit').mockResolvedValue(undefined)
 render(<RedditExploreSources/>);fireEvent.click(screen.getByRole('button',{name:'管理 subreddit'}))
 fireEvent.click(await screen.findByRole('button',{name:'移除 r/golang'}))
 await waitFor(()=>expect(screen.queryByRole('link',{name:'r/golang'})).toBeNull())
 expect(remove).toHaveBeenCalledWith('golang');expect(screen.getByRole('link',{name:'r/programming'})).toBeTruthy()
})
it('keeps failed removals visible and hides management for non-admins',async()=>{
 localStorage.setItem('user',JSON.stringify({is_admin:true}))
 vi.spyOn(api,'getRedditSubreddits').mockResolvedValue([{name:'golang',enabled:true,last_success_at:null}]);vi.spyOn(api,'removeRedditSubreddit').mockRejectedValue(new Error('offline'))
 const view=render(<RedditExploreSources/>);fireEvent.click(screen.getByRole('button',{name:'管理 subreddit'}));fireEvent.click(await screen.findByRole('button',{name:'移除 r/golang'}))
 expect(await screen.findByRole('alert')).toBeTruthy();expect(screen.getByRole('link',{name:'r/golang'})).toBeTruthy()
 view.unmount();localStorage.clear();render(<RedditExploreSources/>);expect(screen.queryByRole('button',{name:'管理 subreddit'})).toBeNull()
})
