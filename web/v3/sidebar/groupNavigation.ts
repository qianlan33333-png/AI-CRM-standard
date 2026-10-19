// Shared open-only projection for workbench and current-group verification.
// No browser storage, arbitrary target query, message API or autonomous loop.
export const navigationPath = "/api/sidebar/v2/group-navigation";
type Target = {group_asset_id: string; chat_id: string; name: string};
export type NavigationRun = {run_id: string; plan_id: string; plan_name: string; revision: number; state: string; cursor: number; targets: Target[]; observations: {step: number; chat_id: string}[]; sequence?: {messages: object[]; message_index: number; receipts: object[]; account_after?: string}};
type Plan = {plan_id: string; name: string; revision: number};
type View = {run: NavigationRun | null; plans: Plan[]};
export type NavigationHost = {
  requestNavigation(action?: string, body?: object, key?: string): Promise<any>;
  openNavigationTarget(chatId: string): Promise<void>;
  renewNavigationAccount?(challenge?: string): void;
};
function el<K extends keyof HTMLElementTagNameMap>(tag: K, text = "", css = ""): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag); node.textContent = text; node.className = css; return node;
}
const labels: Record<string,string> = {paused:"已暂停",waiting:"待打开",open_requested:"等待侧边栏核验",verified:"等待本地执行",granted:"本地执行中",outcome_unknown:"结果未知，已停止",final_failed:"发送失败，已停止",completed:"全部群已核验",cancelled:"已取消"};
const terminal = (r: NavigationRun) => r.state === "completed" || r.state === "cancelled";
const errorText = (e: unknown) => e instanceof Error ? e.message : "暂时无法读取，请刷新核对。";

