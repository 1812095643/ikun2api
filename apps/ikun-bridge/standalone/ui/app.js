import { invoke } from '@tauri-apps/api/core';
import '../../src/style.css';
import './app.css';

const $ = id => document.getElementById(id);
const connectionDialog = $('connection-dialog');
const cliDialog = $('cli-dialog');
const desktopDialog = $('desktop-dialog');
let busy = '';
let ended = false;
let status = { running: false };
let clients = {};
let statusGeneration = 0;
let afterConnect;
let desktopUndo = false;
let profiles = { selected: null, entries: [] };
let editingId = null;
const operationErrors = [];

function operationStatus() {
  const lines = (status.actions || []).map(item => `${item.state === 'done' ? '✓' : item.state === 'error' ? '!' : '·'} ${item.label}：${item.detail}`);
  for (const [client, installed] of Object.entries(clients)) lines.push(`${installed ? '✓' : '·'} ${client === 'cli' ? 'Claude Code' : 'Claude Desktop'}：${installed ? '已找到客户端' : '未找到客户端'}`);
  $('operations').value = [...lines, ...operationErrors.slice(-4)].join('\n');
}

function preference(name, value) {
  try {
    if (value === undefined) return localStorage.getItem(name) || '';
    localStorage.setItem(name, value);
  } catch { /* Preferences are optional. */ }
  return '';
}

let cliPath = preference('bridge.cliPath');
let gatewayUrl = preference('bridge.gatewayUrl');

function feedback(message = '', success = false, dialog = document.querySelector('dialog[open]')) {
  const target = dialog?.querySelector('.dialog-feedback') ?? $('notice');
  if (dialog) target.hidden = !message;
  target.textContent = message;
  target.dataset.kind = success ? 'success' : 'error';
}

function row(id, label, tone) {
  const cell = $(id);
  cell.dataset.tone = tone;
  cell.querySelector('span').textContent = label;
}

function controls() {
  const running = status.running;
  $('connect').hidden = false;
  $('connect').lastChild.textContent = running ? '切换连接' : '连接设置';
  $('stop').hidden = !running;
  $('stop').disabled = Boolean(busy) || ended || !running;
  for (const id of ['connect', 'check', 'launch-cli', 'launch-desktop', 'cli-settings', 'desktop-settings', 'quit', 'desktop-apply', 'desktop-restore', 'cli-restore', 'save-cli', 'profile-new', 'profile-save', 'profile']) {
    $(id).disabled = Boolean(busy) || ended;
  }
  for (const input of ['gateway', 'key', 'cli-path', 'profile-name']) $(input).readOnly = Boolean(busy) || ended;
  $('profile-delete').disabled = Boolean(busy) || ended || editingId === status.profile_id;
  document.querySelectorAll('[data-close]').forEach(button => { button.disabled = Boolean(busy); });
  $('desktop-restore').hidden = !desktopUndo;
  $('desktop-backup').textContent = desktopUndo ? '可恢复' : '无';
  $('start').disabled = Boolean(busy) || ended;
  $('start').textContent = busy === 'start' ? '连接中…' : afterConnect ? '保存并启动' : status.running ? '保存并切换' : '保存并连接';
  $('check').setAttribute('aria-busy', busy === 'check');
}

async function refresh() {
  const generation = ++statusGeneration;
  const next = await invoke('bridge_status');
  if (generation !== statusGeneration || ended) return;
  status = next;
  $('version').textContent = status.version;
  $('endpoint').textContent = status.endpoint || '';
  $('upstream').textContent = status.gateway || gatewayUrl || '未配置';
  row('gateway-status', status.gateway ? '已连接' : gatewayUrl ? '已填写' : '未配置', status.gateway ? 'ok' : 'idle');
  $('ticket-detail').textContent = status.reason || '';
  $('ticket-detail').title = status.reason || '';
  $('desktop-endpoint').textContent = status.endpoint || '未启动';
  $('requests').hidden = !status.running;
  $('requests').textContent = `${status.requests || 0} 次请求`;
  row('running', status.running ? '运行中' : '未启动', status.running ? 'ok' : 'idle');
  row('ticket', status.ticket || '未领取', status.ticket === '已领取' ? 'ok' : status.reason ? 'warning' : 'idle');
  operationStatus();
  controls();
}

