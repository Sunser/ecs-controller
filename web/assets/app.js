const passwordInput = document.getElementById('passwordInput');
const savedPassword = localStorage.getItem('loginPassword') || '';
passwordInput.value = savedPassword;

let currentSettings = null;
let autoRefreshEnabled = localStorage.getItem('autoRefreshEnabled') !== 'false';
let autoRefreshIntervalMs = Number(localStorage.getItem('autoRefreshIntervalMs') || 30000);
let autoRefreshTimer = null;
let autoRefreshBusy = false;
const NOTIFY_EVENTS = [
  {value: 'auto_start', label: '后台自动启动'},
  {value: 'manual_start', label: '手工启动'},
  {value: 'manual_stop', label: '手工关机'},
  {value: 'manual_required', label: '等待人工决策'},
  {value: 'traffic_exceeded', label: '流量超阈值'},
  {value: 'error', label: '错误告警'}
];

document.getElementById('unlockBtn').onclick = unlock;
document.getElementById('logoutBtn').onclick = function() {
  localStorage.removeItem('loginPassword');
  location.reload();
};
document.getElementById('refreshLogsBtn').onclick = loadLogs;
document.getElementById('saveSettingsBtn').onclick = saveSettings;
document.getElementById('autoRefreshToggle').onclick = toggleAutoRefresh;
document.getElementById('autoRefreshInterval').onchange = changeAutoRefreshInterval;
document.getElementById('keepAliveTarget').onchange = renderOptionHelp;
document.getElementById('trafficPolicy').onchange = renderOptionHelp;
document.getElementById('stopMode').onchange = renderOptionHelp;
document.getElementById('notificationEnabled').onchange = renderOptionHelp;
document.querySelectorAll('.tab').forEach(function(button) {
  button.onclick = function() { switchTab(button.dataset.tab); };
});
initAutoRefreshControls();
if (savedPassword) unlock();

function authHeaders() {
  return {'X-Login-Password': localStorage.getItem('loginPassword') || passwordInput.value};
}

async function unlock() {
  localStorage.setItem('loginPassword', passwordInput.value);
  const ok = await refreshPageData();
  if (ok) {
    document.getElementById('loginView').classList.add('hidden');
    document.getElementById('appView').classList.remove('hidden');
    document.getElementById('autoRefreshControl').classList.remove('hidden');
    document.getElementById('logoutBtn').classList.remove('hidden');
    document.getElementById('autoRefreshToggle').classList.remove('hidden');
    scheduleAutoRefresh();
  }
}

async function refreshPageData() {
  const ok = await loadAll();
  if (ok) await loadLogs();
  return ok;
}

async function loadAll() {
  const status = await api('/api/status');
  if (!status) return false;
  const settings = await api('/api/settings');
  if (!settings) return false;
  currentSettings = settings;
  renderStatus(status.snapshot || {});
  renderSettings(settings);
  updatePageRefreshTime();
  document.getElementById('loginError').textContent = '';
  return true;
}

async function loadLogs() {
  const body = await api('/api/logs?limit=160');
  if (!body) return false;
  renderLogs(body.logs || []);
  updatePageRefreshTime();
  return true;
}

async function api(path, options) {
  const init = options || {};
  init.headers = Object.assign({}, init.headers || {}, authHeaders());
  const response = await fetch(path, init);
  const body = await response.json().catch(function() { return {}; });
  if (!response.ok) {
    const message = body.error || 'request failed';
    document.getElementById('loginError').textContent = message;
    document.getElementById('settingsError').textContent = message;
    return null;
  }
  return body;
}

function switchTab(name) {
  document.querySelectorAll('.tab').forEach(function(button) {
    button.classList.toggle('active', button.dataset.tab === name);
  });
  ['overview', 'instances', 'settings', 'logs'].forEach(function(tab) {
    document.getElementById(tab + 'Tab').classList.toggle('hidden', tab !== name);
  });
  if (name === 'logs') loadLogs();
}