export class GroupNavigationPage {
  private view?: View;
  private busy = false;
  private epoch = 0;
  private notice = "";
  private pendingStart?: {plan: Plan; key: string};
  constructor(private root: HTMLElement, private host: NavigationHost) {
    root.dataset.v3SidebarContext = "navigation";
    document.title = "群导航执行页";
    document.addEventListener("visibilitychange", () => {
      this.epoch++;
      if (document.visibilityState === "visible") void this.load();
      else { this.busy = true; this.render(); }
    });
    void this.load();
  }
  async load(): Promise<void> {
    const epoch = ++this.epoch; this.busy = true; this.render();
    try {
      const view = await this.host.requestNavigation() as View;
      if (epoch !== this.epoch) return;
      if (!Array.isArray(view.plans) || (view.run && (!Array.isArray(view.run.targets) || !Number.isInteger(view.run.cursor)))) throw new Error("导航投影无效，请刷新核对。");
      this.view = view;
      if (this.pendingStart && view.run?.plan_id === this.pendingStart.plan.plan_id) this.pendingStart = undefined;
    } catch (e) { if (epoch === this.epoch) { this.view = undefined; this.notice = errorText(e); } }
    finally { if (epoch === this.epoch) { this.busy = false; this.render(); } }
  }
  private button(label: string, action: () => void, primary = false) {
    const b = el("button", label, "btn " + (primary ? "primary" : "ghost")); b.type = "button"; b.disabled = this.busy; b.addEventListener("click", action); return b;
  }
  private async mutate(action: string, body: object, key?: string): Promise<void> {
    if (this.busy) return;
    const epoch = ++this.epoch; this.busy = true; this.notice = "正在保存当前步骤…"; this.render();
    try {
      const run = await this.host.requestNavigation(action, body, key) as NavigationRun;
      if (epoch !== this.epoch) return;
      this.view = {...this.view!, run}; this.pendingStart = undefined;
      if (action === "open") {
        const target = run.targets[run.cursor];
        if (run.state !== "open_requested" || !target?.chat_id) throw new Error("当前步骤不能打开，请刷新核对。");
        this.notice = "正在打开「" + target.name + "」。本地助手将自动继续。"; this.render();
        // The server saves open_requested BEFORE the WebView may disappear.
        await this.host.openNavigationTarget(target.chat_id);
        if (epoch === this.epoch) this.notice = "已收到 SDK 回调，等待本地助手继续。";
      } else this.notice = action === "start" ? "群清单已保存，当前暂停。" : "进度已保存。";
    } catch (e) {
      if (epoch === this.epoch) this.notice = errorText(e) + " 请刷新读取服务端进度；不会自动重复操作。";
    } finally { if (epoch === this.epoch) { this.busy = false; this.render(); } }
  }
  private render(): void {
    const panel = el("section", "", "panel group-binding-card"); panel.setAttribute("aria-label", "群导航执行");
    const head = el("div", "", "head"); head.append(el("h2", "群导航执行"), this.button("刷新进度", () => {void this.load();})); panel.append(head);
    panel.append(el("p", this.view?.run?.sequence ? "按任务包中的固定群 ID 顺序执行，由已连接的本地助手发送消息和素材。" : "按后台计划逐群打开并核验。本阶段只切群，不发送消息。", "mini"));
    const status = el("p", this.notice || (this.busy ? "正在读取已保存进度…" : ""), "status"); status.setAttribute("role", "status"); status.setAttribute("aria-live", "polite"); panel.append(status);
    const view = this.view;
    if (view) {
      const r = view.run;
      if (!r || terminal(r)) {
        const label = el("label", "运营计划", "group-binding-label"), select = el("select"); select.setAttribute("aria-label", "运营计划"); select.disabled = this.busy;
        const option = el("option", "请选择已配置群的计划"); option.value = ""; select.append(option);
        for (const p of view.plans) {const o = el("option", p.name); o.value = p.plan_id; select.append(o);}
        label.append(select); panel.append(label);
        const start = this.button("保存群清单", () => {
          const plan = view.plans.find(p => p.plan_id === select.value); if (!plan) return;
          const pending = this.pendingStart?.plan.plan_id === plan.plan_id ? this.pendingStart : {plan, key:crypto.randomUUID()}; this.pendingStart = pending;
          void this.mutate("start", {plan_id:plan.plan_id, plan_revision:plan.revision}, pending.key);
        }, true); start.disabled = true; select.addEventListener("change", () => {start.disabled = this.busy || !select.value;}); panel.append(start);
        if (!view.plans.length) panel.append(el("p", "请先在 CRM 群运营计划中配置由你负责的群。", "mini"));
      }
      if (r) {
        panel.append(el("h3", r.plan_name), el("p", `${r.sequence&&r.state==="completed"?"本次任务已完成":labels[r.state] || r.state} · ${r.cursor}/${r.targets.length} 群${r.sequence?"完成":"已核验"}`, "mini"));
        if(r.sequence) panel.append(el("p",`每群 ${r.sequence.messages.length} 条 · ${terminal(r)?"":`本群第 ${r.sequence.message_index+1} 条 · `}已记录 ${r.sequence.receipts.length} 条`,"mini"));
        const list = el("ol"); r.targets.forEach((t,i) => {const li=el("li",`${i<r.cursor?"已核验":i===r.cursor&&!terminal(r)?"当前":"待处理"} · ${t.name || "未命名群"}`); const id=el("details"),s=el("summary","查看群 ID","mini");id.append(s,el("p",t.chat_id,"mini group-binding-id"));li.append(id);list.append(li);});panel.append(list);
        if (!terminal(r)) {
          const actions=el("div","","row-actions");
          if (r.state === "paused") actions.append(this.button("继续导航",()=>{void this.mutate("resume",{run_id:r.run_id,revision:r.revision});},true));
          else if(r.state==="waiting"||r.state==="open_requested"||(r.sequence&&r.state==="verified")) {
            actions.append(this.button(r.state!=="waiting"?"重新打开本群":"打开当前群",()=>{void this.mutate("open",{run_id:r.run_id,revision:r.revision});},true));
            actions.append(this.button("暂停",()=>{void this.mutate("pause",{run_id:r.run_id,revision:r.revision});}));
          }
          if(!r.sequence || !["granted","outcome_unknown","paused"].includes(r.state)) actions.append(this.button("结束本次导航",()=>{void this.mutate("cancel",{run_id:r.run_id,revision:r.revision});}));panel.append(actions);
          panel.append(el("p",r.sequence?"切群后本地助手会自动继续，进度已保存在云端。":"切群后可重新进入本页继续。SDK 回调不作为已进入目标群的证明。","mini"));
        }
      }
    }
    this.root.replaceChildren(panel);
  }
}

