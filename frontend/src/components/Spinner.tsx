export default function Spinner({ size = 14, color = 'currentColor' }: { size?: number; color?: string }) {
  return (
    <>
      <style>{`@keyframes rsspal-spin{to{transform:rotate(360deg)}}`}</style>
      <span
        role="status"
        aria-label="加载中"
        style={{
          display: 'inline-block',
          width: size,
          height: size,
          border: `${Math.max(2, Math.round(size / 8))}px solid ${color}`,
          borderTopColor: 'transparent',
          borderRadius: '50%',
          animation: 'rsspal-spin 0.8s linear infinite',
          verticalAlign: 'middle',
          boxSizing: 'border-box',
        }}
      />
    </>
  )
}
