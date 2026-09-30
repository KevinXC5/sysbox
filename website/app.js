const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

const menuButton = document.querySelector('.menu-toggle');
const nav = document.querySelector('#nav');
function closeMenu() {
  nav.classList.remove('open');
  menuButton.setAttribute('aria-expanded', 'false');
  menuButton.setAttribute('aria-label', '打开导航菜单');
}
menuButton.addEventListener('click', () => {
  const open = nav.classList.toggle('open');
  menuButton.setAttribute('aria-expanded', String(open));
  menuButton.setAttribute('aria-label', open ? '关闭导航菜单' : '打开导航菜单');
});
nav.querySelectorAll('a').forEach(link => link.addEventListener('click', closeMenu));
document.addEventListener('keydown', event => {
  if (event.key === 'Escape' && nav.classList.contains('open')) {
    closeMenu();
    menuButton.focus();
  }
});

// 像素字标：沿用终端里 ANSI Shadow 字体的 SYSBOX 字形，█ 画成像素块，框线字符画成阴影描边。
const glyphs = {
  S: ['███████╗', '██╔════╝', '███████╗', '╚════██║', '███████║', '╚══════╝'],
  Y: ['██╗   ██╗', '╚██╗ ██╔╝', ' ╚████╔╝ ', '  ╚██╔╝  ', '   ██║   ', '   ╚═╝   '],
  B: ['██████╗ ', '██╔══██╗', '██████╔╝', '██╔══██╗', '██████╔╝', '╚═════╝ '],
  O: [' ██████╗ ', '██╔═══██╗', '██║   ██║', '██║   ██║', '╚██████╔╝', ' ╚═════╝ '],
  X: ['██╗  ██╗', '╚██╗██╔╝', ' ╚███╔╝ ', ' ██╔██╗ ', '██╔╝ ██╗', '╚═╝  ╚═╝']
};
const rows = [0, 1, 2, 3, 4, 5].map(r => [...'SYSBOX'].map(letter => glyphs[letter][r]).join(''));
const columns = Math.max(...rows.map(row => [...row].length));
// 终端字符格约为 1:2，双线框字符用两条平行线表示
function edgePath(char, x, y) {
  const cx = x + 0.5, cy = y + 1, d = 0.16;
  const paths = {
    '═': `M${x} ${cy - d}H${x + 1}M${x} ${cy + d}H${x + 1}`,
    '║': `M${cx - d} ${y}V${y + 2}M${cx + d} ${y}V${y + 2}`,
    '╗': `M${x} ${cy - d}H${cx + d}V${y + 2}M${x} ${cy + d}H${cx - d}V${y + 2}`,
    '╔': `M${x + 1} ${cy - d}H${cx - d}V${y + 2}M${x + 1} ${cy + d}H${cx + d}V${y + 2}`,
    '╚': `M${cx - d} ${y}V${cy + d}H${x + 1}M${cx + d} ${y}V${cy - d}H${x + 1}`,
    '╝': `M${cx + d} ${y}V${cy + d}H${x}M${cx - d} ${y}V${cy - d}H${x}`
  };
  return paths[char];
}
document.querySelectorAll('[data-wordmark]').forEach((holder, index) => {
  const id = `wm-fill-${index}`;
  let pixels = '', edges = '';
  rows.forEach((row, r) => {
    [...row].forEach((char, c) => {
      const delay = `style="--d:${c * 22 + r * 30}ms" data-col="${c}"`;
      if (char === '█') pixels += `<rect class="px" ${delay} x="${c + 0.05}" y="${r * 2 + 0.05}" width="0.9" height="1.9"/>`;
      else if (edgePath(char, c, r * 2)) edges += `<path class="edge" ${delay} d="${edgePath(char, c, r * 2)}"/>`;
    });
  });
  holder.innerHTML = `<svg viewBox="-0.2 -0.2 ${columns + 0.4} 12.4" xmlns="http://www.w3.org/2000/svg">
    <defs><linearGradient id="${id}" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="${columns}" y2="12"><stop offset="0" stop-color="#4f7bff"/><stop offset=".55" stop-color="#3b8cff"/><stop offset="1" stop-color="#22d3ee"/></linearGradient></defs>
    <g fill="none" stroke="#3b5bdb" stroke-width="0.09" opacity="0.75">${edges}</g>
    <g fill="url(#${id})">${pixels}</g></svg>`;
  holder.style.setProperty('--wm-fill', `url(#${id})`);
  if (!reducedMotion && !holder.classList.contains('outline')) holder.classList.add('booting');
  lightOnPointer(holder);
});

