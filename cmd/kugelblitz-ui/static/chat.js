// ═══════════════════════════════════════════
// Kugelblitz Chat Client
// ═══════════════════════════════════════════

const state = {
    sessionId: null,
    activePlanId: null,
    thinkingId: null,
    replyId: null,
    hitlWaiting: false,
    processing: false,
    totalTokens: 0,
    abortController: null,
    tokenReports: [],
    tokenCumulative: { input:0, output:0, reasoning:0, total:0 },
    maxSourceTokens: 1,
};

const welcomeEl = document.getElementById('welcome');
const messagesEl = document.getElementById('messages');
const goalInput = document.getElementById('goal-input');
const sendBtn = document.getElementById('send-btn');

document.addEventListener('DOMContentLoaded', () => {
    loadSessions();
    updateWelcome();
});

function updateWelcome() {
    welcomeEl.style.display = messagesEl.children.length === 0 ? 'flex' : 'none';
}

// ═══ Right Panel Tabs ═══
function switchRightTab(name) {
    document.querySelectorAll('.right-tab').forEach(b => b.classList.remove('active'));
    document.querySelectorAll('.right-content').forEach(c => c.classList.remove('active'));
    document.querySelector(`.right-tab[onclick*="${name}"]`)?.classList.add('active');
    const content = document.getElementById('right-' + name);
    if (content) content.classList.add('active');
}

// ═══ Session Management ═══
async function loadSessions() {
    try {
        const res = await fetch('/api/session');
        const sessions = await res.json();
        const sel = document.getElementById('session-select');
        sel.innerHTML = '<option value="">— 历史会话 —</option>';
        if (Array.isArray(sessions)) {
            sessions.forEach(s => {
                const opt = document.createElement('option');
                opt.value = s.id;
                let label = s.id;
                if (s.goal) label += ' — ' + (s.goal.length > 30 ? s.goal.substring(0,30) + '…' : s.goal);
                if (s.turn_count) label += ` [${s.turn_count}轮]`;
                opt.textContent = label;
                sel.appendChild(opt);
            });
        }
    } catch (e) { /* ignore */ }
}

async function newSession() {
    try {
        const res = await fetch('/api/session', { method: 'POST' });
        const data = await res.json();
        state.sessionId = data.session_id;
        resetAll();
        loadSessions();
    } catch (e) { /* ignore */ }
}

async function switchSession(id) {
    if (!id) return;
    state.sessionId = id;
    resetAll();
    document.getElementById('current-session').textContent = id;

    // Load full history from Web UI's own storage
    try {
        const res = await fetch(`/api/session/${encodeURIComponent(id)}`);
        if (!res.ok) { console.warn('session not found:', id); return; }
        const data = await res.json();
        messagesEl.innerHTML = '';
        if (data.turns && Array.isArray(data.turns)) {
            data.turns.forEach((turn, ti) => {
                // Turn separator (except first)
                if (ti > 0) {
                    appendMessage('system', '🔄', '新一轮对话', `<p><em>${escapeHtml(turn.goal || '')}</em></p>`);
                }
                if (turn.messages && Array.isArray(turn.messages)) {
                    turn.messages.forEach(m => renderStoredMessage(m));
                }
            });
        }
        // Restore usage
        if (data.total_usage) {
            state.tokenCumulative = {
                input: data.total_usage.input || 0,
                output: data.total_usage.output || 0,
                reasoning: data.total_usage.reasoning || 0,
                total: data.total_usage.total || 0
            };
            updateUsagePanel();
            document.getElementById('token-counter').textContent = fmtNum(state.tokenCumulative.total) + ' tokens';
        }
        updateWelcome();
        scrollDown();
    } catch (e) {
        console.error('load session error:', e);
    }
}

