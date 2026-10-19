import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { resolveChromiumBinary } from '../../internal/webshell/chromium_binary.mjs';
const fixture = JSON.parse(process.env.AICRM_SYSTEM_SETTINGS_INSTALLED_FIXTURE || '{}');
assert.ok(['linux','darwin'].includes(process.platform), 'Chromium contract platform'); // The release workbench runs Linux.
assert.match(fixture.base || '', /^https:\/\/example\.com:\d+$/);
assert.ok(Number.isSafeInteger(fixture.target) && fixture.target > 0);
fixture.policy = `/api/admin/access/users/${fixture.target}/cli-policy`;
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
class CDP {
  pending = new Map(); sequence = 0; errors = 0; policyPuts = 0;
  constructor(socket) {
    this.socket = socket;
    socket.addEventListener('message', event => {
      const message = JSON.parse(String(event.data));
      if (message.method === 'Runtime.exceptionThrown') this.errors++;
      if (message.method === 'Network.requestWillBeSent') {
        const request = message.params.request;
        const pathname = new URL(request.url).pathname;
        if (request.method === 'PUT' && pathname === fixture.policy) this.policyPuts++;
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

async function installedSettings(cdp) {
  await cdp.call('Emulation.setDeviceMetricsOverride', { width: 1487, height: 1058, deviceScaleFactor: 1, mobile: false });
  for (const [name, cookie] of Object.entries(fixture.cookies)) {
    await cdp.call('Network.setCookie', { name, value: cookie, url: fixture.base, path: '/', secure: true, httpOnly: name === 'aicrm_admin_session' });
  }
  await cdp.call('Page.navigate', { url: fixture.base + '/admin/config' });
  await until(cdp, `!!document.querySelector('a[href="/admin/config/system"]')`, 'installed config entry missing');
  await value(cdp, `document.querySelector('a[href="/admin/config/system"]').click(); true`);
  await until(cdp, `document.querySelector('#ss-editor .ss-form') && document.querySelectorAll('#ss-users input').length >= 3`, 'installed settings page did not load real directory');
  assert.equal(await value(cdp, `document.querySelectorAll('.admin-sidebar').length`), 1, 'settings duplicated shell');
  assert.equal(await value(cdp, `document.documentElement.scrollWidth > innerWidth`), false, 'settings overflows desktop');
  // Requests below use real authentication, cookies and CSRF. Keep all bearer
  // values inside this synthetic browser; return assertions/counts only.
  await value(cdp, `(() => {
    const csrf=()=>decodeURIComponent(document.cookie.split(';').map(x=>x.trim()).find(x=>x.startsWith('aicrm_admin_csrf=')).slice(17));
    window.settingsCall=async(method,path,body,token,source)=>{
      const headers={Accept:'application/json',...(body===undefined?{}:{'Content-Type':'application/json','X-CSRF-Token':token?.csrf_token||csrf()}),...(token?{Authorization:'Bearer '+token.access_token}:{}),...(source?{'X-Forwarded-For':source}:{})};
      const r=await fetch(path,{method,credentials:token?'omit':'same-origin',headers,...(body===undefined?{}:{body:JSON.stringify(body)})});
      const text=await r.text();let data;try{data=JSON.parse(text);}catch{} return {status:r.status,data};
    };
    window.settingsLogin=()=>settingsCall('POST','/api/admin/access/cli/login',{username:'settings-installed-viewer',password:'isolated-settings-password'},null,'203.0.113.20');
    return true;
  })()`);
  assert.equal(await value(cdp, `(async()=>{const r=await settingsLogin();window.settingsShort=r.data;return r.status===200&&Date.parse(r.data.expires_at)-Date.now()>23*3600000;})()`), true, 'default CLI token is not 24 hours');
  await value(cdp, `document.querySelector('input[aria-label$="（ID ${fixture.target}）"]').click(); true`);
  await until(cdp, `!!document.querySelector('#ss-editor .ss-form') && document.querySelector('#ss-editor .ss-account')?.textContent.includes('settings-installed-viewer')`, 'target selection failed');
  await value(cdp, `(()=>{const add=[...document.querySelectorAll('#ss-editor button')].find(x=>x.textContent==='添加 IP');add.click();add.click();const inputs=document.querySelectorAll('.ss-source input');['203.0.113.20','2001:db8::20'].forEach((v,i)=>{inputs[i].value=v;inputs[i].dispatchEvent(new Event('input',{bubbles:true}));});document.querySelector('.ss-form').requestSubmit();return true;})()`);
  await until(cdp, `document.querySelector('#ss-alert')?.textContent.startsWith('白名单已保存并读回确认')`, 'plain IP save/readback failed');
  assert.deepEqual(await value(cdp, `[...document.querySelectorAll('.ss-source input')].map(x=>x.value).sort()`), ['2001:db8::20','203.0.113.20'], 'host prefixes leaked into human input');
  assert.equal(await value(cdp, `(async()=>{const r=await settingsCall('GET',${JSON.stringify(fixture.policy)});return r.status===200&&r.data.version===2&&JSON.stringify(r.data.allowed_cidrs.slice().sort())===JSON.stringify(['2001:db8::20/128','203.0.113.20/32']);})()`), true, 'installed canonical API readback mismatch');
  assert.equal(await value(cdp, `(async()=> (await settingsCall('GET','/api/admin/access/cli/session',undefined,settingsShort,'203.0.113.20')).status)()`), 401, 'old token survived UI save');
  assert.equal(await value(cdp, `(async()=>{const r=await settingsLogin();window.settingsLong=r.data;return r.status===200&&r.data.expires_at===null;})()`), true, 'allowed source did not obtain long token');
  assert.equal(await value(cdp, `(async()=> (await settingsCall('GET','/api/admin/access/cli/session',undefined,settingsLong,'203.0.113.20')).status)()`), 200);
  assert.equal(await value(cdp, `(async()=> (await settingsCall('GET','/api/admin/access/cli/session',undefined,settingsLong,'198.51.100.20')).status)()`), 403, 'wrong source accepted');
  await screenshot(cdp, 'settings-plain-ip-saved');
  await cdp.call('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
  assert.equal(await value(cdp, `document.documentElement.scrollWidth > innerWidth`), false, 'settings overflows mobile');
  await screenshot(cdp, 'settings-mobile');
  await value(cdp, `(()=>{while(document.querySelector('.ss-remove'))document.querySelector('.ss-remove').click();document.querySelector('.ss-form').requestSubmit();return true;})()`);
  await until(cdp, `document.querySelector('#ss-alert')?.textContent.startsWith('白名单已保存并读回确认') && document.querySelector('.ss-version')?.textContent.includes('3')`, 'clear/readback failed');
  assert.equal(await value(cdp, `(async()=>{const r=await settingsCall('GET',${JSON.stringify(fixture.policy)});return r.status===200&&r.data.version===3&&r.data.allowed_cidrs.length===0;})()`), true);
  assert.equal(await value(cdp, `(async()=> (await settingsCall('GET','/api/admin/access/cli/session',undefined,settingsLong,'203.0.113.20')).status)()`), 401, 'long token survived clear');
  assert.equal(await value(cdp, `(async()=>{const r=await settingsLogin();return r.status===200&&Date.parse(r.data.expires_at)-Date.now()>23*3600000;})()`), true, 'clear did not restore 24-hour policy');
  await screenshot(cdp, 'settings-cleared');
  assert.equal(cdp.policyPuts, 2, 'settings UI repeated policy write');
  // Each lower role signs in through the actual installed browser login. A
  // cookie cannot be promoted to a super administrator by opening this route.
  for (const role of ['admin','viewer']) {
    await cdp.call('Network.clearBrowserCookies');
    await cdp.call('Page.navigate', { url: fixture.base + '/login' });
    await until(cdp, `!!document.querySelector('input[name="login_csrf_token"]')`, 'login CSRF missing');
    const outcome=await value(cdp, `(async()=>{
      const csrf=document.querySelector('input[name="login_csrf_token"]').value;
      const r=await fetch('/login',{method:'POST',headers:{'Content-Type':'application/json',Accept:'application/json'},body:JSON.stringify({username:${JSON.stringify('settings-installed-'+role)},password:'isolated-settings-password',login_csrf_token:csrf})});
      if(r.status!==200)return {login:r.status};
      const page=await fetch('/admin/config/system',{cache:'no-store'});const markup=await page.text();
      const headers={Accept:'application/json','Content-Type':'application/json','X-CSRF-Token':decodeURIComponent(document.cookie.split(';').map(x=>x.trim()).find(x=>x.startsWith('aicrm_admin_csrf=')).slice(17))};
      const statuses=[];for(const [url,method] of [['/api/admin/access/users','GET'],[${JSON.stringify(fixture.policy)},'GET'],[${JSON.stringify(fixture.policy)},'PUT']]){const x=await fetch(url,{method,headers,...(method==='PUT'?{body:JSON.stringify({allowed_cidrs:['203.0.113.21']})}:{})});statuses.push(x.status);}
      return {login:r.status,page:page.status,hasSettings:markup.includes('data-system-settings-root'),statuses};
    })()`);
    assert.deepEqual(outcome,{login:200,page:403,hasSettings:false,statuses:[role==='admin'?200:403,403,403]}, 'installed '+role+' permissions mismatch');
  }
  assert.equal(cdp.policyPuts,4,'expected two UI saves and two rejected lower-role writes');
}
const profile = await fs.mkdtemp(path.join(os.tmpdir(), 'aicrm-system-settings-'));
let browser, cdp, failure;
try {
  browser = spawn(resolveChromiumBinary(), ['--headless=new','--no-sandbox','--ignore-certificate-errors','--no-proxy-server','--host-resolver-rules=MAP example.com 127.0.0.1','--disable-background-networking','--remote-debugging-port=0',`--user-data-dir=${profile}`,'about:blank'], { stdio:'ignore' });
  let port;
  for(let i=0;i<200;i++){try{port=(await fs.readFile(path.join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0];if(/^\d+$/.test(port))break;}catch{}await pause(50);}
  assert.match(port||'',/^\d+$/,'isolated Chromium startup');
  const target=await(await fetch(`http://127.0.0.1:${port}/json/new?about:blank`,{method:'PUT'})).json();
  const socket=new WebSocket(target.webSocketDebuggerUrl);
  await new Promise((resolve,reject)=>{socket.addEventListener('open',resolve,{once:true});socket.addEventListener('error',reject,{once:true});});
  cdp=new CDP(socket);await cdp.call('Page.enable');await cdp.call('Runtime.enable');await cdp.call('Network.enable');
  await installedSettings(cdp);
  assert.equal(cdp.errors,0,'installed settings emitted a JavaScript exception');
  console.log(JSON.stringify({contract:'system-settings-installed',status:'passed',policy_puts:2,rejected_policy_puts:cdp.policyPuts-2,javascript_errors:cdp.errors}));
}catch(error){failure=error;if(cdp)await screenshot(cdp,'settings-failure').catch(()=>{});}
finally{
  cdp?.close();
  if(browser&&browser.exitCode===null&&browser.signalCode===null){browser.kill('SIGTERM');for(let i=0;i<60&&browser.exitCode===null&&browser.signalCode===null;i++)await pause(50);if(browser.exitCode===null&&browser.signalCode===null){browser.kill('SIGKILL');await new Promise(resolve=>browser.once('exit',resolve));}}
  await fs.rm(profile,{recursive:true,force:true,maxRetries:5}).catch(()=>console.warn('closed synthetic browser profile cleanup warning'));
}
if(failure)throw failure;
