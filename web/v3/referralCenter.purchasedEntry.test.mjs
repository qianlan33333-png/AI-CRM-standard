import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';
import { buildTestBrowserBundle } from '../scripts/test-browser-bundle.mjs';

const bundle = await buildTestBrowserBundle(fileURLToPath(new URL('./referralCenter.ts', import.meta.url)));
const json = (body, status = 200) => new Response(JSON.stringify(body), { status });
const wait = async (check) => {
  for (let i = 0; i < 200; i++) {
    if (check()) return;
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
  throw new Error('purchased entry did not settle');
};

async function fixture({
  eligible = true,
  failJoin = false,
  failInvite = false,
  participation = null,
  available,
  state = 'active',
  holdJoin = false,
  unauthenticated = false,
  priorAttempt = false,
  storageDisabled = false,
} = {}) {
  const calls = [];
  let joined = participation;
  let release;
  const pending = new Promise((resolve) => { release = resolve; });
  const dom = new JSDOM('<main id="referral-root"></main>', {
    url: 'https://crm.example/referral?campaign=6&invite=rfi_' + 'B'.repeat(43),
    runScripts: 'outside-only',
    pretendToBeVisual: true,
    beforeParse(window) {
      window.Headers = Headers;
      window.Response = Response;
      window.Blob = Blob;
      window.HTMLDialogElement.prototype.showModal = function () { this.setAttribute('open', ''); };
      window.HTMLDialogElement.prototype.close = function () { this.removeAttribute('open'); this.dispatchEvent(new window.Event('close')); };
      window.FileReader = class {
        readAsDataURL(blob) { blob.arrayBuffer().then((bytes) => { this.result = `data:${blob.type};base64,${Buffer.from(bytes).toString('base64')}`; this.onload?.(); }); }
        abort() {}
      };
      window.Image = function () {
        const image = window.document.createElement('img');
        Object.defineProperties(image, { naturalWidth: { value: 1250 }, naturalHeight: { value: 2426 } });
        image.decode = async () => {};
        return image;
      };
      window.HTMLCanvasElement.prototype.getContext = function () {
        return {
          drawImage: (image) => { this.source = Buffer.from(image.src.split(',')[1], 'base64').toString(); },
          fillRect() {}, save() {}, restore() {}, measureText(text) { return { width: text.length * 30 }; }, fillText() {},
        };
      };
      window.HTMLCanvasElement.prototype.toBlob = function (callback, type) { queueMicrotask(() => callback(new Blob([this.source], { type }))); };
      if (storageDisabled) {
        Object.defineProperty(window, 'sessionStorage', {
          configurable: true,
          get() { throw new window.DOMException('storage is disabled', 'SecurityError'); },
        });
      }
      if (priorAttempt) window.sessionStorage.setItem('referral-purchased-join-attempted:6', '1');
      window.fetch = async (input, init = {}) => {
        const url = new URL(String(input), window.location.href);
        const path = url.pathname;
        calls.push({ path, method: init.method || 'GET', body: init.body, key: new Headers(init.headers).get('Idempotency-Key') });
        if (path.endsWith('/campaigns/6')) return json({
          id: 6,
          name: '6.8 公开课',
          state,
          effective_state: state,
          team_mode: 'individual',
          qualification_mode: 'product_purchase',
          product_url: '/p/31314141',
          starts_at: '2026-01-01T00:00:00Z',
          ends_at: '2099-01-01T00:00:00Z',
          posters: [{ slot: 1, description: '海报一', image_url: '/api/v1/referral/campaigns/6/posters/1', image_width: 1250, image_height: 2426 }],
        });
        if (path.endsWith('/me')) {
          if (unauthenticated) return json({ error: 'identity_required' }, 401);
          return json({ participation: joined, purchase_eligible: joined ? false : eligible, invitation_available: available ?? !!joined });
        }
        if (path.includes('/invitations/')) return json({ inviter_display_name: '后来分享活动的人' });
        if (path.endsWith('/leaderboard')) return json({ items: [] });
        if (path.endsWith('/participations')) {
          if (holdJoin) await pending;
          if (failJoin) return json({ error: 'referral_participation_required' }, 403);
          joined = { joined_at: '2026-10-15T11:00:00Z', state: 'active' };
          return json({ participation: joined, invitation_available: true });
        }
        if (path.endsWith('/invite')) return failInvite
          ? json({ error: 'unavailable' }, 503)
          : json({ url: 'https://crm.example/referral/invite/rfi_' + 'A'.repeat(43) });
        if (path.endsWith('/posters/1')) return new Response(new Blob([path], { type: 'image/jpeg' }));
        if (path.endsWith('/session/bridge')) return json({ error: 'identity_required' }, 401);
        throw new Error(`unexpected request: ${path}`);
      };
    },
  });
  dom.window.eval(bundle);
  const doc = dom.window.document;
  await wait(() => doc.querySelector('[data-testid="referral-invite"]') || doc.querySelector('a[href*="oauth/start"]'));
  return {
    dom,
    doc,
    calls,
    release,
    click: () => doc.querySelector('[data-testid="referral-invite"]')?.click(),
  };
}

{
  const f = await fixture({ holdJoin: true });
  await wait(() => f.calls.filter((call) => call.path.endsWith('/participations')).length === 1);
  const join = f.calls.find((call) => call.path.endsWith('/participations'));
  assert.equal(join.method, 'POST', 'a trusted eligible /me response automatically starts membership recovery');
  assert.deepEqual(JSON.parse(join.body), {}, 'the current invitation URL must not reattribute an earlier purchase');
  assert.ok(join.key.length >= 16, 'membership recovery uses an idempotency key');
  assert.equal(f.doc.querySelector('[data-testid="referral-invite"]').textContent, '正在确认参加…');
  assert.equal(f.doc.querySelector('[data-testid="referral-invite"]').disabled, true);
  assert.equal(f.calls.some((call) => call.path.endsWith('/invite') && call.method === 'POST'), false, 'poster generation waits for confirmed membership and a user click');
  f.release();
  await wait(() => f.calls.filter((call) => call.path.endsWith('/me')).length === 2);
  const refresh = f.calls.filter((call) => call.path.endsWith('/me'))[1];
  assert.ok(f.calls.indexOf(join) < f.calls.indexOf(refresh), 'membership is read back from /me after the POST');
  await wait(() => f.doc.querySelector('[data-testid="referral-invite"]')?.textContent === '生成溯源码');
  assert.equal(f.calls.filter((call) => call.path.endsWith('/participations')).length, 1, 'rendering and page initialization do not duplicate the automatic join');
  f.click();
  await wait(() => f.doc.querySelector('[data-testid="referral-poster-image"]'));
  assert.equal(f.calls.filter((call) => call.path.endsWith('/invite') && call.method === 'POST').length, 1);
  assert.equal(f.calls.some((call) => call.path.endsWith('/product-context')), false);
  assert.equal(f.dom.window.location.pathname, '/referral');
  f.doc.querySelector('[data-testid="referral-close-poster"]').click();
  f.click();
  await wait(() => f.doc.querySelector('[data-testid="referral-poster-image"]'));
  assert.equal(f.calls.filter((call) => call.path.endsWith('/participations')).length, 1, 'opening the personal poster again reuses membership');
  f.dom.window.close();
}

{
  const f = await fixture({ holdJoin: true, storageDisabled: true });
  await wait(() => f.calls.filter((call) => call.path.endsWith('/participations')).length === 1);
  f.release();
  await wait(() => f.calls.filter((call) => call.path.endsWith('/me')).length === 2);
  await wait(() => f.doc.querySelector('[data-testid="referral-invite"]')?.textContent === '生成溯源码');
  assert.equal(f.calls.filter((call) => call.path.endsWith('/participations')).length, 1, 'disabled browser storage does not interrupt membership confirmation');
  f.click();
  await wait(() => f.doc.querySelector('[data-testid="referral-poster-image"]'));
  assert.equal(f.calls.filter((call) => call.path.endsWith('/invite') && call.method === 'POST').length, 1, 'poster creation still works without session storage');
  f.dom.window.close();
}

{
  const f = await fixture({ failJoin: true });
  await wait(() => f.doc.querySelector('[data-referral-message]')?.textContent.includes('重试确认参加'));
  assert.equal(f.calls.filter((call) => call.path.endsWith('/participations')).length, 1, 'failed recovery is not retried automatically');
  assert.equal(f.calls.filter((call) => call.path.endsWith('/me')).length, 2, 'failed writes still get a safe readback');
  const firstKey = f.calls.find((call) => call.path.endsWith('/participations')).key;
  assert.equal(f.doc.querySelector('[data-testid="referral-invite"]').disabled, false, 'a failed check can be retried explicitly');
  f.click();
  await wait(() => f.calls.filter((call) => call.path.endsWith('/participations')).length === 2);
  await wait(() => f.calls.filter((call) => call.path.endsWith('/me')).length === 3);
  const joinKeys = f.calls.filter((call) => call.path.endsWith('/participations')).map((call) => call.key);
  assert.deepEqual(joinKeys, [firstKey, firstKey], 'an explicit retry reuses the original idempotency key');
  assert.equal(f.calls.some((call) => call.path.endsWith('/invite') && call.method === 'POST'), false, 'membership failure never creates a poster invite');
  f.dom.window.close();
}

{
  const f = await fixture({ failInvite: true });
  await wait(() => f.calls.filter((call) => call.path.endsWith('/me')).length === 2);
  await wait(() => f.doc.querySelector('[data-testid="referral-invite"]')?.textContent === '生成溯源码');
  f.click();
  await wait(() => f.doc.querySelector('[data-referral-message]')?.textContent);
  assert.equal(f.calls.filter((call) => call.path.endsWith('/participations')).length, 1, 'poster failures do not repeat membership writes');
  assert.equal(f.calls.filter((call) => call.path.endsWith('/invite') && call.method === 'POST').length, 1);
  assert.equal(f.doc.querySelector('[data-testid="referral-invite"]').textContent, '生成溯源码');
  f.dom.window.close();
}

{
  const f = await fixture({ eligible: false });
  assert.equal(f.doc.querySelector('[data-testid="referral-invite"]').textContent, '购买并参与');
  assert.equal(f.calls.some((call) => call.path.endsWith('/participations')), false);
  f.dom.window.close();
}

{
  const f = await fixture({ state: 'ended', eligible: true });
  assert.equal(f.doc.querySelector('[data-testid="referral-invite"]').disabled, true);
  assert.equal(f.calls.some((call) => call.path.endsWith('/participations')), false, 'closed activities never auto-join');
  f.dom.window.close();
}

for (const state of ['reversed', 'revoked']) {
  const f = await fixture({ participation: { joined_at: '2026-10-15T11:00:00Z', state }, eligible: true });
  const cta = f.doc.querySelector('[data-testid="referral-invite"]');
  assert.equal(cta.textContent, '参加资格已撤销');
  assert.equal(cta.disabled, true);
  assert.equal(f.calls.some((call) => call.path.endsWith('/participations')), false, `${state} participation cannot be resurrected by automatic enrollment`);
  assert.equal(f.calls.some((call) => call.path.endsWith('/invite') && call.method === 'POST'), false, `${state} participation cannot create a poster`);
  f.dom.window.close();
}

{
  const f = await fixture({
    participation: { joined_at: '2026-10-15T11:00:00Z', state: 'active' },
    eligible: true,
    available: false,
  });
  const cta = f.doc.querySelector('[data-testid="referral-invite"]');
  assert.equal(cta.textContent, '生成溯源码');
  assert.equal(cta.disabled, true, 'members without invitation availability cannot open the personal poster');
  assert.equal(f.calls.some((call) => call.path.endsWith('/participations')), false, 'existing membership does not trigger another automatic join');
  cta.dispatchEvent(new f.dom.window.MouseEvent('click', { bubbles: true }));
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(f.calls.some((call) => call.path.endsWith('/invite') && call.method === 'POST'), false, 'the invite endpoint is not called when invitations are unavailable');
  assert.equal(f.doc.querySelector('[data-testid="referral-poster-image"]'), null);
  f.dom.window.close();
}

{
  const f = await fixture({ unauthenticated: true });
  assert.ok(f.doc.querySelector('a[href*="oauth/start"]'));
  assert.equal(f.calls.some((call) => call.path.endsWith('/participations')), false, 'untrusted sessions cannot trigger membership writes');
  f.dom.window.close();
}

{
  const f = await fixture({ priorAttempt: true });
  assert.equal(f.calls.some((call) => call.path.endsWith('/participations')), false, 'a repeated initialization does not repeat an unresolved automatic attempt');
  assert.equal(f.doc.querySelector('[data-testid="referral-invite"]').textContent, '重试确认参加');
  f.dom.window.close();
}

console.log('purchased entry: trusted auto-join, /me readback, idempotent retry, direct personal poster, and closed/reversed/anonymous guards passed');
