// Shared, decorative line icons. Interactive parents provide their accessible name.
const paths: Record<string, string> = {
  articles: 'M5 3h14v18H5z M8 7h8 M8 11h8 M8 15h5',
  bookmark: 'M6 3h12v18l-6-4-6 4z',
  explore: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0 M16 8l-3 5-5 3 3-5z',
  calendar: 'M5 5h14v16H5z M8 3v4 M16 3v4 M5 10h14 M8 14h3 M8 17h6',
  rss: 'M5 11a8 8 0 0 1 8 8 M5 5a14 14 0 0 1 14 14 M5 18h1v1H5z',
  spark: 'm12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5z',
  chart: 'M4 4v16h16 M8 16v-4 M12 16V7 M16 16v-6',
  history: 'M3 11a9 9 0 1 1 2.6 7.4 M3 5v6h6 M12 7v5l3 2',
  settings: 'M4 7h16 M4 17h16 M9 4v6 M15 14v6',
  logout: 'M10 4H4v16h6 M9 12h12 M17 8l4 4-4 4',
  shield: 'm12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6z M9 12l2 2 4-4',
  user: 'M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0 M4 21v-2a8 8 0 0 1 16 0v2',
  more: 'M4 11h2v2H4z M11 11h2v2h-2z M18 11h2v2h-2z',
  close: 'm6 6 12 12 M6 18 18 6',
  menu: 'M4 6h16 M4 12h16 M4 18h16',
  book: 'M12 6C9 3 5 3 2 4v15c3-1 7-1 10 2 3-3 7-3 10-2V4c-3-1-7-1-10 2v15',
}
const aliases: Record<string, string> = { '📰': 'articles', '⭐': 'bookmark', '🔭': 'explore', '📅': 'calendar', '📡': 'rss', '💡': 'spark', '📊': 'chart', '🕘': 'history', '⚙️': 'settings', '🚪': 'logout', '🛡️': 'shield', '👤': 'user' }
export default function UiIcon({ name, size = 18 }: { name: string; size?: number }) {
  return <svg className="ui-icon" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false"><path d={paths[aliases[name] || name] || paths.articles} /></svg>
}
