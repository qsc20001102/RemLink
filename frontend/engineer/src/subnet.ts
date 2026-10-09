function parseIPv4(raw: string, label: string): number[] {
  const parts = raw.trim().split('.')
  if (parts.length !== 4 || parts.some(part => !/^(0|[1-9]\d{0,2})$/.test(part) || Number(part) > 255)) {
    throw new Error(`${label}格式不正确，请填写四段 0–255 的数字，例如 192.168.13.10。`)
  }
  return parts.map(Number)
}

export function calculateSubnet(ip: string, mask: string): string {
  const address = parseIPv4(ip, 'IPv4 地址')
  const octets = parseIPv4(mask, '子网掩码')
  const bits = octets.map(octet => octet.toString(2).padStart(8, '0')).join('')
  if (!/^1*0*$/.test(bits)) {
    throw new Error('子网掩码必须连续，例如 255.255.255.0。')
  }
  const prefix = bits.indexOf('0') === -1 ? 32 : bits.indexOf('0')
  if (prefix === 0) throw new Error('不支持 0.0.0.0 掩码，请填写具体设备网段的子网掩码。')
  const network = address.map((octet, index) => octet & octets[index]!).join('.')
  return `${network}/${prefix}`
}