function renderStatus(snapshot) {
  const instances = snapshot.instances || [];
  const accounts = snapshot.accounts || [];
  const running = instances.filter(function(row) { return row.status === 'Running'; }).length;
  const spot = instances.filter(function(row) { return row.spot; }).length;
  const exceeded = accounts.filter(function(row) {
    return Number(row.usage_percent || 0) >= Number((currentSettings && currentSettings.traffic.warning_percent) || 95);
  }).length;
  text('metricInstances', instances.length);
  text('metricRunning', running);
  text('metricSpot', spot);
  text('metricExceeded', exceeded);
  updateDataRefreshTime(snapshot.generated_at);
  if (currentSettings) {
    text('policySummary', '流量策略 ' + policyLabel(currentSettings.keep_alive.traffic_policy));
    text('targetSummary', '保活目标 ' + targetLabel(currentSettings.keep_alive.target));
    text('warningSummary', '流量阈值 ' + currentSettings.traffic.warning_percent + '%');
    text('refreshSummary', '后台检查 ' + currentSettings.server.refresh_interval);
  }
  renderAccountRows(accounts);
  renderOverviewRows(instances);
  renderInstanceRows(instances);
}

function renderAccountRows(rows) {
  const body = document.getElementById('accountRows');
  clear(body);
  rows.forEach(function(row) {
    const scopes = normalizedTrafficScopes(row);
    const tr = document.createElement('tr');
    tr.appendChild(cell(accountText(row)));
    tr.appendChild(cell(scopeTrafficStack(scopes, row.traffic_regions || [])));
    tr.appendChild(cell(scopeLimitStack(scopes)));
    tr.appendChild(cell(scopeUsageStack(scopes)));
    body.appendChild(tr);
  });
}

function renderOverviewRows(rows) {
  const body = document.getElementById('overviewRows');
  clear(body);
  rows.forEach(function(row) {
    const tr = document.createElement('tr');
    tr.appendChild(cell(accountText(row)));
    tr.appendChild(cell(instanceText(row)));
    tr.appendChild(cell(statusBadge(row.status)));
    tr.appendChild(cell(ipText(row)));
    tr.appendChild(cell(instanceTrafficText(row)));
    tr.appendChild(cell(decisionText(row.keep_alive_decision, row.manual_paused)));
    body.appendChild(tr);
  });
}

function renderInstanceRows(rows) {
  const body = document.getElementById('instanceRows');
  clear(body);
  rows.forEach(function(row) {
    const tr = document.createElement('tr');
    tr.appendChild(cell(accountText(row)));
    tr.appendChild(cell(instanceText(row)));
    tr.appendChild(cell(esc(row.region_id || '-')));
    tr.appendChild(cell(statusBadge(row.status)));
    tr.appendChild(cell(ipText(row)));
    tr.appendChild(cell(instanceTrafficText(row)));
    tr.appendChild(cell(instanceShapeText(row)));
    tr.appendChild(cell('<span class="badge">' + (row.spot ? '是' : '否') + '</span>'));
    tr.appendChild(cell(operationText(row.last_operation || {})));
    tr.appendChild(actionCell(row));
    body.appendChild(tr);
  });
}

function renderSettings(settings) {
  document.getElementById('refreshInterval').value = settings.server.refresh_interval;
  document.getElementById('regionRefreshInterval').value = (settings.discovery || {}).region_refresh_interval || '24h';
  document.getElementById('requestTimeout').value = settings.server.request_timeout;
  document.getElementById('warningPercent').value = settings.traffic.warning_percent;
  document.getElementById('keepAliveEnabled').value = String(settings.keep_alive.enabled);
  document.getElementById('keepAliveTarget').value = settings.keep_alive.target;
  document.getElementById('trafficPolicy').value = settings.keep_alive.traffic_policy;
  document.getElementById('startCooldown').value = settings.keep_alive.start_cooldown;
  document.getElementById('stopMode').value = settings.keep_alive.stop_mode || 'StopCharging';
  document.getElementById('includeIds').value = (settings.keep_alive.include_instance_ids || []).join('\n');
  document.getElementById('logLevel').value = settings.logging.level;
  document.getElementById('notificationEnabled').value = String(settings.notification.enabled);
  renderNotifyEventPicker(settings.notification.notify_events || []);
  renderOptionHelp();
}

