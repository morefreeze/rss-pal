const COLORS = ['#a855f7', '#ec4899', '#f59e0b', '#10b981', '#3b82f6', '#ef4444']
const PARTICLE_COUNT = 12

export default function ConfettiBurst() {
  return (
    <div className="confetti-burst" aria-hidden="true">
      {Array.from({ length: PARTICLE_COUNT }).map((_, i) => {
        const angle = (Math.PI * 2 * i) / PARTICLE_COUNT + (Math.random() - 0.5) * 0.4
        const distance = 26 + Math.random() * 22
        const cx = Math.cos(angle) * distance
        const cy = Math.sin(angle) * distance + 6 // slight downward bias
        const cr = (Math.random() - 0.5) * 720
        const delay = Math.random() * 80
        const color = COLORS[i % COLORS.length]
        return (
          <span
            key={i}
            style={{
              background: color,
              animationDelay: `${delay}ms`,
              ['--cx' as any]: `${cx}px`,
              ['--cy' as any]: `${cy}px`,
              ['--cr' as any]: `${cr}deg`,
            }}
          />
        )
      })}
    </div>
  )
}
