package modeltier

import (
	"fmt"
	"regexp"
)

var providerIDPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

// ValidProviderID checks the id of a provider created by /provider: lower-case
// letters, digits, '-' and '_' only (':' would break "providerID:modelID"
// references), and not the reserved "main". Hand-written ids already in
// config.toml are not subject to this rule.
func ValidProviderID(id string) error {
	switch {
	case id == "":
		return fmt.Errorf("modeltier: provider id is empty")
	case id == MainProviderID:
		return fmt.Errorf("modeltier: %q is reserved for the main loop", MainProviderID)
	case !providerIDPattern.MatchString(id):
		return fmt.Errorf("modeltier: provider id %q may only contain lower-case letters, digits, '-' and '_'", id)
	}
	return nil
}
