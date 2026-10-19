import { resolveChromiumBinary } from '../../internal/webshell/chromium_binary.mjs';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';
const base=process.env.AICRM_PURCHASED_BROWSER_URL,campaign=Number(process.env.AICRM_PURCHASED_BROWSER_CAMPAIGN);
assert.match(base||'',/^https:\/\/127\.0\.0\.1:/);assert.ok(campaign>0);
const profile=await fs.mkdtemp(path.join(os.tmpdir(),'referral-purchased-browser-'));
const screenshots=process.env.AICRM_PURCHASED_BROWSER_SCREENSHOTS||path.join(profile,'screenshots');await fs.mkdir(screenshots,{recursive:true});
const sleep=ms=>new Promise(r=>setTimeout(r,ms));let chrome,socket;const pending=new Map();let sequence=0;
try {
 chrome=spawn(resolveChromiumBinary(),['--headless=new','--no-sandbox','--remote-debugging-port=0',`--user-data-dir=${profile}`,'--no-first-run','--ignore-certificate-errors','--disable-background-networking','about:blank'],{stdio:'ignore'});
 let port;for(let i=0;i<200&&!port;i++){try{port=(await fs.readFile(path.join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0]}catch{}if(!port)await sleep(100)}assert.ok(port,'browser startup');
 const tab=await(await fetch(`http://127.0.0.1:${port}/json/new?about:blank`,{method:'PUT'})).json();
 socket=new WebSocket(tab.webSocketDebuggerUrl);await new Promise((r,j)=>{socket.addEventListener('open',r,{once:true});socket.addEventListener('error',j,{once:true})});
 const writes=[],errors=[];
 socket.addEventListener('message',e=>{const m=JSON.parse(String(e.data));if(m.id){const p=pending.get(m.id);pending.delete(m.id);m.error?p?.reject(new Error(m.error.message)):p?.resolve(m.result)}else if(m.method==='Network.requestWillBeSent'&&m.params.request.method==='POST')writes.push(m.params.request);else if(m.method==='Runtime.exceptionThrown')errors.push(m.params.exceptionDetails.text)});
 const call=(method,params={})=>new Promise((resolve,reject)=>{const id=++sequence;pending.set(id,{resolve,reject});socket.send(JSON.stringify({id,method,params}))});
 const evaluate=async expression=>{const r=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(r.exceptionDetails)throw new Error(r.exceptionDetails.exception?.description||r.exceptionDetails.text);return r.result?.value};
 const wait=async expression=>{for(let i=0;i<180;i++){if(await evaluate(expression))return;await sleep(75)}throw new Error('UI condition failed: '+expression+' page='+await evaluate('document.body.innerText'))};
 const capture=async name=>{const s=await call('Page.captureScreenshot',{format:'png'});await fs.writeFile(path.join(screenshots,name),Buffer.from(s.data,'base64'))};
 await call('Page.enable');await call('Runtime.enable');await call('Network.enable');
 await call('Network.setCookie',{name:'aicrm_distribution_session',value:process.env.AICRM_PURCHASED_BROWSER_SESSION,url:base,secure:true,sameSite:'Lax'});
 await call('Network.setCookie',{name:'aicrm_distribution_csrf',value:'purchased-entry-csrf',url:base,secure:true,sameSite:'Strict'});
 await call('Emulation.setDeviceMetricsOverride',{width:375,height:850,deviceScaleFactor:1,mobile:true});
 await call('Page.navigate',{url:`${base}/referral?campaign=${campaign}`});
 await wait("document.querySelector('[data-testid=referral-invite]')?.textContent==='生成溯源码'");
 await wait("!document.querySelector('[data-testid=referral-invite]')?.disabled");assert.equal(writes.filter(r=>r.url.endsWith('/participations')).length,1,'trusted eligibility automatically joins once through POST');assert.deepEqual(JSON.parse(writes.find(r=>r.url.endsWith('/participations')).postData),{});await capture('02-purchased-entry-375.png');
 await evaluate("document.querySelector('[data-testid=referral-invite]').click();document.querySelector('[data-testid=referral-invite]').click()");
 await wait("document.querySelector('[data-testid=referral-poster-image]')?.src.startsWith('data:image/')");
 assert.equal(await evaluate('location.pathname'),'/referral');
 assert.equal(await evaluate("!!document.querySelector('[data-testid=referral-accept-dialog]')"),false);
 assert.equal(writes.filter(r=>r.url.endsWith('/participations')).length,1);
 assert.deepEqual(JSON.parse(writes.find(r=>r.url.endsWith('/participations')).postData),{});
 assert.equal(writes.filter(r=>r.url.endsWith('/invite')).length,1);assert.equal(writes.some(r=>r.url.endsWith('/product-context')),false);
 assert.ok(await evaluate("(()=>{const i=document.querySelector('[data-testid=referral-poster-image]');return i.naturalWidth>0&&i.naturalHeight>0})()"));
 await capture('03-personal-poster-375.png');
 await evaluate("document.querySelector('[data-testid=referral-close-poster]').click()");
 await call('Page.reload');await wait("document.querySelector('[data-testid=referral-invite]')?.textContent==='生成溯源码'");
 await evaluate("document.querySelector('[data-testid=referral-invite]').click()");await wait("document.querySelector('[data-testid=referral-poster-image]')?.src.startsWith('data:image/')");
 assert.equal(writes.filter(r=>r.url.endsWith('/participations')).length,1,'reload uses durable participation');assert.deepEqual(errors,[]);
 console.log('purchased_entry_chromium: PASS real HTTP/PG direct paid CTA -> personal rendered poster -> reload/reopen; screenshots='+screenshots);
} finally {
 try{socket?.close()}catch{}if(chrome){chrome.kill('SIGTERM');await Promise.race([new Promise(r=>chrome.once('exit',r)),sleep(3000)]);if(chrome.exitCode===null)chrome.kill('SIGKILL')}
 await fs.rm(profile,{recursive:true,force:true});
}
