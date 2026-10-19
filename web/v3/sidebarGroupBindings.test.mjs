import assert from 'node:assert/strict';
import fs from 'node:fs';
import { build } from 'esbuild';
import { JSDOM } from 'jsdom';

const html = fs.readFileSync('web/dist/sidebar/index.html', 'utf8');
const overlayPath = new URL(new JSDOM(html).window.document.querySelector('#sidebar-workbench-root').dataset.overlayUrl, 'https://fixture.test/sidebar/index.html').pathname;
const overlay = fs.readFileSync('web/dist' + overlayPath, 'utf8');
const bundle = (await build({ entryPoints:['web/v3/sidebar/main.ts'], bundle:true, format:'iife', write:false, target:'es2022' })).outputFiles[0].text;
async function until(fn, message) { for (let i=0;i<150;i++){if(fn())return;await new Promise(r=>setTimeout(r,2));} assert.fail(message); }
let current = 'group-a', binds = [], calls = [], oldResolve, delay = false, loseResponse = false, failBeforeCommit = false, deny = false;
const oldResponse = new Promise(r=>{oldResolve=r;});
const state = new Map();
const dom = new JSDOM(html, {url:'https://fixture.test/sidebar/bind-mobile',runScripts:'outside-only',pretendToBeVisual:true});
const w=dom.window;w.AbortController=AbortController;w.AbortSignal=AbortSignal;
w.document.cookie='aicrm_sidebar_csrf=fixture-csrf; path=/';
let ready=[];
w.wx={ config(){queueMicrotask(()=>ready.splice(0).forEach(f=>f()));},ready(f){ready.push(f);},error(){},agentConfig(o){o.success();},invoke(method,_,done){
  calls.push(method);
  done({err_msg:method+':ok',...(method==='getContext'?{entry:'group_chat_tools'}:method==='getCurExternalChat'?{chatId:current}: {})});
}};
const response=(p,status=200)=>({ok:status===200,status,text:async()=>JSON.stringify(p)});
w.fetch=async(path,options={})=>{
  const url=new URL(path,w.location.href);calls.push(url.pathname);
  if(url.pathname==='/api/sidebar/jssdk-config')return response({corp_id:'corp',agent_id:'1',config:{timestamp:1,nonceStr:'n',signature:'s'},agent_config:{timestamp:1,nonceStr:'n',signature:'s'}});
  const body=JSON.parse(options.body||'{}');
  if(url.pathname.endsWith('/context')){
    if(deny)return response({error:{code:'permission_denied'}},403);
    const data={chat_id:body.chat_id,name:body.chat_id==='group-a'?'体验群 A':'体验群 B',owner_name:'测试群主',checked_at:'2026-10-15T06:00:00Z',bindings:state.get(body.chat_id)||[],candidates:(state.get(body.chat_id)||[]).length?[]:[{plan_id:'12',name:'日课草稿',status:'draft',revision:3}],total:1,limit:50,offset:0};
    if(delay){delay=false;await oldResponse;}
    return response(data);
  }
  if(url.pathname.endsWith('/bind')){
    assert.equal(options.headers['X-CSRF-Token'],'fixture-csrf');
    binds.push({body,key:options.headers['Idempotency-Key']});
    if(failBeforeCommit){failBeforeCommit=false;throw new TypeError('request outcome unavailable');}
    state.set(body.chat_id,[{plan_id:body.plan_id,name:'日课草稿',status:'draft',revision:4}]);
    if(loseResponse){loseResponse=false;throw new TypeError('network response lost');}
    return response({state:'bound',plan:{plan_id:body.plan_id,status:'draft',revision:4}});
  }
  assert.fail('unexpected request '+url.pathname);
};
w.eval(bundle);w.document.dispatchEvent(new w.Event('DOMContentLoaded'));
await until(()=>w.__AICRMSidebarBridge,'Host missing');
w.eval(overlay);
await until(()=>w.document.querySelector('select')&&!w.document.querySelector('select').disabled,'group card missing');
assert.equal(w.__AICRMSidebarBridge.contextMode(),'group');
assert.equal(calls.includes('getCurExternalContact'),false,'group must never call the contact resolver');
assert.equal(calls.includes('/api/sidebar/v2/bootstrap'),false,'group must never mint a customer token');
assert.equal(w.document.querySelector('.top').hidden,true);
const select=w.document.querySelector('select');select.value='12';select.dispatchEvent(new w.Event('change'));
loseResponse=true;
const save=[...w.document.querySelectorAll('button')].find(b=>b.textContent==='绑定当前群');save.click();save.click();
await until(()=>w.document.querySelector('.group-binding-card').textContent.includes('已核对，当前群已绑定'),'save readback failed');
assert.equal(binds.length,1,'double click duplicated write');
assert.equal(binds[0].body.chat_id,'group-a');assert.equal(binds[0].body.expected_revision,3);
assert.equal(w.document.querySelector('.group-binding-card').textContent.includes('日课草稿 · 草稿'),true,'binding must retain draft status');
await w.__AICRMSidebarBridge.retry();
assert.equal(binds.length,1,'reopen must only read');
assert.equal(w.document.querySelector('.group-binding-card').textContent.includes('日课草稿 · 草稿'),true);
// A late A response must not replace B, even if the network ignores abort.
delay=true;const first=w.__AICRMSidebarBridge.retry().catch(()=>{});
await until(()=>delay===false,'delayed group read not started');current='group-b';
Object.defineProperty(w.document,'visibilityState',{configurable:true,value:'hidden'});
w.document.dispatchEvent(new w.Event('visibilitychange'));
assert.equal(w.document.querySelector('.group-binding-card'),null,'hidden WebView retained old group');
Object.defineProperty(w.document,'visibilityState',{configurable:true,value:'visible'});
w.document.dispatchEvent(new w.Event('visibilitychange'));
await until(()=>w.document.querySelector('.group-binding-card')?.textContent.includes('体验群 B'),'resume joined the old pending request');
oldResolve();await first;
assert.equal(w.document.querySelector('.group-binding-card').textContent.includes('体验群 B'),true);
assert.equal(w.document.querySelector('.group-binding-card').textContent.includes('体验群 A'),false);
// An unconfirmed save must retain its original idempotency key and revision.
function chooseAndSave(){const select=w.document.querySelector('select');select.value='12';select.dispatchEvent(new w.Event('change'));[...w.document.querySelectorAll('button')].find(b=>b.textContent==='绑定当前群'||b.textContent==='重试原绑定请求').click();}
failBeforeCommit=true;chooseAndSave();
await until(()=>w.document.querySelector('select')&&!w.document.querySelector('select').disabled&&w.document.querySelector('.group-binding-card').textContent.includes('结果待确认'),'unknown result not exposed');
chooseAndSave();
await until(()=>w.document.querySelector('.group-binding-card').textContent.includes('已核对，当前群已绑定'),'explicit retry failed');
assert.equal(binds.length,3);assert.equal(binds[1].key,binds[2].key);assert.equal(binds[1].body.expected_revision,binds[2].body.expected_revision);
// Detect a group change at submission time even without a focus event.
current='group-c';await w.__AICRMSidebarBridge.retry();current='group-d';chooseAndSave();
await until(()=>w.document.querySelector('select')&&!w.document.querySelector('select').disabled,'group changed before submit did not recover');
assert.equal(binds.length,3,'group change submitted the old selection');
deny=true;await w.__AICRMSidebarBridge.retry();
assert.equal(w.document.querySelector('select'),null,'permission failure retained writable data');
assert.match(w.document.querySelector('.group-binding-card').textContent,/没有群运营管理权限/);
w.close();
console.log('PASS sidebar group recognition, bind/readback, lost response, original-key retry, duplicate click, reopen, cross-group stale response, submit-time switch, denied state; zero contact/send calls');