async function saveSettings() {
  if (!currentSettings) return;
  const update = JSON.parse(JSON.stringify(currentSettings));
  update.server.refresh_interval = document.getElementById('refreshInterval').value;
  update.discovery = update.discovery || {};
  update.discovery.region_refresh_interval = document.getElementById('regionRefreshInterval').value;
  update.server.request_timeout = document.getElementById('requestTimeout').value;
  update.traffic.warning_percent = Number(document.getElementById('warningPercent').value);
  update.keep_alive.enabled = document.getElementById('keepAliveEnabled').value === 'true';
  update.keep_alive.target = document.getElementById('keepAliveTarget').value;
  update.keep_alive.traffic_policy = document.getElementById('trafficPolicy').value;
  update.keep_alive.start_cooldown = document.getElementById('startCooldown').value;
  update.keep_alive.stop_mode = document.getElementById('stopMode').value;
  update.keep_alive.include_instance_ids = splitLines(document.getElementById('includeIds').value);
  update.logging.level = document.getElementById('logLevel').value;
  update.notification.enabled = document.getElementById('notificationEnabled').value === 'true';
  update.notification.notify_events = selectedNotifyEvents();
  const saved = await api('/api/settings', {
    method: 'PUT',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify(update)
  });
  if (saved) {
    currentSettings = saved;
    renderSettings(saved);
    document.getElementById('settingsError').textContent = '已保存';
    await loadAll();
  }
}

async function action(row, op) {
  const instanceId = row.instance_id || '';
  const options = {method: 'POST'};
  if (op === 'stop') {
    if (!confirm('确认关机 ' + instanceId + '？模式：' + effectiveStopModeLabel(row, defaultStopMode()) + '。关机后该实例会暂停自动保活。')) return;
  }
  const result = await api('/api/instances/' + encodeURIComponent(instanceId) + '/' + op, options);
  if (result) {
    await loadAll();
    await loadLogs();
  }
}

function actionCell(row) {
  const td = document.createElement('td');
  const box = document.createElement('div');
  box.className = 'actions';
  const start = document.createElement('button');
  start.textContent = '启动';
  start.onclick = function() { action(row, 'start'); };
  const stop = document.createElement('button');
  stop.className = 'danger';
  stop.textContent = '关机';
  stop.onclick = function() { action(row, 'stop'); };
  box.appendChild(start);
  box.appendChild(stop);
  td.appendChild(box);
  return td;
}

function renderLogs(logs) {
  const box = document.getElementById('logRows');
  clear(box);
  logs.slice().reverse().filter(function(row) {
    return row.message !== 'cdt traffic loaded';
  }).forEach(function(row) {
    const item = document.createElement('div');
    item.className = 'log-row';
    const fields = formatFields(row.fields || {});
    const fieldsHTML = fields ? '<span class="log-fields">' + esc(fields) + '</span>' : '';
    item.appendChild(logText(formatTime(row.time)));
    item.appendChild(logCell('<span class="badge ' + levelClass(row.level) + '">' + esc(row.level || '-') + '</span>'));
    item.appendChild(logText(logModuleLabel(row.module)));
    item.appendChild(logCell('<div class="log-message"><strong>' + esc(logTaskLabel(row.message)) + '</strong>' + fieldsHTML + '</div>'));
    box.appendChild(item);
  });
}

function renderNotifyEventPicker(selected) {
  const box = document.getElementById('notifyEvents');
  clear(box);
  const selectedSet = new Set(selected || []);
  const allSelected = selectedSet.has('all');
  NOTIFY_EVENTS.forEach(function(event) {
    const label = document.createElement('label');
    label.className = 'event-chip';
    const input = document.createElement('input');
    input.type = 'checkbox';
    input.value = event.value;
    input.checked = allSelected || selectedSet.has(event.value);
    input.onchange = renderOptionHelp;
    const span = document.createElement('span');
    span.textContent = event.label;
    label.appendChild(input);
    label.appendChild(span);
    box.appendChild(label);
  });
}

function selectedNotifyEvents() {
  return Array.from(document.querySelectorAll('#notifyEvents input:checked')).map(function(input) {
    return input.value;
  });
}

