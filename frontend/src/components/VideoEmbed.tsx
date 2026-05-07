import { VideoEmbedData } from './parseVideoPlaceholder'

function buildSrc(d: VideoEmbedData): string {
  if (d.platform === 'youtube') {
    let s = `https://www.youtube-nocookie.com/embed/${d.id}?rel=0`
    if (d.start && d.start > 0) s += `&start=${d.start}`
    return s
  }
  // bilibili
  const page = d.page && d.page > 0 ? d.page : 1
  let s = `https://player.bilibili.com/player.html?bvid=${d.id}&high_quality=1&autoplay=0&page=${page}`
  if (d.start && d.start > 0) s += `&t=${d.start}`
  return s
}

export default function VideoEmbed(props: VideoEmbedData) {
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
        loading="lazy"
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
