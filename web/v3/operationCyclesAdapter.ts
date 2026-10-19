import { mountOperationExcelWorkspace } from './excelBatches';
// This is the only v3-owned browser seam for operation cycles. It supplies a
// narrow AdminApi.loadDb implementation plus the host-side HTTP binding for
// the donor's existing primary action. It never renders or styles controls.
import { emptyAdminDb, type AdminReadContext } from '../src/api/admin';
import { request } from '../src/api/transport';
import { api } from '../src/shared/api/client';
import type { AdminDb, CycleRun, Tone } from '../src/shared/api/types';

type RecordValue = Record<string, unknown>;
type Strategy = { strategyKey: string; title: string; status: string; version: number; runKey: string; runOrdinal: number; actionKey: string; snapshot: RecordValue; native?: {package_id:number;interval_hours:number} };
export type OperationCycleStageInput = { key: string; label: string; color: string; state: 'completed' | 'current' | 'pending' };
export type OperationCycleStrategyDefinitionInput = { schedule: string; indicator_color: string; primary_action: 'start_review' | 'view_progress'; stages: OperationCycleStageInput[]; native_generation?:{package_id:number;interval_hours:number} };
export type OperationCycleStrategyCreateInput = { strategy_key: string; title: string; definition: OperationCycleStrategyDefinitionInput };
export type OperationCycleStrategyUpdateInput = { expected_version: number; title: string; definition: OperationCycleStrategyDefinitionInput };

const object = (value: unknown): RecordValue => value !== null && typeof value === 'object' && !Array.isArray(value) ? value as RecordValue : {};
const array = (value: unknown): unknown[] => Array.isArray(value) ? value : [];
const string = (value: unknown): string => typeof value === 'string' ? value : '';
const requiredString = (value: unknown, field: string): string => {
  const result = string(value);
  if (!result) throw new Error(`运营周期读取响应缺少 ${field}`);
  return result;
};
const tone = (value: unknown): Tone => {
  const result = string(value);
  return result === 'ok' || result === 'blue' || result === 'warn' || result === 'red' || result === 'gray' || result === 'purple' ? result : 'gray';
};

async function get(path: string): Promise<RecordValue> {
  const response = await fetch(path, { credentials: 'same-origin', headers: { Accept: 'application/json' } });
  if (!response.ok) throw new Error(`运营周期读取失败（HTTP ${response.status}）`);
  return object(await response.json());
}

async function listStrategies(): Promise<Strategy[]> {
  const result = await get('/api/admin/operation-cycles/strategies?limit=100&offset=0');
  return array(result.items).map((value, index) => {
    const item = object(value);
    const snapshot = object(item.snapshot);
    if (!snapshot.schema_version || snapshot.schema_version !== 'operation_cycle_snapshot.v1') throw new Error(`运营周期读取响应第 ${index + 1} 项不是已投影快照`);
    const version = Number(item.version);
    if (!Number.isSafeInteger(version) || version < 1) throw new Error(`运营周期读取响应第 ${index + 1} 项缺少有效版本`);
    const runKey = string(snapshot.run_key);
    const runOrdinal = Number(item.run_ordinal ?? 0);
    if (runKey && (!Number.isSafeInteger(runOrdinal) || runOrdinal < 1 || runOrdinal > 999999999)) throw new Error(`运营周期读取响应第 ${index + 1} 项缺少稳定运行档案编号`);
    return {
      strategyKey: requiredString(item.strategy_key, 'strategy_key'),
      title: requiredString(item.title, 'title'),
      status: requiredString(item.status, 'status'),
      version,
      runKey,
      runOrdinal,
      actionKey: string(snapshot.action_key) || (string(snapshot.action) === '查看进度' ? 'view_progress' : 'start_review'),
      snapshot,
      native: object(item.definition).native_generation as Strategy['native'],
    };
  });
}

async function write(path: string, method: 'POST' | 'PUT', body: RecordValue, idempotencyKey: string): Promise<RecordValue> {
  const response = await request(path, { method, headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'Idempotency-Key': idempotencyKey }, body: JSON.stringify(body) });
  return object(await response.json());
}