function accountText(row) {
  return '<div class="row-title"><strong>' + esc(row.account_name || '-') + '</strong><span class="muted">' + siteLabel(row.account_site) + '</span></div>';
}

function instanceText(row) {
  return '<div class="row-title"><strong>' + esc(row.instance_name || row.instance_id || '-') + '</strong><span class="muted mono">' + esc(row.instance_id || '-') + '</span></div>';
}

function ipText(row) {
  const parts = [];
  if (row.public_ip) parts.push({label: 'IPv4', value: row.public_ip});
  (row.ipv6_addresses || []).forEach(function(ip) { parts.push({label: 'IPv6', value: ip}); });
  if (!parts.length) return '<span class="muted">-</span>';
  return '<div class="ip-stack">' + parts.map(function(item) {
    return '<div class="ip-line"><span class="ip-label">' + item.label + '</span><span class="mono">' + esc(item.value) + '</span></div>';
  }).join('') + '</div>';
}

function instanceTrafficText(row) {
  const used = Number(row.instance_traffic_gb || 0).toFixed(2);
  const err = row.instance_traffic_error ? '<br><span class="error">' + esc(row.instance_traffic_error) + '</span>' : '';
  const note = row.instance_traffic_source === 'unknown' && !row.instance_traffic_error ? '<span class="muted">暂无数据</span>' : '';
  return '<div class="traffic-main"><strong>' + used + '</strong><span class="muted">GB</span></div>' + note + err;
}

function instanceShapeText(row) {
  const cpu = Number(row.cpu || 0);
  const memory = Number(row.memory_mb || 0);
  const bandwidthIn = Number(row.internet_bandwidth_in);
  const bandwidthOut = Number(row.internet_bandwidth_out || 0);
  if (cpu <= 0 || memory <= 0) return '<span class="muted">-</span>';
  const bandwidthText = '<span class="muted">' + bandwidthLabel(bandwidthIn, bandwidthOut) + '</span>';
  return '<div class="row-title"><strong>' + cpu + ' 核 ' + memoryLabel(memory) + '</strong>' + bandwidthText + '</div>';
}

function memoryLabel(memoryMB) {
  if (memoryMB < 1024) return memoryMB + 'M';
  const memoryGB = memoryMB / 1024;
  if (Number.isInteger(memoryGB)) return memoryGB + 'G';
  return memoryGB.toFixed(1).replace(/\.0$/, '') + 'G';
}

function bandwidthLabel(inBandwidth, outBandwidth) {
  if (!Number.isFinite(outBandwidth) || outBandwidth <= 0) return '-';
  if (Number.isFinite(inBandwidth) && inBandwidth >= 0) {
    return outBandwidth + '/' + inBandwidth + 'Mbps';
  }
  return outBandwidth + 'Mbps';
}

function trafficAmount(value) {
  return '<div class="traffic-main"><strong>' + Number(value || 0).toFixed(2) + '</strong><span class="muted">GB</span></div>';
}

function normalizedTrafficScopes(row) {
  if (Array.isArray(row.traffic_scopes) && row.traffic_scopes.length) {
    return row.traffic_scopes;
  }
  return [{
    key: 'total',
    name: '账号合计',
    traffic_gb: row.monthly_traffic_gb,
    limit_gb: row.monthly_limit_gb,
    usage_percent: row.usage_percent
  }];
}

function scopeTrafficStack(scopes, regions) {
  return '<div class="scope-stack">' + scopes.map(function(scope) {
    const tooltip = scopeTrafficTooltip(scope, regions);
    const title = tooltip ? ' title="' + attr(tooltip) + '"' : '';
    const cls = tooltip ? 'scope-line has-detail' : 'scope-line';
    const cue = tooltip ? '<i class="detail-cue">明细</i>' : '';
    return '<div class="' + cls + '"' + title + '><span><span class="scope-name">' + esc(scope.name || scopeLabel(scope.key)) + '</span>' + cue + '</span>' + trafficAmount(scope.traffic_gb) + '</div>';
  }).join('') + '</div>';
}

function scopeTrafficTooltip(scope, regions) {
  const key = scope.key || '';
  const matched = (regions || []).filter(function(region) {
    return region.scope === key;
  });
  if (!matched.length) return '';
  return matched.map(function(region) {
    return (region.name || region.region_id || '-') + ' ' + Number(region.traffic_gb || 0).toFixed(2) + 'GB';
  }).join('\n');
}