async function inspectClients(path = cliPath, validate = false) {
  const next = await invoke('bridge_clients', { path });
  if (validate && path && !next.cli) throw new Error('找不到此路径下的 Claude Code 程序');
  clients = next;
  for (const [id, installed] of Object.entries(clients)) {
    row(`${id}-status`, installed ? '已安装' : id === 'cli' ? '未找到' : '未安装', installed ? 'ok' : 'idle');
  }
  operationStatus();
}

async function run(action, fn) {
  if (busy || ended) return;
  const dialog = document.querySelector('dialog[open]');
  busy = action;
  ++statusGeneration;
  controls();
  feedback();
  try { await fn(); }
  catch (error) { const message = error instanceof Error ? error.message : String(error); feedback(message, false, dialog?.open ? dialog : null); operationErrors.push(`! ${message}`); operationStatus(); }
  finally { busy = ''; controls(); if (!ended) refresh().catch(() => {}); }
}

function open(dialog) {
  feedback('', false, dialog);
  dialog.showModal();
}

function applyProfiles(next) {
  profiles = next;
  const selected = next.entries.find(entry => entry.id === next.selected);
  if (selected) gatewayUrl = selected.gateway;
}

function editProfile(id) {
  const entry = profiles.entries.find(entry => entry.id === id);
  editingId = entry?.id || null;
  $('profile').replaceChildren(...profiles.entries.map(entry => new Option(`${entry.name} · ${entry.gateway.replace(/^https:\/\//, '')}`, entry.id)), new Option('新连接', ''));
  $('profile').value = editingId || '';
  $('profile-name').value = entry?.name || '';
  $('gateway').value = entry?.gateway || (profiles.entries.length ? '' : gatewayUrl);
  $('key').value = '';
  $('key').required = !entry;
  $('key').placeholder = entry ? `${entry.key_hint} · 留空保留` : '填写 API Key';
  $('profile-delete').hidden = !entry;
  $('profile-delete').textContent = '删除';
  delete $('profile-delete').dataset.confirm;
  controls();
}

async function openConnection(after) {
  await profilesReady;
  afterConnect = after;
  editProfile(status.profile_id || profiles.selected);
  open(connectionDialog);
}

async function saveProfile() {
  if (!$('gateway-form').reportValidity()) return null;
  const next = await invoke('bridge_profile_save', { input: { id: editingId, name: $('profile-name').value.trim(), gateway: $('gateway').value.trim(), key: $('key').value.trim() || null } });
  applyProfiles(next);
  editProfile(next.selected);
  return next.selected;
}

async function connectProfile(id) {
  await invoke('bridge_start', { profileId: id });
  applyProfiles(await invoke('bridge_profiles'));
  await refresh();
}

async function connectAndLaunch(client) {
  await profilesReady;
  if (!profiles.selected) return openConnection(client);
  run('start', async () => { await connectProfile(profiles.selected); await launchClient(client); });
}

function openCli() {
  $('cli-path').value = cliPath;
  open(cliDialog);
}

async function launchClient(client) {
  if (client === 'desktop') {
    feedback('正在启动 Claude Desktop…', true);
    try {
      await invoke('bridge_launch_desktop');
      feedback('已配置并打开 Claude Desktop', true);
    } finally { await checkDesktopBackup(); }
  } else {
    await invoke('bridge_launch', { path: cliPath });
    feedback('已启动 Claude Code', true);
  }
}

async function checkDesktopBackup() {
  desktopUndo = await invoke('bridge_desktop_configured');
  controls();
}

for (const dialog of document.querySelectorAll('.settings-dialog')) {
  dialog.querySelectorAll('[data-close]').forEach(button => {
    button.onclick = () => dialog.close();
  });
  dialog.addEventListener('cancel', event => { if (busy) event.preventDefault(); });
}
connectionDialog.addEventListener('close', () => { $('key').value = ''; });

