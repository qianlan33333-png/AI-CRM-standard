import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';
import { webcrypto } from 'node:crypto';
import path from 'node:path';
import { build } from 'esbuild';
import { JSDOM, VirtualConsole } from 'jsdom';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const bundle = await build({
  stdin: {
    contents: "import './web/v3/orderAdapter'; import {AdminController} from './web/src/admin/controller'; import {api} from './web/src/shared/api/client'; window.OrderControllerFixture = AdminController; window.OrderAdapterApi = api;",
    resolveDir: root,
    loader: 'ts',
  },
  plugins: [{
    name: 'order-sales-source-test-fixtures',
    setup(buildContext) {
      const fixtureModules = new Map([
        ['../src/admin/controller', `export class AdminController {
          constructor(api = {mode:'http'}, page = 'orders') { this.api = api; this.page = page; this.state = { orderFilters: {} }; this.db = { rows: { orders: [] }, orderList: { total: 0, hasMore: false } }; }
          renderVals() { return { rows: { orders: this.db.rows.orders }, orderPage: { filters: this.state.orderFilters, summary: '订单' } }; }
        }`],
        ['../src/api/transport', `export function apiRequestOptions(options = {}) { const headers = new Headers(options.headers || {}); headers.set('X-CSRF-Token', 'csrf-fixture'); return { ...options, credentials: 'same-origin', headers }; }`],
        ['../src/shared/api/client', `export const api = {
          mode: 'http',
          async loadDb(context) {
            if (context?.page !== 'orders') return { rows: { orders: [] } };
            const response = await fetch('/api/admin/orders?limit=50&offset=0');
            const payload = await response.json();
            return { rows: { orders: (payload.items || []).map((item) => ({ no: item.merchant_order_no, payer: item.payer_name || '买家', uid: item.payer_id || '', product: item.product_name || '商品', time: item.created_at || '2026-10-15', amount: '1.00', status: 'paid', pay: item.provider_label || '微信支付', tone: 'ok' })) }, orderList: { total: (payload.items || []).length, hasMore: false } };
          }
        };`],
        ['../src/admin/main', ''],
      ]);
      buildContext.onResolve({ filter: /.*/ }, (args) => {
        if (args.path === './web/src/admin/controller') return { path: '../src/admin/controller', namespace: 'order-sales-source-fixtures' };
        if (args.path === './web/src/shared/api/client') return { path: '../src/shared/api/client', namespace: 'order-sales-source-fixtures' };
        if (fixtureModules.has(args.path)) return { path: args.path, namespace: 'order-sales-source-fixtures' };
        return undefined;
      });
      buildContext.onLoad({ filter: /.*/, namespace: 'order-sales-source-fixtures' }, (args) => ({ contents: fixtureModules.get(args.path) || '', loader: 'ts' }));
    },
  }],
  bundle: true,
  format: 'iife',
  platform: 'browser',
  write: false,
  logLevel: 'silent',
});

const pause = (milliseconds = 15) => new Promise((resolve) => setTimeout(resolve, milliseconds));
async function waitFor(check, message) {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    const value = check();
    if (value) return value;
    await pause();
  }
  throw new Error(message);
}

