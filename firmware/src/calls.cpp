#include "calls.h"

#include <Arduino.h>
#include <WiFi.h>
#include <esp_heap_caps.h>
#include <fcntl.h>
#include <freertos/FreeRTOS.h>
#include <freertos/queue.h>
#include <freertos/task.h>
#include <lwip/netdb.h>
#include <lwip/sockets.h>
#include <opus.h>  // codec-opus (platformio.ini lib_deps)
#include <time.h>
#include <unistd.h>

#include <cstdio>
#include <cstring>
#include <new>

#include "board.h"
#include "net.h"
#include "wallclock.h"

namespace calls {
namespace {

namespace mc = mesh::call;

constexpr int kRate = 16000;
constexpr size_t kOpusFrame = 320;  // 20 ms
constexpr uint32_t kFrameMs = 20;
constexpr size_t kRing = 16000 * 3 / 10;  // 300 ms of samples
constexpr size_t kMicMax = 16000 / 10;    // keep at most 100 ms of microphone
constexpr size_t kSpeakerStart = 640;     // 40 ms queued before playing (USB and task jitter)
constexpr uint32_t kResolveEvery = 10 * 60 * 1000;

// ---- a sample ring shared by the main loop and the call task ----
struct Ring {
  int16_t* buf = nullptr;
  size_t head = 0, count = 0;
  portMUX_TYPE mux = portMUX_INITIALIZER_UNLOCKED;
  bool playing = false;

