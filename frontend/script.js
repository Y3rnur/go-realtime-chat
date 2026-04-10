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

function getStoredToken() { return ""; }
function saveStoredToken(tok) { /* no-op for cookie-only auth*/ }

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
    document.getElementById("new-conv").addEventListener("click", (e) => { e.preventDefault(); openNewConversationModal(); });
    wireNewConversationModal();
    wireConversationInfoModal();
    backBtn.addEventListener("click", () => sidebar.classList.add("open"));
}

// Conversation info modal logic
function wireConversationInfoModal() {
    const infoModal = document.getElementById('infoModal');
    const infoModalBackdrop = document.getElementById('infoModalBackdrop');
    const infoModalClose = document.getElementById('infoModalClose');
    const infoTitle = document.getElementById('infoTitle');
    const infoSubtitle = document.getElementById('infoSubtitle');
    const infoAvatar = document.getElementById('infoAvatar');
    const infoDescription = document.getElementById('infoDescription');
    const infoActions = document.getElementById('infoActions');
    const participantsList = document.getElementById('participantsList');
    const participantsMore = document.getElementById('participantsMore');

    let infoOpenConv = null;
    let participantsOffset = 0;
    const PARTICIPANTS_PAGE = 25;

    function openConversationInfo(convId) {
        if (!convId) return;
        infoOpenConv = convId;
        participantsOffset = 0;
        participantsList.innerHTML = "";
        infoModal.classList.remove('hidden');
        infoModal.setAttribute('aria-hidden', 'false');
        fetchConversationInfo(convId);
        fetchParticipants(convId, PARTICIPANTS_PAGE, 0);
    }

    function closeConversationInfo() {
        infoOpenConv = null;
        participantsOffset = 0;
        participantsList.innerHTML = "";
        infoModal.classList.add('hidden');
        infoModal.setAttribute('aria-hidden', 'true');
    }

    infoModalClose.addEventListener('click', closeConversationInfo);
    infoModalBackdrop.addEventListener('click', closeConversationInfo);
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape') closeConversationInfo(); });

    async function fetchConversationInfo(convId) {
        try {
            const res = await fetch(`/api/conversations/${convId}/info`, { credentials: 'same-origin' });
            if (!res.ok) throw new Error('failed');
            const info = await res.json();
            renderConversationInfo(info);
        } catch (err) {
            showToast('Failed to load conversation info', 'error');
        }
    }

    function renderConversationInfo(info) {
        infoTitle.textContent = info.title || (info.is_group ? 'Group' : 'Conversation');
        infoSubtitle.textContent = info.is_group ? `${info.participant_count} participants` : `Direct chat`;
        infoAvatar.src = info.avatar_url || '/static/default-avatar.png';
        infoDescription.textContent = info.description || '';

        infoActions.innerHTML = '';
        if (info.is_group) {
            const editBtn = document.createElement('button');
            editBtn.textContent = 'Edit title/description';
            editBtn.className = 'conversation-info-edit-btn'
            editBtn.addEventListener('click', async () => {
                const newTitle = prompt('New title', info.title || '');
                const newDesc = prompt('New description', info.description || '');
                if (newTitle === null && newDesc === null) return;
                try {
                    const res = await fetch(`/api/conversations/${info.id}`, {
                        method: 'PATCH',
                        credentials: 'same-origin',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ title: newTitle, description: newDesc }),
                    });
                    if (!res.ok) { showToast('Failed to update', 'error'); return; }
                    const updated = await res.json();
                    renderConversationInfo(updated);
                    showToast('Updated conversation', 'success');
                } catch (err) {
                    showToast('Network error', 'error');
                }
            });
            infoActions.appendChild(editBtn);
        } else {
            const msgBtn = document.createElement('button');
            msgBtn.textContent = 'Message';
            msgBtn.className = 'conversation-info-direct-message-btn';
            msgBtn.addEventListener('click', () => {
                closeConversationInfo();
                openConversation(info.id);
            })
            infoActions.appendChild(msgBtn);
        }
    }

    async function fetchParticipants(convId, limit = 25, offset = 0) {
        try {
            const res = await fetch(`/api/conversations/${convId}/participants?limit=${limit}&offset=${offset}`, { credentials: 'same-origin'} );
            if (!res.ok) throw new Error('failed');
            const list = await res.json();
            renderParticipants(list, offset === 0);
            participantsOffset += list.length;
            if (list.length === limit) {
                participantsMore.innerHTML = '';
                const moreBtn = document.createElement('button');
                moreBtn.textContent = 'Load more';
                moreBtn.addEventListener('click', () => { fetchParticipants(convId, limit, participantsOffset); });
                participantsMore.appendChild(moreBtn);
            } else {
                participantsMore.innerHTML = '';
            }
        } catch (err) {
            showToast('Failed to load participants', 'error');
        }
    }

    function renderParticipants(list, replace = false) {
        if (replace) participantsList.innerHTML = '';
        for (const p of list) {
            const row = document.createElement('div');
            row.className = 'participant-row';
            row.innerHTML = `
                <img class="participant-avatar" src="${p.avatar_url || '/static/defaul-avatar.png'}" alt="">
                <div class="participant-info">
                    <div class="participant-name">${escapeHtml(p.display_name || 'Unknown')}</div>
                    <div class="participant-meta">${escapeHtml(p.role || 'member')} • ${new Date(p.joined_at).toLocaleString()}</div>
                </div>
                <div class="participant-actions"></div>
            `;
            const actions = row.querySelector('.participant-actions');
            const me = state.me;
            if (String(p.user_id) !== String(me)) {
                // blocking feature
                const blockBtn = document.createElement('button');
                blockBtn.textContent = 'Block';
                blockBtn.className = 'conversation-info-block-btn';
                blockBtn.addEventListener('click', async () => {
                    if (!confirm(`Block ${p.display_name || 'user'}?`)) return;
                    try {
                        const res = await fetch(`/api/users/${p.user_id}/block`, {method: 'POST', credentials: 'same-origin' });
                        const body = await res.text().catch(()=>"");
                        if (res.ok) { 
                            showToast('User blocked', 'success'); 
                        } else { 
                            console.debug("POST /api/users/:id/block failed", res.status, body);
                            showToast('Failed to block', 'error'); 
                        }
                    } catch (err) { 
                        showToast('Network error', 'error'); 
                    }
                });
                actions.appendChild(blockBtn);
            } else {
                // leaving from the group (self remove)
                if (infoOpenConv) {
                    const leaveBtn = document.createElement('button');
                    leaveBtn.textContent = 'Leave';
                    leaveBtn.className = 'conversation-info-leave-btn';
                    leaveBtn.addEventListener('click', async () => {
                        if (!confirm('Leave conversation?')) return;
                        try {
                            const res = await fetch(`/api/conversations/${infoOpenConv}/participants/${me}`, { method: 'DELETE', credentials: 'same-origin' });
                            if (res.status === 204) { showToast('Left conversation', 'success'); closeConversationInfo(); } else showToast('Failed to leave', 'error');
                        } catch (_) { showToast('Network error', 'error'); }
                    });
                    actions.appendChild(leaveBtn);
                }
            }
            participantsList.appendChild(row);
        }
    }

    // expose handler for WS messages so existing WS listener can call it
    window.handleWsInfoEvents = function(msg) {
        if (!infoOpenConv) return;
        if (msg.type === 'conversation_updated' && msg.conversation && String(msg.conversation.id) === String(infoOpenConv)) {
            renderConversationInfo(msg.conversation);
        }
        if ((msg.type === 'participant_added' || msg.type === 'participant_removed') && String(msg.conversation_id) === String(infoOpenConv)) {
            participantsOffset = 0;
            fetchParticipants(infoOpenConv, PARTICIPANTS_PAGE, 0);
        }
    };

    // attaching header click to open modal for current conversation
    const chatHeaderTitle = document.querySelector('.chat-title');
    if (chatHeaderTitle) {
        chatHeaderTitle.addEventListener('click', () => {
            if (state.active) openConversationInfo(state.active);
        });
    }
}

