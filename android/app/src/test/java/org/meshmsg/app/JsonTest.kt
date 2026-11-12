package org.meshmsg.app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.meshmsg.app.data.Delivery
import org.meshmsg.app.data.GroupKind
import org.meshmsg.app.data.Json
import org.meshmsg.app.data.PathInfo
import org.meshmsg.app.data.PathState
import org.meshmsg.app.data.DEFAULT_RELAYS
import org.meshmsg.app.data.Settings
import org.meshmsg.app.data.Relay
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import org.json.JSONObject

/** Parsing of the JSON the Go core really produces (samples taken from `go test ./mobile`). */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class JsonTest {
    private val chats = """[
      {"contact":{"hash":"aa11","name":"Sara","uri":"meshmsg:AAA?n=Sara","added":"2026-11-11T10:00:00Z"},
       "last":{"id":"01","peer":"aa11","outgoing":true,"text":"سلام","sent":"2026-11-11T10:05:00Z","received":"2026-11-11T10:05:00.123Z","delivered":false},
       "unread":0},
      {"contact":{"hash":"bb22cc33dd44","name":"","uri":"meshmsg:BBB","added":"2026-11-11T10:00:00Z"},"unread":3}
    ]"""

    @Test fun chats() {
        val up = Json.chats(chats, linkUp = true)
        assertEquals(2, up.size)
        assertEquals("Sara", up[0].contact.displayName)
        assertEquals("سلام", up[0].last!!.text)
        assertEquals(Delivery.Sent, up[0].last!!.delivery)
        assertEquals(1794391500000L, up[0].last!!.sentMs)
        assertEquals("bb22cc33", up[1].contact.displayName) // unnamed: short hash
        assertNull(up[1].last)
        assertEquals(3, up[1].unread)
        // With no path the same message is only queued: the UI must not say "sent".
        assertEquals(Delivery.Queued, Json.chats(chats, linkUp = false)[0].last!!.delivery)
        assertTrue(Json.chats("[]", true).isEmpty())
    }

    // From `go test ./mobile` (TestGroupOverBLEThroughNonMember).
    @Test fun groupChats() {
        val list = Json.chats(
            """[{"contact":{"hash":"","name":"","uri":"","added":"0001-01-01T00:00:00Z"},
                 "group":{"hash":"b520a898","name":"Family","title":"Family","kind":"private","joined":"2026-11-12T11:00:00Z","owner":"aa11","is_owner":true,
                   "members":[{"hash":"aa11","name":"Me","owner":true,"you":true},{"hash":"bb22","name":"Sara"}]},
                 "last":{"id":"07","peer":"b520a898","outgoing":false,"text":"از طریق بلوتوث","sent":"2026-11-12T11:00:00Z","received":"2026-11-12T11:00:01Z","delivered":false,"group":true,"from":"aa11bb22cc33","from_name":"Ali"},
                 "unread":1},
               {"contact":{"hash":"","name":"","uri":"","added":"0001-01-01T00:00:00Z"},
                 "group":{"hash":"22f95cf3","name":"global","title":"#global","kind":"global","uri":"meshgroup:#global","joined":"2026-11-12T11:00:00Z"},
                 "unread":0}]""",
            linkUp = true,
        )
        assertEquals(2, list.size)
        val family = list[0].contact
        assertEquals("b520a898", family.hash)
        assertEquals("Family", family.displayName)
        assertEquals(GroupKind.Private, family.group)
        assertEquals("", family.uri)
        assertTrue(family.isOwner)
        assertFalse(family.removed)
        assertEquals(listOf("Me", "Sara"), family.members.map { it.displayName })
        assertTrue(family.members[0].owner && family.members[0].you)
        assertTrue(list[0].last!!.group)
        assertEquals("Ali", list[0].last!!.fromName)
        assertEquals(GroupKind.Global, list[1].contact.group)
        assertTrue(list[1].contact.group!!.public)
        assertEquals("#global", list[1].contact.displayName)
        // A sender without a name shows a short hash.
        val m = Json.message(JSONObject("""{"id":"1","peer":"p","text":"x","group":true,"from":"aa11bb22cc33"}"""), true)
        assertEquals("aa11bb22", m.fromName)
        val sent = Json.message(JSONObject("""{"id":"2","peer":"p","text":"x","group":true,"outgoing":true,"recipients":3,"delivered_to":2}"""), true)
        assertEquals(3, sent.recipients)
        assertEquals(2, sent.deliveredTo)
        assertEquals(Delivery.Sent, sent.delivery)
        assertFalse(Json.chats(chats, true)[0].contact.isGroup)
    }

    @Test fun messages() {
        val ms = Json.messages(
            """[{"id":"1","peer":"aa","outgoing":true,"text":"a","sent":"2026-11-11T10:05:00Z","received":"2026-11-11T10:05:00Z","delivered":true},
                {"id":"2","peer":"aa","outgoing":false,"text":"b","sent":"2026-11-11T10:06:00Z","received":"2026-11-11T10:06:02Z","delivered":false}]""",
            linkUp = false,
        )
        assertEquals(Delivery.Delivered, ms[0].delivery)
        assertFalse(ms[1].outgoing)
        assertEquals(ms[1].receivedMs, ms[1].timeMs)
    }

    @Test fun status() {
        val s = Json.status(
            """{"hash":"abcd","uri":"meshmsg:X","uptime":"5s","links":{"ble":"2 peers","ir-1":"connected","eu-1":"unreachable","usb":"client, EU_868 869.525 MHz","core":"x"},
                "routes":7,"stored":12,"router":{"In":9,"Dup":1,"Invalid":0,"Delivered":4,"Forwarded":5,"Purged":0},"gateway":true,"bridges":null,"contacts":2,
                "lora":[{"name":"usb","connected":true,"queued":0,"sent":3,"received":4,"bad_frames":0,
                  "status":{"firmware_version":1,"role":1,"region":0,"preset":1,"freq_hz":869525000,"tx_dbm":14,"short_id":9,"uptime_s":60,"stored":2,"queued":0,
                            "neighbours":3,"air_used_ms":36000,"air_budget_ms":360000,"rx_frames":10,"tx_frames":5,"battery_mv":3900}}]}""",
        )
        assertEquals(12, s.stored)
        assertEquals(7, s.knownNodes)
        assertEquals(5L, s.forwarded)
        assertTrue(s.gateway && s.anyPathUp)
        val byName = s.paths.associateBy { it.name }
        assertFalse("core" in byName)
        assertEquals(PathInfo.Kind.Nearby to PathState.Up, byName["ble"]!!.let { it.kind to it.state })
        assertEquals(PathInfo.Kind.Relay to PathState.Up, byName["ir-1"]!!.let { it.kind to it.state })
        assertEquals(PathState.Waiting, byName["eu-1"]!!.state)
        assertEquals(PathInfo.Kind.LoRa to PathState.Up, byName["usb"]!!.let { it.kind to it.state })
        val n = s.lora.single()
        assertEquals(listOf("repeater", "EU_868", "BALANCED"), listOf(n.role, n.region, n.preset))
        assertEquals(3, n.neighbours)
        assertEquals(3900, n.batteryMv)
    }

    @Test fun pathStates() {
        assertEquals(PathState.Waiting, Json.pathState(PathInfo.Kind.Nearby, "no peers"))
        assertEquals(PathState.Up, Json.pathState(PathInfo.Kind.Nearby, "1 peer"))
        assertEquals(PathState.Off, Json.pathState(PathInfo.Kind.Nearby, "off"))
        assertEquals(PathState.Waiting, Json.pathState(PathInfo.Kind.Relay, "connecting"))
        assertEquals(PathState.Waiting, Json.pathState(PathInfo.Kind.LoRa, "disconnected: no LoRa node on USB"))
        assertFalse(Json.status("""{"links":{"ble":"no peers","r":"unreachable"}}""").anyPathUp)
    }

    @Test fun settingsMakeCoreConfig() {
        val s = Settings(RuntimeEnvironment.getApplication())
        assertEquals(listOf(Relay("eu-1", "http://87.106.5.192")), s.relays) // default relay
        s.displayName = "  Ali "
        s.gateway = true
        s.relays = listOf(Relay("ir-1", "https://relay.example", "direct"))
        val o = JSONObject(s.coreConfig())
        assertEquals("Ali", o.getString("name"))
        assertTrue(o.getBoolean("delegate"))
        assertTrue(o.getJSONObject("gateway").getBoolean("enabled"))
        assertFalse(o.getJSONObject("discovery").getBoolean("off")) // relay discovery on by default
        s.discovery = false
        assertTrue(JSONObject(s.coreConfig()).getJSONObject("discovery").getBoolean("off"))
        assertEquals("https://relay.example", o.getJSONArray("relays").getJSONObject(0).getString("url"))
        assertEquals(s.relays, Settings(RuntimeEnvironment.getApplication()).relays) // persisted
        s.clear()
        assertTrue(s.relays == DEFAULT_RELAYS && s.gateway && !s.onboarded) // gateway on by default
        s.relays = emptyList() // removing the default sticks
        assertTrue(Settings(RuntimeEnvironment.getApplication()).relays.isEmpty())
    }
}
