import { apiRequestOptions } from '../src/api/transport';
import { downloadQr } from '../src/admin/sections/qr';
import { openDetailDrawer } from './shared/ui/detailDrawer';
import { openShareQrDialog } from './shared/ui/shareQrDialog';
import { copyPlainText } from './shared/ui/plainTextClipboard';

export type ProductSalesSourceContext = {
  productID: number;
  productKind: 'wechat_pay' | 'service_period';
  productName: string;
  productCode: string;
};

type SalesSource = {
  id: number;
  product_id: number;
  name: string;
  version: number;
  public_path: string;
};

type SalesSourceCollection = { items: SalesSource[]; standardPath: string };
type PendingMutation = {
  method: 'POST' | 'PUT';
  path: string;
  body: string;
  key: string;
  name: string;
  sourceID?: number;
};
type ReadbackExpectation = { sourceID?: number; name: string; version?: number; message: string };

const sourceActionContexts = new WeakMap<HTMLButtonElement, ProductSalesSourceContext>();
const object = (value: unknown): Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {};

function sourceEndpoint(context: ProductSalesSourceContext): string {
  const base = context.productKind === 'wechat_pay' ? '/api/admin/wechat-pay/products' : '/api/admin/service-period-products';
  return `${base}/${context.productID}/sales-sources`;
}

function validProductContext(context: ProductSalesSourceContext): boolean {
  return Number.isSafeInteger(context.productID) && context.productID > 0 &&
    (context.productKind === 'wechat_pay' || context.productKind === 'service_period');
}

function publicURL(path: unknown, productKind: ProductSalesSourceContext['productKind']): URL {
  if (typeof path !== 'string' || !path.trim()) throw new Error('渠道公开地址无效');
  let url: URL;
  try { url = new URL(path, location.origin); } catch { throw new Error('渠道公开地址无效'); }
  const prefix = productKind === 'wechat_pay' ? '/p/' : '/s/';
  const segment = url.pathname.startsWith(prefix) ? url.pathname.slice(prefix.length) : '';
  let decodedSegment = '';
  try { decodedSegment = decodeURIComponent(segment); } catch { /* rejected below */ }
  if (url.origin !== location.origin || url.username || url.password || !segment || segment.includes('/') ||
    !decodedSegment || decodedSegment.includes('/') || url.hash) throw new Error('渠道公开地址不属于当前商品入口');
  return url;
}

function sourceItem(value: unknown, productID: number, productKind: ProductSalesSourceContext['productKind'], standardPath: URL): SalesSource {
  const item = object(value);
  const id = Number(item.id);
  const itemProductID = Number(item.product_id);
  const version = Number(item.version);
  const name = typeof item.name === 'string' ? item.name.trim() : '';
  if (!Number.isSafeInteger(id) || id < 1 || itemProductID !== productID || !name || !Number.isSafeInteger(version) || version < 1) {
    throw new Error('渠道响应缺少有效商品、名称或版本');
  }
  const url = publicURL(item.public_path, productKind);
  const sourceIDs = url.searchParams.getAll('sales_source');
  if (url.pathname !== standardPath.pathname || url.searchParams.size !== 1 || sourceIDs.length !== 1 || sourceIDs[0] !== String(id)) {
    throw new Error('渠道公开地址与来源 ID 不匹配');
  }
  return { id, product_id: itemProductID, name, version, public_path: url.toString() };
}

async function responseJSON(response: Response, action: string): Promise<unknown> {
  const payload = await response.json().catch(() => null);
  if (!response.ok) {
    if (response.status === 401 || response.status === 403) throw new Error(`当前账号无权${action}`);
    if (response.status === 409) throw new Error('渠道已更新，请重新读取后再操作');
    throw new Error(`${action}失败（HTTP ${response.status}）`);
  }
  if (payload === null) throw new Error(`${action}响应无效`);
  return payload;
}

