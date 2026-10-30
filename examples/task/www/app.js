const out = document.getElementById('out');
const status = document.getElementById('status');
const exactInput = document.getElementById('exact');

function show(value) {
  out.textContent = typeof value === 'string' ? value : JSON.stringify(value, null, 2);
}

function formatTime(seconds) {
  if (!seconds) return '—';
  const date = new Date(seconds * 1000);
  const pad = (n) => String(n).padStart(2, '0');
  return `${date.getMonth() + 1}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
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
  const tasks = Array.isArray(state.tasks) ? state.tasks : [];
  status.textContent = state.exact
    ? `精确闹钟可用 · ${tasks.length} 个任务`
    : `精确闹钟未授权，将按非精确调度 · ${tasks.length} 个任务`;
  status.classList.toggle('off', !state.exact);
  if (tasks.length === 0) {
    out.textContent = '还没有任务。';
    return;
  }
  out.textContent = tasks.map((task) => {
    const rule = task.kind === 'every' ? `每 ${task.every} 秒`
      : task.kind === 'daily' ? `每天 ${task.daily}` : `延时 ${task.delay} 秒`;
    return `${task.name} · ${rule} · 下次 ${formatTime(task.nextRun)} · runId ${task.runId}`;
  }).join('\n');
}

function schedule(mode, extra) {
  call('schedule', Object.assign({ mode, exact: exactInput.checked }, extra))
    .then(refresh);
}

document.getElementById('every').addEventListener('click', () => schedule('every', { every: 900 }));
document.getElementById('daily').addEventListener('click', () => schedule('daily', { daily: '08:30' }));
document.getElementById('delay').addEventListener('click', () => schedule('delay', { delay: 60 }));
document.getElementById('cancel').addEventListener('click', () => call('cancel', { name: 'demo' }).then(refresh));
document.getElementById('refresh').addEventListener('click', refresh);

window.berba.on('task', (event) => {
  show({ task: event });
  refresh();
});

refresh();
