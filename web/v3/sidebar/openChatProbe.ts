// Temporary, user-triggered Mac navigation probe. No message or business write.
type Target = { chatId: string; name: string };
export type OpenChatProbeHost = {
  confirmGroup(): Promise<void>;
  openTestChat(chatId: string): Promise<string>;
};
const key = "aicrm.sidebar.open-test-chat.v1";
export const isProbeGroup = (name: string): boolean => name === "测试多群1" || name === "测试多群5";
function target(): Target | undefined {
  try {
    const value = JSON.parse(localStorage.getItem(key) || "null");
    if (value && isProbeGroup(value.name) && typeof value.chatId === "string" && value.chatId.trim()) return { chatId: value.chatId, name: value.name };
  } catch { /* Storage unavailable or malformed: require a fresh selection. */ }
}

export function openChatProbe(current: Target, host: OpenChatProbeHost): HTMLElement {
  const details = document.createElement("details");
  details.className = "open-chat-probe";
  const summary = document.createElement("summary"); summary.className = "mini"; summary.textContent = "打开测试群（不发消息）";
  const description = document.createElement("p"); description.className = "mini"; description.textContent = "记住此测试群后，切到另一个测试群再打开。只切换会话，不发送消息。";
  const info = document.createElement("p"); info.className = "mini group-binding-id";
  const status = document.createElement("p"); status.className = "status"; status.setAttribute("role", "status"); status.setAttribute("aria-live", "polite");
  const actions = document.createElement("div"); actions.className = "row-actions";
  const remember = document.createElement("button"), open = document.createElement("button"), clear = document.createElement("button");
  for (const button of [remember, open, clear]) { button.type = "button"; button.className = "btn ghost"; }
  remember.textContent = "记住此测试群"; open.textContent = "打开已记住的测试群"; clear.textContent = "清除本机记录";
  let busy = false;
  const refresh = () => {
    const saved = target(); info.textContent = saved ? "目标：" + saved.name + " · " + saved.chatId : "尚未记住测试群。";
    remember.disabled = busy; open.disabled = busy || !saved; clear.disabled = busy || !saved;
  };
  remember.addEventListener("click", async () => {
    if (busy || !isProbeGroup(current.name)) return;
    busy = true; refresh();
    try { await host.confirmGroup(); if (!details.isConnected) return; localStorage.setItem(key, JSON.stringify(current)); status.textContent = "已在本机记住「" + current.name + "」。"; }
    catch (error) { status.textContent = error instanceof Error ? error.message : "未能记住测试群。"; }
    finally { busy = false; refresh(); }
  });
  open.addEventListener("click", async () => {
    const saved = target(); if (busy || !saved || !isProbeGroup(current.name)) return;
    busy = true; status.textContent = "正在打开「" + saved.name + "」…"; refresh();
    try {
      await host.confirmGroup(); if (!details.isConnected) return;
      const result = await host.openTestChat(saved.chatId);
      status.textContent = "接口结果：" + result + "。请核对实际打开的群。";
    } catch (error) { status.textContent = error instanceof Error ? error.message : "打开结果未确认，请核对客户端。"; }
    finally { busy = false; refresh(); }
  });
  clear.addEventListener("click", () => {
    try { localStorage.removeItem(key); status.textContent = "已清除本机测试群记录。"; }
    catch { status.textContent = "本机存储不可用。"; }
    refresh();
  });
  actions.append(remember, open, clear); details.append(summary, description, info, actions, status); refresh(); return details;
}
