import assert from 'node:assert/strict';
import fs from 'node:fs';
import { build } from 'esbuild';
import { JSDOM } from 'jsdom';
const html = fs.readFileSync('web/dist/sidebar/index.html', 'utf8');
const overlayPath = new URL(new JSDOM(html).window.document.querySelector('#sidebar-workbench-root').dataset.overlayUrl, 'https://fixture.test/sidebar/index.html').pathname;
const overlay = fs.readFileSync('web/dist' + overlayPath, 'utf8');
const bundle = (await build({ entryPoints: ['web/v3/sidebar/main.ts'], bundle: true, format: 'iife', write: false, target: 'es2022' })).outputFiles[0].text;
const dom = new JSDOM(html, { url: 'https://fixture.test/sidebar/bind-mobile', runScripts: 'outside-only', pretendToBeVisual: true });
const w = dom.window; w.AbortController = AbortController; w.AbortSignal = AbortSignal;
let current = 'fixture-test-1', deny = false, callback, configs = [], opens = [], ready = [], requests = [], methods = [];
w.wx = {
  config(o) { configs.push(o); queueMicrotask(() => ready.splice(0).forEach(f => f())); }, ready(f) { ready.push(f); }, error() {}, agentConfig(o) { configs.push(o); o.success(); },
  invoke(method, payload, done) {
    methods.push(method);
    if (method === 'openExistedChatWithMsg') { opens.push(JSON.parse(JSON.stringify(payload))); callback = done; return; }
    assert.ok(['getContext', 'getCurExternalChat'].includes(method), 'unexpected SDK call: ' + method);
    done({ err_msg: method + ':ok', ...(method === 'getContext' ? { entry: 'group_chat_tools' } : { chatId: current }) });
  },
};
const response = (body, status = 200) => ({ ok: status === 200, status, text: async () => JSON.stringify(body) });
w.fetch = async (path, options = {}) => {
  const url = new URL(path, w.location.href); requests.push(url.pathname);
  if (url.pathname === '/api/sidebar/jssdk-config') return response({ corp_id: 'corp', agent_id: '1', config: { timestamp: 1, nonceStr: 'n', signature: 's' }, agent_config: { timestamp: 1, nonceStr: 'n', signature: 's' } });
  assert.equal(url.pathname, '/api/sidebar/v2/group-ops/context', 'no business write or send endpoint allowed');
  if (deny) return response({ error: { code: 'permission_denied' } }, 403);
  const chat = JSON.parse(options.body).chat_id;
  return response({ chat_id: chat, name: chat === 'fixture-test-1' ? '测试多群1' : chat === 'fixture-test-5' ? '测试多群5' : '正式群', owner_name: '测试群主', checked_at: '2026-10-15T06:00:00Z', bindings: [], candidates: [], total: 0, limit: 50, offset: 0 });
};
async function until(fn, message) { for (let i = 0; i < 200; i++) { if (fn()) return; await new Promise(r => setTimeout(r, 2)); } assert.fail(message); }
const button = text => [...w.document.querySelectorAll('button')].find(b => b.textContent === text);
const panel = () => w.document.querySelector('.open-chat-probe');
w.eval(bundle); w.document.dispatchEvent(new w.Event('DOMContentLoaded'));
await until(() => w.__AICRMSidebarBridge, 'Host missing'); w.eval(overlay);
await until(() => panel(), 'probe missing');
assert.equal(panel().open, false, 'probe must start collapsed');
assert.equal(opens.length, 0, 'loading must not navigate');
assert.ok(configs.length >= 2 && configs.every(c => c.jsApiList.includes('openExistedChatWithMsg')), 'both SDK registrations must declare navigation');
button('记住此测试群').click(); await until(() => panel().textContent.includes('已在本机记住'), 'remember failed');
current = 'fixture-test-5'; await w.__AICRMSidebarBridge.retry();
assert.match(panel().textContent, /目标：测试多群1/);
button('打开已记住的测试群').click(); button('打开已记住的测试群').click();
await until(() => opens.length === 1, 'open did not invoke');
assert.deepEqual(opens[0], { chatId: 'fixture-test-1' }, 'payload must contain only exact chatId and no msg');
callback({ err_msg: 'openExistedChatWithMsg:fail no permission' });
await until(() => panel().textContent.includes('fail no permission'), 'failure must be visible');
assert.equal(opens.length, 1, 'failure must not automatically retry or fall back');
button('打开已记住的测试群').click(); await until(() => opens.length === 2, 'explicit retry missing');
callback({ err_msg: 'openExistedChatWithMsg:ok' });
await until(() => panel().textContent.includes('接口结果：openExistedChatWithMsg:ok'), 'success receipt missing');
current = 'fixture-production'; button('打开已记住的测试群').click();
await until(() => !panel(), 'non-test group must hide the probe');
assert.equal(opens.length, 2, 'stale context navigated');
current = 'fixture-test-5'; await w.__AICRMSidebarBridge.retry();
button('清除本机记录').click(); assert.equal(button('打开已记住的测试群').disabled, true);
assert.equal(w.localStorage.getItem('aicrm.sidebar.open-test-chat.v1'), null);
w.localStorage.setItem('aicrm.sidebar.open-test-chat.v1', JSON.stringify({ name: '正式群', chatId: 'fixture-production' }));
await w.__AICRMSidebarBridge.retry(); assert.equal(button('打开已记住的测试群').disabled, true, 'non-test stored target accepted');
deny = true; await w.__AICRMSidebarBridge.retry(); assert.equal(panel(), null, 'permission denial retained probe');
assert.equal(methods.includes('sendChatMessage'), false);
assert.equal(requests.some(p => /send|bind$|intent|execution/.test(p)), false);
w.close();
console.log('PASS open-only exact chatId/no msg, two-group navigation, zero writes/sends, registration, collapsed entry, duplicate click, visible failure, explicit retry, stale context, non-test/denied hide, local record clear');
