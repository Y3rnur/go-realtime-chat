const DEMO_USER_ID = "3e62ced9-275f-4e96-82f1-d505730df6af";

const WS_TOKEN = "";

const api = {
    conversations: (userId) => `/api/conversations?user_id=${encodeURIComponent(userId)}`,
    messages: (conversationId) => `/api/messages?conversation_id=${encodeURIComponent(conversationId)}`,
};

const conversationsEl = document.getElementById("conversations");
const messagesEl = document.getElementById("messages");
const chatNameEl = document.getElementById("chat-name");
const chatSubEl = document.getElementById("chat-sub");
const composer = document.getElementById("composer");
const inputMsg = document.getElementById("input-msg");
const sidebar = document.getElementById("sidebar");
const backBtn = document.getElementById("back-btn");

let state = {
    me: DEMO_USER_ID,
    convs: [],
    active: null,
    messages: {},
    _messagesReqId: 0,
};

let messagesFetchController = null;
let wsConn = null;

// For reconnection purposes
let reconnectAttempts = 0;
let reconnectTimer = null;
const RECONNECT_BASE_MS = 500;
const RECONNECT_MAX_MS = 30000;
const RECONNECT_MAX_ATTEMPTS = 0;

// For debounce purposes
let wsConnectDebounceTimer = null;
const WS_CONNECT_DEBOUNCE_MS = 150;

let manualClose = false;    // will be true if caller intentionally closes ws

function init() {
    if (!state.me || state.me === "3e62ced9-275f-4e96-82f1-d505730df6af") {
        console.warn("Set DEMO_USER_ID in frontend/script.js to a real user UUID.");
    }
    loadConversations();
    composer.addEventListener("submit", onSend);
    document.getElementById("new-conv").addEventListener("click", () => sidebar.classList.toggle("open"));
    backBtn.addEventListener("click", () => sidebar.classList.add("open"));
}

async function loadConversations() {
    try {
        const res = await fetch(api.conversations(state.me));
        if (!res.ok) throw new Error("Failed to load conversations");
        const data = await res.json();
        state.convs = data;
        renderConversations();
    } catch (err) {
        console.error(err);
        conversationsEl.innerHTML = `<li class="error">Could not load conversations</li>`;
    }
}

function renderConversations() {
    conversationsEl.innerHTML = "";
    for (const c of state.convs) {
        const li = document.createElement("li");
        li.className = c.id === state.active ? "active" : "";
        li.tabIndex = 0;
        li.innerHTML = `
            <img class="avatar" src="https://via.placeholder.com/40" alt="avatar" />
            <div class="conv-meta">
                <div class="name">${escapeHtml(c.title || "Direct")}</div>
                <div class="last"></div>
            </div>
        `;
        li.addEventListener("click", () => openConversation(c.id));
        conversationsEl.appendChild(li);
    }
}

async function openConversation(id) {
    const reqId = ++state._messagesReqId;

    if (messagesFetchController) {
        try { messagesFetchController.abort(); } catch (_) {}
    }
    messagesFetchController = new AbortController();
    const signal = messagesFetchController.signal;

    state.active = id;
    renderConversations();
    chatNameEl.textContent = "Loading...";
    sidebar.classList.remove("open");

    messagesEl.innerHTML = `<div class="loading">Loading messages...</div>`;

    try {
        const res = await fetch(api.messages(id), { signal });
        if (!res.ok) throw new Error("Failed to load messages");
        const data = await res.json();

        if (reqId !== state._messagesReqId || state.active !== id) {
            return;
        }

        state.messages[id] = data;
        const conv = state.convs.find((x) => x.id === id);
        chatNameEl.textContent = conv?.title || "Conversation";
        renderMessages(id, { scrollToBottom: true});

        // connecting the websocket for this active conversation
        if (wsConnectDebounceTimer) clearTimeout(wsConnectDebounceTimer);
        wsConnectDebounceTimer = setTimeout(() => {
            wsConnectDebounceTimer = null;
            wsConnect(id);
        }, WS_CONNECT_DEBOUNCE_MS);
    } catch (err) {
        if (err.name === "AbortError") {
            return;
        }
        console.error(err);
        if (state.active === id) {
            messagesEl.innerHTML = `<div class="error">Could not load messages</div>`;
            chatNameEl.textContent = "Conversation";
        }
    } finally {
        if (messagesFetchController && messagesFetchController.signal === signal) {
            messagesFetchController = null;
        }
    }
}

