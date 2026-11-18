// LVGL's heap in PSRAM (platformio.ini: LV_USE_STDLIB_MALLOC=255, custom).
// With the C library's malloc every LVGL object (all under 4 kB) landed in
// internal RAM, which Wi-Fi, Bluetooth and TLS need: walking through the
// menus used it up and the board restarted ("esp-aes: Failed to allocate
// memory", "BLE_INIT: Malloc failed", owner, beta35). The draw buffer stays in
// internal RAM (display.cpp): that is where speed matters.
#include <esp_heap_caps.h>
#include <lvgl.h>

#include <cstring>

namespace {
constexpr uint32_t kPsram = MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT;
constexpr uint32_t kInternal = MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT;
}  // namespace

extern "C" {

void lv_mem_init(void) {}

void lv_mem_deinit(void) {}

lv_mem_pool_t lv_mem_add_pool(void* mem, size_t bytes) {
  (void)mem;
  (void)bytes;
  return nullptr;
}

void lv_mem_remove_pool(lv_mem_pool_t pool) { (void)pool; }

void* lv_malloc_core(size_t size) {
  void* p = heap_caps_malloc(size, kPsram);
  return p ? p : heap_caps_malloc(size, kInternal);
}

void* lv_realloc_core(void* p, size_t new_size) {
  void* r = heap_caps_realloc(p, new_size, kPsram);
  return r ? r : heap_caps_realloc(p, new_size, kInternal);
}

void lv_free_core(void* p) { heap_caps_free(p); }

void lv_mem_monitor_core(lv_mem_monitor_t* mon_p) {
  if (mon_p) std::memset(mon_p, 0, sizeof *mon_p);
}

lv_result_t lv_mem_test_core(void) { return LV_RESULT_OK; }

}  // extern "C"
