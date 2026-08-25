package io.github.manugh.xg2g.android

import io.github.manugh.xg2g.android.transport.ServerTargetResolver

internal data class ServerEndpoint(
    val url: String,
    val kind: String,
    val priority: Int,
    val tlsMode: String,
    val allowPairing: Boolean,
    val allowStreaming: Boolean,
    val allowWeb: Boolean,
    val allowNative: Boolean,
    val advertiseReason: String,
    val source: String
)

internal fun normalizePublishedEndpoints(values: List<ServerEndpoint>): List<ServerEndpoint> {
    if (values.isEmpty()) {
        return emptyList()
    }
    return values
        .asSequence()
        .map { endpoint ->
            endpoint.copy(
                url = endpoint.url.trim(),
                kind = endpoint.kind.trim(),
                tlsMode = endpoint.tlsMode.trim(),
                advertiseReason = endpoint.advertiseReason.trim(),
                source = endpoint.source.trim()
            )
        }
        .filter { it.url.isNotEmpty() }
        .sortedWith(compareBy<ServerEndpoint> { it.priority }.thenBy { it.url })
        .distinctBy { it.url }
        .toList()
}

internal fun preferredNativeServerUrl(
    currentServerUrl: String?,
    endpoints: List<ServerEndpoint>
): String? {
    val normalizedCurrent = currentServerUrl?.let(ServerTargetResolver::normalizeServerUrl)
    val candidates = endpoints
        .asSequence()
        .filter { it.allowNative }
        .mapNotNull { ServerTargetResolver.normalizeServerUrl(it.url) }
        .distinct()
        .toList()

    if (normalizedCurrent != null && normalizedCurrent in candidates) {
        return normalizedCurrent
    }
    return candidates.firstOrNull()
}

internal fun matchesPublishedEndpointServerUrl(baseUrl: String, endpoints: List<ServerEndpoint>): Boolean {
    return endpoints
        .asSequence()
        .mapNotNull { ServerTargetResolver.normalizeServerUrl(it.url) }
        .any { it == baseUrl }
}
