package server

import (
	"fmt"
	"regexp"
	"strings"
)

const maxSlugLength = 63

var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

func validateSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("slug must be provided")
	}
	if len(slug) > maxSlugLength {
		return fmt.Errorf("slug must be %d characters or less", maxSlugLength)
	}
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("slug must match %s", slugPattern.String())
	}
	return nil
}

func validateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name must be provided")
	}
	return nil
}

func validatePermissions(permissions []string) error {
	seen := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		if _, ok := permissionToRelation[permission]; !ok {
			return fmt.Errorf("unknown permission %q", permission)
		}
		if _, dup := seen[permission]; dup {
			return fmt.Errorf("duplicate permission %q", permission)
		}
		seen[permission] = struct{}{}
	}
	return nil
}
