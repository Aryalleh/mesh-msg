package org.meshmsg.app.data

import android.content.Context
import android.net.ConnectivityManager
import android.os.ParcelFileDescriptor
import android.util.Log
import org.meshmsg.meshcore.NetworkBinder
import java.net.InetAddress
import java.net.NetworkInterface

/**
 * Ties each of the core's per-address UDP sockets to the Android network that
 * owns the address. Android picks the routing table per network: without
 * this, packets for a phone on the same Wi-Fi leave through the default
 * network (mobile data when the Wi-Fi has no internet) and are lost, so
 * calls and direct links on a shared Wi-Fi find no path.
 */
class NetBinder(context: Context) : NetworkBinder {
    private val cm = context.applicationContext.getSystemService(ConnectivityManager::class.java)

    override fun bindSocket(fd: Int, ip: String) {
        try {
            val addr = InetAddress.getByName(ip) // a literal address: no lookup
            @Suppress("DEPRECATION")
            val net = cm?.allNetworks?.firstOrNull { n ->
                cm.getLinkProperties(n)?.linkAddresses?.any { it.address == addr } == true
            }
            if (net == null) {
                // A hotspot we provide is not a Network; its routes always apply.
                Log.i(TAG, "no network owns $ip")
                return
            }
            // fromFd duplicates the descriptor; the binding is on the socket itself.
            ParcelFileDescriptor.fromFd(fd).use { net.bindSocket(it.fileDescriptor) }
            Log.i(TAG, "socket on $ip bound to $net")
        } catch (e: Exception) {
            Log.w(TAG, "bind $ip: $e")
        }
    }

    override fun localAddrs(): String = try {
        val out = ArrayList<String>()
        for (nif in NetworkInterface.getNetworkInterfaces() ?: return "") {
            if (!nif.isUp || nif.isLoopback) continue
            for (a in nif.inetAddresses) {
                if (a.isLoopbackAddress || a.isLinkLocalAddress || a.isMulticastAddress) continue
                a.hostAddress?.substringBefore('%')?.let(out::add)
            }
        }
        Log.i(TAG, "local addresses: $out")
        out.joinToString(",")
    } catch (e: Exception) {
        Log.w(TAG, "local addresses: $e")
        ""
    }

    private companion object {
        const val TAG = "NetBinder"
    }
}
