function pad(n: number) {
  return String(n).padStart(2, '0')
}

export function formatDateTimeLocal(d: Date) {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

export function defaultOrderedRange(): [string, string] {
  const end = new Date()
  const start = new Date()
  start.setDate(start.getDate() - 29)
  start.setHours(0, 0, 0, 0)
  end.setHours(23, 59, 59, 0)
  return [formatDateTimeLocal(start), formatDateTimeLocal(end)]
}

export const dateShortcuts = [
  {
    text: '今天',
    value: () => {
      const start = new Date()
      start.setHours(0, 0, 0, 0)
      const end = new Date()
      end.setHours(23, 59, 59, 0)
      return [start, end]
    },
  },
  {
    text: '近7天',
    value: () => {
      const end = new Date()
      end.setHours(23, 59, 59, 0)
      const start = new Date()
      start.setDate(start.getDate() - 6)
      start.setHours(0, 0, 0, 0)
      return [start, end]
    },
  },
  {
    text: '近30天',
    value: () => {
      const end = new Date()
      end.setHours(23, 59, 59, 0)
      const start = new Date()
      start.setDate(start.getDate() - 29)
      start.setHours(0, 0, 0, 0)
      return [start, end]
    },
  },
]