export async function createOperationCycleStrategy(input: OperationCycleStrategyCreateInput, idempotencyKey: string): Promise<RecordValue> {
  return write('/api/admin/operation-cycles/strategies', 'POST', input, idempotencyKey);
}

export async function updateOperationCycleStrategy(strategyKey: string, input: OperationCycleStrategyUpdateInput, idempotencyKey: string): Promise<RecordValue> {
  return write(`/api/admin/operation-cycles/strategies/${encodeURIComponent(strategyKey)}`, 'PUT', input, idempotencyKey);
}

export async function setOperationCycleStatus(strategyKey: string, expectedVersion: number, status: 'active' | 'paused' | 'archived', idempotencyKey: string): Promise<RecordValue> {
  return write(`/api/admin/operation-cycles/strategies/${encodeURIComponent(strategyKey)}/status`, 'POST', { expected_version: expectedVersion, status }, idempotencyKey);
}

export async function listOperationCycleStrategyVersions(strategyKey: string): Promise<RecordValue> {
  return get(`/api/admin/operation-cycles/strategies/${encodeURIComponent(strategyKey)}/versions?limit=100&offset=0`);
}

export async function listOperationCycleRunVersions(runKey: string): Promise<RecordValue> {
  return get(`/api/admin/operation-cycles/runs/${encodeURIComponent(runKey)}/versions?limit=100&offset=0`);
}

let loadedStrategies: Strategy[] = [];

function stableActionKey(strategy: Strategy, operation: string): string {
  return `cycle-${operation}-${strategy.strategyKey}-${strategy.version}-${strategy.runKey || 'no-run'}`;
}

async function runPrimaryAction(strategy: Strategy): Promise<void> {
  if (strategy.status === 'draft' || strategy.status === 'paused') {
    await setOperationCycleStatus(strategy.strategyKey, strategy.version, 'active', stableActionKey(strategy, 'activate'));
    globalThis.alert?.('运营周期已启用');
    globalThis.location.reload();
    return;
  }
  if(strategy.native){globalThis.location.assign('/admin/cloud-orchestrator/plans');return;}
  if (strategy.actionKey === 'view_progress') {
    if (!strategy.runKey) throw new Error('该策略暂无运行档案');
    globalThis.location.assign(`/admin/operation-cycles/cyclesDetail.html?id=${strategy.runOrdinal}`);
    return;
  }
  if (!strategy.runKey) throw new Error('该策略暂无可复盘的运行档案');
  await write(`/api/admin/operation-cycles/strategies/${encodeURIComponent(strategy.strategyKey)}/actions/${encodeURIComponent(strategy.actionKey)}/start`, 'POST', { run_key: strategy.runKey, parent_request_id: '' }, stableActionKey(strategy, 'start'));
  globalThis.alert?.('复盘请求已受理');
}

document.addEventListener('click', (event) => {
  if (document.body.dataset.page !== 'cycles') return;
  const target = event.target;
  if (!(target instanceof Element)) return;
  const button = target.closest('button');
  const row = button?.closest('tbody tr');
  if (!button || !row) return;
  const buttons = Array.from(row.querySelectorAll('button'));
  const rowIndex = Array.from(row.parentElement?.querySelectorAll(':scope > tr') || []).indexOf(row);
  if (rowIndex < 0 || rowIndex >= loadedStrategies.length) return;
  const strategy = loadedStrategies[rowIndex];
  if (buttons[0] === button) {
    event.preventDefault();
    event.stopImmediatePropagation();
    void runPrimaryAction(strategy).catch((error) => globalThis.alert?.(error instanceof Error ? error.message : '运营周期操作失败'));
  } else if (buttons[1] === button && !strategy.runKey) {
    event.preventDefault();
    event.stopImmediatePropagation();
    globalThis.alert?.('该策略暂无运行档案');
  }
}, true);

function stringList(value: unknown): string[] {
  return array(value).map((item) => requiredString(item, 'display text'));
}