export function navigationVerification(host: Pick<NavigationHost,"requestNavigation"|"renewNavigationAccount">, currentGroup: () => Promise<string>): HTMLElement {
  const root=el("section","","group-navigation-verification");
  // A single mount handles the current server projection. It neither opens a
  // group nor sends a message, and has no polling loop or browser queue.
  queueMicrotask(async()=>{
    const active=()=>root.isConnected && document.visibilityState!=="hidden";
    if(!active())return;
    try {
      const view=await host.requestNavigation() as View;if(!active())return;
      const r=view.run;
      if(!r?.sequence) {root.replaceChildren(manualNavigationVerification(host,currentGroup));return;}
      if(!["open_requested","verified"].includes(r.state))return;
      const status=el("p","正在连接本地助手…","status");status.setAttribute("role","status");root.replaceChildren(status);
      const actual=await currentGroup();if(!active())return;
      if(!actual || actual!==r.targets[r.cursor]?.chat_id)throw new Error("当前会话与任务目标不符，已暂停。");
      const after=r.sequence.account_after;
      if(!after || !Number.isFinite(Date.parse(after)) || Date.parse(after)<=0)throw new Error("等待本地助手接管任务。");
      const challenge=`${r.run_id}:${r.cursor}:${after}`;
      if(new URL(window.location.href).searchParams.get("rpa_account")!==challenge) {
        if(!host.renewNavigationAccount)throw new Error("无法恢复执行账号，请重新打开侧边栏。");
        host.renewNavigationAccount(challenge);return;
      }
      // The marker only avoids another redirect. The existing server confirms
      // the authenticated OAuth staff/sender and issued-at against AccountAfter.
      const result=await host.requestNavigation("confirm",{run_id:r.run_id,revision:r.revision,step:r.cursor,chat_id:actual}) as NavigationRun;
      if(!active())return;
      if(result.run_id!==r.run_id || result.cursor!==r.cursor || result.state!=="verified")throw new Error("任务进度已变化，已暂停。");
      root.replaceChildren(el("p","本地助手已就绪","status"),el("p",actual,"mini group-binding-id"));
      root.firstElementChild?.setAttribute("role","status");
    } catch(e) {if(active()){const status=el("p",errorText(e),"status");status.setAttribute("role","status");root.replaceChildren(status);}}
  });
  return root;
}

function manualNavigationVerification(host: Pick<NavigationHost,"requestNavigation"|"renewNavigationAccount">, currentGroup: () => Promise<string>): HTMLElement {
  const details=el("details","","group-navigation-verification"),summary=el("summary","核验本次群导航","mini"),button=el("button","读取并核验当前群","btn ghost"),status=el("p","","status");button.type="button";status.setAttribute("role","status");
  let busy=false;
  button.addEventListener("click",async()=>{
    if(busy)return;busy=true;button.disabled=true;status.textContent="正在核验 SDK 当前群…";
    try {
      const view=await host.requestNavigation() as View;if(!details.isConnected || document.visibilityState==="hidden")return;
      const r=view.run;if(!r || (r.state!=="open_requested"&&!(r.sequence&&r.state==="verified")))throw new Error("当前没有等待核验的导航步骤。");
      const actual=await currentGroup();if(!details.isConnected || document.visibilityState==="hidden")return;
      if(actual!==r.targets[r.cursor]?.chat_id)throw new Error("实际群 ID 与本步目标不符，已停止；不会推进。");
      const result=await host.requestNavigation("confirm",{run_id:r.run_id,revision:r.revision,step:r.cursor,chat_id:actual}) as NavigationRun;
      if(details.isConnected)status.textContent=result.sequence ? `当前群 ID 已核验。等待本地助手执行第 ${result.sequence.message_index+1} 条。` : `已核验 ${result.cursor}/${result.targets.length} 群。请重新进入工作台执行页继续。`;
    }catch(e){if(details.isConnected)status.textContent=errorText(e);}
    finally{busy=false;button.disabled=false;}
  });
  details.append(summary);
  if(typeof host.renewNavigationAccount === "function") {const account=el("button","验证执行账号","btn ghost");account.type="button";account.addEventListener("click",()=>{host.renewNavigationAccount?.();});details.append(account);}
  details.append(button,status);return details;
}