function renderMessages(convId, opts = {}) {
    messagesEl.innerHTML = "";
    const list = state.messages[convId] || [];
    let lastDate = null;

    for (const m of list) {
        const d = new Date(m.created_at);
        const dateKey = d.toISOString().slice(0, 10);
        if (dateKey !== lastDate) {
            const dateDiv = document.createElement("div");
            dateDiv.className = "date-sep";
            dateDiv.textContent = formatDate(d);
            messagesEl.appendChild(dateDiv);
            lastDate = dateKey;
        }

        const div = document.createElement("div");
        const isMe = String(m.author_id) === String(state.me);
        div.className = "msg " + (isMe ? "me" : "them");

        const authorLine = !isMe && m.author_name ? `<div class="author">${escapeHtml(m.author_name)}</div>` : "";
        div.innerHTML = `${authorLine}<div class="text">${escapeHtml(m.body || "")}</div><span class="time">${formatTime(m.created_at)}</span>`;
        div.setAttribute("data-date", dateKey);

        messagesEl.appendChild(div);
    }
    
    if (opts.scrollToBottom !== false) {
        messagesEl.scrollTop = messagesEl.scrollHeight;
    }
}

async function onSend(e) {
    e.preventDefault();
    const text = inputMsg.value.trim();
    if (!text || !state.active) return;

    const tempId = "local-" + Date.now() + "-" + Math.floor(Math.random() * 1000);
    const tempMsg = {
        id: tempId,
        conversation_id: state.active,
        author_id: state.me,
        body: text,
        created_at: new Date().toISOString(),
        _local: true,
    };

    state.messages[state.active] = state.messages[state.active] || [];
    state.messages[state.active].push(tempMsg);
    renderMessages(state.active, { scrollToBottom: true });

    inputMsg.value = "";

    try {
        const res = await fetch("/api/messages", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                conversation_id: state.active,
                author_id: state.me,
                body: text,
            }),
        });

        if (!res.ok) {
            state.messages[state.active] = (state.messages[state.active] || []).filter(m => m.id !== tempId);
            renderMessages(state.active); 
            const body = await res.text().catch(() => "");
            console.error("send message failed", res.status, body);
            alert("Failed to send message");
            return;
        }

    } catch (err) {
        state.messages[state.active] = (state.messages[state.active] || []).filter(m => m.id !== tempId);
        renderMessages(state.active);
        console.error("send message error", err);
        alert("Failed to send message (network)");
    }
}

// Updating connection status in the UI
function updateConnectionStatus(text, cls) {
    if (!chatSubEl) return;
    chatSubEl.textContent = text || "";
    chatSubEl.className = cls ? `sub ${cls}` : "sub";
}

// Clearing any pending reconnect timer
function clearReconnectTimer() {
    if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
    }
}

// Scheduling a reconnect
function scheduleReconnect() {
    if (manualClose) return;

    if (RECONNECT_MAX_ATTEMPTS && reconnectAttempts >= RECONNECT_MAX_ATTEMPTS) {
        updateConnectionStatus("Disconnected", "disconnected");
        return;
    }

    const base = RECONNECT_BASE_MS * Math.pow(2, reconnectAttempts);
    const jitter = Math.floor(Math.random() * 300);
    let delay = Math.min(base + jitter, RECONNECT_MAX_MS);
    reconnectAttempts++;

    updateConnectionStatus(`Reconnecting in ${Math.round(delay/1000)}s...`, "reconnecting");

    clearReconnectTimer();

    reconnectTimer = setTimeout(() => {
        reconnectTimer = null;
        if (state.active) {
            wsConnect(state.active);
        } else {
            updateConnectionStatus("Disconnected", "disconnected");
        }
    }, delay);
}

