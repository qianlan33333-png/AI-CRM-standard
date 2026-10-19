import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { resolveChromiumBinary } from '../../internal/webshell/chromium_binary.mjs';

const fixture = JSON.parse(process.env.AICRM_NEW_CAPABILITIES_FIXTURE || '{}');
assert.equal(process.platform, 'linux', 'registered installed browser contracts require Linux');
assert.match(fixture.base || '', /^https:\/\/127\.0\.0\.1:\d+$/);
assert.ok(['sidebar', 'sales'].includes(fixture.mode));
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
class CDP {
  pending = new Map(); sequence = 0; errors = 0; binds = 0; forbidden = 0;
  constructor(socket) {
    this.socket = socket;
    socket.addEventListener('message', event => {
      const message = JSON.parse(String(event.data));
      if (message.method === 'Runtime.exceptionThrown') this.errors++;
      if (message.method === 'Network.requestWillBeSent') {
        const request = message.params.request;
        const pathname = new URL(request.url).pathname;
        if (request.method === 'POST' && pathname === '/api/sidebar/v2/group-ops/bind') this.binds++;
        if (pathname.includes('/send-intents') || pathname === '/api/sidebar/v2/bootstrap') this.forbidden++;
      }
      const pending = this.pending.get(message.id);
      if (!pending) return;
      this.pending.delete(message.id);
      message.error ? pending.reject(new Error('CDP request failed')) : pending.resolve(message.result || {});
    });
  }
  call(method, params = {}) {
    return new Promise((resolve, reject) => {
      const id = ++this.sequence;
      const timer = setTimeout(() => { this.pending.delete(id); reject(new Error('CDP deadline: ' + method)); }, 10000);
      this.pending.set(id, { resolve: result => { clearTimeout(timer); resolve(result); }, reject: error => { clearTimeout(timer); reject(error); } });
      this.socket.send(JSON.stringify({ id, method, params }));
    });
  }
  close() { for (const pending of this.pending.values()) pending.reject(new Error('CDP closed')); this.pending.clear(); this.socket.close(); }
}
async function value(cdp, expression) {
  const result = await cdp.call('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true });
  assert.ok(!result.exceptionDetails, 'isolated page expression failed');
  return result.result?.value;
}
async function until(cdp, expression, label) {
  for (let i = 0; i < 240; i++) { if (await value(cdp, expression)) return; await pause(50); }
  throw new Error(label);
}
async function screenshot(cdp, name) {
  await fs.mkdir(fixture.screenshots, { recursive: true });
  const shot = await cdp.call('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true });
  await fs.writeFile(path.join(fixture.screenshots, name + '.png'), Buffer.from(shot.data, 'base64'));
}
async function sidebar(cdp) {
  await cdp.call('Emulation.setDeviceMetricsOverride', { width: 375, height: 812, deviceScaleFactor: 1, mobile: true });
  await cdp.call('Page.addScriptToEvaluateOnNewDocument', { source: `
    window.__syntheticSDKCalls=[];
    let ready=[];
    window.wx={config(){queueMicrotask(()=>ready.splice(0).forEach(f=>f()));},ready(f){ready.push(f);},error(){},agentConfig(o){o.success();},invoke(method,_,done){
      window.__syntheticSDKCalls.push(method);
      done({err_msg:method+':ok',...(method==='getContext'?{entry:'group_chat_tools'}:method==='getCurExternalChat'?{chatId:'browser-group'}:{})});
    }};
  ` });
  await cdp.call('Network.setCookie', { name: 'aicrm_sidebar_session', value: fixture.session, url: fixture.base, path: '/', secure: true, httpOnly: true });
  await cdp.call('Network.setCookie', { name: 'aicrm_sidebar_csrf', value: fixture.csrf, url: fixture.base, path: '/', secure: true });
  await cdp.call('Page.navigate', { url: fixture.base + '/sidebar/bind-mobile' });
  await until(cdp, `!!document.querySelector('.group-binding-card select')&&!document.querySelector('.group-binding-card select').disabled`, 'packaged group card did not become ready');
  assert.equal(await value(cdp, `document.documentElement.scrollWidth>window.innerWidth`), false, 'mobile group card overflows');
  await screenshot(cdp, 'sidebar-before-bind');
  await value(cdp, `(()=>{
    const select=document.querySelector('.group-binding-card select');select.value=${JSON.stringify(fixture.plan)};select.dispatchEvent(new Event('change'));
    const nativeFetch=window.fetch.bind(window);let lost=false;
    window.fetch=async(...args)=>{const result=await nativeFetch(...args);if(!lost&&String(args[0]).endsWith('/group-ops/bind')){lost=true;throw new TypeError('synthetic response lost after commit');}return result;};
    const button=[...document.querySelectorAll('.group-binding-card button')].find(n=>n.textContent==='绑定当前群');button.click();button.click();return true;
  })()`);
  await until(cdp, `document.querySelector('.group-binding-card')?.textContent.includes('已核对，当前群已绑定')`, 'lost response was not reconciled by installed context');
  await screenshot(cdp, 'sidebar-after-bind');
  assert.equal(cdp.binds, 1, 'duplicate click repeated a binding');
  await cdp.call('Page.reload', { ignoreCache: true });
  await until(cdp, `document.querySelector('.group-binding-card')?.textContent.includes('浏览器草稿 · 草稿')`, 'reopening lost persisted draft binding');
  assert.equal(cdp.binds, 1, 'reopening wrote another binding');
  assert.equal(await value(cdp, `window.__syntheticSDKCalls.includes('getCurExternalContact')||window.__syntheticSDKCalls.includes('sendChatMessage')`), false);
  assert.equal(cdp.forbidden, 0, 'group flow minted a customer token or accepted a send');
  await screenshot(cdp, 'sidebar-reopened');
}
async function sales(cdp) {
  await cdp.call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
  await cdp.call('Network.setCookie', { name: 'aicrm_admin_session', value: fixture.session, url: fixture.base, path: '/', secure: true, httpOnly: true });
  await cdp.call('Network.setCookie', { name: 'aicrm_admin_csrf', value: fixture.csrf, url: fixture.base, path: '/', secure: true });
  for (const [kind, route] of [['standard', '/admin/products.html'], ['period', '/admin/spProducts.html']]) {
    await cdp.call('Page.navigate', { url: fixture.base + route });
    await until(cdp, `!!document.querySelector('[data-product-sales-sources-action]')`, 'packaged product source action missing');
    await value(cdp, `(()=>{const action=document.querySelector('[data-product-sales-sources-action]');const owner=action.closest('[data-table-action-menu-panel]')?.dataset.tableActionMenuPanel;document.querySelector('[data-table-action-menu-trigger="'+owner+'"]')?.click();action.click();return true;})()`);
    await until(cdp, `!!document.querySelector('[data-product-sales-sources-drawer] input[aria-label="新增渠道名称"]')`, 'source drawer did not read installed API');
    const name = '浏览器渠道-' + kind;
    await value(cdp, `(()=>{const input=document.querySelector('[aria-label="新增渠道名称"]');input.value=${JSON.stringify(name)};input.dispatchEvent(new Event('input',{bubbles:true}));input.closest('form').requestSubmit();return true;})()`);
    await until(cdp, `document.querySelector('[data-product-sales-sources-drawer]')?.textContent.includes('渠道已创建并重新读取')`, 'browser source create/readback failed');
    await value(cdp, `(()=>{const article=[...document.querySelectorAll('[data-product-sales-sources-drawer] article')].find(n=>n.querySelector('strong')?.textContent===${JSON.stringify(name)});[...article.querySelectorAll('button')].find(n=>n.textContent==='改名').click();return true;})()`);
    await value(cdp, `(()=>{const input=document.querySelector('[aria-label="${name}渠道名称"]');input.value=${JSON.stringify(name + '-改名')};input.dispatchEvent(new Event('input',{bubbles:true}));[...input.parentElement.querySelectorAll('button')].find(n=>n.textContent==='保存改名').click();return true;})()`);
    await until(cdp, `document.querySelector('[data-product-sales-sources-drawer]')?.textContent.includes('渠道名称已更新并重新读取')`, 'browser source rename/readback failed');
    await screenshot(cdp, 'sales-' + kind);
    assert.equal(await value(cdp, `(()=>{const article=[...document.querySelectorAll('[data-product-sales-sources-drawer] article')].find(n=>n.querySelector('strong')?.textContent===${JSON.stringify(name + '-改名')});const url=new URL(article.querySelector('a').href);return url.pathname==='/admin/orders'&&Number(url.searchParams.get('product_id'))>0&&Number(url.searchParams.get('sales_source'))>0;})()`), true, 'source order link lost its exact attribution');
    await cdp.call('Page.reload', { ignoreCache: true });
    await until(cdp, `!!document.querySelector('[data-product-sales-sources-action]')`, 'reopened source action missing');
    await value(cdp, `document.querySelector('[data-product-sales-sources-action]').click();true`);
    await until(cdp, `document.querySelector('[data-product-sales-sources-drawer]')?.textContent.includes(${JSON.stringify(name + '-改名')})`, 'reopening lost source rename');
  }
}
const profile = await fs.mkdtemp(path.join(os.tmpdir(), 'aicrm-new-capabilities-'));
let browser, cdp, failure;
try {
  browser = spawn(resolveChromiumBinary(), ['--headless=new', '--no-sandbox', '--ignore-certificate-errors', '--no-proxy-server', '--disable-background-networking', '--remote-debugging-port=0', `--user-data-dir=${profile}`, 'about:blank'], { stdio: 'ignore' });
  let port;
  for (let i = 0; i < 200; i++) { try { port = (await fs.readFile(path.join(profile, 'DevToolsActivePort'), 'utf8')).split('\n')[0]; if (/^\d+$/.test(port)) break; } catch {} await pause(50); }
  assert.match(port || '', /^\d+$/, 'isolated Chromium startup');
  const target = await (await fetch(`http://127.0.0.1:${port}/json/new?about:blank`, { method: 'PUT' })).json();
  const socket = new WebSocket(target.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => { socket.addEventListener('open', resolve, { once: true }); socket.addEventListener('error', reject, { once: true }); });
  cdp = new CDP(socket);
  await cdp.call('Page.enable'); await cdp.call('Runtime.enable'); await cdp.call('Network.enable');
  // This contract supplies a synthetic SDK before document load. Keep the
  // packaged remote SDK tag from replacing it or introducing network timing.
  await cdp.call('Network.setBlockedURLs', { urls: ['https://res.wx.qq.com/*'] });
  await (fixture.mode === 'sidebar' ? sidebar(cdp) : sales(cdp));
  assert.equal(cdp.errors, 0, 'packaged capability emitted a JavaScript exception');
  console.log(JSON.stringify({ contract: 'new-capabilities-installed-browser', mode: fixture.mode, status: 'passed', javascript_errors: cdp.errors, binding_posts: cdp.binds, forbidden_calls: cdp.forbidden }));
} catch (error) { failure = error; if (cdp) await screenshot(cdp, fixture.mode + '-failure').catch(() => {}); }
finally {
  cdp?.close();
  if (browser && browser.exitCode === null && browser.signalCode === null) {
    browser.kill('SIGTERM');
    for (let i = 0; i < 60 && browser.exitCode === null && browser.signalCode === null; i++) await pause(50);
    if (browser.exitCode === null && browser.signalCode === null) { browser.kill('SIGKILL'); await new Promise(resolve => browser.once('exit', resolve)); }
  }
  await fs.rm(profile, { recursive: true, force: true, maxRetries: 5 }).catch(() => console.warn('closed synthetic browser profile cleanup warning'));
}
if (failure) throw failure;
