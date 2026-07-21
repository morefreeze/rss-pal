import { createContext } from 'react'
import type { ReaderActionContextValue } from './types'

export const ReaderActionContext = createContext<ReaderActionContextValue | null>(null)
