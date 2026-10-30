const out = document.getElementById('out');
const nameInput = document.getElementById('name');

function show(value) {
  out.textContent = JSON.stringify(value, null, 2);
}

async function call(bridge, args) {
  out.textContent = '调用中…';
  try {
    const res = await window.berba.call(bridge, args);
    show(res);
    return res;
  } catch (err) {
    out.textContent = '调用失败: ' + err.message;
    return null;
  }
}

window.berba.on('nativeResult', (event) => {
  show(event);
});

document.getElementById('greet').addEventListener('click', () => {
  call('greet', { name: nameInput.value });
});

document.getElementById('greetLater').addEventListener('click', () => {
  call('greetLater', { name: nameInput.value });
});

document.getElementById('list').addEventListener('click', () => {
  call('list', {});
});
