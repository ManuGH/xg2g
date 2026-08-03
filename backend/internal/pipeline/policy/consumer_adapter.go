package policy

import (
	"fmt"

	domainPolicy "github.com/ManuGH/xg2g/internal/domain/policy"
)

// ConsumerFromIntent maps an explicit consumer classification string to a domainPolicy.ConsumerType.
// It performs NO string search or URL heuristics. If the input is unmapped or invalid, it returns ErrConsumerMetadataUnavailable.
func ConsumerFromIntent(raw string) (domainPolicy.ConsumerType, error) {
	c := domainPolicy.ConsumerType(raw)
	if !c.IsValid() {
		return "", fmt.Errorf("%w: unrecognized consumer intent %q", ErrConsumerMetadataUnavailable, raw)
	}
	return c, nil
}
