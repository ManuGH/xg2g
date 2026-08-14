package io.github.manugh.xg2g.android.profile

internal class PinVerificationManager {
    private var enteredPin: String = ""

    val currentPinLength: Int
        get() = enteredPin.length

    val isComplete: Boolean
        get() = enteredPin.length == 4

    fun appendDigit(digit: Int): String {
        if (digit in 0..9 && enteredPin.length < 4) {
            enteredPin += digit.toString()
        }
        return enteredPin
    }

    fun deleteDigit(): String {
        if (enteredPin.isNotEmpty()) {
            enteredPin = enteredPin.dropLast(1)
        }
        return enteredPin
    }

    fun clear() {
        enteredPin = ""
    }

    fun verifyPin(expectedPin: String): Boolean {
        val matches = enteredPin == expectedPin.trim()
        if (!matches) {
            clear()
        }
        return matches
    }
}
