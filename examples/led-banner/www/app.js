(function () {
  'use strict';

  var DEFAULTS = {
    text: '哥哥好厉害！',
    size: 60,
    speed: 320,
    color: '#ffef5c',
    background: '#08080c',
    glow: true
  };

  var TEXT_COLORS = ['#ffef5c', '#ff4d6d', '#58e6ff', '#7dff6b', '#ffffff', '#ff9f1c'];
  var BG_COLORS = ['#08080c', '#0c1638', '#2a0716', '#04231d', '#160a2e', '#000000'];

  var els = {
    text: document.getElementById('text'),
    size: document.getElementById('size'),
    sizeVal: document.getElementById('sizeVal'),
    speed: document.getElementById('speed'),
    speedVal: document.getElementById('speedVal'),
    textColors: document.getElementById('textColors'),
    bgColors: document.getElementById('bgColors'),
    glow: document.getElementById('glow'),
    preview: document.getElementById('preview'),
    previewTrack: document.getElementById('previewTrack'),
    previewA: document.getElementById('previewA'),
    previewB: document.getElementById('previewB'),
    start: document.getElementById('start'),
    hint: document.getElementById('hint'),
    banner: document.getElementById('banner'),
    track: document.getElementById('track'),
    chunkA: document.getElementById('chunkA'),
    chunkB: document.getElementById('chunkB')
  };

  var settings = copy(DEFAULTS);
  var swatchRows = [];
  var bannerOpen = false;
  var saveTimer = 0;
  var lastTap = 0;

  function copy(source) {
    return JSON.parse(JSON.stringify(source));
  }

  function call(name, args) {
    if (!window.berba || typeof window.berba.call !== 'function') {
      return Promise.reject(new Error('桥接不可用'));
    }
    return window.berba.call(name, args);
  }

  function numberOr(value, fallback) {
    var n = Number(value);
    return isFinite(n) ? n : fallback;
  }

  function clamp(value, min, max, fallback) {
    var n = numberOr(value, fallback);
    return Math.min(max, Math.max(min, Math.round(n)));
  }

  function colorOr(value, fallback) {
    return typeof value === 'string' && /^#[0-9a-fA-F]{6}$/.test(value) ? value.toLowerCase() : fallback;
  }

  function normalize(raw) {
    var src = raw && typeof raw === 'object' ? raw : {};
    var text = typeof src.text === 'string' ? src.text.slice(0, 40) : DEFAULTS.text;
    return {
      text: text,
      size: clamp(src.size, 20, 90, DEFAULTS.size),
      speed: clamp(src.speed, 80, 900, DEFAULTS.speed),
      color: colorOr(src.color, DEFAULTS.color),
      background: colorOr(src.background, DEFAULTS.background),
      glow: src.glow !== false
    };
  }

  function inkFor(hex) {
    var r = parseInt(hex.slice(1, 3), 16);
    var g = parseInt(hex.slice(3, 5), 16);
    var b = parseInt(hex.slice(5, 7), 16);
    return (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255 > 0.55 ? '#16130a' : '#101014';
  }

  function displayText() {
    return settings.text.trim() || '应援文字';
  }

  function setHint(message, isError) {
    els.hint.textContent = message;
    els.hint.classList.toggle('error', !!isError);
  }

  function paintRange(input) {
    var min = Number(input.min);
    var max = Number(input.max);
    var pct = ((Number(input.value) - min) / (max - min)) * 100;
    input.style.setProperty('--fill', pct.toFixed(2) + '%');
  }

  function scheduleSave() {
    clearTimeout(saveTimer);
    saveTimer = setTimeout(function () {
      call('saveSettings', settings).catch(function () {});
    }, 300);
  }

  function buildSwatches(container, colors, key) {
    container.innerHTML = '';
    colors.forEach(function (color) {
      var button = document.createElement('button');
      button.type = 'button';
      button.className = 'swatch';
      button.style.background = color;
      button.dataset.color = color;
      button.setAttribute('aria-label', color);
      button.addEventListener('click', function () {
        settings[key] = color;
        syncSwatches();
        update();
      });
      container.appendChild(button);
    });

    var custom = document.createElement('input');
    custom.type = 'color';
    custom.className = 'swatch-custom';
    custom.value = settings[key];
    custom.setAttribute('aria-label', '自定义颜色');
    custom.addEventListener('input', function () {
      settings[key] = colorOr(custom.value, settings[key]);
      syncSwatches();
      update();
    });
    container.appendChild(custom);

    swatchRows.push({ key: key, container: container, custom: custom });
  }

  function syncSwatches() {
    swatchRows.forEach(function (row) {
      Array.prototype.forEach.call(row.container.children, function (node) {
        if (node.dataset && node.dataset.color) {
          node.classList.toggle('active', node.dataset.color === settings[row.key]);
        }
      });
      row.custom.value = settings[row.key];
    });
  }

  function syncForm() {
    els.text.value = settings.text;
    els.size.value = String(settings.size);
    els.speed.value = String(settings.speed);
    els.sizeVal.textContent = String(settings.size);
    els.speedVal.textContent = String(settings.speed);
    els.glow.checked = settings.glow;
    paintRange(els.size);
    paintRange(els.speed);
    syncSwatches();
  }

  function applyAccent() {
    var root = document.documentElement.style;
    root.setProperty('--accent', settings.color);
    root.setProperty('--on-accent', inkFor(settings.color));
  }

  function setGlow(on) {
    els.preview.classList.toggle('glow', on);
    els.banner.classList.toggle('glow', on);
  }

  function renderPreview() {
    var text = displayText();
    [els.previewA, els.previewB].forEach(function (chunk) {
      chunk.textContent = text;
      chunk.style.color = settings.color;
    });
    els.preview.style.background = settings.background;

    var ph = els.preview.clientHeight || 132;
    var fontSize = Math.max(22, Math.min(ph * 0.75, ph * settings.size / 100));
    els.preview.style.setProperty('--preview-size', fontSize.toFixed(1) + 'px');

    var width = els.previewA.getBoundingClientRect().width || 120;
    var duration = Math.max(3, width / Math.max(40, settings.speed * 0.2));
    els.previewTrack.style.animationDuration = duration.toFixed(2) + 's';

    applyAccent();
    setGlow(settings.glow);
  }

  function applyBannerStyle() {
    var text = displayText();
    els.chunkA.textContent = text;
    els.chunkB.textContent = text;
    els.banner.style.setProperty('--banner-bg', settings.background);
    els.track.style.setProperty('--banner-color', settings.color);
    els.track.style.setProperty('--banner-size', Math.round(window.innerHeight * settings.size / 100) + 'px');
    setGlow(settings.glow);
  }

  function layoutBanner() {
    applyBannerStyle();
    var width = els.chunkA.getBoundingClientRect().width || 200;
    var duration = Math.max(3, width / Math.max(40, settings.speed));
    els.track.style.animationDuration = duration.toFixed(2) + 's';
  }

  function restartTrack() {
    els.track.style.animationName = 'none';
    void els.track.offsetWidth;
    els.track.style.animationName = '';
  }

  function update() {
    renderPreview();
    if (bannerOpen) {
      layoutBanner();
    }
    scheduleSave();
  }

  function openBanner() {
    if (bannerOpen) {
      return;
    }
    settings.text = els.text.value.slice(0, 40);
    bannerOpen = true;
    setHint('进入后双击屏幕或按返回键退出', false);
    applyBannerStyle();
    els.banner.classList.add('active');
    document.body.classList.add('banner-open');
    restartTrack();
    layoutBanner();
    call('enterBanner', {}).catch(function (err) {
      hideBanner();
      setHint('进入全屏失败: ' + err.message, true);
      call('exitBanner', {}).catch(function () {});
    });
  }

  function hideBanner() {
    if (!bannerOpen) {
      return;
    }
    bannerOpen = false;
    els.banner.classList.remove('active');
    document.body.classList.remove('banner-open');
  }

  function leaveBanner() {
    var wasOpen = bannerOpen;
    hideBanner();
    if (wasOpen) {
      call('exitBanner', {}).catch(function () {});
    }
  }

  function bind() {
    els.text.addEventListener('input', function () {
      settings.text = els.text.value.slice(0, 40);
      update();
    });

    els.size.addEventListener('input', function () {
      settings.size = clamp(els.size.value, 20, 90, DEFAULTS.size);
      els.sizeVal.textContent = String(settings.size);
      paintRange(els.size);
      update();
    });

    els.speed.addEventListener('input', function () {
      settings.speed = clamp(els.speed.value, 80, 900, DEFAULTS.speed);
      els.speedVal.textContent = String(settings.speed);
      paintRange(els.speed);
      update();
    });

    els.glow.addEventListener('change', function () {
      settings.glow = els.glow.checked;
      setGlow(settings.glow);
      scheduleSave();
    });

    els.start.addEventListener('click', openBanner);

    els.text.addEventListener('keydown', function (event) {
      if (event.key === 'Enter') {
        event.preventDefault();
        openBanner();
      }
    });

    els.banner.addEventListener('click', function () {
      var now = Date.now();
      if (now - lastTap < 350) {
        lastTap = 0;
        leaveBanner();
      } else {
        lastTap = now;
      }
    });

    els.banner.addEventListener('contextmenu', function (event) {
      event.preventDefault();
    });

    function reflow() {
      renderPreview();
      if (bannerOpen) {
        layoutBanner();
      }
    }

    window.addEventListener('resize', reflow);
    window.addEventListener('orientationchange', function () {
      setTimeout(reflow, 150);
    });

    if (window.berba && typeof window.berba.on === 'function') {
      window.berba.on('bannerExit', hideBanner);
    }
  }

  function init() {
    buildSwatches(els.textColors, TEXT_COLORS, 'color');
    buildSwatches(els.bgColors, BG_COLORS, 'background');
    bind();
    syncForm();
    renderPreview();

    if (!window.berba) {
      return;
    }
    call('getSettings', {}).then(function (saved) {
      settings = normalize(saved);
      syncForm();
      renderPreview();
    }).catch(function (err) {
      setHint('读取设置失败: ' + err.message, true);
    });
  }

  init();
})();
