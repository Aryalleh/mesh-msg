// A board call peer on the host: the firmware's call manager (lib/meshcall)
// with real UDP sockets, driven over stdin/stdout. core/call's
// TestBoardPeer runs it against the Go phone code (signals over the pipes,
// media over loopback UDP). Lines, one per message:
//
//   in:  peer <pub hex> | call | answer | hangup | mute 0|1 | sig <hex>
//   out: id <pub hex> | sig <hex> | state|<state>|<reason>|<code>|<codec>|<class>|<video codec>
//        | rx <frames> (audio from the other side, every 25 frames)
//        | video <bytes> <fnv-1a 32 hex> (each video frame taken in, MJPEG receive-only)
//
// While a call is active it sends one "board" + seq audio frame every 20 ms.
#include <arpa/inet.h>
#include <fcntl.h>
#include <netinet/in.h>
#include <poll.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>

#include <cstdio>
#include <cstring>
#include <string>
#include <vector>

#include "call.h"

namespace mc = mesh::call;

namespace {

uint32_t now_ms() {
  timespec t;
  clock_gettime(CLOCK_MONOTONIC, &t);
  return uint32_t(uint64_t(t.tv_sec) * 1000 + uint64_t(t.tv_nsec) / 1000000);
}

void urandom(uint8_t* out, size_t n) {
  FILE* f = std::fopen("/dev/urandom", "rb");
  if (!f || std::fread(out, 1, n, f) != n) std::abort();
  std::fclose(f);
}

std::string hex(const uint8_t* b, size_t n) {
  static const char* d = "0123456789abcdef";
  std::string s;
  for (size_t i = 0; i < n; i++) s += d[b[i] >> 4], s += d[b[i] & 15];
  return s;
}

std::vector<uint8_t> unhex(const std::string& s) {
  std::vector<uint8_t> out;
  for (size_t i = 0; i + 1 < s.size(); i += 2) out.push_back(uint8_t(std::stoul(s.substr(i, 2), nullptr, 16)));
  return out;
}

void say(const std::string& s) {
  std::fputs((s + "\n").c_str(), stdout);
  std::fflush(stdout);
}

// Loopback-only UDP: one IPv4 socket, host candidate 127.0.0.1.
struct UdpNet : mc::Net {
  int open() override {
    int fd = socket(AF_INET, SOCK_DGRAM, 0);
    if (fd < 0) return -1;
    sockaddr_in a{};
    a.sin_family = AF_INET;
    a.sin_addr.s_addr = htonl(INADDR_ANY);
    if (bind(fd, reinterpret_cast<sockaddr*>(&a), sizeof a) != 0) {
      ::close(fd);
      return -1;
    }
    fcntl(fd, F_SETFL, O_NONBLOCK);
    return fd;
  }
  void close(int s) override { ::close(s); }
  uint16_t port(int s) override {
    sockaddr_in a{};
    socklen_t n = sizeof a;
    getsockname(s, reinterpret_cast<sockaddr*>(&a), &n);
    return ntohs(a.sin_port);
  }
  bool send(int s, const mc::Addr& to, const uint8_t* d, size_t n) override {
    if (!to.is4()) return false;
    sockaddr_in a{};
    a.sin_family = AF_INET;
    std::memcpy(&a.sin_addr, to.ip, 4);
    a.sin_port = htons(to.port);
    return sendto(s, d, n, 0, reinterpret_cast<sockaddr*>(&a), sizeof a) == ssize_t(n);
  }
  size_t addrs(mc::Addr* out, size_t max) override {
    if (!max) return 0;
    out[0] = mc::Addr::v4(127, 0, 0, 1, 0);
    return 1;
  }
};

struct Peer {
  mesh::Identity id;
  mesh::PublicKey peer;
  bool have_peer = false;
  mc::Manager* m = nullptr;
  mc::State last = mc::State::Idle;
};

Peer* g;

mc::Caps voice(std::initializer_list<uint8_t> codecs) {
  mc::Caps c;
  c.n = 1;
  c.streams[0].media = mc::kMediaAudio;
  for (uint8_t k : codecs) c.streams[0].codecs[c.streams[0].n_codecs++] = k;
  c.streams[0].max_kbps = 24;
  return c;
}

void on_event(void*) {
  const mc::Info& in = g->m->info();
  char line[160];
  std::snprintf(line, sizeof line, "state|%s|%s|%s|%u|%d|%u", mc::state_name(in.state), in.reason, in.code,
                unsigned(in.codec), in.path.cls, unsigned(in.video ? in.video_codec : 0));
  say(line);
  if (in.peer_muted) say("muted");
}

}  // namespace

