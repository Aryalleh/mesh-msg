package org.meshmsg.app

import androidx.compose.runtime.Composable
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onRoot
import com.github.takahirom.roborazzi.captureRoboImage
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.meshmsg.app.data.BatteryMode
import org.meshmsg.app.data.Chat
import org.meshmsg.app.data.Contact
import org.meshmsg.app.data.Delivery
import org.meshmsg.app.data.LoRaNode
import org.meshmsg.app.data.Message
import org.meshmsg.app.data.NetStatus
import org.meshmsg.app.data.PathInfo
import org.meshmsg.app.data.PathState
import org.meshmsg.app.data.Relay
import org.meshmsg.app.ui.AddContactScreen
import org.meshmsg.app.ui.ChatScreen
import org.meshmsg.app.ui.ChatsScreen
import org.meshmsg.app.ui.LinkError
import org.meshmsg.app.ui.NetworkScreen
import org.meshmsg.app.ui.OnboardingScreen
import org.meshmsg.app.ui.SettingsActions
import org.meshmsg.app.ui.SettingsScreen
import org.meshmsg.app.ui.SettingsState
import org.meshmsg.app.ui.TextLimits
import org.meshmsg.app.ui.theme.MeshMsgTheme
import org.robolectric.ParameterizedRobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.util.TimeZone

/**
 * Renders every screen on the JVM (Robolectric native graphics) to
 * build/outputs/roborazzi/, in dark and light, English and Persian (RTL), on a
 * small phone, and once at font scale 2. `./gradlew recordRoborazziDebug`.
 */
@RunWith(ParameterizedRobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34])
class ScreenshotTest(private val variant: Variant) {
    data class Variant(val name: String, val qualifiers: String, val dark: Boolean) {
        override fun toString() = name
    }

    companion object {
        @JvmStatic
        @ParameterizedRobolectricTestRunner.Parameters(name = "{0}")
        fun variants() = listOf(
            Variant("fa-dark", "fa-w360dp-h740dp-xhdpi", true),
            Variant("en-dark", "en-w360dp-h740dp-xhdpi", true),
            Variant("fa-light", "fa-w360dp-h740dp-xhdpi", false),
            Variant("en-light", "en-w360dp-h740dp-xhdpi", false),
            Variant("fa-small-bigfont", "fa-w320dp-h568dp-xhdpi", true),
        )

        // 2026-11-11 18:30 UTC
        const val NOW = 1794421800000L
        const val MIN = 60_000L
    }

    @get:Rule val compose = createComposeRule()

    private val fa get() = variant.name.startsWith("fa")

    private fun shot(name: String, content: @Composable () -> Unit) {
        TimeZone.setDefault(TimeZone.getTimeZone("UTC"))
        RuntimeEnvironment.setQualifiers("+" + variant.qualifiers)
        if (variant.name.endsWith("bigfont")) RuntimeEnvironment.setFontScale(2f)
        val file = "build/outputs/roborazzi/${name}_${variant.name}.png"
        compose.setContent { MeshMsgTheme(dark = variant.dark, content = content) }
        compose.onRoot().captureRoboImage(file)
    }

    private val sara = Contact("3bb4c2a7e6a56b04", if (fa) "سارا" else "Sara", "meshmsg:x")
    private val ali = Contact("a524e6359d9f7453", if (fa) "علی" else "Ali", "meshmsg:y")
    private val unnamed = Contact("6a07a087418c93f0", "", "meshmsg:z")

    private fun msg(id: Int, out: Boolean, text: String, ago: Long, d: Delivery = Delivery.Delivered) =
        Message("$id", sara.hash, out, text, NOW - ago, NOW - ago, d)

    private val conversation
        get() = if (fa) listOf(
            msg(1, false, "سلام، اینترنت اونجا هم قطعه؟", 26 * 60 * MIN),
            msg(2, true, "آره از دیشب. ولی این برنامه کار می‌کنه!", 25 * 60 * MIN),
            msg(3, false, "خداروشکر. مامان اینا خوبن؟", 40 * MIN),
            msg(4, true, "همه خوبن، نگران نباش. فردا میام پیشتون.", 12 * MIN, Delivery.Delivered),
            msg(5, true, "چیزی لازم ندارید بیارم؟", 3 * MIN, Delivery.Sent),
            msg(6, true, "اگه جواب ندادم یعنی هنوز تو راهم", 1 * MIN, Delivery.Queued),
        ) else listOf(
            msg(1, false, "Hi, is the internet down there too?", 26 * 60 * MIN),
            msg(2, true, "Yes, since last night. But this app works!", 25 * 60 * MIN),
            msg(3, false, "Thank god. Is the family OK?", 40 * MIN),
            msg(4, true, "Everyone is fine, don't worry. I'll come over tomorrow.", 12 * MIN, Delivery.Delivered),
            msg(5, true, "Do you need me to bring anything?", 3 * MIN, Delivery.Sent),
            msg(6, true, "If I don't answer I'm still on the road", 1 * MIN, Delivery.Queued),
        )

