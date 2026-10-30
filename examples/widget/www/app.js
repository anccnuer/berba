const out = document.getElementById('out');
const status = document.getElementById('status');
const title = document.getElementById('previewTitle');
const updated = document.getElementById('previewUpdated');
const cards = document.getElementById('previewCards');

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

function formatTime(ms) {
  if (!ms) return '';
  const date = new Date(ms);
  const pad = (n) => String(n).padStart(2, '0');
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** 渲染与设备小组件一致的卡片内容（桌面侧只做文本 + 颜色）。 */
function renderCards(sample) {
  const list = Array.isArray(sample) ? sample : (sample && sample.sample) || [];
  cards.innerHTML = '';
  list.slice(0, 5).forEach((card) => {
    const li = document.createElement('li');
    li.className = 'card';
    const label = document.createElement('span');
    label.className = 'card-label';
    label.textContent = card.title || '';
    const value = document.createElement('strong');
    value.className = 'card-value';
    value.textContent = card.value || '';
    if (card.color) value.style.color = card.color;
    li.append(label, value);
    if (card.subtitle) {
      const sub = document.createElement('span');
      sub.className = 'card-sub';
      sub.textContent = card.subtitle;
      li.append(sub);
    }
    cards.append(li);
  });
  if (list.length > 5) {
    const extra = document.createElement('li');
    extra.className = 'card extra';
    extra.textContent = `还有 ${list.length - 5} 张卡片未渲染`;
    cards.append(extra);
  }
}

async function refresh() {
  const res = await call('refresh', {});
  if (!res || !res.ok) return;
  const cached = res.cards || {};
  title.textContent = '今日概览';
  updated.textContent = formatTime(cached.updatedAt);
  renderCards(cached);
  status.textContent = `已推送 ${cached.count || 0} 张卡片 · ${formatTime(cached.updatedAt)}`;
  status.classList.remove('off');
}

async function loadState() {
  const state = await call('state', {});
  if (!state) return;
  const widgets = Array.isArray(state.widgets) ? state.widgets : [];
  status.textContent = widgets.length > 0
    ? `桌面已有 ${widgets.length} 个实例 · 定时任务 ${state.task.length} 个`
    : '还没有放置小组件：长按桌面添加后，点「立即刷新」可见内容';
  if (widgets.length === 0) status.classList.add('off');
  renderCards(state.cached);
  updated.textContent = formatTime(state.cached && state.cached.updatedAt);
}

document.getElementById('refresh').addEventListener('click', refresh);
document.getElementById('clear').addEventListener('click', () => call('clear', {}).then(loadState));
document.getElementById('schedule').addEventListener('click', () =>
  call('schedule', { name: 'widget-refresh', every: 900 }).then(loadState));
document.getElementById('reload').addEventListener('click', loadState);

window.berba.on('widget', (event) => {
  show({ 小组件刷新: event });
});

window.berba.on('widget-click', (event) => {
  show({ 点击: event });
  status.textContent = `点击了实例 ${event.id}（冷启动 ${event.coldStart}）`;
  renderCards(event.cards);
});

window.berba.on('task', (event) => {
  show({ 定时任务: event });
});

loadState();
