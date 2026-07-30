import { VideoEmbedData } from './parseVideoPlaceholder'
import YouTubeBrowserPlayer from './YouTubeBrowserPlayer'

function buildSrc(d: VideoEmbedData): string {
  const page = d.page && d.page > 0 ? d.page : 1
  let s = `https://player.bilibili.com/player.html?bvid=${d.id}&high_quality=1&autoplay=0&page=${page}`
  if (d.start && d.start > 0) s += `&t=${d.start}`
  return s
}

export default function VideoEmbed(props: VideoEmbedData) {
  if (props.platform === 'youtube') {
    const start = typeof props.start === 'number' && Number.isFinite(props.start) && props.start > 0
      ? props.start
      : undefined
    return (
      <YouTubeBrowserPlayer
        videoId={props.id}
        start={start}
        originalURL={`https://www.youtube.com/watch?v=${props.id}${start ? `&t=${start}s` : ''}`}
      />
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
