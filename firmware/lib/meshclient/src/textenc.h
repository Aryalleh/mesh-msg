// Compact text encoding (content type 1), byte-identical to core/textenc:
// Persian/Arabic at one byte per character. 0x00 escapes one UTF-8 rune,
// 0x01..0x7F is ASCII, 0x80.. indexes the table (append-only, wire format).
#pragma once

#include <cstddef>
#include <cstdint>
#include <string>

namespace mesh {
namespace textenc {

// Picks the smaller of UTF-8 (type 0) and compact (type 1).
uint8_t encode(const std::string& utf8, std::string& out);
// Reverses encode; false on invalid input.
bool decode(uint8_t content_type, const uint8_t* b, size_t n, std::string& out);

}  // namespace textenc
}  // namespace mesh
