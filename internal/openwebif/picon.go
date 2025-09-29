// SPDX-License-Identifier: MIT
package openwebif

import (
	"net/url"
	"strings"
)

func PiconURL(owiBase, sref string) string {
	return strings.TrimRight(owiBase, "/") + "/picon/" + url.PathEscape(sref) + ".png"
}
