import assert from 'node:assert/strict';
import { webcrypto } from 'node:crypto';
import { build } from 'esbuild';
import { JSDOM, VirtualConsole } from 'jsdom';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const bundle = await build({
  stdin: {
    contents: "import { mountProductSalesSourcesAction } from './web/v3/productSalesSources'; window.mountProductSalesSourcesAction = mountProductSalesSourcesAction;",
    resolveDir: root,
    loader: 'ts',
  },
  plugins: [{
    name: 'product-sales-sources-test-hosts',
    setup(buildContext) {
      buildContext.onResolve({ filter: /^\.\.\/src\/api\/transport$/ }, () => ({ path: 'transport', namespace: 'sales-transport' }));
      buildContext.onLoad({ filter: /.*/, namespace: 'sales-transport' }, () => ({
        contents: `export function apiRequestOptions(init = {}) { const headers = new Headers(init.headers || {}); headers.set('X-CSRF-Token', 'csrf-fixture'); return { ...init, credentials: 'same-origin', headers }; }`,
        loader: 'ts',
      }));
      buildContext.onResolve({ filter: /^\.\.\/src\/admin\/sections\/qr$/ }, () => ({ path: 'qr-export', namespace: 'sales-qr' }));
      buildContext.onLoad({ filter: /.*/, namespace: 'sales-qr' }, () => ({
        contents: `export function downloadQr(url, filename) { window.__downloads.push({ url, filename }); }`, loader: 'ts',
      }));
      buildContext.onResolve({ filter: /^\.\/shared\/ui\/detailDrawer$/ }, () => ({ path: 'detail-drawer', namespace: 'sales-detail' }));
      buildContext.onLoad({ filter: /.*/, namespace: 'sales-detail' }, () => ({
        contents: `export function openDetailDrawer(title, body) { const dialog = document.createElement('dialog'); dialog.setAttribute('aria-label', title); dialog.append(body); dialog.showModal = () => dialog.setAttribute('open', ''); dialog.close = () => { dialog.removeAttribute('open'); dialog.dispatchEvent(new Event('close')); dialog.remove(); }; document.body.append(dialog); dialog.showModal(); return dialog; }`, loader: 'ts',
      }));
      buildContext.onResolve({ filter: /^\.\/shared\/ui\/shareQrDialog$/ }, () => ({ path: 'share-qr-dialog', namespace: 'sales-share' }));
      buildContext.onLoad({ filter: /.*/, namespace: 'sales-share' }, () => ({
        contents: `export function openShareQrDialog(options) { window.__lastShare = options; const dialog = document.createElement('dialog'); dialog.dataset.sharedQrDialog = 'true'; const input = document.createElement('input'); input.readOnly = true; input.value = options.url; dialog.append(input); for (const action of options.actions) { const button = document.createElement('button'); button.type = 'button'; button.textContent = action.label; button.addEventListener('click', () => { void action.onClick(); }); dialog.append(button); } dialog.showModal = () => dialog.setAttribute('open', ''); dialog.close = () => dialog.remove(); document.body.append(dialog); dialog.showModal(); return dialog; }`, loader: 'ts',
      }));
    },
  }],
  bundle: true,
  format: 'iife',
  platform: 'browser',
  write: false,
  logLevel: 'silent',
});

