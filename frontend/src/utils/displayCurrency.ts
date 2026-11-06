// The site's display convention: 1 USD of internal credit equals 1 CNY.
// This affects presentation only, never the currency or amount of a payment.
export const CNY_PER_USD = 1

export function usdToCny(amount: number): number {
  return amount * CNY_PER_USD
}

export function cnyToUsd(amount: number): number {
  return amount / CNY_PER_USD
}
