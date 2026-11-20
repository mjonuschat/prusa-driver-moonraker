import assert from 'node:assert/strict';
import test from 'node:test';

import { endpointWrite, rolesChanged, rolesOf } from '../src/endpoint.ts';

const form = {
  name: 'voron',
  address: 'printer.local',
  unitUid: '',
  vendor: '',
  model: '',
};

await test('a new endpoint is created only if its name is unused', () => {
  const request = endpointWrite(form);
  assert.equal(request.path.name, 'voron');
  assert.deepEqual(request.header, { 'If-None-Match': '*' });
  assert.equal(request.body.address, 'printer.local');
});

await test('unchanged hardware roles do not require a save', () => {
  const options = {
    chamber_object: 'temperature_sensor Chamber',
    unit_uid: 'voron',
  };
  assert.equal(rolesChanged(options, rolesOf(options)), false);
  assert.equal(
    rolesChanged(options, { ...rolesOf(options), chamber_object: 'none' }),
    true,
  );
  assert.equal(rolesChanged(undefined, rolesOf(undefined)), false);
});

await test('correcting a created endpoint uses its version and the same name', () => {
  const request = endpointWrite(
    { ...form, name: 'other', address: 'fixed.local' },
    { name: 'voron', version: 'v2' },
  );
  assert.equal(request.path.name, 'voron');
  assert.deepEqual(request.header, { 'If-Match': '"v2"' });
  assert.equal(request.body.address, 'fixed.local');
  assert.equal(request.body.options.unit_uid, 'voron');
});
