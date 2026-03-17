package validator

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	emailRegex      = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	phoneRegex      = regexp.MustCompile(`^\+?[1-9]\d{1,14}$`)
	usernameRegex   = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,20}$`)
	urlRegex        = regexp.MustCompile(`^https?:\/\/(www\.)?[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b([-a-zA-Z0-9()@:%_\+.~#?&//=]*)$`)
	nameRegex       = regexp.MustCompile(`^[a-zA-Z\s\-'\.]+$`)
	postalRegex     = regexp.MustCompile(`^[A-Z0-9\s\-]{3,10}$`)
	cardNumberRegex = regexp.MustCompile(`[\s\-]`)
	uuidRegex       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	dateRegex       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	timeRegex       = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}$`)
	datetimeRegex   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{3})?Z?$`)
)

// IsValidEmail checks if the email format is valid
func IsValidEmail(email string) bool {
	if strings.TrimSpace(email) == "" {
		return false
	}
	return emailRegex.MatchString(email)
}

// IsValidPhone checks if the phone number format is valid
func IsValidPhone(phone string) bool {
	if strings.TrimSpace(phone) == "" {
		return false
	}
	return phoneRegex.MatchString(phone)
}

// IsValidUsername checks if the username format is valid
func IsValidUsername(username string) bool {
	if strings.TrimSpace(username) == "" {
		return false
	}
	return usernameRegex.MatchString(username)
}

// IsValidURL checks if the URL format is valid
func IsValidURL(url string) bool {
	if strings.TrimSpace(url) == "" {
		return false
	}
	return urlRegex.MatchString(url)
}

// IsStrongPassword checks if the password meets security requirements
func IsStrongPassword(password string) bool {
	if len(password) < 8 {
		return false
	}

	var (
		hasUpper   bool
		hasLower   bool
		hasNumber  bool
		hasSpecial bool
	)

	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsNumber(char):
			hasNumber = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}

	return hasUpper && hasLower && hasNumber && hasSpecial
}

// IsValidName checks if the name contains only letters, spaces, and common punctuation
func IsValidName(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}

	return nameRegex.MatchString(name) && len(name) >= 2
}

// IsValidAge checks if the age is within reasonable bounds
func IsValidAge(age int) bool {
	return age >= 13 && age <= 120
}

// IsValidPostalCode checks if the postal code format is valid (basic check)
func IsValidPostalCode(postalCode string) bool {
	if strings.TrimSpace(postalCode) == "" {
		return false
	}

	// Basic check for common formats (can be enhanced for specific countries)
	return postalRegex.MatchString(strings.ToUpper(postalCode))
}

// IsValidCreditCard checks if the credit card number passes Luhn algorithm
func IsValidCreditCard(cardNumber string) bool {
	// Remove spaces and dashes
	cardNumber = cardNumberRegex.ReplaceAllString(cardNumber, "")

	if len(cardNumber) < 13 || len(cardNumber) > 19 {
		return false
	}

	sum := 0
	alternate := false

	// Process from right to left
	for i := len(cardNumber) - 1; i >= 0; i-- {
		digit := int(cardNumber[i] - '0')

		if alternate {
			digit *= 2
			if digit > 9 {
				digit = (digit % 10) + 1
			}
		}

		sum += digit
		alternate = !alternate
	}

	return sum%10 == 0
}

// IsValidUUID checks if the string is a valid UUID
func IsValidUUID(uuid string) bool {
	return uuidRegex.MatchString(strings.ToLower(uuid))
}

// IsValidDate checks if the date string is in YYYY-MM-DD format
func IsValidDate(date string) bool {
	return dateRegex.MatchString(date)
}

// IsValidTime checks if the time string is in HH:MM:SS format
func IsValidTime(time string) bool {
	return timeRegex.MatchString(time)
}

// IsValidDateTime checks if the datetime string is in ISO format
func IsValidDateTime(datetime string) bool {
	return datetimeRegex.MatchString(datetime)
}
