import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { JSDOM } from 'jsdom';

const html = fs.readFileSync(new URL('./templates/system_settings.html', import.meta.url), 'utf8').replace('{{define "system_settings"}}','').replace('{{end}}','');
const script = fs.readFileSync(new URL('./static/admin_console/system_settings.js', import.meta.url), 'utf8');
const waitFor = async (fn) => { for(let i=0;i<200;i++){if(fn())return;await new Promise(r=>setTimeout(r,2));} throw new Error('expected UI state did not arrive'); };
const response = (data,status=200) => ({ok:status>=200&&status<300,status,json:async()=>structuredClone(data)});
function setup(options={}) {
 const calls=[],policies=new Map([[2,{allowed_cidrs:['115.190.113.5/32','115.190.115.55/32'],version:2}],[3,{allowed_cidrs:[],version:1}]]);
 const dom=new JSDOM(html,{url:'https://fixture.invalid/admin/config/system',runScripts:'outside-only',pretendToBeVisual:true});
 const w=dom.window;
 w.document.cookie='aicrm_admin_csrf=synthetic-csrf';
 w.fetch=async(url,init={})=>{
  calls.push({url,...init});
  const custom=await options.fetch?.(url,init,policies);if(custom)return custom;
  if(url.endsWith('/access/users')) return response({actor:{admin_user_id:2,role:options.role||'super_admin'},users:[{authorized_account:true,admin_user_id:2,display_name:'当前超级管理员',username:'owner'},{authorized_account:true,admin_user_id:3,display_name:'运营管理员',username:'operator'},{authorized_account:false,admin_user_id:0,display_name:'未开通员工'}]});
  if(url.endsWith('/runtime-catalog'))return response({categories:[{key:'wecom_base',label:'企业微信基础',fields:[{label:'启用企业微信'}]},{key:'ai_automation',label:'AI与自动化',fields:[]},{key:'reliability',label:'稳定性',fields:[]}]});
  const id=Number(url.match(/users\/(\d+)/)?.[1]);
  if(init.method==='PUT'){
   assert.equal(init.headers['X-CSRF-Token'],'synthetic-csrf');
   const values=JSON.parse(init.body).allowed_cidrs;
   policies.set(id,{allowed_cidrs:values.map(v=>v.includes('/')?v:`${v}/${v.includes(':')?'128':'32'}`).sort(),version:policies.get(id).version+1});
  }
  return response(policies.get(id));
 };
 w.eval(script);
 const q=s=>w.document.querySelector(s),all=s=>[...w.document.querySelectorAll(s)];
 const ready=()=>waitFor(()=>q('.ss-form')&&calls.some(c=>c.url.endsWith('/runtime-catalog')));
 const submit=()=>q('.ss-form').dispatchEvent(new w.Event('submit',{cancelable:true,bubbles:true}));
 return {dom,w,calls,policies,q,all,ready,submit};
}
test('plain IP display, true account selection, PUT and matching GET confirmation',async()=>{
 const h=setup();try{
 await h.ready();assert.equal(h.all('#ss-users tr').length,2);assert.deepEqual(h.all('.ss-source input').map(n=>n.value),['115.190.113.5','115.190.115.55']);
 const input=h.q('.ss-source input');input.value='115.190.113.6';input.dispatchEvent(new h.w.Event('input'));
 h.submit();h.submit();await waitFor(()=>h.q('#ss-alert').classList.contains('is-success'));
 const writes=h.calls.filter(c=>c.method==='PUT');assert.equal(writes.length,1);assert.deepEqual(JSON.parse(writes[0].body).allowed_cidrs,['115.190.113.6','115.190.115.55']);assert.equal(writes[0].credentials,'same-origin');
 assert.match(h.q('.ss-version').textContent,/3/);assert.match(h.q('#ss-alert').textContent,/读回确认/);
 h.all('input[name="ss-account"]')[1].click();await waitFor(()=>h.q('.ss-account').textContent.includes('ID 3'));assert.equal(h.all('.ss-source').length,0);assert.match(h.q('.ss-editor').textContent,/24 小时/);
 }finally{h.dom.window.close();}
});
test('delete all sources explicitly submits an empty policy',async()=>{
 const h=setup();try{await h.ready();h.q('.ss-remove').click();h.q('.ss-remove').click();h.submit();await waitFor(()=>h.q('#ss-alert').classList.contains('is-success'));assert.deepEqual(h.policies.get(2).allowed_cidrs,[]);assert.match(h.q('.is-selected').textContent,/24 小时/);}finally{h.dom.window.close();}
});
test('failed policy read is not displayed as an empty 24-hour policy',async()=>{
 const h=setup({fetch:async(url)=>url.endsWith('/3/cli-policy')?response({},503):null});try{await h.ready();await waitFor(()=>h.q('#ss-users').textContent.includes('读取失败'));assert.match(h.q('#ss-users').textContent,/读取失败/);h.all('input[name="ss-account"]')[1].click();await waitFor(()=>h.q('#ss-editor').textContent.includes('未读取前不能保存'));assert.equal(h.q('.ss-primary'),null);await new Promise(r=>setTimeout(r,15));}finally{h.dom.window.close();}
});
test('ordinary administrator never requests any account policies',async()=>{
 const h=setup({role:'admin'});try{await waitFor(()=>h.q('#ss-alert').textContent.includes('仅超级管理员'));assert.equal(h.calls.length,1);assert.equal(h.q('#ss-users').children.length,0);}finally{h.dom.window.close();}
});
test('rejected write preserves the draft and never reports success',async()=>{
 const h=setup({fetch:async(_,init)=>init.method==='PUT'?response({},400):null});try{await h.ready();const input=h.q('.ss-source input');input.value='bad-ip';input.dispatchEvent(new h.w.Event('input'));h.submit();await waitFor(()=>h.q('#ss-alert').textContent.includes('格式不正确'));assert.equal(h.q('.ss-source input').value,'bad-ip');assert.equal(h.q('#ss-alert').classList.contains('is-success'),false);}finally{h.dom.window.close();}
});
test('unknown write result only reads back and does not repeat PUT',async()=>{
 const h=setup({fetch:async(_,init)=>{if(init.method==='PUT')throw new Error('response lost');}});try{await h.ready();h.submit();await waitFor(()=>h.q('#ss-alert').textContent.includes('未自动重试'));assert.equal(h.calls.filter(c=>c.method==='PUT').length,1);assert.equal(h.q('#ss-alert').classList.contains('is-success'),false);}finally{h.dom.window.close();}
});
test('permission loss clears account details despite late responses',async()=>{
 let unblock;const held=new Promise(r=>unblock=r);
 const h=setup({fetch:async(url)=>url.endsWith('/2/cli-policy')?held:url.endsWith('/3/cli-policy')?response({},403):null});try{await waitFor(()=>h.q('#ss-alert').textContent.includes('仅超级管理员'));unblock(response({allowed_cidrs:['203.0.113.1/32'],version:99}));await new Promise(r=>setTimeout(r,10));assert.equal(h.q('#ss-users').children.length,0);assert.equal(h.q('#ss-editor').children.length,0);}finally{h.dom.window.close();}
});
test('category tabs and search point to existing real settings',async()=>{
 const h=setup();try{await h.ready();h.all('.ss-tab')[1].click();assert.equal(h.q('#ss-accounts').hidden,true);assert.equal(h.q('.ss-category a').getAttribute('href'),'/admin/configDetail.html?cat=wecom_base');const search=h.q('#ss-search');search.value='大模型';search.dispatchEvent(new h.w.Event('input'));assert.match(h.q('#ss-categories').textContent,/大模型设置/);assert.equal(h.q('.ss-category a').getAttribute('href'),'/admin/configDetail.html?cat=ai_models');}finally{h.dom.window.close();}
});