function renderStoredMessage(m) {
    switch (m.role) {
        case 'user':
            appendMessage('user', '👤', 'You', `<p>${escapeHtml(m.content)}</p>`);
            break;
        case 'assistant':
            appendMessage('assistant', '🤖', 'Agent', `<div class="content">${marked.parse(m.content || '')}</div>`);
            break;
        case 'think':
            appendMessage('thinking', '💭', '思考过程', `<div class="think-body">${marked.parse(m.content || '')}</div>`);
            break;
        case 'tool_call':
            appendMessage('tool_call', '🔧', m.tool_name || 'tool', `<pre>${escapeHtml(JSON.stringify(m.tool_args, null, 2))}</pre>`);
            break;
        case 'tool_result':
            appendMessage('tool_result', '📋', m.tool_name || 'result', `<pre>${escapeHtml(JSON.stringify(m.tool_out, null, 2))}</pre>`);
            break;
        case 'system':
            appendMessage('system', '📌', 'System', `<p>${m.content}</p>`);
            break;
        case 'error':
            appendMessage('error', '❌', 'Error', `<p>${escapeHtml(m.content)}</p>`);
            break;
        default:
            appendMessage('system', '💬', m.role, `<p>${escapeHtml(m.content)}</p>`);
    }
}

function resetAll() {
    messagesEl.innerHTML = '';
    state.thinkingId = null;
    state.replyId = null;
    state.activePlanId = null;
    state.totalTokens = 0;
    state.tokenReports = [];
    state.tokenCumulative = { input:0, output:0, reasoning:0, total:0 };
    state.maxSourceTokens = 1;
    updateWelcome();
    updateStatus('就绪', 'connected');
    document.getElementById('current-session').textContent = state.sessionId || '';
    document.getElementById('session-info').textContent = '';
    resetPlanPanel();
    resetUsagePanel();
    document.getElementById('token-counter').textContent = '';
}

// ═══ Stop ═══
async function stopSession() {
    if (!state.processing) return;

    if (state.abortController) {
        state.abortController.abort();
        state.abortController = null;
    }

    if (state.sessionId) {
        try {
            await fetch(`/api/cancel/${state.sessionId}`, { method: 'POST' });
        } catch (e) { /* ignore */ }
    }

    state.hitlWaiting = false;
    document.getElementById('hitl-dialog').classList.remove('active');

    finishSend('connected');
    appendMessage('system', '⏹', 'System', '<p>会话已中断</p>');
}

// ═══ Send ═══
async function sendMessage() {
    const goal = goalInput.value.trim();
    if (!goal) return;
    goalInput.value = '';
    goalInput.disabled = true;
    sendBtn.style.display = 'none';
    document.getElementById('stop-btn').style.display = 'flex';
    state.processing = true;

    var isFollowUp = messagesEl.children.length > 0;
    if (!isFollowUp) {
        messagesEl.innerHTML = '';
        state.tokenReports = [];
        state.tokenCumulative = { input:0, output:0, reasoning:0, total:0 };
        state.maxSourceTokens = 1;
        resetUsagePanel();
        updateWelcome();
    }
    state.thinkingId = null;
    state.replyId = null;

    appendMessage('user', '👤', 'You', `<p>${escapeHtml(goal)}</p>`);
    updateStatus('处理中…', 'processing');
    document.getElementById('current-session').textContent = state.sessionId || '';

    state.abortController = new AbortController();
    try {
        const res = await fetch('/api/chat', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ session_id: state.sessionId, goal }),
            signal: state.abortController.signal,
        });
        if (!res.ok) {
            const errData = await res.json().catch(() => ({}));
            appendMessage('error', '❌', 'Error', `<p>${escapeHtml(errData.error || 'HTTP ' + res.status)}</p>`);
            finishSend('error');
            return;
        }
        await readSSEStream(res);
    } catch (e) {
        if (e.name !== 'AbortError') appendMessage('error', '❌', 'Error', `<p>${escapeHtml(e.message)}</p>`);
    }
    finishSend('connected');
}

