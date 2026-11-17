package org.meshmsg.boardlink

import android.hardware.usb.UsbConstants
import android.hardware.usb.UsbDevice
import android.hardware.usb.UsbDeviceConnection
import android.hardware.usb.UsbEndpoint
import android.hardware.usb.UsbInterface
import android.hardware.usb.UsbManager
import java.io.Closeable
import java.io.IOException

/**
 * A USB serial port in plain Android USB host calls, for what ESP32-S3 boards
 * have: the chip's own USB (USB-Serial/JTAG, a CDC-ACM device), Silicon Labs
 * CP210x and WCH CH340/CH341 bridges. Other CDC-ACM bridges (CH343/CH9102)
 * work as CDC-ACM.
 */
class UsbSerial private constructor(
    private val manager: UsbManager,
    val device: UsbDevice,
    val kind: Kind,
    private val conn: UsbDeviceConnection,
    private val claimed: List<UsbInterface>,
    private val input: UsbEndpoint,
    private val output: UsbEndpoint,
    private val ifc: Int,
) : SerialLink, Closeable {

    enum class Kind(val label: String) {
        NativeUsb("ESP32-S3 USB"),
        CdcAcm("USB serial (CDC)"),
        Cp210x("CP210x USB-UART"),
        Ch34x("CH340 USB-UART"),
    }

    override val nativeUsb: Boolean get() = kind == Kind.NativeUsb
    @Volatile private var closed = false

    override fun write(data: ByteArray) {
        var off = 0
        while (off < data.size) {
            if (closed) throw IOException("closed")
            val len = minOf(data.size - off, 16 * 1024)
            val n = conn.bulkTransfer(output, data, off, len, 3000)
            if (n <= 0) {
                checkPresent()
                throw IOException("USB write failed")
            }
            off += n
        }
    }

    override fun read(buf: ByteArray, timeoutMs: Int): Int {
        if (closed) throw IOException("closed")
        val n = conn.bulkTransfer(input, buf, buf.size, timeoutMs.coerceAtLeast(1))
        if (n < 0) {
            // A timeout and a pulled cable both return -1.
            checkPresent()
            return 0
        }
        return n
    }

    private fun checkPresent() {
        if (manager.deviceList.values.none { it.deviceName == device.deviceName }) throw IOException("the board was unplugged")
    }

    override fun setLines(dtr: Boolean, rts: Boolean) {
        val d = if (dtr) 1 else 0
        val r = if (rts) 1 else 0
        when (kind) {
            Kind.NativeUsb, Kind.CdcAcm -> ctrl(0x21, 0x22, d or (r shl 1), ifc)
            Kind.Cp210x -> ctrl(0x41, CP_SET_MHS, d or (r shl 1) or 0x0300, ifc)
            Kind.Ch34x -> ctrl(0x40, 0xA4, ((d shl 5) or (r shl 6)).inv() and 0xFF, 0)
        }
    }

    override fun setBaud(baud: Int) {
        when (kind) {
            Kind.NativeUsb, Kind.CdcAcm -> {
                val coding = byteArrayOf(
                    baud.toByte(), (baud shr 8).toByte(), (baud shr 16).toByte(), (baud shr 24).toByte(),
                    0, 0, 8, // 1 stop bit, no parity, 8 data bits
                )
                ctrl(0x21, 0x20, 0, ifc, coding)
            }
            Kind.Cp210x -> ctrl(0x41, CP_SET_BAUDRATE, 0, ifc, byteArrayOf(baud.toByte(), (baud shr 8).toByte(), (baud shr 16).toByte(), (baud shr 24).toByte()))
            Kind.Ch34x -> {
                val (a, b) = ch34xBaud(baud)
                ctrl(0x40, 0x9A, 0x1312, a)
                ctrl(0x40, 0x9A, 0x0F2C, b)
            }
        }
    }

    private fun ctrl(type: Int, req: Int, value: Int, index: Int, data: ByteArray? = null) {
        val n = conn.controlTransfer(type, req, value, index, data, data?.size ?: 0, 1000)
        if (n < 0) {
            checkPresent()
            throw IOException("USB control request 0x%02x failed".format(req))
        }
    }

    override fun close() {
        if (closed) return
        closed = true
        claimed.forEach { runCatching { conn.releaseInterface(it) } }
        runCatching { conn.close() }
    }

    companion object {
        const val ESPRESSIF = 0x303A
        const val SILABS = 0x10C4
        const val WCH = 0x1A86
        private const val CP_IFC_ENABLE = 0x00
        private const val CP_SET_LINE_CTL = 0x03
        private const val CP_SET_MHS = 0x07
        private const val CP_SET_BAUDRATE = 0x1E

        fun kindOf(d: UsbDevice): Kind? {
            val cdc = (0 until d.interfaceCount).any { d.getInterface(it).interfaceClass == UsbConstants.USB_CLASS_CDC_DATA }
            return when {
                d.vendorId == ESPRESSIF && cdc -> Kind.NativeUsb
                d.vendorId == SILABS -> Kind.Cp210x
                cdc -> Kind.CdcAcm
                d.vendorId == WCH -> Kind.Ch34x
                else -> null
            }
        }

        /** The first serial device plugged in, the board's own USB first. */
        fun find(manager: UsbManager): UsbDevice? =
            manager.deviceList.values.filter { kindOf(it) != null }.minByOrNull { kindOf(it)!!.ordinal }

        fun open(manager: UsbManager, device: UsbDevice, baud: Int = 115200): UsbSerial {
            val kind = kindOf(device) ?: throw IOException("not a serial device")
            val conn = manager.openDevice(device) ?: throw IOException("cannot open the USB device (permission?)")
            try {
                val claimed = mutableListOf<UsbInterface>()
                val data: UsbInterface
                var ifc = 0
                if (kind == Kind.NativeUsb || kind == Kind.CdcAcm) {
                    val all = (0 until device.interfaceCount).map { device.getInterface(it) }
                    val comm = all.firstOrNull { it.interfaceClass == UsbConstants.USB_CLASS_COMM }
                    data = all.first { it.interfaceClass == UsbConstants.USB_CLASS_CDC_DATA }
                    if (comm != null) {
                        conn.claimInterface(comm, true)
                        claimed += comm
                        ifc = comm.id
                    }
                } else {
                    data = device.getInterface(0)
                    ifc = data.id
                }
                if (!conn.claimInterface(data, true)) throw IOException("cannot claim the USB serial interface")
                claimed += data
                var input: UsbEndpoint? = null
                var output: UsbEndpoint? = null
                for (e in 0 until data.endpointCount) {
                    val ep = data.getEndpoint(e)
                    if (ep.type != UsbConstants.USB_ENDPOINT_XFER_BULK) continue
                    if (ep.direction == UsbConstants.USB_DIR_IN) input = ep else output = ep
                }
                if (input == null || output == null) throw IOException("no bulk endpoints")
                val port = UsbSerial(manager, device, kind, conn, claimed, input, output, ifc)
                port.init(baud)
                return port
            } catch (e: Exception) {
                conn.close()
                throw if (e is IOException) e else IOException(e.message ?: e.toString())
            }
        }

        /** CH340 baud registers, as Linux's ch341 driver computes them. */
        fun ch34xBaud(baud: Int): Pair<Int, Int> {
            var factor: Long
            var divisor: Long
            if (baud == 921600) {
                divisor = 7
                factor = 0xF300
            } else {
                factor = 1532620800L / baud
                divisor = 3
                while (factor > 0xFFF0 && divisor > 0) {
                    factor = factor shr 3
                    divisor--
                }
                factor = 0x10000 - factor
            }
            divisor = divisor or 0x80
            return Pair(((factor and 0xFF00) or divisor).toInt(), (factor and 0xFF).toInt())
        }
    }

    private fun init(baud: Int) {
        when (kind) {
            Kind.NativeUsb, Kind.CdcAcm -> setBaud(baud)
            Kind.Cp210x -> {
                ctrl(0x41, CP_IFC_ENABLE, 1, ifc)
                ctrl(0x41, CP_SET_LINE_CTL, 0x0800, ifc) // 8N1
                setBaud(baud)
            }
            Kind.Ch34x -> {
                val v = ByteArray(2)
                conn.controlTransfer(0xC0, 0x5F, 0, 0, v, 2, 1000)
                ctrl(0x40, 0xA1, 0, 0)
                setBaud(baud)
                ctrl(0x40, 0x9A, 0x2518, 0x00C3) // 8N1, RX and TX on
                ctrl(0x40, 0xA1, 0x501F, 0xD90A)
                setBaud(baud)
            }
        }
        setLines(dtr = false, rts = false)
    }
}