function regionTrafficStack(row) {
  const regions = row.traffic_regions || [];
  if (!regions.length) {
    if (row.traffic_error) return '<span class="error">' + esc(row.traffic_error) + '</span>';
    return '<span class="muted">-</span>';
  }
  return '<div class="region-stack region-inline-stack">' + regions.map(function(region) {
    return '<div class="region-line"><span>' + esc(region.name || region.region_id || '-') + '</span><strong>' + Number(region.traffic_gb || 0).toFixed(2) + '</strong><em>GB</em></div>';
  }).join('') + '</div>';
}

function scopeLimitStack(scopes) {
  return '<div class="scope-stack">' + scopes.map(function(scope) {
    return '<div class="scope-line"><span>' + esc(scope.name || scopeLabel(scope.key)) + '</span><strong>' + Number(scope.limit_gb || 0).toFixed(0) + '</strong><em>GB</em></div>';
  }).join('') + '</div>';
}

function scopeUsageStack(scopes) {
  return '<div class="scope-stack">' + scopes.map(function(scope) {
    return '<div class="scope-meter-line"><span>' + esc(scope.name || scopeLabel(scope.key)) + '</span>' + trafficMeter(scope.usage_percent) + '</div>';
  }).join('') + '</div>';
}

function trafficMeter(value) {
  const pctNumber = Number(value || 0);
  const pct = pctNumber.toFixed(1);
  const warning = Number((currentSettings && currentSettings.traffic.warning_percent) || 95);
  const width = Math.max(0, Math.min(100, pctNumber));
  const tone = pctNumber >= warning ? 'danger' : pctNumber >= warning * 0.8 ? 'warn' : '';
  return '<div class="traffic-meter"><span class="' + tone + '" style="width:' + width.toFixed(1) + '%"></span></div><span class="muted">' + pct + '%</span>';
}

function formatTime(value) {
  if (!value) return '-';
  const normalized = String(value).replace(/\.(\d{3})\d+(Z|[+-]\d\d:\d\d)$/, '.$1$2');
  const date = new Date(normalized);
  if (Number.isNaN(date.getTime())) return value;
  const parts = new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false
  }).formatToParts(date).reduce(function(acc, item) {
    acc[item.type] = item.value;
    return acc;
  }, {});
  return parts.year + '-' + parts.month + '-' + parts.day + ' ' + parts.hour + ':' + parts.minute + ':' + parts.second;
}

function updatePageRefreshTime() {
  text('pageUpdated', '页面刷新 ' + formatTime(new Date()));
}

function updateDataRefreshTime(value) {
  text('dataUpdated', value ? '数据更新 ' + formatTime(value) : '数据更新 -');
}

function initAutoRefreshControls() {
  const select = document.getElementById('autoRefreshInterval');
  const values = Array.from(select.options).map(function(option) { return Number(option.value); });
  if (!values.includes(autoRefreshIntervalMs)) {
    autoRefreshIntervalMs = 30000;
  }
  select.value = String(autoRefreshIntervalMs);
  updateAutoRefreshToggle();
}

function changeAutoRefreshInterval() {
  autoRefreshIntervalMs = Number(document.getElementById('autoRefreshInterval').value || 30000);
  localStorage.setItem('autoRefreshIntervalMs', String(autoRefreshIntervalMs));
  scheduleAutoRefresh();
}

function toggleAutoRefresh() {
  autoRefreshEnabled = !autoRefreshEnabled;
  localStorage.setItem('autoRefreshEnabled', String(autoRefreshEnabled));
  updateAutoRefreshToggle();
  scheduleAutoRefresh();
}

function updateAutoRefreshToggle() {
  document.getElementById('autoRefreshToggle').textContent = autoRefreshEnabled ? '暂停自动刷新' : '启用自动刷新';
}

