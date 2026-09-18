/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, test } from 'vitest'

import type { Channel } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  buildSettingJSON,
  parseSensitiveWords,
  transformChannelToFormDefaults,
} from '../channel-form'

describe('channel sensitive-word settings', () => {
  test('parses a trimmed line-based word list', () => {
    expect(parseSensitiveWords(' blocked \n\n other\t')).toEqual([
      'blocked',
      'other',
    ])
  })

  test('omits disabled empty sensitive settings', () => {
    const settings = JSON.parse(
      buildSettingJSON({
        ...CHANNEL_FORM_DEFAULT_VALUES,
        sensitive_check_enabled: false,
        sensitive_words: ' \n\t',
      })
    ) as Record<string, unknown>

    expect(settings.sensitive_check_enabled).toBeUndefined()
    expect(settings.sensitive_words).toBeUndefined()
  })

  test('serializes enabled filtering and preserves words when disabled', () => {
    const enabled = JSON.parse(
      buildSettingJSON({
        ...CHANNEL_FORM_DEFAULT_VALUES,
        sensitive_check_enabled: true,
        sensitive_words: 'blocked\nother',
      })
    ) as Record<string, unknown>
    expect(enabled.sensitive_check_enabled).toBe(true)
    expect(enabled.sensitive_words).toEqual(['blocked', 'other'])

    const disabled = JSON.parse(
      buildSettingJSON({
        ...CHANNEL_FORM_DEFAULT_VALUES,
        sensitive_check_enabled: false,
        sensitive_words: 'blocked',
      })
    ) as Record<string, unknown>
    expect(disabled.sensitive_check_enabled).toBeUndefined()
    expect(disabled.sensitive_words).toEqual(['blocked'])
  })

  test('loads the stored array into the textarea value', () => {
    const channel = {
      name: 'test',
      type: 1,
      channel_info: { multi_key_mode: 'random' },
      setting: JSON.stringify({
        sensitive_check_enabled: true,
        sensitive_words: ['blocked', 'other'],
      }),
    } as Channel

    const values = transformChannelToFormDefaults(channel)
    expect(values.sensitive_check_enabled).toBe(true)
    expect(values.sensitive_words).toBe('blocked\nother')
  })
})
