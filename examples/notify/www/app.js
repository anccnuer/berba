const out = document.getElementById('out');
const status = document.getElementById('status');
const titleInput = document.getElementById('title');
const bodyInput = document.getElementById('body');

function show(value) {
  out.textContent = typeof value === 'string' ? value : JSON.stringify(value, null, 2);
}

async function call(name, args) {
  show('调用中…');
  try {
    const res = await window.berba.call(name, args);
    show(res);
    return res;
  } catch (err) {
    show('调用失败: ' + err.message);
    return null;
  }
}

async function refresh() {
  const state = await call('state', {});
  if (!state) return;
  status.textContent = state.enabled
    ? '通知可用'
    : '通知不可用：请先授予通知权限';
  status.classList.toggle('off', !state.enabled);
}

window.berba.on('permission', (event) => {
  show({ permission: event });
  refresh();
});

window.berba.on('notification', (event) => {
  show({ notification: event });
});

document.getElementById('permission').addEventListener('click', () => {
  call('requestPermission', {});
});

document.getElementById('show').addEventListener('click', async () => {
  await call('show', {});
  refresh();
});

document.getElementById('showCustom').addEventListener('click', async () => {
  await call('show', {
    title: titleInput.value,
    body: bodyInput.value,
    text: '自定义通知载荷',
    id: 'demo'
  });
  refresh();
});

document.getElementById('cancel').addEventListener('click', () => {
  call('cancel', { id: 'demo' });
});

document.getElementById('cancelAll').addEventListener('click', () => {
  call('cancelAll', {});
});

refresh();
