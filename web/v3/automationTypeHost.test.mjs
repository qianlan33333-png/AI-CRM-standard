import assert from 'node:assert/strict';
import { build } from 'esbuild';
import { JSDOM } from 'jsdom';
const code=(await build({entryPoints:['web/v3/automationTypeHost.ts'],bundle:true,write:false,format:'iife'})).outputFiles[0].text;
const html=type=>`<select id="agentType" name="automation_type" value="${type}"><option value="agent">Agent 自动化</option><option value="fixed_script">固定话术</option></select>`;
for(const initial of ['fixed_script','agent']) {
 const dom=new JSDOM(`<body data-page="agentEdit"><form>${html(initial)}<input name="role_prompt"></form>`,{runScripts:'outside-only'});
 dom.window.eval(code);
 const doc=dom.window.document;
 const flush=async()=>{await new Promise(resolve=>setTimeout(resolve,0));};
 let select=doc.querySelector('#agentType');assert.equal(select.value,initial,'initial selection follows model');
 // Preserve a user change during unrelated DOM updates.
 const changed=initial==='agent'?'fixed_script':'agent';select.value=changed;
 doc.querySelector('form').append(doc.createElement('span'));await flush();assert.equal(select.value,changed);
 // Save payload reads the actual native select value; refresh mounts a fresh node.
 const stored=new dom.window.FormData(doc.querySelector('form')).get('automation_type');assert.equal(stored,changed);
 doc.querySelector('form').innerHTML=html(stored)+'<input name="role_prompt" value="edited Prompt">';await flush();
 select=doc.querySelector('#agentType');assert.equal(select.value,stored,'saved type survives refresh');
 assert.equal(new dom.window.FormData(doc.querySelector('form')).get('automation_type'),stored,'Prompt save preserves type');
 dom.window.close();
}
console.log('automation type host: fixed-script/agent creation, user selection, save/readback remount and Prompt save passed');
