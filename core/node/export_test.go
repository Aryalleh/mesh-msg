package node

import "time"

// SetLoRaRetry shortens the port reopen delay in tests.
func SetLoRaRetry(d time.Duration) { loraRetry = d }
