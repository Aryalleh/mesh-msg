// What the handheld UI needs from a messaging client: the real one is
// lib/meshclient behind src/ui/client_messenger.cpp (docs/handheld-proto.md
// §8); the stub keeps local notes (the UI simulator, boards without storage).
#pragma once

#include <cstddef>
#include <cstdint>
#include <string>
#include <vector>

namespace ui {

struct Contact {
  std::string id, name;
};

struct Message {
  bool mine = true;
  std::string text;
  uint32_t time = 0;  // Unix seconds, 0 = unknown
  bool delivered = false;  // mine: a signed receipt came back
};

// The call in progress (or the one that just ended), for the call screen.
struct CallView {
  enum class State : uint8_t { None, Outgoing, Ringing, Connecting, Active, Ended };
  State state = State::None;
  bool incoming = false;
  std::string peer;    // contact name
  std::string code;    // "123 456": both sides see the same, compare by voice
  std::string reason;  // why it ended
  bool missed = false, muted = false, peer_muted = false, video_offered = false;
  bool video = false;  // the other side can show its camera in this call
  int path = 0;          // path class: 1 same network … 5 across NAT
  uint32_t seconds = 0;  // talking time
};

class Messenger {
 public:
  virtual ~Messenger() = default;
  // False: no client core; the UI says so and works on local notes only.
  virtual bool connected() const = 0;
  virtual std::vector<Contact> contacts() = 0;
  virtual std::vector<Message> messages(const std::string& contact) = 0;
  virtual bool send(const std::string& contact, const std::string& text, uint32_t now) = 0;
  // Calls a contact; false + why (shown to the user) if it can't start.
  virtual bool call(const std::string& contact) = 0;
  virtual std::string call_error() { return ""; }
  virtual void hang_up() = 0;
  virtual bool answer() { return false; }
  virtual void set_muted(bool) {}
  virtual CallView call_view() { return CallView(); }
  // Grows with every change of the call.
  virtual uint32_t call_revision() { return 0; }
  // The newest JPEG from the other side's camera since the last call, or false.
  virtual bool take_video(const uint8_t** jpeg, size_t* n) {
    (void)jpeg, (void)n;
    return false;
  }
  // Our contact link (QR code for the apps to add us) and nickname; "" = none.
  virtual std::string my_link() { return ""; }
  virtual std::string my_name() { return ""; }
  // Grows when contacts or messages change: the UI refreshes.
  virtual uint32_t revision() { return 0; }
};

Messenger& stub_messenger();

}  // namespace ui
