// Per-article persistent selection set for the link_set inline marking flow.
// Extracted from the legacy BatchFetchModal so both the article page (writer)
// and the confirm dialog (reader, indirectly via the page) share one path.
//
// Why localStorage with TTL instead of server-side: selection is a per-device
// scratchpad — the user can mark candidates over several reading sessions
// then submit a batch. Cross-device sync isn't worth a new API surface for
// this. 1-day TTL keeps abandoned selections from accumulating forever.

import { normalizeHTTPURL } from './url'

const SELECTION_TTL_MS = 24 * 60 * 60 * 1000
const DRAFT_VERSION = 2

export type DraftLink = {
  url: string
  title: string
  addedAt: number
}

const selectionKey = (articleId: number) => `rsspal_batch_sel_${articleId}`

export function loadSavedURLs(articleId: number): string[] {
	return loadDraftLinks(articleId).map((link) => link.url)
}

function fallbackTitle(url: string): string {
	try {
		return new URL(url).hostname || url
	} catch {
		return url
	}
}

function normalizeDrafts(rawLinks: unknown, defaultAddedAt: number): DraftLink[] {
	if (!Array.isArray(rawLinks)) return []
	const seen = new Set<string>()
	const drafts: DraftLink[] = []
	for (const raw of rawLinks) {
		const source = typeof raw === 'string' ? { url: raw } : raw
		if (!source || typeof source !== 'object') continue
		const value = source as { url?: unknown; title?: unknown; addedAt?: unknown }
		if (typeof value.url !== 'string') continue
		const url = normalizeHTTPURL(value.url)
		if (!url || seen.has(url)) continue
		seen.add(url)
		const title = typeof value.title === 'string' && value.title.trim()
			? value.title.trim()
			: fallbackTitle(url)
		const addedAt = typeof value.addedAt === 'number' && Number.isFinite(value.addedAt)
			? value.addedAt
			: defaultAddedAt
		drafts.push({ url, title, addedAt })
	}
	return drafts
}

export function loadDraftLinks(
	articleId: number,
	storage: Storage = localStorage,
	now: number = Date.now(),
): DraftLink[] {
  try {
		const raw = storage.getItem(selectionKey(articleId))
    if (!raw) return []
		const parsed = JSON.parse(raw) as { version?: unknown; links?: unknown; urls?: unknown; savedAt?: unknown }
    if (typeof parsed?.savedAt !== 'number') return []
		if (now - parsed.savedAt > SELECTION_TTL_MS) {
			storage.removeItem(selectionKey(articleId))
      return []
    }
		const rawLinks = parsed.version === DRAFT_VERSION ? parsed.links : parsed.urls
		return normalizeDrafts(rawLinks, parsed.savedAt)
  } catch {
    return []
  }
}

export function saveSelectedURLs(articleId: number, urls: string[]): void {
	const now = Date.now()
	saveDraftLinks(articleId, urls.map((url) => ({
		url,
		title: fallbackTitle(url),
		addedAt: now,
	})), localStorage, now)
}

export function saveDraftLinks(
	articleId: number,
	links: DraftLink[],
	storage: Storage = localStorage,
	now: number = Date.now(),
): void {
  try {
		const normalized = normalizeDrafts(links, now)
		if (normalized.length === 0) {
			storage.removeItem(selectionKey(articleId))
      return
    }
		storage.setItem(
      selectionKey(articleId),
		JSON.stringify({ version: DRAFT_VERSION, links: normalized, savedAt: now }),
    )
  } catch {
    /* quota or disabled — ignore */
  }
}

export function enrichDraftLinkTitle(link: DraftLink, discoveredTitle: string): DraftLink {
	const title = discoveredTitle.trim().replace(/\s+/g, ' ')
	if (!title || (link.title !== link.url && link.title !== fallbackTitle(link.url))) {
		return link
	}
	return title === link.title ? link : { ...link, title }
}
