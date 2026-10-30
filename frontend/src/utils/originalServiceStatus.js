  const OFFICIAL_BUCKET_MS = 30 * 60 * 1000;
  const OFFICIAL_BUCKET_COUNT = 48;
  const OFFICIAL_WINDOW_MS = OFFICIAL_BUCKET_MS * OFFICIAL_BUCKET_COUNT;
  const STATUS_SEVERITY = {
    operational: 0,
    under_maintenance: 1,
    degraded_performance: 2,
    partial_outage: 3,
    full_outage: 4
  };
  const STATUS_LABELS = {
    operational: '正常运行',
    under_maintenance: '维护中',
    degraded_performance: '性能下降',
    partial_outage: '部分中断',
    full_outage: '完全中断',
    identified: '已确认',
    investigating: '调查中',
    monitoring: '监控中',
    resolved: '已解决'
  };

  const escapeOfficialText = value => String(value ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');

  const formatOfficialDate = value => {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '';
    return new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: 'numeric', day: 'numeric' }).format(date);
  };

  const formatOfficialTime = value => {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '';
    return new Intl.DateTimeFormat('zh-CN', {
      timeZone: 'Asia/Shanghai', month: 'numeric', day: 'numeric',
      hour: '2-digit', minute: '2-digit', hourCycle: 'h23'
    }).format(date);
  };

  const getOfficialWindow = () => {
    const end = Math.floor(Date.now() / 60000) * 60000;
    return { start: end - OFFICIAL_WINDOW_MS, end };
  };

  const formatOfficialDuration = value => {
    const seconds = Math.max(0, Math.floor(value / 1000));
    if (seconds < 60) return `${seconds} 秒`;
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes} 分钟`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours} 小时`;
    return `${Math.floor(hours / 24)} 天`;
  };

  const statusLabel = value => STATUS_LABELS[value] || value || '未知状态';

  const statusColor = value => ({
    operational: 'operational',
    under_maintenance: 'maintenance',
    degraded_performance: 'degraded',
    partial_outage: 'partial',
    full_outage: 'outage'
  }[value] || 'nodata');

  const statusFromImpacts = (impacts, componentIds, bucketStart, bucketEnd) => {
    let status = 'operational';
    for (const impact of impacts || []) {
      if (!componentIds.has(impact.component_id)) continue;
      const started = Date.parse(impact.start_at || '') || 0;
      const ended = impact.end_at ? Date.parse(impact.end_at) : Number.POSITIVE_INFINITY;
      if (started < bucketEnd && ended > bucketStart && STATUS_SEVERITY[impact.status] > STATUS_SEVERITY[status]) status = impact.status;
    }
    return status;
  };

  const buildOfficialChart = (group, impacts, window) => {
    const componentIds = new Set((group.components || []).map(component => component.component_id));
    const width = 668;
    const gap = 3;
    const cellWidth = (width - gap * (OFFICIAL_BUCKET_COUNT - 1)) / OFFICIAL_BUCKET_COUNT;
    const step = cellWidth + gap;
    const rects = [];
    // 查询与绘图共用本次同步的窗口，避免重绘时错位或把未来时段画成正常。
    for (let index = 0; index < OFFICIAL_BUCKET_COUNT; index += 1) {
      const bucketStart = window.start + index * OFFICIAL_BUCKET_MS;
      const bucketEnd = bucketStart + OFFICIAL_BUCKET_MS;
      const status = statusFromImpacts(impacts, componentIds, bucketStart, bucketEnd);
      const label = `${formatOfficialTime(bucketStart)} — ${formatOfficialTime(bucketEnd)} · ${statusLabel(status)}`;
      rects.push(`<rect x="${(index * step).toFixed(3)}" y="0" width="${cellWidth.toFixed(3)}" height="16" rx="1" data-start="${new Date(bucketStart).toISOString()}" data-end="${new Date(bucketEnd).toISOString()}" class="ikun-openai-chart-${statusColor(status)}"><title>${escapeOfficialText(label)}</title></rect>`);
    }
    return `<svg class="ikun-openai-chart" viewBox="0 0 ${width} 16" preserveAspectRatio="none" role="img" aria-label="${escapeOfficialText(group.name)} 最近 24 小时状态，每格 30 分钟">${rects.join('')}</svg><div class="ikun-openai-chart-axis" aria-hidden="true"><span>${escapeOfficialText(formatOfficialTime(window.start))}</span><span>${escapeOfficialText(formatOfficialTime(window.end))}</span></div>`;
  };

  const getOfficialGroups = summary => (summary?.structure?.items || [])
    .map(item => item.group)
    .filter(group => group && !group.hidden && Array.isArray(group.components));

  const getGroupStatus = (group, summary) => {
    const affected = new Map((summary?.affected_components || []).map(item => [item.component_id, item.status]));
    let status = 'operational';
    for (const component of group.components || []) {
      const current = affected.get(component.component_id) || 'operational';
      if (STATUS_SEVERITY[current] > STATUS_SEVERITY[status]) status = current;
    }
    return status;
  };

  const getGroupUptime = (group, history) => history?.component_uptimes?.find(item => item.status_page_component_group_id === group.id)?.uptime;

  const getIncidentGroups = (incident, groups) => {
    const affected = new Set((incident?.affected_components || []).map(item => item.component_id));
    return groups.filter(group => (group.components || []).some(component => affected.has(component.component_id))).map(group => group.name);
  };

  const buildOfficialIssuePanel = data => {
    const summary = data.summary || {};
    const groups = getOfficialGroups(summary);
    const incidents = summary.ongoing_incidents || [];
    const isHealthy = incidents.length === 0 && (summary.affected_components || []).length === 0;
    const title = isHealthy ? '当前服务运行正常' : '我们目前遇到了一些问题';
    const body = incidents.length === 0
      ? '<p class="ikun-openai-issue-empty">官方状态页暂未报告进行中的事件。</p>'
      : incidents.map(incident => {
        const update = incident.updates?.[incident.updates.length - 1];
        const message = update?.message_string || incident.write_up_contents || '';
        const affectedNames = getIncidentGroups(incident, groups);
        const started = incident.published_at || incident.component_impacts?.[0]?.start_at;
        const status = incident.status || incident.status_summaries?.[0]?.worst_component_status;
        const paragraphs = escapeOfficialText(message).split(/\n{2,}/).map(text => `<p>${text.replaceAll('\n', '<br>')}</p>`).join('');
        return `<article class="ikun-openai-incident"><div class="ikun-openai-incident-badge">${affectedNames.map(name => `<span>${escapeOfficialText(name)}</span>`).join('')}</div><h3>${escapeOfficialText(incident.name || 'OpenAI 服务事件')}</h3><div class="ikun-openai-incident-copy">${paragraphs}</div><p class="ikun-openai-incident-meta">${escapeOfficialText(statusLabel(status))}<span>·</span>${started ? `持续 ${escapeOfficialText(formatOfficialDuration(Date.now() - Date.parse(started)))}` : '刚刚更新'}${affectedNames.length ? `<span>·</span>影响 ${escapeOfficialText(affectedNames.join('、'))}` : ''}</p></article>`;
      }).join('');
    const updatedAt = summary.ongoing_incidents?.[0]?.updates?.at(-1)?.published_at || new Date(data.fetchedAt || data.window.end).toISOString();
    return `<section class="ikun-openai-panel ikun-openai-issue-panel ${isHealthy ? 'is-healthy' : 'is-degraded'}" aria-label="OpenAI 官方状态摘要"><header class="ikun-openai-panel-header"><div class="ikun-openai-panel-title"><span class="ikun-openai-status-icon">${isHealthy ? '✓' : '!'}</span><h2>${title}</h2></div></header><div class="ikun-openai-panel-body">${body}</div><footer class="ikun-openai-panel-footer">更新于 ${escapeOfficialText(formatOfficialDate(updatedAt))} · <a href="https://status.openai.com/" target="_blank" rel="noopener noreferrer">查看官方状态</a></footer></section>`;
  };

  const buildOfficialSystemPanel = data => {
    const summary = data.summary || {};
    const groups = getOfficialGroups(summary);
    const history = data.history || {};
    const window = data.window;
    const rows = groups.map(group => {
      const status = getGroupStatus(group, summary);
      const uptime = getGroupUptime(group, history);
      const components = (group.components || []).map(component => `<li><span class="ikun-openai-component-dot ikun-openai-component-${statusColor('operational')}"></span><span>${escapeOfficialText(component.name)}</span></li>`).join('');
      return `<div class="ikun-openai-group" data-openai-group="${escapeOfficialText(group.id)}"><button type="button" class="ikun-openai-group-header" aria-expanded="false"><span class="ikun-openai-group-name"><span class="ikun-openai-status-icon ikun-openai-status-icon-small ikun-openai-status-${statusColor(status)}">${status === 'operational' ? '✓' : '!'}</span><strong>${escapeOfficialText(group.name)}</strong><span class="ikun-openai-group-count">${group.components.length} 个组成部分</span></span><span class="ikun-openai-group-uptime">${uptime ? `${escapeOfficialText(uptime)}% 官方可用率` : '暂无数据'}<span class="ikun-openai-chevron">›</span></span></button><div class="ikun-openai-group-chart">${buildOfficialChart(group, history.component_impacts || [], window)}</div><ul class="ikun-openai-group-components">${components}</ul></div>`;
    }).join('');
    return `<section class="ikun-openai-panel ikun-openai-system-panel" aria-label="OpenAI 官方系统状态"><header class="ikun-openai-panel-header ikun-openai-system-header"><div class="ikun-openai-panel-title"><h2>系统状态</h2><span class="ikun-openai-period">近 24 小时 · 每格 30 分钟</span></div></header><div class="ikun-openai-system-body">${rows}</div></section>`;
  };

  const buildOfficialStack = data => `<div class="ikun-openai-status-stack">${buildOfficialIssuePanel(data)}${buildOfficialSystemPanel(data)}</div>`;


export { buildOfficialStack, getOfficialWindow };
