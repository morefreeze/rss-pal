import { describe,it,expect } from 'vitest'
import { authSearch,parseAuthIntent,postAuthURL } from '../src/utils/authIntent'
describe('share registration invitation',()=>{
 it('preserves the invitation between auth pages but drops it after login',()=>{
  const intent=parseAuthIntent('?intent=subscribe&source=https%3A%2F%2Fexample.com%2Frss&share=s%3Ao6XKeCZTDZ22')
  expect(authSearch(intent)).toBe('?intent=subscribe&source=https%3A%2F%2Fexample.com%2Frss&share=s%3Ao6XKeCZTDZ22')
  expect(postAuthURL(intent)).toBe('/feeds?add=1&source=https%3A%2F%2Fexample.com%2Frss')
 })
 it('supports use intent without a source and ignores malformed or duplicate refs',()=>{
  expect(authSearch(parseAuthIntent('?intent=use&share=s%3Ao6XKeCZTDZ22'))).toBe('?intent=use&share=s%3Ao6XKeCZTDZ22')
  for(const query of ['?share=https://evil.example','?share=s%3Ao6XKeCZTDZ22&share=s%3Ao6XKeCZTDZ22','?share=%zz']) expect(authSearch(parseAuthIntent(query))).toBe('?intent=use')
 })
})