function displayRun(id: number, value: RecordValue): CycleRun {
  const dossier = object(value.dossier);
  if (Object.keys(dossier).length === 0) throw new Error('运营周期运行档案尚未提供安全投影');
  const delivery = object(dossier.delivery);
  const retro = object(dossier.retro);
  const next = object(dossier.next);
  return {
    id,
    label: requiredString(dossier.label, 'dossier.label'),
    objective: requiredString(dossier.objective, 'dossier.objective'),
    strategy: requiredString(dossier.strategy, 'dossier.strategy'),
    runKey: requiredString(value.run_key, 'run_key'),
    snapshotRev: String(value.revision ?? ''),
    audience: requiredString(dossier.audience, 'dossier.audience'),
    intendedSendAt: requiredString(dossier.intended_send_at, 'dossier.intended_send_at'),
    planScheduledFor: requiredString(dossier.plan_scheduled_for, 'dossier.plan_scheduled_for'),
    firstSentAt: requiredString(dossier.first_sent_at, 'dossier.first_sent_at'),
    lastSentAt: requiredString(dossier.last_sent_at, 'dossier.last_sent_at'),
    attempts: array(dossier.attempts).map((entry) => {
      const attempt = object(entry);
      return { label: requiredString(attempt.label, 'attempt.label'), statusLabel: requiredString(attempt.status_label, 'attempt.status_label'), tone: tone(attempt.tone), summary: requiredString(attempt.summary, 'attempt.summary'), startedAt: requiredString(attempt.started_at, 'attempt.started_at'), finishedAt: requiredString(attempt.finished_at, 'attempt.finished_at'), stages: array(attempt.stages).map((stage) => { const item = object(stage); return { label: requiredString(item.label, 'stage.label'), status: requiredString(item.status, 'stage.status') }; }) };
    }),
    funnel: array(dossier.funnel).map((entry) => { const item = object(entry); return { label: requiredString(item.label, 'funnel.label'), value: requiredString(item.value, 'funnel.value') }; }),
    audienceNote: requiredString(dossier.audience_note, 'dossier.audience_note'),
    reviewStatus: requiredString(dossier.review_status, 'dossier.review_status'),
    reviewTone: tone(dossier.review_tone),
    planVersion: requiredString(dossier.plan_version, 'dossier.plan_version'),
    planStatus: requiredString(dossier.plan_status, 'dossier.plan_status'),
    planSource: requiredString(dossier.plan_source, 'dossier.plan_source'),
    targetCount: requiredString(dossier.target_count, 'dossier.target_count'),
    delivery: { sent: requiredString(delivery.sent, 'delivery.sent'), failed: requiredString(delivery.failed, 'delivery.failed'), retryable: requiredString(delivery.retryable, 'delivery.retryable'), rate: requiredString(delivery.rate, 'delivery.rate'), statusLabel: requiredString(delivery.status_label, 'delivery.status_label'), source: requiredString(delivery.source, 'delivery.source'), failureSummary: requiredString(delivery.failure_summary, 'delivery.failure_summary') },
    windows: array(dossier.windows).map((entry) => { const item = object(entry); return { label: requiredString(item.label, 'window.label'), statusLabel: requiredString(item.status_label, 'window.status_label'), tone: tone(item.tone), metrics: array(item.metrics).map((metric) => { const field = object(metric); return { label: requiredString(field.label, 'metric.label'), value: requiredString(field.value, 'metric.value'), desc: requiredString(field.desc, 'metric.desc') }; }), start: requiredString(item.start, 'window.start'), end: requiredString(item.end, 'window.end'), quality: requiredString(item.quality, 'window.quality'), limitation: requiredString(item.limitation, 'window.limitation') }; }),
    retro: { summary: requiredString(retro.summary, 'retro.summary'), detail: requiredString(retro.detail, 'retro.detail'), findings: stringList(retro.findings), limitations: stringList(retro.limitations) },
    next: { statusLabel: requiredString(next.status_label, 'next.status_label'), tone: tone(next.tone), summary: requiredString(next.summary, 'next.summary'), rationale: requiredString(next.rationale, 'next.rationale'), confirmedAt: requiredString(next.confirmed_at, 'next.confirmed_at'), appliedVersion: requiredString(next.applied_version, 'next.applied_version'), note: requiredString(next.note, 'next.note'), changes: stringList(next.changes) },
    references: array(dossier.references).map((entry) => { const item = object(entry); return { label: requiredString(item.label, 'reference.label'), desc: requiredString(item.desc, 'reference.desc') }; }),
  };
}