function finishSend(dotState) {
    state.processing = false;
    goalInput.disabled = false;
    sendBtn.style.display = 'flex';
    document.getElementById('stop-btn').style.display = 'none';
    goalInput.focus();
    state.thinkingId = null;
    state.abortController = null;
    updateStatus('就绪', dotState || 'connected');
    loadSessions();
}

// ═══ SSE Reader ═══
async function readSSEStream(response) {
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buf = '';
    while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        const lines = buf.split('\n');
        buf = lines.pop() || '';
        let et = '';
        for (const line of lines) {
            if (line.startsWith('event: ')) { et = line.slice(7).trim(); }
            else if (line.startsWith('data: ')) {
                try { handleEvent(et, JSON.parse(line.slice(6))); } catch (e) { console.error('SSE parse error:', et, e); }
                et = '';
            }
        }
    }
}

// ═══ Event Router ═══
function handleEvent(type, data) {
    switch (type) {
        case 'think':         onThink(data); break;
        case 'reply':         onReply(data); break;
        case 'tool_call':     onToolCall(data); break;
        case 'tool_result':   onToolResult(data); break;
        case 'plan_update':   onPlanUpdate(data); break;
        case 'plan_rollback': onPlanRollback(data); break;
        case 'hitl':          onHitl(data); break;
        case 'done':          onDone(data); break;
        case 'error':         onError(data); break;
        case 'usage':         onUsage(data); break;
        case 'token_report':  onTokenReport(data); break;
        case 'task_updated':  onTaskUpdated(data); break;
    }
}

// ── Think ──
function onThink(data) {
    if (!data.text) return;
    if (data.identity) updateAgentIdentity(data.identity); closeReply();
    if (!state.thinkingId) {
        state.thinkingId = createThinkBlock(data.text);
    } else {
        const el = document.getElementById(state.thinkingId);
        if (el) {
            el.dataset.raw = (el.dataset.raw||'') + data.text;
            el.querySelector('.think-body').innerHTML = marked.parse(el.dataset.raw);
        }
    }
}

function createThinkBlock(text) {
    const id = 'think-' + Date.now(), div = document.createElement('div');
    div.className = 'message thinking'; div.id = id; div.dataset.raw = text;
    div.innerHTML = `<div class="think-header" onclick="toggleThink(this)">
        <span class="icon">💭</span><span class="label">思考中…</span>
        <span class="think-arrow">▸</span>
    </div><div class="think-body collapsed">${marked.parse(text)}</div>`;
    messagesEl.appendChild(div); scrollDown(); updateWelcome(); return id;
}

function toggleThink(hdr) {
    const body = hdr.nextElementSibling;
    const arrow = hdr.querySelector('.think-arrow');
    if (body.classList.contains('collapsed')) {
        body.classList.remove('collapsed'); arrow.textContent = '▾';
        hdr.querySelector('.label').textContent = '思考过程';
    } else {
        body.classList.add('collapsed'); arrow.textContent = '▸';
        hdr.querySelector('.label').textContent = '思考过程';
    }
}

function closeThink() {
    if (state.thinkingId) {
        const el = document.getElementById(state.thinkingId);
        if (el) el.querySelector('.label').textContent = '思考过程';
        state.thinkingId = null;
    }
}

// ── Reply ──
function onReply(data) {
    if (!data.text) return;
    if (data.identity) updateAgentIdentity(data.identity); closeThink();
    if (!state.replyId) {
        state.replyId = createReplyBlock(data.text);
    } else {
        const el = document.getElementById(state.replyId);
        if (el) {
            el.dataset.raw = (el.dataset.raw||'') + data.text;
            el.querySelector('.content').innerHTML = marked.parse(el.dataset.raw);
        }
    }
}

function createReplyBlock(text) {
    const id = 'msg-' + Date.now(), div = document.createElement('div');
    div.className = 'message assistant'; div.id = id; div.dataset.raw = text;
    div.innerHTML = `<div class="header"><span class="icon">🤖</span><span class="label">Agent</span></div><div class="content">${marked.parse(text)}</div>`;
    messagesEl.appendChild(div); scrollDown(); updateWelcome(); return id;
}

