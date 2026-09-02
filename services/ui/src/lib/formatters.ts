export function formatCentsToDollars(cents: number): string {
  return (cents / 100).toLocaleString('en-US', {
    style: 'currency',
    currency: 'USD',
  });
}

export function formatBpsToPercent(bps: number): string {
  return `${(bps / 100).toFixed(2)}%`;
}
