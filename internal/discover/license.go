package discover

import "strings"

// guessLicense maps the text of a LICENSE or COPYING file to an SPDX id. It
// returns "" when it does not recognise the text.
func guessLicense(text string) string {
	t := strings.ToLower(strings.Join(strings.Fields(text), " "))
	has := func(s string) bool { return strings.Contains(t, s) }
	switch {
	case has("gnu lesser general public license"):
		if has("version 3") {
			return "LGPL-3.0"
		}
		return "LGPL-2.1"
	case has("gnu general public license"):
		return gplVersion(t)
	case has("mit license"), has("permission is hereby granted, free of charge"):
		return "MIT"
	case has("apache license") && has("version 2.0"):
		return "Apache-2.0"
	case has("redistribution and use in source and binary forms") && has("neither the name"):
		return "BSD-3-Clause"
	}
	return ""
}

// gplVersion picks GPL-3.0 or GPL-2.0 from already lower-cased GPL text.
func gplVersion(t string) string {
	switch {
	case strings.Contains(t, "version 3"):
		return "GPL-3.0"
	case strings.Contains(t, "version 2"):
		return "GPL-2.0"
	}
	return ""
}