function closeReply() { state.replyId = null; }

// ── Tool Call ──
function onToolCall(data) {
    closeThink(); closeReply();
    if (data.identity) updateAgentIdentity(data.identity);
    const id = 'tc-'+data.tool_call_id, div = document.createElement('div');
    div.className = 'message tool_call'; div.id = id;
    div.innerHTML = `<div class="header" onclick="toggleMsg(this)"><span class="icon">🔧</span><span class="label">${escapeHtml(data.tool_name)}</span><span class="expand-icon">▾</span></div><div class="body" style="max-height:400px"><div class="tool-name">调用参数</div><pre>${escapeHtml(JSON.stringify(data.args,null,2))}</pre></div>`;
    messagesEl.appendChild(div); scrollDown(); updateWelcome();
}

// ── Tool Result ──
function onToolResult(data) {
    closeThink(); closeReply();
    const output = data.output || {}, isErr = !!output.error;

    const callEl = document.getElementById('tc-' + data.tool_call_id);
    if (callEl) {
        const body = callEl.querySelector('.body');
        if (body) {
            const sec = document.createElement('div');
            sec.className = 'tool-result' + (isErr ? ' error' : '');
            sec.innerHTML = '<div class="tool-name">调用结果</div><pre>' +
                escapeHtml(JSON.stringify(output, null, 2)) + '</pre>';
            body.appendChild(sec);
            body.classList.remove('collapsed');
            body.style.maxHeight = 'none';
            callEl.querySelector('.expand-icon').textContent = '▾';
        }
        return;
    }

    let sum = '';
    if (isErr) sum = '<span style="color:var(--red)">' + escapeHtml(String(output.error)) + '</span>';
    else {
        const keys = Object.keys(output);
        if (keys.length === 0) sum = '<span style="color:var(--text-tertiary)">(empty)</span>';
        else if (keys.length <= 2 && typeof output[keys[0]] === 'string' && output[keys[0]].length < 100)
            sum = keys.map(k => '<b style="color:var(--text-secondary)">' + k + ':</b> ' + escapeHtml(String(output[k]))).join('<br>');
        else sum = '<span style="color:var(--text-tertiary)">' + keys.length + ' 字段: ' + keys.slice(0, 4).join(', ') + (keys.length > 4 ? '…' : '') + '</span>';
    }
    const div = document.createElement('div');
    div.className = 'message tool_result' + (isErr ? ' error' : '');
    div.id = 'tr-' + data.tool_call_id;
    div.innerHTML = '<div class="header" onclick="toggleMsg(this)"><span class="icon">' + (isErr ? '❌' : '📋') +
        '</span><span class="label">' + escapeHtml(data.tool_name) +
        '</span><span class="expand-icon">▾</span></div>' +
        '<div class="body" style="max-height:300px"><div class="result-preview">' + sum +
        '</div><pre style="margin-top:8px">' + escapeHtml(JSON.stringify(output, null, 2)) + '</pre></div>';
    messagesEl.appendChild(div); scrollDown(); updateWelcome();
}

// ── Plan Update ──
function onPlanUpdate(data) {
    // New plan_id replaces old plan
    if (data.plan_id !== state.activePlanId) {
        resetPlanPanel();
    }
    state.activePlanId = data.plan_id;
    document.getElementById('plan-name').textContent = data.name||'—';
    const st = document.getElementById('plan-status'), status = data.status||'init';
    st.style.display = 'inline-flex'; st.className = 'plan-status '+status;
    st.querySelector('.status-label').textContent = status.toUpperCase();
    document.getElementById('session-info').textContent = `plan: ${data.name||'—'} · ${status}`;

    const tl = document.getElementById('task-list');
    if (!data.tasks||data.tasks.length===0) {
        tl.innerHTML = '<div class="no-plan"><div class="empty-icon">📋</div><div>暂无任务</div></div>';
    } else {
        const icons = {done:'✅',doing:'🔄',pending:'⏳',failed:'❌'};
        tl.innerHTML = data.tasks.map(t=>`<div class="task-item" data-task-id="${t.id}"><span class="task-icon ${t.status}">${icons[t.status]||'⏳'}</span><span class="task-goal">${escapeHtml(t.goal)}</span></div>`).join('');
    }
    const counts={done:0,doing:0,pending:0,failed:0};
    data.tasks.forEach(t=>{ if(counts[t.status]!==undefined) counts[t.status]++; });
    document.getElementById('plan-info').textContent = `${data.tasks.length} 任务 · ${counts.done} 完成 · ${counts.doing} 进行中`;
}