function scheduleAutoRefresh() {
  clearTimeout(autoRefreshTimer);
  updateAutoRefreshToggle();
  if (!autoRefreshEnabled || document.getElementById('appView').classList.contains('hidden')) return;
  autoRefreshTimer = setTimeout(async function() {
    if (!autoRefreshBusy) {
      autoRefreshBusy = true;
      try {
        await refreshPageData();
      } finally {
        autoRefreshBusy = false;
      }
    }
    scheduleAutoRefresh();
  }, autoRefreshIntervalMs);
}

function renderOptionHelp() {
  const target = document.getElementById('keepAliveTarget').value;
  const policy = document.getElementById('trafficPolicy').value;
  const stopMode = document.getElementById('stopMode').value;
  const notificationEnabled = document.getElementById('notificationEnabled').value === 'true';
  const targetText = {
    disabled: '关闭后台自动保活，仅保留查看和手工操作。',
    all: '所有发现到的实例都参与保活，请确认不会误启非抢占式机器。',
    spot_only: '只对抢占式实例自动保活，推荐默认使用。',
    include_list: '只保活“指定保活实例 ID”中列出的实例。',
  }[target] || '-';
  const policyText = {
    ignore_limit: '忽略流量限制，后台继续自动保活。',
    pause_when_exceeded: '实例所属流量额度池超过阈值后后台暂停自动保活，适合严格控制流量。',
    manual_only_when_exceeded: '实例所属流量额度池未超阈值时自动保活；超过阈值后暂停后台自动保活，但页面仍允许手工启动。'
  }[policy] || '-';
  const stopModeText = {
    StopCharging: '节省停机。非包年包月实例可使用 StopCharging；包年包月实例会自动降级为普通停机，避免传入不适用的停机模式。',
    KeepCharging: '普通停机。关机后实例保持原计费模式，适合不希望触发节省停机规则的实例。'
  }[stopMode] || '-';
  document.getElementById('optionHelp').innerHTML =
    '<div><span class="badge blue">' + esc(targetLabel(target)) + '</span> ' + targetText + '</div>' +
    '<div><span class="badge blue">' + esc(policyLabel(policy)) + '</span> ' + policyText + '</div>' +
    '<div><span class="badge blue">' + esc(stopModeLabel(stopMode)) + '</span> ' + stopModeText + '</div>' +
    '<div class="help-list">' +
    '<div class="help-row"><strong>后台检查间隔</strong><span>控制后台多久刷新一次账号流量、实例状态和保活决策；页面右上角可以单独选择自动刷新间隔。</span></div>' +
    '<div class="help-row"><strong>地域缓存时间</strong><span>账号地域列表会缓存一段时间，避免每轮都调用 DescribeRegions。新开地域后可以缩短这个值或重启服务。</span></div>' +
    '<div class="help-row"><strong>重复启动保护间隔</strong><span>同一实例启动后，在这个时间内不会再次自动提交启动请求，防止重复调用 StartInstance。后台检查频率由刷新间隔控制。</span></div>' +
    '<div class="help-row"><strong>企业微信通知</strong><span>' + (notificationEnabled ? '已启用，通知事件按下方点选项发送。' : '未启用，开启后按点选事件发送企业微信应用消息。') + '</span></div>' +
    '<div class="help-row"><strong>通知事件</strong><span>后台自动启动、手工操作、等待人工决策、流量告警和错误告警可以分别选择。</span></div>' +
    '</div>';
}

function decisionText(decision, paused) {
  const d = decision || {};
  const title = d.kind === 'start' ? '将自动启动' : d.kind === 'manual_required' ? '等待人工决策' : '不需要后台动作';
  const reason = reasonLabel(d.reason);
  let html = '<div class="decision-box"><div class="decision-head"><strong>' + esc(title) + '</strong>';
  if (paused) html += '<span class="badge amber">手工暂停保活</span>';
  html += '</div><span>' + esc(reason) + '</span></div>';
  return html;
}

function operationText(op) {
  if (!op || !op.action || !op.occurred_at || String(op.occurred_at).startsWith('0001-')) {
    return '<span class="muted">-</span>';
  }
  const at = formatTime(op.occurred_at);
  let html = '<div class="operation-box"><strong>' + esc(actionLabel(op.action)) + '</strong><span>' + esc(at) + '</span>';
  if (op.success === false && op.message) {
    html += '<span class="error">' + esc(op.message) + '</span>';
  }
  html += '</div>';
  return html;
}

