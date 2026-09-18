/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { cleanup, render, screen } from '@testing-library/react'
import { createInstance } from 'i18next'
import { afterEach, beforeEach, expect, it } from 'vitest'
import { I18nextProvider } from 'react-i18next'

import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { UserSubscriptionQuotaCell } from '../user-subscription-quota-cell'

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})

function renderCell(
  props: React.ComponentProps<typeof UserSubscriptionQuotaCell>
) {
  return render(
    <I18nextProvider i18n={i18n}>
      <UserSubscriptionQuotaCell {...props} />
    </I18nextProvider>
  )
}

beforeEach(() => {
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})

afterEach(() => {
  cleanup()
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})

it('renders no subscription without a quota popover', () => {
  renderCell({ hasSubscription: false })

  expect(screen.getByText('—')).toBeInTheDocument()
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})

it('renders finite subscription usage with a bounded progress bar', () => {
  renderCell({ hasSubscription: true, total: 1_000_000, used: 250_000 })

  expect(screen.getByText('0.5 / 2')).toBeInTheDocument()
  expect(screen.getByRole('progressbar')).toHaveAttribute(
    'aria-valuenow',
    '25'
  )
})

it('renders zero-total subscriptions as unlimited without progress', () => {
  renderCell({ hasSubscription: true, total: 0, used: 250_000 })

  expect(screen.getByText('0.5 / Unlimited')).toBeInTheDocument()
  expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
})
