import { resolveChromiumBinary } from '../../internal/webshell/chromium_binary.mjs';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';

const base = process.env.AICRM_REFERRAL_ADMIN_BROWSER_URL;
const campaign = Number(process.env.AICRM_REFERRAL_ADMIN_BROWSER_CAMPAIGN);
const savedProduct = Number(process.env.AICRM_REFERRAL_ADMIN_BROWSER_SAVED_PRODUCT);
const addProduct = Number(process.env.AICRM_REFERRAL_ADMIN_BROWSER_ADD_PRODUCT);
assert.match(base || '', /^https:\/\/127\.0\.0\.1:/);
assert.ok(campaign > 0 && savedProduct > 0 && addProduct > 0 && savedProduct !== addProduct);

const profile = await fs.mkdtemp(path.join(os.tmpdir(), 'referral-compatible-browser-'));
const evidenceDir = process.env.AICRM_COMPATIBLE_BROWSER_SCREENSHOTS || '/tmp/aicrm-shop-membership-ui-evidence';
await fs.mkdir(evidenceDir, { recursive: true });
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
let chrome;
let socket;
const pending = new Map();
let sequence = 0;
const writes = [];
const errors = [];

try {
  chrome = spawn(resolveChromiumBinary(), [
    '--headless=new', '--no-sandbox', '--remote-debugging-port=0',
    `--user-data-dir=${profile}`, '--no-first-run', '--ignore-certificate-errors',
    '--disable-background-networking', 'about:blank',
  ], { stdio: 'ignore' });

  let port;
  for (let attempt = 0; attempt < 200 && !port; attempt++) {
    try {
      port = (await fs.readFile(path.join(profile, 'DevToolsActivePort'), 'utf8')).split('\n')[0];
    } catch {}
    if (!port) await sleep(100);
  }
  assert.ok(port, 'Chromium DevTools startup');

  const tab = await (await fetch(`http://127.0.0.1:${port}/json/new?about:blank`, { method: 'PUT' })).json();
  socket = new WebSocket(tab.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {
    socket.addEventListener('open', resolve, { once: true });
    socket.addEventListener('error', reject, { once: true });
  });
  socket.addEventListener('message', (event) => {
    const message = JSON.parse(String(event.data));
    if (message.id) {
      const request = pending.get(message.id);
      pending.delete(message.id);
      if (message.error) request?.reject(new Error(message.error.message));
      else request?.resolve(message.result || {});
    } else if (message.method === 'Network.requestWillBeSent' && message.params.request.method === 'PUT') {
      writes.push(message.params.request);
    } else if (message.method === 'Runtime.exceptionThrown') {
      errors.push(message.params.exceptionDetails.text);
    }
  });

  const call = (method, params = {}) => new Promise((resolve, reject) => {
    const id = ++sequence;
    pending.set(id, { resolve, reject });
    socket.send(JSON.stringify({ id, method, params }));
  });
  const evaluate = async (expression) => {
    const response = await call('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true });
    if (response.exceptionDetails) {
      throw new Error(response.exceptionDetails.exception?.description || response.exceptionDetails.text);
    }
    return response.result?.value;
  };
  const wait = async (expression) => {
    for (let attempt = 0; attempt < 220; attempt++) {
      if (await evaluate(expression)) return;
      await sleep(75);
    }
    throw new Error(`UI condition failed: ${expression}; page=${await evaluate('document.body.innerText')}`);
  };
  const capture = async (name) => {
    const image = await call('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true });
    await fs.writeFile(path.join(evidenceDir, `${name}-${Date.now()}.png`), Buffer.from(image.data, 'base64'));
  };

  await call('Page.enable');
  await call('Runtime.enable');
  await call('Network.enable');
  await call('Network.setCookie', {
    name: 'aicrm_admin_session', value: process.env.AICRM_REFERRAL_ADMIN_BROWSER_SESSION,
    url: base, secure: true, sameSite: 'Lax',
  });
  await call('Network.setCookie', {
    name: 'aicrm_admin_csrf', value: process.env.AICRM_REFERRAL_ADMIN_BROWSER_CSRF,
    url: base, secure: true, sameSite: 'Strict',
  });
  await call('Emulation.setDeviceMetricsOverride', { width: 1365, height: 900, deviceScaleFactor: 1, mobile: false });

  const settingsURL = `${base}/admin/referral/settings?campaign=${campaign}`;
  await call('Page.navigate', { url: settingsURL });
  await wait("document.querySelector('[data-testid=referral-admin-settings-page]')");
  await wait(`document.querySelector('[data-testid=referral-compatible-product-select] select')?.options.length === 3 && document.querySelector('[data-testid=referral-compatible-product-remove-${savedProduct}]')`);
  await wait(`document.querySelector('[data-testid=referral-compatible-products]')?.textContent.includes('小店兼容课一')`);
  assert.equal(await evaluate("document.querySelector('[data-testid=referral-compatible-product-select] select')?.disabled"), false,
    'active campaigns keep compatibility selection editable');

  await evaluate(`document.querySelector('[data-testid=referral-compatible-product-remove-${savedProduct}]').click()`);
  await wait(`!document.querySelector('[data-testid=referral-compatible-product-remove-${savedProduct}]')`);
  await wait("document.querySelector('[data-testid=referral-compatible-product-select] select')?.options.length === 3");
  await evaluate(`(()=>{const s=document.querySelector('[data-testid=referral-compatible-product-select] select');s.value='${addProduct}';s.dispatchEvent(new Event('change',{bubbles:true}));})()`);
  await wait(`document.querySelector('[data-testid=referral-compatible-product-remove-${addProduct}]')`);
  await capture('compatible-products-active-editor');
  await evaluate("document.querySelector('[data-testid=referral-admin-save-campaign]').click()");
  await wait("location.pathname === '/admin/referral' && document.body.innerText.includes('活动配置已保存。')");

  const uiWrite = writes.find((request) => request.url.endsWith(`/api/admin/referral/campaigns/${campaign}`));
  assert.ok(uiWrite?.postData, 'the browser sent an actual campaign update');
  assert.deepEqual(JSON.parse(uiWrite.postData).compatible_product_ids, [addProduct],
    'started campaign update saves its selected WeChat Shop product');

  await call('Page.navigate', { url: settingsURL });
  await wait(`document.querySelector('[data-testid=referral-compatible-product-remove-${addProduct}]')`);
  await wait("document.querySelector('[data-testid=referral-compatible-products]')?.textContent.includes('小店兼容课二')");
  await capture('compatible-products-refreshed');

  const raced = await evaluate(`(async()=>{
    const path='/api/admin/referral/campaigns/${campaign}';
    const current=await fetch(path,{credentials:'same-origin',cache:'no-store'}).then(r=>r.json());
    const body={expected_version:current.version,name:current.name,cover_url:current.cover_url,
      description:(current.description||'')+'并发更新',reward_rules:current.reward_rules,
      starts_at:current.starts_at,ends_at:current.ends_at,team_mode:current.team_mode,
      qualification_mode:current.qualification_mode,product_id:current.product_id,
      product_type:current.product_type,leaderboard_metric:current.leaderboard_metric,
      compatible_product_ids:current.compatible_product_ids};
    const csrf=document.cookie.split(';').map(x=>x.trim()).find(x=>x.startsWith('aicrm_admin_csrf='))?.split('=').slice(1).join('=')||'';
    const response=await fetch(path,{method:'PUT',credentials:'same-origin',headers:{'Content-Type':'application/json',
      'X-CSRF-Token':decodeURIComponent(csrf),'Idempotency-Key':'referral-compatible-browser-race-0001'},body:JSON.stringify(body)});
    return {status:response.status,body:await response.json()};
  })()`);
  assert.equal(raced.status, 200, `concurrent admin update failed: ${JSON.stringify(raced.body)}`);
  await evaluate("document.querySelector('[data-testid=referral-admin-save-campaign]').click()");
  await wait("document.querySelector('[data-testid=referral-admin-form-message]')?.textContent.includes('记录已变化，请重新读取后再处理。')");
  await capture('compatible-products-version-conflict');
  assert.deepEqual(errors, [], `browser JavaScript exceptions: ${errors.join('; ')}`);
  console.log(`referral_compatible_products_chromium: PASS real admin HTTP save/reload, active edit and visible stale-version conflict; screenshots=${evidenceDir}`);
} finally {
  try { socket?.close(); } catch {}
  if (chrome) {
    chrome.kill('SIGTERM');
    await Promise.race([new Promise((resolve) => chrome.once('exit', resolve)), sleep(3000)]);
    if (chrome.exitCode === null) chrome.kill('SIGKILL');
  }
  await fs.rm(profile, { recursive: true, force: true });
}
