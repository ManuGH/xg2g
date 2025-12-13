// SPDX-License-Identifier: MIT

// Package openwebif provides OpenWebIF client functionality for Enigma2 receivers.
package openwebif

import (
	"strings"
)

// PiconURL generates the URL for a channel's picon image based on the service reference.
func PiconURL(owiBase, sref string) string {
	// Normalize service reference for picon lookup:
	// HD channels (type 19, 1F, 16, etc.) should fall back to SD (type 1)
	// This matches OpenWebif's frontend behavior where picons are typically
	// stored with SD service type even for HD channels.
	// e.g., 1:0:1:132F:3EF:1:C00000:0:0:0: -> 1:0:1:132F:3EF:1:C00000:0:0:0:
	normalizedSref := NormalizeServiceRefForPicon(sref)
	// We do NOT normalize by default anymore, to support strict picon matching (e.g. 1_0_19...)
	// UPDATE: We DO normalize now to fix broken picons for HD/HEVC channels (as requested)

	// Convert service reference colons to underscores for Enigma2 picon naming
	// e.g., 1:0:1:132F:3EF:1:C00000:0:0:0: -> 1_0_1_132F_3EF_1_C00000_0_0_0
	piconRef := strings.ReplaceAll(normalizedSref, ":", "_")
	piconRef = strings.TrimSuffix(piconRef, "_") // Remove trailing underscore

	// If owiBase already contains the file API path, just append the picon filename
	if strings.Contains(owiBase, "/file?file=") {
		return owiBase + "/" + piconRef + ".png"
	}

	// Standard format: append /picon/ path with converted service reference
	return strings.TrimRight(owiBase, "/") + "/picon/" + piconRef + ".png"
}

// NormalizeServiceRefForPicon converts HD service types to SD for picon lookup.
// Most picon sets use SD service type (1) even for HD channels.
// Format: 1:0:ServiceType:SID:TID:NID:Namespace:0:0:0:
func NormalizeServiceRefForPicon(sref string) string {
	// Split service reference by colons
	parts := strings.Split(sref, ":")
	if len(parts) < 3 {
		return sref
	}

	// Check if this is an HD service type (19=HDTV, 1F=HEVC HD, 16=H264 HD, 11=MPEG2 HD)
	serviceType := parts[2]
	if serviceType == "19" || serviceType == "1F" || serviceType == "16" || serviceType == "11" {
		// Convert to SD service type (1)
		parts[2] = "1"
	}

	return strings.Join(parts, ":")
}