const calls = [];
const sources = new Map([[101, [{ id: 11, product_id: 101, name: '直播', version: 1, public_path: '/p/sku-101?sales_source=11' }]], [202, []]]);
let failNextPost = true;
let failNextGet = false;
const virtualConsole = new VirtualConsole();
const errors = [];
virtualConsole.on('jsdomError', (error) => errors.push(String(error.message)));
const json = (value, status = 200) => new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } });
const dom = new JSDOM('<!doctype html><body><div id="ordinary-actions"></div><div id="period-actions"></div></body>', {
  url: 'https://test.invalid/admin/wechat-pay/products', runScripts: 'outside-only', pretendToBeVisual: true, virtualConsole,
  beforeParse(window) {
    window.Request = Request; window.Response = Response; window.Headers = Headers;
    Object.defineProperty(window, 'crypto', { value: webcrypto, configurable: true });
    window.__downloads = [];
    Object.defineProperty(window.navigator, 'clipboard', { value: { writeText: async (value) => { window.__copiedText = value; } }, configurable: true });
    window.fetch = async (input, init = {}) => {
      const url = new URL(typeof input === 'string' ? input : input.url, window.location.href);
      const method = String(init.method || 'GET').toUpperCase();
      const headers = new Headers(init.headers || {});
      calls.push({ url, method, headers, body: init.body || '' });
      const match = url.pathname.match(/^\/api\/admin\/(?:wechat-pay\/products|service-period-products)\/(\d+)\/sales-sources(?:\/(\d+))?$/);
      if (!match) return json({ error: 'unexpected_path' }, 404);
      const productID = Number(match[1]);
      const productKind = url.pathname.includes('service-period-products') ? 'service_period' : 'wechat_pay';
      if (method === 'GET') {
        if (failNextGet) { failNextGet = false; return json({ error: 'temporary_failure' }, 503); }
        return json({ items: sources.get(productID) || [], standard_path: `${productKind === 'service_period' ? '/s/' : '/p/'}${productKind === 'service_period' ? 'cycle' : 'sku'}-${productID}` });
      }
      if (method === 'POST') {
        if (failNextPost) { failNextPost = false; return json({ error: 'outcome_unknown' }, 503); }
        const body = JSON.parse(init.body);
        const id = 12;
        const item = { id, product_id: productID, name: body.name, version: 1, public_path: `/p/sku-${productID}?sales_source=${id}` };
        sources.get(productID).push(item);
        return json(item, 201);
      }
      if (method === 'PUT') {
        const body = JSON.parse(init.body);
        const targetID = Number(url.pathname.split('/').at(-1));
        const source = sources.get(productID).find((candidate) => candidate.id === targetID);
        assert.equal(body.expected_version, source.version, 'rename sends the current source version');
        source.name = body.name; source.version += 1;
        return json({ ...source }, 200);
      }
      return json({ error: 'unsupported_method' }, 405);
    };
  },
});

const pause = (ms = 10) => new Promise((resolve) => setTimeout(resolve, ms));
async function waitFor(check, label) {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    const value = check();
    if (value) return value;
    await pause();
  }
  throw new Error(label);
}

