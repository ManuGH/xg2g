package io.github.manugh.xg2g.android.auth

/**
 * Converts JCA ECDSA signatures into the encoding JWS ES256 requires.
 *
 * `Signature.getInstance("SHA256withECDSA")` (both the AndroidKeyStore provider and the
 * software provider) emits an ASN.1 DER `SEQUENCE { INTEGER r, INTEGER s }`, which for P-256
 * is 68-72 bytes. JWS ES256 (RFC 7518 §3.4) instead requires the raw concatenation R || S,
 * each left-padded to exactly 32 bytes. Handing DER to a spec-compliant verifier - such as the
 * xg2g backend's `verifyES256Signature`, which rejects anything that is not exactly 64 bytes -
 * fails every time.
 */
internal object ES256Signature {

    private const val COORDINATE_LENGTH = 32
    private const val RAW_LENGTH = COORDINATE_LENGTH * 2

    private const val TAG_SEQUENCE = 0x30
    private const val TAG_INTEGER = 0x02

    /**
     * Converts a DER-encoded ECDSA P-256 signature into the raw 64-byte R || S form.
     *
     * @throws IllegalArgumentException if [der] is not a well-formed P-256 ECDSA signature.
     */
    fun derToRaw(der: ByteArray): ByteArray {
        var offset = 0

        require(der.size >= 8) { "DER signature too short: ${der.size} bytes" }
        require(der[offset].toInt() and 0xFF == TAG_SEQUENCE) {
            "DER signature must start with SEQUENCE tag, got 0x%02x".format(der[offset])
        }
        offset++

        val sequenceLength = readLength(der, offset).also { offset = it.nextOffset }.value
        require(offset + sequenceLength == der.size) {
            "DER SEQUENCE length $sequenceLength does not match payload size ${der.size - offset}"
        }

        val r = readInteger(der, offset).also { offset = it.nextOffset }.magnitude
        val s = readInteger(der, offset).also { offset = it.nextOffset }.magnitude
        require(offset == der.size) { "unexpected trailing bytes in DER signature" }

        val raw = ByteArray(RAW_LENGTH)
        System.arraycopy(r, 0, raw, COORDINATE_LENGTH - r.size, r.size)
        System.arraycopy(s, 0, raw, RAW_LENGTH - s.size, s.size)
        return raw
    }

    private class Length(val value: Int, val nextOffset: Int)

    private class Integer(val magnitude: ByteArray, val nextOffset: Int)

    private fun readLength(der: ByteArray, start: Int): Length {
        var offset = start
        require(offset < der.size) { "truncated DER signature" }

        val first = der[offset++].toInt() and 0xFF
        if (first and 0x80 == 0) {
            return Length(first, offset)
        }

        val byteCount = first and 0x7F
        require(byteCount in 1..2) { "unsupported DER length encoding: $byteCount bytes" }
        require(offset + byteCount <= der.size) { "truncated DER length field" }

        var value = 0
        repeat(byteCount) {
            value = (value shl 8) or (der[offset++].toInt() and 0xFF)
        }
        return Length(value, offset)
    }

    private fun readInteger(der: ByteArray, start: Int): Integer {
        var offset = start
        require(offset < der.size) { "truncated DER signature: missing INTEGER" }
        require(der[offset].toInt() and 0xFF == TAG_INTEGER) {
            "expected DER INTEGER tag, got 0x%02x".format(der[offset])
        }
        offset++

        val length = readLength(der, offset).also { offset = it.nextOffset }.value
        require(length > 0 && offset + length <= der.size) { "invalid DER INTEGER length: $length" }
        require(der[offset].toInt() and 0x80 == 0) { "negative DER INTEGER in ECDSA signature" }

        // DER encodes minimal two's-complement integers, so a leading 0x00 is present only to
        // keep a 32-byte value positive. Strip it before left-padding to the fixed width.
        var contentStart = offset
        var contentLength = length
        while (contentLength > 1 && der[contentStart].toInt() == 0) {
            contentStart++
            contentLength--
        }
        require(contentLength <= COORDINATE_LENGTH) {
            "ECDSA component exceeds P-256 field size: $contentLength bytes"
        }

        val magnitude = ByteArray(contentLength)
        System.arraycopy(der, contentStart, magnitude, 0, contentLength)
        return Integer(magnitude, offset + length)
    }
}
