export const pairPath = "/api/admin/access/rpa-pair";
export function mountRPAPair(root: HTMLElement, request: (path: string, body: object) => Promise<any>): void {
  if(new URL(location.href).searchParams.has("rpa_pair"))document.title = "连接群运营助手";
  const panel = document.createElement("section"); panel.className = "group-ops__card group-ops__rpa-pair";
  const title = document.createElement("h2"); title.className="group-ops__section-title";title.textContent = "连接群运营助手";
  const hint = document.createElement("p"); hint.className = "group-ops__status"; hint.textContent = "连接后仅可执行由你负责的群运营任务，首次保持暂停。连接有效期 90 天，可在本地设置断开。";
  const open=document.createElement("a");open.className="group-ops__button";open.textContent="打开本地助手";open.href="aicrm-groupops://connect";
  const label = document.createElement("label"); label.className = "group-ops__field";const caption=document.createElement('span');caption.textContent="连接码";label.append(caption);
  const code = document.createElement("input"); code.setAttribute("aria-label", "连接码"); code.autocomplete = "off"; code.spellcheck = false;
  code.value = new URL(location.href).searchParams.get("rpa_pair") || ""; label.append(code);
  const status = document.createElement("p"); status.className = "group-ops__status"; status.setAttribute("role", "status");
  const inspect = document.createElement("button"); inspect.className = "group-ops__button"; inspect.textContent = "查看待连接设备";
  const approve = document.createElement("button"); approve.className = "group-ops__button group-ops__button--primary"; approve.textContent = "连接这台 Mac"; approve.hidden = true;
  const actions = document.createElement("div"); actions.className = "group-ops__row-actions"; actions.append(inspect, approve);
  let previewed = "", busy = false;
  const normalized = () => code.value.trim().toUpperCase().replaceAll("-", "");
  const act = async (confirm: boolean) => {
    if (busy || (confirm && previewed !== normalized())) return;
    const submitted=normalized();
    busy = true; inspect.disabled = approve.disabled = true;
    try {
      const result = await request(pairPath + (confirm ? "/approve" : "/preview"), {user_code: submitted});
      if(submitted!==normalized()){approve.hidden=true;previewed="";status.textContent="连接码已变化，请重新查看设备。";return;}
      if (confirm || result.state === "active") { status.textContent = "已连接「" + result.device_name + "」。请回到本地助手点击启用。"; approve.hidden = true; code.disabled = true; inspect.hidden = true; }
      else { previewed = submitted; status.textContent = "待连接设备：" + result.device_name; approve.hidden = false; }
    } catch { status.textContent = "连接码无效或已过期，请在本地助手重新连接。"; approve.hidden = true; previewed = ""; }
    finally { busy = false; inspect.disabled = approve.disabled = false; }
  };
  inspect.addEventListener("click", () => { void act(false); }); approve.addEventListener("click", () => { void act(true); });
  code.addEventListener("input", () => { approve.hidden = true; previewed = ""; });
  panel.append(title, hint,open, label, actions, status); root.replaceChildren(panel);
  if (code.value) void act(false);
}
