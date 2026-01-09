const DEMO_USER_ID = "3e62ced9-275f-4e96-82f1-d505730df6af";

const WS_TOKEN = "";

const api = {
    // conversations is now an auth-protected endpoint; no user_id param required.
    conversations: () => `/api/conversations`,
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
    users: {},
};

// Auth UI DOM refs
let authLoginBtn, authLogoutBtn, authEmail, authPw, authForm, authInfo, authName;

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

// For typing debounce states
let typingSentAt = 0;
const TYPING_THROTTLE_MS = 2000;
const TYPING_INDICATOR_MS = 3000;
const typingTimers = {};
let statusFallback = "";
function showStatus(text, cls) {
    if (!chatSubEl) return;
    chatSubEl.textContent = text || "";
    chatSubEl.className = cls ? `sub ${cls}` : "sub";
}

function getDisplayName(userId) {
    if (!userId) return "";
    if (String(userId) === String(state.me)) return "You";
    if (state.users && state.users[userId]) return state.users[userId];
    if (typeof userId === "string" && userId.includes("-")) return userId.split("-")[0];
    return String(userId).slice(0, 8);
}

function showToast(message, type = "info", timeout = 4000) {
    const el = document.createElement("div");
    el.className = `toast toast-${type}`;
    el.textContent = message;
    const bg = type === "error" ? "#e74c3c" : (type === "success" ? "#2ecc71" : (type === "info" ? "#2d9bf0" : "#333"));
    Object.assign(el.style, {
        position: "fixed",
        right: "20px",
        bottom: "20px",
        padding: "8px 12px",
        background: bg,
        color: "#fff",
        borderRadius: "6px",
        boxShadow: "0 2px 10px rgba(0, 0, 0, 0.2)",
        zIndex: 9999,
        transition: "opacity 0.25s ease",
    });
    document.body.appendChild(el);
    setTimeout(() => { el.style.opacity = "0"; setTimeout(() => el.remove(), 250); }, timeout);
}

function handleLoggedOut(message) {
    try { saveStoredToken(""); } catch(_) {}
    state.me = DEMO_USER_ID;
    delete state.users[state.me];
    state.convs = [];
    state.active = null;
    state.messages = {};
    if (authForm) authForm.style.display = "block";
    if (authInfo) authInfo.style.display = "none";
    if (authName) authName.textContent = "";
    if (messagesEl) messagesEl.innerHTML = "";
    if (chatNameEl) chatNameEl.textContent = "Select a conversation";
    if (chatSubEl) chatSubEl.textContent = "-";
    renderConversations();
    closeWs(true);
    if (message) showToast(message, "error", 2000);
}

function init() {
    if (!state.me || state.me === "3e62ced9-275f-4e96-82f1-d505730df6af") {
        console.warn("Set DEMO_USER_ID in frontend/script.js to a real user UUID.");
    }

    // Auth UI wiring
    authLoginBtn = document.getElementById("auth-login");
    authLogoutBtn = document.getElementById("auth-logout");
    authEmail = document.getElementById("auth-email");
    authPw = document.getElementById("auth-pw");
    authForm = document.getElementById("auth-form");
    authInfo = document.getElementById("auth-info");
    authName = document.getElementById("auth-name");

    if (authLoginBtn) {
        authLoginBtn.addEventListener("click", async () => {
            const email = (authEmail.value || "").trim();
            const pw = authPw.value || "";
            if (!email || !pw) return alert("email & password required");
            await login(email, pw);
        });
    }
    if (authLogoutBtn) {
        authLogoutBtn.addEventListener("click", () => {
            logout();
        });
    }

    (async () => {
        const ok = await refreshAccess();
        if (!ok) {
            saveStoredToken("");
            if (authForm && authInfo && authName) {
                authForm.style.display = "block";
                authInfo.style.display = "none";
                authName.textContent = "";
            }
        }
        loadConversations();
    })();

    composer.addEventListener("submit", onSend);
    document.getElementById("new-conv").addEventListener("click", () => sidebar.classList.toggle("open"));
    backBtn.addEventListener("click", () => sidebar.classList.add("open"));
}

