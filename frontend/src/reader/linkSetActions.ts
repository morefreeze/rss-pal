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
  onAdd: (links: LinkInput[]) => ActionResult
  onRemove: (urls: string[]) => ActionResult
  onOpen: (url: string) => ActionResult
  onCopy: (url: string) => ActionResult
}

export function createLinkSetActions({
  target,
  draftURLs,
  fetchedURLs,
  onAdd,
  onRemove,
  onOpen,
  onCopy,
}: LinkSetActionOptions): ReaderContextAction[] {
  const unmarked: ReaderLinkTarget[] = []
  const drafts: ReaderLinkTarget[] = []
  const fetched: ReaderLinkTarget[] = []

  for (const link of target.links) {
    if (fetchedURLs.has(link.url)) fetched.push(link)
    else if (draftURLs.has(link.url)) drafts.push(link)
    else unmarked.push(link)
  }

  const mobile = target.kind === 'long-press-link'
  const actions: ReaderContextAction[] = []
  if (unmarked.length > 0) {
    actions.push({
      id: 'link-set-add',
      label: mobile ? '加入待抓取' : `加入待抓取（${unmarked.length}）`,
      run: () => onAdd(unmarked.map(({ url, title }) => ({ url, title }))),
    })
  }
  if (drafts.length > 0) {
    actions.push({
      id: 'link-set-remove',
      label: mobile ? '移出待抓取' : `移出待抓取（${drafts.length}）`,
      run: () => onRemove(drafts.map((link) => link.url)),
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
