import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import path from 'node:path';
import {JSDOM,VirtualConsole} from 'jsdom';
const settle=async()=>{for(let i=0;i<18;i++)await new Promise(r=>setImmediate(r));};
async function open(kind,entry,storage={}){
 const html=await readFile(path.join(process.argv[2],`${kind}-${entry}.html`),'utf8'),posts=[],errors=[];
 const console=new VirtualConsole();console.on('jsdomError',e=>errors.push(e.message));
 const url=`https://example.test/${kind==='standard'?'pay/course':'s/course/pay'}?sales_source=${entry==='a'?11:22}`;
 const dom=new JSDOM(html,{url,runScripts:'dangerously',virtualConsole:console,beforeParse(w){Object.defineProperty(w.navigator,'userAgent',{value:'MicroMessenger'});for(const [k,v]of Object.entries(storage))w.sessionStorage.setItem(k,v);w.fetch=async(url,options={})=>{if(options.method==='POST'){posts.push({body:JSON.parse(options.body),key:options.headers['Idempotency-Key']});return{ok:false,status:503,json:async()=>({code:'unavailable'})}};const value=String(url).includes('checkout-session')?{checkout_session_binding:'a'.repeat(43),can_create_checkout:true}:String(url).includes('purchase-status')?{purchase_state:'available',can_purchase:true}:String(url).includes('coupons/available')?{items:[]}:{};return{ok:true,status:200,json:async()=>value}}}});await settle();assert.deepEqual(errors,[]);return{dom,posts};
}
for(const kind of ['standard','service_period']){
 const a=await open(kind,'a'),b=await open(kind,'b');
 assert.equal(new URL(a.dom.window.document.getElementById('authContinue').href).searchParams.get('return_url'),new URL(a.dom.window.location.href).pathname+'?sales_source=11');
 a.dom.window.document.getElementById('buy').click();b.dom.window.document.getElementById('buy').click();await settle();
 assert.equal(a.posts[0].body.sales_source.id,11);assert.equal(b.posts[0].body.sales_source.id,22);
 const saved={};for(let i=0;i<a.dom.window.sessionStorage.length;i++){const k=a.dom.window.sessionStorage.key(i);saved[k]=a.dom.window.sessionStorage.getItem(k)}
 a.dom.window.document.getElementById('buy').click();await settle();assert.deepEqual(a.posts[1],a.posts[0],'same request retains source and key');
 const resumed=await open(kind,'b',saved);resumed.dom.window.document.getElementById('buy').click();await settle();assert.deepEqual(resumed.posts[0],a.posts[0],'B must retry the frozen A request');
 const legacy=Object.fromEntries(Object.entries(saved).map(([k,raw])=>{const v=JSON.parse(raw);delete v.payload.sales_source;return[k,JSON.stringify(v)]}));const old=await open(kind,'b',legacy);old.dom.window.document.getElementById('buy').click();await settle();assert.equal(Object.hasOwn(old.posts[0].body,'sales_source'),false,'legacy request must stay unrecorded');
 for(const p of [a,b,resumed,old])p.dom.window.close();
}
console.log('source tab isolation, auth return, frozen retry and legacy omission: PASS');
