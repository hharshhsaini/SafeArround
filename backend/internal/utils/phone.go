package utils

import "strings"

// NormalizePhone turns a phone number as people type it into the E.164 form
// stored for users and emergency contacts (e.g. "+919876543210"), so SMS can
// be sent to it and it matches a registered user's number. Spaces, dashes,
// dots and brackets are dropped; a bare 10-digit number is taken as Indian.
func NormalizePhone(phone string) string {
	phone = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '.', '(', ')':
			return -1
		}
		return r
	}, strings.TrimSpace(phone))

	if len(phone) == 10 {
		return "+91" + phone
	}
	if !strings.HasPrefix(phone, "+") {
		return "+" + phone
	}
	return phone
}
