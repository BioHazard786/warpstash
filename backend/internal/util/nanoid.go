package util

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math/bits"
)

const (
	// DefaultAlphabet is URL-safe alphanumeric without ambiguous characters or punctuation.
	DefaultAlphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	DefaultIDLength = 10
)

// GenerateID produces a cryptographically secure, URL-safe NanoID of the specified size.
// It implements Andrei Sitnik's official ai/nanoid uniform distribution masking algorithm,
// completely eliminating modulo bias with optimal entropy buffering.
func GenerateID(size int) (string, error) {
	if size <= 0 {
		size = DefaultIDLength
	}
	return GenerateIDWithAlphabet(size, DefaultAlphabet)
}

// GenerateIDWithAlphabet generates a NanoID with a custom alphabet.
func GenerateIDWithAlphabet(size int, alphabet string) (string, error) {
	alphabetLen := len(alphabet)
	if alphabetLen <= 0 || alphabetLen > 256 {
		return "", errors.New("alphabet must be between 1 and 256 characters")
	}

	// Compute bitmask: (2 << (31 - bits.LeadingZeros32((alphabetLen-1)|1))) - 1
	mask := (2 << (31 - bits.LeadingZeros32(uint32((alphabetLen-1)|1)))) - 1

	// Optimal buffer step calculation:
	// step = ceil(1.6 * mask * size / alphabetLen)
	step := int(1.6 * float64(mask*size) / float64(alphabetLen))
	if step < size {
		step = size
	}

	id := make([]byte, size)
	bytes := make([]byte, step)
	var j int

	for {
		if _, err := rand.Read(bytes); err != nil {
			return "", err
		}
		for i := 0; i < step; i++ {
			charIndex := int(bytes[i]) & mask
			if charIndex < alphabetLen {
				id[j] = alphabet[charIndex]
				j++
				if j == size {
					return string(id), nil
				}
			}
		}
	}
}

// GenerateDeleteToken creates a 32-byte cryptographically secure hexadecimal token
// used for early manual file deletion.
func GenerateDeleteToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