async function readSalesSources(context: ProductSalesSourceContext): Promise<SalesSourceCollection> {
  const response = await fetch(sourceEndpoint(context), {
    method: 'GET', credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json' },
  });
  const payload = object(await responseJSON(response, '读取商品渠道'));
  if (!Array.isArray(payload.items)) throw new Error('商品渠道列表响应无效');
  const standardURL = publicURL(payload.standard_path, context.productKind);
  if (standardURL.search || standardURL.hash) throw new Error('标准渠道地址不应包含查询参数');
  const seen = new Set<number>();
  const items = payload.items.map((raw) => {
    const item = sourceItem(raw, context.productID, context.productKind, standardURL);
    if (seen.has(item.id)) throw new Error('商品渠道列表包含重复 ID');
    seen.add(item.id);
    return item;
  });
  return { items, standardPath: standardURL.toString() };
}

function safeFilename(value: string): string {
  const clean = value.trim().replace(/[^\p{L}\p{N}._-]+/gu, '-').replace(/^-+|-+$/g, '');
  return clean || 'product-sales-source';
}

function idempotencyKey(): string {
  const suffix = globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `product-sales-source-${suffix}`;
}

function markAction(button: HTMLButtonElement): void {
  (button as HTMLButtonElement & { __dcBound?: boolean }).__dcBound = true;
  button.dataset.capabilityState = 'real';
}

function uiButton(label: string): HTMLButtonElement {
  const button = document.createElement('button');
  button.type = 'button';
  button.textContent = label;
  button.style.cssText = 'min-height:32px;padding:0 11px;border:1px solid #DEE0E3;border-radius:6px;background:#fff;color:#344054;font-size:13px;cursor:pointer;white-space:nowrap';
  markAction(button);
  return button;
}

function uiInput(value = ''): HTMLInputElement {
  const input = document.createElement('input');
  input.type = 'text';
  input.value = value;
  input.style.cssText = 'min-height:34px;min-width:0;border:1px solid #DEE0E3;border-radius:6px;padding:0 10px;font-size:13px;box-sizing:border-box';
  return input;
}

function showActionMessage(message: string, success = false): void {
  document.getElementById('product-sales-sources-toast')?.remove();
  const toast = document.createElement('div');
  toast.id = 'product-sales-sources-toast';
  toast.setAttribute('role', success ? 'status' : 'alert');
  toast.textContent = message;
  toast.style.cssText = `position:fixed;right:24px;bottom:24px;z-index:10002;padding:12px 16px;border-radius:8px;background:${success ? '#16803C' : '#D83931'};color:#fff;font-size:13px;box-shadow:0 8px 28px rgba(0,0,0,.18)`;
  document.body.appendChild(toast);
  window.setTimeout(() => toast.remove(), 5000);
}

function salesSourceOrderURL(context: ProductSalesSourceContext, source: 'standard' | number): string {
  const url = new URL('/admin/orders', location.origin);
  url.searchParams.set('product_id', String(context.productID));
  url.searchParams.set('sales_source', String(source));
  return `${url.pathname}${url.search}`;
}

function showSourceQr(context: ProductSalesSourceContext, title: string, path: string, sourceName: string): void {
  const url = publicURL(path, context.productKind).toString();
  openShareQrDialog({
    title: `渠道二维码 · ${sourceName}`,
    url,
    qrLabel: `${sourceName}渠道`,
    actions: [
      { label: '复制链接', onClick: async () => { showActionMessage(await copyPlainText(url) ? '渠道链接已复制' : '复制失败，请检查浏览器权限'); } },
      { label: '预览', onClick: () => { window.open(url, '_blank', 'noopener,noreferrer'); } },
      { label: '保存二维码', onClick: () => downloadQr(url, `${safeFilename(context.productCode || String(context.productID))}-${safeFilename(title)}.svg`) },
    ],
  });
}

