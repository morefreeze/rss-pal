import type {
  ReaderContextAction,
  ReaderContextTarget,
  ReaderLinkTarget,
} from './types'

type LinkInput = Pick<ReaderLinkTarget, 'url' | 'title'>
type ActionResult = void | Promise<void>

type LinkSetActionOptions = {
  target: ReaderContextTarget
  draftURLs: ReadonlySet<string>
  fetchedURLs: ReadonlySet<string>
  activeURLs?: ReadonlySet<string>
  onAdd: (links: LinkInput[]) => ActionResult
  onRemove: (urls: string[]) => ActionResult
  onOpen: (url: string) => ActionResult
  onCopy: (url: string) => ActionResult
  onViewTasks: () => ActionResult
}

export function createLinkSetActions({
  target,
  draftURLs,
  fetchedURLs,
  activeURLs,
  onAdd,
  onRemove,
  onOpen,
  onCopy,
  onViewTasks,
}: LinkSetActionOptions): ReaderContextAction[] {
  const startable: ReaderLinkTarget[] = []
  const drafts: ReaderLinkTarget[] = []
  const fetched: ReaderLinkTarget[] = []
  const active: ReaderLinkTarget[] = []

  for (const link of target.links) {
    if (activeURLs?.has(link.url)) active.push(link)
    else if (fetchedURLs.has(link.url)) fetched.push(link)
    else {
      startable.push(link)
      if (draftURLs.has(link.url)) drafts.push(link)
    }
  }

  const mobile = target.kind === 'long-press-link'
  const actions: ReaderContextAction[] = []
  if (startable.length > 0) {
    actions.push({
      id: 'link-set-add',
      label: mobile ? '立即开始抓取' : `立即开始抓取（${startable.length}）`,
      run: () => onAdd(startable.map(({ url, title }) => ({ url, title }))),
    })
  }
  if (drafts.length > 0) {
    actions.push({
      id: 'link-set-remove',
      label: mobile ? '移出待抓取' : `移出待抓取（${drafts.length}）`,
      run: () => onRemove(drafts.map((link) => link.url)),
    })
  }
  if (active.length > 0) {
    actions.push({
      id: 'link-set-active',
      label: mobile ? '查看抓取任务' : `查看抓取任务（${active.length}）`,
      run: onViewTasks,
    })
  }
  if (fetched.length > 0) {
    actions.push({
      id: 'link-set-fetched',
      label: mobile ? '已抓取' : `已抓取（${fetched.length}）`,
      disabled: true,
      run: () => {},
    })
  }

  if (mobile) {
    const link = target.links[0]
    actions.push(
      {
        id: 'link-open',
        label: '在新标签页打开',
        run: () => onOpen(link.url),
      },
      {
        id: 'link-copy',
        label: '复制链接',
        run: () => onCopy(link.url),
      },
    )
  }

  return actions
}
