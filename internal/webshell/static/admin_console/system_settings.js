(() => {
  "use strict";
  const root = document.querySelector("[data-system-settings-root]");
  if (!root) return;
  const byID = (id) => root.querySelector(`#${id}`);
  const el = (tag, cls, text) => { const node = document.createElement(tag); if (cls) node.className = cls.includes("ss-button") ? `admin-button ${cls.includes("ss-primary") ? "admin-button--primary" : "admin-button--secondary"} ${cls}` : cls; if (text !== undefined) node.textContent = text; return node; };
  const groups = [
    { name: "账号与访问", keys: ["admin_access"] },
    { name: "企业微信", keys: ["wecom_base", "sidebar_identity", "wechat_oauth"] },
    { name: "AI与自动化", keys: ["ai_models", "ai_automation"] },
    { name: "支付与开票", keys: ["wechat_pay", "alipay", "wechat_shop"] },
    { name: "开放接口", keys: ["open_api_key", "api_token", "webhooks_push"] },
    { name: "运行设置", keys: ["reliability"] },
  ];
  let users = [], actor = {}, categories = [], selected = null, tab = 0, epoch = 0, busy = false, denied = false;
  const policies = new Map(), drafts = new Map(), failures = new Set();
  const query = () => byID("ss-search").value.trim().toLocaleLowerCase();
  const match = (value) => String(value || "").toLocaleLowerCase().includes(query());
  const policyURL = (id) => `/api/admin/access/users/${encodeURIComponent(String(id))}/cli-policy`;
  const message = (text, tone = "") => { const n = byID("ss-alert"); n.textContent = text; n.hidden = !text; n.className = `ss-alert${tone ? ` is-${tone}` : ""}`; };
  const cookie = () => { const p = document.cookie.split(";").map((s) => s.trim()).find((s) => s.startsWith("aicrm_admin_csrf=")); if (!p) return ""; try { return decodeURIComponent(p.slice(17)); } catch (_) { return ""; } };
  function revoke(status) {
    epoch++; denied = true; users = []; selected = null; policies.clear(); drafts.clear(); failures.clear();
    byID("ss-users").replaceChildren(); byID("ss-editor").replaceChildren(); byID("ss-categories").replaceChildren();
    byID("ss-accounts").hidden = true; byID("ss-search").disabled = true;
    byID("ss-tabs").replaceChildren();
    message(status === 401 ? "登录已失效，请重新登录。" : "仅超级管理员可以管理系统配置。", "error");
  }
  async function request(url, init = {}) {
    const controller = new AbortController();
    let timer;
    const task = (async () => {
      const response = await fetch(url, { ...init, credentials: "same-origin", cache: "no-store", signal: controller.signal,
        headers: { Accept: "application/json", ...(init.body === undefined ? {} : { "Content-Type": "application/json", "X-CSRF-Token": cookie() }), ...(init.headers || {}) } });
      if (response.status === 401 || response.status === 403) { revoke(response.status); throw new Error("访问已失效，请重新登录后继续。"); }
      if (!response.ok) throw Object.assign(new Error(response.status === 400 ? "IP 格式不正确，请检查后再保存。" : `配置请求失败（${response.status}），请重新读取后核对。`), { status: response.status });
      return response.json();
    })();
    try {
      return await Promise.race([task, new Promise((_, reject) => { timer = setTimeout(() => { controller.abort(); reject(new Error("请求超时，请重新读取后核对。")); }, 15000); })]);
    } finally { clearTimeout(timer); }
  }
  function validPolicy(p) { return p && Array.isArray(p.allowed_cidrs) && p.allowed_cidrs.every((v) => typeof v === "string") && Number.isSafeInteger(p.version) && p.version > 0; }
  // Canonical storage stays CIDR; host addresses should read like human IPs.
  function displayIP(value) {
    const v4 = /^(\d+\.\d+\.\d+\.\d+)\/32$/.exec(value);
    if (v4) return v4[1];
    if (value.includes(":") && value.endsWith("/128")) return value.slice(0, -4);
    return value;
  }
  async function readPolicy(id, versionEpoch) {
    const p = await request(policyURL(id));
    if (!validPolicy(p)) throw new Error("白名单响应不完整，请重新读取。");
    if (denied || versionEpoch !== epoch) return null;
    if ((policies.get(id)?.version || 0) > p.version) return policies.get(id);
    policies.set(id, p); failures.delete(id); renderTable();
    return p;
  }
  function visibleUsers() { return users.filter((u) => !query() || match("CLI 白名单 账号 访问") || match(`${u.display_name} ${u.username} ${u.admin_user_id}`)); }
  function renderTable() {
    const body = byID("ss-users"); body.replaceChildren();
    for (const user of visibleUsers()) {
      const id = user.admin_user_id, p = policies.get(id), row = el("tr", selected === id ? "is-selected" : "");
      const name = el("td"), label = el("label", "ss-select");
      const radio = document.createElement("input"); radio.type = "radio"; radio.name = "ss-account"; radio.checked = selected === id; radio.disabled = busy;
      radio.setAttribute("aria-label", `选择${user.display_name || user.username}（ID ${id}）`);
      radio.addEventListener("change", () => selectUser(id));
      label.append(radio, el("span", "", user.display_name || user.username || `账号 ${id}`)); name.append(label);
      row.append(name, el("td", "", user.username || `ID ${id}`), el("td", "", p ? (p.allowed_cidrs.length ? "限定来源 · 长期有效" : "24 小时") : failures.has(id) ? "读取失败" : "正在读取…"), el("td", "", p ? (p.allowed_cidrs.length ? `${p.allowed_cidrs.length} 项` : "未配置") : "—"));
      body.append(row);
    }
    if (!body.children.length) { const row = el("tr"), cell = el("td", "", "没有匹配的后台账号"); cell.colSpan = 4; row.append(cell); body.append(row); }
  }
  function renderSources(container, values) {
    container.replaceChildren();
    values.forEach((value, index) => {
      const row = el("div", "ss-source"), input = document.createElement("input");
      input.type = "text"; input.value = value; input.autocomplete = "off"; input.spellcheck = false; input.placeholder = "填写 IP，例如 115.190.113.5";
      input.setAttribute("aria-label", `允许访问的 IP ${index + 1}`);
      input.addEventListener("input", () => { values[index] = input.value; });
      const remove = el("button", "ss-text-button ss-remove", "删除"); remove.type = "button"; remove.setAttribute("aria-label", `删除第 ${index + 1} 个 IP`);
      remove.addEventListener("click", () => { values.splice(index, 1); renderSources(container, values); });
      row.append(input, remove); container.append(row);
    });
    if (!values.length) container.append(el("p", "", "未配置白名单，新 CLI Token 有效期为 24 小时。"));
  }
  function renderEditor() {
    const host = byID("ss-editor"); host.replaceChildren();
    const user = users.find((u) => u.admin_user_id === selected); if (!user) return;
    const id = selected, p = policies.get(id), account = el("div", "ss-account");
    account.append(el("h2", "", user.display_name || user.username || `账号 ${id}`), el("p", "", `后台账号：${user.username || "未设置密码账号"} · ID ${id}`)); host.append(account);
    if (!p) {
      host.append(el("p", "ss-placeholder", failures.has(id) ? "白名单读取失败，请重试；未读取前不能保存。" : "正在读取白名单…"));
      const retry = el("button", "ss-button", "重新读取"); retry.addEventListener("click", () => reload(id)); host.append(retry); return;
    }
    if (!drafts.has(id)) drafts.set(id, p.allowed_cidrs.map(displayIP));
    const values = drafts.get(id), form = el("form", "ss-form");
    form.append(el("h3", "", "编辑 CLI 白名单"), el("p", "", "填写允许访问的 IP；如需配置网段，也支持 IP 段。"));
    const sources = el("div", "ss-sources"); renderSources(sources, values); form.append(sources);
    const add = el("button", "ss-text-button", "添加 IP"); add.type = "button"; add.addEventListener("click", () => { values.push(""); renderSources(sources, values); sources.querySelector(".ss-source:last-child input")?.focus(); }); form.append(add);
    form.append(el("p", "ss-rules", "来源匹配时长期有效；清空后，新 Token 为 24 小时。"), el("div", "ss-warning", "保存后，该账号旧 CLI Token 将失效，需重新登录。"));
    const actions = el("div", "ss-actions"), save = el("button", "ss-button ss-primary", "保存白名单"); save.type = "submit";
    const refresh = el("button", "ss-button", "重新读取"); refresh.type = "button"; refresh.addEventListener("click", () => reload(id));
    actions.append(save, refresh, el("span", "ss-version", `配置版本：${p.version}（只读）`)); form.append(actions);
    form.addEventListener("submit", (event) => { event.preventDefault(); if (!busy) void savePolicy(id, values.slice()); }); host.append(form);
  }
  function lock(value) { busy = value; root.querySelectorAll("button,input").forEach((n) => { n.disabled = value || denied; }); }
  async function selectUser(id) {
    if (busy || denied) return;
    selected = id; message(""); renderTable(); renderEditor();
    if (!policies.has(id)) {
      try { await readPolicy(id, epoch); } catch (e) { if (!denied) { failures.add(id); message(e.message, "error"); } }
      if (!denied && selected === id) { renderTable(); renderEditor(); }
    }
  }
  async function reload(id) {
    if (busy || denied) return;
    lock(true); message("正在重新读取…");
    try { const p = await readPolicy(id, epoch); if (p) { drafts.delete(id); message("已重新读取当前配置。", "success"); renderEditor(); } }
    catch (e) { if (!denied) message(e.message, "error"); }
    finally { lock(false); }
  }
  const samePolicy = (a, b) => a.version === b.version && JSON.stringify(a.allowed_cidrs) === JSON.stringify(b.allowed_cidrs);
  async function savePolicy(id, values) {
    const clean = values.map((s) => s.trim());
    if (clean.some((s) => !s)) { message("请填写 IP，或删除空白行。", "error"); return; }
    lock(true); message("正在保存并确认配置…");
    let accepted = null;
    try {
      accepted = await request(policyURL(id), { method: "PUT", body: JSON.stringify({ allowed_cidrs: clean }) });
      if (!validPolicy(accepted)) throw new Error("保存响应不完整，正在核对当前配置。");
      const actual = await readPolicy(id, epoch);
      if (denied || !actual) return;
      if (!samePolicy(accepted, actual)) { message("保存后配置再次变化，请重新读取核对；保留你的编辑内容。", "error"); return; }
      drafts.delete(id); renderEditor(); message("白名单已保存并读回确认。该账号旧 CLI Token 已失效，请从允许来源重新登录。", "success");
    } catch (e) {
      if (denied) return;
      // One PUT only. An uncertain transport result is reconciled by GET,
      // never by resubmitting a policy (which would revoke another token).
      if (e.status && e.status < 500) { message(e.message, "error"); return; }
      try {
        const actual = await readPolicy(id, epoch);
        if (!denied && actual) message("保存结果尚未确认，已读取服务器当前配置。请核对后再决定是否保存；未自动重试。", "error");
      } catch (_) { if (!denied) message("保存结果未知，当前配置也未能读取。请稍后重新读取；未自动重试。", "error"); }
    } finally { lock(false); }
  }
  function showCategories() {
    const host = byID("ss-categories"); host.replaceChildren();
    const entries = categories.filter((c) => (query() || groups[tab].keys.includes(c.key)) && (!query() || match(`${c.label} ${c.group} ${(c.fields || []).map((f) => f.label).join(" ")}`)));
    if (tab === 5 && !query()) entries.push({ key: "releases", label: "配置发布与生效", managed_url: "/admin/config/releases", fields: [] });
    for (const cat of entries) {
      const row = el("div", "ss-category"), title = el("div"); title.append(el("h2", "", cat.label));
      title.append(el("p", "", cat.disabled || (cat.fields || []).slice(0, 3).map((f) => f.label).join("、") || "在对应管理页设置，沿用现有保存与生效规则。")); row.append(title);
      if (!cat.disabled) { const a = el("a", "ss-button ss-link", "配置"); a.href = cat.managed_url || `/admin/configDetail.html?cat=${encodeURIComponent(cat.key)}`; row.append(a); }
      host.append(row);
    }
    if (!entries.length) host.append(el("p", "ss-placeholder", query() ? "没有匹配的配置项。" : "正在读取配置目录…"));
  }
  function renderView() {
    if (denied) return;
    byID("ss-tabs").querySelectorAll("button").forEach((b, i) => { if (i === tab) b.setAttribute("aria-current", "page"); else b.removeAttribute("aria-current"); });
    const accountSearch = query() && visibleUsers().length > 0;
    byID("ss-accounts").hidden = tab !== 0 || (query() && !accountSearch);
    byID("ss-categories").hidden = tab === 0 && (!query() || accountSearch);
    renderTable(); showCategories();
  }
  for (const [i, group] of groups.entries()) { const b = el("button", "ss-tab", group.name); b.type = "button"; b.addEventListener("click", () => { if (!busy) { tab = i; byID("ss-search").value = ""; renderView(); } }); byID("ss-tabs").append(b); }
  byID("ss-search").addEventListener("input", renderView);
  async function start() {
    const currentEpoch = ++epoch;
    try {
      const listing = await request("/api/admin/access/users");
      if (denied || currentEpoch !== epoch) return;
      if (listing.actor?.role !== "super_admin") { revoke(403); return; }
      if (!Array.isArray(listing.users)) throw new Error("后台账号目录响应不完整。");
      actor = listing.actor;
      users = listing.users.filter((u) => u.authorized_account === true && Number.isSafeInteger(u.admin_user_id) && u.admin_user_id > 0);
      selected = users.find((u) => u.admin_user_id === actor.admin_user_id)?.admin_user_id || users[0]?.admin_user_id || null;
      byID("ss-loading").hidden = true; renderView(); renderEditor();
      // Bound directory reads; an account without a successful policy read is
      // never presented as having the default 24-hour policy.
      let index = 0;
      await Promise.all(Array.from({ length: Math.min(4, users.length) }, async () => {
        while (!denied && index < users.length) { const id = users[index++].admin_user_id;
          try { await readPolicy(id, currentEpoch); } catch (_) { if (!denied) failures.add(id); }
          if (!denied && currentEpoch === epoch) { renderTable(); if (selected === id && !busy) renderEditor(); }
        }
      }));
    } catch (e) { if (!denied) { byID("ss-loading").textContent = "账号读取失败，请刷新页面重试。"; message(e.message, "error"); } }
    if (denied) return;
    try {
      const catalog = await request("/api/admin/config/runtime-catalog");
      if (!Array.isArray(catalog.categories)) throw new Error("配置目录响应不完整。");
      if (denied || currentEpoch !== epoch) return;
      categories = catalog.categories.slice();
      if (!categories.some((c) => c.key === "ai_models")) categories.unshift({ key: "ai_models", label: "大模型设置", fields: [], managed_url: "/admin/configDetail.html?cat=ai_models" });
      renderView();
    } catch (e) { if (!denied) message(`配置目录读取失败：${e.message}`, "error"); }
  }
  void start();
})();
