// Minimal JSON reader for the host tests: enough to load
// spec/test-vectors/vectors.json without a third-party dependency.
#pragma once

#include <cstdint>
#include <map>
#include <memory>
#include <stdexcept>
#include <string>
#include <vector>

namespace json {

struct Value {
  enum Kind { Null, Bool, Number, String, Array, Object } kind = Null;
  bool b = false;
  double num = 0;
  std::string str;
  std::vector<Value> arr;
  std::map<std::string, Value> obj;

  const Value& operator[](const std::string& k) const {
    auto it = obj.find(k);
    if (kind != Object || it == obj.end()) throw std::runtime_error("json: missing key " + k);
    return it->second;
  }
  bool has(const std::string& k) const { return kind == Object && obj.count(k) != 0; }
  const std::string& s() const {
    if (kind != String) throw std::runtime_error("json: not a string");
    return str;
  }
  long long i() const {
    if (kind != Number) throw std::runtime_error("json: not a number");
    return static_cast<long long>(num);
  }
};

class Parser {
 public:
  explicit Parser(const std::string& src) : s_(src) {}

  Value parse() {
    Value v = value();
    ws();
    if (p_ != s_.size()) fail("trailing data");
    return v;
  }

 private:
  [[noreturn]] void fail(const char* what) {
    throw std::runtime_error(std::string("json: ") + what + " at offset " + std::to_string(p_));
  }
  void ws() {
    while (p_ < s_.size() && (s_[p_] == ' ' || s_[p_] == '\n' || s_[p_] == '\r' || s_[p_] == '\t')) p_++;
  }
  char peek() {
    ws();
    if (p_ >= s_.size()) fail("unexpected end");
    return s_[p_];
  }
  void expect(char c) {
    if (peek() != c) fail("unexpected character");
    p_++;
  }
  bool lit(const char* w) {
    const std::string word(w);
    if (s_.compare(p_, word.size(), word) == 0) {
      p_ += word.size();
      return true;
    }
    return false;
  }

  Value value() {
    Value v;
    const char c = peek();
    if (c == '{') {
      v.kind = Value::Object;
      p_++;
      if (peek() == '}') {
        p_++;
        return v;
      }
      for (;;) {
        if (peek() != '"') fail("expected key");
        std::string k = string();
        expect(':');
        v.obj[k] = value();
        if (peek() == ',') {
          p_++;
          continue;
        }
        expect('}');
        return v;
      }
    }
    if (c == '[') {
      v.kind = Value::Array;
      p_++;
      if (peek() == ']') {
        p_++;
        return v;
      }
      for (;;) {
        v.arr.push_back(value());
        if (peek() == ',') {
          p_++;
          continue;
        }
        expect(']');
        return v;
      }
    }
    if (c == '"') {
      v.kind = Value::String;
      v.str = string();
      return v;
    }
    if (lit("true")) {
      v.kind = Value::Bool;
      v.b = true;
      return v;
    }
    if (lit("false")) {
      v.kind = Value::Bool;
      return v;
    }
    if (lit("null")) return v;
    size_t used = 0;
    try {
      v.num = std::stod(s_.substr(p_, 64), &used);
    } catch (...) {
      fail("bad value");
    }
    v.kind = Value::Number;
    p_ += used;
    return v;
  }

  static void utf8(std::string& out, uint32_t cp) {
    if (cp < 0x80) {
      out += char(cp);
    } else if (cp < 0x800) {
      out += char(0xC0 | cp >> 6);
      out += char(0x80 | (cp & 0x3F));
    } else if (cp < 0x10000) {
      out += char(0xE0 | cp >> 12);
      out += char(0x80 | (cp >> 6 & 0x3F));
      out += char(0x80 | (cp & 0x3F));
    } else {
      out += char(0xF0 | cp >> 18);
      out += char(0x80 | (cp >> 12 & 0x3F));
      out += char(0x80 | (cp >> 6 & 0x3F));
      out += char(0x80 | (cp & 0x3F));
    }
  }

  uint32_t hex4() {
    if (p_ + 4 > s_.size()) fail("short \\u escape");
    const uint32_t v = static_cast<uint32_t>(std::stoul(s_.substr(p_, 4), nullptr, 16));
    p_ += 4;
    return v;
  }

  std::string string() {
    expect('"');
    std::string out;
    while (p_ < s_.size()) {
      const char c = s_[p_++];
      if (c == '"') return out;
      if (c != '\\') {
        out += c;
        continue;
      }
      if (p_ >= s_.size()) break;
      const char e = s_[p_++];
      switch (e) {
        case '"': out += '"'; break;
        case '\\': out += '\\'; break;
        case '/': out += '/'; break;
        case 'b': out += '\b'; break;
        case 'f': out += '\f'; break;
        case 'n': out += '\n'; break;
        case 'r': out += '\r'; break;
        case 't': out += '\t'; break;
        case 'u': {
          uint32_t cp = hex4();
          if (cp >= 0xD800 && cp < 0xDC00 && lit("\\u")) cp = 0x10000 + ((cp - 0xD800) << 10) + (hex4() - 0xDC00);
          utf8(out, cp);
          break;
        }
        default: fail("bad escape");
      }
    }
    fail("unterminated string");
  }

  const std::string& s_;
  size_t p_ = 0;
};

inline Value parse(const std::string& src) { return Parser(src).parse(); }

}  // namespace json