function statusBadge(status) {
  const cls = status === 'Running' ? 'green' : status === 'Stopped' ? 'red' : 'amber';
  return '<span class="badge ' + cls + '">' + esc(statusLabel(status)) + '</span>';
}

function policyLabel(value) {
  return {
    ignore_limit: '忽略流量限制继续保活',
    pause_when_exceeded: '流量超阈值后暂停保活',
    manual_only_when_exceeded: '流量超阈值后人工决策'
  }[value] || value || '-';
}

function stopModeLabel(value) {
  return {
    StopCharging: '节省停机',
    KeepCharging: '普通停机'
  }[value] || value || '-';
}

function defaultStopMode() {
  return (currentSettings && currentSettings.keep_alive.stop_mode) || 'StopCharging';
}

function effectiveStopModeValue(row, requestedStopMode) {
  const configured = requestedStopMode || defaultStopMode();
  if (configured === 'StopCharging' && row.instance_charge_type === 'PrePaid') return 'KeepCharging';
  return configured;
}

function effectiveStopModeLabel(row, requestedStopMode) {
  const configured = requestedStopMode || defaultStopMode();
  const effective = effectiveStopModeValue(row, configured);
  let text = stopModeLabel(effective);
  if (configured === 'StopCharging' && effective === 'KeepCharging') {
    text += '（包年包月不使用节省停机）';
  }
  return text;
}

function targetLabel(value) {
  return {
    disabled: '关闭保活',
    all: '保活所有实例',
    spot_only: '保活抢占式实例',
    include_list: '只保活指定实例'
  }[value] || value || '-';
}

function reasonLabel(value) {
  return {
    keep_alive_disabled: '保活总开关已关闭',
    target_disabled: '保活目标已关闭',
    instance_not_stopped: '实例当前不是 Stopped',
    manual_paused: '上次是手工关机，暂停后台保活',
    prepaid_keep_charging: '包年包月实例自动使用普通停机',
    target_not_matched: '实例不在当前保活目标范围',
    account_traffic_exceeded_manual_required: '实例所属流量额度池已超过阈值，页面仍可手工启动',
    account_traffic_exceeded_paused: '实例所属流量额度池已超过阈值，后台暂停自动启动',
    account_traffic_unknown_manual_required: '账号流量读取失败，交给人工决策',
    account_traffic_unknown_paused: '账号流量读取失败，后台暂停自动启动',
    start_cooldown: '距离上次启动还在重复启动保护间隔内',
    stopped_target: '已停机且符合保活条件'
  }[value] || value || '-';
}

function statusLabel(value) {
  return {
    Running: '运行中',
    Stopped: '已停机',
    Starting: '启动中',
    Stopping: '关机中'
  }[value] || value || '-';
}

function actionLabel(value) {
  return {
    manual_start: '手工启动',
    manual_stop: '手工关机',
    auto_start: '后台自动启动'
  }[value] || value || '-';
}

function siteLabel(value) {
  return {china: '中国站', international: '国际站'}[value] || esc(value || '-');
}

function scopeLabel(value) {
  return {mainland: '中国内地', overseas: '非中国内地', total: '账号合计'}[value] || value || '-';
}

function levelClass(level) {
  return {DEBUG: 'blue', INFO: 'green', WARN: 'amber', ERROR: 'red'}[level] || '';
}

function logModuleLabel(value) {
  return {
    server: '服务',
    config: '配置',
    state: '状态',
    time: '时区',
    refresh: '巡检任务',
    account: '流量任务',
    aliyun: '云 API',
    traffic: '流量任务',
    keepalive: '保活任务',
    notify: '通知',
    notification: '通知'
  }[value] || value || '-';
}

