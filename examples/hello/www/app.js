const out = document.getElementById('out');

async function call(name, args) {
  out.textContent = '调用中…';
  try {
    const res = await window.berba.call(name, args);
    out.textContent = JSON.stringify(res, null, 2);
    return res;
  } catch (err) {
    out.textContent = '调用失败: ' + err.message;
    return null;
  }
}

document.getElementById('add').addEventListener('click', () => {
  call('add', { a: 2, b: 40 });
});

document.getElementById('device').addEventListener('click', () => {
  call('deviceInfo', {});
});

document.getElementById('increment').addEventListener('click', async () => {
  const res = await call('increment', {});
  if (res) document.title = 'Hello berba (' + res.count + ')';
});
