(function () {
  'use strict';

  var DEFAULTS = {
    text: '演唱会加油！',
    font: 'black',
    size: 60,
    speed: 320,
    color: '#ffef5c',
    background: '#08080c'
  };

  var FONTS = {
    black: { family: '"sans-serif-black", "Noto Sans SC", sans-serif', weight: 900 },
    sans: { family: 'Roboto, "Noto Sans SC", "PingFang SC", "Microsoft YaHei", sans-serif', weight: 700 },
    serif: { family: '"Noto Serif SC", "Songti SC", SimSun, serif', weight: 700 },
    condensed: { family: '"sans-serif-condensed", "Arial Narrow", sans-serif', weight: 700 },
    mono: { family: '"Roboto Mono", "Noto Sans Mono", monospace', weight: 700 },
    casual: { family: 'casual, cursive', weight: 700 }
  };

  var TEXT_COLORS = ['#ffef5c', '#ff4d6d', '#58e6ff', '#7dff6b', '#ffffff', '#ff9f1c'];
  var BG_COLORS = ['#08080c', '#0c1638', '#2a0716', '#04231d', '#160a2e', '#000000'];

  var els = {
    text: document.getElementById('text'),
    font: document.getElementById('font'),
    size: document.getElementById('size'),
    sizeVal: document.getElementById('sizeVal'),
    speed: document.getElementById('speed'),
    speedVal: document.getElementById('speedVal'),
    color: document.getElementById('color'),
    bgColor: document.getElementById('bgColor'),
    textColors: document.getElementById('textColors'),
    bgColors: document.getElementById('bgColors'),
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
  var bannerOpen = false;
  var saveTimer = 0;
  var lastTap = 0;

  function copy(source) {
    return JSON.parse(JSON.stringify(source));
  }

  function call(name, args) {
    if (!window.guiapp || typeof window.guiapp.call !== 'function') {
      return Promise.reject(new Error('桥接不可用'));
    }
    return window.guiapp.call(name, args);
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
      font: FONTS[src.font] ? src.font : DEFAULTS.font,
      size: clamp(src.size, 20, 90, DEFAULTS.size),
      speed: clamp(src.speed, 80, 900, DEFAULTS.speed),
      color: colorOr(src.color, DEFAULTS.color),
      background: colorOr(src.background, DEFAULTS.background)
    };
  }

  function displayText() {
    return settings.text.trim() || '应援文字';
  }

  function fontOf(key) {
    return FONTS[key] || FONTS.sans;
  }

  function setHint(message, isError) {
    els.hint.textContent = message;
    els.hint.classList.toggle('error', !!isError);
  }

  function scheduleSave() {
    clearTimeout(saveTimer);
    saveTimer = setTimeout(function () {
      call('saveSettings', settings).catch(function () {});
    }, 300);
  }

  function buildSwatches(container, colors, key, input) {
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
        input.value = color;
        syncSwatches();
        update();
      });
      container.appendChild(button);
    });
  }

  function syncSwatches() {
    [els.textColors, els.bgColors].forEach(function (container) {
      Array.prototype.forEach.call(container.children, function (button) {
        var key = container === els.textColors ? 'color' : 'background';
        button.classList.toggle('active', button.dataset.color === settings[key]);
      });
    });
  }

  function syncForm() {
    els.text.value = settings.text;
    els.font.value = settings.font;
    els.size.value = String(settings.size);
    els.speed.value = String(settings.speed);
    els.color.value = settings.color;
    els.bgColor.value = settings.background;
    els.sizeVal.textContent = String(settings.size);
    els.speedVal.textContent = String(settings.speed);
    syncSwatches();
  }

  function renderPreview() {
    var font = fontOf(settings.font);
    var text = displayText();
    var sizePx = Math.round(10 + settings.size * 0.5);
    [els.previewA, els.previewB].forEach(function (chunk) {
      chunk.textContent = text;
      chunk.style.fontFamily = font.family;
      chunk.style.fontWeight = String(font.weight);
      chunk.style.fontSize = sizePx + 'px';
      chunk.style.color = settings.color;
    });
    els.preview.style.background = settings.background;
    var width = els.previewA.getBoundingClientRect().width || 120;
    var duration = Math.max(3, width / Math.max(40, settings.speed * 0.2));
    els.previewTrack.style.animationDuration = duration.toFixed(2) + 's';
  }

  function applyBannerStyle() {
    var font = fontOf(settings.font);
    var text = displayText();
    els.chunkA.textContent = text;
    els.chunkB.textContent = text;
    els.banner.style.setProperty('--banner-bg', settings.background);
    els.track.style.setProperty('--banner-color', settings.color);
    els.track.style.setProperty('--banner-size', Math.round(window.innerHeight * settings.size / 100) + 'px');
    els.track.style.fontFamily = font.family;
    els.track.style.fontWeight = String(font.weight);
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

    els.font.addEventListener('change', function () {
      settings.font = FONTS[els.font.value] ? els.font.value : DEFAULTS.font;
      update();
    });

    els.size.addEventListener('input', function () {
      settings.size = clamp(els.size.value, 20, 90, DEFAULTS.size);
      els.sizeVal.textContent = String(settings.size);
      update();
    });

    els.speed.addEventListener('input', function () {
      settings.speed = clamp(els.speed.value, 80, 900, DEFAULTS.speed);
      els.speedVal.textContent = String(settings.speed);
      update();
    });

    els.color.addEventListener('input', function () {
      settings.color = colorOr(els.color.value, DEFAULTS.color);
      syncSwatches();
      update();
    });

    els.bgColor.addEventListener('input', function () {
      settings.background = colorOr(els.bgColor.value, DEFAULTS.background);
      syncSwatches();
      update();
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

    if (window.guiapp && typeof window.guiapp.on === 'function') {
      window.guiapp.on('bannerExit', hideBanner);
    }
  }

  function init() {
    buildSwatches(els.textColors, TEXT_COLORS, 'color', els.color);
    buildSwatches(els.bgColors, BG_COLORS, 'background', els.bgColor);
    bind();
    syncForm();
    renderPreview();

    if (!window.guiapp) {
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