function logTaskLabel(value) {
  return {
    'listening': '服务监听',
    'started': '开始巡检',
    'finished': '巡检完成',
    'failed': '任务失败',
    'load failed': '加载失败',
    'load timezone failed': '时区加载失败',
    'cdt traffic detail': '账号流量明细',
    'cdt traffic threshold reached': '流量阈值告警',
    'cdt traffic unavailable': '账号流量读取失败',
    'describe regions failed': '地域发现失败',
    'describe instances failed': '实例发现失败',
    'describe network interfaces failed': 'IPv6 网卡读取失败',
    'cms instance traffic loaded': '实例流量明细',
    'cms instance traffic restored from cache': '实例流量明细',
    'cms instance traffic unavailable': '实例流量读取失败',
    'manual start blocked by repeat protection': '手工启动被保护间隔拦截',
    'manual start failed': '手工启动失败',
    'manual start submitted': '手工启动已提交',
    'manual stop failed': '手工关机失败',
    'manual stop submitted': '手工关机已提交',
    'auto start decision': '准备自动启动',
    'auto start failed': '自动启动失败',
    'auto start submitted': '自动启动已提交',
    'manual decision required': '等待人工决策',
    'decision skipped': '跳过保活动作',
    'check finished': '保活检查完成',
    'message sent': '消息已发送',
    'message send failed': '消息发送失败',
    'http server failed': 'HTTP 服务失败',
    'http shutdown failed': 'HTTP 关闭失败'
  }[value] || value || '-';
}

function fieldLabel(key) {
  const labels = {
    channel: '渠道',
    receivers: '接收人',
    total_traffic: '总流量',
    mainland_traffic: '中国内地流量',
    mainland_limit: '中国内地额度',
    overseas_traffic: '非中国内地流量',
    overseas_limit: '非中国内地额度'
  };
  if (Object.prototype.hasOwnProperty.call(labels, key)) return labels[key];
  return {
    account: '账号',
    accounts: '账号数',
    region: '地域',
    instance: '实例',
    instances: '实例数',
    error: '错误',
    errors: '错误数',
    event: '事件',
    status: '状态',
    reason: '原因',
    usage: '使用率',
    duration: '耗时',
    checked: '检查实例',
    starts: '自动启动',
    manual_required: '人工决策',
    skipped: '跳过',
    channel: '渠道',
    addr: '地址',
    config: '配置',
    web_dir: 'Web目录',
    path: '路径',
    tz: '时区',
    stop_mode: '停机模式',
    configured_stop_mode: '配置停机模式',
    used: '使用量',
    metric: '指标',
    points: '点数'
  }[key] || key;
}

function notifyEventLabel(value) {
  const event = NOTIFY_EVENTS.find(function(item) {
    return item.value === value;
  });
  return event ? event.label : (value || '-');
}

function fieldValue(key, value) {
  if (value === null || value === undefined || value === '') return '-';
  if (key === 'event') return notifyEventLabel(value);
  if (key === 'channel') {
    return {wechat: '企业微信'}[value] || value;
  }
  if (key === 'reason') return reasonLabel(value);
  if (key === 'status') return statusLabel(value);
  if (key === 'stop_mode' || key === 'configured_stop_mode') return stopModeLabel(value);
  return String(value);
}

function formatFields(fields) {
  return Object.keys(fields).sort().map(function(key) {
    return fieldLabel(key) + '：' + fieldValue(key, fields[key]);
  }).join('  ');
}

function cell(html) {
  const td = document.createElement('td');
  td.innerHTML = html;
  return td;
}

function logCell(html) {
  const div = document.createElement('div');
  div.className = 'log-cell';
  div.innerHTML = html;
  return div;
}

function logText(value) {
  const div = document.createElement('div');
  div.className = 'log-cell';
  div.textContent = value;
  return div;
}

function cellWithColspan(html, span) {
  const td = cell(html);
  td.colSpan = span;
  return td;
}

function textNode(value) {
  const div = document.createElement('div');
  div.textContent = value == null ? '' : String(value);
  return div;
}

function splitLines(value) {
  return value.split(/\r?\n/).map(function(item) { return item.trim(); }).filter(Boolean);
}

function text(id, value) {
  document.getElementById(id).textContent = value;
}

function clear(node) {
  while (node.firstChild) node.removeChild(node.firstChild);
}

function esc(value) {
  return String(value == null ? '' : value).replace(/[&<>"']/g, function(ch) {
    return {'&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;'}[ch];
  });
}

function attr(value) {
  return esc(value).replace(/\n/g, '&#10;');
}
