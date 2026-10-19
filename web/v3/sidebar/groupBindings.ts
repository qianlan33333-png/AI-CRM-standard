// Current-group presentation uses the existing sidebar cards and controls.
// The Host owns SDK identity, request cancellation and the employee session.
import { isProbeGroup, openChatProbe, type OpenChatProbeHost } from "./openChatProbe";
import { navigationVerification, type NavigationHost } from "./groupNavigation";
type Plan = { plan_id: string; name: string; status: string; revision: number };
type Context = { chat_id: string; name: string; owner_name: string; checked_at: string; bindings: Plan[]; candidates: Plan[]; total: number; limit: number; offset: number };
type Host = OpenChatProbeHost & {
  request(path: string, body: object, key?: string): Promise<any>;
  confirmGroup(): Promise<void>;
  refresh(): Promise<void>;
  navigation?: Pick<NavigationHost,"requestNavigation"|"renewNavigationAccount"> & {currentGroup(): Promise<string>};
};
const pending = new Map<string, { key: string; revision: number }>();
const states: Record<string, string> = { draft: "草稿", paused: "已暂停", active: "已启用" };
function element<K extends keyof HTMLElementTagNameMap>(tag: K, value = "", css = ""): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag); node.textContent = value; node.className = css; return node;
}

export class GroupBindingCard {
  private epoch = 0;
  private disposed = false;
  private busy = false;
  private snapshot?: Context;
  private query = "";
  private notice = "";
  private readonly panel = element("section", "", "panel group-binding-card");
  private readonly top: HTMLElement | null;
  constructor(private root: HTMLElement, private chat: string, private host: Host) {
    this.top = root.querySelector<HTMLElement>(".top");
    if (this.top) this.top.hidden = true;
    root.dataset.v3SidebarContext = "group";
    this.panel.setAttribute("aria-label", "当前群业务绑定");
    root.querySelector("#content")?.replaceChildren(this.panel);
  }
  dispose(): void { this.disposed = true; this.epoch++; this.panel.remove(); if (this.top) this.top.hidden = false; }
  async load(query = this.query, offset = 0): Promise<void> {
    if (this.disposed) return;
    const epoch = ++this.epoch;
    this.busy = true; this.query = query;
    this.render();
    try {
      await this.host.confirmGroup();
      const data = await this.host.request("/api/sidebar/v2/group-ops/context", { chat_id: this.chat, q: query, limit: 50, offset }) as Context;
      if (this.disposed || epoch !== this.epoch) return;
      if (data.chat_id !== this.chat || !Array.isArray(data.bindings) || !Array.isArray(data.candidates)) throw new Error("当前群读取结果不一致，请重新识别。");
      this.snapshot = data;
      for (const plan of data.bindings) {
        if (pending.delete(this.chat + ":" + plan.plan_id)) this.notice = "已核对，当前群已绑定到「" + plan.name + "」。";
      }
    } catch (error) {
      if (this.disposed || epoch !== this.epoch) return;
      this.snapshot = undefined;
      this.notice = error instanceof Error ? error.message : "群信息暂不可用，请重新识别。";
    } finally {
      if (!this.disposed && epoch === this.epoch) { this.busy = false; this.render(); }
    }
  }
  private button(label: string, action: () => void, primary = false): HTMLButtonElement {
    const node = element("button", label, "btn " + (primary ? "primary" : "ghost"));
    node.type = "button"; node.disabled = this.busy; node.addEventListener("click", action); return node;
  }
  private render(): void {
    if (this.disposed) return;
    const header = element("div", "", "head");
    header.append(element("h2", "当前群业务绑定"), this.button("重新识别", () => { void this.host.refresh(); }));
    const nodes: Node[] = [header];
    const status = element("p", this.busy ? "正在核对当前群…" : this.notice, "status");
    status.setAttribute("role", "status"); status.setAttribute("aria-live", "polite");
    if (status.textContent) nodes.push(status);
    const data = this.snapshot;
    if (data) {
      nodes.push(element("h3", data.name || "未设置群名"));
      nodes.push(element("p", "群主：" + (data.owner_name || "当前账号"), "mini"));
      const details = element("details"), summary = element("summary", "查看群 ID", "mini");
      details.append(summary, element("p", this.chat, "mini group-binding-id")); nodes.push(details);
      nodes.push(element("h3", "已有绑定"));
      if (!data.bindings.length) nodes.push(element("p", "当前群尚未绑定运营计划。", "mini"));
      for (const plan of data.bindings) nodes.push(element("p", plan.name + " · " + (states[plan.status] || plan.status), "mini"));
      const search = element("form", "", "group-binding-search");
      const input = element("input"); input.type = "search"; input.value = this.query; input.placeholder = "搜索运营计划"; input.setAttribute("aria-label", "搜索运营计划"); input.disabled = this.busy;
      const searchButton = this.button("搜索", () => {}); searchButton.type = "submit";
      search.addEventListener("submit", (event) => { event.preventDefault(); if (!this.busy) void this.load(input.value.trim()); });
      search.append(input, searchButton); nodes.push(search);
      const label = element("label", "选择运营计划", "group-binding-label"), select = element("select");
      select.setAttribute("aria-label", "选择运营计划"); select.disabled = this.busy;
      const empty = element("option", data.candidates.length ? "请选择计划" : "没有可绑定的计划"); empty.value = ""; select.append(empty);
      for (const plan of data.candidates) { const option = element("option", plan.name + " · " + (states[plan.status] || plan.status)); option.value = plan.plan_id; select.append(option); }
      label.append(select); nodes.push(label);
      const save = this.button("绑定当前群", () => {
        const plan = data.candidates.find((p) => p.plan_id === select.value);
        if (plan && !this.busy) void this.bind(plan);
      }, true);
      save.disabled = true;
      select.addEventListener("change", () => { save.disabled = this.busy || !select.value; save.textContent = pending.has(this.chat + ":" + select.value) ? "重试原绑定请求" : "绑定当前群"; });
      nodes.push(save);
      const paging = element("div", "", "row-actions");
      const prev = this.button("上一页", () => { void this.load(this.query, Math.max(0, data.offset - data.limit)); }); prev.disabled ||= data.offset === 0;
      const next = this.button("下一页", () => { void this.load(this.query, data.offset + data.limit); }); next.disabled ||= data.offset + data.candidates.length >= data.total;
      if (data.total > data.limit || data.offset > 0) { paging.append(prev, element("span", "共 " + data.total + " 个可绑定计划", "mini"), next); nodes.push(paging); }
      nodes.push(element("p", "仅显示由你负责的草稿、暂停计划。绑定后计划状态保持不变。", "mini"));
      nodes.push(element("p", "核对时间：" + new Date(data.checked_at).toLocaleString("zh-CN", { timeZone: "Asia/Shanghai" }), "mini"));
      if (isProbeGroup(data.name)) nodes.push(openChatProbe({ chatId: this.chat, name: data.name }, this.host));
      if (this.host.navigation && !this.busy) nodes.push(navigationVerification(this.host.navigation, () => this.host.navigation!.currentGroup()));
    }
    this.panel.replaceChildren(...nodes);
  }
  private async bind(plan: Plan): Promise<void> {
    this.busy = true;
    const epoch = ++this.epoch;
    const scope = this.chat + ":" + plan.plan_id;
    const attempt = pending.get(scope) || { key: crypto.randomUUID(), revision: plan.revision };
    pending.set(scope, attempt);
    this.notice = "正在保存「" + plan.name + "」的群绑定…"; this.render();
    try {
      await this.host.confirmGroup();
      await this.host.request("/api/sidebar/v2/group-ops/bind", { chat_id: this.chat, plan_id: plan.plan_id, expected_revision: attempt.revision }, attempt.key);
      if (this.disposed || epoch !== this.epoch) return;
      // Keep the original attempt until the following context read proves the
      // binding. A lost response or readback is never an automatic new write.
      this.notice = "保存已受理，正在核对绑定结果…";
    } catch (error) {
      if (this.disposed || epoch !== this.epoch) return;
      const status = Number((error as { status?: number }).status || 0);
      if (status >= 400 && status < 500) pending.delete(scope);
      this.notice = status === 409 ? "计划已更新，正在重新读取；请重新选择后再操作。" : status >= 400 && status < 500 ? (error as Error).message : "绑定结果待确认，请重新识别核对；未确认前仅可重试原请求。";
    }
    if (!this.disposed && epoch === this.epoch) { this.busy = false; await this.load(this.query); }
  }
}