int main() {
  uint8_t seed[mesh::kSeedSize];
  urandom(seed, sizeof seed);
  Peer p{mesh::Identity::from_seed(seed), {}, false, nullptr, mc::State::Idle};
  g = &p;
  UdpNet net;
  mc::Env env;
  env.self = &p.id;
  env.net = &net;
  env.direct.check_ms = 3000;
  env.send = [](void*, const mesh::PublicKey&, const uint8_t* s, size_t n) {
    say("sig " + hex(s, n));
    return true;
  };
  env.allowed = [](void*, const mesh::PublicKey& k) {
    return g->have_peer && std::memcmp(k.x, g->peer.x, 32) == 0;
  };
  env.event = on_event;
  env.unix_now = [](void*) { return uint32_t(time(nullptr)); };
  env.random = urandom;
  mc::Manager m(env);
  p.m = &m;
  static uint8_t vbuf[64 * 1024];
  mc::VideoParams want;
  want.width = 320, want.height = 240, want.fps = 10;
  m.set_video(vbuf, sizeof vbuf, want,
              [](void*, const uint8_t* f, size_t n, uint32_t, bool) {
                uint32_t h = 2166136261u;
                for (size_t i = 0; i < n; i++) h = (h ^ f[i]) * 16777619u;
                char l[64];
                std::snprintf(l, sizeof l, "video %zu %08x", n, h);
                say(l);
              },
              nullptr);
  uint8_t pub[mesh::kPublicSize];
  std::memcpy(pub, p.id.pub().x, 32);
  std::memcpy(pub + 32, p.id.pub().ed, 32);
  say("id " + hex(pub, sizeof pub));

  std::string in;
  uint32_t next_audio = now_ms(), seq = 0, rx = 0;
  for (;;) {
    pollfd fds[2] = {{0, POLLIN, 0}, {m.socket(), POLLIN, 0}};
    int nf = m.socket() >= 0 ? 2 : 1;
    poll(fds, nfds_t(nf), 5);
    uint32_t now = now_ms();
    if (fds[0].revents & (POLLIN | POLLHUP)) {
      char buf[4096];
      ssize_t k = read(0, buf, sizeof buf);
      if (k <= 0) return 0;  // the test is done
      in.append(buf, size_t(k));
      size_t nl;
      while ((nl = in.find('\n')) != std::string::npos) {
        std::string line = in.substr(0, nl);
        in.erase(0, nl + 1);
        if (line.rfind("peer ", 0) == 0) {
          auto b = unhex(line.substr(5));
          if (b.size() == mesh::kPublicSize) p.peer = mesh::PublicKey::from_bytes(b.data()), p.have_peer = true;
        } else if (line == "call") {
          m.start(p.peer, voice({mc::kCodecOpus}), now);
        } else if (line == "answer") {
          m.answer(voice({mc::kCodecOpus, mc::kCodecAmrWb}), now);
        } else if (line == "hangup") {
          m.hangup(now);
        } else if (line.rfind("mute ", 0) == 0) {
          m.set_muted(line[5] == '1');
        } else if (line.rfind("sig ", 0) == 0) {
          auto b = unhex(line.substr(4));
          m.on_signal(p.peer, b.data(), b.size(), now);
        }
      }
    }
    if (nf == 2 && (fds[1].revents & POLLIN)) {
      for (;;) {
        uint8_t d[1500];
        sockaddr_in a{};
        socklen_t al = sizeof a;
        ssize_t k = recvfrom(fds[1].fd, d, sizeof d, 0, reinterpret_cast<sockaddr*>(&a), &al);
        if (k <= 0) break;
        const uint8_t* ip = reinterpret_cast<const uint8_t*>(&a.sin_addr);
        m.on_datagram(mc::Addr::v4(ip[0], ip[1], ip[2], ip[3], ntohs(a.sin_port)), d, size_t(k), now);
        if (m.socket() != fds[1].fd) break;  // the call ended
      }
    }
    m.tick(now);
    if (m.info().state == mc::State::Active && int32_t(now - next_audio) >= 0) {
      next_audio = now + 20;
      uint8_t f[9] = {'b', 'o', 'a', 'r', 'd'};
      std::memcpy(f + 5, &seq, 4);
      seq++;
      m.send_audio(f, sizeof f, now);
      uint8_t out[mc::kMaxAudio];
      size_t k;
      if (m.pull_audio(out, sizeof out, &k) == mc::Jitter::kFrame && k >= 5 && std::memcmp(out, "phone", 5) == 0) {
        rx++;
        if (rx % 25 == 0) say("rx " + std::to_string(rx));
      }
    }
  }
}
