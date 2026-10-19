import assert from 'node:assert/strict';
import {build} from 'esbuild';
import {JSDOM} from 'jsdom';

const bundle=(await build({entryPoints:['web/v3/sidebar/main.ts'],bundle:true,format:'iife',write:false,target:'es2022'})).outputFiles[0].text;
const module=(await build({entryPoints:['web/v3/sidebar/groupNavigation.ts'],bundle:true,format:'iife',globalName:'Navigation',write:false,target:'es2022'})).outputFiles[0].text;
const path='/api/sidebar/v2/group-navigation';
let run=null, sdk=[], requests=[], ready=[], blocked=false, rejectOpen=false;
const response=(data,status=200)=>({ok:status===200,status,text:async()=>JSON.stringify(data)});
const initial={run_id:'8',plan_id:'12',plan_name:'内部测试计划',revision:1,state:'paused',cursor:0,targets:[{group_asset_id:'1',chat_id:'group-a',name:'测试多群1'},{group_asset_id:'2',chat_id:'group-b',name:'测试多群5'}],observations:[]};
const dom=new JSDOM('<div id="sidebar-workbench-root"></div>',{url:'https://fixture.test/sidebar/bind-mobile?entry=group-navigation&chat_id=untrusted',runScripts:'outside-only',pretendToBeVisual:true});
const w=dom.window;w.AbortController=AbortController;w.AbortSignal=AbortSignal;w.document.cookie='aicrm_sidebar_csrf=fixture-csrf; path=/';
w.wx={config(){queueMicrotask(()=>ready.splice(0).forEach(f=>f()));},ready(f){ready.push(f);},error(){},agentConfig(o){o.success();},invoke(method,payload,done){sdk.push({method,payload});assert.equal(method,'openExistedChatWithMsg','workbench must not ask for current conversation');assert.deepEqual(Object.keys(payload),['chatId']);done({err_msg:method+(rejectOpen?':fail':':ok')});}};
w.fetch=async(url,options={})=>{
 const u=new URL(url,w.location.href);requests.push(u.pathname);
 if(u.pathname==='/api/sidebar/jssdk-config')return response({corp_id:'corp',agent_id:'1',config:{timestamp:1,nonceStr:'n',signature:'s'},agent_config:{timestamp:1,nonceStr:'n',signature:'s'}});
 assert.ok(u.pathname.startsWith(path),'unexpected request '+u.pathname);
 if(!options.body)return response({plans:[{plan_id:'12',name:'内部测试计划',revision:3}],run});
 assert.equal(options.headers['X-CSRF-Token'],'fixture-csrf');const body=JSON.parse(options.body);const action=u.pathname.slice(path.length+1);
 if(action==='start'){assert.ok(options.headers['Idempotency-Key']);run=structuredClone(initial);return response(run);}
 assert.equal(body.run_id,run.run_id);assert.equal(body.revision,run.revision);
 if(blocked)return response({error:{code:'operations_conflict'}},409);
 if(action==='resume')run={...run,state:'waiting',revision:run.revision+1};
 else if(action==='open')run={...run,state:'open_requested',revision:run.revision+1};
 else if(action==='pause')run={...run,state:'paused',revision:run.revision+1};
 else assert.fail('unexpected mutation '+action);
 return response(run);
};
const until=async(fn,message)=>{for(let i=0;i<150;i++){if(fn())return;await new Promise(r=>setTimeout(r,2));}assert.fail(message);};
const button=(label)=>[...w.document.querySelectorAll('button')].find(b=>b.textContent===label);
w.eval(bundle);w.document.dispatchEvent(new w.Event('DOMContentLoaded'));
await until(()=>w.document.querySelector('select'),'plan list unavailable');
assert.equal(sdk.length,0,'workbench initialization needs no current chat');
w.document.querySelector('select').value='12';w.document.querySelector('select').dispatchEvent(new w.Event('change'));button('保存群清单').click();
await until(()=>button('继续导航')&&!button('继续导航').disabled,'run not initially paused');assert.equal(sdk.length,0);
button('继续导航').click();await until(()=>button('打开当前群')&&!button('打开当前群').disabled,'resume missing');
button('打开当前群').click();await until(()=>sdk.length===1&&button('重新打开本群')&&!button('重新打开本群').disabled,'open missing');
assert.equal(sdk[0].payload.chatId,'group-a','URL target replaced the authorized server target');assert.equal(run.cursor,0,'SDK callback must not confirm target');
// A failed cloud step must never call the SDK.
blocked=true;button('重新打开本群').click();await until(()=>w.document.body.textContent.includes('不会自动重复操作'),'failure state missing');assert.equal(sdk.length,1);blocked=false;
// A fresh workbench page resumes the current cloud step, with no localStorage.
const root=w.document.getElementById('sidebar-workbench-root');w.eval(module+';window.NavigationModule=Navigation;');new w.NavigationModule.GroupNavigationPage(root,{requestNavigation:async()=>({plans:[],run}),openNavigationTarget:async()=>assert.fail('reload auto opened')});
await until(()=>root.textContent.includes('等待侧边栏核验'),'reload did not resume');assert.equal(w.localStorage.length,0);
// Sidebar confirmation reads the actual SDK group again and refuses wrong/hidden context.
let actual='wrong', confirmations=[];
const host={requestNavigation:async(action,body)=>{if(!action)return {run,plans:[]};confirmations.push(body);return {...run,cursor:1};}};
const verify=w.NavigationModule.navigationVerification(host,async()=>actual);root.replaceChildren(verify);
await until(()=>verify.querySelector('button'),'manual no-send navigation unavailable');
verify.querySelector('button').click();await until(()=>verify.textContent.includes('实际群 ID 与本步目标不符'),'wrong group accepted');assert.equal(confirmations.length,0);
actual='group-a';verify.querySelector('button').click();await until(()=>verify.textContent.includes('已核验 1/2'),'valid confirmation missing');assert.equal(confirmations.length,1);assert.equal(confirmations[0].chat_id,'group-a');
Object.defineProperty(w.document,'visibilityState',{configurable:true,value:'hidden'});verify.querySelector('button').click();await new Promise(r=>setTimeout(r,5));assert.equal(confirmations.length,1,'hidden context confirmed');
assert.equal(requests.includes('/api/sidebar/v2/bootstrap'),false);assert.equal(sdk.some(c=>c.payload.msg),false);
// RPA removes the manual verification flow. Only the current, visible mount
// may confirm; the OAuth marker is not an authorization proof.
Object.defineProperty(w.document,'visibilityState',{configurable:true,value:'visible'});
const after='2026-10-16T05:21:55.237467Z';
const sequence={messages:[{}],message_index:0,receipts:[],account_after:after};
const rpa={...structuredClone(initial),state:'open_requested',sequence};
let rpaConfirms=0, renewals=[], deny=false, currentReads=0;
const challenge=`${rpa.run_id}:${rpa.cursor}:${after}`;
const rpaHost={requestNavigation:async(action,body)=>{
 if(!action)return {run:rpa,plans:[]};
 assert.equal(action,'confirm');assert.equal(body.chat_id,'group-a');rpaConfirms++;
 if(deny)throw new w.Error('执行账号认证已失效');
 return {...rpa,state:'verified'};
},renewNavigationAccount:value=>renewals.push(value)};
const mount=()=>{const node=w.NavigationModule.navigationVerification(rpaHost,async()=>{currentReads++;return actual;});root.replaceChildren(node);return node;};
actual='group-a';let automatic=mount();
await until(()=>renewals.length===1,'fresh account redirect missing');
assert.equal(renewals[0],challenge);assert.equal(rpaConfirms,0);assert.equal(automatic.querySelectorAll('button,summary').length,0,'RPA retained manual verification controls');
w.history.replaceState(null,'','/sidebar/bind-mobile?rpa_account='+encodeURIComponent(challenge));
automatic=mount();await until(()=>automatic.textContent.includes('本地助手已就绪'),'automatic confirmation missing');
assert.equal(rpaConfirms,1);assert.ok(automatic.textContent.includes('group-a'));assert.equal(renewals.length,1,'account unnecessarily renewed again');
actual='wrong';automatic=mount();await until(()=>automatic.textContent.includes('当前会话与任务目标不符'),'wrong RPA group accepted');assert.equal(rpaConfirms,1);
actual='group-a';deny=true;automatic=mount();await until(()=>automatic.textContent.includes('执行账号认证已失效'),'server account denial ignored');assert.equal(automatic.textContent.includes('本地助手已就绪'),false);assert.equal(rpaConfirms,2);deny=false;
for(const state of ['paused','granted','outcome_unknown','completed']){rpa.state=state;automatic=mount();await new Promise(r=>setTimeout(r,5));assert.equal(rpaConfirms,2,'terminal or unready state confirmed');}
rpa.state='open_requested';Object.defineProperty(w.document,'visibilityState',{configurable:true,value:'hidden'});automatic=mount();await new Promise(r=>setTimeout(r,5));assert.equal(rpaConfirms,2,'hidden RPA page confirmed');
Object.defineProperty(w.document,'visibilityState',{configurable:true,value:'visible'});automatic=mount();automatic.remove();await new Promise(r=>setTimeout(r,5));assert.equal(rpaConfirms,2,'detached RPA page confirmed');
assert.ok(currentReads>=4);
w.close();console.log('group navigation: workbench isolation, frozen target, paused start, reload, no-msg, callback separation, failed-step refusal, sidebar mismatch and hidden-context contracts passed');
