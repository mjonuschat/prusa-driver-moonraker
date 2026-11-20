import assert from 'node:assert/strict';
import test from 'node:test';

import { endpointWrite } from '../src/endpoint.ts';

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