function openProductSalesSources(context: ProductSalesSourceContext, trigger: HTMLButtonElement): void {
  if (!validProductContext(context) || !trigger.isConnected) {
    showActionMessage('商品操作上下文已失效，请刷新列表后重试');
    return;
  }
  document.querySelector<HTMLDialogElement>('dialog[data-product-sales-sources-drawer]')?.close();
  const body = document.createElement('section');
  body.dataset.productSalesSourcesBody = '';
  body.style.cssText = 'display:grid;gap:14px;padding:4px 0';
  const dialog = openDetailDrawer(`分渠道售卖 · ${context.productName || '商品'}`, body);
  dialog.dataset.productSalesSourcesDrawer = '';

  let collection: SalesSourceCollection | undefined;
  let loading = true;
  let loadError = '';
  let notice = '';
  let createName = '';
  let editingID: number | undefined;
  let editName = '';
  let pending: PendingMutation | undefined;
  let readback: ReadbackExpectation | undefined;

  const render = (): void => {
    body.replaceChildren();
    const intro = document.createElement('p');
    intro.textContent = '标准渠道使用商品原始入口；新增渠道会生成独立的售卖来源链接。订单来源按下单入口记录。';
    intro.style.cssText = 'margin:0;color:#646A73;font-size:13px;line-height:1.6';
    body.appendChild(intro);

    if (loading) {
      const status = document.createElement('p'); status.setAttribute('role', 'status'); status.textContent = '正在读取商品渠道…';
      status.style.cssText = 'margin:0;padding:16px;color:#8F959E'; body.appendChild(status); return;
    }
    if (loadError) {
      const error = document.createElement('p'); error.setAttribute('role', 'alert'); error.textContent = loadError;
      error.style.cssText = 'margin:0;color:#D83931;font-size:13px'; body.appendChild(error);
      if (pending && !readback && collection) {
        const retry = uiButton('重试原请求');
        retry.addEventListener('click', () => {
          const source = pending?.sourceID ? collection?.items.find((candidate) => candidate.id === pending?.sourceID) : undefined;
          if (pending?.method === 'POST') void mutate('POST', undefined, pending.name);
          else if (pending?.method === 'PUT' && source) void mutate('PUT', source, pending.name);
        });
        body.appendChild(retry);
      }
      const reread = uiButton(readback ? '重新读取确认' : '重新读取'); reread.addEventListener('click', () => { void refresh(); });
      body.appendChild(reread); return;
    }
    if (notice) {
      const status = document.createElement('p'); status.setAttribute('role', readback ? 'alert' : 'status'); status.textContent = notice;
      status.style.cssText = `margin:0;color:${readback ? '#D83931' : '#16803C'};font-size:13px`;
      body.appendChild(status);
    }
    if (!collection) return;

    const mutationLocked = Boolean(pending || readback);
    const section = document.createElement('div');
    section.style.cssText = 'display:grid;gap:10px';
    const standard = sourceCard('标准渠道', '商品原始售卖入口', collection.standardPath, 'standard', context, mutationLocked);
    section.appendChild(standard);
    for (const item of collection.items) {
      section.appendChild(sourceCard(item.name, `渠道 ID ${item.id} · 版本 ${item.version}`, item.public_path, item.id, context, mutationLocked, item));
    }
    if (collection.items.length === 0) {
      const empty = document.createElement('p'); empty.textContent = '还没有命名渠道。';
      empty.style.cssText = 'margin:0;padding:14px;border:1px dashed #D7DBE0;border-radius:8px;color:#8F959E;font-size:13px';
      section.appendChild(empty);
    }
    body.appendChild(section);

    const form = document.createElement('form');
    form.style.cssText = 'display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px;padding-top:14px;border-top:1px solid #EFF0F1';
    const label = document.createElement('label'); label.style.cssText = 'display:grid;gap:6px;min-width:0';
    const labelText = document.createElement('span'); labelText.textContent = '新增渠道名称'; labelText.style.cssText = 'font-size:12px;color:#646A73';
    const nameInput = uiInput(pending?.method === 'POST' ? pending.name : createName);
    nameInput.setAttribute('aria-label', '新增渠道名称');
    nameInput.placeholder = '输入渠道名称';
    nameInput.disabled = mutationLocked && pending?.method !== 'POST';
    nameInput.readOnly = pending?.method === 'POST';
    nameInput.addEventListener('input', () => { createName = nameInput.value; });
    label.append(labelText, nameInput);
    const submit = uiButton(pending?.method === 'POST' ? '重试创建' : '创建渠道');
    submit.type = 'submit';
    submit.disabled = mutationLocked && pending?.method !== 'POST';
    submit.classList.add('shared-qr-dialog__action--primary');
    form.append(label, submit);
    form.addEventListener('submit', (event) => {
      event.preventDefault();
      const name = pending?.method === 'POST' ? pending.name : nameInput.value.trim();
      if (!name) { nameInput.focus(); showActionMessage('请输入渠道名称'); return; }
      void mutate('POST', undefined, name);
    });
    body.appendChild(form);

    if (readback) {
      const reread = uiButton('重新读取确认');
      reread.addEventListener('click', () => { void refresh(); });
      body.appendChild(reread);
    }
  };

  function sourceCard(title: string, description: string, path: string, source: 'standard' | number, owner: ProductSalesSourceContext, disabled: boolean, item?: SalesSource): HTMLElement {
    const card = document.createElement('article');
    card.style.cssText = 'display:grid;grid-template-columns:minmax(0,1fr) auto;gap:12px;align-items:center;padding:12px;border:1px solid #EFF0F1;border-radius:8px;background:#fff';
    const details = document.createElement('div'); details.style.cssText = 'min-width:0;display:grid;gap:4px';
    const name = document.createElement('strong'); name.textContent = title; name.style.cssText = 'font-size:13px;color:#1F2329';
    const hint = document.createElement('span'); hint.textContent = description; hint.style.cssText = 'font-size:12px;color:#8F959E';
    const url = document.createElement('code'); url.textContent = path; url.style.cssText = 'overflow-wrap:anywhere;color:#475467;font-size:12px';
    details.append(name, hint, url);
    const actions = document.createElement('div'); actions.style.cssText = 'display:flex;flex-wrap:wrap;justify-content:flex-end;gap:6px';
    const share = uiButton('二维码'); share.disabled = disabled;
    share.addEventListener('click', () => showSourceQr(owner, title, path, title));
    const orders = document.createElement('a'); orders.textContent = '查看订单'; orders.href = salesSourceOrderURL(owner, source);
    orders.style.cssText = 'display:inline-flex;align-items:center;min-height:30px;padding:0 10px;border:1px solid #DEE0E3;border-radius:6px;color:#3370FF;font-size:13px;text-decoration:none;white-space:nowrap';
    if (item) {
      if (editingID === item.id) {
        const input = uiInput(pending?.sourceID === item.id ? pending.name : editName || item.name);
        input.setAttribute('aria-label', `${item.name}渠道名称`); input.readOnly = pending?.sourceID === item.id; input.disabled = disabled && pending?.sourceID !== item.id;
        input.addEventListener('input', () => { editName = input.value; });
        const save = uiButton(pending?.sourceID === item.id ? '重试保存' : '保存改名');
        save.disabled = disabled && pending?.sourceID !== item.id;
        save.addEventListener('click', () => {
          const nextName = pending?.sourceID === item.id ? pending.name : input.value.trim();
          if (!nextName) { input.focus(); showActionMessage('请输入渠道名称'); return; }
          void mutate('PUT', item, nextName);
        });
        const cancel = uiButton('取消'); cancel.disabled = pending?.sourceID === item.id;
        cancel.addEventListener('click', () => { editingID = undefined; editName = ''; render(); });
        actions.append(input, save, cancel);
      } else {
        const rename = uiButton('改名'); rename.disabled = disabled;
        rename.addEventListener('click', () => { editingID = item.id; editName = item.name; render(); });
        actions.append(share, orders, rename);
      }
    } else actions.append(share, orders);
    card.append(details, actions);
    return card;
  }

  async function refresh(): Promise<void> {
    loading = true; loadError = ''; notice = ''; render();
    try {
      collection = await readSalesSources(context);
      loading = false;
      if (readback) {
        const item = readback.sourceID ? collection.items.find((candidate) => candidate.id === readback?.sourceID) : undefined;
        const confirmed = Boolean(item && item.name === readback.name && (readback.version === undefined || item.version === readback.version));
        if (confirmed) {
          notice = readback.message;
          readback = undefined;
          editingID = undefined;
          editName = '';
          createName = '';
        } else {
          notice = '已重新读取渠道列表，但未能确认刚才的改动；请核对当前列表后再继续。';
        }
      }
      render();
    } catch (error) {
      loading = false;
      loadError = error instanceof Error ? error.message : '商品渠道读取失败';
      if (readback) { loadError = '保存请求已提交，但重新读取失败；请再次读取确认。'; }
      render();
    }
  }

  async function mutate(method: 'POST' | 'PUT', item: SalesSource | undefined, name: string): Promise<void> {
    if (!collection || loading || readback) return;
    const endpoint = sourceEndpoint(context);
    const requestPath = method === 'PUT' && item ? `${endpoint}/${item.id}` : endpoint;
    const bodyObject = method === 'POST' ? { name } : { name, expected_version: item?.version };
    const body = JSON.stringify(bodyObject);
    const matchingPending = pending && pending.method === method && pending.path === requestPath && pending.body === body;
    if (pending && !matchingPending) {
      notice = '上一条提交结果尚未确认，请重试原请求。'; render(); return;
    }
    const attempt: PendingMutation = matchingPending && pending ? pending : {
      method, path: requestPath, body, key: idempotencyKey(), name, ...(item ? { sourceID: item.id } : {}),
    };
    pending = attempt;
    loadError = ''; notice = ''; render();
    try {
      const response = await fetch(requestPath, apiRequestOptions({
        method,
        credentials: 'same-origin',
        headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'Idempotency-Key': attempt.key },
        body,
      }));
      const raw = await response.json().catch(() => null);
      if (!response.ok) {
        pending = response.status >= 500 ? attempt : undefined;
        loadError = response.status >= 500 ? '提交结果暂时无法确认；重试会复用原请求。' : response.status === 409 ? '渠道已更新，请重新读取后再操作。' : response.status === 401 || response.status === 403 ? '当前账号无权管理商品渠道。' : `商品渠道保存失败（HTTP ${response.status}）`;
        render();
        if (response.status === 409) {
          notice = loadError; loadError = '';
          await refresh();
          notice = '渠道已更新，列表已重新读取；请核对后再操作。'; render();
        }
        return;
      }
      pending = undefined;
      const mutationItem = object(raw);
      const resultID = Number(mutationItem.id);
      const resultVersion = Number(mutationItem.version);
      const responseMatches = Number(mutationItem.product_id) === context.productID &&
        Number.isSafeInteger(resultID) && resultID > 0 && typeof mutationItem.name === 'string' && mutationItem.name.trim() === name &&
        Number.isSafeInteger(resultVersion) && resultVersion > 0;
      readback = {
        ...(responseMatches ? { sourceID: resultID, version: resultVersion } : item ? { sourceID: item.id } : {}),
        name,
        message: method === 'POST' ? '渠道已创建并重新读取' : '渠道名称已更新并重新读取',
      };
      if (method === 'POST') createName = name;
      else { editingID = item?.id; editName = name; }
      await refresh();
    } catch {
      pending = attempt;
      loadError = '提交结果暂时无法确认；重试会复用原请求。';
      render();
    }
  }

  void refresh();
}

export function mountProductSalesSourcesAction(container: HTMLElement, context: ProductSalesSourceContext): void {
  const menuOwner = container.dataset.tableActionMenuOwner;
  const actionHost = menuOwner
    ? document.querySelector<HTMLElement>(`[data-table-action-menu-panel="${menuOwner}"]`) || container
    : container;
  let button = actionHost.querySelector<HTMLButtonElement>(':scope > [data-product-sales-sources-action]');
  if (!button) {
    button = document.createElement('button');
    button.type = 'button';
    button.dataset.productSalesSourcesAction = '';
    button.textContent = '分渠道售卖';
    button.style.cssText = 'height:26px;padding:0 9px;border:1px solid #DEE0E3;border-radius:5px;background:#fff;color:#344054;font-size:12px;cursor:pointer;white-space:nowrap';
    markAction(button);
    const action = button;
    button.addEventListener('click', (event) => {
      event.preventDefault();
      event.stopPropagation();
      const current = sourceActionContexts.get(action);
      if (current) openProductSalesSources(current, action);
    });
    actionHost.appendChild(button);
  }
  sourceActionContexts.set(button, { ...context });
}
