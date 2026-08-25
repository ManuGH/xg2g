package io.github.manugh.xg2g.android.transport

import io.github.manugh.xg2g.android.ServerEndpoint
import org.json.JSONArray
import io.github.manugh.xg2g.android.contract.PublishedEndpoint as WirePublishedEndpoint

// The one place a published endpoint crosses from the wire into the app.
//
// Three transports used to decode this array themselves — the device-auth
// repository, the pairing client and the native auth transport — with the same
// ten optString/optBoolean calls copied between them. Three copies of a wire
// shape is three chances for one of them to disagree with the schema, which is
// the reason the contract is generated in the first place.

/**
 * Decodes a published-endpoint array.
 *
 * Each element is decoded by the generated contract type, so the field names
 * and their types come from the schema rather than from a string literal here.
 * A malformed element is skipped rather than failing the whole array: an
 * endpoint list that is partly unreadable should still yield the endpoints that
 * are readable, because the alternative is a client that cannot reach a server
 * it was told about.
 */
internal fun parseServerEndpoints(array: JSONArray?): List<ServerEndpoint> {
    if (array == null) return emptyList()
    return (0 until array.length()).mapNotNull { index ->
        val item = array.optJSONObject(index) ?: return@mapNotNull null
        runCatching { WirePublishedEndpoint.fromJson(item).toDomain() }.getOrNull()
    }
}

/**
 * The wire endpoint as the app uses it.
 *
 * The schema models kind, TLS mode and source as enums; the app stores and
 * compares them as the strings they are on the wire, so the conversion happens
 * here instead of at every comparison.
 */
internal fun WirePublishedEndpoint.toDomain(): ServerEndpoint = ServerEndpoint(
    url = url,
    kind = kind.wireValue,
    priority = priority,
    tlsMode = tlsMode.wireValue,
    allowPairing = allowPairing,
    allowStreaming = allowStreaming,
    allowWeb = allowWeb,
    allowNative = allowNative,
    advertiseReason = advertiseReason,
    source = source.wireValue
)
