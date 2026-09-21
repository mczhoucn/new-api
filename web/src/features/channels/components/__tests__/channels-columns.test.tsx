import { describe, expect, it } from 'vitest'

import { getConcurrencyVariant } from '../channels-columns'

describe('channel concurrency colors', () => {
  it.each([
    [0, 10, 'neutral'],
    [1, 10, 'success'],
    [8, 10, 'warning'],
    [10, 10, 'danger'],
    [11, 10, 'danger'],
  ] as const)(
    'uses the expected color for %s active connections out of %s',
    (current, limit, expected) => {
      expect(getConcurrencyVariant(current, limit)).toBe(expected)
    }
  )
})
