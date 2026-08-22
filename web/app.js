const el = (id) => document.getElementById(id);

const state = {
  document: null,
  selected: null,
  draft: null,
  schema: {},
};

const keys = {
  get admin() { return sessionStorage.getItem('rulecast.admin') ?? ''; },
  set admin(value) { sessionStorage.setItem('rulecast.admin', value); },
  get sdk() { return sessionStorage.getItem('rulecast.sdk') ?? ''; },
  set sdk(value) { sessionStorage.setItem('rulecast.sdk', value); },
};

async function call(path, { method = 'GET', body, key = keys.admin, signal } = {}) {
  const headers = {};
  if (key) headers['X-API-Key'] = key;
  if (body !== undefined) headers['Content-Type'] = 'application/json';

  const response = await fetch(path, {
    method,
    headers,
    signal,
    body: body === undefined ? undefined : JSON.stringify(body),
  });

  const text = await response.text();
  const payload = text ? JSON.parse(text) : null;

  if (!response.ok) {
    const error = new Error(payload?.message ?? `${response.status} ${response.statusText}`);
    error.payload = payload;
    throw error;
  }
  return payload;
}

/* ------------------------------------------------------------------ loading */

async function load() {
  try {
    const flags = await call('/api/v1/admin/flags');
    state.document = flags;
    el('status').textContent =
      `${flags.environment} · version ${flags.version} · ${flags.flags.length} flags`;
    renderFlagList();
    renderSchema(await call('/api/v1/admin/schema'));
    renderVersions(await call('/api/v1/admin/versions?limit=15'));

    if (state.selected) select(state.selected);
  } catch (error) {
    el('status').textContent = `not loaded: ${error.message}`;
  }
}

function renderFlagList() {
  const list = el('flag-list');
  list.replaceChildren();

  for (const flag of state.document.flags) {
    const item = document.createElement('li');
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'flag-button' + (flag.key === state.selected ? ' is-active' : '');

    const dot = document.createElement('span');
    dot.className = 'dot' + (flag.enabled ? ' on' : '');

    const name = document.createElement('span');
    name.textContent = flag.key;

    const kind = document.createElement('span');
    kind.className = 'flag-kind';
    kind.textContent = flag.kind;

    button.append(dot, name, kind);
    button.addEventListener('click', () => select(flag.key));
    item.append(button);
    list.append(item);
  }
}

function renderVersions(history) {
  const list = el('version-list');
  list.replaceChildren();

  for (const version of history ?? []) {
    const item = document.createElement('li');

    const number = document.createElement('span');
    number.className = 'version-number';
    number.textContent = `v${version.version}`;

    const comment = document.createElement('span');
    comment.className = 'version-comment';
    comment.title = version.comment ?? '';
    comment.textContent = version.comment || '—';

    item.append(number, comment);

    if (version.version !== state.document.version) {
      const rollback = document.createElement('button');
      rollback.type = 'button';
      rollback.className = 'button button-ghost';
      rollback.textContent = 'Roll back';
      rollback.addEventListener('click', () => rollbackTo(version.version));
      item.append(rollback);
    }

    list.append(item);
  }
}

function renderSchema(schema) {
  state.schema = schema ?? {};
  const list = el('schema');
  list.replaceChildren();

  for (const name of Object.keys(schema ?? {}).sort()) {
    const item = document.createElement('li');
    const label = document.createElement('span');
    label.textContent = name;
    const type = document.createElement('span');
    type.className = 'type';
    type.textContent = schema[name];
    item.append(label, type);
    list.append(item);
  }
}

/* ------------------------------------------------------------------- editor */

function select(key) {
  const flag = state.document.flags.find((candidate) => candidate.key === key);
  if (!flag) return;

  state.selected = key;
  state.draft = structuredClone(flag);

  el('empty').hidden = true;
  el('editor').hidden = false;
  el('publish-result').hidden = true;

  el('flag-key').textContent = flag.key;
  el('flag-description').textContent = flag.description ?? '';
  el('flag-enabled').checked = flag.enabled;

  renderFlagList();
  renderVariations();
  renderRules();
}

