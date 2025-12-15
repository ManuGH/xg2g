// SPDX-License-Identifier: MIT

package proxy

import "github.com/ManuGH/xg2g/internal/core/useragent"

func IsSafariClient(userAgent string) bool {
	return useragent.IsSafariBrowser(userAgent)
}

func IsNativeAppleClient(userAgent string) bool {
	return useragent.IsNativeAppleClient(userAgent)
}