// 指针附近的像素抬起并提亮
function lightOnPointer(holder) {
  const svg = holder.querySelector('svg');
  const pixels = [...svg.querySelectorAll('.px')];
  let frame;
  holder.addEventListener('pointermove', event => {
    cancelAnimationFrame(frame);
    frame = requestAnimationFrame(() => {
      const box = svg.getBoundingClientRect();
      const col = (event.clientX - box.left) / box.width * (columns + 0.4);
      const row = (event.clientY - box.top) / box.height * 12.4;
      pixels.forEach(pixel => {
        const dx = Number(pixel.getAttribute('x')) - col;
        const dy = (Number(pixel.getAttribute('y')) - row) / 2;
        pixel.classList.toggle('lit', dx * dx + dy * dy < 9);
      });
    });
  });
  holder.addEventListener('pointerleave', () => pixels.forEach(pixel => pixel.classList.remove('lit')));
}

// 页脚字标进入视口时自左向右扫过一次
const footerMark = document.querySelector('.footer-mark .wordmark');
if (footerMark && !reducedMotion && 'IntersectionObserver' in window) {
  const sweep = new IntersectionObserver(entries => {
    if (!entries[0].isIntersecting) return;
    sweep.disconnect();
    footerMark.querySelectorAll('.px').forEach(pixel => {
      const col = Number(pixel.dataset.col);
      setTimeout(() => pixel.classList.add('lit'), col * 35);
      setTimeout(() => pixel.classList.remove('lit'), col * 35 + 500);
    });
  }, { threshold: 0.5 });
  sweep.observe(footerMark);
}

// 首屏截图叠放：前后两张轮换，先错开再交换层级，避免直接跳变
const deck = document.querySelector('[data-deck]');
const themeLabel = deck.querySelector('[data-theme-label]');
let deckBusy = false;
function shuffleDeck() {
  if (deckBusy) return;
  const front = deck.querySelector('.is-front');
  const back = deck.querySelector('.is-back');
  if (reducedMotion) {
    front.classList.replace('is-front', 'is-back');
    back.classList.replace('is-back', 'is-front');
    themeLabel.textContent = back.dataset.theme === 'dark' ? '深色' : '浅色';
    return;
  }
  deckBusy = true;
  deck.classList.add('is-shuffling');
  setTimeout(() => {
    front.classList.replace('is-front', 'is-back');
    back.classList.replace('is-back', 'is-front');
    themeLabel.textContent = back.dataset.theme === 'dark' ? '深色' : '浅色';
    deck.classList.remove('is-shuffling');
  }, 380);
  setTimeout(() => { deckBusy = false; }, 1000);
}
let deckTimer;
function scheduleDeck() {
  clearInterval(deckTimer);
  if (!reducedMotion) deckTimer = setInterval(shuffleDeck, 5200);
}
deck.addEventListener('click', event => {
  if (event.target.closest('.is-back, .stage-theme')) {
    shuffleDeck();
    scheduleDeck();
  }
});
deck.addEventListener('pointerenter', () => clearInterval(deckTimer));
deck.addEventListener('pointerleave', scheduleDeck);
if ('IntersectionObserver' in window) {
  new IntersectionObserver(entries => {
    if (entries[0].isIntersecting) scheduleDeck();
    else clearInterval(deckTimer);
  }).observe(deck);
}

// 界面区：标签选择界面，下方按钮切换深浅主题
const views = {
  cache: { name: '开发缓存', alt: '按类别列出缓存占用与可选条目', caption: '扫描编辑器、包管理器和构建工具的缓存，按类别列出占用空间，勾选后清理。' },
  processes: { name: '进程管理', alt: '按 CPU 排序的进程列表，包含内存、用户与监听端口', caption: '按 CPU、内存或端口排序，搜索进程名、PID、命令与端口，多选后结束进程。' },
  docker: { name: 'Docker 管理', alt: '容器列表与详情并排，显示挂载、网络与端口', caption: '容器列表和详情并排：挂载、网络与端口一目了然，日志、终端、启停都在快捷键上。' }
};
const shot = { view: 'cache', theme: 'dark' };
const previewImage = document.querySelector('#preview-image');
function renderShot() {
  const view = views[shot.view];
  previewImage.src = `assets/${shot.view}-${shot.theme}.png`;
  previewImage.alt = `sysbox ${shot.theme === 'dark' ? '深色' : '浅色'}主题${view.name}界面，${view.alt}`;
  document.querySelector('#preview-caption').textContent = view.caption;
}
function bindToggle(selector, key) {
  const buttons = document.querySelectorAll(`[${selector}]`);
  buttons.forEach(button => button.addEventListener('click', () => {
    shot[key] = button.getAttribute(selector);
    buttons.forEach(item => {
      const active = item === button;
      item.classList.toggle('active', active);
      item.setAttribute('aria-pressed', String(active));
    });
    renderShot();
  }));
}
bindToggle('data-view', 'view');
bindToggle('data-shot-theme', 'theme');
// 空闲时预载其余截图，切换时不闪白
addEventListener('load', () => {
  Object.keys(views).forEach(view => ['dark', 'light'].forEach(theme => { new Image().src = `assets/${view}-${theme}.png`; }));
});
// 截图切换使用短淡入，不阻塞按钮
previewImage.addEventListener('load', () => {
  if (!reducedMotion && previewImage.animate) {
    previewImage.animate([{ opacity: 0.4, transform: 'translateY(6px)' }, { opacity: 1, transform: 'none' }], { duration: 350, easing: 'ease-out' });
  }
});