function resetPlanPanel() {
    document.getElementById('plan-name').textContent='暂无计划';
    const st=document.getElementById('plan-status'); st.style.display='none'; st.className='plan-status init';
    document.getElementById('plan-info').textContent='';
    document.getElementById('task-list').innerHTML='<div class="no-plan"><div class="empty-icon">📋</div><div>Agent 尚未创建计划</div></div>';
}

// ── Task Updated ──
function onTaskUpdated(data) {
    const el = document.querySelector(`.task-item[data-task-id="${data.task_id}"]`);
    if (el) {
        const icon = el.querySelector('.task-icon');
        if (icon) {
            const icons = {done:'✅',doing:'🔄',pending:'⏳',failed:'❌'};
            icon.textContent = icons[data.status] || '⏳';
            icon.className = 'task-icon ' + data.status;
        }
    }
}

// ── Plan Rollback ──
function onPlanRollback(data) {
    const version = data.target_version || '?';
    const name = escapeHtml(data.plan_name || '');
    const div = document.createElement('div');
    div.className = 'message system';
    div.innerHTML = `<div class="content" style="background:var(--yellow-bg, #fff8e1);border-left:3px solid var(--yellow, #f9a825);padding:10px 14px;border-radius:4px;">
        <strong>⚠️ 计划已自动回滚至版本 ${version}</strong><br>
        审查发现执行可能偏离目标 <em>${name}</em>，已恢复到上一版本。Agent 将在下一轮回复中确认是否继续。
    </div>`;
    messagesEl.appendChild(div); scrollDown(); updateWelcome();
    updateStatus('计划已回滚', 'processing');
}

// ── HITL ──
function onHitl(data) {
    try {
        if (data.session_id) state.sessionId = data.session_id;
        state.hitlWaiting = true;
        var d = document.getElementById('hitl-dialog');
        if (!d) { console.error('hitl-dialog missing'); return; }

        var el = document.getElementById('hitl-reason');
        if (el) el.innerHTML = data.reason ? marked.parse('**原因：** '+data.reason) : '';
        el = document.getElementById('hitl-question');
        if (el) el.innerHTML = marked.parse(data.question||'需要您的输入');

        var btns = document.getElementById('hitl-buttons');
        if (!btns) { console.error('hitl-buttons missing'); return; }
        if (data.source === 'planner_confirm') {
            btns.innerHTML =
                '<button class="btn-confirm" onclick="respondHITL(\'批准\')">✓ 批准</button>' +
                '<button class="btn-replan" onclick="respondHITL(\'重新规划\')">🔄 重新规划</button>' +
                '<button class="btn-reject" onclick="respondHITL(\'拒绝\')">✗ 拒绝</button>' +
                '<input type="text" id="hitl-custom" placeholder="自定义回复…">' +
                '<button class="btn-custom" onclick="respondHITL(document.getElementById(\'hitl-custom\').value)">发送</button>';
        } else {
            btns.innerHTML =
                '<button class="btn-confirm" onclick="respondHITL(\'确认\')">✓ 确认</button>' +
                '<button class="btn-reject" onclick="respondHITL(\'拒绝\')">✗ 拒绝</button>' +
                '<input type="text" id="hitl-custom" placeholder="自定义回复…">' +
                '<button class="btn-custom" onclick="respondHITL(document.getElementById(\'hitl-custom\').value)">发送</button>';
        }

        d.classList.add('active');
        updateStatus('等待输入…', 'processing');
        var inp = document.getElementById('hitl-custom');
        if (inp) { inp.value = ''; inp.focus(); }
    } catch(e) {
        console.error('onHitl error:', e);
    }
}

