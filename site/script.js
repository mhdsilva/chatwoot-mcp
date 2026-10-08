const tickets = {
  acme: { avatar: 'AL', name: 'Acme Logística', id: 'Conversa #9321', summary: 'Cliente precisa corrigir o endereço de um pedido que ainda não foi despachado.', history: '24 mensagens analisadas', priority: 'Urgente · risco de despacho', action: 'Confirmar novo endereço e sinalizar logística', tool: 'assign_conversation' },
  juliana: { avatar: 'JC', name: 'Juliana Costa', id: 'Conversa #9318', summary: 'Cliente quer entender valores e condições antes da renovação anual.', history: '11 mensagens analisadas', priority: 'Normal · decisão comercial', action: 'Atribuir para a equipe financeira', tool: 'assign_conversation' },
  norte: { avatar: 'MN', name: 'Mercado Norte', id: 'Conversa #9312', summary: 'Pagamento foi realizado, mas o boleto continua aparecendo em aberto.', history: '17 mensagens analisadas', priority: 'Alta · pagamento pendente', action: 'Registrar nota e solicitar comprovante', tool: 'add_private_note' },
  oficina: { avatar: 'OC', name: 'Oficina Central', id: 'Conversa #9309', summary: 'Cliente enviou imagens que mostram uma possível falha na peça recebida.', history: '31 mensagens e 3 anexos', priority: 'Alta · produto com defeito', action: 'Adicionar etiqueta de garantia', tool: 'add_conversation_labels' },
  beatriz: { avatar: 'BL', name: 'Beatriz Lima', id: 'Conversa #9297', summary: 'O acesso foi liberado e a equipe aguarda a confirmação da cliente.', history: '9 mensagens analisadas', priority: 'Normal · validação pendente', action: 'Manter status aguardando cliente', tool: 'set_conversation_status' },
  solar: { avatar: 'SE', name: 'Solar Engenharia', id: 'Conversa #9274', summary: 'A equipe precisa do número do pedido para localizar a solicitação comercial.', history: '14 mensagens analisadas', priority: 'Normal · falta informação', action: 'Aguardar retorno do cliente', tool: 'set_conversation_status' },
  lucas: { avatar: 'LM', name: 'Lucas Martins', id: 'Conversa #9251', summary: 'Proposta comercial enviada e pendente de aprovação pelo cliente.', history: '22 mensagens analisadas', priority: 'Normal · proposta enviada', action: 'Programar acompanhamento comercial', tool: 'add_private_note' }
};

const inspector = {
  avatar: document.querySelector('[data-inspector-avatar]'),
  name: document.querySelector('[data-inspector-name]'),
  id: document.querySelector('[data-inspector-id]'),
  summary: document.querySelector('[data-inspector-summary]'),
  history: document.querySelector('[data-inspector-history]'),
  priority: document.querySelector('[data-inspector-priority]'),
  action: document.querySelector('[data-inspector-action]'),
  tool: document.querySelector('[data-inspector-tool]')
};

const simulateButton = document.querySelector('[data-simulate]');

function selectTicket(key) {
  const ticket = tickets[key];
  if (!ticket) return;

  document.querySelectorAll('[data-ticket]').forEach((element) => {
    element.classList.toggle('selected', element.dataset.ticket === key);
  });

  Object.entries(inspector).forEach(([field, element]) => {
    element.textContent = ticket[field];
  });

  simulateButton.classList.remove('success');
  simulateButton.innerHTML = 'Simular próxima ação <span>→</span>';
}

document.querySelectorAll('[data-ticket]').forEach((ticket) => {
  ticket.addEventListener('click', () => selectTicket(ticket.dataset.ticket));
});

document.querySelectorAll('[data-filter]').forEach((filter) => {
  filter.addEventListener('click', () => {
    document.querySelectorAll('[data-filter]').forEach((item) => item.classList.toggle('active', item === filter));
    document.querySelectorAll('[data-ticket]').forEach((ticket) => {
      ticket.hidden = filter.dataset.filter !== 'all' && ticket.dataset.channel !== filter.dataset.filter;
    });
  });
});

simulateButton.addEventListener('click', () => {
  simulateButton.classList.add('success');
  simulateButton.textContent = 'Ação simulada com sucesso';
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
}, { threshold: .1 });

document.querySelectorAll('.reveal').forEach((element) => observer.observe(element));
