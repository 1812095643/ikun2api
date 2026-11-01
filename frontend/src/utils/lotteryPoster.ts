import { lotteryMoney, lotteryTime, prizeNames, type LotteryWin } from '@/api/lottery'

function lines(ctx: CanvasRenderingContext2D, value: string, width: number) {
  const result: string[] = []
  let line = ''
  for (const letter of value) {
    if (ctx.measureText(line + letter).width > width && line) {
      result.push(line)
      line = letter
    } else line += letter
  }
  if (line) result.push(line)
  return result
}

export async function createLotteryPoster(win: LotteryWin, siteName = 'IKUN2api'): Promise<Blob> {
  await document.fonts.ready
  const canvas = document.createElement('canvas')
  canvas.width = 1080
  canvas.height = 1580
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('当前浏览器无法生成海报，请换一个浏览器试试。')
  ctx.font = '30px sans-serif'
  const emailLines = lines(ctx, win.user_email || '未设置邮箱', 820)
  const nameLines = lines(ctx, win.user_name || '幸运用户', 820)
  canvas.height = Math.max(1580, 1370 + (emailLines.length + nameLines.length) * 38)
  const height = canvas.height
  ctx.fillStyle = '#e8e3d9'
  ctx.fillRect(0, 0, 1080, height)
  ctx.fillStyle = '#f8f5ee'
  ctx.beginPath()
  ctx.roundRect(52, 52, 976, height - 104, 28)
  ctx.fill()
  ctx.fillStyle = '#292922'
  ctx.fillRect(52, 52, 976, 108)
  ctx.font = '600 27px sans-serif'
  ctx.fillStyle = '#f8f5ee'
  ctx.fillText(siteName, 104, 119, 590)
  ctx.textAlign = 'right'
  ctx.font = '22px monospace'
  ctx.fillText(`LUCKY NO. ${String(win.id).padStart(6, '0')}`, 976, 117)
  ctx.textAlign = 'left'
  ctx.fillStyle = '#a96e2c'
  ctx.font = '42px Georgia, serif'
  ctx.fillText('A little luck, a lovely day.', 104, 248)
  ctx.fillStyle = '#292922'
  ctx.font = '600 74px "Noto Serif SC", "Songti SC", serif'
  ctx.fillText('这次，好运是我。', 104, 360)
  ctx.save()
  ctx.translate(925, 370)
  ctx.rotate(-0.15)
  ctx.strokeStyle = '#a96e2c'
  ctx.lineWidth = 3
  ctx.beginPath()
  ctx.arc(0, 0, 58, 0, Math.PI * 2)
  ctx.stroke()
  ctx.textAlign = 'center'
  ctx.font = '48px Georgia, serif'
  ctx.fillStyle = '#a96e2c'
  ctx.fillText('✳', 0, 18)
  ctx.restore()
  ctx.fillStyle = '#7c7569'
  ctx.font = '27px sans-serif'
  ctx.fillText(`${prizeNames[win.prize_key]} · 本次中奖`, 104, 466)
  ctx.fillStyle = '#292922'
  const amount = `$${lotteryMoney(win.amount)}`
  let size = 174
  ctx.font = `${size}px Georgia, serif`
  while (ctx.measureText(amount).width > 840 && size > 60) {
    size -= 4
    ctx.font = `${size}px Georgia, serif`
  }
  ctx.fillText(amount, 98, 653)
  ctx.fillStyle = '#7c7569'
  ctx.font = '26px sans-serif'
  ctx.fillText(win.claimed_at ? '奖金已存入本站余额' : '本站余额奖励 · 待领取', 108, 718)
  ctx.strokeStyle = '#c7bfae'
  ctx.setLineDash([8, 10])
  ctx.lineWidth = 2
  ctx.beginPath()
  ctx.moveTo(52, 790)
  ctx.lineTo(1028, 790)
  ctx.stroke()
  ctx.setLineDash([])
  ctx.fillStyle = '#e8e3d9'
  for (const x of [52, 1028]) {
    ctx.beginPath()
    ctx.arc(x, 790, 20, 0, Math.PI * 2)
    ctx.fill()
  }
  let y = 872
  ctx.fillStyle = '#7c7569'
  ctx.font = '24px sans-serif'
  ctx.fillText('获奖用户', 104, y)
  y += 55
  ctx.fillStyle = '#292922'
  ctx.font = '600 34px sans-serif'
  for (const line of nameLines) {
    ctx.fillText(line, 104, y)
    y += 42
  }
  ctx.fillStyle = '#7c7569'
  ctx.font = '30px sans-serif'
  y += 5
  for (const line of emailLines) {
    ctx.fillText(line, 104, y)
    y += 38
  }
  y += 42
  ctx.font = '24px sans-serif'
  ctx.fillText('来自这场好运', 104, y)
  y += 46
  ctx.fillStyle = '#292922'
  ctx.font = '30px sans-serif'
  for (const line of lines(ctx, win.title, 820).slice(0, 2)) {
    ctx.fillText(line, 104, y)
    y += 40
  }
  const base = height - 290
  ctx.fillStyle = '#eae3d5'
  ctx.beginPath()
  ctx.roundRect(104, base, 872, 135, 12)
  ctx.fill()
  ctx.fillStyle = '#7c7569'
  ctx.font = '26px sans-serif'
  ctx.fillText('我的累计中奖', 136, base + 48)
  ctx.fillStyle = '#292922'
  ctx.font = '48px Georgia, serif'
  ctx.fillText(`${win.win_count || 1}`, 136, base + 105)
  ctx.font = '24px sans-serif'
  ctx.fillText('次', 210 + String(win.win_count || 1).length * 20, base + 104)
  ctx.textAlign = 'right'
  ctx.font = '25px sans-serif'
  ctx.fillStyle = '#7c7569'
  ctx.fillText(lotteryTime(win.created_at), 944, base + 85)
  ctx.textAlign = 'left'
  ctx.font = '21px sans-serif'
  ctx.fillStyle = '#8c8477'
  ctx.fillText('好运有记录，快乐值得分享。', 104, height - 93)
  ctx.textAlign = 'right'
  ctx.font = '21px monospace'
  ctx.fillText('IKUN2api / LUCKY CLUB', 976, height - 93)
  return new Promise((resolve, reject) =>
    canvas.toBlob(
      (blob) => (blob ? resolve(blob) : reject(new Error('海报生成未完成，请重试。'))),
      'image/png'
    )
  )
}
