// ═══════════════════════════════════════════
// Kugelblitz Settings Client
// ═══════════════════════════════════════════

function switchSettingsTab(name) {
    document.querySelectorAll('.settings-tab').forEach(b => b.classList.remove('active'));
    document.querySelectorAll('.settings-tab-content').forEach(c => c.classList.remove('active'));
    document.querySelector('.settings-tab[onclick*="' + name + '"]')?.classList.add('active');
    var el = document.getElementById(name === 'files' ? 'settings-files-tab' : 'settings-config');
    if (el) el.classList.add('active');
    if (name === 'files') loadSettingsFiles();
}

const settingsState = {
    files: [],
    activeName: null,
    content: '',
    dirty: false,
};

const fileIcons = {
    '.yaml': '⚡', '.yml': '⚙️', '.md': '📝',
    '.json': '{ }', '.jsonl': '[ ]', '.mcp.json': '🔌', 'kugelblitz.yaml': '⚡',
};

function fileIcon(name) {
    for (const [ext, icon] of Object.entries(fileIcons))
        if (name.endsWith(ext)) return icon;
    return '📄';
}

// ═══ Provider Config ═══

async function loadConfigForm() {
    try {
        const res = await fetch('/api/settings/config');
        if (!res.ok) return;
        const cfg = await res.json();

        // Provider
        setVal('cfg-provider', cfg.provider_name, 'deepseek');
        setVal('cfg-model', cfg.model, 'deepseek-v4-flash');
        setVal('cfg-baseurl', cfg.base_url, 'https://api.deepseek.com');
        setVal('cfg-apikey', cfg.api_key, '');

        // Model Runtime
        setBool('cfg-stream', cfg.stream_mode, true);
        setBool('cfg-thinking', cfg.enable_thinking, true);
        setVal('cfg-reasoning-effort', cfg.reasoning_effort, 'high');

        // Runtime
        setNum('cfg-max-cycles', cfg.max_state_machine_cycles, 30);

        // Context Compress
        setNum('cfg-compress-attempts', cfg.compress_max_attempts, 1);
        setNum('cfg-compress-chars', cfg.compress_max_tool_result_chars, 4000);
        setNum('cfg-keep-last-n', cfg.compress_keep_last_n, 20);
        setNum('cfg-min-msgs', cfg.compress_min_messages, 10);

        // Target Drift
        setNum('cfg-review-interval', cfg.review_interval, 12);
        setNum('cfg-max-failures', cfg.max_failures_before_review, 5);

        // MCP
        loadMCPConfig(cfg.mcp_servers || {});

        document.getElementById('config-status').textContent = '';
        document.getElementById('config-status').className = 'config-status';
    } catch (e) { /* ignore */ }
}

function setVal(id, val, def) {
    document.getElementById(id).value = val || def;
}
function setBool(id, val, def) {
    document.getElementById(id).checked = (val !== undefined) ? val : def;
}
function setNum(id, val, def) {
    document.getElementById(id).value = (val !== undefined && val !== 0) ? val : def;
}

function onProviderChange() {
    const provider = document.getElementById('cfg-provider').value;
    const baseUrl = document.getElementById('cfg-baseurl');

    if (provider === 'openai') {
        if (!baseUrl.value || baseUrl.value.includes('deepseek'))
            baseUrl.value = 'https://api.openai.com/v1';
    } else {
        if (!baseUrl.value || baseUrl.value.includes('openai'))
            baseUrl.value = 'https://api.deepseek.com';
    }
}

function toggleApiKey() {
    const el = document.getElementById('cfg-apikey');
    el.type = el.type === 'password' ? 'text' : 'password';
}

async function saveConfig() {
    const statusEl = document.getElementById('config-status');
    statusEl.textContent = '保存中…';
    statusEl.className = 'config-status';

    const cfg = {
        provider_name:              document.getElementById('cfg-provider').value,
        model:                      document.getElementById('cfg-model').value,
        base_url:                   document.getElementById('cfg-baseurl').value.trim(),
        api_key:                    document.getElementById('cfg-apikey').value.trim(),
        stream_mode:                document.getElementById('cfg-stream').checked,
        enable_thinking:            document.getElementById('cfg-thinking').checked,
        reasoning_effort:           document.getElementById('cfg-reasoning-effort').value,
        max_state_machine_cycles:   parseInt(document.getElementById('cfg-max-cycles').value) || 30,
        compress_max_attempts:      parseInt(document.getElementById('cfg-compress-attempts').value) || 1,
        compress_max_tool_result_chars: parseInt(document.getElementById('cfg-compress-chars').value) || 4000,
        compress_keep_last_n:       parseInt(document.getElementById('cfg-keep-last-n').value) || 20,
        compress_min_messages:      parseInt(document.getElementById('cfg-min-msgs').value) || 10,
        review_interval:            parseInt(document.getElementById('cfg-review-interval').value) || 12,
        max_failures_before_review: parseInt(document.getElementById('cfg-max-failures').value) || 5,
        mcp_servers:                collectMCPServers(),
    };

    try {
        const res = await fetch('/api/settings/config', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(cfg),
        });
        const data = await res.json();
        if (data.status === 'saved') {
            statusEl.textContent = '✓ 配置已保存';
            statusEl.className = 'config-status';
        } else {
            statusEl.textContent = '✗ ' + (data.error || '保存失败');
            statusEl.className = 'config-status error';
        }
    } catch (e) {
        statusEl.textContent = '✗ ' + e.message;
        statusEl.className = 'config-status error';
    }
    setTimeout(() => { statusEl.textContent = ''; }, 3000);
}

// ═══ MCP Servers ═══

