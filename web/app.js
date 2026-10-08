(() => {
  const csrfToken = document.querySelector('meta[name="csrf-token"]')?.content || '';
  const $ = (id) => document.getElementById(id);

  async function requestJSON(path, options = {}) {
    const response = await fetch(path, {
      credentials: 'same-origin',
      headers: { Accept: 'application/json', ...(options.headers || {}) },
      ...options,
    });
    const text = await response.text();
    let data = {};
    if (text) {
      try { data = JSON.parse(text); } catch { throw new Error('O servidor retornou uma resposta inválida.'); }
    }
    if (!response.ok) {
      throw new Error(data.message || data.error || `Não foi possível concluir a solicitação (${response.status}).`);
    }
    return data;
  }

  function setLoading(loadingId, contentId, errorId, isLoading) {
    $(loadingId).hidden = !isLoading;
    if (isLoading) {
      $(contentId).hidden = true;
      $(errorId).hidden = true;
    }
  }

  function readableError(error) {
    return error instanceof Error ? error.message : 'Ocorreu um erro inesperado.';
  }

  async function loadStatus() {
    setLoading('status-loading', 'status-content', 'status-error', true);
    try {
      const result = await requestJSON('/api/status');
      const connected = result.connected === true;
      const configured = result.configured === true || result.has_token === true;
      const statusMessage = typeof result.message === 'string' ? result.message.trim() : '';
      $('connection-dot').classList.toggle('is-connected', connected);
      $('connection-label').textContent = connected
        ? 'Conectado'
        : statusMessage || (configured ? 'Configuração salva · conexão não verificada' : 'Ainda não configurado');
      $('status-url').textContent = result.base_url || '—';
      $('status-account').textContent = result.account_name || (result.account_id ? `ID ${result.account_id}` : '—');
      $('status-user').textContent = result.user_name || '—';
      $('status-token').textContent = result.has_token === true ? 'Sim' : 'Não';
      $('status-tested').textContent = result.last_checked || result.last_tested || '—';
      $('status-content').hidden = false;
      $('status-error').hidden = true;
    } catch (error) {
      $('status-loading').hidden = true;
      $('status-error').textContent = readableError(error);
      $('status-error').hidden = false;
    }
  }

  async function loadClientConfig() {
    setLoading('client-loading', 'client-content', 'client-error', true);
    try {
      const result = await requestJSON('/api/client-config');
      const config = result.config ?? result.json ?? result;
      $('client-config').textContent = typeof config === 'string' ? config : JSON.stringify(config, null, 2);
      $('copy-button').disabled = false;
      $('client-content').hidden = false;
      $('client-error').hidden = true;
    } catch (error) {
      $('client-loading').hidden = true;
      $('client-error').textContent = readableError(error);
      $('client-error').hidden = false;
    }
  }

  async function loadTools() {
    setLoading('tools-loading', 'tools-list', 'tools-error', true);
    try {
      const result = await requestJSON('/api/tools');
      const tools = Array.isArray(result) ? result : (result.tools || []);
      const list = $('tools-list');
      list.replaceChildren();
      for (const tool of tools) {
        const item = document.createElement('li');
        const name = document.createElement('strong');
        const description = document.createElement('span');
        name.textContent = typeof tool === 'string' ? tool : (tool.name || 'Ferramenta');
        description.textContent = typeof tool === 'string' ? '' : (tool.description || '');
        item.append(name, description);
        list.append(item);
      }
      if (tools.length === 0) {
        const item = document.createElement('li');
        item.textContent = 'Nenhuma ferramenta disponível.';
        list.append(item);
      }
      list.hidden = false;
      $('tools-error').hidden = true;
    } catch (error) {
      $('tools-loading').hidden = true;
      $('tools-error').textContent = readableError(error);
      $('tools-error').hidden = false;
    }
  }

  $('setup-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const form = event.currentTarget;
    const errorBox = $('setup-error');
    const successBox = $('setup-success');
    const button = $('save-button');
    const tokenField = $('token');
    errorBox.hidden = true;
    successBox.hidden = true;
    if (!form.reportValidity()) return;

    button.disabled = true;
    button.setAttribute('aria-busy', 'true');
    button.querySelector('.button-label').textContent = 'Testando conexão…';
    button.querySelector('.button-spinner').hidden = false;
    try {
      const result = await requestJSON('/api/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          base_url: $('base-url').value.trim(),
          account_id: Number($('account-id').value),
          token: tokenField.value,
        }),
      });
      successBox.textContent = result.message || 'Conexão testada e configuração salva.';
      successBox.hidden = false;
      tokenField.value = '';
      await Promise.all([loadStatus(), loadClientConfig()]);
    } catch (error) {
      errorBox.textContent = readableError(error);
      errorBox.hidden = false;
      tokenField.value = '';
    } finally {
      button.disabled = false;
      button.removeAttribute('aria-busy');
      button.querySelector('.button-label').textContent = 'Testar e salvar conexão';
      button.querySelector('.button-spinner').hidden = true;
    }
  });

  $('copy-button').addEventListener('click', async () => {
    const status = $('copy-status');
    try {
      await navigator.clipboard.writeText($('client-config').textContent);
      status.textContent = 'Configuração copiada.';
    } catch {
      status.textContent = 'Não foi possível copiar. Selecione e copie o bloco manualmente.';
    }
  });

  async function installClient(client, label) {
    const status = $('install-status');
    const confirmed = window.confirm(`Adicionar o servidor "chatwoot" ao ${label}? Um backup da configuração atual será criado.`);
    if (!confirmed) return;
    status.textContent = 'Configurando…';
    try {
      const result = await requestJSON('/api/configure-client', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ client, confirm: true }),
      });
      status.textContent = result.message || 'Cliente configurado.';
    } catch (error) {
      status.textContent = readableError(error);
    }
  }

  $('install-claude')?.addEventListener('click', () => installClient('claude', 'Claude Desktop'));
  $('install-codex')?.addEventListener('click', () => installClient('codex', 'Codex'));

  Promise.all([loadStatus(), loadClientConfig(), loadTools()]);
})();
