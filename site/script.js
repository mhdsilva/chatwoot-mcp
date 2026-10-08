const demos = {
  localizar: {
    prompt: 'Ache a conversa do cliente pelo telefone final 7021.',
    tool: 'search_contacts',
    result: 'Busca concluída · 1 contato',
    answer: 'Encontrei o contato e duas conversas. A mais recente está na caixa WhatsApp e aceita resposta.'
  },
  entender: {
    prompt: 'O que ficou combinado nessa conversa?',
    tool: 'get_conversation',
    result: 'Histórico lido · 15 mensagens',
    answer: 'O cliente confirmou o escopo e enviou um áudio por último. Há contexto suficiente para preparar o retorno.'
  },
  responder: {
    prompt: 'Envie a resposta que acabei de aprovar.',
    tool: 'send_reply',
    result: 'Aceita pela API · envio único',
    answer: 'A resposta foi aceita pelo Chatwoot. O ID e o estado retornados ficaram registrados.'
  }
};

const demoBody = document.querySelector('[data-demo-body]');
const demoTabs = document.querySelectorAll('[data-demo-tab]');

function renderDemo(key) {
  const demo = demos[key];
  demoBody.innerHTML = `
    <div class="message user-message">${demo.prompt}</div>
    <div class="tool-call"><span class="tool-icon">⌁</span><span><strong>${demo.tool}</strong><small>${demo.result}</small></span><span class="tool-check">✓</span></div>
    <div class="message agent-message"><span class="avatar">AI</span><p>${demo.answer}</p></div>
  `;
}

demoTabs.forEach((tab) => {
  tab.addEventListener('click', () => {
    demoTabs.forEach((item) => item.setAttribute('aria-selected', String(item === tab)));
    renderDemo(tab.dataset.demoTab);
  });
});

const header = document.querySelector('[data-header]');
const menuButton = document.querySelector('[data-menu-button]');
const menu = document.querySelector('[data-menu]');

window.addEventListener('scroll', () => header.classList.toggle('scrolled', window.scrollY > 18), { passive: true });
menuButton.addEventListener('click', () => {
  const open = menu.classList.toggle('open');
  menuButton.setAttribute('aria-expanded', String(open));
});
menu.querySelectorAll('a').forEach((link) => link.addEventListener('click', () => {
  menu.classList.remove('open');
  menuButton.setAttribute('aria-expanded', 'false');
}));

const observer = new IntersectionObserver((entries) => {
  entries.forEach((entry) => {
    if (entry.isIntersecting) {
      entry.target.classList.add('visible');
      observer.unobserve(entry.target);
    }
  });
}, { threshold: 0.13 });

document.querySelectorAll('.reveal').forEach((element) => observer.observe(element));
