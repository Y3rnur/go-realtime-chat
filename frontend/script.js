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
        renderMessages(id, { scrollToBottom: true});
    } catch (err) {
        console.error(err);
        messagesEl.innerHTML = `<div class="error">Could not load messages</div>`;
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
            const body = await res.text().catch(() => "");
            console.error("send message failed", res.status, body);
            alert("Failed to send message");
        }

        const saved = await res.json();
        state.messages[state.active] = state.messages[state.active] || [];
        state.messages[state.active].push(saved);
        renderMessages(state.active, { scrollToBottom: true});
    } catch (err) {
        console.error("send message error", err);
        alert("Failed to send message (network)");
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