// ADM-UI-08：配置 diff/校验、审计筛选口径。
import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { canSave, changedFields, invalidFields } from '../src/features/config/diff';
import { normalizeActionFilter } from '../src/features/audit/filters';
import type { ConfigField } from '../src/api';

const fields: ConfigField[] = [
  { key: 'session.max_minutes', kind: 'int', default: '720', min: 1, max: 1440, help: '' },
  { key: 'client.force_update', kind: 'bool', default: 'false', help: '' },
  { key: 'diag.label', kind: 'string', default: 'x', help: '' },
];
const current = { 'session.max_minutes': '720', 'client.force_update': 'false', 'diag.label': 'x' };

describe('config diff', () => {
  it('TestChangedFieldsOnlyReportsRealChanges', () => {
    expect(changedFields(fields, { ...current }, current)).toEqual([]);
    const changed = changedFields(fields, { ...current, 'session.max_minutes': '600' }, current);
    expect(changed).toEqual([{ key: 'session.max_minutes', from: '720', to: '600' }]);
  });

  it('TestInvalidFieldsFollowsContractKind', () => {
    expect(invalidFields(fields, { ...current, 'session.max_minutes': '' })).toHaveLength(1);
    expect(invalidFields(fields, { ...current, 'session.max_minutes': '0' })).toHaveLength(1);
    expect(invalidFields(fields, { ...current, 'session.max_minutes': '99999' })).toHaveLength(1);
    expect(invalidFields(fields, { ...current, 'client.force_update': 'yes' })).toHaveLength(1);
    expect(invalidFields(fields, { ...current, 'session.max_minutes': '600' })).toHaveLength(0);
  });

  it('TestCanSaveGatesOnPermissionChangesAndValidity', () => {
    const changes = [{ key: 'session.max_minutes', from: '720', to: '600' }];
    expect(canSave({ canWrite: true, busy: false, changes, invalid: [] })).toBe(true);
    expect(canSave({ canWrite: false, busy: false, changes, invalid: [] })).toBe(false);
    expect(canSave({ canWrite: true, busy: true, changes, invalid: [] })).toBe(false);
    expect(canSave({ canWrite: true, busy: false, changes: [], invalid: [] })).toBe(false);
    expect(canSave({ canWrite: true, busy: false, changes, invalid: [fields[0]] })).toBe(false);
  });
});

describe('audit filters', () => {
  it('TestAuditActionFilterIsExactMatch', () => {
    expect(normalizeActionFilter('   ')).toBeUndefined();
    expect(normalizeActionFilter(' user.ban ')).toBe('user.ban');
  });

  it('TestActionOptionsComeFromServer', () => {
    // 动作选项由 GET /admin/v1/audit/actions 提供；前端不再从当前页推导。
    const app = readFileSync(resolve(__dirname, '../src/App.tsx'), 'utf8');
    expect(app).toContain('.auditActions()');
    expect(app).toContain('actionOptions');
    expect(app).not.toContain('seenActions(rows)');
  });
});
