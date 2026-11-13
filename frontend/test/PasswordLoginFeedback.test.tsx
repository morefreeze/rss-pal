import { fireEvent, render, screen } from '@testing-library/react'
import axios, { AxiosError } from 'axios'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { api } from '../src/api/client'
import LoginPage from '../src/pages/LoginPage'

const originalAdapter = api.defaults.adapter

afterEach(() => {
  api.defaults.adapter = originalAdapter
  window.history.replaceState(null, '', '/')
  localStorage.clear()
})

it('shows an invalid password inline without retrying login or clearing the current session', async () => {
  const path = '/login?intent=capture&source=https%3A%2F%2Fexample.org%2Farticle'
  window.history.replaceState(null, '', path)
  localStorage.setItem('token', 'current-session')
  localStorage.setItem('refresh_token', 'remembered-session')
  const refresh = vi.spyOn(axios, 'post').mockResolvedValue({ data: { token: 'refreshed-session' } })
  let loginAttempts = 0
  api.defaults.adapter = async config => {
    const response = { data: {}, status: 200, statusText: 'OK', headers: {}, config }
    if (config.url === '/auth/init') {
      throw new AxiosError('Already initialized', undefined, config, undefined, { ...response, status: 409 })
    }
    if (config.url === '/auth/methods') return response
    if (config.url !== '/auth/login') throw new Error('Unexpected request in password login test')
    loginAttempts += 1
    throw new AxiosError('Unauthorized', undefined, config, undefined, {
      ...response,
      status: 401,
      data: { error: '账号或密码错误' },
    })
  }
  const onLogin = vi.fn()
  render(<MemoryRouter initialEntries={[path]}><LoginPage onLogin={onLogin} /></MemoryRouter>)

  const username = screen.getByLabelText<HTMLInputElement>('用户名或邮箱')
  const password = screen.getByLabelText<HTMLInputElement>('密码')
  fireEvent.change(username, { target: { value: 'reader' } })
  fireEvent.change(password, { target: { value: 'incorrect-password' } })
  fireEvent.click(screen.getByRole('button', { name: '登录' }))

  expect((await screen.findByRole('alert')).textContent).toBe('账号或密码错误')
  expect(username.value).toBe('reader')
  expect(password.value).toBe('incorrect-password')
  expect(window.location.pathname + window.location.search).toBe(path)
  expect(refresh).not.toHaveBeenCalled()
  expect(loginAttempts).toBe(1)
  expect(localStorage.getItem('token')).toBe('current-session')
  expect(localStorage.getItem('refresh_token')).toBe('remembered-session')
  expect(onLogin).not.toHaveBeenCalled()
})