// New conversation modal logic
let ncSelected = [];    // array of { id, display_name }
function openNewConversationModal() {
    ncSelected = [];
    document.getElementById("nc-title").value = "";
    document.getElementById("nc-is-group").checked = false;
    document.getElementById("nc-search").value = "";
    document.getElementById("nc-results").innerHTML = "";
    document.getElementById("nc-selected").innerHTML = "";
    document.getElementById("new-conv-modal").style.display = "block";
    document.getElementById("nc-search").focus();
}

function closeNewConversationModal() {
    document.getElementById("new-conv-modal").style.display = "none";
}

function renderSelectedParticipants() {
    const container = document.getElementById("nc-selected");
    container.innerHTML = "";
    for (const p of ncSelected) {
        const pill = document.createElement("div");
        pill.style.cssText = "background:#1f2937;color#fff;padding:6px 8px;border-radius:999px;display:flex;align-items:center;gap:8px";
        pill.innerHTML = `<span>${escapeHtml(p.display_name || p.id)}</span><button data-id="${p.id}" style="background:transparent;border:none;color:#fff;cursor:pointer">X</button>`;
        pill.querySelector("button").addEventListener("click", (e) => {
            const id = e.currentTarget.getAttribute("data-id");
            ncSelected = ncSelected.filter(x => String(x.id) !== String(id));
            renderSelectedParticipants();
        });
        container.appendChild(pill);
    }
}