// 键帽演示：依次按下常用快捷键并显示对应操作，点击键帽也会触发
const keyActions = {
  up: ['↑', '上移选择'], down: ['↓', '下移选择'], enter: ['enter', '打开选中的工具'], esc: ['esc', '返回上一级'],
  tab: ['tab', '切换资源面板'], s: ['s', '扫描可释放的空间'], t: ['t', '切换深浅主题'], q: ['q', '退出 sysbox']
};
const keySequence = ['down', 'down', 'enter', 's', 'tab', 'esc', 't', 'up', 'q'];
function pressKey(key) {
  const cap = document.querySelector(`.keycap[data-key="${key}"]`);
  if (!cap) return;
  cap.classList.add('pressed');
  setTimeout(() => cap.classList.remove('pressed'), 240);
  document.querySelector('#key-name').textContent = keyActions[key][0];
  document.querySelector('#key-action').textContent = keyActions[key][1];
}
document.querySelectorAll('.keycap').forEach(cap => cap.addEventListener('click', () => pressKey(cap.dataset.key)));
if (!reducedMotion && 'IntersectionObserver' in window) {
  let step = 0, timer;
  new IntersectionObserver(entries => {
    clearInterval(timer);
    if (entries[0].isIntersecting) timer = setInterval(() => pressKey(keySequence[step++ % keySequence.length]), 1500);
  }).observe(document.querySelector('.keyboard'));
}

const platforms = {
  macos: { command: 'curl -fsSL https://raw.githubusercontent.com/KevinXC5/sysbox/main/install.sh | bash', shell: 'TERMINAL / BASH', note: '默认安装到 ~/.local/bin/sysbox。' },
  windows: { command: 'irm https://raw.githubusercontent.com/KevinXC5/sysbox/main/install.ps1 | iex', shell: 'WINDOWS / POWERSHELL', note: '默认安装到 %LOCALAPPDATA%\\Programs\\sysbox 并加入用户 PATH。建议使用 Windows Terminal。' }
};
function selectPlatform(platform) {
  const selected = platforms[platform];
  document.querySelector('#install-command').textContent = selected.command;
  document.querySelector('#shell-label').textContent = selected.shell;
  document.querySelector('#install-note').textContent = selected.note;
  document.querySelectorAll('[data-platform]').forEach(button => {
    const active = button.dataset.platform === platform;
    button.classList.toggle('active', active);
    button.setAttribute('aria-pressed', String(active));
  });
}
document.querySelectorAll('[data-platform]').forEach(button => {
  button.addEventListener('click', () => selectPlatform(button.dataset.platform));
});
// 根据访问者的平台展示对应命令，仍可手动切换。
if (/Windows/i.test(navigator.userAgent)) selectPlatform('windows');

let toastTimer;
function showToast(message) {
  const toast = document.querySelector('#toast');
  clearTimeout(toastTimer);
  toast.textContent = message;
  toast.classList.add('visible');
  toastTimer = setTimeout(() => toast.classList.remove('visible'), 3500);
}
document.querySelector('#copy-command').addEventListener('click', async () => {
  const command = document.querySelector('#install-command');
  try {
    await navigator.clipboard.writeText(command.textContent);
    showToast('安装命令已复制');
  } catch {
    // 剪贴板不可用时选中命令，方便用户用系统快捷键复制。
    const range = document.createRange();
    range.selectNodeContents(command);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    showToast('自动复制不可用，已选中命令，请手动复制');
  }
});

// 顶栏底部的阅读进度
const header = document.querySelector('.site-header');
function updateProgress() {
  const max = document.documentElement.scrollHeight - innerHeight;
  header.style.setProperty('--progress', max > 0 ? (scrollY / max).toFixed(4) : 0);
}
addEventListener('scroll', updateProgress, { passive: true });
updateProgress();

// 进入视口时分批显现，减少动态效果设置下直接展示全部内容。
if (!reducedMotion && 'IntersectionObserver' in window) {
  const observer = new IntersectionObserver(entries => {
    entries.forEach(entry => {
      if (entry.isIntersecting) {
        entry.target.classList.add('is-visible');
        observer.unobserve(entry.target);
      }
    });
  }, { threshold: 0.12 });
  document.querySelectorAll('.section-heading, .tool-card, .tool-index, .safety-row, .preview-copy, .preview-workspace, .install-copy, .install-panel, .faq-head, .faq-list').forEach(element => {
    element.classList.add('reveal');
    const siblings = [...element.parentElement.children].filter(item => item.matches('.tool-card, .safety-row'));
    if (siblings.length) element.style.setProperty('--reveal-delay', `${siblings.indexOf(element) * 90}ms`);
    observer.observe(element);
  });
}
