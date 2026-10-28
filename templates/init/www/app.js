const out = document.getElementById('out');

document.getElementById('call').addEventListener('click', async () => {
  out.textContent = '调用中…';
  try {
    const res = await window.berba.call('hello', {});
    out.textContent = JSON.stringify(res, null, 2);
  } catch (err) {
    out.textContent = '调用失败: ' + err.message;
  }
});
