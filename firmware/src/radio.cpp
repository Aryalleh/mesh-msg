#include "radio.h"

#include <Arduino.h>
#include <RadioLib.h>
#include <SPI.h>

#include "meshtastic.h"

namespace radio {
namespace {

SX1262 g_radio = new Module(RADIO_CS, RADIO_DIO1, RADIO_RST, RADIO_BUSY, SPI);
volatile bool g_irq = false;
bool g_txing = false;
bool g_ok = false;

// Meshtastic LongFast (docs/board.md §3): the board joins the Meshtastic
// backbone, so modulation, sync word and preamble are Meshtastic's. Region
// and frequency come from the node config; the frequency must be the one
// the local Meshtastic mesh uses (869.525 MHz for EU_868, 906.875 MHz for US
// with the default LongFast channel).
constexpr float kBwKHz = 250.0f;
constexpr uint8_t kSf = 11;
constexpr uint8_t kCr = 5;  // 4/5
constexpr uint8_t kSyncWord = 0x2B;
constexpr uint16_t kPreamble = 16;

void IRAM_ATTR on_dio1() { g_irq = true; }

bool apply(const mesh::host::NodeConfig& cfg, bool first) {
  const mesh::host::RegionInfo& r = mesh::host::kRegions[cfg.region];
  float mhz = cfg.channel_hz() / 1e6f;
  int8_t dbm = cfg.tx_dbm;
  if (dbm > r.max_dbm) dbm = r.max_dbm;
  if (dbm > 22) dbm = 22;
  if (first) {
    int16_t st = g_radio.begin(mhz, kBwKHz, kSf, kCr, kSyncWord, dbm, kPreamble, RADIO_TCXO_MV / 1000.0f, false);
    if (st != RADIOLIB_ERR_NONE) return false;
#if RADIO_DIO2_RF_SWITCH
    g_radio.setDio2AsRfSwitch(true);
#endif
    g_radio.setCRC(2);
    g_radio.explicitHeader();
    g_radio.autoLDRO();
    g_radio.setDio1Action(on_dio1);
  } else {
    g_radio.standby();
    if (g_radio.setFrequency(mhz) != RADIOLIB_ERR_NONE) return false;
    if (g_radio.setOutputPower(dbm) != RADIOLIB_ERR_NONE) return false;
  }
  g_txing = false;
  g_irq = false;
  return g_radio.startReceive() == RADIOLIB_ERR_NONE;
}

}  // namespace

bool begin(const mesh::host::NodeConfig& cfg) {
  SPI.begin(RADIO_SCK, RADIO_MISO, RADIO_MOSI, RADIO_CS);
  g_ok = apply(cfg, true);
  return g_ok;
}

bool configure(const mesh::host::NodeConfig& cfg) {
  if (!g_ok) return false;
  return apply(cfg, false);
}

bool transmit(const uint8_t* frame, size_t n) {
  if (!g_ok || g_txing || n == 0 || n > mesh::meshtastic::kRadioMax) return false;
  g_irq = false;
  if (g_radio.startTransmit(const_cast<uint8_t*>(frame), n) != RADIOLIB_ERR_NONE) {
    g_radio.startReceive();
    return false;
  }
  g_txing = true;
  return true;
}

bool transmitting() { return g_txing; }

bool tx_done() {
  if (!g_txing || !g_irq) return false;
  g_irq = false;
  g_txing = false;
  g_radio.finishTransmit();
  g_radio.startReceive();
  return true;
}

size_t receive(uint8_t* buf, size_t cap, int* snr_db) {
  if (!g_ok || g_txing || !g_irq) return 0;
  g_irq = false;
  size_t n = g_radio.getPacketLength();
  size_t got = 0;
  if (n > 0 && n <= cap && g_radio.readData(buf, n) == RADIOLIB_ERR_NONE) {
    got = n;
    if (snr_db) *snr_db = int(g_radio.getSNR());
  }
  g_radio.startReceive();
  return got;
}

bool cad_busy() {
  if (!g_ok || g_txing) return true;
  int16_t st = g_radio.scanChannel();
  g_irq = false;  // CAD-done also raises DIO1
  g_radio.startReceive();
  return st == RADIOLIB_LORA_DETECTED;
}

const char* chip_name() { return "SX1262"; }

}  // namespace radio
