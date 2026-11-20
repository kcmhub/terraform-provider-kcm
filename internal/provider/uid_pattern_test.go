package provider

import "regexp"

// regexpUID matches the uid KCM assigns: the lower case name as a slug, then six hexadecimal digits.
func regexpUID() *regexp.Regexp {
	return regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*-[0-9a-f]{6}$`)
}