async function operationCyclesDb(context: AdminReadContext): Promise<AdminDb> {
  const db = emptyAdminDb();
  if (context.page === 'cyclesDetail') {
    const ordinal = Number(context.id || '');
    if (!Number.isSafeInteger(ordinal) || ordinal < 1 || ordinal > 999999999) throw new Error('运行档案编号无效或已失效');
    const result = await get(`/api/admin/operation-cycles/run-ordinals/${ordinal}`);
    if (Number(result.run_ordinal) !== ordinal) throw new Error('运行档案稳定编号校验失败');
    const snapshot = object(result.snapshot);
    if (snapshot.schema_version !== 'operation_cycle_snapshot.v1' || requiredString(result.run_key, 'run_key') !== requiredString(snapshot.run_key, 'snapshot.run_key')) throw new Error('运行档案响应未通过安全投影校验');
    db.cycleRuns[ordinal] = displayRun(ordinal, snapshot);
    return db;
  }
  const strategies = await listStrategies();
  loadedStrategies = strategies;
  db.cycleTasks = strategies.map((strategy, index) => ({
    id: index + 1,
    name: string(strategy.snapshot.name) || strategy.title,
    cron: string(strategy.snapshot.cron),
    dot: string(strategy.snapshot.dot),
    action: string(strategy.snapshot.action),
    runId: strategy.runOrdinal || index + 1,
    steps: array(strategy.snapshot.steps).map((entry) => { const step = object(entry); return { label: requiredString(step.label, 'step.label'), color: requiredString(step.color, 'step.color'), dim: step.dim === true }; }),
  }));
  return db;
}

const donorLoadDb = api.loadDb.bind(api);
api.loadDb = (context?: AdminReadContext): Promise<AdminDb> => {
  if (context?.page === 'cycles' || context?.page === 'cyclesDetail') return operationCyclesDb(context);
  return donorLoadDb(context);
};

// Dynamic import is deliberate: the binding must be installed before the
// unmodified donor entry reads document.body.dataset.page.
// @ts-expect-error The byte-frozen donor entry is a side-effect-only script.
void import('../src/admin/main');

function mountExcelOperations(): void {
  if (!['/admin/operation-cycles', '/admin/operation-cycles/', '/admin/cycles.html'].includes(location.pathname)) return;
  const stage = document.querySelector<HTMLElement>('main#stage');
  if (!stage) return;
  const mount = () => {
    if (!stage.children.length || stage.querySelector('.operation-excel-workspace')) return;
    // Keep the frozen shell's embedded header intact. The scroll region holds
    // the mutable donor table and is the V3-owned list/detail mount point.
    const parent = Array.from(stage.querySelectorAll<HTMLElement>('div')).find(
      node => node.style.overflow === 'auto',
    );
    if (parent) void mountOperationExcelWorkspace(parent).then(() => mountNativeStrategies(parent));
  };
  // The frozen donor renders asynchronously and may replace its stage on refresh.
  new MutationObserver(mount).observe(stage, { childList: true, subtree: true });
  mount();
}
if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mountExcelOperations, { once: true });
else mountExcelOperations();

