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
import { useTranslation } from 'react-i18next'

import { QuotaDetailsPopover } from '@/components/quota-details-popover'
import { Progress } from '@/components/ui/progress'
import { formatQuota } from '@/lib/format'

export type UserSubscriptionQuotaCellProps = {
  hasSubscription?: boolean
  total?: number
  used?: number
}

export function UserSubscriptionQuotaCell(
  props: UserSubscriptionQuotaCellProps
) {
  const { t } = useTranslation()
  if (!props.hasSubscription) {
    return <span className='text-muted-foreground text-sm'>—</span>
  }

  const total = Math.max(0, Number(props.total ?? 0))
  const used = Math.max(0, Number(props.used ?? 0))
  const unlimited = total === 0
  const usedDisplay = formatQuota(used)
  const totalDisplay = unlimited ? t('Unlimited') : formatQuota(total)
  const percentage = unlimited
    ? null
    : Math.min(100, Math.max(0, (used / total) * 100))
  const triggerLabel = `${t('Used amount')} ${usedDisplay} / ${totalDisplay}`

  return (
    <QuotaDetailsPopover
      title={`${t('Subscription')} ${t('Total Quota')}`}
      triggerLabel={triggerLabel}
      details={[
        { label: t('Used amount'), value: usedDisplay },
        { label: t('Total Quota'), value: totalDisplay },
      ]}
    >
      <div className='flex min-w-[140px] flex-col gap-1 text-sm tabular-nums'>
        <span>
          {usedDisplay} / {totalDisplay}
        </span>
        {percentage !== null && (
          <Progress
            value={percentage}
            aria-label={t('Usage percentage')}
            className='h-1.5'
          />
        )}
      </div>
    </QuotaDetailsPopover>
  )
}