// login helper
async function login(email, password) {
    try {
        const res = await fetch("/api/login", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ email, password }),
            credentials: "same-origin"
        });
        if (!res.ok) {
            const body = await res.text().catch(()=>"");
            alert("Login failed: " + (body || res.status));
            return;
        }
        const data = await res.json();
        
        if (data && data.user && data.user.id) {
            state.me = data.user.id;
            if (data.user.display_name) state.users[state.me] = data.user.display_name;
            authForm.style.display = "none";
            authInfo.style.display = "inline-block";
            authName.textContent = data.user.display_name || state.me;

            showToast(`Welcome ${data.user.display_name || "User"}!`, "success", 3000);
            // refreshing conversations and current view
            await loadConversations();
            if (state.active) openConversation(state.active);
        }
    } catch (err) {
        console.error("login error", err);
        alert("Login error");
    }
}

function logout() {
    // telling server to clear cookie, then clear client-side token and state
    (async () => {
        try {
            const res = await fetch("/api/logout", { method: "POST", credentials: "same-origin" });
            if (res.ok) {
                try { const body = await res.json().catch(()=>null); console.debug("logout response", body); } catch(_) {}
            }
        } catch (err) {
            console.warn("logout request failed", err);
        }
        showToast("Logged out successfully!", "success", 2500);
        saveStoredToken("");
        // clearing client-side auth + conversation/message state and UI
        state.me = DEMO_USER_ID;
        delete state.users[state.me];
        state.convs = [];
        state.active = null;
        state.messages = {};
        if (authForm) authForm.style.display = "block";
        if (authInfo) authInfo.style.display = "none";
        if (authName) authName.textContent = "";
        // clear messages UI immediately without needing page reload
        if (messagesEl) messagesEl.innerHTML = "";
        if (chatNameEl) chatNameEl.textContent = "Select a conversation";
        if (chatSubEl) chatSubEl.textContent = "-";
        renderConversations();
        closeWs(true);
    })();
}

// rotating refresh token to obtain a fresh access token
async function refreshAccess() {
    try {
        const res = await fetch("/api/refresh", {
            method: "GET",
            credentials: "same-origin",
        });
        if (!res.ok) {
            handleLoggedOut("Session expired - please log in.");
            return false;
        }
        const data = await res.json().catch(()=>null);

        if (data && data.user && data.user.id) {
            state.me = data.user.id;
            if (data.user.display_name) state.users[state.me] = data.user.display_name;
            if (authForm && authInfo && authName) {
                authForm.style.display = "none";
                authInfo.style.display = "inline-block";
                authName.textContent = data.user.display_name || state.me;
            }
        }
        return true;
    } catch (err) {
        console.debug("refreshAccess error", err);
        handleLoggedOut("Network error while refreshing session.");
        saveStoredToken("");
        return false;
    }
}

async function loadConversations() {
    try {
        const res = await fetch(api.conversations(), {credentials: "same-origin"});
        if (!res.ok) {
            if (res.status === 401) {
                handleLoggedOut("Session expired - please log in.");
                return;
            }
            console.error("loadConversations failed:", res.status, res.statusText);
            state.convs = [];
            renderConversations();
            return;
        }
        const data = await res.json();
        if (!Array.isArray(data)) {
            console.error("loadConversations unexpected payload:", data);
            state.convs = [];
        } else {
            state.convs = data;
        }
        renderConversations();
    } catch (err) {
        console.error(err);
        conversationsEl.innerHTML = `<li class="error">Could not load conversations</li>`;
    }
}

