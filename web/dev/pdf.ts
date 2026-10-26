/**
 * A tiny PDF writer for the fake session's "typst watch": one A5 page of
 * text lines in Helvetica. Enough for PDF.js to render and for the preview
 * reload to be visible.
 */

function esc(s: string): string {
  // WinAnsi-ish: keep ASCII, replace the rest with '?'.
  return s.replace(/[\\()]/g, (c) => '\\' + c).replace(/[^\x20-\x7e]/g, '?')
}

export function makePdf(lines: { text: string; size: number }[], footer?: string): Buffer {
  const W = 420
  const H = 595
  let y = H - 64
  let content = 'BT\n'
  for (const l of lines) {
    const lead = l.size > 12 ? l.size * 1.6 : l.size * 1.55
    if (l.size > 12) y -= 6
    content += `/F${l.size > 12 ? 2 : 1} ${l.size} Tf\n1 0 0 1 48 ${y.toFixed(1)} Tm\n(${esc(l.text)}) Tj\n`
    y -= lead
    if (y < 60) break
  }
  if (footer) content += `/F1 8 Tf\n1 0 0 1 48 36 Tm\n(${esc(footer)}) Tj\n`
  content += 'ET\n'
  const objs = [
    '<< /Type /Catalog /Pages 2 0 R >>',
    '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${W} ${H}] /Resources << /Font << /F1 4 0 R /F2 5 0 R >> >> /Contents 6 0 R >>`,
    '<< /Type /Font /Subtype /Type1 /BaseFont /Times-Roman /Encoding /WinAnsiEncoding >>',
    '<< /Type /Font /Subtype /Type1 /BaseFont /Times-Bold /Encoding /WinAnsiEncoding >>',
    `<< /Length ${Buffer.byteLength(content, 'latin1')} >>\nstream\n${content}endstream`,
  ]
  let out = '%PDF-1.4\n'
  const offsets: number[] = []
  objs.forEach((o, i) => {
    offsets.push(Buffer.byteLength(out, 'latin1'))
    out += `${i + 1} 0 obj\n${o}\nendobj\n`
  })
  const xref = Buffer.byteLength(out, 'latin1')
  out += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n`
  for (const off of offsets) out += String(off).padStart(10, '0') + ' 00000 n \n'
  out += `trailer\n<< /Size ${objs.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`
  return Buffer.from(out, 'latin1')
}