try {
  dom.window.eval(bundle.outputFiles[0].text);
  const mount = dom.window.mountProductSalesSourcesAction;
  const ordinary = dom.window.document.getElementById('ordinary-actions');
  mount(ordinary, { productID: 101, productKind: 'wechat_pay', productName: '年度课程', productCode: 'sku-101' });
  const action = ordinary.querySelector('[data-product-sales-sources-action]');
  assert.equal(action.textContent, '分渠道售卖');
  action.click();
  await waitFor(() => dom.window.document.querySelector('[data-product-sales-sources-drawer]')?.textContent.includes('直播'), 'ordinary source drawer did not load');

  const drawer = dom.window.document.querySelector('[data-product-sales-sources-drawer]');
  const orderLinks = [...drawer.querySelectorAll('a')].map((link) => new URL(link.href));
  assert.equal(orderLinks.length, 2, 'standard and named sources each link to the existing order list');
  assert.equal(orderLinks[0].pathname, '/admin/orders');
  assert.equal(orderLinks[0].searchParams.get('product_id'), '101');
  assert.equal(orderLinks[0].searchParams.get('product'), null, 'historical order lookup must not depend on a mutable product name or code');
  assert.equal(orderLinks[0].searchParams.get('sales_source'), 'standard');
  assert.equal(orderLinks[1].searchParams.get('sales_source'), '11');

  const standardCard = [...drawer.querySelectorAll('article')].find((card) => card.textContent.includes('标准渠道'));
  standardCard.querySelector('button').click();
  const qrDialog = await waitFor(() => dom.window.document.querySelector('dialog[data-shared-qr-dialog="true"]'), 'standard QR dialog did not open');
  assert.equal(qrDialog.querySelector('input').value, 'https://test.invalid/p/sku-101');
  qrDialog.querySelector('button').click();
  await waitFor(() => dom.window.__copiedText, 'copy action did not copy the approved source URL');
  assert.equal(dom.window.__copiedText, 'https://test.invalid/p/sku-101');
  [...qrDialog.querySelectorAll('button')].find((button) => button.textContent === '保存二维码').click();
  assert.equal(dom.window.__downloads[0].url, 'https://test.invalid/p/sku-101');
  assert.match(dom.window.__downloads[0].filename, /sku-101/);
  qrDialog.close();

  const createInput = drawer.querySelector('input[aria-label="新增渠道名称"]');
  createInput.value = '活动渠道'; createInput.dispatchEvent(new dom.window.Event('input', { bubbles: true }));
  drawer.querySelector('form button').click();
  await waitFor(() => [...dom.window.document.querySelectorAll('[role="alert"]')].some((item) => item.textContent.includes('重试会复用原请求')), 'unknown create outcome was not surfaced');
  const firstPost = calls.find((call) => call.method === 'POST');
  assert.equal(firstPost.headers.get('X-CSRF-Token'), 'csrf-fixture', 'mutations use the existing CSRF request options');
  assert.ok(firstPost.headers.get('Idempotency-Key'), 'mutation includes the required idempotency key');
  drawer.querySelector('button').click();
  await waitFor(() => drawer.textContent.includes('活动渠道') && drawer.textContent.includes('重新读取'), 'retry did not resolve and reread the created source');
  const posts = calls.filter((call) => call.method === 'POST');
  assert.equal(posts.length, 2, 'unknown create outcome retries the original request');
  assert.equal(posts[0].headers.get('Idempotency-Key'), posts[1].headers.get('Idempotency-Key'), 'retry reuses the original idempotency key');
  assert.deepEqual(JSON.parse(posts[0].body), { name: '活动渠道' });
  assert.ok(calls.filter((call) => call.method === 'GET').length >= 2, 'source list is reread after creation');

  const createdCard = [...drawer.querySelectorAll('article')].find((card) => card.textContent.includes('活动渠道'));
  createdCard.querySelector('button:last-child').click();
  const editingCard = [...drawer.querySelectorAll('article')].find((card) => card.querySelector('input[aria-label="活动渠道渠道名称"]'));
  const editInput = editingCard.querySelector('input');
  editInput.value = '活动渠道改名'; editInput.dispatchEvent(new dom.window.Event('input', { bubbles: true }));
  [...editingCard.querySelectorAll('button')].find((button) => button.textContent === '保存改名').click();
  await waitFor(() => drawer.textContent.includes('活动渠道改名') && drawer.textContent.includes('版本 2'), 'rename did not reread the updated source');
  const put = calls.find((call) => call.method === 'PUT');
  assert.equal(put.headers.get('X-CSRF-Token'), 'csrf-fixture');
  assert.ok(put.headers.get('Idempotency-Key'));
  assert.deepEqual(JSON.parse(put.body), { name: '活动渠道改名', expected_version: 1 });

  drawer.close();
  failNextGet = true;
  action.click();
  await waitFor(() => [...dom.window.document.querySelectorAll('[role="alert"]')].some((item) => item.textContent.includes('HTTP 503')), 'read failure did not show an error');
  dom.window.document.querySelector('[data-product-sales-sources-drawer] button').click();
  await waitFor(() => dom.window.document.querySelector('[data-product-sales-sources-drawer]')?.textContent.includes('活动渠道改名'), 'manual reread did not recover the drawer');

  const periodic = dom.window.document.getElementById('period-actions');
  mount(periodic, { productID: 202, productKind: 'service_period', productName: '周期会员', productCode: 'cycle-202' });
  periodic.querySelector('[data-product-sales-sources-action]').click();
  const periodicDrawer = await waitFor(() => {
    const node = dom.window.document.querySelector('[data-product-sales-sources-drawer]');
    return node?.querySelector('article a') ? node : undefined;
  }, 'periodic source drawer did not load');
  assert.ok(calls.some((call) => call.url.pathname === '/api/admin/service-period-products/202/sales-sources' && call.method === 'GET'), 'periodic products use their own source endpoint');
  const periodicOrderLink = periodicDrawer.querySelector('a');
  assert.equal(new URL(periodicOrderLink.href).searchParams.get('product_id'), '202');
  assert.equal(new URL(periodicOrderLink.href).searchParams.get('product'), null, 'periodic historical order lookup also avoids mutable product-code matching');
  assert.equal(new URL(periodicOrderLink.href).searchParams.get('sales_source'), 'standard');
  assert.deepEqual(errors, [], 'product source management should not throw browser errors');
} finally {
  dom.window.close();
}

console.log('product sales-source drawer create, rename, refetch, error, QR, and order-link journey: PASS');