function renderVariations() {
  const table = el('variations');
  table.replaceChildren();

  const head = table.createTHead().insertRow();
  for (const column of ['key', 'value', 'role']) {
    const cell = document.createElement('th');
    cell.textContent = column;
    head.append(cell);
  }

  const body = table.createTBody();
  for (const variation of state.draft.variations) {
    const row = body.insertRow();
    row.insertCell().textContent = variation.key;
    row.insertCell().textContent = JSON.stringify(variation.value);

    const roles = [];
    if (variation.key === state.draft.default) roles.push('default');
    if (variation.key === state.draft.offVariation) roles.push('off');
    row.insertCell().textContent = roles.join(', ') || '—';
  }
}

function variationOptions(selected) {
  const select = document.createElement('select');
  for (const variation of state.draft.variations) {
    const option = document.createElement('option');
    option.value = variation.key;
    option.textContent = variation.key;
    if (variation.key === selected) option.selected = true;
    select.append(option);
  }
  return select;
}

function renderRules() {
  const container = el('rules');
  container.replaceChildren();

  state.draft.rules ??= [];

  state.draft.rules.forEach((rule, index) => {
    const card = document.createElement('div');
    card.className = 'rule';

    const head = document.createElement('div');
    head.className = 'rule-head';

    const id = document.createElement('span');
    id.className = 'rule-id';
    id.textContent = rule.id || `rule-${index + 1}`;

    const remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'button button-danger';
    remove.textContent = 'Remove';
    remove.addEventListener('click', () => {
      state.draft.rules.splice(index, 1);
      renderRules();
    });

    head.append(id, remove);

    const editor = document.createElement('textarea');
    editor.className = 'code';
    editor.rows = 2;
    editor.value = rule.expression ?? '';
    editor.spellcheck = false;

    const validation = document.createElement('div');
    validation.className = 'validation';

    let timer;
    editor.addEventListener('input', () => {
      rule.expression = editor.value;
      clearTimeout(timer);
      timer = setTimeout(() => validate(rule, editor.value, validation), 250);
    });

    const serve = document.createElement('div');
    serve.className = 'rule-serve';
    renderServe(rule, serve);

    card.append(head, editor, validation, serve);
    container.append(card);

    validate(rule, editor.value, validation);
  });
}

function renderServe(rule, container) {
  container.replaceChildren();

  const label = document.createElement('label');
  label.textContent = 'serve';
  container.append(label);

  const mode = document.createElement('select');
  for (const option of ['variation', 'rollout']) {
    const item = document.createElement('option');
    item.value = option;
    item.textContent = option;
    if ((option === 'rollout') === Boolean(rule.serve?.rollout?.length)) item.selected = true;
    mode.append(item);
  }
  mode.addEventListener('change', () => {
    rule.serve = mode.value === 'rollout'
      ? { rollout: state.draft.variations.map((variation, index) => ({
          variation: variation.key,
          weight: index === 0 ? 10 : 90 / Math.max(state.draft.variations.length - 1, 1),
        })) }
      : { variation: state.draft.variations[0].key };
    renderServe(rule, container);
  });
  container.append(mode);

  if (rule.serve?.rollout?.length) {
    for (const entry of rule.serve.rollout) {
      const name = document.createElement('span');
      name.className = 'rule-id';
      name.textContent = entry.variation;

      const weight = document.createElement('input');
      weight.className = 'input';
      weight.type = 'number';
      weight.min = '0';
      weight.step = '1';
      weight.style.minWidth = '72px';
      weight.value = entry.weight;
      weight.addEventListener('input', () => { entry.weight = Number(weight.value); });

      container.append(name, weight);
    }
    return;
  }

  const chooser = variationOptions(rule.serve?.variation);
  chooser.addEventListener('change', () => { rule.serve = { variation: chooser.value }; });
  container.append(chooser);
}

const validated = new WeakMap();
const inFlight = new WeakMap();

async function validate(rule, expression, target) {
  const text = expression.trim();

  if (!text) {
    validated.delete(rule);
    target.className = 'validation';
    target.textContent = '';
    return;
  }
  if (validated.get(rule) === text) {
    return;
  }

  inFlight.get(rule)?.abort();
  const controller = new AbortController();
  inFlight.set(rule, controller);

  try {
    const result = await call('/api/v1/admin/validate', {
      method: 'POST',
      body: { expression },
      signal: controller.signal,
    });
    validated.set(rule, text);

    if (result.valid) {
      target.className = 'validation ok';
      target.textContent = `ok — ${result.normalised}`;
    } else {
      target.className = 'validation bad';
      target.textContent = result.annotated || result.message;
    }
  } catch (error) {
    if (error.name === 'AbortError') {
      return;
    }
    target.className = 'validation bad';
    target.textContent = error.message;
  }
}