let ncSearchTimer = null;

// wire New Conversation modal buttons (also called from init)
function wireNewConversationModal() {
    const ncCancel = document.getElementById("nc-cancel");
    const ncCreate = document.getElementById("nc-create");
    const ncSearch = document.getElementById("nc-search");
    const ncResults = document.getElementById("nc-results");

    ncCancel.addEventListener("click", () => closeNewConversationModal());

    ncSearch.addEventListener("input", () => {
        if (ncSearchTimer) clearTimeout(ncSearchTimer);
        ncSearchTimer = setTimeout(async () => {
            const q = ncSearch.value.trim();
            ncResults.innerHTML = "";
            if (!q) return;
            try {
                console.debug("[NC] search users q=", q);
                const res = await fetch(`/api/users?q=${encodeURIComponent(q)}`, { credentials: "same-origin"});
                console.debug("[NC] /api/users status=", res.status);
                if (!res.ok) {
                    // brief message for non-200
                    ncResults.innerHTML = `<li style="padding:6px;color:#faa">Search error (${res.status})</li>`;
                    return;
                }
                const users = await res.json();
                ncResults.innerHTML = "";
                if (!Array.isArray(users) || users.length === 0) {
                    ncResults.innerHTML = `<li style="padding:6px;color:#999">No users found</li>`;
                    return;
                }
                for (const u of users) {
                    const li = document.createElement("li");
                    li.style.cssText = "padding:6px;border-bottom:1px solid rgba(255,255,255,0.04);cursor:pointer";
                    li.textContent = u.display_name || u.email || String(u.id);
                    li.addEventListener("click", () => {
                        // add if not already selected
                        if (!ncSelected.some(x => String(x.id) === String(u.id))) {
                            ncSelected.push(u);
                            renderSelectedParticipants();
                        }
                    });
                    ncResults.appendChild(li);
                }
            } catch (err) {
                console.error("user search failed", err);
                ncResults.innerHTML = `<li style="padding:6px;color:#faa">Network error</li>`;
            }
        }, 250);
    });

    ncCreate.addEventListener("click", async () => {
        const title = document.getElementById("nc-title").value.trim() || null;
        const isGroup = document.getElementById("nc-is-group").checked;
        const participants = ncSelected.map(p => String(p.id));
        try {
            const res = await fetch("/api/conversations", {
                method: "POST",
                credentials: "same-origin",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ title, is_group: isGroup, participants})
            });
            if (!res.ok) {
                const body = await res.text().catch(()=>"");
                showToast("Failed to create conversation: " + (body || res.status), "error", 4000);
                return;
            }
            const conv = await res.json();
            // optionally refresh conversation list. We will add new conv to state
            state.convs = state.convs || [];
            state.convs.unshift(conv);
            renderConversations();
            showToast("Conversation created", "success", 2500);
            closeNewConversationModal();
        } catch (err) {
            console.error("create conversation failed", err);
            showToast("Network error creating conversation", "error", 4000);
        }
    });
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
        const display = c.is_group ? (c.title || "Group") : (c.display_name || (c.title || "Direct"));
        const avatarSrc = c.avatar_url || c.display_avatar || c.partner_avatar || '/static/default-avatar.png';
        const li = document.createElement("li");
        li.className = c.id === state.active ? "active" : "";
        li.tabIndex = 0;
        li.innerHTML = `
            <img class="avatar" src="${escapeHtml(avatarSrc)}" alt="avatar" />
            <div class="conv-meta">
                <div class="name">${escapeHtml(display)}</div>
                <div class="last"></div>
            </div>
        `;
        li.addEventListener("click", () => openConversation(c.id));
        conversationsEl.appendChild(li);

        const imgEl = li.querySelector('img.avatar');
        if (imgEl) {
            imgEl.addEventListener('error', () => { imgEl.src = '/static/default-avatar.png'; });
        }
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

        // updating chat header avatar
        const chatAvatarEl = document.getElementById('chat-avatar');
        if (chatAvatarEl) {
        let headerAvatar = conv?.avatar_url || conv?.avatar || conv?.display_avatar || conv?.partner_avatar || null;

        if (!headerAvatar && !conv?.is_group) {
            // try to fetch participants and pick the other participant's avatar
            try {
            const res = await fetch(`/api/conversations/${id}/participants?limit=5&offset=0`, { credentials: 'same-origin' });
            if (res.ok) {
                const parts = await res.json();
                const other = parts.find(p => String(p.user_id) !== String(state.me));
                if (other && other.avatar_url) headerAvatar = other.avatar_url;
            } else {
                console.debug("participants fetch failed:", res.status);
            }
            } catch (err) {
            console.debug("participants fetch error for avatar:", err);
            }
        }

        headerAvatar = headerAvatar || '/static/default-avatar.png';
        chatAvatarEl.src = headerAvatar;
        chatAvatarEl.alt = conv?.title || conv?.display_name || 'Conversation';
        chatAvatarEl.onerror = () => { chatAvatarEl.src = '/static/default-avatar.png'; };
        }
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