$('connect').onclick = () => openConnection(undefined).catch(error => feedback(String(error), false, null));
$('profile').onchange = () => { editProfile($('profile').value); feedback(); };
$('profile-new').onclick = () => { editProfile(null); $('profile-name').focus(); feedback(); };
$('profile-save').onclick = () => run('save-profile', async () => { if (await saveProfile()) feedback('连接已保存', true, connectionDialog); });
$('profile-delete').onclick = () => {
  if ($('profile-delete').dataset.confirm !== editingId) {
    $('profile-delete').dataset.confirm = editingId;
    $('profile-delete').textContent = '确认删除';
    return;
  }
  run('delete-profile', async () => {
    applyProfiles(await invoke('bridge_profile_delete', { id: editingId }));
    editProfile(profiles.selected);
    feedback('连接已删除', true, connectionDialog);
  });
};
$('cli-settings').onclick = openCli;
$('desktop-settings').onclick = () => {
  open(desktopDialog);
  checkDesktopBackup().catch(error => feedback(String(error), false, desktopDialog));
};

$('gateway-form').addEventListener('submit', event => {
  event.preventDefault();
  run('start', async () => {
    const id = await saveProfile();
    if (!id) return;
    await connectProfile(id);
    preference('bridge.gatewayUrl', gatewayUrl);
    connectionDialog.close();
    if (afterConnect) await launchClient(afterConnect);
  });
});
$('cli-form').addEventListener('submit', event => {
  event.preventDefault();
  const path = $('cli-path').value.trim();
  run('save-cli', async () => {
    await inspectClients(path, true);
    cliPath = path;
    preference('bridge.cliPath', cliPath);
    cliDialog.close();
  });
});
$('check').onclick = () => run('check', async () => {
  await inspectClients();
  await checkDesktopBackup();
  try { if (status.running) await invoke('bridge_check'); }
  finally { await refresh(); }
  feedback(status.running ? '连接检查完成' : '客户端检查完成', true);
});
$('stop').onclick = () => run('stop', async () => { await invoke('bridge_stop'); await refresh(); });
$('launch-cli').onclick = () => {
  if (clients.cli === false) return openCli();
  if (!status.running) return connectAndLaunch('cli').catch(error => feedback(String(error), false, null));
  run('cli', () => launchClient('cli'));
};
function launchDesktop() {
  if (clients.desktop === false) return feedback('未找到 Claude Desktop');
  if (!status.running) {
    if (desktopDialog.open) desktopDialog.close();
    return connectAndLaunch('desktop').catch(error => feedback(String(error), false, null));
  }
  run('desktop', () => launchClient('desktop'));
}
$('launch-desktop').onclick = launchDesktop;
$('desktop-apply').onclick = launchDesktop;
$('desktop-restore').onclick = () => run('restore', async () => {
  try {
    const keptEdits = await invoke('bridge_restore_desktop');
    feedback(keptEdits ? '已恢复，保留手动修改' : '已恢复原配置', true);
  } finally { await checkDesktopBackup(); }
});
$('cli-restore').onclick = () => run('restore-cli', async () => {
  const kept = await invoke('bridge_restore_cli');
  feedback(kept ? '已恢复可撤销项，保留手动修改；已运行的 CLI 需重启' : '原配置已恢复，已运行的 CLI 需重启', true);
});
$('quit').onclick = () => run('quit', async () => {
  ended = true;
  try { await invoke('bridge_quit'); }
  catch (error) { ended = false; throw error; }
});

const profilesReady = invoke('bridge_profiles').then(applyProfiles).catch(error => { feedback(String(error), false, null); });
controls();
refresh().catch(error => feedback(String(error), false, null));
inspectClients().catch(error => feedback(String(error), false, null));
setInterval(() => {
  if (!busy && !ended && !document.hidden) refresh().catch(error => feedback(String(error), false, null));
}, 3000);