/* ---------------------------------------------------------------- publishing */

async function publish() {
  const notice = el('publish-result');
  notice.hidden = false;
  notice.className = 'notice';
  notice.textContent = 'publishing…';

  state.draft.enabled = el('flag-enabled').checked;
  state.draft.rules = (state.draft.rules ?? []).filter((rule) => rule.expression?.trim());
  state.draft.rules.forEach((rule, index) => { rule.id ||= `rule-${index + 1}`; });

  try {
    const result = await call(`/api/v1/admin/flags/${encodeURIComponent(state.draft.key)}`, {
      method: 'PUT',
      body: { comment: el('comment').value, flag: state.draft },
    });
    notice.className = 'notice ok';
    notice.textContent = `published version ${result.version} with ${result.flags} flags`;
    el('comment').value = '';
    await load();
  } catch (error) {
    notice.className = 'notice bad';
    const problems = error.payload?.problems;
    notice.textContent = problems?.length
      ? problems.map((problem) => `${problem.path}: ${problem.message}`).join('\n')
      : error.message;
  }
}

async function rollbackTo(version) {
  try {
    await call(`/api/v1/admin/versions/${version}/rollback`, { method: 'POST' });
    await load();
  } catch (error) {
    el('status').textContent = `rollback failed: ${error.message}`;
  }
}

/* ------------------------------------------------------------------- try it */

async function evaluate() {
  const output = el('evaluation');
  if (!state.selected) {
    output.textContent = 'select a flag first';
    return;
  }

  let parsed;
  try {
    parsed = JSON.parse(el('attributes').value);
  } catch (error) {
    output.textContent = `attributes must be valid JSON: ${error.message}`;
    return;
  }
  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    output.textContent = 'attributes must be a JSON object';
    return;
  }

  const attributes = unwrapRequest(parsed);
  const ignored = Object.keys(attributes).filter((key) => !(key in state.schema));

  try {
    const result = await call('/api/v1/sdk/evaluate', {
      method: 'POST',
      key: keys.sdk,
      body: { flagKey: state.selected, attributes },
    });

    let text = JSON.stringify(result, null, 2);
    if (attributes !== parsed) {
      text += '\n\nnote: a full request body was pasted; only its "attributes" were used.';
    }
    if (ignored.length) {
      text += `\n\nignored, not in the schema: ${ignored.join(', ')}`;
    }
    output.textContent = text;
  } catch (error) {
    output.textContent = error.message;
  }
}

function unwrapRequest(parsed) {
  const looksLikeAFullRequest =
    typeof parsed.flagKey === 'string' &&
    parsed.attributes !== null &&
    typeof parsed.attributes === 'object';

  return looksLikeAFullRequest ? parsed.attributes : parsed;
}

/* ---------------------------------------------------------------- bootstrap */

function init() {
  el('admin-key').value = keys.admin;
  el('sdk-key').value = keys.sdk;
  el('admin-key').addEventListener('change', (event) => { keys.admin = event.target.value; load(); });
  el('sdk-key').addEventListener('change', (event) => { keys.sdk = event.target.value; });

  el('attributes').value = JSON.stringify({
    'user.key': 'user-1234',
    'user.country': 'DE',
    'user.tier': 'vip',
    'user.spend': 400,
    'user.language': 'en',
    'user.beta': false,
    'device.os': 'ios',
    'app.version': '4.12.0',
  }, null, 2);

  el('reload').addEventListener('click', load);
  el('publish').addEventListener('click', publish);
  el('evaluate').addEventListener('click', evaluate);
  el('add-rule').addEventListener('click', () => {
    state.draft.rules ??= [];
    state.draft.rules.push({
      id: `rule-${state.draft.rules.length + 1}`,
      expression: '',
      serve: { variation: state.draft.variations[0].key },
    });
    renderRules();
  });

  load();
}

init();
