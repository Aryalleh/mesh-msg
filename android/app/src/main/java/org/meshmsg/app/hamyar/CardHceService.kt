package org.meshmsg.app.hamyar

import android.nfc.cardemulation.HostApduService
import android.os.Bundle

/**
 * The phone as a student card over NFC (declared only in the Hamyar build's manifest, src/hamyar). A checker's phone
 * selects our AID and gets this moment's code. The phone must be unlocked (res/xml/hamyar_card_apdu.xml).
 */
class CardHceService : HostApduService() {
    override fun processCommandApdu(apdu: ByteArray, extras: Bundle?): ByteArray =
        CardCodes.answer(apdu, HamyarApi(this).cachedCard)

    override fun onDeactivated(reason: Int) {}
}