    private val limits = TextLimits(onePacket = 172, max = 1800, bytes = { it.toByteArray().size })

    @Test fun chats() = shot("chats") {
        ChatsScreen(
            chats = listOf(
                Chat(sara, conversation.last(), 0),
                Chat(ali, Message("9", ali.hash, false, if (fa) "رسیدی خبر بده" else "Tell me when you arrive", NOW - 50 * MIN, NOW - 50 * MIN, Delivery.Delivered), 2),
                Chat(unnamed, null, 0),
            ),
            anyPathUp = true, onOpen = {}, onAdd = {}, nowMs = NOW,
        )
    }

    @Test fun chatsNoPath() = shot("chats-nopath") {
        ChatsScreen(listOf(Chat(sara, conversation.last(), 0)), anyPathUp = false, onOpen = {}, onAdd = {}, nowMs = NOW)
    }

    @Test fun chatsEmpty() = shot("chats-empty") { ChatsScreen(emptyList(), anyPathUp = false, onOpen = {}, onAdd = {}, nowMs = NOW) }

    @Test fun chat() = shot("chat") {
        ChatScreen(sara, conversation, anyPathUp = true, limits = limits, onSend = {}, onBack = {}, onRename = {}, onRemove = {}, nowMs = NOW)
    }

    @Test fun chatLongDraft() = shot("chat-longdraft") {
        ChatScreen(
            sara, conversation.take(2), anyPathUp = false, limits = limits, onSend = {}, onBack = {}, onRename = {}, onRemove = {}, nowMs = NOW,
            initialDraft = "x".repeat(160),
        )
    }

    @Test fun addContact() = shot("add") {
        AddContactScreen("meshmsg:Jf3kQ0vT8mZp2xW7aYbN5cR1dE6gH9iK4lM0oP3qS8u", onBack = {}, onCopy = {}, onShare = {}, onPaste = { "" }, onAdd = { _, _ -> null })
    }

    @Test fun addContactError() = shot("add-error") {
        AddContactScreen(
            "meshmsg:Jf3kQ0vT8mZp2xW7aYbN5cR1dE6gH9iK4lM0oP3qS8u", onBack = {}, onCopy = {}, onShare = {}, onPaste = { "" }, onAdd = { _, _ -> null },
            initialLink = "https://example.com/not-a-link", initialError = LinkError.Invalid,
        )
    }

    @Test fun network() = shot("network") {
        NetworkScreen(
            NetStatus(
                hash = "3bb4", stored = 42, knownNodes = 17, gateway = true, forwarded = 128,
                paths = listOf(
                    PathInfo(PathInfo.Kind.Nearby, "ble", PathState.Up, "2 peers"),
                    PathInfo(PathInfo.Kind.LoRa, "usb", PathState.Up, "client"),
                    PathInfo(PathInfo.Kind.Relay, "ir-1", PathState.Waiting, "unreachable"),
                ),
                lora = listOf(LoRaNode("usb", true, "", "client", "EU_868", "BALANCED", 869_525_000, 14, 3, 3900, 36_000, 360_000)),
            ),
            bluetoothAllowed = true, onAllowBluetooth = {}, onConnectUsb = {},
        )
    }

    @Test fun networkNothing() = shot("network-nothing") {
        NetworkScreen(NetStatus(), bluetoothAllowed = false, onAllowBluetooth = {}, onConnectUsb = {})
    }

    private val noActions = object : SettingsActions {
        override fun setName(name: String) {}
        override fun setStealth(on: Boolean) {}
        override fun setGateway(on: Boolean) {}
        override fun setBattery(mode: BatteryMode) {}
        override fun addRelay(relay: Relay) {}
        override fun removeRelay(relay: Relay) {}
        override fun wipe() {}
    }

    private val settingsState
        get() = SettingsState(
            displayName = if (fa) "رضا" else "Reza", myHash = "3bb4c2a7e6a56b04fe21bf96da1c4c67", stealth = true, gateway = false,
            battery = BatteryMode.Saver, relays = listOf(Relay("ir-1", "https://relay-ir.example")),
        )

    @Test fun settings() = shot("settings") { SettingsScreen(settingsState, noActions) }

    // Dialogs (rename, remove, add relay, erase) are not captured: under
    // Robolectric a Compose Dialog window never reports idle at these screen
    // sizes, even an empty AlertDialog. They need a device or emulator.

    @Test fun onboarding() = shot("onboarding") { OnboardingScreen(onStart = {}) }
}