async function wsConnect(convId) {
    manualClose = false;
    closeWs(false);

    clearReconnectTimer();

    updateConnectionStatus("Checking membership...", "checking");

    try {
        const checkUrl = `/api/ws_check?conversation_id=${encodeURIComponent(convId)}&user_id=${encodeURIComponent(state.me)}`;
        const res = await fetch(checkUrl, { method: "GET" });
        if (res.status == 403) {
            updateConnectionStatus("Forbidden (no access)", "forbidden");
            return;
        }
        if (!res.ok) {
            updateConnectionStatus("Disconnected", "disconnected");
            scheduleReconnect();
            return;
        }
    } catch (err) {
        console.error("[WS] membership preflight error", err);
        updateConnectionStatus("Disconnected", "disconnected");
        scheduleReconnect();
        return;
    }

    const proto = location.protocol ==="https:" ? "wss" : "ws";
    const tokenPart = WS_TOKEN ? `&token=${encodeURIComponent(WS_TOKEN)}` : "";
    const url = `${proto}://${location.host}/ws?conversation_id=${encodeURIComponent(convId)}&user_id=${encodeURIComponent(state.me)}${tokenPart}`;
    console.debug("[WS] connecting to", url);
    updateConnectionStatus("Connecting...", "connecting");

    try {
        wsConn = new WebSocket(url);
    } catch (err) {
        console.error("[WS] new WebSocket constructor failed", err);
        wsConn = null;
        scheduleReconnect();
        return;
    }
    
    const conn = wsConn;

    conn.addEventListener("open", () => { 
        if (wsConn !== conn) return;   
        console.debug("[WS] open", convId);
        reconnectAttempts = 0;
        clearReconnectTimer();
        updateConnectionStatus("Connected", "connected");
    });

    conn.addEventListener("message", (ev) => {
        if (wsConn !== conn) return;
        try {
            const msg = JSON.parse(ev.data);
            console.debug("[WS] message received for conv", msg.conversation_id, "id", msg.id);
            state.messages[msg.conversation_id] = state.messages[msg.conversation_id] || [];

            const msgs = state.messages[msg.conversation_id];
            const tempIndex = msgs.findIndex(m => {
                return m.id && String(m.id).startsWith("local-") &&
                    m.author_id === msg.author_id &&
                    m.body === msg.body;
            });
            if (tempIndex !== -1) {
                msgs.splice(tempIndex, 1);
            }

            // deduping by id
            const exists = msgs.some((m) => m.id === msg.id);
            if (!exists) {
                msgs.push(msg);
                if (state.active === msg.conversation_id) {
                    renderMessages(state.active, { scrollToBottom: true });
                }
            }
        } catch (err) {
            console.error("[WS] message parse error", err);
        }
    });

    conn.addEventListener("close", (ev) => {
        if (wsConn === conn) {
            wsConn = null;
        }
        console.debug("[WS] close", ev && ev.code, ev && ev.reason);
        if (manualClose) {
            updateConnectionStatus("Disconnected", "disconnected");
            clearReconnectTimer();
            return;
        }
        scheduleReconnect();
    });

    conn.addEventListener("error", (e) => {
        if (wsConn !== conn) return;
        console.debug("[WS] error", e);
    });
}

function closeWs(isManual = true) {
    if (wsConn) {
        try { wsConn.close(); } catch (_) {}
        wsConn = null;
    }
    if (isManual) {
        manualClose = true;
        clearReconnectTimer();
        updateConnectionStatus("Disconnected", "disconnected");
    }
}

function formatTime(ts) {
    const d = new Date(ts);
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function formatDate(d) {
    return d.toLocaleDateString([], { day: "2-digit", month: "short", year: "numeric"});
}

function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

document.addEventListener("DOMContentLoaded", init);