async function respondHITL(response) {
    if (!state.hitlWaiting||!response) return;
    state.hitlWaiting = false;
    document.getElementById('hitl-dialog').classList.remove('active');
    updateStatus('处理中…','processing');
    try {
        const r=await fetch(`/api/hitl/${state.sessionId}`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({response})});
        if(!r.ok){const e=await r.json();showToast('HITL 失败: '+(e.error||'unknown'),'error');}
    } catch(e) { showToast('HITL 请求失败: '+e.message,'error'); }
}

// ── Done ──
function onDone(data) {
    closeThink(); closeReply();
    if (data.session_id) state.sessionId = data.session_id;
    if (data.usage) {
        state.totalTokens += data.usage.total || 0;
        document.getElementById('token-counter').textContent = fmtNum(state.totalTokens)+' tokens';
    }
    updateStatus('完成','connected');
    loadSessions();
}

// ── Error ──
function onError(data) {
    closeThink(); closeReply();
    appendMessage('error','❌','Error',`<p>${escapeHtml(data.message||'Unknown error')}</p>`);
    updateStatus('错误','disconnected');
}

// ── Usage (per-LLM-call; now also updates the right panel) ──
function onUsage(data) {
    // Status bar
    if (data.total) {
        state.totalTokens += data.total; if (data.identity) updateAgentIdentity(data.identity);
        document.getElementById('token-counter').textContent = fmtNum(state.totalTokens)+' tokens';
    }
    // Right panel — update cumulative from every LLM call
    if (data.input)  state.tokenCumulative.input += data.input;
    if (data.output) state.tokenCumulative.output += data.output;
    if (data.reasoning) state.tokenCumulative.reasoning += data.reasoning;
    if (data.total) state.tokenCumulative.total += data.total;

    // Add to per-source reports
    if (data.identity) {
        state.tokenReports.push({
            identity: data.identity,
            input: data.input || 0,
            output: data.output || 0,
            reason: data.reasoning || 0,
            total: data.total || 0
        });
    }

    updateUsagePanel();
}

// ── Token Report (per-identity breakdown at turn end) ──
function onTokenReport(data) {
    if (data.identity !== 'total') {
        if (data.input) state.tokenCumulative.input += data.input;
        if (data.output) state.tokenCumulative.output += data.output;
        if (data.reasoning) state.tokenCumulative.reasoning += data.reasoning;
        if (data.total) state.tokenCumulative.total += data.total;
    } else {
        state.tokenCumulative.input = data.input || state.tokenCumulative.input;
        state.tokenCumulative.output = data.output || state.tokenCumulative.output;
        state.tokenCumulative.reasoning = data.reasoning || state.tokenCumulative.reasoning;
        state.tokenCumulative.total = data.total || state.tokenCumulative.total;
    }

    if (data.reports && Array.isArray(data.reports)) {
        state.tokenReports = data.reports;
    } else if (data.identity && data.identity !== 'total') {
        state.tokenReports.push(data);
    }

    updateUsagePanel();
}

