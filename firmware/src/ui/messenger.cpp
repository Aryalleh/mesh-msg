#include "messenger.h"

namespace ui {

namespace {

// Local notes: one conversation, kept in RAM (PSRAM through malloc).
class Stub final : public Messenger {
 public:
  bool connected() const override { return false; }
  std::vector<Contact> contacts() override { return {Contact{"notes", "یادداشت‌ها / Notes"}}; }
  std::vector<Message> messages(const std::string& contact) override {
    return contact == "notes" ? notes_ : std::vector<Message>{};
  }
  bool send(const std::string& contact, const std::string& text, uint32_t now) override {
    if (contact != "notes" || text.empty()) return false;
    if (notes_.size() >= 200) notes_.erase(notes_.begin());
    notes_.push_back(Message{true, text, now});
    return true;
  }
  bool call(const std::string&) override { return false; }
  void hang_up() override {}

 private:
  std::vector<Message> notes_;
};

}  // namespace

Messenger& stub_messenger() {
  static Stub s;
  return s;
}

}  // namespace ui