function attachMsgHoverMenus(convId) {
    const container = messagesEl;
    if (!container) return;
    // close any open menu when clicking outside - add listener only once
    if (!window._msgMenuDocListenerAttached) {
        document.addEventListener("click", (e) => {
            const open = container.querySelectorAll(".msg-menu.open");
            open.forEach(m => {
                if (!m.contains(e.target) && !e.target.closest(".msg-hover-trigger")) m.classList.remove("open");
            });
        });
        window._msgMenuDocListenerAttached = true;
    }

    for (const el of container.querySelectorAll(".msg")) {
        const msgId = el.getAttribute("data-msg-id");
        if (!msgId) continue;
        // avoid re-adding
        if (el.querySelector(".msg-hover-trigger")) continue;

        // find corresponding message in state to check author and deleted state
        const list = state.messages[convId] || [];
        const msg = list.find(x => String(x.id) === String(msgId));
        if (!msg) continue;
        const isMeMsg = String(msg.author_id) === String(state.me);
        const isDeleted = !!msg.deleted || !!msg.is_deleted;
        // only show menu for own non-deleted messages
        if (!isMeMsg || isDeleted) continue;

        // create trigger
        const trigger = document.createElement("button");
        trigger.className = "msg-hover-trigger";
        trigger.type = "button";
        el.appendChild(trigger);

        // create menu
        const menu = document.createElement("div");
        menu.className = "msg-menu";
        const editBtn = document.createElement("button");
        editBtn.className = "edit";
        editBtn.textContent = "Edit";
        const delBtn = document.createElement("button");
        delBtn.className = "delete";
        delBtn.textContent = "Delete";
        menu.appendChild(editBtn);
        menu.appendChild(delBtn);
        el.appendChild(menu);

        // wire trigger toggle
        trigger.addEventListener("click", (ev) => {
            ev.stopPropagation();
            // close other menus
            container.querySelectorAll(".msg-menu.open").forEach(m => { if (m !== menu) m.classList.remove("open"); });
            menu.classList.toggle("open");
        });

        // wire buttons to existing handlers (assumes functions exist)
        editBtn.addEventListener("click", (ev) => {
            ev.stopPropagation();
            menu.classList.remove("open");
            openInlineEditor(convId, msgId);
        });
        delBtn.addEventListener("click", (ev) => {
            ev.stopPropagation();
            menu.classList.remove("open");
            confirmDeleteMessage(msgId);
        });
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
        const isDeleted = !!m.deleted || !!m.is_deleted;
        const isEdited = !!m.edited_at;

        div.className = "msg " + (isMe ? "me" : "them");
        div.setAttribute("data-msg-id", m.id);

        const authorLine = !isMe && m.author_name ? `<div class="author">${escapeHtml(m.author_name)}</div>` : "";
        const bodyHtml = isDeleted
            ? `<div class="text deleted">Message deleted</div>`
            : `<div class="text">${escapeHtml(m.body || "")}</div>` + (isEdited ? `<div class="edited">(edited)</div>` : '');
        
        // actions will be provided by the hover menu (meatball trigger).
        div.innerHTML = `${authorLine}${bodyHtml}<span class="time">${formatTime(m.created_at)}</span>`;
        messagesEl.appendChild(div);
    }
    
    attachMsgHoverMenus(convId);
    if (opts.scrollToBottom !== false) {
        messagesEl.scrollTop = messagesEl.scrollHeight;
    }
}