function loadMCPConfig(servers) {
    const container = document.getElementById('mcp-servers');
    container.innerHTML = '';
    for (const [name, srv] of Object.entries(servers)) {
        addMCPServer(name, srv.command, srv.args || [], srv.env || {});
    }
}

function addMCPServer(name, command, args, env) {
    name = name || '';
    command = command || '';
    args = args || [];
    env = env || {};

    const container = document.getElementById('mcp-servers');
    const idx = Date.now();
    const id = 'mcp-' + idx;

    const envStr = Object.entries(env).map(([k, v]) => k + '=' + v).join('\n');
    const argsStr = args.join(', ');

    const div = document.createElement('div');
    div.className = 'mcp-server-entry';
    div.id = id;
    div.innerHTML =
        '<div class="mcp-server-header">' +
            '<input type="text" class="mcp-srv-name" placeholder="服务器名称" value="' + escapeHtml(name) + '">' +
            '<button class="btn-remove-server" onclick="document.getElementById(\'' + id + '\').remove()">✕</button>' +
        '</div>' +
        '<label>Command</label>' +
        '<input type="text" class="mcp-srv-cmd" placeholder="npx" value="' + escapeHtml(command) + '">' +
        '<label>Args (逗号分隔)</label>' +
        '<input type="text" class="mcp-srv-args" placeholder="-y, @scope/server-name" value="' + escapeHtml(argsStr) + '">' +
        '<label>Env (KEY=VALUE, 每行一个)</label>' +
        '<textarea class="mcp-srv-env" rows="2" placeholder="GITHUB_TOKEN=ghp_xxx">' + escapeHtml(envStr) + '</textarea>';
    container.appendChild(div);
}

function collectMCPServers() {
    const servers = {};
    const entries = document.querySelectorAll('.mcp-server-entry');
    entries.forEach(entry => {
        const nameEl = entry.querySelector('.mcp-srv-name');
        const name = (nameEl && nameEl.value.trim()) || '';
        if (!name) return;

        const cmdEl = entry.querySelector('.mcp-srv-cmd');
        const cmd = (cmdEl && cmdEl.value.trim()) || '';
        if (!cmd) return;

        const argsEl = entry.querySelector('.mcp-srv-args');
        const argsRaw = (argsEl && argsEl.value.trim()) || '';
        const args = argsRaw ? argsRaw.split(',').map(s => s.trim()).filter(s => s) : [];

        const envEl = entry.querySelector('.mcp-srv-env');
        const envRaw = (envEl && envEl.value.trim()) || '';
        const env = {};
        if (envRaw) {
            envRaw.split('\n').forEach(line => {
                const eqIdx = line.indexOf('=');
                if (eqIdx > 0) {
                    env[line.substring(0, eqIdx).trim()] = line.substring(eqIdx + 1).trim();
                }
            });
        }

        servers[name] = { command: cmd, args: args, env: env };
    });
    return servers;
}

// ═══ File List ═══
async function loadSettingsFiles() {
    try {
        const res = await fetch('/api/settings/files');
        settingsState.files = await res.json();
        renderFileList();
    } catch (e) { /* ignore */ }
}

function renderFileList() {
    const c = document.getElementById('settings-files');
    let html = '';
    settingsState.files.forEach(f => {
        const active = settingsState.activeName === f.name ? ' active' : '';
        html += `<div class="file-item${active}" onclick="openFile('${escapeAttr(f.name)}')">
            <span class="file-icon">${fileIcon(f.name)}</span>${escapeHtml(f.name)}
        </div>`;
    });
    c.innerHTML = html;
}

// ═══ Open File ═══
async function openFile(name) {
    if (settingsState.dirty && !confirm('有未保存的更改，确定丢弃？')) return;

    settingsState.activeName = name;
    settingsState.dirty = false;
    updateSaveBtn();
    updateModifiedDot();
    renderFileList();

    document.getElementById('editor-filename').textContent = name;
    try {
        const res = await fetch(`/api/settings/file/${encodeURIComponent(name)}`);
        const data = await res.json();
        settingsState.content = data.content || '';
        document.getElementById('editor-content').value = settingsState.content;
    } catch (e) {
        document.getElementById('editor-content').value = '// 加载失败: ' + e.message;
    }
}

// Track changes
document.addEventListener('DOMContentLoaded', () => {
    const ta = document.getElementById('editor-content');
    ta.addEventListener('input', () => {
        if (settingsState.activeName && ta.value !== settingsState.content) {
            settingsState.dirty = true;
            updateSaveBtn();
            updateModifiedDot();
        }
    });
});

// ═══ Save File ═══
async function saveSettings() {
    if (!settingsState.activeName) return;
    const content = document.getElementById('editor-content').value;
    try {
        const res = await fetch(`/api/settings/file/${encodeURIComponent(settingsState.activeName)}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ content }),
        });
        const data = await res.json();
        if (data.status === 'saved') {
            settingsState.dirty = false;
            settingsState.content = content;
            updateSaveBtn();
            updateModifiedDot();
            showToast('✓ ' + settingsState.activeName + ' 已保存', 'success');
        } else {
            showToast('保存失败: ' + (data.error || 'unknown'), 'error');
        }
    } catch (e) {
        showToast('保存失败: ' + e.message, 'error');
    }
}

function updateSaveBtn() {
    document.getElementById('btn-save-settings').disabled = !settingsState.dirty;
}

function updateModifiedDot() {
    const dot = document.getElementById('modified-dot');
    dot.classList.toggle('show', settingsState.dirty);
}

// ═══ Key bindings ═══
document.addEventListener('keydown', e => {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
        e.preventDefault();
        if (document.getElementById('settings-panel').classList.contains('active'))
            saveSettings();
    }
});

// ═══ Helpers ═══
function escapeAttr(s) { return s.replace(/'/g, "\\'").replace(/"/g, '&quot;'); }
