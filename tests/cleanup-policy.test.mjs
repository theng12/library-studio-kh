import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

function node(id) {
  return {
    id, hidden: false, textContent: "", childElementCount: 0, scrollHeight: 0, rows: [],
    attrs: {}, dataset: {}, listeners: {},
    setAttribute(k, v) { this.attrs[k] = String(v); },
    getAttribute(k) { return this.attrs[k] ?? null; },
    addEventListener(type, fn) { (this.listeners[type] ||= []).push(fn); },
    querySelectorAll() { return this.rows; },
    contains() { return false; },
    focus() { this.focused = true; },
    replaceChildren(...kids) { this.childElementCount = kids.length; },
  };
}

function setup(extra = {}) {
  const values = { stagedDays: '3', managedTrashDays: '4', nativeTrashDays: '30', pressureFreeGB: '200', seedRatio: '2', seedMinutes: '2880' };
  const elements = Object.fromEntries(Object.entries(values).map(([key, value]) => [key, { value, focus() {}, parentElement: { textContent: key } }]));
  elements.stagedCleanupOn = { checked: true };
  elements.pressureBypassRetention = { checked: false };
  elements.managedTrashPath = { value: '' };
  const form = { elements, addEventListener() {} };
  const nodes = {};
  const nodeFor = (selector) => nodes[selector] || (nodes[selector] = node(selector.replace(/^#/, "")));
  const document = { activeElement: null, getElementById: (id) => nodeFor("#" + id) };
  const context = vm.createContext({
    $: (selector) => selector === '#cleanup-form' ? form : nodeFor(selector),
    document,
    ...extra,
  });
  const source = fs.readFileSync(new URL('../internal/server/web/cleanup.js', import.meta.url), 'utf8');
  vm.runInContext(source + '\nthis.policy=cleanupPolicy;this.fill=cleanupFill;this.apply=cleanupCollapseApply;'
    + ';this.toggle=cleanupCollapseToggle;this.lock=cleanupLockNumbers;this.checkPath=cleanupCheckPath;', context);
  return { context, form, nodeFor, document };
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

test('absent settings show every number at its default, disabled while the switch is off', () => {
  const { context, form } = setup();
  context.fill({});
  // The same numbers config.Default() uses: a field must never show a value the
  // engine would not use.
  const defaults = { stagedDays: '3', managedTrashDays: '4', nativeTrashDays: '30', pressureFreeGB: '50', seedRatio: '2', seedMinutes: '2880' };
  for (const [key, want] of Object.entries(defaults)) {
    assert.equal(String(form.elements[key].value), want, key + ' shows its default');
    assert.equal(form.elements[key].disabled, true, key + ' is disabled while the switch is off');
    assert.notEqual(form.elements[key].hidden, true, key + ' is visible, never hidden');
  }
  assert.equal(form.elements.stagedCleanupOn.checked, false);
});

test('saved values beat the defaults and the switch unlocks the numbers', () => {
  const { context, form } = setup();
  context.fill({ stagedCleanupOn: true, stagedDays: 7, pressureFreeGB: 0, seedMinutes: 2880 });
  assert.equal(String(form.elements.stagedDays.value), '7');
  assert.equal(String(form.elements.pressureFreeGB.value), '0', 'a saved 0 is kept, not replaced by the default');
  assert.equal(String(form.elements.seedMinutes.value), '2880');
  assert.equal(String(form.elements.seedRatio.value), '2', 'an absent field still falls back to its default');
  assert.equal(form.elements.stagedDays.disabled, false, 'editable while the switch is on');
  context.fill({ stagedCleanupOn: false, stagedDays: 7 });
  assert.equal(form.elements.stagedDays.disabled, true, 'switching off locks the numbers again');
  assert.equal(String(form.elements.stagedDays.value), '7', 'switching off never blanks a saved number');
});

test('the lock follows the switch even before a reload', () => {
  const { context, form } = setup();
  form.elements.stagedCleanupOn.checked = true;
  context.lock();
  assert.equal(form.elements.seedRatio.disabled, false);
  form.elements.stagedCleanupOn.checked = false;
  context.lock();
  assert.equal(form.elements.seedRatio.disabled, true);
  assert.notEqual(form.elements.seedRatio.value, '', 'a locked number keeps its value');
});

test('a long block starts collapsed and the control reports aria-expanded', () => {
  const { context, nodeFor } = setup();
  const button = nodeFor('#cleanup-status-toggle');
  const region = nodeFor('#cleanup-status');
  region.childElementCount = 3;
  region.rows = Array(20).fill({});
  context.apply(button, region);
  assert.equal(region.hidden, true, 'content that runs long starts collapsed');
  assert.equal(button.hidden, false, 'the control is shown when there is something to collapse');
  assert.equal(button.getAttribute('aria-expanded'), 'false');
  assert.equal(button.textContent, 'Show more');
});

test('Show more/less flips the block, keeps aria-expanded honest, and remembers the choice', () => {
  const { context, nodeFor } = setup();
  const button = nodeFor('#cleanup-status-toggle');
  const region = nodeFor('#cleanup-status');
  region.childElementCount = 3;
  region.rows = Array(20).fill({});
  context.apply(button, region);
  context.toggle(button, region); // the owner expands
  assert.equal(region.hidden, false);
  assert.equal(button.getAttribute('aria-expanded'), 'true');
  assert.equal(button.textContent, 'Show less');
  context.apply(button, region); // a re-render must not undo the owner's choice
  assert.equal(region.hidden, false);
  context.toggle(button, region); // and collapses again on the second press
  assert.equal(region.hidden, true);
  assert.equal(button.getAttribute('aria-expanded'), 'false');
  assert.equal(button.textContent, 'Show more');
});

test('a short block stays open and an empty one hides the control', () => {
  const { context, nodeFor } = setup();
  const button = nodeFor('#cleanup-companions-toggle');
  const region = nodeFor('#companion-settings');
  region.childElementCount = 1;
  region.rows = Array(2).fill({});
  context.apply(button, region);
  assert.equal(region.hidden, false);
  assert.equal(button.getAttribute('aria-expanded'), 'true');
  assert.equal(button.textContent, 'Show less');
  region.childElementCount = 0;
  context.apply(button, region);
  assert.equal(button.hidden, true, 'nothing to collapse means no control to press');
});

test('a block never collapses out from under focus', () => {
  const { context, nodeFor, document } = setup();
  const button = nodeFor('#cleanup-status-toggle');
  const region = nodeFor('#cleanup-status');
  region.childElementCount = 3;
  region.rows = Array(20).fill({});
  const inside = {};
  document.activeElement = inside;
  region.contains = (el) => el === inside;
  context.apply(button, region);
  assert.equal(region.hidden, true);
  assert.equal(button.focused, true, 'focus lands on the control that stays');
});

test('a typed folder that fails its check keeps the saved value and states the reason', async () => {
  const { context, nodeFor } = setup({
    window: { folderPicker: { checkPath: async (value) => (value && !value.startsWith('/') ? 'Type a full folder path, starting with /.' : '') } },
  });
  context.fill({ managedTrashPath: '/Users/me/_trash' });
  const line = nodeFor('#managedTrashPath-picker-status');
  const patch = { managedTrashPath: 'trash' };
  const problem = await context.checkPath(patch);
  assert.match(problem, /full folder path/);
  assert.equal(patch.managedTrashPath, '/Users/me/_trash', 'the saved folder stays in the POST');
  assert.equal(line.textContent, problem, 'the reason lands in the field’s own status line');
  assert.equal(line.dataset.invalid, '1');
  const ok = { managedTrashPath: '/Users/me/other' };
  assert.equal(await context.checkPath(ok), '');
  assert.equal(ok.managedTrashPath, '/Users/me/other', 'a usable path saves as typed');
  assert.equal(line.dataset.invalid, undefined, 'a passing path clears the old reason');
});