function updateUsagePanel() {
    const c = state.tokenCumulative;
    const total = c.total || 1;

    document.getElementById('usage-input').textContent = fmtNum(c.input);
    document.getElementById('usage-output').textContent = fmtNum(c.output);
    document.getElementById('usage-reasoning').textContent = fmtNum(c.reasoning);
    document.getElementById('usage-total').textContent = fmtNum(c.total);

    const pctI = Math.round(c.input/total*100);
    const pctO = Math.round(c.output/total*100);
    const pctR = Math.round(c.reasoning/total*100);
    document.getElementById('bar-input').style.width = pctI+'%';
    document.getElementById('bar-output').style.width = pctO+'%';
    document.getElementById('bar-reasoning').style.width = pctR+'%';

    const container = document.getElementById('usage-by-source');
    if (!state.tokenReports.length) {
        container.innerHTML = '<div class="usage-empty">等待用量数据…</div>';
        return;
    }

    const maxTotal = Math.max(1, ...state.tokenReports.map(r=>r.total||0));
    container.innerHTML = state.tokenReports.map(r => {
        const pct = Math.round((r.total||0)/maxTotal*100);
        return `<div class="usage-source-item">
            <span class="src-id" title="${escapeHtml(r.identity)}">${escapeHtml(r.identity)}</span>
            <span class="src-tokens">${fmtNum(r.total||0)}</span>
            <div class="src-bar-wrap"><div class="src-bar" style="width:${pct}%"></div></div>
        </div>`;
    }).join('');
}

function resetUsagePanel() {
    document.getElementById('usage-input').textContent = '0';
    document.getElementById('usage-output').textContent = '0';
    document.getElementById('usage-reasoning').textContent = '0';
    document.getElementById('usage-total').textContent = '0';
    document.getElementById('bar-input').style.width = '0%';
    document.getElementById('bar-output').style.width = '0%';
    document.getElementById('bar-reasoning').style.width = '0%';
    document.getElementById('usage-by-source').innerHTML = '<div class="usage-empty">等待用量数据…</div>';
}

// ═══ Helpers ═══
function appendMessage(type, icon, label, content) {
    const d = document.createElement('div');
    d.className = `message ${type}`;
    d.innerHTML = `<div class="header"><span class="icon">${icon}</span><span class="label">${escapeHtml(label)}</span></div><div class="content">${content}</div>`;
    messagesEl.appendChild(d); scrollDown(); updateWelcome();
}

function toggleMsg(hdr) {
    const body = hdr.nextElementSibling;
    if (body) body.classList.toggle('collapsed');
    const icon = hdr.querySelector('.expand-icon');
    if (icon) icon.textContent = body.classList.contains('collapsed') ? '▸' : '▾';
}

function scrollDown() { messagesEl.scrollTop = messagesEl.scrollHeight; }

function updateStatus(text, dotState) {
    document.getElementById('status-text').textContent = text;
    document.getElementById('status-dot').className = 'dot ' + (dotState||'connected');
}

function updateAgentIdentity(id) {
    if (id) document.getElementById('session-info').textContent = 'agent: ' + id;
}

function fmtNum(n) {
    if (!n || n<0) return '0';
    if (n>=1e6) return (n/1e6).toFixed(1)+'M';
    if (n>=1e3) return (n/1e3).toFixed(1)+'k';
    return String(Math.round(n));
}

function escapeHtml(s) { const d=document.createElement('div'); d.textContent=s; return d.innerHTML; }

function showToast(msg, type) {
    const t=document.createElement('div');
    t.className='toast toast-'+(type||'success'); t.textContent=msg;
    document.body.appendChild(t);
    setTimeout(()=>{t.style.opacity='0';t.style.transition='opacity .3s'});
    setTimeout(()=>t.remove(),2300);
}

// ═══ Tab Switch ═══
function switchTab(tab) {
    const chat=document.getElementById('chat-panel'), settings=document.getElementById('settings-panel');
    const right=document.getElementById('right-panel');
    const tChat=document.getElementById('tab-chat'), tSettings=document.getElementById('tab-settings');
    if (tab==='settings') {
        chat.style.display='none'; right.style.display='none';
        settings.classList.add('active');
        tChat.classList.remove('active'); tSettings.classList.add('active');
        loadConfigForm(); loadSettingsFiles();
    } else {
        chat.style.display='flex'; right.style.display='flex';
        settings.classList.remove('active');
        tChat.classList.add('active'); tSettings.classList.remove('active');
    }
}
