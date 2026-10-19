// The frozen editor emits a select value attribute, which HTML does not use
// to select an option. Hydrate each rendered control once from its model;
// later user changes remain owned by the existing form/save handler.
export function installAutomationTypeHost(root: Document = document): void {
  const hydrated = new WeakSet<HTMLSelectElement>();
  const hydrate = () => {
    if (root.body?.dataset.page !== 'agentEdit') return;
    const select = root.querySelector<HTMLSelectElement>('select#agentType');
    if (!select || hydrated.has(select)) return;
    const type = select.getAttribute('value');
    if (type !== 'agent' && type !== 'fixed_script') return;
    select.value = type;
    hydrated.add(select);
  };
  new MutationObserver(hydrate).observe(root.documentElement, { childList: true, subtree: true });
  hydrate();
}

installAutomationTypeHost();