const listRecords = [
  { id: 501, merchant_order_no: 'order-standard', detail_url: '/admin/orderDetail.html?id=order-standard&provider=no_payment', provider: 'no_payment', provider_label: '无需支付', currency: 'CNY', product_name: '商品甲', sales_source: { kind: 'standard' } },
  { id: 502, merchant_order_no: 'order-channel', detail_url: '/admin/orderDetail.html?id=order-channel&provider=wechat', provider: 'wechat', provider_label: '微信支付', currency: 'CNY', product_name: '商品乙', sales_source: { kind: 'channel', id: 23, name: '直播' } },
  { id: 503, merchant_order_no: 'order-unrecorded', detail_url: '/admin/orderDetail.html?id=order-unrecorded&provider=wechat', provider: 'wechat', provider_label: '微信支付', currency: 'CNY', product_name: '商品丙', sales_source: { kind: 'unrecorded' } },
];
const calls = [];
const exportBodies = [];
const virtualConsole = new VirtualConsole();
const errors = [];
virtualConsole.on('jsdomError', (error) => errors.push({ message: error.message, cause: error.cause?.stack || error.detail?.stack }));
const response = (body, status = 200, headers = { 'Content-Type': 'application/json' }) => new Response(typeof body === 'string' ? body : JSON.stringify(body), { status, headers });
const listRows = listRecords.map((item) => `<tr><td>2026-10-15</td><td><div>${item.merchant_order_no}</div></td><td><div>买家</div><div>customer:1</div></td><td>${item.product_name}</td><td>1.00</td><td><span>paid</span></td><td>${item.provider_label}</td><td><a href="#">查看详情</a></td></tr>`).join('');
const listDom = new JSDOM(`<!doctype html><body data-page="orders">
  <div>
    <label><span>单号</span><input id="orderTransactionId"></label>
    <label><span>商品</span><input id="orderProductCode"></label>
    <label><span>手机号</span><input id="orderMobile"></label>
    <button type="button">查询</button><button type="button">清空</button><button type="button">导出微信支付 CSV</button>
  </div>
  <table><thead><tr><th>创建时间</th><th>微信 / 平台单号</th><th>付款人 / 客户身份</th><th>商品</th><th>金额</th><th>状态</th><th>支付来源</th><th>操作</th></tr></thead><tbody>${listRows}</tbody></table>
</body>`, {
  url: 'https://test.invalid/admin/orders?product_id=41&sales_source=23',
  runScripts: 'outside-only',
  pretendToBeVisual: true,
  virtualConsole,
  beforeParse(window) {
    window.Request = Request; window.Response = Response; window.Headers = Headers;
    Object.defineProperty(window, 'crypto', { value: webcrypto, configurable: true });
    window.URL.createObjectURL = () => 'blob:order-export-fixture'; window.URL.revokeObjectURL = () => {};
    window.HTMLAnchorElement.prototype.click = function () { window.fixtureDownload = this.download; };
    window.fetch = async (input, init = {}) => {
      const rawURL = typeof input === 'string' ? input : input instanceof window.URL ? input.toString() : input.url;
      const url = new URL(rawURL, window.location.href);
      const method = String(init.method || (input instanceof window.Request ? input.method : 'GET')).toUpperCase();
      calls.push({ url, method, body: init.body || '' });
      if (url.pathname === '/api/admin/orders' && method === 'GET') return response({ items: listRecords });
      if (url.pathname === '/api/admin/wechat-pay/products/41/sales-sources') return response({
        items: [{ id: 23, product_id: 41, name: '直播', version: 1, public_path: '/p/sku-41?sales_source=23' }],
        standard_path: '/p/sku-41',
      });
      if (url.pathname === '/api/admin/service-period-products/41/sales-sources') return response({ error: 'not_found' }, 404);
      if (url.pathname === '/api/admin/exports/preview') return response({ order_count: 3, row_count: 3, truncated: false });
      if (url.pathname === '/api/admin/exports') {
        exportBodies.push(JSON.parse(init.body));
        return response('xlsx-fixture', 200, { 'Content-Type': 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' });
      }
      return response({ error: 'unexpected_path' }, 404);
    };
  },
});

try {
  listDom.window.eval(bundle.outputFiles[0].text);
  const document = listDom.window.document;
  const sourceFilter = document.getElementById('orderSalesSource');
  assert.ok(sourceFilter instanceof listDom.window.HTMLSelectElement, 'sales-source filter is a select for operators');
  assert.equal(sourceFilter.value, '23', 'the source filter is seeded from a deep link');
  await waitFor(() => sourceFilter.dataset.orderSalesSourceOptionsState === 'loaded', 'the product source names were not loaded');
  assert.deepEqual([...sourceFilter.options].map((option) => option.textContent), ['全部渠道', '标准渠道', '直播', '未记录']);
  const controller = new listDom.window.OrderControllerFixture({ mode: 'http' }, 'orders');
  controller.db = await listDom.window.OrderAdapterApi.loadDb({ page: 'orders' });
  controller.renderVals();
  document.querySelector('tbody tr').append(document.createElement('span'));
  await waitFor(() => document.querySelector('[data-order-sales-source-cell]')?.textContent === '标准渠道', 'the standard-source label did not render');

  const initialListRequest = calls.find((call) => call.url.pathname === '/api/admin/orders' && call.method === 'GET');
  assert.equal(initialListRequest.url.searchParams.get('product_id'), '41', 'the exact product ID survives the deep link');
  assert.equal(initialListRequest.url.searchParams.get('sales_source'), '23', 'the selected source ID reaches the list request');
  assert.equal(initialListRequest.url.searchParams.get('product'), null, 'the exact product link does not depend on the mutable product name');
  assert.equal(document.querySelector('thead [data-order-sales-source-header]')?.textContent, '售卖渠道');
  const sourceCells = [...document.querySelectorAll('[data-order-sales-source-cell]')].map((cell) => cell.textContent);
  assert.deepEqual(sourceCells, ['标准渠道', '直播', '未记录'], 'standard, named and unrecorded sales sources are visible on their matching rows');

  for (const value of ['standard', 'unrecorded', '23']) {
    sourceFilter.value = value;
    await listDom.window.fetch('/api/admin/orders?limit=50&offset=0');
    const query = calls.at(-1).url.searchParams;
    assert.equal(query.get('product_id'), '41', `the exact product ID remains with source ${value}`);
    assert.equal(query.get('sales_source'), value, `source filter ${value} reaches the list request`);
    assert.equal(query.get('product'), null, 'source filtering does not add a fuzzy product condition');
  }

  const exportButton = document.getElementById('order-export-all');
  assert.ok(exportButton, 'the existing export action is upgraded in place');
  sourceFilter.value = '23';
  exportButton.click();
  await waitFor(() => exportBodies.length === 1, 'the filtered order export was not requested');
  assert.equal(exportBodies[0].filter.product_id, '41', 'the exact product ID is preserved in export filters');
  assert.equal(exportBodies[0].filter.sales_source, '23', 'the selected source is preserved in export filters');
  assert.equal(exportBodies[0].filter.product, undefined, 'the export avoids mutable product-name matching for the deep link');
  assert.equal(listDom.window.fixtureDownload, '订单明细.xlsx');

  const invalidOption = document.createElement('option');
  invalidOption.value = 'invalid';
  invalidOption.textContent = '无效';
  sourceFilter.append(invalidOption);
  sourceFilter.value = 'invalid';
  document.querySelector('button').click();
  assert.equal(sourceFilter.getAttribute('aria-invalid'), 'true', 'invalid source filter values are blocked before query');
  assert.match(sourceFilter.validationMessage, /有效的售卖渠道/);
  document.querySelectorAll('button')[1].click();
  assert.equal(sourceFilter.value, '', 'clear empties the source filter');
  assert.deepEqual([...sourceFilter.options].map((option) => option.textContent), ['全部渠道', '标准渠道', '未记录'], 'clear removes product-specific source choices');
  const clearedURL = new URL(listDom.window.location.href);
  assert.equal(clearedURL.searchParams.has('product_id'), false, 'clear removes the deep exact product ID');
  assert.equal(clearedURL.searchParams.has('sales_source'), false, 'clear removes the deep source filter');
  await listDom.window.fetch('/api/admin/orders?sales_source=23');
  assert.equal(calls.at(-1).url.searchParams.has('sales_source'), false, 'an emptied source field removes stale URL request parameters');
  await pause(100);
  assert.deepEqual(errors, [], 'the order list sales-source journey should not throw browser errors');
} finally {
  listDom.window.close();
}

async function exerciseSourceReadFailure() {
  const failureCalls = [];
  const failureErrors = [];
  const failureConsole = new VirtualConsole();
  failureConsole.on('jsdomError', (error) => failureErrors.push(error.message));
  const failureDom = new JSDOM(`<!doctype html><body data-page="orders">
    <div><label><span>商品</span><input id="orderProductCode"></label><button type="button">查询</button><button type="button">清空</button></div>
  </body>`, {
    url: 'https://test.invalid/admin/orders?product_id=99&sales_source=91',
    runScripts: 'outside-only',
    virtualConsole: failureConsole,
    beforeParse(window) {
      window.Request = Request; window.Response = Response; window.Headers = Headers;
      window.fetch = async (input, init = {}) => {
        const rawURL = typeof input === 'string' ? input : input instanceof window.URL ? input.toString() : input.url;
        const url = new URL(rawURL, window.location.href);
        const method = String(init.method || (input instanceof window.Request ? input.method : 'GET')).toUpperCase();
        failureCalls.push({ url, method });
        if (url.pathname === '/api/admin/orders') return response({ items: [] });
        if (url.pathname === '/api/admin/wechat-pay/products/99/sales-sources') return response({ error: 'temporarily_unavailable' }, 503);
        if (url.pathname === '/api/admin/service-period-products/99/sales-sources') return response({ error: 'not_found' }, 404);
        return response({ error: 'unexpected_path' }, 404);
      };
    },
  });
  try {
    failureDom.window.eval(bundle.outputFiles[0].text);
    const select = failureDom.window.document.getElementById('orderSalesSource');
    assert.equal(select.value, '91', 'a deep-linked source stays selected before source names load');
    await waitFor(() => select.dataset.orderSalesSourceOptionsState === 'unavailable', 'a failed source read should be recorded');
    assert.equal(select.value, '91', 'a failed source read must preserve the deep-linked filter');
    assert.ok([...select.options].some((option) => option.value === '91' && option.textContent === '当前渠道（ID 91）'), 'an unknown numeric source receives a Chinese fallback label');
    await failureDom.window.fetch('/api/admin/orders?limit=50&offset=0');
    const listCall = failureCalls.find((call) => call.url.pathname === '/api/admin/orders');
    assert.equal(listCall.url.searchParams.get('product_id'), '99');
    assert.equal(listCall.url.searchParams.get('sales_source'), '91', 'the source read failure cannot silently broaden the order query');
    assert.equal(listCall.url.searchParams.get('product'), null);
    assert.deepEqual(failureCalls.filter((call) => call.url.pathname.includes('/sales-sources')).map((call) => call.url.pathname).sort(), [
      '/api/admin/service-period-products/99/sales-sources',
      '/api/admin/wechat-pay/products/99/sales-sources',
    ].sort(), 'only the two product-specific source endpoints are consulted');
    await pause(30);
    assert.deepEqual(failureErrors, []);
    failureDom.window.dispatchEvent(new failureDom.window.Event('pagehide'));
  } finally {
    failureDom.window.close();
  }
}

await exerciseSourceReadFailure();

async function exerciseClearDuringSourceRead() {
  let releaseRegularSource;
  let regularSourceRequested = false;
  const pendingRegularSource = new Promise((resolve) => { releaseRegularSource = resolve; });
  const pendingCalls = [];
  const pendingDom = new JSDOM('<!doctype html><body data-page="orders"><div><label><span>商品</span><input id="orderProductCode"></label><button type="button">查询</button><button type="button">清空</button></div></body>', {
    url: 'https://test.invalid/admin/orders?product_id=67&sales_source=81',
    runScripts: 'outside-only',
    beforeParse(window) {
      window.Request = Request; window.Response = Response; window.Headers = Headers;
      window.fetch = async (input, init = {}) => {
        const rawURL = typeof input === 'string' ? input : input instanceof window.URL ? input.toString() : input.url;
        const url = new URL(rawURL, window.location.href);
        pendingCalls.push(url);
        if (url.pathname === '/api/admin/wechat-pay/products/67/sales-sources') {
          regularSourceRequested = true;
          return pendingRegularSource;
        }
        if (url.pathname === '/api/admin/service-period-products/67/sales-sources') return response({ error: 'not_found' }, 404);
        if (url.pathname === '/api/admin/orders') return response({ items: [] });
        return response({ error: 'unexpected_path' }, 404);
      };
    },
  });
  try {
    pendingDom.window.eval(bundle.outputFiles[0].text);
    const select = pendingDom.window.document.getElementById('orderSalesSource');
    await waitFor(() => regularSourceRequested, 'the product-specific source lookup did not start');
    pendingDom.window.document.querySelectorAll('button')[1].click();
    assert.equal(select.value, '', 'clear removes the selected source before the pending lookup completes');
    assert.deepEqual([...select.options].map((option) => option.textContent), ['全部渠道', '标准渠道', '未记录']);
    releaseRegularSource(response({
      items: [{ id: 81, product_id: 67, name: '旧渠道', version: 1, public_path: '/p/sku-67?sales_source=81' }],
      standard_path: '/p/sku-67',
    }));
    await pause(50);
    assert.equal(select.value, '', 'the completed lookup cannot restore a cleared source');
    assert.deepEqual([...select.options].map((option) => option.textContent), ['全部渠道', '标准渠道', '未记录'], 'the completed lookup cannot restore stale product choices');
    const url = new URL(pendingDom.window.location.href);
    assert.equal(url.searchParams.has('product_id'), false);
    assert.equal(url.searchParams.has('sales_source'), false);
    pendingDom.window.dispatchEvent(new pendingDom.window.Event('pagehide'));
  } finally {
    pendingDom.window.close();
  }
}

await exerciseClearDuringSourceRead();

async function exerciseSourceDeepLinkWithoutProduct() {
  const noProductCalls = [];
  const noProductDom = new JSDOM('<!doctype html><body data-page="orders"><div><label><span>单号</span><input id="orderTransactionId"></label><button type="button">查询</button><button type="button">清空</button></div></body>', {
    url: 'https://test.invalid/admin/orders?sales_source=91',
    runScripts: 'outside-only',
    beforeParse(window) {
      window.Request = Request; window.Response = Response; window.Headers = Headers;
      window.fetch = async (input, init = {}) => {
        const rawURL = typeof input === 'string' ? input : input instanceof window.URL ? input.toString() : input.url;
        const url = new URL(rawURL, window.location.href);
        noProductCalls.push(url);
        if (url.pathname === '/api/admin/orders') return response({ items: [] });
        return response({ error: 'unexpected_path' }, 404);
      };
    },
  });
  try {
    noProductDom.window.eval(bundle.outputFiles[0].text);
    const select = noProductDom.window.document.getElementById('orderSalesSource');
    assert.ok([...select.options].some((option) => option.value === '91' && option.textContent === '当前渠道（ID 91）'));
    assert.equal(noProductCalls.some((url) => url.pathname.includes('/sales-sources')), false, 'without product_id there is no global source lookup');
    await noProductDom.window.fetch('/api/admin/orders?limit=50&offset=0');
    const listCall = noProductCalls.find((url) => url.pathname === '/api/admin/orders');
    assert.equal(listCall.searchParams.get('sales_source'), '91', 'a numeric source deep link still works without product_id');
    noProductDom.window.dispatchEvent(new noProductDom.window.Event('pagehide'));
  } finally {
    noProductDom.window.close();
  }
}

await exerciseSourceDeepLinkWithoutProduct();

const detailErrors = [];
const detailVirtualConsole = new VirtualConsole();
detailVirtualConsole.on('jsdomError', (error) => detailErrors.push({ message: error.message, cause: error.cause?.stack || error.detail?.stack }));
const detailDom = new JSDOM('<!doctype html><body data-page="orderDetail"><article><div><h2>订单详情</h2></div></article></body>', {
  url: 'https://test.invalid/admin/orderDetail.html?id=detail-501&provider=no_payment',
  runScripts: 'outside-only',
  pretendToBeVisual: true,
  virtualConsole: detailVirtualConsole,
  beforeParse(window) {
    window.Request = Request; window.Response = Response; window.Headers = Headers;
    window.fetch = async (input) => {
      const url = new URL(typeof input === 'string' ? input : input instanceof window.URL ? input.toString() : input.url, window.location.href);
      if (url.pathname === '/api/admin/orders/detail-501') return response({
        id: 501, merchant_order_no: 'detail-501', provider: 'no_payment', provider_label: '无需支付', currency: 'CNY',
        created_at: '2026-10-15T00:00:00Z', status: 'paid', product_name: '无需支付商品', amount_yuan: '0.00',
        sales_source: { kind: 'channel', id: 23, name: '直播' },
      });
      return response({ error: 'unexpected_path' }, 404);
    };
  },
});
try {
  detailDom.window.eval(bundle.outputFiles[0].text);
  await waitFor(() => detailDom.window.document.body.textContent.includes('直播'), 'order detail did not show its sales source');
  const sourceSection = [...detailDom.window.document.querySelectorAll('[data-order-detail-section]')]
    .find((section) => section.querySelector('h3')?.textContent === '售卖渠道');
  assert.equal(sourceSection?.textContent.includes('直播'), true, 'the order detail contains the sales-source label');
  await pause(50);
  assert.deepEqual(detailErrors, [], 'the order detail sales-source journey should not throw browser errors');
} finally {
  detailDom.window.close();
}

console.log('order sales-source Chinese filter, source lookup/fallback, display, detail, exact product link, and export journey: PASS');
