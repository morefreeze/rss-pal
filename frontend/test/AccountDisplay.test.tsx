import {render,screen,fireEvent} from '@testing-library/react'
import {expect,it} from 'vitest'
import AccountDisplay from '../src/components/AccountDisplay'
it('shows QQ nickname and avatar without replacing a custom nickname',()=>{
 render(<AccountDisplay user={{username:'reader',nickname:'自己选的名字',qq_nickname:'QQ昵称',qq_avatar_url:'https://q.qlogo.cn/qqapp/1/id/40'}} />)
 expect(screen.getByText('自己选的名字')).toBeTruthy()
 expect(screen.getByText('（QQ：QQ昵称）')).toBeTruthy()
 expect(screen.getByRole('img',{name:'QQ 头像'}).getAttribute('referrerpolicy')).toBe('no-referrer')
})
it('uses QQ profile for the initial display and handles unavailable avatars',()=>{
 const {rerender}=render(<AccountDisplay user={{username:'reader_random',qq_nickname:'QQ读者',qq_avatar_url:'https://q.qlogo.cn/qqapp/1/id/40'}} />)
 expect(screen.getByText('QQ读者')).toBeTruthy()
 fireEvent.error(screen.getByRole('img'))
 expect(screen.queryByRole('img')).toBeNull()
 rerender(<AccountDisplay user={{username:'admin'}} />)
 expect(screen.getByText('admin')).toBeTruthy()
})
it('does not render an unsafe avatar destination',()=>{
 render(<AccountDisplay user={{username:'reader',qq_avatar_url:'https://attacker.example/photo'}} />)
 expect(screen.queryByRole('img')).toBeNull()
})
