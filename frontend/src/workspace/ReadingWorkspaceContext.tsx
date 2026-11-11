import { createContext, useContext, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import type { WorkspaceModule } from './workspaceRoutes'

export interface ReadingWorkspaceValue {
  activeModule: WorkspaceModule
  isActive: boolean
  /** False for inactive modules, phone readers, and reader focus mode. */
  isListVisible: boolean
  isReaderOpen: boolean
  selectedArticleId: number | null
  isPhone: boolean
  isCompact: boolean
  listPath: string
  returnPath: string
  getListScrollElement: () => HTMLElement | null
  closeReader: () => void
  openScopes: () => void
  sidebarTarget: HTMLElement | null
  overviewTarget: HTMLElement | null
}
export const ReadingWorkspaceContext = createContext<ReadingWorkspaceValue | null>(null)
export function useReadingWorkspace() { return useContext(ReadingWorkspaceContext) }

export function WorkspaceSidebar({ children }: { children: ReactNode }) {
  const workspace = useReadingWorkspace()
  if (!workspace) return <>{children}</>
  if (!workspace.isActive || !workspace.sidebarTarget) return null
  return createPortal(children, workspace.sidebarTarget)
}

export function WorkspaceOverview({ children }: { children: ReactNode }) {
  const workspace = useReadingWorkspace()
  if (!workspace) return <>{children}</>
  if (!workspace.isActive) return null
  if (workspace.isPhone) return <div className="workspace-inline-overview">{children}</div>
  if (workspace.isReaderOpen || !workspace.overviewTarget) return null
  return createPortal(children, workspace.overviewTarget)
}
