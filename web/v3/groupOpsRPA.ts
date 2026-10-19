type Request = (url: string, options?: Record<string, any>) => Promise<any>;
const packagePath = "/api/admin/automation-conversion/group-ops/rpa-package";
const states: Record<string,string> = {paused:"已暂停",waiting:"等待进入群聊",open_requested:"等待群 ID 核验",verified:"等待本地执行",granted:"本地执行中",outcome_unknown:"结果未知，已停止",final_failed:"发送失败，已停止",completed:"本次任务已完成",cancelled:"已取消"};
function el<K extends keyof HTMLElementTagNameMap>(tag: K, text = "", css = ""): HTMLElementTagNameMap[K] { const n = document.createElement(tag); n.textContent = text; n.className = css; return n; }
// Configuration belongs to the local Agent/CLI. This view has no write path.
export function mountRPASequence(root: HTMLElement, planID: string, request: Request): void {
  root.id = "rpa-sequence";
  let saved: any = null, release: any = null, busy = false, notice = "";
  const load = async () => {
    if (busy) return; busy = true; render();
    try { const view=await request(packagePath+"?plan_id="+encodeURIComponent(planID));saved=view.package;if(saved&&saved.plan_id!==planID)throw new Error();notice=""; }
    catch { saved=null;notice="任务包无法读取，请确认当前账号是计划负责人后重试。"; }
    try {
      const item=await request('/downloads/groupops/release.json');
      release=/^groupops-helper-[0-9]+\.[0-9]+\.[0-9]+-[a-f0-9]{12}\.dmg$/.test(item.filename) && /^[a-f0-9]{64}$/.test(item.sha256) ? item : null;
    } catch { release=null; }
    finally { busy=false;render(); }
  };
  function render() {
    const card=el("section","","group-ops__card");card.append(el("h3","本地 Agent 任务包","group-ops__section-title"));
    card.append(el("p","在本地与 Agent 对话完成配置，保存后即可由群运营助手执行。这里查看已保存的 Markdown 和执行结果。","group-ops__status"));
    const local=el("a","打开本地助手","group-ops__button");local.href="aicrm-groupops://connect";
    const setup=el("div","","group-ops__row-actions");setup.append(local);
    if(release){const download=el("a",release.signed&&release.notarized?"下载 Mac 助手":"下载内部测试版","group-ops__button");download.href='/downloads/groupops/'+release.filename;download.setAttribute('download','');setup.append(download);}
    for(const [label,href] of [["安装说明","/static/groupops/install.html"],["Agent 接入说明","/static/groupops/install.html#agent"]]){const link=el("a",label,"group-ops__button");link.href=href;link.target='_blank';link.rel='noopener';setup.append(link);}
    card.append(setup,el("p",release?`版本 ${release.version} · ${release.signed&&release.notarized?"已签名并公证":"内部测试，尚未完成正式签名与公证"}`:"安装包尚未发布，可先查看安装与 Agent 接入说明。","group-ops__status"));
    const status=el("p",notice||(busy?"正在读取…":saved?"任务包已保存":"尚未保存任务包。请让本地 Agent 通过 CLI 保存。"),"group-ops__status");status.setAttribute("role","status");card.append(status);
    if(saved){
      const run=saved.run;
      card.append(el("p",`任务包 ${saved.package_id} · ${new Date(saved.created_at).toLocaleString()}`,"group-ops__status"));
      if(run){card.append(el("p",`${run.plan_name} · ${states[run.state]||run.state} · ${run.cursor}/${run.targets.length} 群完成 · ${run.sequence?.receipts?.length||0} 条已记录`,"group-ops__status"));
        const receipts=el("ol");for(const receipt of run.sequence?.receipts||[]){const group=run.targets[receipt.group_index];receipts.append(el("li",`${group?.name||"群"} · 第 ${receipt.message_index+1} 条 · ${receipt.state==="client_executed"?"客户端已提交":states[receipt.state]||receipt.state}`));}card.append(receipts);
      }
      const details=el("details"),summary=el("summary","查看 Markdown");details.append(summary);const source=el("pre",saved.markdown);source.style.whiteSpace="pre-wrap";source.style.overflowWrap="anywhere";source.style.maxHeight="24rem";source.style.overflow="auto";details.append(source);card.append(details);
    }
    const refresh=el("button","刷新任务包与进度","group-ops__button");refresh.type="button";refresh.disabled=busy;refresh.addEventListener("click",()=>{void load();});card.append(refresh);root.replaceChildren(card);
  }
  void load();
}
