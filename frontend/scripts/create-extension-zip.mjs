import { readdir, readFile, stat, writeFile } from 'node:fs/promises'
import path from 'node:path'

const inputDir = process.argv[2]
const outputPath = process.argv[3]

if (!inputDir || !outputPath) {
  throw new Error('usage: node scripts/create-extension-zip.mjs <input-dir> <output.zip>')
}

const crcTable = new Uint32Array(256)
for (let n = 0; n < 256; n += 1) {
  let c = n
  for (let k = 0; k < 8; k += 1) {
    c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
  }
  crcTable[n] = c >>> 0
}

function crc32(data) {
  let crc = 0xffffffff
  for (const byte of data) {
    crc = crcTable[(crc ^ byte) & 0xff] ^ (crc >>> 8)
  }
  return (crc ^ 0xffffffff) >>> 0
}

function dosDateTime(date) {
  const year = Math.max(1980, date.getFullYear())
  const time = (date.getHours() << 11) | (date.getMinutes() << 5) | Math.floor(date.getSeconds() / 2)
  const day = (year - 1980) << 9 | (date.getMonth() + 1) << 5 | date.getDate()
  return { time, day }
}

function u16(value) {
  const b = Buffer.allocUnsafe(2)
  b.writeUInt16LE(value)
  return b
}

function u32(value) {
  const b = Buffer.allocUnsafe(4)
  b.writeUInt32LE(value)
  return b
}

async function walk(dir, root, entries) {
  const items = await readdir(dir, { withFileTypes: true })
  items.sort((a, b) => a.name.localeCompare(b.name))
  for (const item of items) {
    const fullPath = path.join(dir, item.name)
    const relativePath = path.relative(root, fullPath).split(path.sep).join('/')
    if (item.isDirectory()) {
      await walk(fullPath, root, entries)
    } else if (item.isFile()) {
      const info = await stat(fullPath)
      entries.push({ fullPath, relativePath, mtime: info.mtime })
    }
  }
}

async function main() {
  const root = path.resolve(inputDir)
  const topLevel = path.basename(root).replace(/\\/g, '/')
  const entries = []
  await walk(root, root, entries)

  const chunks = []
  const centralDirectory = []
  let offset = 0

  for (const entry of entries) {
    const data = await readFile(entry.fullPath)
    const name = Buffer.from(`${topLevel}/${entry.relativePath}`, 'utf8')
    const { time, day } = dosDateTime(entry.mtime)
    const crc = crc32(data)

    const localHeader = Buffer.concat([
      u32(0x04034b50),
      u16(20),
      u16(0),
      u16(0),
      u16(time),
      u16(day),
      u32(crc),
      u32(data.length),
      u32(data.length),
      u16(name.length),
      u16(0),
      name,
    ])

    chunks.push(localHeader, data)
    centralDirectory.push(Buffer.concat([
      u32(0x02014b50),
      u16(20),
      u16(20),
      u16(0),
      u16(0),
      u16(time),
      u16(day),
      u32(crc),
      u32(data.length),
      u32(data.length),
      u16(name.length),
      u16(0),
      u16(0),
      u16(0),
      u16(0),
      u32(0),
      u32(offset),
      name,
    ]))
    offset += localHeader.length + data.length
  }

  const centralStart = offset
  const centralSize = centralDirectory.reduce((sum, chunk) => sum + chunk.length, 0)
  const endRecord = Buffer.concat([
    u32(0x06054b50),
    u16(0),
    u16(0),
    u16(entries.length),
    u16(entries.length),
    u32(centralSize),
    u32(centralStart),
    u16(0),
  ])

  await writeFile(outputPath, Buffer.concat([...chunks, ...centralDirectory, endRecord]))
}

await main()