  void push(const int16_t* p, size_t n, size_t keep) {
    taskENTER_CRITICAL(&mux);
    for (size_t i = 0; i < n; i++) {
      if (count == kRing) head = (head + 1) % kRing, count--;
      buf[(head + count) % kRing] = p[i];
      count++;
    }
    while (count > keep) head = (head + 1) % kRing, count--;  // too much latency: drop the oldest
    taskEXIT_CRITICAL(&mux);
  }
  // n samples, zeros for what is missing; returns how many were real.
  size_t pop(int16_t* out, size_t n) {
    taskENTER_CRITICAL(&mux);
    size_t k = count < n ? count : n;
    for (size_t i = 0; i < k; i++) out[i] = buf[(head + i) % kRing];
    head = (head + k) % kRing;
    count -= k;
    taskEXIT_CRITICAL(&mux);
    for (size_t i = k; i < n; i++) out[i] = 0;
    return k;
  }
  void clear() {
    taskENTER_CRITICAL(&mux);
    head = count = 0;
    playing = false;
    taskEXIT_CRITICAL(&mux);
  }
  size_t size() {
    taskENTER_CRITICAL(&mux);
    size_t n = count;
    taskEXIT_CRITICAL(&mux);
    return n;
  }
};

Ring g_mic, g_speaker;

// ---- video in: reassembly buffer (the manager's) and a triple buffer to
// the UI, which decodes the newest frame when it can (frames may be skipped).
constexpr size_t kVideoMax = 64 * 1024;
uint8_t* g_vreasm = nullptr;
struct VideoSlot {
  uint8_t* d = nullptr;
  size_t n = 0;
};
VideoSlot g_vslot[3];
int g_vw = 0, g_vready = 1, g_vr = 2;  // writing, newest whole, being read
bool g_vfresh = false;
portMUX_TYPE g_vmux = portMUX_INITIALIZER_UNLOCKED;
volatile uint32_t g_vframes = 0;

// The call task: one whole frame = orientation(1) | JPEG.
void on_video_frame(void*, const uint8_t* f, size_t n, uint32_t, bool) {
  if (n < 3 || n - 1 > kVideoMax) return;
  VideoSlot& w = g_vslot[g_vw];
  std::memcpy(w.d, f + 1, n - 1);
  w.n = n - 1;
  taskENTER_CRITICAL(&g_vmux);
  int t = g_vready;
  g_vready = g_vw;
  g_vw = t;
  g_vfresh = true;
  taskEXIT_CRITICAL(&g_vmux);
  g_vframes = g_vframes + 1;
}

// ---- UDP over lwIP: one IPv4 socket on every interface ----
class LwipNet final : public mc::Net {
 public:
  int open() override {
    int fd = socket(AF_INET, SOCK_DGRAM, IPPROTO_UDP);
    if (fd < 0) return -1;
    sockaddr_in a = {};
    a.sin_family = AF_INET;
    a.sin_addr.s_addr = htonl(INADDR_ANY);
    a.sin_port = 0;
    if (bind(fd, reinterpret_cast<sockaddr*>(&a), sizeof a) != 0) {
      ::close(fd);
      return -1;
    }
    fcntl(fd, F_SETFL, O_NONBLOCK);
    return fd;
  }
  void close(int s) override { ::close(s); }
  uint16_t port(int s) override {
    sockaddr_in a = {};
    socklen_t n = sizeof a;
    if (getsockname(s, reinterpret_cast<sockaddr*>(&a), &n) != 0) return 0;
    return ntohs(a.sin_port);
  }
  bool send(int s, const mc::Addr& to, const uint8_t* d, size_t n) override {
    if (!to.is4()) return false;
    sockaddr_in a = {};
    a.sin_family = AF_INET;
    std::memcpy(&a.sin_addr.s_addr, to.ip, 4);
    a.sin_port = htons(to.port);
    return sendto(s, d, n, 0, reinterpret_cast<sockaddr*>(&a), sizeof a) == int(n);
  }
  // The Wi-Fi address, and the hotspot's (a phone on the board's own hotspot).
  size_t addrs(mc::Addr* out, size_t max) override {
    size_t k = 0;
    IPAddress ips[2] = {WiFi.isConnected() ? WiFi.localIP() : IPAddress(), WiFi.softAPIP()};
    for (const IPAddress& ip : ips) {
      if (k < max && uint32_t(ip) != 0) out[k++] = mc::Addr::v4(ip[0], ip[1], ip[2], ip[3], 0);
    }
    return k;
  }
};

LwipNet g_net;

// ---- between the main loop and the call task ----
enum class Op : uint8_t { Signal, Start, Answer, Hangup, Mute };
struct Cmd {
  Op op;
  bool contact;
  bool flag;
  mesh::PublicKey peer;
  uint16_t n;
  uint8_t d[mc::kMaxSignal];
};
struct Out {
  mesh::PublicKey to;
  uint16_t n;
  uint8_t d[mc::kMaxSignal];
};
QueueHandle_t g_cmds = nullptr, g_out = nullptr;

portMUX_TYPE g_view_mux = portMUX_INITIALIZER_UNLOCKED;
View g_view;
volatile uint32_t g_rev = 1;
volatile bool g_live = false;

// ---- the call task's own state ----
const mesh::Identity* g_self = nullptr;
mc::Manager* g_mgr = nullptr;
bool g_contact = false;  // the signal being handled comes from a saved contact
bool g_muted = false;
mc::AgentConfig g_direct;
uint32_t g_resolved_at = 0;
bool g_resolved = false;
OpusEncoder* g_enc = nullptr;
OpusDecoder* g_dec = nullptr;
uint8_t g_codec_for = 0;  // the call the codec state belongs to (reset per call)
uint32_t g_next_frame = 0;
// Counters for the log.
uint32_t g_frames_out = 0, g_frames_in = 0, g_plc = 0, g_calls = 0;
char g_last[48] = "";

uint32_t ms() { return millis(); }

void update_view() {
  const mc::Info& in = g_mgr->info();
  View v;
  v.state = in.state;
  v.incoming = in.incoming;
  v.peer = in.peer;
  std::memcpy(v.code, in.code, sizeof v.code);
  v.codec = in.codec;
  v.path_cls = in.path.cls;
  snprintf(v.reason, sizeof v.reason, "%s", in.reason ? in.reason : "");
  v.missed = in.missed;
  v.peer_muted = in.peer_muted;
  v.muted = g_muted;
  v.video_offered = in.video_offered;
  v.video = in.video;
  v.video_frames = g_vframes;
  v.active_ms = in.active_ms;
  taskENTER_CRITICAL(&g_view_mux);
  g_view = v;
  g_rev = g_rev + 1;
  taskEXIT_CRITICAL(&g_view_mux);
  g_live = g_mgr->live();
  if (in.state == mc::State::Ended) snprintf(g_last, sizeof g_last, "%s", v.reason);
}

// The relays' STUN reflectors (UDP 3478 and 3479 on the relay host): the
// board learns its public address from them for calls across NATs.
void resolve_reflectors(bool force) {
  uint32_t now = ms();
  if (!force && g_resolved && now - g_resolved_at < kResolveEvery) return;
  g_resolved_at = now;
  std::string host = net::relay_host();
  if (host.empty() || !WiFi.isConnected()) return;
  IPAddress ip;
  if (!ip.fromString(host.c_str()) && !WiFi.hostByName(host.c_str(), ip)) return;
  mc::AgentConfig c;
  c.reflectors[0] = mc::Addr::v4(ip[0], ip[1], ip[2], ip[3], 3478);
  c.reflectors[1] = mc::Addr::v4(ip[0], ip[1], ip[2], ip[3], 3479);
  c.n_reflectors = 2;
  g_direct = c;
  g_mgr->set_direct(c);
  g_resolved = true;
}

mc::Caps voice() {
  mc::Caps c;
  c.n = 1;
  c.streams[0].id = mc::kStreamAudio;
  c.streams[0].media = mc::kMediaAudio;
  c.streams[0].codecs[0] = mc::kCodecOpus;  // the board has Opus only
  c.streams[0].n_codecs = 1;
  c.streams[0].max_kbps = 24;
  return c;
}

bool codec_ready() {
  int err = 0;
  if (!g_enc) {
    g_enc = opus_encoder_create(kRate, 1, OPUS_APPLICATION_VOIP, &err);
    if (err != OPUS_OK) g_enc = nullptr;
    if (g_enc) {
      opus_encoder_ctl(g_enc, OPUS_SET_BITRATE(24000));
      opus_encoder_ctl(g_enc, OPUS_SET_COMPLEXITY(3));
      opus_encoder_ctl(g_enc, OPUS_SET_SIGNAL(OPUS_SIGNAL_VOICE));
    }
  }
  if (!g_dec) {
    g_dec = opus_decoder_create(kRate, 1, &err);
    if (err != OPUS_OK) g_dec = nullptr;
  }
  return g_enc && g_dec;
}

// One 20 ms step of an active call: our voice out, theirs into the speaker.
void media_step() {
  if (!codec_ready()) return;
  int16_t pcm[kOpusFrame];
  g_mic.pop(pcm, kOpusFrame);
  if (g_muted) std::memset(pcm, 0, sizeof pcm);
  uint8_t pkt[mc::kMaxAudio];
  int n = opus_encode(g_enc, pcm, int(kOpusFrame), pkt, int(sizeof pkt));
  if (n > 0 && g_mgr->send_audio(pkt, size_t(n), ms())) g_frames_out++;
  size_t got = 0;
  int r = g_mgr->pull_audio(pkt, sizeof pkt, &got);
  int k = 0;
  if (r == mc::Jitter::kFrame) {
    k = opus_decode(g_dec, pkt, int(got), pcm, int(kOpusFrame), 0);
    g_frames_in++;
  } else if (r == mc::Jitter::kLost) {
    k = opus_decode(g_dec, nullptr, 0, pcm, int(kOpusFrame), 0);  // concealment
    g_plc++;
  }
  if (k > 0) g_speaker.push(pcm, size_t(k), kRing);
}

void handle(const Cmd& c) {
  uint32_t now = ms();
  switch (c.op) {
    case Op::Signal: {
      g_contact = c.contact;
      mc::State before = g_mgr->info().state;
      g_mgr->on_signal(c.peer, c.d, c.n, now);
      if (g_mgr->info().state == mc::State::Ringing && before != mc::State::Ringing) {
        g_calls++;
        resolve_reflectors(false);  // while it rings: answering starts at once
      }
      break;
    }
    case Op::Start:
      resolve_reflectors(false);
      g_muted = false;
      if (g_mgr->start(c.peer, voice(), now)) g_calls++;
      break;
    case Op::Answer:
      resolve_reflectors(false);
      g_muted = false;
      g_mgr->answer(voice(), now);
      break;
    case Op::Hangup:
      g_mgr->hangup(now);
      break;
    case Op::Mute:
      g_muted = c.flag;
      g_mgr->set_muted(c.flag);
      update_view();
      break;
  }
}

void task(void*) {
  uint8_t buf[1500];
  for (;;) {
    // Wait for a datagram (or 5 ms).
    int fd = g_mgr->socket();
    if (fd >= 0) {
      fd_set rd;
      FD_ZERO(&rd);
      FD_SET(fd, &rd);
      timeval tv = {0, 5000};
      select(fd + 1, &rd, nullptr, nullptr, &tv);
    } else {
      vTaskDelay(pdMS_TO_TICKS(10));
    }
    static Cmd c;  // this task only
    while (xQueueReceive(g_cmds, &c, 0) == pdTRUE) handle(c);
    uint32_t now = ms();
    for (int i = 0; i < 32 && (fd = g_mgr->socket()) >= 0; i++) {
      sockaddr_in a = {};
      socklen_t al = sizeof a;
      int k = recvfrom(fd, buf, sizeof buf, 0, reinterpret_cast<sockaddr*>(&a), &al);
      if (k <= 0) break;
      const uint8_t* ip = reinterpret_cast<const uint8_t*>(&a.sin_addr.s_addr);
      g_mgr->on_datagram(mc::Addr::v4(ip[0], ip[1], ip[2], ip[3], ntohs(a.sin_port)), buf, size_t(k), now);
    }
    g_mgr->tick(now);
    if (g_mgr->info().state == mc::State::Active) {
      if (g_codec_for == 0) {  // a new call: fresh codec state and queues
        g_codec_for = 1;
        if (codec_ready()) {
          opus_encoder_ctl(g_enc, OPUS_RESET_STATE);
          opus_decoder_ctl(g_dec, OPUS_RESET_STATE);
        }
        g_mic.clear();
        g_speaker.clear();
        g_next_frame = now;
      }
      for (int i = 0; i < 3 && int32_t(now - g_next_frame) >= 0; i++) {
        media_step();
        g_next_frame += kFrameMs;
      }
      if (int32_t(now - g_next_frame) > int32_t(5 * kFrameMs)) g_next_frame = now;  // after a stall
    } else if (g_codec_for) {
      g_codec_for = 0;
      g_speaker.clear();
    }
  }
}

}  // namespace

bool begin(const mesh::Identity& self) {
  if (g_mgr) return true;
  g_self = &self;
  g_mic.buf = static_cast<int16_t*>(heap_caps_malloc(kRing * 2, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT));
  g_speaker.buf = static_cast<int16_t*>(heap_caps_malloc(kRing * 2, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT));
  g_vreasm = static_cast<uint8_t*>(heap_caps_malloc(kVideoMax, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT));
  for (VideoSlot& v : g_vslot) v.d = static_cast<uint8_t*>(heap_caps_malloc(kVideoMax, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT));
  g_cmds = xQueueCreateWithCaps(8, sizeof(Cmd), MALLOC_CAP_SPIRAM);
  g_out = xQueueCreateWithCaps(8, sizeof(Out), MALLOC_CAP_SPIRAM);
  if (!g_mic.buf || !g_speaker.buf || !g_cmds || !g_out) return false;
  mc::Env env;
  env.self = g_self;
  env.net = &g_net;
  env.send = [](void*, const mesh::PublicKey& peer, const uint8_t* s, size_t n) {
    if (n > mc::kMaxSignal) return false;
    static Out o;  // the call task only (the manager runs there)
    o.to = peer;
    o.n = uint16_t(n);
    std::memcpy(o.d, s, n);
    return xQueueSend(g_out, &o, 0) == pdTRUE;
  };
  env.allowed = [](void*, const mesh::PublicKey&) { return g_contact; };
  env.event = [](void*) { update_view(); };
  // An OFFER expires 45 s after this: the best clock we have, trusted or not.
  env.unix_now = [](void*) { return uint32_t(time(nullptr)); };
  env.random = [](uint8_t* out, size_t n) { board::random_bytes(out, n); };
  void* mem = heap_caps_malloc(sizeof(mc::Manager), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
  if (!mem) return false;
  g_mgr = new (mem) mc::Manager(env);
  if (g_vreasm && g_vslot[0].d && g_vslot[1].d && g_vslot[2].d) {
    // What the board shows: 320 × 240 at about 10 frames a second.
    mc::VideoParams want;
    want.width = 320, want.height = 240, want.fps = 10;
    g_mgr->set_video(g_vreasm, kVideoMax, want, on_video_frame, nullptr);
  }
  // Above the main loop (priority 1) on the same core: 20 ms audio steps
  // must not wait for a screen redraw. It sleeps in select() in between.
  return xTaskCreatePinnedToCore(task, "mava-call", 16384, nullptr, 2, nullptr, 1) == pdPASS;
}

namespace {
bool send_cmd(const Cmd& c) { return g_cmds && xQueueSend(g_cmds, &c, 0) == pdTRUE; }
}  // namespace

void on_signal(const mesh::PublicKey& from, bool contact, const uint8_t* sig, size_t n) {
  if (!g_cmds || n > mc::kMaxSignal) return;
  static Cmd c;  // main loop only
  c.op = Op::Signal;
  c.contact = contact;
  c.peer = from;
  c.n = uint16_t(n);
  std::memcpy(c.d, sig, n);
  send_cmd(c);
}

bool start(const mesh::PublicKey& peer) {
  if (live() || !g_cmds) return false;
  static Cmd c;
  c.op = Op::Start;
  c.peer = peer;
  c.n = 0;
  return send_cmd(c);
}

bool answer() {
  if (view().state != mc::State::Ringing) return false;
  static Cmd c;
  c.op = Op::Answer;
  c.n = 0;
  return send_cmd(c);
}

void hangup() {
  static Cmd c;
  c.op = Op::Hangup;
  c.n = 0;
  send_cmd(c);
}

void set_muted(bool muted) {
  static Cmd c;
  c.op = Op::Mute;
  c.flag = muted;
  c.n = 0;
  send_cmd(c);
}

bool take_signal(mesh::PublicKey& to, uint8_t* out, size_t cap, size_t* n) {
  if (!g_out) return false;
  static Out o;  // main loop only
  if (xQueueReceive(g_out, &o, 0) != pdTRUE) return false;
  if (o.n > cap) return false;
  to = o.to;
  std::memcpy(out, o.d, o.n);
  *n = o.n;
  return true;
}

View view() {
  taskENTER_CRITICAL(&g_view_mux);
  View v = g_view;
  taskEXIT_CRITICAL(&g_view_mux);
  return v;
}

uint32_t revision() { return g_rev; }
bool live() { return g_live; }

void mic(const int16_t* pcm, size_t n) {
  if (g_live && g_mic.buf) g_mic.push(pcm, n, kMicMax);
}

bool take_video(const uint8_t** jpeg, size_t* n) {
  if (!g_live) return false;
  taskENTER_CRITICAL(&g_vmux);
  bool fresh = g_vfresh;
  if (fresh) {
    int t = g_vr;
    g_vr = g_vready;
    g_vready = t;
    g_vfresh = false;
  }
  taskEXIT_CRITICAL(&g_vmux);
  if (!fresh) return false;
  *jpeg = g_vslot[g_vr].d;
  *n = g_vslot[g_vr].n;
  return *n > 0;
}

bool speaker(int16_t* out, size_t n) {
  if (!g_live || !g_speaker.buf) return false;
  if (!g_speaker.playing) {
    if (g_speaker.size() < kSpeakerStart) return false;
    g_speaker.playing = true;
  }
  if (g_speaker.pop(out, n) < n) g_speaker.playing = false;  // ran dry: buffer again
  return true;
}

void status(char* out, size_t cap) {
  View v = view();
  unsigned long late = g_mgr ? g_mgr->jitter().stats().late : 0, delay = g_mgr ? g_mgr->jitter().delay_ms() : 0;
  snprintf(out, cap,
           "%s (path class %d), calls %lu, last ended: %s; audio frames out %lu, in %lu, concealed %lu, late %lu, "
           "jitter buffer %lu ms; video %s, frames in %lu (lost %lu, too big %lu); STUN reflectors %s",
           mc::state_name(v.state), v.path_cls, (unsigned long)g_calls, g_last[0] ? g_last : "-",
           (unsigned long)g_frames_out, (unsigned long)g_frames_in, (unsigned long)g_plc, late, delay,
           v.video ? "on" : "off", (unsigned long)(g_mgr ? g_mgr->video_stats().frames : 0),
           (unsigned long)(g_mgr ? g_mgr->video_stats().lost : 0), (unsigned long)(g_mgr ? g_mgr->video_stats().too_big : 0),
           g_resolved ? "on the relay host" : "none yet");
}

}  // namespace calls