function openInlineEditor(convId, msgId) {
    const list = state.messages[convId] || [];
    const msg = list.find(x => String(x.id) === String(msgId));
    if (!msg) return;
    const curText = msg.body || "";
    const msgEl = messagesEl.querySelector(`[data-msg-id="${msgId}"]`);
    if (!msgEl) return;
    // creating inline editor
    msgEl.innerHTML = `<div class="inline-edit">
        <textarea class="edit-input" rows="3">${escapeHtml(curText)}</textarea>
        <div style="margin-top:6px">
            <button class="save-edit">Save</button>
            <button class="cancel-edit">Cancel</button>
        </div>
    </div>`;
    const saveBtn = msgEl.querySelector(".save-edit");
    const cancelBtn = msgEl.querySelector(".cancel-edit");
    const input = msgEl.querySelector(".edit-input");
    saveBtn.addEventListener("click", async () => {
        const newBody = input.value.trim();
        if (!newBody) { showToast("Message cannot be empty", "error"); return; }
        await saveEditedMessage(convId, msgId, newBody);
    });
    cancelBtn.addEventListener("click", () => {
        renderMessages(convId);
    });
}

async function saveEditedMessage(convId, msgId, newBody) {
    const list = state.messages[convId] || [];
    const idx = list.findIndex(m => String(m.id) === String(msgId));
    if (idx == -1) return;
    const old = { ...list[idx] };

    // optimistic update
    list[idx].body = newBody;
    list[idx].edited_at = new Date().toISOString();
    renderMessages(convId);
    
    try {
        const res = await fetch("/api/messages", {
            method: "PATCH",
            credentials: "same-origin",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ id: msgId, body: newBody }),
        });
        if (!res.ok) {
            const txt = await res.text().catch(()=>"");
            // restore on failure
            list[idx] = old;
            renderMessages(convId);
            showToast("Failed to edit message: " + (txt || res.status), "error");
            return;
        }
        const updated = await res.json();
        // replacing with server-canonical message and re-render
        list[idx] = updated;
        renderMessages(convId);
        showToast("Message edited successfully!", "success");
    } catch (err) {
        list[idx] = old;
        renderMessages(convId);
        showToast("Network error editing message", "error");
    }
}

async function confirmDeleteMessage(msgId) {
    if (!confirm("Delete this message? This cannot be undone.")) return;
    try {
        const res = await fetch("/api/messages", {
            method: "DELETE",
            credentials: "same-origin",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ id: msgId }),
        });
        if (res.status === 204) {
            // apply locally
            for (const convId in state.messages) {
                const list = state.messages[convId];
                const idx = list.findIndex(m => String(m.id) === String(msgId));
                if (idx >= 0) {
                    list[idx].body = null;
                    list[idx].deleted = true;
                    renderMessages(convId);
                    break;
                }
            }
            showToast("Message deleted successfully!", "success");
            return;
        }
        const txt = await res.text().catch(()=>"");
        showToast("Failed to delete message: " + (txt || res.status), "error");
    } catch (err) {
        showToast("Network error deleting message", "error");
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
                    case "conversation_created": {
                        if (msg.conversation) {
                            const conv = msg.conversation;
                            state.convs = state.convs || [];
                            // avoiding duplicates
                            if (!state.convs.find(c => c.id === conv.id)) {
                                state.convs.unshift(conv);
                                renderConversations();
                                showToast("New conversation created", "info", 3000);
                            }
                        }
                        break;
                    }
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
                    case "message_updated": {
                        const m = msg.message;
                        // find and replace by id
                        for (const convId in state.messages) {
                            const list = state.messages[convId];
                            const idx = list.findIndex(x => String(x.id) === String(m.id));
                            if (idx >= 0) {
                                list[idx] = { ...list[idx], ...m };
                                renderMessages(convId);
                                return;
                            }
                        }
                        // not found: append to conversation list
                        if (m && m.conversation_id) {
                            state.messages[m.conversation_id] = state.messages[m.conversation_id] || [];
                            state.messages[m.conversation_id].push(m);
                            if (state.active === m.conversation_id) renderMessages(m.conversation_id);
                        }
                        return;
                    }
                    case "message_deleted": {
                        const mid = msg.id;
                        // update wherever message exists
                        for (const convId in state.messages) {
                            const list = state.messages[convId];
                            const idx = list.findIndex(x => String(x.id) === String(mid));
                            if (idx >= 0) {
                                list[idx].body = null;
                                list[idx].deleted = true;
                                renderMessages(convId);
                                return;
                            }
                        }
                        return;
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