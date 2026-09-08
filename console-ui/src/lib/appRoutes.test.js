import { describe, expect, it } from 'vitest'
import { SYSTEM_APPS } from '../data/systemApps'
import { resolveAppLaunch } from './appRoutes'

describe('desktop declutter app routes', () => {
  it('keeps the desktop system app list at 19 total and 17 visible entries', () => {
    expect(SYSTEM_APPS).toHaveLength(19)
    expect(SYSTEM_APPS.filter(app => !['account', 'browser'].includes(app.id))).toHaveLength(17)
    expect(SYSTEM_APPS.map(app => app.id)).toContain('app-management')
    expect(SYSTEM_APPS.map(app => app.id)).not.toEqual(expect.arrayContaining([
      'downloads',
      'backup',
      'network-connections',
      'links',
      'compose-manager',
    ]))
  })

  it.each([
    ['downloads', 'files', 'downloads'],
    ['backup', 'files', 'backup'],
    ['network-connections', 'network-security', 'connections'],
    ['links', 'network-security', 'links'],
    ['compose-manager', 'docker', 'compose'],
  ])('routes legacy id %s into %s:%s', (legacyId, id, tab) => {
    expect(resolveAppLaunch({ id: legacyId, name: 'legacy' })).toMatchObject({
      id,
      tab,
      sourceId: legacyId,
      name: 'legacy',
    })
  })
})
