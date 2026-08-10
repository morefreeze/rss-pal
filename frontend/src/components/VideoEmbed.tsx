import { VideoEmbedData } from './parseVideoPlaceholder'
import { isPakeWebView } from '../utils/runtimeEnvironment'

function positiveFinite(value: number | undefined): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
}

function buildSrc(d: VideoEmbedData): string {
  if (d.platform === 'youtube') {
    let s = `https://www.youtube-nocookie.com/embed/${d.id}?rel=0`
    if (positiveFinite(d.start)) s += `&start=${d.start}`
    return s
  }

  const page = d.page && d.page > 0 ? d.page : 1
  let s = `https://player.bilibili.com/player.html?bvid=${d.id}&high_quality=1&autoplay=0&page=${page}`
  if (positiveFinite(d.start)) s += `&t=${d.start}`
  return s
}

function buildBilibiliURL(d: VideoEmbedData): string {
  const params = new URLSearchParams()
  if (d.page && d.page > 1) params.set('p', String(d.page))
  if (positiveFinite(d.start)) params.set('t', String(d.start))
  const query = params.toString()
  return `https://www.bilibili.com/video/${d.id}${query ? `?${query}` : ''}`
}

function BilibiliExternalLink(props: VideoEmbedData) {
  const href = buildBilibiliURL(props)
  return (
    <div
      style={{
        width: '100%',
        maxWidth: 800,
        aspectRatio: '16 / 9',
        margin: '12px 0',
        background: '#111827',
        color: '#fff',
        borderRadius: 8,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 12,
      }}
    >
      <div style={{ fontSize: 14, color: '#d1d5db' }}>B 站视频</div>
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          minHeight: 40,
          padding: '0 16px',
          borderRadius: 6,
          background: '#00aeec',
          color: '#fff',
          fontWeight: 600,
          textDecoration: 'none',
        }}
      >
        在 B 站打开
      </a>
    </div>
  )
}

export default function VideoEmbed(props: VideoEmbedData) {
  if (props.platform === 'bilibili' && isPakeWebView()) {
    return <BilibiliExternalLink {...props} />
  }

  const src = buildSrc(props)
  return (
    <div
      style={{
        position: 'relative',
        width: '100%',
        maxWidth: 800,
        aspectRatio: '16 / 9',
        margin: '12px 0',
        background: '#000',
        borderRadius: 8,
        overflow: 'hidden',
      }}
    >
      <iframe
        src={src}
        title={`${props.platform} video ${props.id}`}
        allow="encrypted-media; picture-in-picture"
        allowFullScreen
        referrerPolicy="no-referrer"
        style={{
          position: 'absolute',
          inset: 0,
          width: '100%',
          height: '100%',
          border: 0,
        }}
      />
    </div>
  )
}
