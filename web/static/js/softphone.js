import {
  UserAgent,
  Registerer,
  SessionState,
  Web,
} from "https://cdn.jsdelivr.net/npm/sip.js@0.21.2/+esm";

const el = {
  root: () => document.getElementById("softphone"),
  status: () => document.getElementById("sip-status"),
  dot: () => document.getElementById("sip-dot"),
  ext: () => document.getElementById("sip-extension"),
  pass: () => document.getElementById("sip-password"),
  connect: () => document.getElementById("sip-connect"),
  disconnect: () => document.getElementById("sip-disconnect"),
  auto: () => document.getElementById("sip-auto"),
  call: () => document.getElementById("sip-call"),
  caller: () => document.getElementById("sip-caller"),
  answer: () => document.getElementById("sip-answer"),
  reject: () => document.getElementById("sip-reject"),
  hangup: () => document.getElementById("sip-hangup"),
  audio: () => document.getElementById("sip-remote-audio"),
};

let cfg = null;
let userAgent = null;
let registerer = null;
let currentSession = null;
let screenPopOpened = false;

function setStatus(text, state) {
  const s = el.status();
  const d = el.dot();
  if (s) s.textContent = text;
  if (d) d.dataset.state = state || "off";
}

function extractPhone(uri) {
  if (!uri) return "";
  let raw = String(uri);
  const m = raw.match(/sip:([^@>;]+)/i);
  if (m) raw = m[1];
  raw = raw.replace(/^"/, "").replace(/"$/, "");
  return raw.trim();
}

async function screenPop(phone, callerName) {
  const body = new URLSearchParams();
  if (phone) body.set("phone", phone);
  if (callerName) body.set("caller_name", callerName);
  const res = await fetch("/api/calls/screen-pop", {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body,
  });
  if (!res.ok) return;
  const data = await res.json();
  if (data.redirect) window.location.href = data.redirect;
}

function setupSession(session) {
  currentSession = session;
  screenPopOpened = false;
  const remote = session.remoteIdentity;
  const phone = extractPhone(remote?.uri?.toString?.() || remote?.friendlyName || "");
  const name = remote?.displayName || "";
  el.caller().textContent = name ? `${name} · ${phone || "без номера"}` : phone || "Входящий звонок";
  el.call().hidden = false;
  el.answer().hidden = false;
  el.reject().hidden = false;
  el.hangup().hidden = true;
  setStatus("Входящий звонок", "ring");

  session.stateChange.addListener((state) => {
    switch (state) {
      case SessionState.Established:
        el.answer().hidden = true;
        el.reject().hidden = true;
        el.hangup().hidden = false;
        setStatus("Разговор", "talk");
        try {
          const pc = session.sessionDescriptionHandler?.peerConnection;
          if (pc && el.audio()) {
            const remoteStream = new MediaStream();
            pc.getReceivers().forEach((r) => {
              if (r.track) remoteStream.addTrack(r.track);
            });
            el.audio().srcObject = remoteStream;
          }
        } catch (_) {}
        if (!screenPopOpened) {
          screenPopOpened = true;
          screenPop(phone, name);
        }
        break;
      case SessionState.Terminated:
        el.call().hidden = true;
        currentSession = null;
        setStatus(registerer ? "На линии" : "SIP выкл.", registerer ? "ok" : "off");
        break;
      default:
        break;
    }
  });
}

async function connect() {
  if (!cfg?.enabled) {
    setStatus("SIP не настроен", "off");
    return;
  }
  const extension = (el.ext().value || "").trim();
  const password = el.pass().value || "";
  if (!extension || !password) {
    setStatus("Укажите добавочный и пароль", "err");
    return;
  }

  await fetch("/api/softphone/credentials", {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      extension,
      auth_username: extension,
      password,
      display_name: cfg.display_name || "",
      auto_register: el.auto().checked ? "1" : "0",
    }),
  });

  await disconnect(true);

  const uri = UserAgent.makeURI(`sip:${extension}@${cfg.sip_domain}`);
  if (!uri) {
    setStatus("Некорректный SIP URI", "err");
    return;
  }

  const iceServers = [];
  (cfg.stun_urls || []).forEach((u) => iceServers.push({ urls: u }));
  (cfg.turn_urls || []).forEach((u) => {
    iceServers.push({
      urls: u,
      username: cfg.turn_username || undefined,
      credential: cfg.turn_password || undefined,
    });
  });

  userAgent = new UserAgent({
    uri,
    transportOptions: { server: cfg.websocket_url },
    authorizationUsername: extension,
    authorizationPassword: password,
    displayName: cfg.display_name || extension,
    sessionDescriptionHandlerFactoryOptions: {
      constraints: { audio: true, video: false },
      peerConnectionConfiguration: { iceServers },
    },
    delegate: {
      onInvite: (invitation) => setupSession(invitation),
    },
  });

  setStatus("Подключение…", "wait");
  await userAgent.start();
  registerer = new Registerer(userAgent);
  await registerer.register();
  setStatus("На линии", "ok");
  el.connect().hidden = true;
  el.disconnect().hidden = false;
}

async function disconnect(silent) {
  try {
    if (currentSession) {
      try { await currentSession.bye?.(); } catch (_) {}
      try { currentSession.reject?.(); } catch (_) {}
      currentSession = null;
    }
    if (registerer) {
      try { await registerer.unregister(); } catch (_) {}
      registerer = null;
    }
    if (userAgent) {
      try { await userAgent.stop(); } catch (_) {}
      userAgent = null;
    }
  } finally {
    el.call().hidden = true;
    el.connect().hidden = false;
    el.disconnect().hidden = true;
    if (!silent) setStatus("Снято с линии", "off");
  }
}

async function answer() {
  if (!currentSession) return;
  await currentSession.accept({
    sessionDescriptionHandlerOptions: { constraints: { audio: true, video: false } },
  });
}

async function reject() {
  if (!currentSession) return;
  try { await currentSession.reject(); } catch (_) {}
  currentSession = null;
  el.call().hidden = true;
  setStatus("На линии", "ok");
}

async function hangup() {
  if (!currentSession) return;
  try {
    if (currentSession.state === SessionState.Established) await currentSession.bye();
    else await currentSession.reject();
  } catch (_) {}
}

async function init() {
  const root = el.root();
  if (!root) return;
  const res = await fetch("/api/softphone/config");
  if (!res.ok) return;
  cfg = await res.json();
  if (!cfg.enabled) {
    root.hidden = true;
    return;
  }
  root.hidden = false;
  el.ext().value = cfg.extension || "";
  if (cfg.password) el.pass().value = cfg.password;
  el.auto().checked = !!cfg.auto_register;
  setStatus("Готов к регистрации", "off");

  el.connect().addEventListener("click", () => connect().catch((e) => setStatus(String(e.message || e), "err")));
  el.disconnect().addEventListener("click", () => disconnect(false));
  el.answer().addEventListener("click", () => answer().catch((e) => setStatus(String(e.message || e), "err")));
  el.reject().addEventListener("click", () => reject());
  el.hangup().addEventListener("click", () => hangup());

  if (cfg.auto_register && cfg.extension && cfg.password) {
    connect().catch((e) => setStatus(String(e.message || e), "err"));
  }
}

void Web;
init().catch(console.error);