function mountNativeStrategies(parent:HTMLElement):void {
 if(parent.querySelector('[data-standard-strategies]'))return;
 const section=document.createElement('section');section.className='card';section.dataset.standardStrategies='';section.style.cssText='padding:24px;margin:20px 0;border:1px solid #DEE0E3;border-radius:8px;background:white';
 const heading=document.createElement('h2');heading.textContent='定期生成待审核任务';
 const note=document.createElement('p');note.textContent='使用人群包已发布的 Agent 和发送人。启用后先准备一次，之后按北京时间整点、每日 08:00 或每周四 08:00 执行。生成结果需要人工审核，后台能力由管理员启用。';note.className='mini';
 const form=document.createElement('form');form.style.cssText='display:grid;gap:12px;max-width:640px';
 function field(label:string,node:HTMLElement){const l=document.createElement('label');l.textContent=label;l.style.cssText='display:grid;gap:6px';l.append(node);form.append(l);}
 const title=document.createElement('input');title.required=true;title.maxLength=80;title.placeholder='例如：每周客户需求跟进';field('策略名称',title);
 const audience=document.createElement('select');audience.required=true;field('已配置的人群包',audience);
 const interval=document.createElement('select');for(const [v,label] of [[24,'每天 08:00'],[168,'每周四 08:00'],[1,'每小时整点（测试）']] as const){const o=document.createElement('option');o.value=String(v);o.textContent=label;interval.append(o)}field('执行间隔',interval);
 const save=document.createElement('button');save.type='submit';save.className='btn primary';save.textContent='保存为草稿';
 const status=document.createElement('p');status.setAttribute('role','status');status.className='mini';
 const list=document.createElement('div');let editing:Strategy|undefined;let attempt='';
 form.append(save,status);section.append(heading,note,form,list);parent.prepend(section);
 for(const node of [title,audience,interval])node.style.cssText='padding:10px;border:1px solid #DEE0E3;border-radius:6px;font:inherit';
 async function load(){
  try{const p=await get('/api/admin/ai-audience/packages?limit=100&offset=0');audience.replaceChildren();const placeholder=document.createElement('option');placeholder.value='';placeholder.textContent='请选择人群包';audience.append(placeholder);for(const value of array(p.items)){const item=object(value);const o=document.createElement('option');o.value=String(item.id??item.package_id);o.textContent=string(item.name)||String(item.id);audience.append(o)}if(audience.options.length===1)status.textContent='请先在人群管理中配置人群、Agent 与发送人。';}
  catch{status.textContent='人群读取未成功，请刷新后重试。';save.disabled=true;}
  const strategies=await listStrategies();list.replaceChildren();for(const s of strategies.filter(x=>x.native&&x.status!=='archived')){
   const row=document.createElement('div');row.style.cssText='display:flex;gap:12px;align-items:center;flex-wrap:wrap;padding:14px 0;border-top:1px solid #EFF0F1';
   const text=document.createElement('span');text.textContent=`${s.title} · 每 ${s.native!.interval_hours} 小时 · ${s.status==='active'?'已启用':s.status==='paused'?'已暂停':'草稿'}`;
   const edit=document.createElement('button');edit.type='button';edit.className='btn ghost';edit.textContent='编辑';edit.onclick=()=>{editing=s;title.value=s.title;audience.value=String(s.native!.package_id);interval.value=String(s.native!.interval_hours);save.textContent='保存策略';attempt='';title.focus();};
   const toggle=document.createElement('button');toggle.type='button';toggle.className='btn ghost';toggle.textContent=s.status==='active'?'暂停':'启用';toggle.onclick=async()=>{toggle.disabled=true;try{await setOperationCycleStatus(s.strategyKey,s.version,s.status==='active'?'paused':'active',crypto.randomUUID());await load();}catch{status.textContent='状态更新未确认，请刷新核对。';toggle.disabled=false;}};
   const tasks=document.createElement('a');tasks.href='/admin/cloud-orchestrator/plans';tasks.textContent='查看待审核任务';row.append(text,edit,toggle,tasks);list.append(row);
  }
 }
 form.addEventListener('input',()=>{attempt='';});
 form.onsubmit=async(event)=>{event.preventDefault();if(!form.reportValidity())return;save.disabled=true;attempt ||= crypto.randomUUID();const definition:OperationCycleStrategyDefinitionInput={schedule:`每 ${interval.value} 小时生成待审核任务`,indicator_color:'#3370ff',primary_action:'start_review',stages:[{key:'review',label:'人工审核',color:'#3370ff',state:'pending'}],native_generation:{package_id:Number(audience.value),interval_hours:Number(interval.value)}};try{if(editing)await updateOperationCycleStrategy(editing.strategyKey,{expected_version:editing.version,title:title.value.trim(),definition},attempt);else await createOperationCycleStrategy({strategy_key:`standard-${attempt}`,title:title.value.trim(),definition},attempt);editing=undefined;attempt='';title.value='';save.textContent='保存为草稿';status.textContent='已保存。启用后仍需管理员开启后台执行，生成结果需要人工审核。';await load();}catch{status.textContent='保存未确认；可重试原内容，或刷新核对版本。';}finally{save.disabled=false;}};
 void load().catch(()=>{status.textContent='策略读取未成功，请刷新后重试。';});
}
