import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

function setup() {
  const values = { stagedDays: '3', managedTrashDays: '4', nativeTrashDays: '30', pressureFreeGB: '200', seedRatio: '2', seedMinutes: '2880' };
  const elements = Object.fromEntries(Object.entries(values).map(([key, value]) => [key, { value, focus() {}, parentElement: { textContent: key } }]));
  elements.stagedCleanupOn = { checked: true };
  elements.pressureBypassRetention = { checked: false };
  elements.managedTrashPath = { value: '' };
  const form = { elements, addEventListener() {} };
  const context = vm.createContext({ $: selector => selector === '#cleanup-form' ? form : { addEventListener() {} } });
  const source = fs.readFileSync(new URL('../internal/server/web/cleanup.js', import.meta.url), 'utf8');
  vm.runInContext(source + '\nthis.policy=cleanupPolicy;this.fill=cleanupFill;', context);
  return { context, form };
}

test('pressure retention override must be chosen explicitly and sent with the threshold', () => {
  const { context, form } = setup();
  assert.equal(context.policy(form).pressureBypassRetention, false);
  form.elements.pressureBypassRetention.checked = true;
  const patch = context.policy(form);
  assert.equal(patch.pressureFreeGB, 200);
  assert.equal(patch.pressureBypassRetention, true);
  assert.equal(patch.nativeTrashDays, 30);
  assert.equal(patch.stagedDays + patch.managedTrashDays, 7);
});

test('saved pressure switch is restored and absence remains off', () => {
  const { context, form } = setup();
  context.fill({ pressureBypassRetention: true, pressureFreeGB: 200 });
  assert.equal(form.elements.pressureBypassRetention.checked, true);
  context.fill({});
  assert.equal(form.elements.pressureBypassRetention.checked, false);
});

test('pressure threshold still rejects invalid values before sending a policy', () => {
  const { context, form } = setup();
  form.elements.pressureFreeGB.value = '-1';
  assert.throws(() => context.policy(form), /whole number/);
});