function renderConversations() {
    conversationsEl.innerHTML = "";
    if (!Array.isArray(state.convs) || state.convs.length === 0) {
        conversationsEl.innerHTML = `<li class="empty">No conversations</li>`;
        return;
    }
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
        const res = await fetch(api.messages(id), { signal, credentials: "same-origin" });
        if (!res.ok) {
            if (res.status === 401) {
                handleLoggedOut("Session expired - please log in.");
                return;
            }
            throw new Error("Failed to load messages");
        }
        const data = await res.json();

        if (reqId !== state._messagesReqId || state.active !== id) {
            return;
        }

        state.messages[id] = data;
        const conv = state.convs.find((x) => x.id === id);
        chatNameEl.textContent = conv?.title || "Conversation";
        renderMessages(id, { scrollToBottom: true});

        try {
            if (wsConn && wsConn.readyState === WebSocket.OPEN) {
                const lastMsg = state.messages[id][state.messages[id].length - 1];
                const payload = { type: "read", conversation_id: id};
                if (lastMsg && lastMsg.id) payload.last_read_id = lastMsg.id;
                wsConn.send(JSON.stringify(payload));
            }
        } catch (err) {
            console.debug("[WS] send read failed", err);
        }

        // connecting the websocket for this active conversation (debounced)
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
        if (m.author_id && m.author_name) {
            state.users[m.author_id] = m.author_name;
        }
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
    sendTypingStop();
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
            credentials: "same-origin",
            body: JSON.stringify({
                conversation_id: state.active,
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
        const checkUrl = `/api/ws_check?conversation_id=${encodeURIComponent(convId)}`;
        const res = await fetch(checkUrl, { method: "GET", credentials: "same-origin" });
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
    // prefer cookie auth; stored token included as fallback for dev
    const url = `${proto}://${location.host}/ws?conversation_id=${encodeURIComponent(convId)}`;
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
            if (msg && msg.type) {
                switch (msg.type) {
                    case "typing": {
                        // showing typing indicator in chatSubEl temporarily
                        if (msg.user_id === state.me) break;

                        const who = getDisplayName(msg.user_id);
                        if (msg.stopped) {
                            if (typingTimers[msg.user_id]) {
                                clearTimeout(typingTimers[msg.user_id]);
                                delete typingTimers[msg.user_id];
                            }
                            if (Object.keys(typingTimers).length === 0) {
                                showStatus(statusFallback || "", "");
                            }
                            break;
                        }

                        if (typingTimers[msg.user_id]) {
                            clearTimeout(typingTimers[msg.user_id]);
                        }
                        statusFallback = statusFallback || chatSubEl.textContent || "";
                        showStatus(`${who} is typing...`, "typing");
                        typingTimers[msg.user_id] = setTimeout(() => {
                            delete typingTimers[msg.user_id];
                            if (Object.keys(typingTimers).length === 0) {
                                showStatus(statusFallback || "", "");
                                statusFallback = "";
                            }
                        }, TYPING_INDICATOR_MS);
                        break;
                    }
                    case "presence": {
                        // updating connection status to show presence if for active conversation
                        if (msg.conversation_id === state.active && msg.user_id !== state.me) {
                            showStatus(`${getDisplayName(msg.user_id)} ${msg.status}`, "presence");
                        }
                        break;
                    }
                    case "read": {
                        // for now, no visual mark messages (will add later)
                        if (msg.conversation_id === state.active) {
                            showStatus(`Last read by ${getDisplayName(msg.user_id)}`, "read");
                            setTimeout(() => {
                                // clearing after a moment
                                if (chatSubEl.textContent.startsWith("Last read by")) {
                                    showStatus("", "");
                                }
                            }, 3000);
                        }
                        break;
                    }
                    default:
                        console.debug("[WS] event type not handled", msg.type);
                }
                return;
            }
            console.debug("[WS] message received for conv", msg.conversation_id, "id", msg.id);
            const chatMsg = msg;
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
            
            if (msg.author_id && msg.author_name) {
                state.users[msg.author_id] = msg.author_name;
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

function sendTyping() {
    if (!wsConn || wsConn.readyState !== WebSocket.OPEN || !state.active) return;
    const now = Date.now();
    if (now - typingSentAt < TYPING_THROTTLE_MS) return;
    typingSentAt = now;
    try {
        wsConn.send(JSON.stringify({
            type:               "typing",
            conversation_id:    state.active,
            user_id:            state.me
        }));
    } catch (err) {
        console.debug("[WS] send typing failed", err);
    }
}

function sendTypingStop() {
    if (!wsConn || wsConn.readyState !== WebSocket.OPEN || !state.active) return;
    try {
        wsConn.send(JSON.stringify({
            type:               "typing",
            conversation_id:    state.active,
            user_id:            state.me,
            stopped:            true
        }));
    } catch (err) {
        console.debug("[WS] send typing stop failed", err);
    }
}

inputMsg.addEventListener("input", () => {
    sendTyping();
});
inputMsg.addEventListener("blur", () => {
    sendTypingStop();
});

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