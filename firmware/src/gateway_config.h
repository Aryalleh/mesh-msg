# ESP32-S3 Mava gateway configuration.
#
# Put real values in a local build override instead of committing credentials.
# PlatformIO accepts these as build flags, for example:
#   -DMAVA_WIFI_SSID=\"my-ap\"
#   -DMAVA_WIFI_PASSWORD=\"secret\"
#   -DMAVA_RELAY_URL=\"https://relay.example\"
#
#ifndef MAVA_WIFI_SSID
#define MAVA_WIFI_SSID ""
#endif
#ifndef MAVA_WIFI_PASSWORD
#define MAVA_WIFI_PASSWORD ""
#endif
#ifndef MAVA_RELAY_URL
#define MAVA_RELAY_URL ""
#endif
#ifndef MAVA_RELAY_ORIGIN
#define MAVA_RELAY_ORIGIN "esp32s3-gateway"
#endif
#ifndef MAVA_RELAY_POW_BITS
#define MAVA_RELAY_POW_BITS 0
#endif
#ifndef MAVA_BLE_NAME
#define MAVA_BLE_NAME "Mava-Gateway"
#endif
