const DEMO_USER_ID = "3e62ced9-275f-4e96-82f1-d505730df6af";

const api = {
    conversations: (userId) => `/api/conversations?user_id=${encodeURIComponent(userId)}`,
    messages: (conversationId) => `/api/messages?conversation_id=${encodeURIComponent(conversationId)}`,
};

const conversationsEl = document.getElementById("conversations");
const messagesEl = document.getElementById("messages");
const chatNameEl = document.getElementById("chat-name");
const composer = document.getElementById("composer");
const inputMsg = document.getElementById("input-msg");
const sidebar = document.getElementById("sidebar");
const backBtn = document.getElementById("back-btn");

let state = {
    me: DEMO_USER_ID,
    convs: [],
    active: null,
    messages: {},
};

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
    state.active = id;
    renderConversations();
    chatNameEl.textContent = "Loading...";
    sidebar.classList.remove("open");

    try {
        const res = await fetch(api.messages(id));
        if (!res.ok) throw new Error("Failed to load messages");
        const data = await res.json();
        state.messages[id] = data;
        const conv = state.convs.find((x) => x.id === id);
        chatNameEl.textContent = conv?.title || "Conversation";
        renderMessages(id);
    } catch (err) {
        console.error(err);
        messagesEl.innerHTML = `<div class="error">Could not load messages</div>`;
    }
}

function renderMessages(convId) {
    messagesEl.innerHTML = "";
    const list = state.messages[convId] || [];
    for (const m of list.slice().reverse()) {
        const div = document.createElement("div");
        const isMe = String(m.author_id) === String(state.me);
        div.className = "msg " + (isMe ? "me" : "them");
        div.innerHTML = `<div class="text">${escapeHtml(m.body || "")}</div><span class="time">${formatTime(m.created_at)}</span>`;
        messagesEl.appendChild(div);
    }
    messagesEl.scrollTop = messagesEl.scrollHeight;
}

function onSend(e) {
    e.preventDefault();
    const text = inputMsg.value.trim();
    if (!text || !state.active) return;
    // Below local logic will be replaced later, when the POST will be added (so data message will be sent to backend + db)
    const msg = {
        id: "local-" + Date.now(),
        conversation_id: state.active,
        author_id: state.me,
        body: text,
        created_at: new Date().toISOString(),
    };
    state.messages[state.active] = state.messages[state.active] || [];
    state.messages[state.active].push(msg);
    renderMessages(state.active);
    inputMsg.value = "";
    // TODO: send to backend (POST)
}

function formatTime(ts) {
    const d = new Date(ts);
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

document.addEventListener("DOMContentLoaded", init);