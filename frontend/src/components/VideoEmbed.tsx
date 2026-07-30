import { VideoEmbedData } from './parseVideoPlaceholder'

function buildSrc(d: VideoEmbedData): string {
  const page = d.page && d.page > 0 ? d.page : 1
  let s = `https://player.bilibili.com/player.html?bvid=${d.id}&high_quality=1&autoplay=0&page=${page}`
  if (d.start && d.start > 0) s += `&t=${d.start}`
  return s
}

export default function VideoEmbed(props: VideoEmbedData) {
  if (props.platform === 'youtube') {
    const start = props.start && props.start > 0 ? `&t=${props.start}s` : ''
    return (
      <div
        style={{
          width: '100%',
          maxWidth: 800,
          margin: '12px 0',
          padding: 16,
          border: '1px solid var(--border)',
          borderRadius: 8,
          background: 'var(--surface)',
          color: 'var(--fg-muted)',
        }}
      >
        YouTube 视频请使用文章顶部的北京中转播放器。
        {' '}
        <a
          href={`https://www.youtube.com/watch?v=${props.id}${start}`}
          target="_blank"
          rel="noreferrer"
        >
          在 YouTube 打开
        </a>
      </div>
    )
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
