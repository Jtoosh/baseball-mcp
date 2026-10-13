'use strict';
const form = document.querySelector('#question-form');
const results = document.querySelector('#results');
const toggle = document.querySelector('#show-table');
const statusText = document.querySelector('#status-text');
const loadingBall = document.querySelector('#loading-ball');
const requestError = document.querySelector('#request-error');
const previousLabel = document.querySelector('#previous-label');
let busy = false;
let displayedKind = '';

const markdown = new marked.Marked({
  gfm: true,
  renderer: { html() { return ''; }, image() { return ''; } }
});
function element(tag, text, className) {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = text;
  if (className) node.className = className;
  return node;
}
function message(text, kind) {
  const container = element('div', undefined, 'markdown');
  if (kind === 'error') {
    container.append(element('p', text));
    return container;
  }
  const fragment = DOMPurify.sanitize(markdown.parse(text || ''), {
    ALLOWED_TAGS: ['p', 'br', 'strong', 'em', 'del', 's', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'ul', 'ol', 'li', 'blockquote', 'hr', 'pre', 'code', 'a', 'table', 'thead', 'tbody', 'tr', 'th', 'td'],
    ALLOWED_ATTR: ['href', 'title', 'start'],
    ALLOW_DATA_ATTR: false,
    ALLOW_ARIA_ATTR: false,
    RETURN_DOM_FRAGMENT: true
  });
  fragment.querySelectorAll('a').forEach(link => {
    let destination;
    try { destination = new URL(link.getAttribute('href'), location.href); } catch { /* Invalid links stay as text. */ }
    if (!link.hasAttribute('href') || !destination || !['http:', 'https:'].includes(destination.protocol)) {
      link.replaceWith(...link.childNodes);
      return;
    }
    link.href = destination.href;
    link.target = '_blank';
    link.rel = 'noopener noreferrer';
  });
  fragment.querySelectorAll('table').forEach(table => {
    const wrapper = element('div', undefined, 'table-wrap');
    table.replaceWith(wrapper);
    wrapper.append(table);
  });
  container.append(fragment);
  return container;
}
toggle.addEventListener('change', () => {
  document.querySelectorAll('table[data-supporting-results]').forEach(table => { table.hidden = !toggle.checked; });
});
function supportingTable(data) {
  const table = element('table');
  table.dataset.supportingResults = '';
  table.hidden = !toggle.checked;
  const header = element('thead');
  const headingRow = element('tr');
  for (const label of data.columns) {
    const cell = element('th', label);
    cell.scope = 'col';
    headingRow.append(cell);
  }
  header.append(headingRow);
  const body = element('tbody');
  for (const values of data.rows) {
    const row = element('tr');
    for (const value of values) row.append(element('td', value));
    body.append(row);
  }
  table.append(header, body);
  const wrapper = element('div', undefined, 'table-wrap');
  wrapper.append(table);
  return wrapper;
}
function responseContent(data, question) {
  const content = document.createDocumentFragment();
  content.append(element('p', 'Response to: ' + question, 'response-question'));
  if (data.message) content.append(message(data.message, data.kind));
  if (data.kind === 'clarification') {
    const panel = element('section', undefined, 'panel');
    if (data.choices?.length) {
      for (const choice of data.choices) {
        const button = element('button', choice);
        button.type = 'button';
        button.addEventListener('click', () => ask({ question, confirmation: data.confirmation, choice }));
        const row = element('p');
        row.append(button);
        panel.append(row);
      }
    } else {
      const confirm = element('button', 'Confirm interpretation');
      confirm.type = 'button';
      confirm.id = 'confirm';
      confirm.addEventListener('click', () => ask({ question, confirmation: data.confirmation }));
      panel.append(confirm);
    }
    const revisionForm = element('form');
    const label = element('label', 'Revise the interpretation');
    const revision = element('textarea');
    revision.id = 'revision';
    revision.rows = 2;
    revision.maxLength = 4000;
    revision.required = true;
    label.append(revision);
    const revise = element('button', 'Use revision');
    revise.type = 'submit';
    revisionForm.append(label, revise);
    revisionForm.addEventListener('submit', event => {
      event.preventDefault();
      ask({ question, confirmation: data.confirmation, revision: revision.value });
    });
    panel.append(revisionForm);
    content.append(panel);
  }
  for (const outcome of data.strategies || []) {
    const panel = element('article', undefined, 'panel');
    panel.append(element('h2', outcome.strategy === 'examples' ? 'With worked examples' : 'Schema and instructions'));
    panel.append(message(outcome.message, outcome.kind));
    if (data.seasons?.length) panel.append(element('p', 'Season snapshots: ' + data.seasons.join(', ')));
    if (outcome.kind === 'answer' && outcome.table) panel.append(supportingTable(outcome.table));
    content.append(panel);
  }
  return content;
}
function showFailure(data, question) {
  const content = responseContent(data, question);
  requestError.replaceChildren(element('p', 'The new response has an error.'), content);
  requestError.hidden = false;
  previousLabel.hidden = !results.hasChildNodes();
  previousLabel.textContent = displayedKind === 'answer' ? 'Previous answer' : 'Previous response';
}
async function ask(request) {
  if (busy) return;
  busy = true;
  loadingBall.hidden = false;
  statusText.textContent = results.hasChildNodes() ? 'Loading a new response…' : 'Looking up the answer…';
  results.setAttribute('aria-busy', 'true');
  requestError.hidden = true;
  document.querySelectorAll('button').forEach(button => { button.disabled = true; });
  try {
    const response = await fetch('/api/questions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) });
    const data = await response.json();
    if (!data || !['answer', 'clarification', 'unsupported', 'error'].includes(data.kind)) throw new Error('Invalid response');
    if (!response.ok && data.kind !== 'error') throw new Error('Request failed');
    if (data.kind === 'error') {
      showFailure(data, request.question);
    } else {
      const content = responseContent(data, request.question);
      results.replaceChildren(content);
      displayedKind = data.kind;
      previousLabel.hidden = true;
      requestError.replaceChildren();
    }
  } catch {
    showFailure({ kind: 'error', message: "I couldn't load the new response. Please try again." }, request.question);
  } finally {
    busy = false;
    loadingBall.hidden = true;
    statusText.textContent = '';
    results.setAttribute('aria-busy', 'false');
    document.querySelectorAll('button').forEach(button => { button.disabled = false; });
  }
}
form.addEventListener('submit', event => {
  event.preventDefault();
  ask({ question: document.querySelector('#question').value.trim() });
});
