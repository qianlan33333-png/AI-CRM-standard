import assert from 'node:assert/strict';
import {test} from 'node:test';
import {JSDOM} from 'jsdom';
import fs from 'node:fs';
const source=fs.readFileSync(new URL('./static/sidebar_workbench/sidebar_ai_operations.js',import.meta.url),'utf8');
const tick=()=>new Promise(resolve=>setImmediate(resolve));
test('profile renders only field names and complete details; repeated backgrounds remain separate',async()=>{
 const dom=new JSDOM('<main id="profile"></main>',{runScripts:'outside-only'});const w=dom.window;
 w.eval(source);
 const long='2026年4月已经营写作、个人IP和AI等课程、训练营及年度社群，训练营常复用录播课，由本人答疑、助教点评辅导。'+'完整明细。'.repeat(1000);
 const fields={basic_fields:[{key:'wecom_nickname',label:'企微昵称',value:'不展示昵称'},{key:'identity',label:'身份／角色',value:'知识IP创业者'}],diagnosis_fields:[{key:'current_need',label:'当前主需求',value:'获客策略建议'}],background_fields:[{key:'a',label:'背景与经历',value:long},{key:'b',label:'背景与经历',value:'另一条完整经历 <script>禁止执行</script>'}]};
 let calls=0;
 const view=w.SidebarAIOperations.mount({container:w.document.querySelector('main'),request:async()=>{calls++;return {state:'ready',data:{...fields,synced_at:'2026-10-17T00:00:00Z'},expires_at:new Date(Date.now()+300000).toISOString(),generation_ready:false};},checkContext:async()=>true,readURL:'/read',generateURL:'/generate',friendly:e=>e.message});
 await tick();await tick();
 const body=w.document.body;
 assert.ok(body.textContent.includes(long));assert.equal(body.querySelectorAll('.ai-profile-field').length,4);
 assert.equal(body.querySelectorAll('.ai-profile-list').length,1);
 assert.equal(body.querySelectorAll('.ai-profile-list h3').length,0);
 assert.equal(/不展示昵称|用户基础信息|背景、需求与建议参考/.test(body.textContent),false);
 assert.equal([...body.querySelectorAll('button')].some(n=>/刷新/.test(n.textContent)),false);
 assert.equal(body.querySelectorAll('details,small,time,script').length,0);
 assert.equal(/发生：|整理：|来源|同步时间|2026-10-17T00:00/.test(body.textContent),false);
 assert.equal(body.querySelector('.ai-profile-field dd').textContent,'知识IP创业者');
 assert.equal(calls,1);
 view.dispose();dom.window.close();
});

test('empty mapped profile stays in the quiet field view',async()=>{
 const dom=new JSDOM('<main></main>',{runScripts:'outside-only'});const w=dom.window;w.eval(source);
 const view=w.SidebarAIOperations.mount({container:w.document.querySelector('main'),request:async()=>({state:'ready',data:{profile_mode:'fields'},generation_ready:false}),checkContext:async()=>true,readURL:'/read',friendly:e=>e.message});
 await tick();await tick();
 assert.equal(w.document.querySelector('.status').textContent,'暂无画像');
 assert.equal(w.document.querySelector('.head').hidden,true);
 assert.equal(w.document.querySelector('.seg').hidden,true);
 assert.equal(w.document.body.textContent.includes('尚无清洗后的摘要'),false);
 view.dispose();w.close();
});
